package extract

import (
	"strings"
	"testing"
)

func TestValidateAgainstSchema_OK(t *testing.T) {
	schema := []byte(`{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}`)
	doc := []byte(`{"name":"alice"}`)
	if err := ValidateAgainstSchema(schema, doc); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}

func TestValidateAgainstSchema_MissingRequired(t *testing.T) {
	schema := []byte(`{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}`)
	doc := []byte(`{}`)
	err := ValidateAgainstSchema(schema, doc)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "name") {
		t.Errorf("error should mention missing field: %v", err)
	}
}

func TestValidateAgainstSchema_BadSchema(t *testing.T) {
	err := ValidateAgainstSchema([]byte(`not json`), []byte(`{}`))
	if err == nil {
		t.Fatal("expected error for invalid schema")
	}
}
