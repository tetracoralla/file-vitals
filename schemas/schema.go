// Package schemas exposes the exact JSON Schemas published by the MCP adapter.
package schemas

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"sync"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed file-inspect-input.schema.json
var FileInspectInput []byte

//go:embed inspection-result.schema.json
var InspectionResult []byte

var (
	resultSchemaOnce sync.Once
	resultSchema     *jsonschema.Schema
	resultSchemaErr  error
)

func ValidateInspectionResult(value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return ValidateInspectionResultJSON(encoded)
}

// ValidateInspectionResultJSON validates an already-encoded result document so
// callers that hold the encoded bytes (worker stdout, a marshaled envelope) do
// not pay for another marshal on every validation.
func ValidateInspectionResultJSON(encoded []byte) error {
	resultSchemaOnce.Do(func() {
		document, err := jsonschema.UnmarshalJSON(bytes.NewReader(InspectionResult))
		if err != nil {
			resultSchemaErr = err
			return
		}
		compiler := jsonschema.NewCompiler()
		compiler.AssertFormat()
		if err := compiler.AddResource("inspection-result.schema.json", document); err != nil {
			resultSchemaErr = err
			return
		}
		resultSchema, resultSchemaErr = compiler.Compile("inspection-result.schema.json")
	})
	if resultSchemaErr != nil {
		return resultSchemaErr
	}
	if resultSchema == nil {
		return errors.New("inspection result schema is unavailable")
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	return resultSchema.Validate(instance)
}
