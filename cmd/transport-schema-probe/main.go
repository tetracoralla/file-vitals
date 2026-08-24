package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/tetracoralla/file-vitals/schemas"
)

const maxRequestBytes = 1024 * 1024

type request struct {
	ID                string `json:"id"`
	CapabilityID      string `json:"capabilityId"`
	CapabilityVersion string `json:"capabilityVersion"`
}

func main() {
	os.Exit(run(os.Stdin, os.Stdout, os.Stderr))
}

func run(stdin io.Reader, stdout, stderr io.Writer) int {
	source, err := io.ReadAll(io.LimitReader(stdin, maxRequestBytes+1))
	if err != nil || len(source) > maxRequestBytes {
		fmt.Fprintln(stderr, "request is unavailable or too large")
		return 2
	}
	decoder := json.NewDecoder(bytes.NewReader(source))
	decoder.DisallowUnknownFields()
	var value request
	if err := decoder.Decode(&value); err != nil {
		fmt.Fprintln(stderr, "request is invalid")
		return 2
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		fmt.Fprintln(stderr, "request contains trailing data")
		return 2
	}
	if value.ID == "" || value.CapabilityID != "org.openadam.file.inspect" || value.CapabilityVersion != "0.1.0" {
		fmt.Fprintln(stderr, "capability identity is unsupported")
		return 2
	}
	var inputSchema any
	var outputSchema any
	if err := json.Unmarshal(schemas.FileInspectInput, &inputSchema); err != nil {
		fmt.Fprintln(stderr, "live input schema is invalid")
		return 2
	}
	if err := json.Unmarshal(schemas.InspectionResult, &outputSchema); err != nil {
		fmt.Fprintln(stderr, "live output schema is invalid")
		return 2
	}
	response := map[string]any{
		"id": value.ID,
		"ok": true,
		"bindings": []any{map[string]any{
			"operationId":  "inspect",
			"transport":    "mcp-tool",
			"target":       "file_inspect",
			"inputSchema":  inputSchema,
			"outputSchema": outputSchema,
		}},
	}
	if err := json.NewEncoder(stdout).Encode(response); err != nil {
		return 2
	}
	return 0
}
