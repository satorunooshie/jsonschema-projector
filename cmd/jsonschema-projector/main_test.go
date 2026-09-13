package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/satorunooshie/jsonschema-projector/projector"
)

func TestVersionCommand(t *testing.T) {
	previous := version
	version = "v1.2.3"
	t.Cleanup(func() {
		version = previous
	})

	var stdout, stderr bytes.Buffer
	code := run([]string{"--version"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if stdout.String() != "jsonschema-projector v1.2.3\n" {
		t.Fatalf("unexpected stdout: %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected no stderr, got %q", stderr.String())
	}
}

func TestCommandHelpListsOptions(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"generate", "-h"}, strings.NewReader(""), &stdout, &stderr)
	if code != 2 {
		t.Fatalf("expected flag help exit code 2, got %d", code)
	}
	if !strings.Contains(stderr.String(), "usage: jsonschema-projector generate [options]") || !strings.Contains(stderr.String(), "-output") || !strings.Contains(stderr.String(), "-format") {
		t.Fatalf("command help is missing expected content:\n%s", stderr.String())
	}
}

func TestCheckFormatJSONWritesDiagnosticsToStdoutAndDoesNotWriteOutput(t *testing.T) {
	dir := t.TempDir()
	catalogPath := filepath.Join(dir, "catalog.schema.json")
	configPath := filepath.Join(dir, "projector.yaml")
	outputPath := filepath.Join(dir, "projected.schema.json")

	writeFile(t, catalogPath, `{
  "components": {
    "Text": {
      "type": "object",
      "properties": {
        "child": { "$ref": "#/components/Missing" }
      }
    }
  }
}`)
	writeFile(t, configPath, `input:
  path: catalog.schema.json
output:
  path: projected.schema.json
project:
  includeDefinitions:
    - pointer: "#/components"
  rewriteRefs:
    - from: "#/components/"
      to: "#/$defs/"
  root:
    kind: oneOf
    from: "#/components"
  checkRefs: true
`)

	var stdout, stderr bytes.Buffer
	code := run([]string{"check", "-c", configPath, "--format", "json"}, strings.NewReader(""), &stdout, &stderr)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected no stderr for check --format json, got %q", stderr.String())
	}

	var diagnostics projector.Diagnostics
	if err := json.Unmarshal(stdout.Bytes(), &diagnostics); err != nil {
		t.Fatalf("invalid diagnostics JSON: %v\n%s", err, stdout.String())
	}
	assertDiagnosticCode(t, diagnostics, projector.CodeUnresolvedRef)

	if _, err := os.Stat(outputPath); !os.IsNotExist(err) {
		t.Fatalf("check should not write output file, stat err: %v", err)
	}
}

func TestCheckFormatJSONReportsSchemaCompileDiagnostics(t *testing.T) {
	dir := t.TempDir()
	catalogPath := filepath.Join(dir, "catalog.schema.json")
	configPath := filepath.Join(dir, "projector.yaml")
	outputPath := filepath.Join(dir, "projected.schema.json")

	writeFile(t, catalogPath, `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "components": {
    "Broken": {
      "type": 123
    }
  }
}`)
	writeFile(t, configPath, `input:
  path: catalog.schema.json
output:
  path: projected.schema.json
project:
  includeDefinitions:
    - pointer: "#/components"
  checkRefs: true
validate:
  schema: true
`)

	var stdout, stderr bytes.Buffer
	code := run([]string{"check", "-c", configPath, "--format", "json"}, strings.NewReader(""), &stdout, &stderr)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected no stderr for check --format json, got %q", stderr.String())
	}

	var diagnostics projector.Diagnostics
	if err := json.Unmarshal(stdout.Bytes(), &diagnostics); err != nil {
		t.Fatalf("invalid diagnostics JSON: %v\n%s", err, stdout.String())
	}
	assertDiagnosticCode(t, diagnostics, projector.CodeSchemaCompileFailed)
	assertDiagnosticStage(t, diagnostics, "validate")

	if _, err := os.Stat(outputPath); !os.IsNotExist(err) {
		t.Fatalf("check should not write output file, stat err: %v", err)
	}
}

func TestGenerateRunsProjectValidateAndWritesGoTypes(t *testing.T) {
	dir := t.TempDir()
	catalogPath := filepath.Join(dir, "catalog.schema.json")
	configPath := filepath.Join(dir, "projector.yaml")
	projectedPath := filepath.Join(dir, "build", "projected.schema.json")
	generatedPath := filepath.Join(dir, "internal", "component", "types.gen.go")

	writeFile(t, catalogPath, `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "components": {
    "Person": {
      "type": "object",
      "properties": {
        "name": { "type": "string" }
      },
      "required": ["name"]
    }
  }
}`)
	writeFile(t, configPath, `input:
  path: catalog.schema.json
output:
  path: build/projected.schema.json
project:
  includeDefinitions:
    - pointer: "#/components"
  checkRefs: true
validate:
  schema: true
generate:
  go:
    package: component
    output: internal/component/types.gen.go
`)

	var stdout, stderr bytes.Buffer
	code := run([]string{"generate", "-c", configPath}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d\nstdout:\n%s\nstderr:\n%s", code, stdout.String(), stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected no stdout, got %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected no stderr, got %q", stderr.String())
	}
	if _, err := os.Stat(projectedPath); err != nil {
		t.Fatalf("expected projected schema to be written: %v", err)
	}

	generated, err := os.ReadFile(generatedPath)
	if err != nil {
		t.Fatalf("expected generated Go file to be written: %v", err)
	}
	generatedText := string(generated)
	for _, want := range []string{"package component", "type Person struct", "Name string"} {
		if !strings.Contains(generatedText, want) {
			t.Fatalf("generated Go source does not contain %q:\n%s", want, generatedText)
		}
	}
}

func TestRunCommandIsNotAvailable(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"run"}, strings.NewReader(""), &stdout, &stderr)
	if code != 2 {
		t.Fatalf("expected exit code 2, got %d", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected no stdout, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), `unknown command "run"`) {
		t.Fatalf("expected unknown command message, got %q", stderr.String())
	}
}

func TestGenerateStopsBeforeWritingWhenProjectedSchemaDoesNotCompile(t *testing.T) {
	dir := t.TempDir()
	catalogPath := filepath.Join(dir, "catalog.schema.json")
	configPath := filepath.Join(dir, "projector.yaml")
	projectedPath := filepath.Join(dir, "build", "projected.schema.json")
	generatedPath := filepath.Join(dir, "internal", "component", "types.gen.go")

	writeFile(t, catalogPath, `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "components": {
    "Broken": {
      "type": 123
    }
  }
}`)
	writeFile(t, configPath, `input:
  path: catalog.schema.json
output:
  path: build/projected.schema.json
project:
  includeDefinitions:
    - pointer: "#/components"
  checkRefs: true
validate:
  schema: true
generate:
  go:
    package: component
    output: internal/component/types.gen.go
`)

	var stdout, stderr bytes.Buffer
	code := run([]string{"generate", "-c", configPath, "--format", "json"}, strings.NewReader(""), &stdout, &stderr)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected no stdout, got %q", stdout.String())
	}

	var diagnostics projector.Diagnostics
	if err := json.Unmarshal(stderr.Bytes(), &diagnostics); err != nil {
		t.Fatalf("invalid diagnostics JSON: %v\n%s", err, stderr.String())
	}
	assertDiagnosticCode(t, diagnostics, projector.CodeSchemaCompileFailed)
	assertDiagnosticStage(t, diagnostics, "validate")

	if _, err := os.Stat(projectedPath); !os.IsNotExist(err) {
		t.Fatalf("generate should not write projected schema after validation failure, stat err: %v", err)
	}
	if _, err := os.Stat(generatedPath); !os.IsNotExist(err) {
		t.Fatalf("generate should not write Go types after validation failure, stat err: %v", err)
	}
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertDiagnosticCode(t *testing.T, diagnostics projector.Diagnostics, code string) {
	t.Helper()

	for _, d := range diagnostics {
		if d.Code == code {
			return
		}
	}
	t.Fatalf("expected diagnostic code %q in %v", code, diagnostics)
}

func assertDiagnosticStage(t *testing.T, diagnostics projector.Diagnostics, stage string) {
	t.Helper()

	for _, d := range diagnostics {
		if d.Stage == stage {
			return
		}
	}
	t.Fatalf("expected diagnostic stage %q in %v", stage, diagnostics)
}
