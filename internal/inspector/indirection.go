package inspector

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

const maxGitLFSPointerBytes = 1024

func inspectIndirection(data []byte, size int64) *Indirection {
	if size <= 0 || size >= maxGitLFSPointerBytes || int64(len(data)) < size {
		return nil
	}
	raw := data[:size]
	if !utf8.Valid(raw) {
		return nil
	}
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if len(lines) < 3 || lines[0] != "version https://git-lfs.github.com/spec/v1" {
		return nil
	}
	extensions := map[int]struct{}{}
	for _, line := range lines[1 : len(lines)-2] {
		key, value, ok := strings.Cut(line, " ")
		if !ok || strings.Contains(value, " ") || !validGitLFSExtensionKey(key, extensions) || !strings.HasPrefix(value, "sha256:") || len(value) != len("sha256:")+64 || !lowerHex(strings.TrimPrefix(value, "sha256:")) {
			return nil
		}
	}
	oidLine := lines[len(lines)-2]
	if !strings.HasPrefix(oidLine, "oid sha256:") {
		return nil
	}
	oid := strings.TrimPrefix(oidLine, "oid sha256:")
	if len(oid) != 64 || !lowerHex(oid) {
		return nil
	}
	sizeLine := lines[len(lines)-1]
	if !strings.HasPrefix(sizeLine, "size ") {
		return nil
	}
	declared, err := strconv.ParseInt(strings.TrimPrefix(sizeLine, "size "), 10, 64)
	if err != nil || declared < 0 {
		return nil
	}
	return &Indirection{Kind: "git_lfs_pointer", OID: "sha256:" + oid, DeclaredSize: declared}
}

func validGitLFSExtensionKey(key string, priorities map[int]struct{}) bool {
	parts := strings.SplitN(key, "-", 3)
	if len(parts) != 3 || parts[0] != "ext" || parts[1] == "" || parts[2] == "" {
		return false
	}
	priority, err := strconv.Atoi(parts[1])
	if err != nil || priority < 0 {
		return false
	}
	if _, exists := priorities[priority]; exists {
		return false
	}
	for _, character := range parts[2] {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '_' {
			continue
		}
		return false
	}
	priorities[priority] = struct{}{}
	return true
}

func lowerHex(value string) bool {
	for index := 0; index < len(value); index++ {
		if value[index] >= '0' && value[index] <= '9' || value[index] >= 'a' && value[index] <= 'f' {
			continue
		}
		return false
	}
	return true
}
