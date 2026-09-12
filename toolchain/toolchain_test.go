package toolchain

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/satorunooshie/jsonschema-projector/projector"
)

func TestCheckStopsBeforeValidateWhenProjectionFails(t *testing.T) {
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "catalog.schema.json")
	if err := os.WriteFile(schemaPath, []byte(`{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "components": {
    "Widget": { "$ref": "#/components/Missing" }
  }
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := projector.DefaultConfig()
	cfg.Input.Path = schemaPath
	cfg.Output.Path = filepath.Join(dir, "projected.schema.json")
	cfg.Project.IncludeDefinitions = []projector.DefinitionSource{{Pointer: "#/components"}}
	cfg.Project.RewriteRefs = []projector.RefRewrite{{From: "#/components/", To: "#/$defs/"}}

	result, err := Check(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Diagnostics.HasErrors() {
		t.Fatalf("expected projection diagnostics, got %v", result.Diagnostics)
	}
	for _, d := range result.Diagnostics {
		if d.Stage != StageProject {
			t.Fatalf("expected only project diagnostics before validate, got %v", result.Diagnostics)
		}
	}
}

func TestGenerateRequiresGenerateConfigBeforeProjecting(t *testing.T) {
	cfg := projector.DefaultConfig()

	var stdout bytes.Buffer
	result, err := Generate(context.Background(), cfg, &stdout)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Diagnostics.HasErrors() {
		t.Fatalf("expected generate diagnostics, got %v", result.Diagnostics)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Stage != StageGenerate {
		t.Fatalf("unexpected diagnostics: %v", result.Diagnostics)
	}
}
