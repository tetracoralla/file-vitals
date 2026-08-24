package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/tetracoralla/file-vitals/internal/version"
)

func main() {
	args := os.Args[1:]
	ctx := context.Background()
	stop := func() {}
	if isInspectionInvocation(args) {
		ctx, stop = signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	}
	code := runContext(ctx, args, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

func run(args []string, stdout, stderr io.Writer) int {
	return runContext(context.Background(), args, stdout, stderr)
}

func runContext(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "__worker-inspect" {
		return runWorker(stdout)
	}
	if len(args) > 0 && args[0] == "__worker-batch" {
		return runBatchWorker(stdout)
	}
	if len(args) > 0 && args[0] == "__worker-inventory" {
		return runInventoryWorker(stdout)
	}
	if len(args) > 0 {
		switch args[0] {
		case "mcp":
			return runMCP(stderr)
		case "doctor":
			return runDoctor(args[1:], stdout, stderr)
		case "schema":
			return runSchema(args[1:], stdout, stderr)
		case "version", "--version", "-v":
			fmt.Fprintf(stdout, "%s %s\n", version.Binary, version.Version)
			return 0
		case "help", "--help", "-h":
			printUsage(stdout)
			return 0
		case "inspect":
			args = args[1:]
		case "batch":
			return runBatchContext(ctx, args[1:], stdout, stderr)
		case "inventory":
			return runInventoryContext(ctx, args[1:], stdout, stderr)
		}
	}
	return runInspectContext(ctx, args, stdout, stderr)
}

func isInspectionInvocation(args []string) bool {
	if len(args) == 0 {
		return true
	}
	switch args[0] {
	case "__worker-inspect", "__worker-batch", "__worker-inventory", "mcp", "doctor", "schema", "version", "--version", "-v", "help", "--help", "-h":
		return false
	default:
		return true
	}
}

func printUsage(output io.Writer) {
	fmt.Fprintln(output, `File Vitals

Usage:
  finspect FILE [--quick|--standard|--deep] [--sha256] [--expect-sha256 HEX] [--json]
  finspect batch FILE... [--quick|--standard|--deep] [--sha256] [--json]
  finspect inventory [DIR] [--max-depth N] [--json]
  finspect doctor [--json]
  finspect schema [input|output|batch-input|batch-output|inventory-input|inventory-output]
  finspect mcp

Inspection is read-only. Quick identifies the file; standard adds the applicable
family probe; deep adds bounded archive entry names and expensive metadata.
Batch accepts at most 16 explicit files. Inventory scans at most 32 regular files,
8 directory levels, and 256 directories without following symlinks.

Exit codes: 0 ok, partial, or unsupported · 1 error result · 2 usage error · 3 corrupt file`)
}
