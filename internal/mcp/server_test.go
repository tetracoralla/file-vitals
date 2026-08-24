package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tetracoralla/file-vitals/internal/inspector"
	"github.com/tetracoralla/file-vitals/internal/supervisor"
)

func newTestServer(t *testing.T, root string) *Server {
	t.Helper()
	server, err := New("test", root)
	if err != nil {
		t.Fatal(err)
	}
	server.runWorker = func(ctx context.Context, _ string, file *os.File, request supervisor.Request) inspector.Result {
		source, err := inspector.SourceFromFile(file, request.Name)
		if err != nil {
			return inspector.PublicError(request.Name, request.Mode, request.TimeoutMS, "E_FILE_STAT", "failed")
		}
		return inspector.New().Inspect(ctx, source, inspector.Options{Mode: request.Mode, Hash: request.Hash, Timeout: 5 * time.Second})
	}
	return server
}

func serveLines(t *testing.T, server *Server, lines ...string) []map[string]any {
	t.Helper()
	var output bytes.Buffer
	input := strings.NewReader(strings.Join(lines, "\n") + "\n")
	if err := server.Serve(context.Background(), input, &output); err != nil {
		t.Fatal(err)
	}
	return decodeResponses(t, output.Bytes())
}

func decodeResponses(t *testing.T, data []byte) []map[string]any {
	t.Helper()
	lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
	responses := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		var value map[string]any
		if err := json.Unmarshal(line, &value); err != nil {
			t.Fatalf("invalid response %q: %v", line, err)
		}
		responses = append(responses, value)
	}
	return responses
}

func initializeLine() string {
	return `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`
}

const modernMeta = `"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"test","version":"1"},"io.modelcontextprotocol/clientCapabilities":{}}`

func TestToolListPublishesOneStrictReadOnlyTool(t *testing.T) {
	server := newTestServer(t, t.TempDir())
	responses := serveLines(t, server, initializeLine(), `{"jsonrpc":"2.0","method":"notifications/initialized"}`, `{"jsonrpc":"2.0","id":3,"method":"tools/list","params":{}}`)
	if len(responses) != 2 {
		t.Fatalf("unexpected responses: %#v", responses)
	}
	result := responses[1]["result"].(map[string]any)
	if _, exists := result["resultType"]; exists {
		t.Fatalf("legacy response shape changed: %#v", result)
	}
	tools := result["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tool count: %d", len(tools))
	}
	tool := tools[0].(map[string]any)
	if tool["name"] != "file_inspect" {
		t.Fatalf("tool name: %#v", tool["name"])
	}
	input := tool["inputSchema"].(map[string]any)
	if input["additionalProperties"] != false {
		t.Fatalf("input schema is not strict")
	}
	annotations := tool["annotations"].(map[string]any)
	if annotations["readOnlyHint"] != true || annotations["destructiveHint"] != false || annotations["idempotentHint"] != true || annotations["openWorldHint"] != false {
		t.Fatalf("annotations: %#v", annotations)
	}
}

func TestModernDiscoveryAndStatelessToolFlow(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "ok.txt"), []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := newTestServer(t, root)
	discover := `{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{` + modernMeta + `}}`
	list := `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{` + modernMeta + `}}`
	call := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"file_inspect","arguments":{"path":"ok.txt","mode":"quick"},` + modernMeta + `}}`
	responses := serveLines(t, server, discover, list, call)
	byID := map[float64]map[string]any{}
	for _, item := range responses {
		byID[item["id"].(float64)] = item
	}
	if len(byID) != 3 {
		t.Fatalf("responses: %#v", responses)
	}
	discovery := byID[1]["result"].(map[string]any)
	versions := discovery["supportedVersions"].([]any)
	if discovery["resultType"] != "complete" || len(versions) != 1 || versions[0] != modernProtocol || discovery["ttlMs"] == nil || discovery["cacheScope"] != "private" {
		t.Fatalf("discovery result: %#v", discovery)
	}
	metadata := discovery["_meta"].(map[string]any)
	if metadata["io.modelcontextprotocol/serverInfo"] == nil {
		t.Fatalf("server identity absent: %#v", discovery)
	}
	listed := byID[2]["result"].(map[string]any)
	if listed["resultType"] != "complete" || len(listed["tools"].([]any)) != 1 || listed["ttlMs"] == nil || listed["_meta"] == nil {
		t.Fatalf("modern tools/list: %#v", listed)
	}
	called := byID[3]["result"].(map[string]any)
	if called["resultType"] != "complete" || called["_meta"] == nil || called["structuredContent"].(map[string]any)["status"] != "ok" {
		t.Fatalf("modern tools/call: %#v", called)
	}
}

func TestModernProtocolMetadataIsValidated(t *testing.T) {
	server := newTestServer(t, t.TempDir())
	missingCapabilities := `{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28"}}}`
	unsupported := `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2099-01-01","io.modelcontextprotocol/clientCapabilities":{}}}}`
	legacyWithoutInitialize := `{"jsonrpc":"2.0","id":3,"method":"tools/list","params":{}}`
	malformedClientInfo := `{"jsonrpc":"2.0","id":4,"method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{},"io.modelcontextprotocol/clientInfo":"malformed"}}}`
	optionalClientInfo := `{"jsonrpc":"2.0","id":5,"method":"ping","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}`
	responses := serveLines(t, server, missingCapabilities, unsupported, legacyWithoutInitialize, malformedClientInfo, optionalClientInfo)
	byID := map[float64]map[string]any{}
	for _, item := range responses {
		byID[item["id"].(float64)] = item
	}
	if byID[1]["error"].(map[string]any)["code"] != float64(-32602) {
		t.Fatalf("missing capabilities: %#v", byID[1])
	}
	protocolError := byID[2]["error"].(map[string]any)
	if protocolError["code"] != float64(-32022) || protocolError["data"].(map[string]any)["requested"] != "2099-01-01" {
		t.Fatalf("unsupported version: %#v", protocolError)
	}
	if byID[3]["error"].(map[string]any)["code"] != float64(-32002) {
		t.Fatalf("legacy initialization guard: %#v", byID[3])
	}
	if byID[4]["error"].(map[string]any)["code"] != float64(-32602) {
		t.Fatalf("malformed client info: %#v", byID[4])
	}
	if byID[5]["error"] != nil || byID[5]["result"].(map[string]any)["resultType"] != "complete" {
		t.Fatalf("optional client info: %#v", byID[5])
	}
}

func TestConcurrencyQueueSharesCompleteCallDeadline(t *testing.T) {
	root := t.TempDir()
	for index := 1; index <= maxConcurrentCalls+1; index++ {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("%d.txt", index)), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	server := newTestServer(t, root)
	server.callTimeout = 80 * time.Millisecond
	var executions atomic.Int32
	server.runWorker = func(ctx context.Context, _ string, _ *os.File, request supervisor.Request) inspector.Result {
		executions.Add(1)
		time.Sleep(150 * time.Millisecond)
		return contextFailure(toolInput{Path: request.Name, Mode: request.Mode}, ctx.Err(), server.callTimeout)
	}
	lines := []string{initializeLine()}
	for index := 1; index <= maxConcurrentCalls+1; index++ {
		lines = append(lines, fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"tools/call","params":{"name":"file_inspect","arguments":{"path":"%d.txt"}}}`, index+1, index))
	}
	responses := serveLines(t, server, lines...)
	if executions.Load() != maxConcurrentCalls {
		t.Fatalf("queue admitted %d workers, want %d", executions.Load(), maxConcurrentCalls)
	}
	if len(responses) != maxConcurrentCalls+2 {
		t.Fatalf("response count: %d", len(responses))
	}
	for _, item := range responses {
		id, _ := item["id"].(float64)
		if id <= 1 {
			continue
		}
		structured := item["result"].(map[string]any)["structuredContent"].(map[string]any)
		if structured["error"].(map[string]any)["code"] != "E_TIMEOUT" {
			t.Fatalf("call %v escaped shared deadline: %#v", id, structured)
		}
	}
}

func TestToolCallRejectsUnknownFieldsBeforeExecution(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "ok.txt"), []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := newTestServer(t, root)
	called := false
	server.runWorker = func(context.Context, string, *os.File, supervisor.Request) inspector.Result {
		called = true
		return inspector.Result{}
	}
	responses := serveLines(t, server, initializeLine(), `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"file_inspect","arguments":{"path":"ok.txt","mdoe":"quick"}}}`)
	if called {
		t.Fatal("worker executed for invalid input")
	}
	toolResult := responses[1]["result"].(map[string]any)
	if toolResult["isError"] != true {
		t.Fatalf("invalid input was not a tool error: %#v", toolResult)
	}
	structured := toolResult["structuredContent"].(map[string]any)
	errorValue := structured["error"].(map[string]any)
	if errorValue["code"] != "E_INVALID_INPUT" {
		t.Fatalf("wrong code: %#v", errorValue)
	}
}

func TestToolCallUsesRelativeWorkspaceAndReturnsStructuredResult(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "ok.txt"), []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := newTestServer(t, root)
	responses := serveLines(t, server, initializeLine(), `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"file_inspect","arguments":{"path":"ok.txt","mode":"quick"}}}`)
	toolResult := responses[1]["result"].(map[string]any)
	if toolResult["isError"] != false {
		t.Fatalf("call failed: %#v", toolResult)
	}
	structured := toolResult["structuredContent"].(map[string]any)
	if structured["schema_version"] != "1.0" || structured["status"] != "ok" {
		t.Fatalf("bad structured result: %#v", structured)
	}
}

func TestCancellationTerminatesCallAndLaterCallRecovers(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"slow.txt", "ok.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("hello\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	server := newTestServer(t, root)
	started := make(chan struct{})
	finished := make(chan struct{})
	defaultRunner := server.runWorker
	server.runWorker = func(ctx context.Context, executable string, file *os.File, request supervisor.Request) inspector.Result {
		if request.Name == "slow.txt" {
			close(started)
			<-ctx.Done()
			close(finished)
			return inspector.PublicError(request.Name, request.Mode, request.TimeoutMS, "E_CANCELLED", "The inspection was cancelled.")
		}
		return defaultRunner(ctx, executable, file, request)
	}
	reader, writer := io.Pipe()
	var output bytes.Buffer
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(context.Background(), reader, &output) }()
	_, _ = io.WriteString(writer, initializeLine()+"\n")
	_, _ = io.WriteString(writer, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"file_inspect","arguments":{"path":"slow.txt"}}}`+"\n")
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("slow call did not start")
	}
	_, _ = io.WriteString(writer, `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":2,"reason":"test"}}`+"\n")
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("cancel did not reach worker")
	}
	_, _ = io.WriteString(writer, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"file_inspect","arguments":{"path":"ok.txt","mode":"quick"}}}`+"\n")
	_ = writer.Close()
	if err := <-serveDone; err != nil {
		t.Fatal(err)
	}
	responses := decodeResponses(t, output.Bytes())
	byID := map[float64]map[string]any{}
	for _, response := range responses {
		if id, ok := response["id"].(float64); ok {
			byID[id] = response
		}
	}
	cancelled := byID[2]["result"].(map[string]any)["structuredContent"].(map[string]any)
	if cancelled["status"] != "error" || cancelled["error"].(map[string]any)["code"] != "E_CANCELLED" {
		t.Fatalf("cancel result: %#v", cancelled)
	}
	recovered := byID[3]["result"].(map[string]any)["structuredContent"].(map[string]any)
	if recovered["status"] != "ok" {
		t.Fatalf("later call did not recover: %#v", recovered)
	}
	for _, line := range bytes.Split(bytes.TrimSpace(output.Bytes()), []byte("\n")) {
		if len(line)+1 > maxEnvelopeBytes {
			t.Fatalf("MCP envelope too large: %d", len(line)+1)
		}
	}
}

func TestImmediateCancellationCannotRaceDispatchRegistration(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "ok.txt"), []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := newTestServer(t, root)
	var output bytes.Buffer
	input := strings.Join([]string{
		initializeLine(),
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"file_inspect","arguments":{"path":"ok.txt"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":2,"reason":"immediate"}}`,
	}, "\n") + "\n"
	if err := server.Serve(context.Background(), strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	responses := decodeResponses(t, output.Bytes())
	byID := map[float64]map[string]any{}
	for _, item := range responses {
		byID[item["id"].(float64)] = item
	}
	structured := byID[2]["result"].(map[string]any)["structuredContent"].(map[string]any)
	if structured["error"].(map[string]any)["code"] != "E_CANCELLED" {
		t.Fatalf("immediate cancellation was lost: %#v", structured)
	}
}

func TestOversizedRequestIsRejectedWithoutKillingTheSession(t *testing.T) {
	server := newTestServer(t, t.TempDir())
	var output bytes.Buffer
	oversized := `{"jsonrpc":"2.0","id":2,"method":"ping","params":{"pad":"` +
		strings.Repeat("x", maxRequestBytes) + `"}}` + "\n"
	followUp := `{"jsonrpc":"2.0","id":3,"method":"ping","params":{}}` + "\n"
	input := strings.NewReader(initializeLine() + "\n" + oversized + followUp)
	if err := server.Serve(context.Background(), input, &output); err != nil {
		t.Fatalf("session died on an oversized request: %v", err)
	}
	responses := decodeResponses(t, output.Bytes())
	if len(responses) != 3 {
		t.Fatalf("response count: %d (%s)", len(responses), output.String())
	}
	if _, present := responses[1]["id"]; !present || responses[1]["id"] != nil || responses[1]["error"].(map[string]any)["code"] != float64(-32700) {
		t.Fatalf("oversized request was not a parse error: %#v", responses[1])
	}
	if responses[2]["id"] != float64(3) || responses[2]["error"] != nil {
		t.Fatalf("session did not recover after the oversized request: %#v", responses[2])
	}
}
