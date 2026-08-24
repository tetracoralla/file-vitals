package procmon

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	ErrOutput = errors.New("process output limit exceeded")
	ErrMemory = errors.New("process memory limit exceeded")
)

const memorySampleInterval = 100 * time.Millisecond

type Spec struct {
	Name                string
	Args                []string
	Env                 []string
	Stdin               []byte
	File                *os.File
	Files               []*os.File
	StdoutBytes         int
	StderrBytes         int
	MemoryBytes         int64
	InheritProcessGroup bool
}

type Result struct {
	Stdout []byte
	Stderr []byte
}

type limitedBuffer struct {
	mu       sync.Mutex
	buf      bytes.Buffer
	limit    int
	overflow bool
	overOnce sync.Once
	overCh   chan struct{}
}

func newLimitedBuffer(limit int, overflow chan struct{}) *limitedBuffer {
	return &limitedBuffer{limit: limit, overCh: overflow}
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	remaining := b.limit - b.buf.Len()
	if remaining > 0 {
		if remaining > len(p) {
			remaining = len(p)
		}
		_, _ = b.buf.Write(p[:remaining])
	}
	if remaining < len(p) {
		b.overflow = true
		b.overOnce.Do(func() { close(b.overCh) })
	}
	b.mu.Unlock()
	return len(p), nil
}

func (b *limitedBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.buf.Bytes()...)
}

func (b *limitedBuffer) Overflowed() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.overflow
}

func Run(ctx context.Context, spec Spec) (Result, error) {
	if spec.Name == "" {
		return Result{}, errors.New("empty process name")
	}
	if spec.StdoutBytes <= 0 || spec.StderrBytes <= 0 {
		return Result{}, errors.New("process output limits must be positive")
	}

	cmd := exec.Command(spec.Name, spec.Args...)
	if len(spec.Env) > 0 {
		cmd.Env = append(os.Environ(), spec.Env...)
	}
	if !spec.InheritProcessGroup {
		configureProcessGroup(cmd)
	}
	if len(spec.Stdin) > 0 {
		cmd.Stdin = bytes.NewReader(spec.Stdin)
	}
	if len(spec.Files) > 0 {
		cmd.ExtraFiles = append([]*os.File(nil), spec.Files...)
	} else if spec.File != nil {
		cmd.ExtraFiles = []*os.File{spec.File}
	}
	overflow := make(chan struct{})
	stdout := newLimitedBuffer(spec.StdoutBytes, overflow)
	stderr := newLimitedBuffer(spec.StderrBytes, overflow)
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		return Result{}, err
	}
	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()

	ticker := time.NewTicker(memorySampleInterval)
	defer ticker.Stop()
	for {
		select {
		case err := <-waitCh:
			result := Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
			if stdout.Overflowed() || stderr.Overflowed() {
				return result, ErrOutput
			}
			return result, err
		case <-overflow:
			stopProcess(cmd, !spec.InheritProcessGroup)
			<-waitCh
			return Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}, ErrOutput
		case <-ctx.Done():
			stopProcess(cmd, !spec.InheritProcessGroup)
			<-waitCh
			return Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}, ctx.Err()
		case <-ticker.C:
			if spec.MemoryBytes <= 0 {
				continue
			}
			var rss int64
			var err error
			if spec.InheritProcessGroup {
				rss, err = residentBytes(cmd.Process.Pid)
			} else {
				rss, err = residentGroupBytes(cmd.Process.Pid)
			}
			if err == nil && rss > spec.MemoryBytes {
				stopProcess(cmd, !spec.InheritProcessGroup)
				<-waitCh
				return Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}, ErrMemory
			}
		}
	}
}

func stopProcess(cmd *exec.Cmd, processTree bool) {
	if processTree {
		killProcessTree(cmd)
		return
	}
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

func FDPath() string {
	if runtime.GOOS == "linux" {
		return "/proc/self/fd/3"
	}
	return "/dev/fd/3"
}

func residentBytes(pid int) (int64, error) {
	if runtime.GOOS == "linux" {
		data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
		if err != nil {
			return 0, err
		}
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "VmRSS:") {
				fields := strings.Fields(line)
				if len(fields) < 2 {
					break
				}
				kb, err := strconv.ParseInt(fields[1], 10, 64)
				return kb * 1024, err
			}
		}
		return 0, errors.New("VmRSS not found")
	}
	result, err := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0, err
	}
	kb, err := strconv.ParseInt(strings.TrimSpace(string(result)), 10, 64)
	return kb * 1024, err
}
