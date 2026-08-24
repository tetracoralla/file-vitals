package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/tetracoralla/file-vitals/internal/inspector"
	"github.com/tetracoralla/file-vitals/internal/procmon"
	"github.com/tetracoralla/file-vitals/internal/supervisor"
	"github.com/tetracoralla/file-vitals/internal/version"
	"github.com/tetracoralla/file-vitals/schemas"
)

type doctorProbe struct {
	Name      string `json:"name"`
	Available bool   `json:"available"`
	Required  bool   `json:"required,omitempty"`
	Path      string `json:"path,omitempty"`
	Version   string `json:"version,omitempty"`
}

func runDoctor(args []string, stdout, stderr io.Writer) int {
	jsonOutput := len(args) == 1 && args[0] == "--json"
	if len(args) > 1 || len(args) == 1 && !jsonOutput {
		fmt.Fprintln(stderr, "doctor accepts only --json")
		return 2
	}
	probes := []doctorProbe{
		{Name: "internal-signatures", Available: true, Required: true, Version: version.Version},
		probeResultSchema(),
		probeWorkerSelfTest(),
		probeVersion("file", "--version"),
		probeVersion("ffprobe", "-version"),
		probeVersion("pdfinfo", "-v"),
		probeVersion("fc-scan", "--version"),
	}
	failedRequired := false
	for _, probe := range probes {
		if probe.Required && !probe.Available {
			failedRequired = true
		}
	}
	if jsonOutput {
		_ = json.NewEncoder(stdout).Encode(map[string]any{"version": version.Version, "probes": probes})
		if failedRequired {
			return 1
		}
		return 0
	}
	for _, probe := range probes {
		status := "unavailable"
		if probe.Available {
			status = "available"
		}
		fmt.Fprintf(stdout, "%s: %s", probe.Name, status)
		if probe.Version != "" {
			fmt.Fprintf(stdout, " · %s", probe.Version)
		}
		if probe.Required {
			fmt.Fprint(stdout, " · required")
		}
		fmt.Fprintln(stdout)
	}
	if failedRequired {
		fmt.Fprintln(stderr, "A required probe is unavailable; inspection results cannot be trusted.")
		return 1
	}
	return 0
}

// probeResultSchema proves the embedded result schema both compiles and accepts
// a published error result. A schema that fails to compile would silently turn
// every MCP call into E_WORKER_PROTOCOL.
func probeResultSchema() doctorProbe {
	if err := schemas.ValidateInspectionResult(inspector.PublicError("doctor.txt", inspector.ModeQuick, 5000, "E_DOCTOR", "schema self-check")); err != nil {
		return doctorProbe{Name: "result-schema", Required: true}
	}
	if err := schemas.ValidateBatchResult(inspector.PublicBatchError(5000, "E_DOCTOR", "schema self-check")); err != nil {
		return doctorProbe{Name: "result-schema", Required: true}
	}
	if err := schemas.ValidateInventoryResult(inspector.PublicInventoryError(".", 4, 5000, "E_DOCTOR", "schema self-check")); err != nil {
		return doctorProbe{Name: "result-schema", Required: true}
	}
	return doctorProbe{Name: "result-schema", Available: true, Required: true}
}

// probeWorkerSelfTest exercises the real supervisor-to-worker path (executable
// relocation, exec permission, fd-3 inheritance) on one small file, because a
// broken deployed bundle would otherwise surface only as per-call failures.
func probeWorkerSelfTest() doctorProbe {
	executable, err := os.Executable()
	if err != nil {
		return doctorProbe{Name: "worker-self-test", Required: true}
	}
	directory, err := os.MkdirTemp("", "ufi-doctor-")
	if err != nil {
		return doctorProbe{Name: "worker-self-test", Required: true}
	}
	defer os.RemoveAll(directory)
	path := filepath.Join(directory, "doctor.txt")
	if err := os.WriteFile(path, []byte("doctor\n"), 0o600); err != nil {
		return doctorProbe{Name: "worker-self-test", Required: true}
	}
	file, err := os.Open(path)
	if err != nil {
		return doctorProbe{Name: "worker-self-test", Required: true}
	}
	defer file.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result := supervisor.Run(ctx, executable, file, supervisor.Request{Name: "doctor.txt", Mode: inspector.ModeQuick, Hash: inspector.HashNone, TimeoutMS: 5000})
	if result.Status == "ok" {
		return doctorProbe{Name: "worker-self-test", Available: true, Required: true, Path: boundedCLI(executable, 160)}
	}
	return doctorProbe{Name: "worker-self-test", Required: true, Path: boundedCLI(executable, 160)}
}

func probeVersion(name string, args ...string) doctorProbe {
	path, err := exec.LookPath(name)
	if err != nil {
		return doctorProbe{Name: name}
	}
	return probePathVersion(name, path, args...)
}

func probePathVersion(name, path string, args ...string) doctorProbe {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result, err := procmon.Run(ctx, procmon.Spec{Name: path, Args: args, StdoutBytes: 16 * 1024, StderrBytes: 16 * 1024, MemoryBytes: inspector.MaxMemoryBytes})
	if err != nil {
		return doctorProbe{Name: name, Path: path}
	}
	combined := bytes.TrimSpace(result.Stdout)
	if len(combined) == 0 {
		combined = bytes.TrimSpace(result.Stderr)
	}
	line := string(combined)
	if index := strings.IndexByte(line, '\n'); index >= 0 {
		line = line[:index]
	}
	return doctorProbe{Name: name, Available: true, Path: path, Version: boundedCLI(line, 160)}
}

func boundedCLI(value string, maximum int) string {
	value = strings.ToValidUTF8(value, "�")
	if len(value) > maximum {
		value = value[:maximum]
		for !utf8.ValidString(value) && len(value) > 0 {
			value = value[:len(value)-1]
		}
	}
	return value
}

func runSchema(args []string, stdout, stderr io.Writer) int {
	which := "output"
	if len(args) == 1 {
		which = args[0]
	} else if len(args) > 1 {
		fmt.Fprintln(stderr, "schema accepts input, output, batch-input, batch-output, inventory-input, or inventory-output")
		return 2
	}
	var data []byte
	switch which {
	case "input":
		data = schemas.FileInspectInput
	case "output", "result":
		data = schemas.InspectionResult
	case "batch-input":
		data = schemas.FileInspectBatchInput
	case "batch-output":
		data = schemas.FileInspectBatchResult
	case "inventory-input":
		data = schemas.WorkspaceInventoryInput
	case "inventory-output":
		data = schemas.WorkspaceInventoryResult
	default:
		fmt.Fprintln(stderr, "schema accepts input, output, batch-input, batch-output, inventory-input, or inventory-output")
		return 2
	}
	var value any
	if json.Unmarshal(data, &value) != nil {
		fmt.Fprintln(stderr, "Embedded schema is invalid.")
		return 1
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(value)
	return 0
}
