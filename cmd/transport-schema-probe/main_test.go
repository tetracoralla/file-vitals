package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestProbePublishesLiveSchemas(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(strings.NewReader(`{"id":"q","capabilityId":"org.openadam.file.inspect","capabilityVersion":"0.1.0"}`), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d stderr=%q", code, stderr.String())
	}
	var response struct {
		ID       string `json:"id"`
		OK       bool   `json:"ok"`
		Bindings []struct {
			OperationID  string         `json:"operationId"`
			Transport    string         `json:"transport"`
			Target       string         `json:"target"`
			InputSchema  map[string]any `json:"inputSchema"`
			OutputSchema map[string]any `json:"outputSchema"`
		} `json:"bindings"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("invalid response: %v", err)
	}
	if response.ID != "q" || !response.OK || len(response.Bindings) != 1 {
		t.Fatalf("unexpected response: %+v", response)
	}
	binding := response.Bindings[0]
	if binding.OperationID != "inspect" || binding.Transport != "mcp-tool" || binding.Target != "file_inspect" {
		t.Fatalf("unexpected binding: %+v", binding)
	}
	// The live schemas must be the strict published contracts.
	if binding.InputSchema["additionalProperties"] != false || binding.OutputSchema["additionalProperties"] != false {
		t.Fatalf("live schemas are not strict: %#v", binding)
	}
}

func TestProbeRejectsBadRequests(t *testing.T) {
	cases := []string{
		``,
		`not json`,
		`{"id":"q","capabilityId":"org.openadam.file.inspect","capabilityVersion":"9.9"}`,
		`{"id":"q","capabilityId":"org.other","capabilityVersion":"0.1.0"}`,
		`{"capabilityId":"org.openadam.file.inspect","capabilityVersion":"0.1.0"}`,
		`{"id":"q","capabilityId":"org.openadam.file.inspect","capabilityVersion":"0.1.0","extra":1}`,
		`{"id":"q","capabilityId":"org.openadam.file.inspect","capabilityVersion":"0.1.0"} trailing`,
	}
	for _, input := range cases {
		var stdout, stderr bytes.Buffer
		if code := run(strings.NewReader(input), &stdout, &stderr); code != 2 || stdout.Len() != 0 || stderr.Len() == 0 {
			t.Fatalf("input %q: code=%d stdout=%q stderr=%q", input, code, stdout.String(), stderr.String())
		}
	}
}
