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

func RunBatch(stdout io.Writer) int {
	debug.SetMemoryLimit(inspector.GoMemoryLimitBytes)
	var request supervisor.BatchRequest
	if !decodeWorkerRequest(inspector.MaxCollectionRequestBytes, &request) || len(request.Items) == 0 || len(request.Items) > inspector.MaxBatchItems {
		return 70
	}
	request.TimeoutMS = normalizedTimeout(request.TimeoutMS)
	sources := make([]inspector.BatchSource, 0, len(request.Items))
	opened := []*os.File{}
	for _, item := range request.Items {
		if item.Error != nil {
			sources = append(sources, inspector.BatchSource{Path: item.Name, Error: item.Error})
			continue
		}
		if item.DescriptorIndex < 0 || item.DescriptorIndex >= inspector.MaxBatchItems {
			sources = append(sources, inspector.BatchSource{Path: item.Name, Error: &inspector.ErrorInfo{Code: "E_INVALID_SOURCE", Message: "The inherited file descriptor index is invalid."}})
			continue
		}
		file := os.NewFile(uintptr(3+item.DescriptorIndex), "batch-file")
		if file == nil {
			sources = append(sources, inspector.BatchSource{Path: item.Name, Error: &inspector.ErrorInfo{Code: "E_FILE_STAT", Message: "The inherited file descriptor could not be inspected."}})
			continue
		}
		opened = append(opened, file)
		source, err := inspector.SourceFromFile(file, item.Name)
		if err != nil {
			sources = append(sources, inspector.BatchSource{Path: item.Name, Error: &inspector.ErrorInfo{Code: "E_FILE_STAT", Message: "The inherited file descriptor could not be inspected."}})
			continue
		}
		sources = append(sources, inspector.BatchSource{Path: item.Name, Source: &source})
	}
	defer closeFiles(opened)
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(request.TimeoutMS)*time.Millisecond)
	defer cancel()
	result := inspectBatchWithRecover(ctx, sources, request)
	if json.NewEncoder(stdout).Encode(result) != nil {
		return 70
	}
	return 0
}

func RunInventory(stdout io.Writer) int {
	debug.SetMemoryLimit(inspector.GoMemoryLimitBytes)
	var request supervisor.InventoryRequest
	if !decodeWorkerRequest(inspector.MaxCollectionRequestBytes, &request) || len(request.Items) > inspector.MaxInventoryFiles || request.MaxDepth < 0 || request.MaxDepth > inspector.MaxInventoryDepth {
		return 70
	}
	request.TimeoutMS = normalizedTimeout(request.TimeoutMS)
	sources := make([]inspector.InventorySource, 0, len(request.Items))
	opened := []*os.File{}
	for _, item := range request.Items {
		if item.DescriptorIndex < 0 || item.DescriptorIndex >= inspector.MaxInventoryFiles {
			sources = append(sources, inspector.InventorySource{Path: item.Name, Error: &inspector.ErrorInfo{Code: "E_INVALID_SOURCE", Message: "The inherited file descriptor index is invalid."}})
			continue
		}
		file := os.NewFile(uintptr(3+item.DescriptorIndex), "inventory-file")
		if file == nil {
			sources = append(sources, inspector.InventorySource{Path: item.Name, Error: &inspector.ErrorInfo{Code: "E_FILE_STAT", Message: "The inherited file descriptor could not be inspected."}})
			continue
		}
		opened = append(opened, file)
		source, err := inspector.SourceFromFile(file, item.Name)
		if err != nil {
			sources = append(sources, inspector.InventorySource{Path: item.Name, Error: &inspector.ErrorInfo{Code: "E_FILE_STAT", Message: "The inherited file descriptor could not be inspected."}})
			continue
		}
		sources = append(sources, inspector.InventorySource{Path: item.Name, Source: source})
	}
	defer closeFiles(opened)
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(request.TimeoutMS)*time.Millisecond)
	defer cancel()
	result := inspectInventoryWithRecover(ctx, sources, request)
	if json.NewEncoder(stdout).Encode(result) != nil {
		return 70
	}
	return 0
}

func inspectBatchWithRecover(ctx context.Context, sources []inspector.BatchSource, request supervisor.BatchRequest) (result inspector.BatchResult) {
	defer func() {
		if recover() != nil {
			result = inspector.PublicBatchError(request.TimeoutMS, "E_INTERNAL", "The batch worker encountered an internal failure.")
		}
	}()
	return inspector.New().InspectBatch(ctx, sources, inspector.Options{Mode: request.Mode, Hash: request.Hash, Timeout: time.Duration(request.TimeoutMS) * time.Millisecond})
}

func inspectInventoryWithRecover(ctx context.Context, sources []inspector.InventorySource, request supervisor.InventoryRequest) (result inspector.InventoryResult) {
	defer func() {
		if recover() != nil {
			result = inspector.PublicInventoryError(request.Root, request.MaxDepth, request.TimeoutMS, "E_INTERNAL", "The inventory worker encountered an internal failure.")
		}
	}()
	collection := inspector.InventoryCollection{DirectoriesScanned: request.DirectoriesScanned, SymlinksSkipped: request.SymlinksSkipped, SpecialSkipped: request.SpecialSkipped, Truncated: request.Truncated}
	return inspector.New().InspectInventory(ctx, request.Root, sources, collection, request.MaxDepth, time.Duration(request.TimeoutMS)*time.Millisecond)
}

func normalizedTimeout(value int64) int64 {
	if value < 100 || value > 60_000 {
		return 10_000
	}
	return value
}

func closeFiles(files []*os.File) {
	for _, file := range files {
		_ = file.Close()
	}
}
