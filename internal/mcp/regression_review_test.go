package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/tetracoralla/file-vitals/internal/inspector"
	"github.com/tetracoralla/file-vitals/internal/supervisor"
)

// writeScript places an executable stand-in for the worker binary on disk.
func writeScript(t *testing.T, body string) string {
	t.Helper()
	script := filepath.Join(t.TempDir(), "worker.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return script
}

// TestWorkerBudgetBreachEndToEnd wires a real overspilling worker through the
// real supervisor path and asserts the MCP envelope publishes the mapped,
// schema-valid E_RESPONSE_TOO_LARGE error instead of dying.
func TestWorkerBudgetBreachEndToEnd(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "ok.txt"), []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	script := writeScript(t, "head -c 2621440 /dev/zero | tr '\\0' 'x'\n")
	server, err := New(script, root)
	if err != nil {
		t.Fatal(err)
	}
	responses := serveLines(t, server, initializeLine(),
		`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"file_inspect","arguments":{"path":"ok.txt"}}}`)
	if len(responses) != 2 {
		t.Fatalf("response count: %d", len(responses))
	}
	result := responses[1]["result"].(map[string]any)
	if result["isError"] != true {
		t.Fatalf("budget breach was not an error envelope: %#v", result)
	}
	structured := result["structuredContent"].(map[string]any)
	errorInfo := structured["error"].(map[string]any)
	if errorInfo["code"] != "E_RESPONSE_TOO_LARGE" {
		t.Fatalf("budget breach code: %#v", errorInfo)
	}
}

// TestSchemaViolatingWorkerResultIsRewritten drives a worker whose output
// parses as JSON but violates the published schema; the server must rewrite it
// to E_WORKER_PROTOCOL rather than forward it.
func TestSchemaViolatingWorkerResultIsRewritten(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "ok.txt"), []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	script := writeScript(t, `printf '{"schema_version":"1.0","status":"fine"}'`)
	server, err := New(script, root)
	if err != nil {
		t.Fatal(err)
	}
	responses := serveLines(t, server, initializeLine(),
		`{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"file_inspect","arguments":{"path":"ok.txt"}}}`)
	result := responses[1]["result"].(map[string]any)
	structured := result["structuredContent"].(map[string]any)
	if structured["status"] != "error" {
		t.Fatalf("schema violation was forwarded: %#v", structured)
	}
	errorInfo := structured["error"].(map[string]any)
	if errorInfo["code"] != "E_WORKER_PROTOCOL" {
		t.Fatalf("rewrite code: %#v", errorInfo)
	}
}

// TestDuplicateRequestIDKeepsLaterCallCancellable covers the registry fix: an
// earlier call finishing under a duplicated request ID must not unregister the
// later call's cancellation entry.
func TestDuplicateRequestIDKeepsLaterCallCancellable(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "ok.txt"), []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := newTestServer(t, root)

	server.runWorker = func(ctx context.Context, _ string, _ *os.File, request supervisor.Request) inspector.Result {
		<-ctx.Done()
		if ctx.Err() != nil {
			return inspector.PublicError(request.Name, request.Mode, request.TimeoutMS, "E_CANCELLED", "The inspection was cancelled.")
		}
		return inspector.PublicError(request.Name, request.Mode, request.TimeoutMS, "E_INTERNAL", "unexpected live call")
	}

	lines := []string{
		initializeLine(),
		`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"file_inspect","arguments":{"path":"ok.txt"}}}`,
		`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"file_inspect","arguments":{"path":"ok.txt"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":6}}`,
	}
	var output safeBuffer
	input := strings.NewReader(strings.Join(lines, "\n") + "\n")
	if err := server.Serve(context.Background(), input, &output); err != nil {
		t.Fatal(err)
	}
	responses := decodeResponses(t, output.bytes())
	cancelled := 0
	for _, response := range responses {
		result, ok := response["result"].(map[string]any)
		if !ok {
			continue
		}
		structured, _ := result["structuredContent"].(map[string]any)
		if errorInfo, ok := structured["error"].(map[string]any); ok {
			if errorInfo["code"] == "E_CANCELLED" {
				cancelled++
			}
		}
	}
	if cancelled != 2 {
		t.Fatalf("cancellation did not reach every duplicate-ID call: cancelled=%d responses=%d", cancelled, len(responses))
	}
}

type safeBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeBuffer) bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return []byte(b.buf.String())
}
