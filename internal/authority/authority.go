package authority

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// openSlots bounds concurrent authority opens. The capacity matches the MCP
// server's maxConcurrentCalls (internal/mcp) so queue admission, not file
// opening, is the only contention point for a single server process.
var openSlots = make(chan struct{}, 4)

type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Message }

func OpenRelative(rootPath, requested string) (*os.File, error) {
	return openRelative(rootPath, requested)
}

func OpenRelativeContext(ctx context.Context, rootPath, requested string) (*os.File, error) {
	select {
	case openSlots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	type outcome struct {
		file *os.File
		err  error
	}
	result := make(chan outcome)
	go func() {
		file, err := openRelative(rootPath, requested)
		defer func() { <-openSlots }()
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

func openRelative(rootPath, requested string) (*os.File, error) {
	if rootPath == "" {
		return nil, &Error{Code: "E_WORKSPACE_REQUIRED", Message: "The MCP host must grant a workspace with UFI_WORKSPACE_ROOT."}
	}
	if requested == "" || len(requested) > 4096 || !utf8.ValidString(requested) || strings.IndexByte(requested, 0) >= 0 {
		return nil, &Error{Code: "E_INVALID_PATH", Message: "Path must be a non-empty UTF-8 relative file path."}
	}
	if filepath.IsAbs(requested) || filepath.VolumeName(requested) != "" {
		return nil, &Error{Code: "E_PATH_ABSOLUTE", Message: "MCP file paths must be relative to the granted workspace."}
	}
	lower := strings.ToLower(strings.TrimSpace(requested))
	if strings.Contains(lower, "://") || strings.HasPrefix(lower, "file:") {
		return nil, &Error{Code: "E_PATH_URI", Message: "URI-like file coordinates are not accepted."}
	}
	for _, segment := range strings.FieldsFunc(requested, func(r rune) bool { return r == '/' || r == '\\' }) {
		if segment == ".." {
			return nil, &Error{Code: "E_PATH_TRAVERSAL", Message: "Parent traversal is outside the granted workspace."}
		}
	}
	clean := filepath.Clean(requested)
	if clean == "." || clean == "" || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return nil, &Error{Code: "E_INVALID_PATH", Message: "Path must identify one file inside the granted workspace."}
	}

	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return nil, &Error{Code: "E_WORKSPACE_INVALID", Message: "The granted workspace root is not available."}
	}
	defer root.Close()
	parts := strings.Split(filepath.ToSlash(clean), "/")
	prefix := ""
	var finalInfo os.FileInfo
	for index, part := range parts {
		if part == "" || part == "." {
			continue
		}
		if prefix == "" {
			prefix = part
		} else {
			prefix = filepath.Join(prefix, part)
		}
		info, err := root.Lstat(prefix)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil, &Error{Code: "E_FILE_NOT_FOUND", Message: "The requested file does not exist inside the granted workspace."}
			}
			return nil, &Error{Code: "E_FILE_ACCESS", Message: "The requested file could not be opened."}
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, &Error{Code: "E_PATH_SYMLINK", Message: "Symlinks are not accepted in MCP file paths."}
		}
		if index < len(parts)-1 && !info.IsDir() {
			return nil, &Error{Code: "E_PATH_COMPONENT", Message: "A parent path component is not a directory."}
		}
		finalInfo = info
	}
	if finalInfo == nil || !finalInfo.Mode().IsRegular() {
		return nil, &Error{Code: "E_NOT_REGULAR_FILE", Message: "Only regular files can be inspected."}
	}
	file, err := root.Open(clean)
	if err != nil {
		return nil, &Error{Code: "E_FILE_ACCESS", Message: "The requested file could not be opened."}
	}
	openedInfo, err := file.Stat()
	if err != nil || !openedInfo.Mode().IsRegular() || !os.SameFile(finalInfo, openedInfo) {
		file.Close()
		return nil, &Error{Code: "E_PATH_CHANGED", Message: "The requested file changed during authority validation."}
	}
	return file, nil
}

func Code(err error) (string, string) {
	var typed *Error
	if errors.As(err, &typed) {
		return typed.Code, typed.Message
	}
	return "E_FILE_ACCESS", fmt.Sprintf("The requested file could not be opened: %s", boundedError(err))
}

func boundedError(err error) string {
	if err == nil {
		return "unknown error"
	}
	value := strings.ToValidUTF8(err.Error(), "�")
	if len(value) > 160 {
		value = value[:160]
	}
	return value
}
