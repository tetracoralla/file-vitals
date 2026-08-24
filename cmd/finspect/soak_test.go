package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tetracoralla/file-vitals/internal/mcp"
)

func openFds(t *testing.T) (int, bool) {
	t.Helper()
	directory, err := os.Open("/dev/fd")
	if err != nil {
		return 0, false
	}
	defer directory.Close()
	names, err := directory.Readdirnames(-1)
	if err != nil {
		return 0, false
	}
	return len(names), true
}

// TestRealWorkerSoakRunsClean drives one MCP server over the real supervisor,
// real worker processes, and real authority opens: sequential and concurrent
// traffic must all resolve, and the process must end where it started on
// goroutines and file descriptors.
func TestRealWorkerSoakRunsClean(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "sample.txt"), []byte("soak sample\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	server, err := mcp.New(executable, root)
	if err != nil {
		t.Fatal(err)
	}

	goroutinesBefore := runtime.NumGoroutine()
	fdsBefore, fdsKnown := openFds(t)

	// The volume stays inside what one 5s deadline can absorb at 4 concurrent
	// slots even under -race, where every real worker spawn (plus the system
	// identity probe in standard mode) is several times slower.
	const sequential = 6
	const concurrent = 4
	initialize := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25"}}`
	nextID := 2
	sequentialLines := make([]string, 0, sequential)
	concurrentLines := make([]string, 0, concurrent)
	for index := 0; index < sequential; index++ {
		sequentialLines = append(sequentialLines, callLine(nextID, "sample.txt", "quick"))
		nextID++
	}
	for index := 0; index < concurrent; index++ {
		concurrentLines = append(concurrentLines, callLine(nextID, "sample.txt", "standard"))
		nextID++
	}

	var output safeSoakBuffer
	if err := server.Serve(context.Background(), strings.NewReader(initialize+"\n"), &output); err != nil {
		t.Fatal(err)
	}
	for _, line := range sequentialLines {
		if err := server.Serve(context.Background(), strings.NewReader(line+"\n"), &output); err != nil {
			t.Fatal(err)
		}
	}
	if err := server.Serve(context.Background(), strings.NewReader(strings.Join(concurrentLines, "\n")+"\n"), &output); err != nil {
		t.Fatal(err)
	}

	responses := splitSoakResponses(t, output.String())
	if len(responses) != 1+sequential+concurrent {
		t.Fatalf("response count: %d (expected %d)", len(responses), 1+sequential+concurrent)
	}
	inspected := 0
	for _, line := range responses[1:] {
		if !strings.Contains(line, `"status":"ok"`) && !strings.Contains(line, `"status": "ok"`) {
			t.Fatalf("soak call did not succeed: %s", line)
		}
		inspected++
	}
	if inspected != sequential+concurrent {
		t.Fatalf("inspected %d of %d calls", inspected, sequential+concurrent)
	}

	// The server waits for call goroutines before Serve returns; a small retry
	// window absorbs runtime-background cleanup timing.
	goroutinesAfter := goroutinesBefore
	for attempt := 0; attempt < 50; attempt++ {
		goroutinesAfter = runtime.NumGoroutine()
		if goroutinesAfter <= goroutinesBefore+1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if goroutinesAfter > goroutinesBefore+1 {
		t.Fatalf("goroutines leaked across soak: before=%d after=%d", goroutinesBefore, goroutinesAfter)
	}
	if fdsKnown {
		fdsAfter, _ := openFds(t)
		if fdsAfter > fdsBefore+2 {
			t.Fatalf("file descriptors leaked across soak: before=%d after=%d", fdsBefore, fdsAfter)
		}
	}
}

func callLine(id int, path, mode string) string {
	return `{"jsonrpc":"2.0","id":` + strconv.Itoa(id) + `,"method":"tools/call","params":{"name":"file_inspect","arguments":{"path":"` + path + `","mode":"` + mode + `"}}}`
}

type safeSoakBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeSoakBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeSoakBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func splitSoakResponses(t *testing.T, data string) []string {
	t.Helper()
	var responses []string
	for _, line := range strings.Split(strings.TrimSpace(data), "\n") {
		if line != "" {
			responses = append(responses, line)
		}
	}
	return responses
}
