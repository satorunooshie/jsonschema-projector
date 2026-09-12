package validator

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/satorunooshie/jsonschema-projector/projector"
)

func TestDraftFixturesCompile(t *testing.T) {
	for _, draft := range []string{"04", "06", "07", "2019-09", "2020-12"} {
		t.Run(draft, func(t *testing.T) {
			schema := readFixture(t, "draft-"+draft+".json")
			diagnostics, err := CompileSchema(context.Background(), schema, projector.ValidationConfig{Schema: true}, "draft-"+draft+".json")
			if err != nil || diagnostics.HasErrors() {
				t.Fatalf("draft %s did not compile: err=%v diagnostics=%v", draft, err, diagnostics)
			}
		})
	}
}

func TestCompositionFixtureCompiles(t *testing.T) {
	schema := readFixture(t, "composition-2020-12.json")
	diagnostics, err := CompileSchema(context.Background(), schema, projector.ValidationConfig{Schema: true}, "composition-2020-12.json")
	if err != nil || diagnostics.HasErrors() {
		t.Fatalf("composition fixture did not compile: err=%v diagnostics=%v", err, diagnostics)
	}
}

func TestExternalReferenceFixtureCompiles(t *testing.T) {
	root := readFixture(t, "external-root.json")
	rootPath := filepath.Join(fixtureDir(t), "external-root.json")
	diagnostics, err := CompileSchema(context.Background(), root, projector.ValidationConfig{Schema: true}, rootPath)
	if err != nil || diagnostics.HasErrors() {
		t.Fatalf("external reference did not compile: err=%v diagnostics=%v", err, diagnostics)
	}
}

func TestReferenceFixturesCompile(t *testing.T) {
	for _, name := range []string{"uri-fragment.json", "circular.json"} {
		t.Run(name, func(t *testing.T) {
			schema := readFixture(t, name)
			diagnostics, err := CompileSchema(context.Background(), schema, projector.ValidationConfig{Schema: true}, name)
			if err != nil || diagnostics.HasErrors() {
				t.Fatalf("reference fixture did not compile: err=%v diagnostics=%v", err, diagnostics)
			}
		})
	}
}

func TestConfigurationSchemaCompiles(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "schema", "projector.config.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	diagnostics, err := CompileSchema(context.Background(), schema, projector.ValidationConfig{Schema: true}, "projector.config.schema.json")
	if err != nil || diagnostics.HasErrors() {
		t.Fatalf("configuration schema did not compile: err=%v diagnostics=%v", err, diagnostics)
	}
}

func readFixture(t *testing.T, name string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fixtureDir(t), name))
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	return schema
}

func fixtureDir(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "testdata", "fixtures")
}
