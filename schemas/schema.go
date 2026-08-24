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

//go:embed file-inspect-batch-input.schema.json
var FileInspectBatchInput []byte

//go:embed file-inspect-batch-result.schema.json
var FileInspectBatchResult []byte

//go:embed workspace-inventory-input.schema.json
var WorkspaceInventoryInput []byte

//go:embed workspace-inventory-result.schema.json
var WorkspaceInventoryResult []byte

var (
	resultSchemaOnce    sync.Once
	resultSchema        *jsonschema.Schema
	resultSchemaErr     error
	batchSchemaOnce     sync.Once
	batchSchema         *jsonschema.Schema
	batchSchemaErr      error
	inventorySchemaOnce sync.Once
	inventorySchema     *jsonschema.Schema
	inventorySchemaErr  error
)

func ValidateInspectionResult(value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return ValidateInspectionResultJSON(encoded)
}

func ValidateBatchResult(value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return ValidateBatchResultJSON(encoded)
}

func ValidateBatchResultJSON(encoded []byte) error {
	batchSchemaOnce.Do(func() {
		batchSchema, batchSchemaErr = compileSchema("file-inspect-batch-result.schema.json", FileInspectBatchResult)
	})
	return validateJSON(batchSchema, batchSchemaErr, encoded, "batch result schema is unavailable")
}

func ValidateInventoryResult(value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return ValidateInventoryResultJSON(encoded)
}

func ValidateInventoryResultJSON(encoded []byte) error {
	inventorySchemaOnce.Do(func() {
		inventorySchema, inventorySchemaErr = compileSchema("workspace-inventory-result.schema.json", WorkspaceInventoryResult)
	})
	return validateJSON(inventorySchema, inventorySchemaErr, encoded, "inventory result schema is unavailable")
}

func compileSchema(name string, source []byte) (*jsonschema.Schema, error) {
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(source))
	if err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()
	if err := compiler.AddResource(name, document); err != nil {
		return nil, err
	}
	return compiler.Compile(name)
}

func validateJSON(schema *jsonschema.Schema, schemaErr error, encoded []byte, unavailable string) error {
	if schemaErr != nil {
		return schemaErr
	}
	if schema == nil {
		return errors.New(unavailable)
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	return schema.Validate(instance)
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
