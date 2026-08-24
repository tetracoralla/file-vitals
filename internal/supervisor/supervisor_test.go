package supervisor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tetracoralla/file-vitals/internal/inspector"
)

// writeWorkerScript creates a fake executable that ignores the worker protocol
// and prints to stdout, standing in for the real binary.
func writeWorkerScript(t *testing.T, body string) string {
	t.Helper()
	script := filepath.Join(t.TempDir(), "worker.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return script
}

func workerFile(t *testing.T) *os.File {
	t.Helper()
	path := filepath.Join(t.TempDir(), "target.txt")
	if err := os.WriteFile(path, []byte("target\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { file.Close() })
	return file
}

func run(t *testing.T, script string) inspector.Result {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return Run(ctx, script, workerFile(t), Request{Name: "target.txt", Mode: inspector.ModeStandard, TimeoutMS: 5000})
}

func TestWorkerResponseBudgetBreachIsMapped(t *testing.T) {
	script := writeWorkerScript(t, "head -c 2621440 /dev/zero | tr '\\0' 'x'\n")
	result := run(t, script)
	if result.Status != "error" || result.Error == nil || result.Error.Code != "E_RESPONSE_TOO_LARGE" {
		t.Fatalf("worker response budget breach was not mapped: %#v", result.Error)
	}
}

func TestWorkerInvalidJSONIsProtocolFailure(t *testing.T) {
	script := writeWorkerScript(t, "printf 'not json at all\\n'\n")
	result := run(t, script)
	if result.Status != "error" || result.Error == nil || result.Error.Code != "E_WORKER_PROTOCOL" {
		t.Fatalf("invalid worker output was not a protocol failure: %#v", result.Error)
	}
}

func TestWorkerSchemaInvalidResultIsProtocolFailure(t *testing.T) {
	script := writeWorkerScript(t, `printf '{"schema_version":"9.9","status":"nope"}'`)
	result := run(t, script)
	if result.Status != "error" || result.Error == nil || result.Error.Code != "E_WORKER_PROTOCOL" {
		t.Fatalf("schema-violating worker result was accepted: %#v", result.Error)
	}
}

func TestMissingExecutableIsWorkerFailure(t *testing.T) {
	file := workerFile(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result := Run(ctx, filepath.Join(t.TempDir(), "does-not-exist"), file, Request{Name: "target.txt", TimeoutMS: 5000})
	if result.Status != "error" || result.Error == nil || result.Error.Code != "E_WORKER_FAILED" {
		t.Fatalf("missing worker executable was not a worker failure: %#v", result.Error)
	}
}

func TestValidWorkerResultRoundTrip(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("/bin/sh unavailable")
	}
	// The real binary is only available in cmd/finspect tests; here we verify
	// the JSON contract with a script that emits a minimal valid result.
	script := writeWorkerScript(t, `printf '{"schema_version":"1.1","status":"ok","file":{"name":"t","size_bytes":1,"extension":""},"identity":{"kind":"text","media_type":"text/plain","format":"Plain text","confidence":"probable","candidates":[],"conflicts":[]},"traits":[],"constraints":[],"integrity":{"readable":true},"diagnostics":[],"provenance":[],"limits":{"mode":"standard","response_bytes_max":262144,"timeout_ms":5000,"memory_bytes_max":402653184}}'`)
	result := run(t, script)
	if result.Status != "ok" || strings.Contains(result.Status, "error") {
		t.Fatalf("valid worker result was not passed through: %#v", result)
	}
}
