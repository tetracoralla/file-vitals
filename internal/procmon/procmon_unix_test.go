//go:build unix

package procmon

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestGroupMemoryHelper(t *testing.T) {
	switch os.Getenv("UFI_GROUP_MEMORY_HELPER") {
	case "parent":
		command := exec.Command(os.Args[0], "-test.run=^TestGroupMemoryHelper$")
		command.Env = append(os.Environ(), "UFI_GROUP_MEMORY_HELPER=child")
		if err := command.Start(); err != nil {
			os.Exit(2)
		}
		_ = command.Wait()
		os.Exit(0)
	case "child":
		memory := make([]byte, 96*1024*1024)
		for index := 0; index < len(memory); index += 4096 {
			memory[index] = 1
		}
		time.Sleep(30 * time.Second)
		os.Exit(0)
	}
}

func TestMemoryLimitIncludesDescendants(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := Run(ctx, Spec{
		Name: os.Args[0], Args: []string{"-test.run=^TestGroupMemoryHelper$"}, Env: []string{"UFI_GROUP_MEMORY_HELPER=parent"},
		StdoutBytes: 1024, StderrBytes: 1024, MemoryBytes: 64 * 1024 * 1024,
	})
	if !errors.Is(err, ErrMemory) {
		t.Fatalf("expected aggregate memory limit, got %v", err)
	}
}

func TestMemoryMonitorBlindWindowStaysBounded(t *testing.T) {
	if memorySampleInterval > 100*time.Millisecond {
		t.Fatalf("memory monitor blind window grew to %s", memorySampleInterval)
	}
}

func TestCancellationKillsProcessTree(t *testing.T) {
	pids := make([]int, 0, 5)
	for attempt := 0; attempt < 5; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
		result, err := Run(ctx, Spec{
			Name: "/bin/sh", Args: []string{"-c", "sleep 30 & echo $!; wait"},
			StdoutBytes: 1024, StderrBytes: 1024, MemoryBytes: 128 * 1024 * 1024,
		})
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("attempt %d: expected deadline, got %v", attempt, err)
		}
		pid, parseErr := strconv.Atoi(strings.TrimSpace(string(result.Stdout)))
		if parseErr != nil || pid <= 0 {
			t.Fatalf("attempt %d: missing child pid in %q", attempt, result.Stdout)
		}
		pids = append(pids, pid)
	}
	for _, pid := range pids {
		deadline := time.Now().Add(time.Second)
		gone := false
		for time.Now().Before(deadline) {
			err := syscall.Kill(pid, 0)
			if errors.Is(err, syscall.ESRCH) {
				gone = true
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		if !gone {
			t.Fatalf("child process %d survived repeated parent cancellation", pid)
		}
	}
}

func TestParseProcStatExtractsGroupAndResidentPages(t *testing.T) {
	line := []byte("4242 ((a bad) comm) S 1 7311 7311 7311 0 4194304 999 0 0 0 5 3 0 0 20 0 4 0 123456 262144 300 18446744073709551615 1 1 0 0 0")
	pgrp, pages, ok := parseProcStat(line)
	if !ok || pgrp != 7311 || pages != 300 {
		t.Fatalf("parse result: pgrp=%d pages=%d ok=%t", pgrp, pages, ok)
	}
	if _, _, ok := parseProcStat([]byte("garbage without parens")); ok {
		t.Fatal("malformed stat line was accepted")
	}
	if _, _, ok := parseProcStat([]byte("1 (short) S 1 2")); ok {
		t.Fatal("truncated stat line was accepted")
	}
}
