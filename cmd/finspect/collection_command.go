package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/tetracoralla/file-vitals/internal/authority"
	"github.com/tetracoralla/file-vitals/internal/inspector"
	"github.com/tetracoralla/file-vitals/internal/supervisor"
)

type batchArgs struct {
	paths   []string
	mode    inspector.Mode
	hash    inspector.HashMode
	json    bool
	timeout time.Duration
}

type inventoryArgs struct {
	path     string
	maxDepth int
	json     bool
	timeout  time.Duration
}

func parseBatchArgs(args []string) (batchArgs, error) {
	parsed := batchArgs{paths: []string{}, mode: inspector.ModeStandard, hash: inspector.HashNone, timeout: 10 * time.Second}
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "--json":
			parsed.json = true
		case arg == "--quick":
			parsed.mode = inspector.ModeQuick
		case arg == "--standard":
			parsed.mode = inspector.ModeStandard
		case arg == "--deep":
			parsed.mode = inspector.ModeDeep
		case arg == "--sha256":
			parsed.hash = inspector.HashSHA256
		case arg == "--mode" || arg == "--hash" || arg == "--timeout":
			if index+1 >= len(args) {
				return parsed, fmt.Errorf("%s requires a value", arg)
			}
			index++
			if err := applyBatchOption(&parsed, arg, args[index]); err != nil {
				return parsed, err
			}
		case strings.HasPrefix(arg, "--mode="):
			parsed.mode = inspector.Mode(strings.TrimPrefix(arg, "--mode="))
		case strings.HasPrefix(arg, "--hash="):
			parsed.hash = inspector.HashMode(strings.TrimPrefix(arg, "--hash="))
		case strings.HasPrefix(arg, "--timeout="):
			if err := applyBatchOption(&parsed, "--timeout", strings.TrimPrefix(arg, "--timeout=")); err != nil {
				return parsed, err
			}
		case strings.HasPrefix(arg, "-"):
			return parsed, fmt.Errorf("unknown option %s", arg)
		default:
			parsed.paths = append(parsed.paths, arg)
		}
	}
	if len(parsed.paths) == 0 || len(parsed.paths) > inspector.MaxBatchItems {
		return parsed, errors.New("batch requires 1 to 16 file paths")
	}
	seen := map[string]struct{}{}
	for _, path := range parsed.paths {
		if _, exists := seen[path]; exists {
			return parsed, errors.New("batch file paths must be unique")
		}
		seen[path] = struct{}{}
	}
	if parsed.mode != inspector.ModeQuick && parsed.mode != inspector.ModeStandard && parsed.mode != inspector.ModeDeep {
		return parsed, errors.New("mode must be quick, standard, or deep")
	}
	if parsed.hash != inspector.HashNone && parsed.hash != inspector.HashSHA256 {
		return parsed, errors.New("hash must be none or sha256")
	}
	if parsed.timeout < 100*time.Millisecond || parsed.timeout > 60*time.Second {
		return parsed, errors.New("timeout must be between 100ms and 60s")
	}
	return parsed, nil
}

func applyBatchOption(parsed *batchArgs, name, value string) error {
	switch name {
	case "--mode":
		parsed.mode = inspector.Mode(value)
	case "--hash":
		parsed.hash = inspector.HashMode(value)
	case "--timeout":
		duration, err := time.ParseDuration(value)
		if err != nil {
			return errors.New("timeout must be a duration such as 10s")
		}
		parsed.timeout = duration
	}
	return nil
}

func runBatchContext(parent context.Context, args []string, stdout, stderr io.Writer) int {
	parsed, err := parseBatchArgs(args)
	if err != nil {
		if containsJSONFlag(args) {
			_ = json.NewEncoder(stdout).Encode(inspector.PublicBatchError(10_000, "E_USAGE", err.Error()))
		} else {
			fmt.Fprintln(stderr, err)
			fmt.Fprintln(stderr, "Run 'finspect help' for usage.")
		}
		return 2
	}
	ctx, cancel := context.WithTimeout(parent, parsed.timeout)
	defer cancel()
	files := []*os.File{}
	defer func() { closeCLIFiles(files) }()
	items := make([]supervisor.CollectionItem, 0, len(parsed.paths))
	for _, path := range parsed.paths {
		file, openErr := openCLIFile(ctx, path)
		if openErr != nil {
			if errors.Is(openErr, context.Canceled) || errors.Is(openErr, context.DeadlineExceeded) {
				return emitBatchResult(parsed, cliBatchContextFailure(openErr, parsed.timeout), stdout)
			}
			code := "E_FILE_ACCESS"
			message := "The file could not be opened."
			if errors.Is(openErr, os.ErrNotExist) {
				code = "E_FILE_NOT_FOUND"
			}
			if errors.Is(openErr, errCLINotRegular) {
				code, message = "E_NOT_REGULAR_FILE", "Only regular files can be inspected."
			}
			items = append(items, supervisor.CollectionItem{Name: path, Error: &inspector.ErrorInfo{Code: code, Message: message}})
			continue
		}
		items = append(items, supervisor.CollectionItem{Name: path, DescriptorIndex: len(files)})
		files = append(files, file)
	}
	executable, err := os.Executable()
	if err != nil {
		return emitBatchResult(parsed, inspector.PublicBatchError(parsed.timeout.Milliseconds(), "E_INTERNAL", "Unable to locate the running executable."), stdout)
	}
	result := supervisor.RunBatch(ctx, executable, files, supervisor.BatchRequest{Items: items, Mode: parsed.mode, Hash: parsed.hash, TimeoutMS: parsed.timeout.Milliseconds()})
	if err := ctx.Err(); err != nil {
		result = cliBatchContextFailure(err, parsed.timeout)
	}
	return emitBatchResult(parsed, result, stdout)
}

func parseInventoryArgs(args []string) (inventoryArgs, error) {
	parsed := inventoryArgs{path: ".", maxDepth: 4, timeout: 10 * time.Second}
	pathSet := false
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "--json":
			parsed.json = true
		case arg == "--max-depth" || arg == "--timeout":
			if index+1 >= len(args) {
				return parsed, fmt.Errorf("%s requires a value", arg)
			}
			index++
			if err := applyInventoryOption(&parsed, arg, args[index]); err != nil {
				return parsed, err
			}
		case strings.HasPrefix(arg, "--max-depth="):
			if err := applyInventoryOption(&parsed, "--max-depth", strings.TrimPrefix(arg, "--max-depth=")); err != nil {
				return parsed, err
			}
		case strings.HasPrefix(arg, "--timeout="):
			if err := applyInventoryOption(&parsed, "--timeout", strings.TrimPrefix(arg, "--timeout=")); err != nil {
				return parsed, err
			}
		case strings.HasPrefix(arg, "-"):
			return parsed, fmt.Errorf("unknown option %s", arg)
		default:
			if pathSet {
				return parsed, errors.New("inventory accepts at most one directory")
			}
			parsed.path, pathSet = arg, true
		}
	}
	if parsed.maxDepth < 0 || parsed.maxDepth > inspector.MaxInventoryDepth {
		return parsed, errors.New("max-depth must be between 0 and 8")
	}
	if parsed.timeout < 100*time.Millisecond || parsed.timeout > 60*time.Second {
		return parsed, errors.New("timeout must be between 100ms and 60s")
	}
	return parsed, nil
}

func applyInventoryOption(parsed *inventoryArgs, name, value string) error {
	if name == "--timeout" {
		duration, err := time.ParseDuration(value)
		if err != nil {
			return errors.New("timeout must be a duration such as 10s")
		}
		parsed.timeout = duration
		return nil
	}
	depth, err := strconv.Atoi(value)
	if err != nil {
		return errors.New("max-depth must be an integer between 0 and 8")
	}
	parsed.maxDepth = depth
	return nil
}

func runInventoryContext(parent context.Context, args []string, stdout, stderr io.Writer) int {
	parsed, err := parseInventoryArgs(args)
	if err != nil {
		if containsJSONFlag(args) {
			_ = json.NewEncoder(stdout).Encode(inspector.PublicInventoryError(parsed.path, parsed.maxDepth, 10_000, "E_USAGE", err.Error()))
		} else {
			fmt.Fprintln(stderr, err)
			fmt.Fprintln(stderr, "Run 'finspect help' for usage.")
		}
		return 2
	}
	ctx, cancel := context.WithTimeout(parent, parsed.timeout)
	defer cancel()
	absolute, err := filepath.Abs(parsed.path)
	if err != nil {
		return emitInventoryResult(parsed, inspector.PublicInventoryError(parsed.path, parsed.maxDepth, parsed.timeout.Milliseconds(), "E_FILE_ACCESS", "The inventory directory could not be resolved."), stdout)
	}
	collection, err := authority.CollectRegularFilesContext(ctx, absolute, ".", parsed.maxDepth, inspector.MaxInventoryFiles, inspector.MaxInventoryDirs)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return emitInventoryResult(parsed, cliInventoryContextFailure(parsed, err), stdout)
		}
		code, message := authority.Code(err)
		return emitInventoryResult(parsed, inspector.PublicInventoryError(parsed.path, parsed.maxDepth, parsed.timeout.Milliseconds(), code, message), stdout)
	}
	defer collection.Close()
	files := make([]*os.File, 0, len(collection.Files))
	items := make([]supervisor.CollectionItem, 0, len(collection.Files))
	for _, item := range collection.Files {
		items = append(items, supervisor.CollectionItem{Name: item.Path, DescriptorIndex: len(files)})
		files = append(files, item.File)
	}
	executable, err := os.Executable()
	if err != nil {
		return emitInventoryResult(parsed, inspector.PublicInventoryError(parsed.path, parsed.maxDepth, parsed.timeout.Milliseconds(), "E_INTERNAL", "Unable to locate the running executable."), stdout)
	}
	result := supervisor.RunInventory(ctx, executable, files, supervisor.InventoryRequest{
		Root: parsed.path, Items: items, DirectoriesScanned: collection.DirectoriesScanned,
		SymlinksSkipped: collection.SymlinksSkipped, SpecialSkipped: collection.SpecialSkipped,
		Truncated: collection.Truncated, MaxDepth: parsed.maxDepth, TimeoutMS: parsed.timeout.Milliseconds(),
	})
	if err := ctx.Err(); err != nil {
		result = cliInventoryContextFailure(parsed, err)
	}
	return emitInventoryResult(parsed, result, stdout)
}

func emitBatchResult(parsed batchArgs, result inspector.BatchResult, stdout io.Writer) int {
	if parsed.json {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		_ = encoder.Encode(result)
	} else {
		printHumanBatch(stdout, result)
	}
	if result.Status == "error" {
		return 1
	}
	return 0
}

func emitInventoryResult(parsed inventoryArgs, result inspector.InventoryResult, stdout io.Writer) int {
	if parsed.json {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		_ = encoder.Encode(result)
	} else {
		printHumanInventory(stdout, result)
	}
	if result.Status == "error" {
		return 1
	}
	return 0
}

func cliBatchContextFailure(err error, timeout time.Duration) inspector.BatchResult {
	if errors.Is(err, context.DeadlineExceeded) {
		return inspector.PublicBatchError(timeout.Milliseconds(), "E_TIMEOUT", "The complete batch inspection deadline was exceeded.")
	}
	return inspector.PublicBatchError(timeout.Milliseconds(), "E_CANCELLED", "The batch inspection was cancelled.")
}

func cliInventoryContextFailure(parsed inventoryArgs, err error) inspector.InventoryResult {
	if errors.Is(err, context.DeadlineExceeded) {
		return inspector.PublicInventoryError(parsed.path, parsed.maxDepth, parsed.timeout.Milliseconds(), "E_TIMEOUT", "The complete workspace inventory deadline was exceeded.")
	}
	return inspector.PublicInventoryError(parsed.path, parsed.maxDepth, parsed.timeout.Milliseconds(), "E_CANCELLED", "The workspace inventory was cancelled.")
}

func containsJSONFlag(args []string) bool {
	for _, arg := range args {
		if arg == "--json" {
			return true
		}
	}
	return false
}

func closeCLIFiles(files []*os.File) {
	for _, file := range files {
		_ = file.Close()
	}
}
