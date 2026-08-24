package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/tetracoralla/file-vitals/internal/authority"
	"github.com/tetracoralla/file-vitals/internal/inspector"
	"github.com/tetracoralla/file-vitals/internal/linereader"
	"github.com/tetracoralla/file-vitals/internal/supervisor"
	"github.com/tetracoralla/file-vitals/internal/version"
	"github.com/tetracoralla/file-vitals/schemas"
)

const (
	modernProtocol       = "2026-07-28"
	latestLegacyProtocol = "2025-11-25"
	maxRequestBytes      = 1024 * 1024
	maxEnvelopeBytes     = 256 * 1024
	toolCallTimeout      = 5 * time.Second
	maxConcurrentCalls   = 4
)

type Server struct {
	executable   string
	workspace    string
	inputSchema  any
	outputSchema any
	writeMu      sync.Mutex
	stateMu      sync.Mutex
	initialized  bool
	cancels      map[string]map[*callHandle]struct{}
	callSlots    chan struct{}
	callTimeout  time.Duration
	wg           sync.WaitGroup
	runWorker    func(context.Context, string, *os.File, supervisor.Request) inspector.Result
}

// callHandle identifies one in-flight call so a duplicate request ID cannot
// make an earlier call's cleanup delete a later call's cancellation entry.
type callHandle struct {
	cancel context.CancelFunc
}

func New(executable, workspace string) (*Server, error) {
	var inputSchema, outputSchema any
	if err := json.Unmarshal(schemas.FileInspectInput, &inputSchema); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(schemas.InspectionResult, &outputSchema); err != nil {
		return nil, err
	}
	return &Server{executable: executable, workspace: workspace, inputSchema: inputSchema, outputSchema: outputSchema, cancels: map[string]map[*callHandle]struct{}{}, callSlots: make(chan struct{}, maxConcurrentCalls), callTimeout: toolCallTimeout, runWorker: supervisor.Run}, nil
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type toolInput struct {
	Path string             `json:"path"`
	Mode inspector.Mode     `json:"mode,omitempty"`
	Hash inspector.HashMode `json:"hash,omitempty"`
}

type requestMetaEnvelope struct {
	Meta *modernRequestMeta `json:"_meta"`
}

type modernRequestMeta struct {
	ProtocolVersion    string          `json:"io.modelcontextprotocol/protocolVersion"`
	ClientCapabilities json.RawMessage `json:"io.modelcontextprotocol/clientCapabilities"`
	ClientInfo         json.RawMessage `json:"io.modelcontextprotocol/clientInfo"`
}

// errRequestTooLarge marks a request line that exceeded the transport budget.
// The remainder of the line is drained so the next request still parses.
var errRequestTooLarge = linereader.ErrTooLarge

func readRequestLine(reader *bufio.Reader, limit int) ([]byte, error) {
	return linereader.ReadRequestLine(reader, limit)
}

func (s *Server) Serve(ctx context.Context, input io.Reader, output io.Writer) error {
	reader := bufio.NewReaderSize(input, 64*1024)
	eof := false
	for !eof {
		line, err := readRequestLine(reader, maxRequestBytes)
		switch {
		case errors.Is(err, errRequestTooLarge):
			s.write(output, parseErrorResponse())
			continue
		case errors.Is(err, io.EOF):
			eof = true
			if len(line) == 0 {
				continue
			}
		case err != nil:
			s.wg.Wait()
			return err
		}
		var req request
		if err := json.Unmarshal(line, &req); err != nil {
			s.write(output, parseErrorResponse())
			continue
		}
		if req.JSONRPC != "2.0" || req.Method == "" {
			s.write(output, response{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32600, Message: "Invalid Request"}})
			continue
		}
		if req.Method == "notifications/cancelled" {
			s.cancel(req.Params)
			continue
		}
		if len(req.ID) == 0 || bytes.Equal(req.ID, []byte("null")) {
			continue
		}
		if req.Method == "tools/call" {
			callCtx, cancel := context.WithTimeout(ctx, s.callTimeout)
			key := string(req.ID)
			handle := &callHandle{cancel: cancel}
			s.stateMu.Lock()
			if s.cancels[key] == nil {
				s.cancels[key] = map[*callHandle]struct{}{}
			}
			s.cancels[key][handle] = struct{}{}
			s.stateMu.Unlock()
			s.wg.Add(1)
			go func(req request, callCtx context.Context, cancel context.CancelFunc, handle *callHandle, key string) {
				defer func() {
					cancel()
					s.stateMu.Lock()
					delete(s.cancels[key], handle)
					if len(s.cancels[key]) == 0 {
						delete(s.cancels, key)
					}
					s.stateMu.Unlock()
					s.wg.Done()
				}()
				s.handleToolCall(callCtx, output, req)
			}(req, callCtx, cancel, handle, key)
			continue
		}
		s.handleSync(output, req)
	}
	s.wg.Wait()
	return nil
}

func (s *Server) handleSync(output io.Writer, req request) {
	modern, protocolError := requestProtocol(req)
	if protocolError != nil {
		s.write(output, *protocolError)
		return
	}
	switch req.Method {
	case "initialize":
		if modern {
			s.write(output, response{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32601, Message: "Method not found"}})
			return
		}
		var params struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil || params.ProtocolVersion == "" {
			s.write(output, invalidParams(req.ID, "initialize requires protocolVersion"))
			return
		}
		protocol := negotiateProtocol(params.ProtocolVersion)
		s.stateMu.Lock()
		already := s.initialized
		if !already {
			s.initialized = true
		}
		s.stateMu.Unlock()
		if already {
			s.write(output, response{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32600, Message: "Server is already initialized"}})
			return
		}
		s.write(output, response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
			"protocolVersion": protocol,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": version.Server, "title": version.Product, "version": version.Version},
			"instructions":    "Use file_inspect once to identify and characterize one relative file inside the granted workspace. Results are read-only and bounded.",
		}})
	case "server/discover":
		if !modern {
			s.write(output, invalidParams(req.ID, "server/discover requires 2026-07-28 request metadata"))
			return
		}
		result := s.modernResult(map[string]any{
			"supportedVersions": []string{modernProtocol},
			"capabilities":      map[string]any{"tools": map[string]any{}},
			"instructions":      "Use file_inspect once to identify and characterize one relative file inside the granted workspace. Results are read-only and bounded.",
		}, true)
		s.write(output, response{JSONRPC: "2.0", ID: req.ID, Result: result})
	case "ping":
		result := map[string]any{}
		if modern {
			result = s.modernResult(result, false)
		}
		s.write(output, response{JSONRPC: "2.0", ID: req.ID, Result: result})
	case "tools/list":
		if !modern && !s.isInitialized() {
			s.write(output, notInitialized(req.ID))
			return
		}
		result := map[string]any{"tools": []any{s.toolDefinition()}}
		if modern {
			result = s.modernResult(result, true)
		}
		s.write(output, response{JSONRPC: "2.0", ID: req.ID, Result: result})
	default:
		s.write(output, response{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32601, Message: "Method not found"}})
	}
}

func (s *Server) handleToolCall(ctx context.Context, output io.Writer, req request) {
	timeout := s.callTimeout
	modern, protocolError := requestProtocol(req)
	if protocolError != nil {
		s.write(output, *protocolError)
		return
	}
	if !modern && !s.isInitialized() {
		s.write(output, notInitialized(req.ID))
		return
	}
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil || params.Name == "" {
		s.write(output, invalidParams(req.ID, "tools/call requires a tool name"))
		return
	}
	if params.Name != "file_inspect" {
		s.write(output, invalidParams(req.ID, "Unknown tool: "+params.Name))
		return
	}
	input, err := decodeToolInput(params.Arguments)
	if err != nil {
		s.writeToolResult(output, req.ID, inspector.PublicError(input.Path, input.Mode, timeout.Milliseconds(), "E_INVALID_INPUT", err.Error()), modern)
		return
	}
	select {
	case s.callSlots <- struct{}{}:
		defer func() { <-s.callSlots }()
	case <-ctx.Done():
		s.writeToolResult(output, req.ID, contextFailure(input, ctx.Err(), timeout), modern)
		return
	}
	file, err := authority.OpenRelativeContext(ctx, s.workspace, input.Path)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			s.writeToolResult(output, req.ID, contextFailure(input, err, timeout), modern)
			return
		}
		code, message := authority.Code(err)
		s.writeToolResult(output, req.ID, inspector.PublicError(input.Path, input.Mode, timeout.Milliseconds(), code, message), modern)
		return
	}
	defer file.Close()
	result := s.runWorker(ctx, s.executable, file, supervisor.Request{Name: input.Path, Mode: input.Mode, Hash: input.Hash, TimeoutMS: timeout.Milliseconds()})
	if err := ctx.Err(); err != nil {
		result = contextFailure(input, err, timeout)
	}
	s.writeToolResult(output, req.ID, result, modern)
}

func contextFailure(input toolInput, err error, timeout time.Duration) inspector.Result {
	if errors.Is(err, context.DeadlineExceeded) {
		return inspector.PublicError(input.Path, input.Mode, timeout.Milliseconds(), "E_TIMEOUT", "The complete inspection deadline was exceeded.")
	}
	return inspector.PublicError(input.Path, input.Mode, timeout.Milliseconds(), "E_CANCELLED", "The inspection was cancelled.")
}

func (s *Server) toolDefinition() map[string]any {
	return map[string]any{
		"name":         "file_inspect",
		"title":        "Inspect file",
		"description":  "Inspect any local file to identify its real format, typed structural properties, routing traits, conflicts, integrity, and probe evidence. Use one call before choosing a file-specific tool. The path must be relative to the granted workspace; inspection is read-only and archives are never extracted.",
		"inputSchema":  s.inputSchema,
		"outputSchema": s.outputSchema,
		"annotations": map[string]any{
			"title": "Inspect file", "readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false,
		},
	}
}

func decodeToolInput(raw json.RawMessage) (toolInput, error) {
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var input toolInput
	if err := decoder.Decode(&input); err != nil {
		return input, errors.New("arguments do not match the file_inspect schema")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return input, errors.New("arguments contain trailing data")
	}
	if input.Path == "" || len(input.Path) > 4096 {
		return input, errors.New("path must contain 1 to 4096 characters")
	}
	if input.Mode == "" {
		input.Mode = inspector.ModeStandard
	}
	if input.Mode != inspector.ModeQuick && input.Mode != inspector.ModeStandard && input.Mode != inspector.ModeDeep {
		return input, errors.New("mode must be quick, standard, or deep")
	}
	if input.Hash == "" {
		input.Hash = inspector.HashNone
	}
	if input.Hash != inspector.HashNone && input.Hash != inspector.HashSHA256 {
		return input, errors.New("hash must be none or sha256")
	}
	return input, nil
}

func (s *Server) writeToolResult(output io.Writer, id json.RawMessage, result inspector.Result, modern bool) {
	if err := schemas.ValidateInspectionResult(result); err != nil {
		result = inspector.PublicError(result.File.Name, result.Limits.Mode, result.Limits.TimeoutMS, "E_WORKER_PROTOCOL", "The inspection result did not satisfy the published output schema.")
	}
	toolResult := map[string]any{
		"content":           []any{map[string]any{"type": "text", "text": summarize(result)}},
		"structuredContent": result,
		"isError":           result.Status == "error",
	}
	if modern {
		toolResult = s.modernResult(toolResult, false)
	}
	// Marshal once: the same payload feeds the size check and the wire write.
	rpc := response{JSONRPC: "2.0", ID: id, Result: toolResult}
	payload, err := json.Marshal(rpc)
	if err != nil || len(payload)+1 > maxEnvelopeBytes {
		fallback := inspector.PublicError(result.File.Name, result.Limits.Mode, result.Limits.TimeoutMS, "E_RESPONSE_TOO_LARGE", "The complete MCP response exceeded its byte budget.")
		fallbackResult := map[string]any{
			"content": []any{map[string]any{"type": "text", "text": "error: E_RESPONSE_TOO_LARGE"}}, "structuredContent": fallback, "isError": true,
		}
		if modern {
			fallbackResult = s.modernResult(fallbackResult, false)
		}
		rpc = response{JSONRPC: "2.0", ID: id, Result: fallbackResult}
		if payload, err = json.Marshal(rpc); err != nil {
			return
		}
	}
	s.writeEncoded(output, payload)
}

func summarize(result inspector.Result) string {
	if result.Status == "error" && result.Error != nil {
		return "error: " + result.Error.Code + " — " + result.Error.Message
	}
	parts := []string{result.Status, result.Identity.Format, result.Identity.MediaType, fmt.Sprintf("%d bytes", result.File.SizeBytes)}
	if result.Image != nil {
		parts = append(parts, fmt.Sprintf("%dx%d", result.Image.Width, result.Image.Height))
	}
	if result.Media != nil && result.Media.DurationMS != nil {
		parts = append(parts, fmt.Sprintf("%d ms", *result.Media.DurationMS))
	}
	if result.Archive != nil {
		parts = append(parts, fmt.Sprintf("%d entries scanned", result.Archive.EntriesScanned))
	}
	return strings.Join(parts, " · ")
}

func (s *Server) cancel(raw json.RawMessage) {
	var params struct {
		RequestID json.RawMessage `json:"requestId"`
	}
	if json.Unmarshal(raw, &params) != nil {
		return
	}
	s.stateMu.Lock()
	handles := make([]*callHandle, 0, len(s.cancels[string(params.RequestID)]))
	for handle := range s.cancels[string(params.RequestID)] {
		handles = append(handles, handle)
	}
	s.stateMu.Unlock()
	for _, handle := range handles {
		handle.cancel()
	}
}

func (s *Server) isInitialized() bool {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	return s.initialized
}

func (s *Server) write(output io.Writer, value response) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return
	}
	s.writeEncoded(output, encoded)
}

func (s *Server) writeEncoded(output io.Writer, encoded []byte) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_, _ = output.Write(append(encoded, '\n'))
}

func invalidParams(id json.RawMessage, message string) response {
	return response{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: -32602, Message: message}}
}

func parseErrorResponse() response {
	return response{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{Code: -32700, Message: "Parse error"}}
}

func notInitialized(id json.RawMessage) response {
	return response{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: -32002, Message: "Server is not initialized"}}
}

func requestProtocol(req request) (bool, *response) {
	if len(req.Params) == 0 {
		return false, nil
	}
	var envelope requestMetaEnvelope
	if err := json.Unmarshal(req.Params, &envelope); err != nil {
		value := invalidParams(req.ID, "request params must be a JSON object")
		return false, &value
	}
	if envelope.Meta == nil || envelope.Meta.ProtocolVersion == "" {
		return false, nil
	}
	if envelope.Meta.ProtocolVersion != modernProtocol {
		return false, &response{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{
			Code: -32022, Message: "Unsupported protocol version",
			Data: map[string]any{"supported": []string{modernProtocol}, "requested": envelope.Meta.ProtocolVersion},
		}}
	}
	capabilities := bytes.TrimSpace(envelope.Meta.ClientCapabilities)
	if len(capabilities) == 0 || bytes.Equal(capabilities, []byte("null")) || capabilities[0] != '{' {
		value := invalidParams(req.ID, "2026-07-28 requests require clientCapabilities in _meta")
		return false, &value
	}
	clientInfo := bytes.TrimSpace(envelope.Meta.ClientInfo)
	if len(clientInfo) > 0 && !validImplementation(clientInfo) {
		value := invalidParams(req.ID, "clientInfo in _meta must contain string name and version fields")
		return false, &value
	}
	return true, nil
}

func validImplementation(raw json.RawMessage) bool {
	var value map[string]json.RawMessage
	if err := json.Unmarshal(raw, &value); err != nil || value == nil {
		return false
	}
	nameValue, hasName := value["name"]
	versionValue, hasVersion := value["version"]
	if !hasName || !hasVersion {
		return false
	}
	var name, implementationVersion string
	return json.Unmarshal(nameValue, &name) == nil && json.Unmarshal(versionValue, &implementationVersion) == nil
}

func (s *Server) modernResult(result map[string]any, cacheable bool) map[string]any {
	result["resultType"] = "complete"
	result["_meta"] = map[string]any{"io.modelcontextprotocol/serverInfo": map[string]any{
		"name": version.Server, "title": version.Product, "version": version.Version,
	}}
	if cacheable {
		result["ttlMs"] = 3600000
		result["cacheScope"] = "private"
	}
	return result
}

func negotiateProtocol(requested string) string {
	switch requested {
	case "2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05":
		return requested
	default:
		return latestLegacyProtocol
	}
}
