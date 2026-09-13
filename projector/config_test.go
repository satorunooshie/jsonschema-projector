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

func TestConfigValidateRejectsInvalidStaticValues(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
		field  string
	}{
		{"missing definition pointer", func(c *Config) { c.Project.IncludeDefinitions = []DefinitionSource{{}} }, "project.includeDefinitions"},
		{"invalid root kind", func(c *Config) { c.Project.Root = &RootSynthesis{Kind: "anyOf", From: "#/components"} }, "project.root.kind"},
		{"missing root source", func(c *Config) { c.Project.Root = &RootSynthesis{} }, "project.root.from"},
		{"invalid draft", func(c *Config) { c.Validation.DefaultDraft = "draft-03" }, "validate.defaultDraft"},
		{"duplicate outputs", func(c *Config) {
			c.Output.Path = "build/types.go"
			c.Generate = &GenerateConfig{Go: &GoGenerateConfig{Output: "build/types.go"}}
		}, "generate.go.output"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Input.Path = "schema.json"
			tt.mutate(&cfg)
			err := cfg.Validate()
			if err == nil || !strings.HasPrefix(err.Error(), tt.field+":") {
				t.Fatalf("expected validation error for %s, got %v", tt.field, err)
			}
		})
	}
}

func TestConfigValidateForWriteRequiresOutputPath(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Input.Path = "schema.json"
	if err := cfg.ValidateForWrite(); !errors.Is(err, ErrOutputRequired) {
		t.Fatalf("expected output required error, got %v", err)
	}
}
