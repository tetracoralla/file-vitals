package supervisor

import (
	"context"
	"encoding/json"
	"errors"
	"os"

	"github.com/tetracoralla/file-vitals/internal/inspector"
	"github.com/tetracoralla/file-vitals/internal/procmon"
	"github.com/tetracoralla/file-vitals/schemas"
)

type CollectionItem struct {
	Name            string               `json:"name"`
	DescriptorIndex int                  `json:"descriptor_index"`
	Error           *inspector.ErrorInfo `json:"error,omitempty"`
}

type BatchRequest struct {
	Items     []CollectionItem   `json:"items"`
	Mode      inspector.Mode     `json:"mode"`
	Hash      inspector.HashMode `json:"hash"`
	TimeoutMS int64              `json:"timeout_ms"`
}

type InventoryRequest struct {
	Root               string           `json:"root"`
	Items              []CollectionItem `json:"items"`
	DirectoriesScanned int              `json:"directories_scanned"`
	SymlinksSkipped    int              `json:"symlinks_skipped"`
	SpecialSkipped     int              `json:"special_entries_skipped"`
	Truncated          bool             `json:"truncated"`
	MaxDepth           int              `json:"max_depth"`
	TimeoutMS          int64            `json:"timeout_ms"`
}

func RunBatch(ctx context.Context, executable string, files []*os.File, request BatchRequest) inspector.BatchResult {
	payload, err := json.Marshal(request)
	if err != nil {
		return inspector.PublicBatchError(request.TimeoutMS, "E_INTERNAL", "The batch worker request could not be encoded.")
	}
	if len(payload) > inspector.MaxCollectionRequestBytes {
		return inspector.PublicBatchError(request.TimeoutMS, "E_INVALID_INPUT", "The batch worker request exceeds its bounded transport budget.")
	}
	process, err := runCollectionProcess(ctx, executable, "__worker-batch", files, payload)
	if err != nil {
		return batchProcessError(ctx, err, request.TimeoutMS)
	}
	if err := schemas.ValidateBatchResultJSON(process.Stdout); err != nil {
		return inspector.PublicBatchError(request.TimeoutMS, "E_WORKER_PROTOCOL", "The batch worker returned a result outside the published schema.")
	}
	var result inspector.BatchResult
	if json.Unmarshal(process.Stdout, &result) != nil {
		return inspector.PublicBatchError(request.TimeoutMS, "E_WORKER_PROTOCOL", "The batch worker returned an invalid result.")
	}
	return result
}

func RunInventory(ctx context.Context, executable string, files []*os.File, request InventoryRequest) inspector.InventoryResult {
	payload, err := json.Marshal(request)
	if err != nil {
		return inspector.PublicInventoryError(request.Root, request.MaxDepth, request.TimeoutMS, "E_INTERNAL", "The inventory worker request could not be encoded.")
	}
	if len(payload) > inspector.MaxCollectionRequestBytes {
		return inspector.PublicInventoryError(request.Root, request.MaxDepth, request.TimeoutMS, "E_INVALID_INPUT", "The inventory worker request exceeds its bounded transport budget.")
	}
	process, err := runCollectionProcess(ctx, executable, "__worker-inventory", files, payload)
	if err != nil {
		return inventoryProcessError(ctx, err, request)
	}
	if err := schemas.ValidateInventoryResultJSON(process.Stdout); err != nil {
		return inspector.PublicInventoryError(request.Root, request.MaxDepth, request.TimeoutMS, "E_WORKER_PROTOCOL", "The inventory worker returned a result outside the published schema.")
	}
	var result inspector.InventoryResult
	if json.Unmarshal(process.Stdout, &result) != nil {
		return inspector.PublicInventoryError(request.Root, request.MaxDepth, request.TimeoutMS, "E_WORKER_PROTOCOL", "The inventory worker returned an invalid result.")
	}
	return result
}

func runCollectionProcess(ctx context.Context, executable, command string, files []*os.File, payload []byte) (procmon.Result, error) {
	return procmon.Run(ctx, procmon.Spec{
		Name: executable, Args: []string{command}, Stdin: payload, Files: files,
		StdoutBytes: inspector.MaxCollectionBytes,
		StderrBytes: inspector.MaxProbeStderrBytes,
		MemoryBytes: inspector.MaxMemoryBytes,
	})
}

func batchProcessError(ctx context.Context, err error, timeoutMS int64) inspector.BatchResult {
	switch {
	case errors.Is(ctx.Err(), context.Canceled):
		return inspector.PublicBatchError(timeoutMS, "E_CANCELLED", "The batch inspection was cancelled.")
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return inspector.PublicBatchError(timeoutMS, "E_TIMEOUT", "The complete batch inspection deadline was exceeded.")
	case errors.Is(err, procmon.ErrMemory):
		return inspector.PublicBatchError(timeoutMS, "E_MEMORY_LIMIT", "The batch worker exceeded its memory budget.")
	case errors.Is(err, procmon.ErrOutput):
		return inspector.PublicBatchError(timeoutMS, "E_RESPONSE_TOO_LARGE", "The batch worker exceeded its response budget.")
	default:
		return inspector.PublicBatchError(timeoutMS, "E_WORKER_FAILED", "The isolated batch worker failed.")
	}
}

func inventoryProcessError(ctx context.Context, err error, request InventoryRequest) inspector.InventoryResult {
	switch {
	case errors.Is(ctx.Err(), context.Canceled):
		return inspector.PublicInventoryError(request.Root, request.MaxDepth, request.TimeoutMS, "E_CANCELLED", "The workspace inventory was cancelled.")
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return inspector.PublicInventoryError(request.Root, request.MaxDepth, request.TimeoutMS, "E_TIMEOUT", "The complete workspace inventory deadline was exceeded.")
	case errors.Is(err, procmon.ErrMemory):
		return inspector.PublicInventoryError(request.Root, request.MaxDepth, request.TimeoutMS, "E_MEMORY_LIMIT", "The inventory worker exceeded its memory budget.")
	case errors.Is(err, procmon.ErrOutput):
		return inspector.PublicInventoryError(request.Root, request.MaxDepth, request.TimeoutMS, "E_RESPONSE_TOO_LARGE", "The inventory worker exceeded its response budget.")
	default:
		return inspector.PublicInventoryError(request.Root, request.MaxDepth, request.TimeoutMS, "E_WORKER_FAILED", "The isolated inventory worker failed.")
	}
}
