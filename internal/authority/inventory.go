package authority

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type InventoryFile struct {
	Path string
	File *os.File
}

type InventoryCollection struct {
	Files              []InventoryFile
	DirectoriesScanned int
	SymlinksSkipped    int
	SpecialSkipped     int
	Truncated          bool
}

func (c *InventoryCollection) Close() {
	for _, item := range c.Files {
		if item.File != nil {
			_ = item.File.Close()
		}
	}
	c.Files = nil
}

func CollectRegularFilesContext(ctx context.Context, rootPath, requested string, maxDepth, maxFiles, maxDirectories int) (InventoryCollection, error) {
	collection := InventoryCollection{Files: []InventoryFile{}}
	if rootPath == "" {
		return collection, &Error{Code: "E_WORKSPACE_REQUIRED", Message: "The MCP host must grant a workspace with UFI_WORKSPACE_ROOT."}
	}
	clean, err := validateRelativePath(requested, true)
	if err != nil {
		return collection, err
	}
	if maxDepth < 0 || maxFiles <= 0 || maxDirectories <= 0 {
		return collection, &Error{Code: "E_INVALID_INPUT", Message: "Inventory limits must be positive and bounded."}
	}
	// os.OpenRoot wraps a not-directory failure in an internal error that
	// errors.Is cannot classify, so classify the shape first; OpenRoot below
	// remains the actual authority boundary.
	if info, statErr := os.Stat(rootPath); statErr == nil && !info.IsDir() {
		return collection, &Error{Code: "E_NOT_DIRECTORY", Message: "Workspace inventory requires a directory inside the granted workspace."}
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return collection, &Error{Code: "E_FILE_NOT_FOUND", Message: "The inventory directory does not exist."}
		}
		if errors.Is(err, os.ErrPermission) {
			return collection, &Error{Code: "E_FILE_ACCESS", Message: "The inventory directory could not be opened."}
		}
		return collection, &Error{Code: "E_WORKSPACE_INVALID", Message: "The granted workspace root is not available."}
	}
	defer root.Close()
	if err := validateDirectoryComponents(root, clean); err != nil {
		return collection, err
	}
	type pendingDirectory struct {
		path  string
		depth int
	}
	queue := []pendingDirectory{{path: clean, depth: 0}}
	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			collection.Close()
			return collection, err
		}
		if collection.DirectoriesScanned >= maxDirectories {
			collection.Truncated = true
			break
		}
		current := queue[0]
		queue = queue[1:]
		entries, readErr := readDirectoryStable(root, current.path)
		if readErr != nil {
			collection.Close()
			return collection, readErr
		}
		collection.DirectoriesScanned++
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				collection.Close()
				return collection, err
			}
			relative := filepath.Join(current.path, entry.Name())
			info, statErr := root.Lstat(relative)
			if statErr != nil {
				// A vanished or unreadable entry is a concurrent change, not a
				// special entry; the truncation flag carries the incompleteness.
				collection.Truncated = true
				continue
			}
			if info.Mode()&os.ModeSymlink != 0 {
				collection.SymlinksSkipped++
				continue
			}
			if info.IsDir() {
				if current.depth >= maxDepth {
					collection.Truncated = true
					continue
				}
				queue = append(queue, pendingDirectory{path: relative, depth: current.depth + 1})
				continue
			}
			if !info.Mode().IsRegular() {
				collection.SpecialSkipped++
				continue
			}
			if len(collection.Files) >= maxFiles {
				collection.Truncated = true
				queue = nil
				break
			}
			file, openErr := root.Open(relative)
			if openErr != nil {
				collection.Truncated = true
				continue
			}
			openedInfo, openedErr := file.Stat()
			afterInfo, afterErr := root.Lstat(relative)
			if openedErr != nil || afterErr != nil || afterInfo.Mode()&os.ModeSymlink != 0 || !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) || !os.SameFile(openedInfo, afterInfo) {
				_ = file.Close()
				collection.Truncated = true
				continue
			}
			collection.Files = append(collection.Files, InventoryFile{Path: filepath.ToSlash(relative), File: file})
		}
	}
	return collection, nil
}

func validateDirectoryComponents(root *os.Root, clean string) error {
	if clean == "." {
		info, err := root.Lstat(".")
		if err != nil || !info.IsDir() {
			return &Error{Code: "E_NOT_DIRECTORY", Message: "Workspace inventory requires a directory inside the granted workspace."}
		}
		return nil
	}
	prefix := ""
	for _, part := range strings.Split(filepath.ToSlash(clean), "/") {
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
				return &Error{Code: "E_FILE_NOT_FOUND", Message: "The requested inventory directory does not exist inside the granted workspace."}
			}
			return &Error{Code: "E_FILE_ACCESS", Message: "The requested inventory directory could not be opened."}
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return &Error{Code: "E_PATH_SYMLINK", Message: "Symlinks are not accepted in workspace inventory paths."}
		}
		if !info.IsDir() {
			return &Error{Code: "E_NOT_DIRECTORY", Message: "Workspace inventory requires a directory inside the granted workspace."}
		}
	}
	return nil
}

func readDirectoryStable(root *os.Root, relative string) ([]os.DirEntry, error) {
	before, err := root.Lstat(relative)
	if err != nil || before.Mode()&os.ModeSymlink != 0 || !before.IsDir() {
		return nil, &Error{Code: "E_PATH_CHANGED", Message: "An inventory directory changed during authority validation."}
	}
	directory, err := root.Open(relative)
	if err != nil {
		return nil, &Error{Code: "E_FILE_ACCESS", Message: "An inventory directory could not be opened."}
	}
	entries, readErr := directory.ReadDir(-1)
	opened, statErr := directory.Stat()
	_ = directory.Close()
	after, afterErr := root.Lstat(relative)
	if readErr != nil || statErr != nil || afterErr != nil || after.Mode()&os.ModeSymlink != 0 || !os.SameFile(before, opened) || !os.SameFile(opened, after) {
		return nil, &Error{Code: "E_PATH_CHANGED", Message: "An inventory directory changed during authority validation."}
	}
	sort.Slice(entries, func(a, b int) bool { return entries[a].Name() < entries[b].Name() })
	return entries, nil
}
