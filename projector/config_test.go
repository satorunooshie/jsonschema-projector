package projector

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigResolvesPathsRelativeToConfigFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config", "projector.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(`input:
  path: schema/catalog.schema.json
output:
  path: build/projected.schema.json
project:
  includeDefinitions:
    - pointer: "#/$defs"
generate:
  go:
    package: component
    output: internal/component/types.gen.go
`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}

	baseDir := filepath.Dir(configPath)
	if cfg.Input.Path != filepath.Join(baseDir, "schema/catalog.schema.json") {
		t.Fatalf("unexpected input path: %s", cfg.Input.Path)
	}
	if cfg.Output.Path != filepath.Join(baseDir, "build/projected.schema.json") {
		t.Fatalf("unexpected output path: %s", cfg.Output.Path)
	}
	if cfg.Generate.Go.Output != filepath.Join(baseDir, "internal/component/types.gen.go") {
		t.Fatalf("unexpected generate output path: %s", cfg.Generate.Go.Output)
	}
}

func TestReadConfigRejectsMultipleDocuments(t *testing.T) {
	_, err := ReadConfig(strings.NewReader("input:\n  path: schema.json\n---\ninput:\n  path: other.json\n"))
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected invalid config error, got %v", err)
	}
}

func TestReadConfigRejectsNilReader(t *testing.T) {
	_, err := ReadConfig(nil)
	if !errors.Is(err, ErrInputRequired) {
		t.Fatalf("expected input required error, got %v", err)
	}
}
