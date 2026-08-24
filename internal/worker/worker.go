// Package worker runs one isolated inspection inside a spawned worker process.
// Both the finspect CLI/MCP entrypoint and the capability adapter dispatch into
// this single implementation so the two boundaries cannot drift.
package worker

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"runtime/debug"
	"time"

	"github.com/tetracoralla/file-vitals/internal/inspector"
	"github.com/tetracoralla/file-vitals/internal/supervisor"
)

// Run reads one supervisor.Request from stdin, inspects the file passed as
// descriptor 3, and writes the result to stdout. The exit code is 0 for any
// published result and 70 for an unusable worker protocol state.
func Run(stdout io.Writer) int {
	debug.SetMemoryLimit(inspector.GoMemoryLimitBytes)
	decoder := json.NewDecoder(io.LimitReader(os.Stdin, 64*1024))
	decoder.DisallowUnknownFields()
	var request supervisor.Request
	if err := decoder.Decode(&request); err != nil {
		return 70
	}
	if request.TimeoutMS < 100 || request.TimeoutMS > 60_000 {
		request.TimeoutMS = 10_000
	}
	file := os.NewFile(uintptr(3), "inspected-file")
	if file == nil {
		return 70
	}
	defer file.Close()
	source, err := inspector.SourceFromFile(file, request.Name)
	if err != nil {
		result := inspector.PublicError(request.Name, request.Mode, request.TimeoutMS, "E_FILE_STAT", "The open file descriptor could not be inspected.")
		_ = json.NewEncoder(stdout).Encode(result)
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(request.TimeoutMS)*time.Millisecond)
	defer cancel()
	result := InspectWithRecover(ctx, source, request)
	if err := json.NewEncoder(stdout).Encode(result); err != nil {
		return 70
	}
	return 0
}

// InspectWithRecover keeps a parser panic from killing the worker silently: a
// crashed process surfaces as a generic E_WORKER_FAILED at the supervisor, but
// a recovered panic can still return the published result contract.
func InspectWithRecover(ctx context.Context, source inspector.Source, request supervisor.Request) (result inspector.Result) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = inspector.PublicError(request.Name, request.Mode, request.TimeoutMS, "E_INTERNAL", "The inspection worker encountered an internal failure.")
		}
	}()
	return inspector.New().Inspect(ctx, source, inspector.Options{Mode: request.Mode, Hash: request.Hash, Timeout: time.Duration(request.TimeoutMS) * time.Millisecond})
}
