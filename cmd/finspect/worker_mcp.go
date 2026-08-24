package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/tetracoralla/file-vitals/internal/mcp"
	"github.com/tetracoralla/file-vitals/internal/worker"
)

func runWorker(stdout io.Writer) int {
	return worker.Run(stdout)
}

func runMCP(stderr io.Writer) int {
	executable, err := os.Executable()
	if err != nil {
		fmt.Fprintln(stderr, "Unable to locate the running executable.")
		return 1
	}
	server, err := mcp.New(executable, os.Getenv("UFI_WORKSPACE_ROOT"))
	if err != nil {
		fmt.Fprintln(stderr, "Unable to load the MCP contract.")
		return 1
	}
	if err := server.Serve(context.Background(), os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(stderr, "MCP transport closed with an error.")
		return 1
	}
	return 0
}
