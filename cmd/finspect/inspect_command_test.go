package main

import (
	"context"
	"errors"
	"testing"
)

func TestOpenCLIFileHonorsCancellationBeforeOpen(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	file, err := openCLIFile(ctx, "missing")
	if file != nil {
		file.Close()
		t.Fatal("cancelled open returned a file")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}
