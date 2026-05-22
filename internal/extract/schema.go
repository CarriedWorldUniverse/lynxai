// Package extract runs schema-driven LLM extractions against fetched pages.
package extract

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// ValidateAgainstSchema checks that doc (JSON) conforms to schema (JSON Schema).
func ValidateAgainstSchema(schema, doc []byte) error {
	schemaDoc, err := jsonschema.UnmarshalJSON(bytes.NewReader(schema))
	if err != nil {
		return fmt.Errorf("parse schema: %w", err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("inline://schema", schemaDoc); err != nil {
		return fmt.Errorf("add schema: %w", err)
	}
	sch, err := c.Compile("inline://schema")
	if err != nil {
		return fmt.Errorf("compile schema: %w", err)
	}
	var v any
	if err := json.Unmarshal(doc, &v); err != nil {
		return fmt.Errorf("parse doc: %w", err)
	}
	if err := sch.Validate(v); err != nil {
		return fmt.Errorf("validate: %w", err)
	}
	return nil
}
