// Package capabilities validates the portable Capability projection exposed by
// File Vitals' conformance adapter.
package capabilities

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed schemas/file.inspect.input.schema.json
var inputSchemaBytes []byte

//go:embed schemas/file.inspect.output.schema.json
var outputSchemaBytes []byte

var (
	compiledOnce sync.Once
	inputSchema  *jsonschema.Schema
	outputSchema *jsonschema.Schema
	compileErr   error
)

func compileOnce() error {
	compiledOnce.Do(func() {
		input, inputErr := compile(inputSchemaBytes)
		if inputErr != nil {
			compileErr = inputErr
			return
		}
		output, outputErr := compile(outputSchemaBytes)
		if outputErr != nil {
			compileErr = outputErr
			return
		}
		inputSchema, outputSchema = input, output
	})
	return compileErr
}

func compile(schemaBytes []byte) (*jsonschema.Schema, error) {
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaBytes))
	if err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()
	if err := compiler.AddResource("contract.json", document); err != nil {
		return nil, err
	}
	return compiler.Compile("contract.json")
}

// validate reads the compiled schema through schemaOf only after compileOnce
// has run: passing the schema directly would evaluate it before compilation.
func validate(schemaOf func() *jsonschema.Schema, value any) error {
	if err := compileOnce(); err != nil {
		return err
	}
	compiled := schemaOf()
	if compiled == nil {
		return errors.New("portable contract schema is unavailable")
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	if err := compiled.Validate(instance); err != nil {
		return fmt.Errorf("portable contract validation failed: %w", err)
	}
	return nil
}

func ValidateInput(value any) error {
	return validate(func() *jsonschema.Schema { return inputSchema }, value)
}

func ValidateOutput(value any) error {
	return validate(func() *jsonschema.Schema { return outputSchema }, value)
}
