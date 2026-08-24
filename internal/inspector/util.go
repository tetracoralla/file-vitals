package inspector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/tetracoralla/file-vitals/internal/procmon"
)

var errProbeUnavailable = errors.New("probe unavailable")

func readAtMost(file *os.File, size int64, maximum int64) ([]byte, bool, error) {
	if maximum < 0 {
		return nil, false, errors.New("negative read limit")
	}
	n := size
	truncated := false
	if n > maximum {
		n = maximum
		truncated = true
	}
	if n == 0 {
		return []byte{}, truncated, nil
	}
	data := make([]byte, int(n))
	read, err := file.ReadAt(data, 0)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, truncated, err
	}
	return data[:read], truncated, nil
}

func bounded(value string, maximum int) string {
	value = strings.ToValidUTF8(value, "�")
	if len(value) <= maximum {
		return value
	}
	value = value[:maximum]
	for !utf8.ValidString(value) && len(value) > 0 {
		value = value[:len(value)-1]
	}
	return value
}

func boolPointer(value bool) *bool    { return &value }
func int64Pointer(value int64) *int64 { return &value }

func baseResult(source Source, options Options) Result {
	modified := ""
	if !source.ModTime.IsZero() {
		modified = source.ModTime.UTC().Format("2006-01-02T15:04:05.999999999Z07:00")
	}
	// A missing name stays empty rather than becoming filepath.Base's ".".
	name := ""
	if source.Name != "" {
		name = filepath.Base(source.Name)
	}
	return Result{
		SchemaVersion: "1.0",
		Status:        "unsupported",
		File: FileInfo{
			Name:        bounded(name, 256),
			SizeBytes:   source.Size,
			Extension:   bounded(strings.ToLower(filepath.Ext(source.Name)), 64),
			ModifiedUTC: modified,
		},
		Identity: Identity{
			Kind:       "unknown",
			MediaType:  "application/octet-stream",
			Format:     "Unknown",
			Confidence: "unknown",
			Candidates: []Candidate{},
			Conflicts:  []string{},
		},
		Traits:      []string{},
		Integrity:   Integrity{Readable: true},
		Diagnostics: []Diagnostic{},
		Provenance:  []Provenance{},
		Limits: AppliedLimits{
			Mode:             options.Mode,
			ResponseBytesMax: MaxResponseBytes,
			TimeoutMS:        options.Timeout.Milliseconds(),
			MemoryBytesMax:   MaxMemoryBytes,
		},
	}
}

func errorResult(source Source, options Options, code, message string) Result {
	result := baseResult(source, options)
	result.Status = "error"
	result.Integrity.Readable = false
	result.Error = &ErrorInfo{Code: code, Message: bounded(message, 512)}
	return result
}

func PublicError(name string, mode Mode, timeoutMS int64, code, message string) Result {
	if mode != ModeQuick && mode != ModeStandard && mode != ModeDeep {
		mode = ModeStandard
	}
	if timeoutMS <= 0 {
		timeoutMS = 5000
	}
	return errorResult(Source{Name: name}, Options{Mode: mode, Hash: HashNone, Timeout: time.Duration(timeoutMS) * time.Millisecond}, code, message)
}

func addDiagnostic(result *Result, code, severity, message string) {
	if len(result.Diagnostics) >= MaxDiagnosticCount {
		return
	}
	result.Diagnostics = append(result.Diagnostics, Diagnostic{
		Code: bounded(code, 64), Severity: severity, Message: bounded(message, 512),
	})
}

func addProvenance(result *Result, probe, version, status string) {
	if len(result.Provenance) >= 32 {
		return
	}
	result.Provenance = append(result.Provenance, Provenance{
		Probe: bounded(probe, 64), Version: bounded(version, 128), Status: status,
	})
}

func makePartial(result *Result) {
	if result.Status == "ok" || result.Status == "unsupported" {
		result.Status = "partial"
	}
}

func makeCorrupt(result *Result) {
	if result.Status != "error" {
		result.Status = "corrupt"
	}
}

func setParseable(result *Result, value bool) {
	result.Integrity.Parseable = boolPointer(value)
}

func fitResponse(result *Result) error {
	encoded, err := json.Marshal(result)
	if err != nil {
		return err
	}
	if len(encoded) <= MaxResponseBytes {
		return nil
	}
	if result.Archive != nil && len(result.Archive.Entries) > 0 {
		result.Archive.Entries = nil
		result.Archive.EntriesTruncated = true
		result.Limits.Truncated = true
		addDiagnostic(result, "RESPONSE_TRUNCATED", "warning", "Archive entry names were removed to fit the response budget.")
		encoded, err = json.Marshal(result)
		if err == nil && len(encoded) <= MaxResponseBytes {
			return nil
		}
	}
	return errors.New("result exceeds response budget")
}

func runProbe(ctx context.Context, file *os.File, name string, args ...string) ([]byte, []byte, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return nil, nil, errProbeUnavailable
	}
	for i := range args {
		if args[i] == "{file}" {
			args[i] = procmon.FDPath()
		}
	}
	result, err := procmon.Run(ctx, procmon.Spec{
		Name: path, Args: args, File: file, InheritProcessGroup: true,
		StdoutBytes: MaxProbeStdoutBytes,
		StderrBytes: MaxProbeStderrBytes,
		MemoryBytes: MaxMemoryBytes,
	})
	return bytes.TrimSpace(result.Stdout), bytes.TrimSpace(result.Stderr), err
}

var knownExtensionMediaTypes = map[string]string{
	".yaml": "application/yaml", ".yml": "application/yaml",
	".toml": "application/toml", ".jsonl": "application/x-ndjson",
	".csv": "text/csv", ".tsv": "text/tab-separated-values",
	".md": "text/markdown", ".svg": "image/svg+xml",
	".woff": "font/woff", ".woff2": "font/woff2", ".otf": "font/otf", ".ttf": "font/ttf",
	".mkv": "video/x-matroska", ".webm": "video/webm", ".mov": "video/quicktime",
}

func extensionMediaType(extension string) string {
	if value := knownExtensionMediaTypes[extension]; value != "" {
		return value
	}
	value := mime.TypeByExtension(extension)
	if index := strings.IndexByte(value, ';'); index >= 0 {
		value = value[:index]
	}
	return strings.TrimSpace(value)
}

func mediaCompatible(detected, extension string) bool {
	if detected == extension {
		return true
	}
	if strings.HasPrefix(detected, "text/") && strings.HasPrefix(extension, "text/") {
		return true
	}
	if detected == "application/zip" && extension == "application/epub+zip" {
		return true
	}
	if strings.HasPrefix(detected, "font/") && strings.HasPrefix(extension, "font/") && (detected == "font/sfnt" || extension == "font/sfnt") {
		return true
	}
	if detected == "application/x-elf" && (extension == "application/x-executable" || extension == "application/x-pie-executable") {
		return true
	}
	if extension == "application/x-elf" && (detected == "application/x-executable" || detected == "application/x-pie-executable") {
		return true
	}
	if (detected == "audio/wav" && extension == "audio/x-wav") || (detected == "audio/x-wav" && extension == "audio/wav") {
		return true
	}
	if (detected == "application/ogg" && extension == "audio/ogg") || (detected == "audio/ogg" && extension == "application/ogg") {
		return true
	}
	return false
}

func isOOXMLMediaType(mediaType string) bool {
	return mediaType == "application/vnd.openxmlformats-officedocument.wordprocessingml.document" ||
		mediaType == "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" ||
		mediaType == "application/vnd.openxmlformats-officedocument.presentationml.presentation"
}
