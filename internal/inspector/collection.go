package inspector

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/tetracoralla/file-vitals/schemas"
)

type CollectionLimits struct {
	ItemMax          int   `json:"item_max"`
	ResponseBytesMax int   `json:"response_bytes_max"`
	TimeoutMS        int64 `json:"timeout_ms"`
	MemoryBytesMax   int64 `json:"memory_bytes_max"`
	Truncated        bool  `json:"truncated,omitempty"`
}

type BatchSource struct {
	Path   string
	Source *Source
	Error  *ErrorInfo
}

type BatchItem struct {
	Index  int    `json:"index"`
	Path   string `json:"path"`
	Result Result `json:"result"`
}

type BatchResult struct {
	SchemaVersion string           `json:"schema_version"`
	Status        string           `json:"status"`
	Items         []BatchItem      `json:"items"`
	Diagnostics   []Diagnostic     `json:"diagnostics"`
	Limits        CollectionLimits `json:"limits"`
	Error         *ErrorInfo       `json:"error,omitempty"`
}

func (i *Inspector) InspectBatch(ctx context.Context, inputs []BatchSource, options Options) BatchResult {
	options = options.Normalized()
	result := BatchResult{
		SchemaVersion: "1.0",
		Status:        "ok",
		Items:         []BatchItem{},
		Diagnostics:   []Diagnostic{},
		Limits: CollectionLimits{
			ItemMax: MaxBatchItems, ResponseBytesMax: MaxCollectionBytes,
			TimeoutMS: options.Timeout.Milliseconds(), MemoryBytesMax: MaxMemoryBytes,
		},
	}
	if len(inputs) == 0 || len(inputs) > MaxBatchItems {
		return PublicBatchError(options.Timeout.Milliseconds(), "E_INVALID_INPUT", "Batch input must contain 1 to 16 files.")
	}
	if options.Mode != ModeQuick && options.Mode != ModeStandard && options.Mode != ModeDeep {
		return PublicBatchError(options.Timeout.Milliseconds(), "E_INVALID_INPUT", "Inspection mode must be quick, standard, or deep.")
	}
	if options.Hash != HashNone && options.Hash != HashSHA256 {
		return PublicBatchError(options.Timeout.Milliseconds(), "E_INVALID_INPUT", "Hash mode must be none or sha256.")
	}
	if options.ExpectedSHA256 != "" {
		return PublicBatchError(options.Timeout.Milliseconds(), "E_INVALID_INPUT", "Batch inspection does not accept one expected digest for multiple files.")
	}
	remainingHashBytes := MaxHashBytes
	completed := 0
	for index, input := range inputs {
		path := bounded(input.Path, 4096)
		var item Result
		switch {
		case input.Error != nil:
			item = PublicError(path, options.Mode, options.Timeout.Milliseconds(), input.Error.Code, input.Error.Message)
		case input.Source == nil:
			item = PublicError(path, options.Mode, options.Timeout.Milliseconds(), "E_INVALID_SOURCE", "No open file was provided for this batch item.")
		default:
			itemOptions := options
			trustedSize := input.Source.Size
			if input.Source.File != nil {
				if stat, err := input.Source.File.Stat(); err == nil {
					trustedSize = stat.Size()
				}
			}
			if itemOptions.Hash == HashSHA256 && trustedSize > remainingHashBytes {
				itemOptions.Hash = HashNone
				item = i.Inspect(ctx, *input.Source, itemOptions)
				makePartial(&item)
				addDiagnostic(&item, "BATCH_HASH_SIZE_LIMIT", "warning", "SHA-256 was omitted because the batch exhausted its cumulative hash-input budget.")
				item.Constraints = deriveConstraints(item)
			} else {
				item = i.Inspect(ctx, *input.Source, itemOptions)
				if itemOptions.Hash == HashSHA256 && item.Status != "error" {
					remainingHashBytes -= item.File.SizeBytes
				}
			}
		}
		if item.Status != "error" {
			completed++
		}
		result.Items = append(result.Items, BatchItem{Index: index, Path: path, Result: item})
	}
	result.Status = collectionStatus(completed, len(result.Items))
	fitBatchResponse(&result)
	if err := schemas.ValidateBatchResult(result); err != nil {
		return PublicBatchError(options.Timeout.Milliseconds(), "E_RESULT_SCHEMA", "The batch result did not satisfy its published schema.")
	}
	return result
}

func PublicBatchError(timeoutMS int64, code, message string) BatchResult {
	if timeoutMS <= 0 {
		timeoutMS = 5000
	}
	return BatchResult{
		SchemaVersion: "1.0", Status: "error", Items: []BatchItem{}, Diagnostics: []Diagnostic{},
		Limits: CollectionLimits{ItemMax: MaxBatchItems, ResponseBytesMax: MaxCollectionBytes, TimeoutMS: timeoutMS, MemoryBytesMax: MaxMemoryBytes},
		Error:  &ErrorInfo{Code: bounded(code, 64), Message: bounded(message, 512)},
	}
}

func fitBatchResponse(result *BatchResult) {
	encoded, err := json.Marshal(result)
	if err == nil && len(encoded)+1 <= MaxCollectionBytes {
		return
	}
	for index := range result.Items {
		item := &result.Items[index].Result
		if item.Archive != nil && len(item.Archive.Entries) > 0 {
			item.Archive.Entries = nil
			item.Archive.EntriesTruncated = true
			item.Limits.Truncated = true
			addDiagnostic(item, "BATCH_RESPONSE_TRUNCATED", "warning", "Archive entry names were removed to fit the cumulative batch response budget.")
		}
	}
	result.Limits.Truncated = true
	result.Status = "partial"
	encoded, err = json.Marshal(result)
	if err == nil && len(encoded)+1 <= MaxCollectionBytes {
		return
	}
	for index := len(result.Items) - 1; index >= 0; index-- {
		path := result.Items[index].Path
		mode := result.Items[index].Result.Limits.Mode
		result.Items[index].Result = PublicError(path, mode, result.Limits.TimeoutMS, "E_BATCH_RESPONSE_LIMIT", "This item result was omitted to preserve the complete batch response budget.")
		result.Limits.Truncated = true
		result.Status = "partial"
		encoded, err = json.Marshal(result)
		if err == nil && len(encoded)+1 <= MaxCollectionBytes {
			return
		}
	}
	result.Items = nil
	result.Status = "error"
	result.Error = &ErrorInfo{Code: "E_RESPONSE_TOO_LARGE", Message: "The batch result could not fit its complete response budget."}
}

func collectionStatus(completed, total int) string {
	if completed == 0 {
		return "error"
	}
	if completed < total {
		return "partial"
	}
	return "ok"
}

type InventorySource struct {
	Path   string
	Source Source
	Error  *ErrorInfo
}

type InventoryCollection struct {
	EntriesScanned     int
	DirectoriesScanned int
	SymlinksSkipped    int
	SpecialSkipped     int
	Truncated          bool
}

type InventoryIdentity struct {
	Kind           string `json:"kind"`
	MediaType      string `json:"media_type"`
	Format         string `json:"format"`
	Confidence     string `json:"confidence"`
	ExtensionMatch *bool  `json:"extension_match,omitempty"`
}

type InventoryItem struct {
	Path            string            `json:"path"`
	Status          string            `json:"status"`
	SizeBytes       int64             `json:"size_bytes"`
	Identity        InventoryIdentity `json:"identity"`
	Traits          []string          `json:"traits"`
	Constraints     []string          `json:"constraints"`
	DiagnosticCodes []string          `json:"diagnostic_codes"`
	Error           *ErrorInfo        `json:"error,omitempty"`
}

type InventoryFormat struct {
	Kind           string `json:"kind"`
	MediaType      string `json:"media_type"`
	Format         string `json:"format"`
	FileCount      int    `json:"file_count"`
	TotalSizeBytes int64  `json:"total_size_bytes"`
}

type InventoryLimits struct {
	MaxDepth         int   `json:"max_depth"`
	MaxFiles         int   `json:"max_files"`
	MaxDirectories   int   `json:"max_directories"`
	MaxEntries       int   `json:"max_entries"`
	ResponseBytesMax int   `json:"response_bytes_max"`
	TimeoutMS        int64 `json:"timeout_ms"`
	MemoryBytesMax   int64 `json:"memory_bytes_max"`
	Truncated        bool  `json:"truncated,omitempty"`
}

type InventoryResult struct {
	SchemaVersion      string            `json:"schema_version"`
	Status             string            `json:"status"`
	Root               string            `json:"root"`
	FilesScanned       int               `json:"files_scanned"`
	EntriesScanned     int               `json:"entries_scanned"`
	DirectoriesScanned int               `json:"directories_scanned"`
	SymlinksSkipped    int               `json:"symlinks_skipped"`
	SpecialSkipped     int               `json:"special_entries_skipped"`
	TotalSizeBytes     int64             `json:"total_size_bytes"`
	Formats            []InventoryFormat `json:"formats"`
	Items              []InventoryItem   `json:"items"`
	Diagnostics        []Diagnostic      `json:"diagnostics"`
	Limits             InventoryLimits   `json:"limits"`
	Error              *ErrorInfo        `json:"error,omitempty"`
}

func (i *Inspector) InspectInventory(ctx context.Context, root string, sources []InventorySource, collection InventoryCollection, maxDepth int, timeout time.Duration) InventoryResult {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	if maxDepth < 0 || maxDepth > MaxInventoryDepth || len(sources) > MaxInventoryFiles || collection.EntriesScanned < 0 || collection.EntriesScanned > MaxInventoryEntries || collection.DirectoriesScanned < 0 || collection.DirectoriesScanned > MaxInventoryDirs {
		return PublicInventoryError(root, maxDepth, timeout.Milliseconds(), "E_INVALID_INPUT", "Inventory limits exceed the published bounds.")
	}
	result := InventoryResult{
		SchemaVersion: "1.1", Status: "ok", Root: bounded(root, 4096),
		EntriesScanned: collection.EntriesScanned, DirectoriesScanned: collection.DirectoriesScanned, SymlinksSkipped: collection.SymlinksSkipped,
		SpecialSkipped: collection.SpecialSkipped, Formats: []InventoryFormat{}, Items: []InventoryItem{}, Diagnostics: []Diagnostic{},
		Limits: InventoryLimits{MaxDepth: maxDepth, MaxFiles: MaxInventoryFiles, MaxDirectories: MaxInventoryDirs, MaxEntries: MaxInventoryEntries, ResponseBytesMax: MaxCollectionBytes, TimeoutMS: timeout.Milliseconds(), MemoryBytesMax: MaxMemoryBytes, Truncated: collection.Truncated},
	}
	formats := map[string]*InventoryFormat{}
	for _, input := range sources {
		if input.Error != nil {
			// A source whose inherited descriptor could not be used is reported
			// as its own failing item instead of being silently dropped.
			result.Items = append(result.Items, InventoryItem{
				Path: bounded(input.Path, 4096), Status: "error",
				Identity: InventoryIdentity{Kind: "unknown", Confidence: "unknown"},
				Traits:   []string{}, Constraints: []string{}, DiagnosticCodes: []string{}, Error: input.Error,
			})
			if result.Status == "ok" {
				result.Status = "partial"
			}
			continue
		}
		itemResult := i.Inspect(ctx, input.Source, Options{Mode: ModeQuick, Hash: HashNone, Timeout: timeout})
		item := InventoryItem{
			Path: bounded(input.Path, 4096), Status: itemResult.Status, SizeBytes: itemResult.File.SizeBytes,
			Identity: InventoryIdentity{Kind: itemResult.Identity.Kind, MediaType: itemResult.Identity.MediaType, Format: itemResult.Identity.Format, Confidence: itemResult.Identity.Confidence, ExtensionMatch: itemResult.Identity.ExtensionMatch},
			Traits:   append([]string{}, itemResult.Traits...), Constraints: append([]string{}, itemResult.Constraints...), DiagnosticCodes: []string{}, Error: itemResult.Error,
		}
		for _, diagnostic := range itemResult.Diagnostics {
			item.DiagnosticCodes = append(item.DiagnosticCodes, diagnostic.Code)
		}
		result.Items = append(result.Items, item)
		result.FilesScanned++
		result.TotalSizeBytes += item.SizeBytes
		key := item.Identity.Kind + "\x00" + item.Identity.MediaType + "\x00" + item.Identity.Format
		format := formats[key]
		if format == nil {
			format = &InventoryFormat{Kind: item.Identity.Kind, MediaType: item.Identity.MediaType, Format: item.Identity.Format}
			formats[key] = format
		}
		format.FileCount++
		format.TotalSizeBytes += item.SizeBytes
		if item.Status == "error" && result.Status == "ok" {
			result.Status = "partial"
		}
	}
	for _, format := range formats {
		result.Formats = append(result.Formats, *format)
	}
	sort.Slice(result.Formats, func(a, b int) bool {
		if result.Formats[a].FileCount != result.Formats[b].FileCount {
			return result.Formats[a].FileCount > result.Formats[b].FileCount
		}
		if result.Formats[a].Format != result.Formats[b].Format {
			return result.Formats[a].Format < result.Formats[b].Format
		}
		if result.Formats[a].Kind != result.Formats[b].Kind {
			return result.Formats[a].Kind < result.Formats[b].Kind
		}
		return result.Formats[a].MediaType < result.Formats[b].MediaType
	})
	if collection.Truncated {
		result.Status = "partial"
		result.Diagnostics = append(result.Diagnostics, Diagnostic{Code: "INVENTORY_LIMIT", Severity: "warning", Message: "Workspace inventory stopped at a declared file, directory, depth, or entry limit, or skipped entries that changed or could not be opened during the scan."})
	}
	fitInventoryResponse(&result)
	if err := schemas.ValidateInventoryResult(result); err != nil {
		return PublicInventoryError(root, maxDepth, timeout.Milliseconds(), "E_RESULT_SCHEMA", "The inventory result did not satisfy its published schema.")
	}
	return result
}

func PublicInventoryError(root string, maxDepth int, timeoutMS int64, code, message string) InventoryResult {
	if timeoutMS <= 0 {
		timeoutMS = 5000
	}
	if maxDepth < 0 {
		maxDepth = 0
	}
	if maxDepth > MaxInventoryDepth {
		maxDepth = MaxInventoryDepth
	}
	return InventoryResult{
		SchemaVersion: "1.1", Status: "error", Root: bounded(root, 4096), Formats: []InventoryFormat{}, Items: []InventoryItem{}, Diagnostics: []Diagnostic{},
		Limits: InventoryLimits{MaxDepth: maxDepth, MaxFiles: MaxInventoryFiles, MaxDirectories: MaxInventoryDirs, MaxEntries: MaxInventoryEntries, ResponseBytesMax: MaxCollectionBytes, TimeoutMS: timeoutMS, MemoryBytesMax: MaxMemoryBytes},
		Error:  &ErrorInfo{Code: bounded(code, 64), Message: bounded(message, 512)},
	}
}

func fitInventoryResponse(result *InventoryResult) {
	encoded, err := json.Marshal(result)
	if err == nil && len(encoded)+1 <= MaxCollectionBytes {
		return
	}
	if !hasDiagnosticCodeValue(result.Diagnostics, "INVENTORY_RESPONSE_TRUNCATED") {
		result.Diagnostics = append(result.Diagnostics, Diagnostic{Code: "INVENTORY_RESPONSE_TRUNCATED", Severity: "warning", Message: "Detailed inventory items were omitted to fit the complete response budget; aggregate counts remain complete for the scanned prefix."})
	}
	for len(result.Items) > 0 {
		result.Items = result.Items[:len(result.Items)-1]
		result.Limits.Truncated = true
		result.Status = "partial"
		encoded, err = json.Marshal(result)
		if err == nil && len(encoded)+1 <= MaxCollectionBytes {
			return
		}
	}
	result.Status = "error"
	result.Error = &ErrorInfo{Code: "E_RESPONSE_TOO_LARGE", Message: "The workspace inventory could not fit its complete response budget."}
}

func hasDiagnosticCodeValue(values []Diagnostic, wanted string) bool {
	for _, value := range values {
		if value.Code == wanted {
			return true
		}
	}
	return false
}
