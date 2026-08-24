//go:build unix

package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestInspectionSignalKillsWorkerAndProbeTree pins the desktop cancellation
// contract: terminating the CLI must cancel the shared inspection context so
// its isolated worker and an active external probe cannot outlive the call.
func TestInspectionSignalKillsWorkerAndProbeTree(t *testing.T) {
	probeDir := t.TempDir()
	probePIDPath := filepath.Join(probeDir, "probe.pid")
	probe := "#!/bin/sh\nprintf '%s\\n' \"$$\" > \"$FILE_VITALS_PROBE_PID_FILE\"\nexec sleep 30\n"
	if err := os.WriteFile(filepath.Join(probeDir, "ffprobe"), []byte(probe), 0o700); err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(t.TempDir(), "slow.mp4")
	if err := os.WriteFile(fixture, []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'}, 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	command := exec.Command(os.Args[0], fixture, "--standard", "--json", "--timeout=30s")
	command.Stdout = &stdout
	command.Stderr = &stderr
	command.Env = append(os.Environ(),
		"FILE_VITALS_TEST_CLI=1",
		"FILE_VITALS_PROBE_PID_FILE="+probePIDPath,
		"PATH="+probeDir+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if command.Process != nil {
			_ = command.Process.Kill()
		}
	})

	probePID := waitForPIDFile(t, probePIDPath, 5*time.Second)
	workerPID := parentPID(t, probePID)
	if err := command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("CLI did not exit after SIGTERM; stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), `"code": "E_CANCELLED"`) {
		t.Fatalf("cancellation result missing; stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
	for _, process := range []struct {
		name string
		pid  int
	}{{"worker", workerPID}, {"probe", probePID}} {
		if !waitForProcessExit(process.pid, 2*time.Second) {
			_ = syscall.Kill(process.pid, syscall.SIGKILL)
			t.Errorf("%s process %d outlived the cancelled CLI", process.name, process.pid)
		}
	}
}

func waitForPIDFile(t *testing.T, path string, timeout time.Duration) int {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			pid, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
			if parseErr == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("probe did not start within %s", timeout)
	return 0
}

func parentPID(t *testing.T, pid int) int {
	t.Helper()
	output, err := exec.Command("ps", "-o", "ppid=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		t.Fatalf("read parent pid for %d: %v", pid, err)
	}
	parent, err := strconv.Atoi(strings.TrimSpace(string(output)))
	if err != nil || parent <= 1 {
		t.Fatalf("invalid parent pid for %d: %q (%v)", pid, output, err)
	}
	return parent
}

func waitForProcessExit(pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		err := syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return true
		}
		if err != nil && !errors.Is(err, syscall.EPERM) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}
