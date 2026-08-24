package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/tetracoralla/file-vitals/internal/inspector"
	"github.com/tetracoralla/file-vitals/internal/supervisor"
)

type inspectArgs struct {
	path           string
	mode           inspector.Mode
	hash           inspector.HashMode
	expectedSHA256 string
	json           bool
	timeout        time.Duration
}

var errCLINotRegular = errors.New("CLI path is not a regular file")

func parseInspectArgs(args []string) (inspectArgs, error) {
	parsed := inspectArgs{mode: inspector.ModeStandard, hash: inspector.HashNone, timeout: 10 * time.Second}
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
		case arg == "--mode" || arg == "--hash" || arg == "--timeout" || arg == "--expect-sha256":
			if index+1 >= len(args) {
				return parsed, fmt.Errorf("%s requires a value", arg)
			}
			index++
			value := args[index]
			switch arg {
			case "--mode":
				parsed.mode = inspector.Mode(value)
			case "--hash":
				parsed.hash = inspector.HashMode(value)
			case "--timeout":
				duration, err := time.ParseDuration(value)
				if err != nil {
					return parsed, errors.New("timeout must be a duration such as 5s")
				}
				parsed.timeout = duration
			case "--expect-sha256":
				parsed.expectedSHA256 = strings.ToLower(value)
			}
		case strings.HasPrefix(arg, "--mode="):
			parsed.mode = inspector.Mode(strings.TrimPrefix(arg, "--mode="))
		case strings.HasPrefix(arg, "--hash="):
			parsed.hash = inspector.HashMode(strings.TrimPrefix(arg, "--hash="))
		case strings.HasPrefix(arg, "--timeout="):
			duration, err := time.ParseDuration(strings.TrimPrefix(arg, "--timeout="))
			if err != nil {
				return parsed, errors.New("timeout must be a duration such as 5s")
			}
			parsed.timeout = duration
		case strings.HasPrefix(arg, "--expect-sha256="):
			parsed.expectedSHA256 = strings.ToLower(strings.TrimPrefix(arg, "--expect-sha256="))
		case strings.HasPrefix(arg, "-"):
			return parsed, fmt.Errorf("unknown option %s", arg)
		default:
			if parsed.path != "" {
				return parsed, errors.New("inspect accepts exactly one file")
			}
			parsed.path = arg
		}
	}
	if parsed.path == "" {
		return parsed, errors.New("a file path is required")
	}
	if parsed.mode != inspector.ModeQuick && parsed.mode != inspector.ModeStandard && parsed.mode != inspector.ModeDeep {
		return parsed, errors.New("mode must be quick, standard, or deep")
	}
	if parsed.hash != inspector.HashNone && parsed.hash != inspector.HashSHA256 {
		return parsed, errors.New("hash must be none or sha256")
	}
	if parsed.expectedSHA256 != "" && !validSHA256(parsed.expectedSHA256) {
		return parsed, errors.New("expected SHA-256 must contain exactly 64 hexadecimal characters")
	}
	if parsed.timeout < 100*time.Millisecond || parsed.timeout > 60*time.Second {
		return parsed, errors.New("timeout must be between 100ms and 60s")
	}
	return parsed, nil
}

func runInspect(args []string, stdout, stderr io.Writer) int {
	return runInspectContext(context.Background(), args, stdout, stderr)
}

func runInspectContext(parent context.Context, args []string, stdout, stderr io.Writer) int {
	jsonRequested := false
	for _, arg := range args {
		if arg == "--json" {
			jsonRequested = true
			break
		}
	}
	parsed, err := parseInspectArgs(args)
	if err != nil {
		if jsonRequested {
			parsed.json = true
			result := inspector.PublicError(parsed.path, parsed.mode, parsed.timeout.Milliseconds(), "E_USAGE", err.Error())
			_ = json.NewEncoder(stdout).Encode(result)
		} else {
			fmt.Fprintln(stderr, err)
			fmt.Fprintln(stderr, "Run 'finspect help' for usage.")
		}
		return 2
	}
	ctx, cancel := context.WithTimeout(parent, parsed.timeout)
	defer cancel()
	file, err := openCLIFile(ctx, parsed.path)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			result := inspector.PublicError(parsed.path, parsed.mode, parsed.timeout.Milliseconds(), "E_TIMEOUT", "The inspection deadline was exceeded while opening the file.")
			return emitResult(parsed, result, stdout)
		}
		if errors.Is(err, context.Canceled) {
			result := inspector.PublicError(parsed.path, parsed.mode, parsed.timeout.Milliseconds(), "E_CANCELLED", "The inspection was cancelled while opening the file.")
			return emitResult(parsed, result, stdout)
		}
		if errors.Is(err, errCLINotRegular) {
			result := inspector.PublicError(parsed.path, parsed.mode, parsed.timeout.Milliseconds(), "E_NOT_REGULAR_FILE", "Only regular files can be inspected.")
			return emitResult(parsed, result, stdout)
		}
		code := "E_FILE_ACCESS"
		if errors.Is(err, os.ErrNotExist) {
			code = "E_FILE_NOT_FOUND"
		}
		result := inspector.PublicError(parsed.path, parsed.mode, parsed.timeout.Milliseconds(), code, "The file could not be opened.")
		return emitResult(parsed, result, stdout)
	}
	defer file.Close()
	executable, err := os.Executable()
	if err != nil {
		result := inspector.PublicError(parsed.path, parsed.mode, parsed.timeout.Milliseconds(), "E_INTERNAL", "Unable to locate the running executable.")
		return emitResult(parsed, result, stdout)
	}
	result := supervisor.Run(ctx, executable, file, supervisor.Request{Name: parsed.path, Mode: parsed.mode, Hash: parsed.hash, ExpectedSHA256: parsed.expectedSHA256, TimeoutMS: parsed.timeout.Milliseconds()})
	return emitResult(parsed, result, stdout)
}

func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for index := 0; index < len(value); index++ {
		if value[index] >= '0' && value[index] <= '9' || value[index] >= 'a' && value[index] <= 'f' {
			continue
		}
		return false
	}
	return true
}

func openCLIFile(ctx context.Context, path string) (*os.File, error) {
	// Reject directories, FIFOs, and devices before opening: opening a read
	// FIFO with no writer would block until the whole deadline. CLI paths are
	// deliberate human paths, so a stat-then-open race is acceptable here; the
	// post-open regular-file check below still stands.
	if info, err := os.Stat(path); err == nil && !info.Mode().IsRegular() {
		return nil, errCLINotRegular
	}
	type outcome struct {
		file *os.File
		err  error
	}
	result := make(chan outcome)
	go func() {
		file, err := os.Open(path)
		if err == nil {
			stat, statErr := file.Stat()
			switch {
			case statErr != nil:
				err = statErr
			case !stat.Mode().IsRegular():
				err = errCLINotRegular
			}
			if err != nil {
				_ = file.Close()
				file = nil
			}
		}
		select {
		case result <- outcome{file: file, err: err}:
		case <-ctx.Done():
			if file != nil {
				_ = file.Close()
			}
		}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case opened := <-result:
		if err := ctx.Err(); err != nil {
			if opened.file != nil {
				_ = opened.file.Close()
			}
			return nil, err
		}
		return opened.file, opened.err
	}
}

func emitResult(parsed inspectArgs, result inspector.Result, stdout io.Writer) int {
	if parsed.json {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		_ = encoder.Encode(result)
	} else {
		printHuman(stdout, result)
	}
	if result.Status == "error" {
		return 1
	}
	if result.Status == "corrupt" {
		return 3
	}
	return 0
}

func formatBytes(value int64) string {
	if value < 1024 {
		return fmt.Sprintf("%d B", value)
	}
	units := []string{"KiB", "MiB", "GiB", "TiB"}
	scaled := float64(value)
	unit := "B"
	for _, candidate := range units {
		scaled /= 1024
		unit = candidate
		if scaled < 1024 {
			break
		}
	}
	return strconv.FormatFloat(scaled, 'f', 1, 64) + " " + unit
}
