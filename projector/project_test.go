package projector

import (
	"context"
	"testing"
)

func TestProjectSchemaMergesRewritesSynthesizesRootAndPreservesUnknownKeywords(t *testing.T) {
	doc := map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"$id":     "https://example.com/input.schema.json",
		"title":   "Input",
		"$defs": map[string]any{
			"Shared": map[string]any{"type": "string"},
		},
		"functions": map[string]any{
			"and": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"left": map[string]any{"$ref": "#/components/ChoiceList"},
				},
			},
		},
		"components": map[string]any{
			"ImageList": map[string]any{"type": "object"},
			"ChoiceList": map[string]any{
				"type":      "object",
				"x-go-name": "ChoiceList",
				"allOf": []any{
					map[string]any{"$ref": "#/functions/and"},
				},
			},
		},
	}
	cfg := ProjectConfig{
		ID:    "https://example.com/component.schema.json",
		Title: "Component",
		IncludeDefinitions: []DefinitionSource{
			{Pointer: "#/$defs"},
			{Pointer: "#/functions"},
			{Pointer: "#/components"},
		},
		RewriteRefs: []RefRewrite{
			{From: "#/functions/", To: "#/$defs/"},
			{From: "#/components/", To: "#/$defs/"},
		},
		Root:      &RootSynthesis{Kind: "oneOf", From: "#/components"},
		CheckRefs: true,
	}

	result, err := ProjectSchema(context.Background(), doc, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if result.Diagnostics.HasErrors() {
		t.Fatalf("unexpected diagnostics: %v", result.Diagnostics)
	}

	if result.Schema["$id"] != "https://example.com/component.schema.json" {
		t.Fatalf("unexpected $id: %v", result.Schema["$id"])
	}
	if result.Schema["title"] != "Component" {
		t.Fatalf("unexpected title: %v", result.Schema["title"])
	}

	oneOf := mustSlice(t, result.Schema["oneOf"])
	if got := mustMap(t, oneOf[0])["$ref"]; got != "#/$defs/ChoiceList" {
		t.Fatalf("unexpected first oneOf ref: %v", got)
	}
	if got := mustMap(t, oneOf[1])["$ref"]; got != "#/$defs/ImageList" {
		t.Fatalf("unexpected second oneOf ref: %v", got)
	}

	defs := mustMap(t, result.Schema["$defs"])
	if _, ok := defs["Shared"]; !ok {
		t.Fatal("expected Shared definition")
	}
	andDef := mustMap(t, defs["and"])
	left := mustMap(t, mustMap(t, andDef["properties"])["left"])
	if left["$ref"] != "#/$defs/ChoiceList" {
		t.Fatalf("expected rewritten component ref, got %v", left["$ref"])
	}

	choice := mustMap(t, defs["ChoiceList"])
	if choice["x-go-name"] != "ChoiceList" {
		t.Fatalf("expected unknown keyword to be preserved, got %v", choice["x-go-name"])
	}
	allOf := mustSlice(t, choice["allOf"])
	if got := mustMap(t, allOf[0])["$ref"]; got != "#/$defs/and" {
		t.Fatalf("expected rewritten function ref, got %v", got)
	}

	originalLeft := mustMap(t, mustMap(t, mustMap(t, mustMap(t, doc["functions"])["and"])["properties"])["left"])
	if originalLeft["$ref"] != "#/components/ChoiceList" {
		t.Fatalf("project mutated input document: %v", originalLeft["$ref"])
	}
}

func TestProjectSchemaReportsDuplicateDefinitionsAndUnresolvedRefs(t *testing.T) {
	doc := map[string]any{
		"$defs": map[string]any{
			"Text": map[string]any{"from": "defs"},
		},
		"components": map[string]any{
			"Text":   map[string]any{"from": "components"},
			"Widget": map[string]any{"$ref": "#/components/Missing"},
		},
	}
	cfg := ProjectConfig{
		IncludeDefinitions: []DefinitionSource{
			{Pointer: "#/$defs"},
			{Pointer: "#/components"},
		},
		RewriteRefs: []RefRewrite{
			{From: "#/components/", To: "#/$defs/"},
		},
		CheckRefs: true,
	}

	result, err := ProjectSchema(context.Background(), doc, cfg)
	if err != nil {
		t.Fatal(err)
	}

	assertDiagnosticCode(t, result.Diagnostics, CodeDuplicateDefinition)
	assertDiagnosticCode(t, result.Diagnostics, CodeUnresolvedRef)

	text := mustMap(t, mustMap(t, result.Schema["$defs"])["Text"])
	if text["from"] != "defs" {
		t.Fatalf("duplicate definition should not overwrite by default, got %v", text["from"])
	}
}

func TestProjectSchemaDoesNotCopyInputIDWhenProjectIDIsUnset(t *testing.T) {
	doc := map[string]any{
		"$id":   "https://example.com/input.schema.json",
		"$defs": map[string]any{},
	}
	cfg := ProjectConfig{
		IncludeDefinitions: []DefinitionSource{{Pointer: "#/$defs"}},
	}

	result, err := ProjectSchema(context.Background(), doc, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := result.Schema["$id"]; ok {
		t.Fatalf("projected schema should not copy input $id: %v", result.Schema["$id"])
	}
}

func TestProjectSchemaPreservesExplicitRootNamesOrder(t *testing.T) {
	doc := map[string]any{
		"components": map[string]any{
			"A": map[string]any{},
			"B": map[string]any{},
		},
	}
	cfg := ProjectConfig{
		IncludeDefinitions: []DefinitionSource{{Pointer: "#/components"}},
		Root: &RootSynthesis{
			Kind:  "oneOf",
			From:  "#/components",
			Names: []string{"B", "A"},
		},
	}

	result, err := ProjectSchema(context.Background(), doc, cfg)
	if err != nil {
		t.Fatal(err)
	}

	oneOf := mustSlice(t, result.Schema["oneOf"])
	if got := mustMap(t, oneOf[0])["$ref"]; got != "#/$defs/B" {
		t.Fatalf("unexpected first oneOf ref: %v", got)
	}
	if got := mustMap(t, oneOf[1])["$ref"]; got != "#/$defs/A" {
		t.Fatalf("unexpected second oneOf ref: %v", got)
	}
}

func assertDiagnosticCode(t *testing.T, diagnostics Diagnostics, code string) {
	t.Helper()

	for _, d := range diagnostics {
		if d.Code == code {
			return
		}
	}
	t.Fatalf("expected diagnostic code %q in %v", code, diagnostics)
}

func mustMap(t *testing.T, value any) map[string]any {
	t.Helper()

	m, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", value)
	}
	return m
}

func mustSlice(t *testing.T, value any) []any {
	t.Helper()

	s, ok := value.([]any)
	if !ok {
		t.Fatalf("expected []any, got %T", value)
	}
	return s
}
