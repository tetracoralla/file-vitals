package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/tetracoralla/file-vitals/capabilities"
	"github.com/tetracoralla/file-vitals/internal/authority"
	"github.com/tetracoralla/file-vitals/internal/inspector"
	"github.com/tetracoralla/file-vitals/internal/linereader"
	"github.com/tetracoralla/file-vitals/internal/supervisor"
	"github.com/tetracoralla/file-vitals/internal/worker"
)

const (
	maxRequestLineBytes = 64 * 1024
	// The capability boundary grants one inspection a larger budget than the
	// MCP tool call (10s vs 5s): a Procedure may schedule slower storage and
	// cannot resubmit interactively the way an Agent client can.
	inspectionTimeout = 10 * time.Second
	// Bounded concurrency mirrors the MCP server's admission policy so one
	// slow storage-backed inspection cannot block every later request.
	maxConcurrentInspections = 4
	// This bounds executing plus queued requests. A worker semaphore alone does
	// not bound the number of goroutines created by a fast JSONL producer.
	maxAdmittedInspections = 16
)

var runInspection = supervisor.Run

type requestEnvelope struct {
	ID          string          `json:"id"`
	OperationID string          `json:"operationId"`
	Input       json.RawMessage `json:"input"`
}

type inspectInput struct {
	Path string             `json:"path"`
	Mode inspector.Mode     `json:"mode,omitempty"`
	Hash inspector.HashMode `json:"hash,omitempty"`
}

type errorEnvelope struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type responseEnvelope struct {
	ID     string         `json:"id"`
	OK     bool           `json:"ok"`
	Result any            `json:"result,omitempty"`
	Error  *errorEnvelope `json:"error,omitempty"`
}

type canonicalResult struct {
	Status      string                  `json:"status"`
	File        inspector.FileInfo      `json:"file"`
	Identity    inspector.Identity      `json:"identity"`
	Traits      []string                `json:"traits"`
	Integrity   inspector.Integrity     `json:"integrity"`
	Structured  *inspector.Structured   `json:"structured,omitempty"`
	Image       *inspector.ImageInfo    `json:"image,omitempty"`
	Archive     *canonicalArchive       `json:"archive,omitempty"`
	Diagnostics []inspector.Diagnostic  `json:"diagnostics"`
	Provenance  []inspector.Provenance  `json:"provenance"`
	Limits      inspector.AppliedLimits `json:"limits"`
}

// The portable file.inspect@0.1.0 contract is intentionally narrower than the
// product result. Project it explicitly so new product-only package blockers
// cannot leak through additional properties and silently mutate the Capability.
type canonicalArchive struct {
	Format                   string                  `json:"format"`
	EntryCount               *int                    `json:"entry_count,omitempty"`
	EntriesScanned           int                     `json:"entries_scanned"`
	TotalUncompressedBytes   *int64                  `json:"total_uncompressed_bytes,omitempty"`
	UncompressedBytesScanned int64                   `json:"uncompressed_bytes_scanned"`
	Encrypted                bool                    `json:"encrypted"`
	Entries                  []canonicalArchiveEntry `json:"entries,omitempty"`
	EntriesTruncated         bool                    `json:"entries_truncated"`
	ScanTruncated            bool                    `json:"scan_truncated"`
}

type canonicalArchiveEntry struct {
	Name            string `json:"name"`
	SizeBytes       int64  `json:"size_bytes"`
	CompressedBytes *int64 `json:"compressed_bytes,omitempty"`
	Directory       bool   `json:"directory"`
	Encrypted       bool   `json:"encrypted"`
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "__worker-inspect" {
		os.Exit(worker.Run(os.Stdout))
	}
	if err := serve(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func serve(input io.Reader, output io.Writer) error {
	reader := bufio.NewReaderSize(input, 64*1024)
	encoder := json.NewEncoder(output)
	var writeMu sync.Mutex
	var wg sync.WaitGroup
	slots := make(chan struct{}, maxConcurrentInspections)
	admission := make(chan struct{}, maxAdmittedInspections)
	eof := false
	for !eof {
		line, err := linereader.ReadRequestLine(reader, maxRequestLineBytes)
		switch {
		case errors.Is(err, linereader.ErrTooLarge):
			// The remainder of the oversized line is drained; the session
			// continues with the next request.
			writeResponse(&writeMu, encoder, responseEnvelope{Error: &errorEnvelope{Code: "INVALID_INPUT", Message: "Capability request line exceeds the transport budget."}})
			continue
		case errors.Is(err, io.EOF):
			eof = true
			if len(line) == 0 {
				continue
			}
		case err != nil:
			wg.Wait()
			return err
		}
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		select {
		case admission <- struct{}{}:
		default:
			var identity struct {
				ID string `json:"id"`
			}
			_ = json.Unmarshal(line, &identity)
			if len(identity.ID) > 128 {
				identity.ID = ""
			}
			writeResponse(&writeMu, encoder, failure(identity.ID, "LIMIT_EXCEEDED", "Provider request capacity is full."))
			continue
		}
		wg.Add(1)
		go func(line []byte) {
			defer func() {
				<-admission
				wg.Done()
			}()
			// Responses complete in request-completion order, not arrival
			// order; clients correlate by the required envelope id.
			writeResponse(&writeMu, encoder, handleRequest(slots, line))
		}(line)
	}
	// An orderly drain lets in-flight inspections publish their results after
	// the client closes the request stream.
	wg.Wait()
	return nil
}

func writeResponse(writeMu *sync.Mutex, encoder *json.Encoder, response responseEnvelope) {
	writeMu.Lock()
	defer writeMu.Unlock()
	_ = encoder.Encode(response)
}

func handleRequest(slots chan struct{}, line []byte) responseEnvelope {
	return handleRequestWithTimeout(slots, line, inspectionTimeout)
}

func handleRequestWithTimeout(slots chan struct{}, line []byte, timeout time.Duration) responseEnvelope {
	var request requestEnvelope
	if err := decodeStrict(line, &request); err != nil {
		return failure(request.ID, "INVALID_INPUT", "Invalid Capability request envelope.")
	}
	if request.ID == "" || len(request.ID) > 128 || request.OperationID != "inspect" {
		return failure(request.ID, "INVALID_INPUT", "Invalid Capability request identity or operation.")
	}
	var input inspectInput
	if err := decodeStrict(request.Input, &input); err != nil || capabilities.ValidateInput(input) != nil {
		return failure(request.ID, "INVALID_INPUT", "Input does not satisfy file.inspect@0.1.0.")
	}
	if input.Mode == "" {
		input.Mode = inspector.ModeStandard
	}
	if input.Hash == "" {
		input.Hash = inspector.HashNone
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	select {
	case slots <- struct{}{}:
		defer func() { <-slots }()
	case <-ctx.Done():
		// Queue admission shares the complete call deadline, mirroring the
		// MCP server: a queued request that cannot start inside its own
		// budget reports LIMIT_EXCEEDED instead of blocking silently.
		return failure(request.ID, "LIMIT_EXCEEDED", "The inspection deadline was exceeded while waiting for an admission slot.")
	}
	root := os.Getenv("OPENADAM_CAPABILITY_WORKSPACE_ROOT")
	if root == "" {
		root = os.Getenv("OPENADAM_PROVIDER_ROOT")
	}
	file, err := authority.OpenRelativeContext(ctx, root, input.Path)
	if err != nil {
		code, message := authority.Code(err)
		return failure(request.ID, canonicalErrorCode(code), message)
	}
	defer file.Close()
	executable, err := os.Executable()
	if err != nil {
		return failure(request.ID, "INSPECTION_FAILED", "The provider executable is unavailable.")
	}
	result := runInspection(ctx, executable, file, supervisor.Request{
		Name: input.Path, Mode: input.Mode, Hash: input.Hash, TimeoutMS: timeout.Milliseconds(),
	})
	if result.Status == "error" {
		if result.Error == nil {
			return failure(request.ID, "INSPECTION_FAILED", "The provider returned an incomplete error.")
		}
		return failure(request.ID, canonicalErrorCode(result.Error.Code), result.Error.Message)
	}
	canonical := canonicalResult{
		Status: result.Status, File: result.File, Identity: result.Identity,
		Traits: result.Traits, Integrity: result.Integrity, Structured: result.Structured,
		Image: result.Image, Archive: projectArchive(result.Archive), Diagnostics: result.Diagnostics,
		Provenance: result.Provenance, Limits: result.Limits,
	}
	if err := capabilities.ValidateOutput(canonical); err != nil {
		return failure(request.ID, "INSPECTION_FAILED", "The provider result violates the portable output contract.")
	}
	return responseEnvelope{ID: request.ID, OK: true, Result: canonical}
}

func projectArchive(source *inspector.ArchiveInfo) *canonicalArchive {
	if source == nil {
		return nil
	}
	projected := &canonicalArchive{
		Format: source.Format, EntryCount: source.EntryCount, EntriesScanned: source.EntriesScanned,
		TotalUncompressedBytes: source.TotalUncompressedBytes, UncompressedBytesScanned: source.UncompressedBytesScanned,
		Encrypted: source.Encrypted, Entries: []canonicalArchiveEntry{}, EntriesTruncated: source.EntriesTruncated, ScanTruncated: source.ScanTruncated,
	}
	for _, entry := range source.Entries {
		projected.Entries = append(projected.Entries, canonicalArchiveEntry{
			Name: entry.Name, SizeBytes: entry.SizeBytes, CompressedBytes: entry.CompressedBytes,
			Directory: entry.Directory, Encrypted: entry.Encrypted,
		})
	}
	return projected
}

func decodeStrict(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON value")
	}
	return nil
}

func failure(id, code, message string) responseEnvelope {
	return responseEnvelope{ID: id, Error: &errorEnvelope{Code: code, Message: message}}
}

func canonicalErrorCode(code string) string {
	switch code {
	case "E_INVALID_INPUT", "E_INVALID_OPTIONS", "E_INVALID_PATH":
		return "INVALID_INPUT"
	case "E_FILE_NOT_FOUND":
		return "FILE_NOT_FOUND"
	case "E_PATH_ABSOLUTE", "E_PATH_CHANGED", "E_PATH_COMPONENT", "E_PATH_SYMLINK", "E_PATH_TRAVERSAL", "E_PATH_URI", "E_NOT_REGULAR_FILE", "E_WORKSPACE_INVALID", "E_WORKSPACE_REQUIRED":
		return "PATH_FORBIDDEN"
	case "E_MEMORY_LIMIT", "E_RESPONSE_TOO_LARGE", "E_TIMEOUT":
		return "LIMIT_EXCEEDED"
	default:
		return "INSPECTION_FAILED"
	}
}
