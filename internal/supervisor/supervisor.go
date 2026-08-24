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

type Request struct {
	Name           string             `json:"name"`
	Mode           inspector.Mode     `json:"mode"`
	Hash           inspector.HashMode `json:"hash"`
	ExpectedSHA256 string             `json:"expected_sha256,omitempty"`
	TimeoutMS      int64              `json:"timeout_ms"`
}

func Run(ctx context.Context, executable string, file *os.File, request Request) inspector.Result {
	payload, err := json.Marshal(request)
	if err != nil {
		return inspector.PublicError(request.Name, request.Mode, request.TimeoutMS, "E_INTERNAL", "The worker request could not be encoded.")
	}
	process, err := procmon.Run(ctx, procmon.Spec{
		Name: executable, Args: []string{"__worker-inspect"}, Stdin: payload, File: file,
		StdoutBytes: inspector.MaxResponseBytes,
		StderrBytes: inspector.MaxProbeStderrBytes,
		MemoryBytes: inspector.MaxMemoryBytes,
	})
	if err != nil {
		switch {
		case errors.Is(ctx.Err(), context.Canceled):
			return inspector.PublicError(request.Name, request.Mode, request.TimeoutMS, "E_CANCELLED", "The inspection was cancelled.")
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			return inspector.PublicError(request.Name, request.Mode, request.TimeoutMS, "E_TIMEOUT", "The inspection deadline was exceeded.")
		case errors.Is(err, procmon.ErrMemory):
			return inspector.PublicError(request.Name, request.Mode, request.TimeoutMS, "E_MEMORY_LIMIT", "The inspection worker exceeded its memory budget.")
		case errors.Is(err, procmon.ErrOutput):
			return inspector.PublicError(request.Name, request.Mode, request.TimeoutMS, "E_RESPONSE_TOO_LARGE", "The inspection worker exceeded its response budget.")
		default:
			return inspector.PublicError(request.Name, request.Mode, request.TimeoutMS, "E_WORKER_FAILED", "The isolated inspection worker failed.")
		}
	}
	// Validate the raw worker bytes before decoding: the boundary check runs
	// against exactly what the worker emitted, with no intermediate marshal.
	var result inspector.Result
	if err := schemas.ValidateInspectionResultJSON(process.Stdout); err != nil {
		return inspector.PublicError(request.Name, request.Mode, request.TimeoutMS, "E_WORKER_PROTOCOL", "The inspection worker returned a result outside the published schema.")
	}
	if err := json.Unmarshal(process.Stdout, &result); err != nil {
		return inspector.PublicError(request.Name, request.Mode, request.TimeoutMS, "E_WORKER_PROTOCOL", "The inspection worker returned an invalid result.")
	}
	return result
}
