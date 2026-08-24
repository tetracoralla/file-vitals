package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tetracoralla/file-vitals/internal/authority"
	"github.com/tetracoralla/file-vitals/internal/inspector"
	"github.com/tetracoralla/file-vitals/internal/supervisor"
	"github.com/tetracoralla/file-vitals/schemas"
)

type batchToolInput struct {
	Paths []string           `json:"paths"`
	Mode  inspector.Mode     `json:"mode,omitempty"`
	Hash  inspector.HashMode `json:"hash,omitempty"`
}

type inventoryToolInput struct {
	Path     string `json:"path"`
	MaxDepth int    `json:"max_depth"`
}

func (s *Server) batchToolDefinition() map[string]any {
	return map[string]any{
		"name": "file_inspect_batch", "title": "Inspect file batch",
		"description": "Inspect 1 to 16 explicit relative files in one bounded call. Preserves input order and returns a schema-valid result for every path, including per-item authority or file errors. Use this instead of repeated file_inspect calls when the paths are already known.",
		"inputSchema": s.batchInputSchema, "outputSchema": s.batchOutputSchema,
		"annotations": map[string]any{"title": "Inspect file batch", "readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false},
	}
}

func (s *Server) inventoryToolDefinition() map[string]any {
	return map[string]any{
		"name": "workspace_inventory", "title": "Inventory workspace files",
		"description": "Produce a deterministic, bounded overview of regular files under one relative workspace directory: identities, routing traits, blockers, aggregate formats, skipped symlinks, and truncation. Scans at most 32 files, 256 directories, and 4096 directory entries and never follows links. Use this when paths are not yet known; use file_inspect for a selected file afterward only if deeper facts are needed.",
		"inputSchema": s.inventoryInputSchema, "outputSchema": s.inventoryOutputSchema,
		"annotations": map[string]any{"title": "Inventory workspace files", "readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false},
	}
}

func (s *Server) handleBatchCall(ctx context.Context, output io.Writer, id json.RawMessage, raw json.RawMessage, modern bool) {
	input, err := decodeBatchToolInput(raw)
	if err != nil {
		s.writeBatchToolResult(output, id, inspector.PublicBatchError(s.callTimeout.Milliseconds(), "E_INVALID_INPUT", err.Error()), modern)
		return
	}
	if !s.acquireCallSlot(ctx) {
		s.writeBatchToolResult(output, id, batchContextFailure(ctx.Err(), s.callTimeout), modern)
		return
	}
	defer func() { <-s.callSlots }()

	files := []*os.File{}
	defer func() { closeMCPFiles(files) }()
	items := make([]supervisor.CollectionItem, 0, len(input.Paths))
	for _, path := range input.Paths {
		file, openErr := authority.OpenRelativeContext(ctx, s.workspace, path)
		if openErr != nil {
			if errors.Is(openErr, context.Canceled) || errors.Is(openErr, context.DeadlineExceeded) {
				s.writeBatchToolResult(output, id, batchContextFailure(openErr, s.callTimeout), modern)
				return
			}
			code, message := authority.Code(openErr)
			items = append(items, supervisor.CollectionItem{Name: path, Error: &inspector.ErrorInfo{Code: code, Message: message}})
			continue
		}
		items = append(items, supervisor.CollectionItem{Name: path, DescriptorIndex: len(files)})
		files = append(files, file)
	}
	result := s.runBatchWorker(ctx, s.executable, files, supervisor.BatchRequest{Items: items, Mode: input.Mode, Hash: input.Hash, TimeoutMS: s.callTimeout.Milliseconds()})
	if err := ctx.Err(); err != nil {
		result = batchContextFailure(err, s.callTimeout)
	}
	s.writeBatchToolResult(output, id, result, modern)
}

func (s *Server) handleInventoryCall(ctx context.Context, output io.Writer, id json.RawMessage, raw json.RawMessage, modern bool) {
	input, err := decodeInventoryToolInput(raw)
	if err != nil {
		s.writeInventoryToolResult(output, id, inspector.PublicInventoryError(input.Path, input.MaxDepth, s.callTimeout.Milliseconds(), "E_INVALID_INPUT", err.Error()), modern)
		return
	}
	if !s.acquireCallSlot(ctx) {
		s.writeInventoryToolResult(output, id, inventoryContextFailure(input, ctx.Err(), s.callTimeout.Milliseconds()), modern)
		return
	}
	defer func() { <-s.callSlots }()

	collection, collectErr := authority.CollectRegularFilesContext(ctx, s.workspace, input.Path, input.MaxDepth, inspector.MaxInventoryFiles, inspector.MaxInventoryDirs, inspector.MaxInventoryEntries)
	if collectErr != nil {
		if errors.Is(collectErr, context.Canceled) || errors.Is(collectErr, context.DeadlineExceeded) {
			s.writeInventoryToolResult(output, id, inventoryContextFailure(input, collectErr, s.callTimeout.Milliseconds()), modern)
			return
		}
		code, message := authority.Code(collectErr)
		s.writeInventoryToolResult(output, id, inspector.PublicInventoryError(input.Path, input.MaxDepth, s.callTimeout.Milliseconds(), code, message), modern)
		return
	}
	defer collection.Close()
	files := make([]*os.File, 0, len(collection.Files))
	items := make([]supervisor.CollectionItem, 0, len(collection.Files))
	for _, item := range collection.Files {
		items = append(items, supervisor.CollectionItem{Name: item.Path, DescriptorIndex: len(files)})
		files = append(files, item.File)
	}
	request := supervisor.InventoryRequest{
		Root: input.Path, Items: items, EntriesScanned: collection.EntriesScanned, DirectoriesScanned: collection.DirectoriesScanned,
		SymlinksSkipped: collection.SymlinksSkipped, SpecialSkipped: collection.SpecialSkipped,
		Truncated: collection.Truncated, MaxDepth: input.MaxDepth, TimeoutMS: s.callTimeout.Milliseconds(),
	}
	result := s.runInventoryWorker(ctx, s.executable, files, request)
	if err := ctx.Err(); err != nil {
		result = inventoryContextFailure(input, err, s.callTimeout.Milliseconds())
	}
	s.writeInventoryToolResult(output, id, result, modern)
}

func decodeBatchToolInput(raw json.RawMessage) (batchToolInput, error) {
	var input batchToolInput
	if err := decodeStrictArguments(raw, &input); err != nil {
		return input, errors.New("arguments do not match the file_inspect_batch schema")
	}
	if len(input.Paths) == 0 || len(input.Paths) > inspector.MaxBatchItems {
		return input, errors.New("paths must contain 1 to 16 entries")
	}
	seen := map[string]struct{}{}
	for _, path := range input.Paths {
		if path == "" || len(path) > 4096 {
			return input, errors.New("each path must contain 1 to 4096 characters")
		}
		key := filepath.Clean(path)
		if _, exists := seen[key]; exists {
			return input, errors.New("paths must not contain duplicates")
		}
		seen[key] = struct{}{}
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

func decodeInventoryToolInput(raw json.RawMessage) (inventoryToolInput, error) {
	wire := struct {
		Path     string `json:"path"`
		MaxDepth *int   `json:"max_depth"`
	}{}
	if err := decodeStrictArguments(raw, &wire); err != nil {
		return inventoryToolInput{}, errors.New("arguments do not match the workspace_inventory schema")
	}
	input := inventoryToolInput{Path: wire.Path, MaxDepth: 4}
	if input.Path == "" {
		input.Path = "."
	}
	if wire.MaxDepth != nil {
		input.MaxDepth = *wire.MaxDepth
	}
	if len(input.Path) > 4096 {
		return input, errors.New("path must contain 1 to 4096 characters")
	}
	if input.MaxDepth < 0 || input.MaxDepth > inspector.MaxInventoryDepth {
		return input, errors.New("max_depth must be between 0 and 8")
	}
	return input, nil
}

func decodeStrictArguments(raw json.RawMessage, target any) error {
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("arguments contain trailing data")
	}
	return nil
}

func batchContextFailure(err error, timeout time.Duration) inspector.BatchResult {
	if errors.Is(err, context.DeadlineExceeded) {
		return inspector.PublicBatchError(timeout.Milliseconds(), "E_TIMEOUT", "The complete batch inspection deadline was exceeded.")
	}
	return inspector.PublicBatchError(timeout.Milliseconds(), "E_CANCELLED", "The batch inspection was cancelled.")
}

func inventoryContextFailure(input inventoryToolInput, err error, timeoutMS int64) inspector.InventoryResult {
	if errors.Is(err, context.DeadlineExceeded) {
		return inspector.PublicInventoryError(input.Path, input.MaxDepth, timeoutMS, "E_TIMEOUT", "The complete workspace inventory deadline was exceeded.")
	}
	return inspector.PublicInventoryError(input.Path, input.MaxDepth, timeoutMS, "E_CANCELLED", "The workspace inventory was cancelled.")
}

func (s *Server) writeBatchToolResult(output io.Writer, id json.RawMessage, result inspector.BatchResult, modern bool) {
	if err := schemas.ValidateBatchResult(result); err != nil {
		result = inspector.PublicBatchError(result.Limits.TimeoutMS, "E_WORKER_PROTOCOL", "The batch result did not satisfy the published output schema.")
	}
	s.writeCollectionResult(output, id, result, summarizeBatch(result), result.Status == "error", modern,
		func() any {
			return inspector.PublicBatchError(result.Limits.TimeoutMS, "E_RESPONSE_TOO_LARGE", "The complete MCP response exceeded its byte budget.")
		})
}

func (s *Server) writeInventoryToolResult(output io.Writer, id json.RawMessage, result inspector.InventoryResult, modern bool) {
	if err := schemas.ValidateInventoryResult(result); err != nil {
		result = inspector.PublicInventoryError(result.Root, result.Limits.MaxDepth, result.Limits.TimeoutMS, "E_WORKER_PROTOCOL", "The inventory result did not satisfy the published output schema.")
	}
	s.writeCollectionResult(output, id, result, summarizeInventory(result), result.Status == "error", modern,
		func() any {
			return inspector.PublicInventoryError(result.Root, result.Limits.MaxDepth, result.Limits.TimeoutMS, "E_RESPONSE_TOO_LARGE", "The complete MCP response exceeded its byte budget.")
		})
}

func (s *Server) writeCollectionResult(output io.Writer, id json.RawMessage, structured any, summary string, isError, modern bool, fallback func() any) {
	toolResult := map[string]any{"content": []any{map[string]any{"type": "text", "text": summary}}, "structuredContent": structured, "isError": isError}
	if modern {
		toolResult = s.modernResult(toolResult, false)
	}
	rpc := response{JSONRPC: "2.0", ID: id, Result: toolResult}
	payload, err := json.Marshal(rpc)
	if err != nil || len(payload)+1 > maxEnvelopeBytes {
		fallbackResult := fallback()
		toolResult = map[string]any{"content": []any{map[string]any{"type": "text", "text": "error: E_RESPONSE_TOO_LARGE"}}, "structuredContent": fallbackResult, "isError": true}
		if modern {
			toolResult = s.modernResult(toolResult, false)
		}
		payload, err = json.Marshal(response{JSONRPC: "2.0", ID: id, Result: toolResult})
		if err != nil {
			return
		}
	}
	s.writeEncoded(output, payload)
}

func summarizeBatch(result inspector.BatchResult) string {
	if result.Status == "error" && result.Error != nil {
		return "error: " + result.Error.Code + " — " + result.Error.Message
	}
	errorsFound := 0
	for _, item := range result.Items {
		if item.Result.Status == "error" {
			errorsFound++
		}
	}
	return fmt.Sprintf("%s · %d files · %d item errors", result.Status, len(result.Items), errorsFound)
}

func summarizeInventory(result inspector.InventoryResult) string {
	if result.Status == "error" && result.Error != nil {
		return "error: " + result.Error.Code + " — " + result.Error.Message
	}
	return strings.Join([]string{result.Status, fmt.Sprintf("%d files", result.FilesScanned), fmt.Sprintf("%d formats", len(result.Formats)), fmt.Sprintf("%d bytes", result.TotalSizeBytes)}, " · ")
}

func closeMCPFiles(files []*os.File) {
	for _, file := range files {
		_ = file.Close()
	}
}
