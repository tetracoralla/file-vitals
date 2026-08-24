package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"testing"
)

// TestMain routes spawned copies of the test binary into the real worker mode,
// so supervisor/mcp tests exercise the genuine worker path end to end.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "__worker-inspect":
			os.Exit(runWorker(os.Stdout))
		case "__worker-batch":
			os.Exit(runBatchWorker(os.Stdout))
		case "__worker-inventory":
			os.Exit(runInventoryWorker(os.Stdout))
		}
	}
	if os.Getenv("FILE_VITALS_TEST_CLI") == "1" {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		code := runContext(ctx, os.Args[1:], os.Stdout, os.Stderr)
		stop()
		os.Exit(code)
	}
	os.Exit(m.Run())
}
