package validator

import (
	"context"
	"testing"

	"github.com/satorunooshie/jsonschema-projector/projector"
)

func TestCompileSchemaAcceptsValidProjectedSchema(t *testing.T) {
	cfg := projector.DefaultConfig().Validation
	schema := map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"$defs": map[string]any{
			"Person": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string"},
				},
				"required": []any{"name"},
			},
		},
	}

	diagnostics, err := CompileSchema(context.Background(), schema, cfg, "projected.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	if diagnostics.HasErrors() {
		t.Fatalf("unexpected diagnostics: %v", diagnostics)
	}
}

func TestCompileSchemaReportsInvalidProjectedSchema(t *testing.T) {
	cfg := projector.DefaultConfig().Validation
	schema := map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"$defs": map[string]any{
			"Broken": map[string]any{
				"type": 123,
			},
		},
	}

	diagnostics, err := CompileSchema(context.Background(), schema, cfg, "projected.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	if !diagnostics.HasErrors() {
		t.Fatalf("expected diagnostics, got %v", diagnostics)
	}
	if diagnostics[0].Code != projector.CodeSchemaCompileFailed {
		t.Fatalf("unexpected diagnostic code: %s", diagnostics[0].Code)
	}
}

func TestCompileSchemaCanBeDisabled(t *testing.T) {
	cfg := projector.DefaultConfig().Validation
	cfg.Schema = false
	schema := map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"type":    123,
	}

	diagnostics, err := CompileSchema(context.Background(), schema, cfg, "projected.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("expected no diagnostics, got %v", diagnostics)
	}
}

func TestCompileSchemaReportsUnsupportedDefaultDraft(t *testing.T) {
	cfg := projector.DefaultConfig().Validation
	cfg.DefaultDraft = "future"

	diagnostics, err := CompileSchema(context.Background(), map[string]any{}, cfg, "projected.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	if !diagnostics.HasErrors() {
		t.Fatalf("expected diagnostics, got %v", diagnostics)
	}
	if diagnostics[0].Code != projector.CodeInvalidValidateConfig {
		t.Fatalf("unexpected diagnostic code: %s", diagnostics[0].Code)
	}
}
