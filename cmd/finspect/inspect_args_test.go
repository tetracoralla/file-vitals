package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tetracoralla/file-vitals/internal/inspector"
)

func TestParseInspectArgsTable(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		err     string
		summary inspectArgs
	}{
		{name: "defaults", args: []string{"a.txt"}, summary: inspectArgs{path: "a.txt", mode: inspector.ModeStandard, hash: inspector.HashNone, timeout: 10 * time.Second}},
		{name: "flags", args: []string{"--deep", "--sha256", "--json", "--timeout=5s", "a.zip"}, summary: inspectArgs{path: "a.zip", mode: inspector.ModeDeep, hash: inspector.HashSHA256, json: true, timeout: 5 * time.Second}},
		{name: "separate values", args: []string{"--mode", "quick", "--hash", "sha256", "a"}, summary: inspectArgs{path: "a", mode: inspector.ModeQuick, hash: inspector.HashSHA256, timeout: 10 * time.Second}},
		{name: "unknown flag", args: []string{"--wat", "a"}, err: "unknown option --wat"},
		{name: "missing value", args: []string{"--mode"}, err: "--mode requires a value"},
		{name: "two paths", args: []string{"a", "b"}, err: "inspect accepts exactly one file"},
		{name: "no path", args: []string{"--json"}, err: "a file path is required"},
		{name: "bad mode", args: []string{"--mode=bogus", "a"}, err: "mode must be quick, standard, or deep"},
		{name: "bad hash", args: []string{"--hash=md5", "a"}, err: "hash must be none or sha256"},
		{name: "bad timeout", args: []string{"--timeout=1ms", "a"}, err: "timeout must be between 100ms and 60s"},
		{name: "unparsable timeout", args: []string{"--timeout", "soon", "a"}, err: "timeout must be a duration such as 5s"},
	}
	for _, item := range cases {
		parsed, err := parseInspectArgs(item.args)
		if item.err != "" {
			if err == nil || err.Error() != item.err {
				t.Fatalf("%s: expected error %q, got %v", item.name, item.err, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: unexpected error %v", item.name, err)
		}
		if parsed != item.summary {
			t.Fatalf("%s: parsed %+v, want %+v", item.name, parsed, item.summary)
		}
	}
}

func TestUsageErrorEmitsStructuredJSONWhenJSONRequested(t *testing.T) {
	for _, args := range [][]string{
		{"--json", "--mode=bogus", "a.txt"},
		{"--wat", "--json", "a.txt"},
	} {
		var stdout, stderr bytes.Buffer
		code := runInspect(args, &stdout, &stderr)
		if code != 2 {
			t.Fatalf("%v: usage exit code: %d", args, code)
		}
		var result inspector.Result
		if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
			t.Fatalf("%v: usage error was not structured JSON: %q (%v)", args, stdout.String(), err)
		}
		if result.Error == nil || result.Error.Code != "E_USAGE" {
			t.Fatalf("%v: usage error code: %#v", args, result.Error)
		}
		if stderr.Len() != 0 {
			t.Fatalf("%v: usage JSON mode still wrote to stderr: %q", args, stderr.String())
		}
	}
}

func TestRunInspectExitCodes(t *testing.T) {
	dir := t.TempDir()
	okPath := filepath.Join(dir, "ok.txt")
	if err := os.WriteFile(okPath, []byte("fine\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	corruptPath := filepath.Join(dir, "broken.json")
	if err := os.WriteFile(corruptPath, []byte("{\"broken\":"), 0o600); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		args []string
		code int
	}{
		{"ok file", []string{"--quick", "--json", okPath}, 0},
		{"missing file", []string{"--quick", "--json", filepath.Join(dir, "nope.txt")}, 1},
		{"directory", []string{"--quick", "--json", dir}, 1},
		{"corrupt json", []string{"--standard", "--json", corruptPath}, 3},
	}
	for _, item := range cases {
		var stdout, stderr bytes.Buffer
		code := runInspect(item.args, &stdout, &stderr)
		if code != item.code {
			t.Fatalf("%s: exit code %d, want %d (stderr=%q)", item.name, code, item.code, stderr.String())
		}
		var result inspector.Result
		if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
			t.Fatalf("%s: output was not a result document: %v", item.name, err)
		}
	}
}

func TestFIFOArgumentIsRejectedFast(t *testing.T) {
	if _, err := os.Stat("/usr/bin/mkfifo"); err != nil {
		t.Skip("mkfifo unavailable")
	}
	fifo := filepath.Join(t.TempDir(), "pipe")
	if out, err := exec.Command("mkfifo", fifo).CombinedOutput(); err != nil {
		t.Skipf("fifo could not be created: %v (%s)", err, out)
	}
	var stdout, stderr bytes.Buffer
	code := runInspect([]string{"--quick", "--json", fifo}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("fifo exit code: %d (stderr=%q)", code, stderr.String())
	}
	var result inspector.Result
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("fifo output was not a result: %v", err)
	}
	if result.Error == nil || result.Error.Code != "E_NOT_REGULAR_FILE" {
		t.Fatalf("fifo was not rejected as non-regular: %#v", result.Error)
	}
	if strings.Contains(stdout.String(), "E_TIMEOUT") {
		t.Fatalf("fifo rejection blocked until the deadline instead of failing fast")
	}
}
