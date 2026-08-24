package inspector

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tetracoralla/file-vitals/schemas"
)

func TestBatchResponseFittingPreservesCorrelationWithExplicitTruncation(t *testing.T) {
	result := BatchResult{
		SchemaVersion: "1.0", Status: "ok", Items: []BatchItem{}, Diagnostics: []Diagnostic{},
		Limits: CollectionLimits{ItemMax: MaxBatchItems, ResponseBytesMax: MaxCollectionBytes, TimeoutMS: 5000, MemoryBytesMax: MaxMemoryBytes},
	}
	for index := 0; index < MaxBatchItems; index++ {
		item := baseResult(Source{Name: fmt.Sprintf("archive-%d.zip", index)}, Options{Mode: ModeDeep, Timeout: 5 * time.Second})
		item.Status = "ok"
		item.Identity = Identity{Kind: "archive", MediaType: "application/zip", Format: "ZIP", Confidence: "exact", Candidates: []Candidate{}, Conflicts: []string{}}
		item.Archive = &ArchiveInfo{
			Format: "zip", EntriesScanned: MaxArchiveEntryNames, UncompressedBytesScanned: MaxArchiveEntryNames,
			PathFacts: ArchivePathFacts{InspectionComplete: true}, Entries: []ArchiveEntry{},
		}
		for entryIndex := 0; entryIndex < MaxArchiveEntryNames; entryIndex++ {
			item.Archive.Entries = append(item.Archive.Entries, ArchiveEntry{Name: strings.Repeat("x", 240) + fmt.Sprintf("-%03d", entryIndex), SizeBytes: 1, Kind: "file"})
		}
		result.Items = append(result.Items, BatchItem{Index: index, Path: item.File.Name, Result: item})
	}
	fitBatchResponse(&result)
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded)+1 > MaxCollectionBytes || result.Status != "partial" || !result.Limits.Truncated || len(result.Items) != MaxBatchItems {
		t.Fatalf("batch response fit failed: bytes=%d result=%#v", len(encoded), result.Limits)
	}
	for index, item := range result.Items {
		if item.Index != index || item.Result.Archive == nil || len(item.Result.Archive.Entries) != 0 || !item.Result.Archive.EntriesTruncated || !hasDiagnostic(item.Result, "BATCH_RESPONSE_TRUNCATED") {
			t.Fatalf("item %d lost correlation or truncation evidence: %#v", index, item)
		}
	}
	if err := schemas.ValidateBatchResult(result); err != nil {
		t.Fatalf("fitted batch left schema: %v", err)
	}
}

func TestInventoryResponseFittingKeepsScannedPrefixAggregates(t *testing.T) {
	result := InventoryResult{
		SchemaVersion: "1.0", Status: "ok", Root: ".", FilesScanned: MaxInventoryFiles, DirectoriesScanned: 1,
		Formats: []InventoryFormat{{Kind: "text", MediaType: "text/plain", Format: "Plain text", FileCount: MaxInventoryFiles, TotalSizeBytes: MaxInventoryFiles}},
		Items:   []InventoryItem{}, Diagnostics: []Diagnostic{},
		Limits: InventoryLimits{MaxDepth: 4, MaxFiles: MaxInventoryFiles, ResponseBytesMax: MaxCollectionBytes, TimeoutMS: 5000, MemoryBytesMax: MaxMemoryBytes},
	}
	for index := 0; index < MaxInventoryFiles; index++ {
		codes := make([]string, 64)
		for codeIndex := range codes {
			codes[codeIndex] = strings.Repeat("A", 64)
		}
		result.Items = append(result.Items, InventoryItem{
			Path: strings.Repeat("p", 4090) + fmt.Sprintf("%02d", index), Status: "ok", SizeBytes: 1,
			Identity: InventoryIdentity{Kind: "text", MediaType: "text/plain", Format: "Plain text", Confidence: "probable"},
			Traits:   []string{}, Constraints: []string{}, DiagnosticCodes: codes,
		})
	}
	fitInventoryResponse(&result)
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded)+1 > MaxCollectionBytes || result.Status != "partial" || !result.Limits.Truncated || result.FilesScanned != MaxInventoryFiles || len(result.Formats) != 1 {
		t.Fatalf("inventory response fit failed: bytes=%d result=%#v", len(encoded), result)
	}
	if !hasDiagnosticCodeValue(result.Diagnostics, "INVENTORY_RESPONSE_TRUNCATED") {
		t.Fatalf("inventory truncation was silent: %#v", result.Diagnostics)
	}
	if err := schemas.ValidateInventoryResult(result); err != nil {
		t.Fatalf("fitted inventory left schema: %v", err)
	}
}

func TestInventorySourceErrorsSurfaceAsFailingItems(t *testing.T) {
	path := filepath.Join(t.TempDir(), "one.txt")
	if err := os.WriteFile(path, []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	source, err := SourceFromFile(file, "one.txt")
	if err != nil {
		t.Fatal(err)
	}
	sources := []InventorySource{
		{Path: "one.txt", Source: source},
		{Path: "broken.bin", Error: &ErrorInfo{Code: "E_FILE_STAT", Message: "The inherited file descriptor could not be inspected."}},
	}
	result := New().InspectInventory(context.Background(), ".", sources, InventoryCollection{DirectoriesScanned: 1}, 1, 5*time.Second)
	if result.Status != "partial" || result.Error != nil {
		t.Fatalf("inventory with a failing source must stay schema-valid and partial: %#v", result)
	}
	if result.FilesScanned != 1 || len(result.Items) != 2 {
		t.Fatalf("failed sources are not scanned but must stay reported: files=%d items=%d", result.FilesScanned, len(result.Items))
	}
	failed := result.Items[1]
	if failed.Path != "broken.bin" || failed.Status != "error" || failed.Error == nil || failed.Error.Code != "E_FILE_STAT" ||
		failed.Identity.Kind != "unknown" || failed.Identity.Confidence != "unknown" || failed.SizeBytes != 0 {
		t.Fatalf("failing inventory item lost its typed error: %#v", failed)
	}
}
