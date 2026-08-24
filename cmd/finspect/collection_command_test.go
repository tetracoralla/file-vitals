package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tetracoralla/file-vitals/internal/inspector"
	"github.com/tetracoralla/file-vitals/schemas"
)

func TestParseCollectionArguments(t *testing.T) {
	batch, err := parseBatchArgs([]string{"--deep", "--sha256", "--timeout=4s", "a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.paths) != 2 || batch.mode != inspector.ModeDeep || batch.hash != inspector.HashSHA256 || batch.timeout != 4*time.Second {
		t.Fatalf("batch arguments: %#v", batch)
	}
	if _, err := parseBatchArgs([]string{"a", "a"}); err == nil {
		t.Fatal("duplicate batch paths accepted")
	}
	inventory, err := parseInventoryArgs([]string{"--max-depth", "0", "--timeout", "3s", "nested"})
	if err != nil {
		t.Fatal(err)
	}
	if inventory.path != "nested" || inventory.maxDepth != 0 || inventory.timeout != 3*time.Second {
		t.Fatalf("inventory arguments: %#v", inventory)
	}
	if _, err := parseInventoryArgs([]string{"--max-depth=9"}); err == nil {
		t.Fatal("unbounded inventory depth accepted")
	}
}

func TestRunBatchUsesOneCollectionResultAndPreservesErrors(t *testing.T) {
	root := t.TempDir()
	ok := filepath.Join(root, "ok.txt")
	if err := os.WriteFile(ok, []byte("ok\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(root, "missing.txt")
	var stdout, stderr bytes.Buffer
	code := runContext(context.Background(), []string{"batch", ok, missing, "--quick", "--json"}, &stdout, &stderr)
	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("batch exit=%d stderr=%q", code, stderr.String())
	}
	var result inspector.BatchResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != "partial" || len(result.Items) != 2 || result.Items[0].Path != ok || result.Items[1].Result.Error == nil || result.Items[1].Result.Error.Code != "E_FILE_NOT_FOUND" {
		t.Fatalf("batch result: %#v", result)
	}
	if err := schemas.ValidateBatchResult(result); err != nil {
		t.Fatalf("batch schema: %v", err)
	}
}

func TestRunInventoryReturnsBoundedSchemaValidOverview(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{"b.txt": "b\n", "nested/a.json": `{"a":1}`} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(root, "b.txt"), filepath.Join(root, "linked.txt")); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := runContext(context.Background(), []string{"inventory", root, "--max-depth=2", "--json"}, &stdout, &stderr)
	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("inventory exit=%d stderr=%q output=%q", code, stderr.String(), stdout.String())
	}
	var result inspector.InventoryResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != "ok" || result.FilesScanned != 2 || result.SymlinksSkipped != 1 || len(result.Items) != 2 {
		t.Fatalf("inventory result: %#v", result)
	}
	if err := schemas.ValidateInventoryResult(result); err != nil {
		t.Fatalf("inventory schema: %v", err)
	}
}

func TestCollectionUsageErrorsRemainStructuredInJSONMode(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runContext(context.Background(), []string{"batch", "--json"}, &stdout, &stderr); code != 2 {
		t.Fatalf("batch usage exit: %d", code)
	}
	var batch inspector.BatchResult
	if json.Unmarshal(stdout.Bytes(), &batch) != nil || batch.Error == nil || batch.Error.Code != "E_USAGE" || stderr.Len() != 0 {
		t.Fatalf("batch usage result: %#v stderr=%q", batch, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runContext(context.Background(), []string{"inventory", "--max-depth=9", "--json"}, &stdout, &stderr); code != 2 {
		t.Fatalf("inventory usage exit: %d", code)
	}
	var inventory inspector.InventoryResult
	if json.Unmarshal(stdout.Bytes(), &inventory) != nil || inventory.Error == nil || inventory.Error.Code != "E_USAGE" || stderr.Len() != 0 {
		t.Fatalf("inventory usage result: %#v stderr=%q", inventory, stderr.String())
	}
}
