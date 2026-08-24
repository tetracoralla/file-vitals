package capabilities

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func canonicalJSON(value any) []byte {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return encoded
}

func digestDocument(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(canonicalJSON(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func TestProviderManifestSchemaDigestsAreCurrent(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "capabilities", "provider.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Implementations []struct {
			Bindings []struct {
				ContractSchemaDigests  map[string]string `json:"contractSchemaDigests"`
				TransportSchemaDigests map[string]string `json:"transportSchemaDigests"`
			} `json:"bindings"`
		} `json:"implementations"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	binding := manifest.Implementations[0].Bindings[0]
	expected := map[string]string{
		"contract.input":   digestDocument(t, filepath.Join(root, "capabilities", "schemas", "file.inspect.input.schema.json")),
		"contract.output":  digestDocument(t, filepath.Join(root, "capabilities", "schemas", "file.inspect.output.schema.json")),
		"transport.input":  digestDocument(t, filepath.Join(root, "schemas", "file-inspect-input.schema.json")),
		"transport.output": digestDocument(t, filepath.Join(root, "schemas", "inspection-result.schema.json")),
	}
	actual := map[string]string{
		"contract.input":   binding.ContractSchemaDigests["input"],
		"contract.output":  binding.ContractSchemaDigests["output"],
		"transport.input":  binding.TransportSchemaDigests["input"],
		"transport.output": binding.TransportSchemaDigests["output"],
	}
	for name, want := range expected {
		if actual[name] != want {
			t.Fatalf("%s digest stale: got %s want %s", name, actual[name], want)
		}
	}
}
