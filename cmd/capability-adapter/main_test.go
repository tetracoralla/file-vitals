package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tetracoralla/file-vitals/internal/inspector"
	"github.com/tetracoralla/file-vitals/internal/supervisor"
	"github.com/tetracoralla/file-vitals/internal/worker"
)

// TestMain routes spawned copies of the test binary into the real worker mode,
// so real-worker tests exercise the genuine supervisor path end to end.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "__worker-inspect" {
		os.Exit(worker.Run(os.Stdout))
	}
	os.Exit(m.Run())
}

func useInProcessInspector(t *testing.T) {
	t.Helper()
	original := runInspection
	runInspection = func(ctx context.Context, _ string, file *os.File, request supervisor.Request) inspector.Result {
		source, err := inspector.SourceFromFile(file, request.Name)
		if err != nil {
			return inspector.PublicError(request.Name, request.Mode, request.TimeoutMS, "E_FILE_STAT", "fixture stat failed")
		}
		return inspector.New().Inspect(ctx, source, inspector.Options{Mode: request.Mode, Hash: request.Hash})
	}
	t.Cleanup(func() { runInspection = original })
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(root + "/../..")
}

func TestCapabilityAdapterInspectsFixture(t *testing.T) {
	useInProcessInspector(t)
	t.Setenv("OPENADAM_PROVIDER_ROOT", repositoryRoot(t))
	request := map[string]any{
		"id": "fixture", "operationId": "inspect",
		"input": map[string]any{"path": "capabilities/fixtures/users.json", "mode": "standard", "hash": "sha256"},
	}
	line, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	response := handleRequest(make(chan struct{}, maxConcurrentInspections), line)
	if !response.OK {
		t.Fatalf("unexpected error: %+v", response.Error)
	}
	result := response.Result.(canonicalResult)
	if result.Identity.Kind != "data" || result.Structured == nil || result.Structured.Format != "json" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestCapabilityAdapterRejectsMissingFixture(t *testing.T) {
	useInProcessInspector(t)
	t.Setenv("OPENADAM_PROVIDER_ROOT", repositoryRoot(t))
	line := []byte(`{"id":"missing","operationId":"inspect","input":{"path":"capabilities/fixtures/missing.json"}}`)
	response := handleRequest(make(chan struct{}, maxConcurrentInspections), line)
	if response.OK || response.Error == nil || response.Error.Code != "FILE_NOT_FOUND" {
		t.Fatalf("unexpected response: %+v", response)
	}
}

func TestCapabilityAdapterEnvelopeRejections(t *testing.T) {
	slots := make(chan struct{}, maxConcurrentInspections)
	cases := []struct {
		name string
		line string
		code string
	}{
		{"unknown envelope field", `{"id":"x","operationId":"inspect","input":{},"extra":1}`, "INVALID_INPUT"},
		{"wrong operation", `{"id":"x","operationId":"delete","input":{"path":"a"}}`, "INVALID_INPUT"},
		{"missing id", `{"operationId":"inspect","input":{"path":"a"}}`, "INVALID_INPUT"},
		{"input not object", `{"id":"x","operationId":"inspect","input":[1]}`, "INVALID_INPUT"},
		{"bad mode", `{"id":"x","operationId":"inspect","input":{"path":"a","mode":"ultra"}}`, "INVALID_INPUT"},
		{"absolute path", `{"id":"x","operationId":"inspect","input":{"path":"/etc/passwd"}}`, "PATH_FORBIDDEN"},
		{"traversal", `{"id":"x","operationId":"inspect","input":{"path":"../../etc/passwd"}}`, "PATH_FORBIDDEN"},
	}
	for _, item := range cases {
		response := handleRequest(slots, []byte(item.line))
		if response.OK || response.Error == nil || response.Error.Code != item.code {
			t.Fatalf("%s: got %+v, want %s", item.name, response.Error, item.code)
		}
	}
}

func TestCanonicalErrorCodeMapping(t *testing.T) {
	expected := map[string]string{
		"E_INVALID_INPUT":  "INVALID_INPUT",
		"E_FILE_NOT_FOUND": "FILE_NOT_FOUND",
		"E_PATH_SYMLINK":   "PATH_FORBIDDEN",
		"E_TIMEOUT":        "LIMIT_EXCEEDED",
		"E_MEMORY_LIMIT":   "LIMIT_EXCEEDED",
		"E_WORKER_FAILED":  "INSPECTION_FAILED",
		"E_INTERNAL":       "INSPECTION_FAILED",
	}
	for source, want := range expected {
		if got := canonicalErrorCode(source); got != want {
			t.Fatalf("canonicalErrorCode(%s) = %s, want %s", source, got, want)
		}
	}
}

// TestOversizedRequestLineIsRejectedWithoutKillingTheSession is the regression
// for the session-killing scanner: an oversized line must produce one error
// response and the next request must still be served.
func TestOversizedRequestLineIsRejectedWithoutKillingTheSession(t *testing.T) {
	useInProcessInspector(t)
	t.Setenv("OPENADAM_PROVIDER_ROOT", repositoryRoot(t))
	pad := strings.Repeat("x", maxRequestLineBytes)
	input := "{\"id\":\"big\",\"operationId\":\"inspect\",\"input\":{\"path\":\"" + pad + "\"}}\n" +
		"{\"id\":\"next\",\"operationId\":\"inspect\",\"input\":{\"path\":\"capabilities/fixtures/users.json\",\"mode\":\"quick\"}}\n"
	var output bytes.Buffer
	if err := serve(strings.NewReader(input), &output); err != nil {
		t.Fatalf("session died on an oversized line: %v", err)
	}
	var responses []responseEnvelope
	for _, line := range strings.Split(strings.TrimSpace(output.String()), "\n") {
		if line == "" {
			continue
		}
		var response responseEnvelope
		if err := json.Unmarshal([]byte(line), &response); err != nil {
			t.Fatalf("invalid response %q: %v", line, err)
		}
		responses = append(responses, response)
	}
	if len(responses) != 2 {
		t.Fatalf("response count: %d (%s)", len(responses), output.String())
	}
	if responses[0].ID != "" || responses[0].OK || responses[0].Error.Code != "INVALID_INPUT" {
		t.Fatalf("oversized line response: %+v", responses[0])
	}
	if responses[1].ID != "next" || !responses[1].OK {
		t.Fatalf("session did not recover: %+v", responses[1])
	}
}

// TestRealWorkerSoakRunsClean drives the real supervisor and real worker
// processes through the capability boundary, sequentially and concurrently.
func TestRealWorkerSoakRunsClean(t *testing.T) {
	t.Setenv("OPENADAM_PROVIDER_ROOT", repositoryRoot(t))
	var lines []string
	for index := 0; index < 4; index++ {
		lines = append(lines, fmt.Sprintf(`{"id":"s%d","operationId":"inspect","input":{"path":"capabilities/fixtures/users.json","mode":"quick"}}`, index))
	}
	for index := 0; index < 4; index++ {
		lines = append(lines, fmt.Sprintf(`{"id":"c%d","operationId":"inspect","input":{"path":"capabilities/fixtures/users.json","mode":"standard"}}`, index))
	}
	var output bytes.Buffer
	if err := serve(strings.NewReader(strings.Join(lines, "\n")+"\n"), &output); err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(output.String()), "\n") {
		if line == "" {
			continue
		}
		var response responseEnvelope
		if err := json.Unmarshal([]byte(line), &response); err != nil {
			t.Fatalf("invalid response %q: %v", line, err)
		}
		if !response.OK {
			t.Fatalf("real-worker call failed: %+v", response.Error)
		}
		if ids[response.ID] {
			t.Fatalf("duplicate response id %q", response.ID)
		}
		ids[response.ID] = true
	}
	if len(ids) != 8 {
		t.Fatalf("responses: unique=%d want 8", len(ids))
	}
}

func TestCapabilityConcurrencyIsBounded(t *testing.T) {
	t.Setenv("OPENADAM_PROVIDER_ROOT", repositoryRoot(t))
	original := runInspection
	var active, maximum atomic.Int32
	runInspection = func(_ context.Context, _ string, _ *os.File, request supervisor.Request) inspector.Result {
		current := active.Add(1)
		for {
			previous := maximum.Load()
			if current <= previous || maximum.CompareAndSwap(previous, current) {
				break
			}
		}
		defer active.Add(-1)
		time.Sleep(75 * time.Millisecond)
		return inspector.PublicError(request.Name, request.Mode, request.TimeoutMS, "E_WORKER_FAILED", "intentional concurrency fixture")
	}
	t.Cleanup(func() { runInspection = original })

	slots := make(chan struct{}, maxConcurrentInspections)
	start := make(chan struct{})
	responses := make(chan responseEnvelope, 8)
	var wait sync.WaitGroup
	for index := 0; index < 8; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-start
			line := []byte(fmt.Sprintf(`{"id":"bounded-%d","operationId":"inspect","input":{"path":"capabilities/fixtures/users.json","mode":"quick"}}`, index))
			responses <- handleRequest(slots, line)
		}(index)
	}
	close(start)
	wait.Wait()
	close(responses)
	if got := maximum.Load(); got != maxConcurrentInspections {
		t.Fatalf("maximum concurrent inspections = %d, want %d", got, maxConcurrentInspections)
	}
	seen := map[string]bool{}
	for response := range responses {
		if response.ID == "" || seen[response.ID] {
			t.Fatalf("invalid response identity: %+v", response)
		}
		seen[response.ID] = true
	}
	if len(seen) != 8 {
		t.Fatalf("response identities = %d, want 8", len(seen))
	}
}

func TestCapabilityQueueAdmissionSharesDeadline(t *testing.T) {
	slots := make(chan struct{}, maxConcurrentInspections)
	for index := 0; index < cap(slots); index++ {
		slots <- struct{}{}
	}
	line := []byte(`{"id":"queued","operationId":"inspect","input":{"path":"capabilities/fixtures/users.json","mode":"quick"}}`)
	started := time.Now()
	response := handleRequestWithTimeout(slots, line, 30*time.Millisecond)
	if response.OK || response.Error == nil || response.Error.Code != "LIMIT_EXCEEDED" {
		t.Fatalf("queued response: %+v", response)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("queue deadline took %s", elapsed)
	}
}

// TestDeepModeProjectsArchiveFacts pins the boundary decision: deep's bounded
// entry names must be visible at the capability projection.
func TestDeepModeProjectsArchiveFacts(t *testing.T) {
	useInProcessInspector(t)
	root := repositoryRoot(t)
	zipPath := filepath.Join(root, "capabilities", "fixtures", "entries.zip")
	if err := writeTestZip(zipPath); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(zipPath) })
	t.Setenv("OPENADAM_PROVIDER_ROOT", root)
	line := []byte(`{"id":"deep","operationId":"inspect","input":{"path":"capabilities/fixtures/entries.zip","mode":"deep"}}`)
	response := handleRequest(make(chan struct{}, maxConcurrentInspections), line)
	if !response.OK {
		t.Fatalf("deep archive inspection failed: %+v", response.Error)
	}
	result := response.Result.(canonicalResult)
	if result.Archive == nil || len(result.Archive.Entries) == 0 {
		t.Fatalf("deep archive facts were not projected: %+v", result.Archive)
	}
}

func writeTestZip(path string) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	zipWriter := zip.NewWriter(file)
	for index := 0; index < 3; index++ {
		entry, entryErr := zipWriter.Create(fmt.Sprintf("dir/entry-%d.txt", index))
		if entryErr != nil {
			return entryErr
		}
		if _, err := entry.Write([]byte("payload\n")); err != nil {
			return err
		}
	}
	return zipWriter.Close()
}
