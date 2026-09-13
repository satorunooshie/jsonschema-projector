package projector

import (
	"io"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config is the complete projector configuration.
type Config struct {
	// Input controls the source schema document.
	Input InputConfig `json:"input" yaml:"input"`
	// Output controls the projected schema artifact.
	Output OutputConfig `json:"output" yaml:"output"`
	// Project controls schema projection.
	Project ProjectConfig `json:"project" yaml:"project"`
	// Validation controls JSON Schema compilation. The serialized key remains "validate".
	Validation ValidationConfig `json:"validate" yaml:"validate"`
	// Generate controls optional Go DTO generation.
	Generate *GenerateConfig `json:"generate,omitempty" yaml:"generate,omitempty"`
}

// snapshotConfig validates c and returns an immutable operation snapshot.
// It prevents concurrent mutation of the caller's slices and nested pointers
// from changing an operation after validation has started.
func snapshotConfig(c Config) (Config, error) {
	if err := c.Validate(); err != nil {
		return Config{}, err
	}
	return Config{
		Input:      c.Input,
		Output:     c.Output,
		Project:    cloneProjectConfig(c.Project),
		Validation: c.Validation,
		Generate:   cloneGenerateConfig(c.Generate),
	}, nil
}

func cloneProjectConfig(c ProjectConfig) ProjectConfig {
	c.IncludeDefinitions = append([]DefinitionSource(nil), c.IncludeDefinitions...)
	c.RewriteRefs = append([]RefRewrite(nil), c.RewriteRefs...)
	if c.Root != nil {
		root := *c.Root
		root.Names = append([]string(nil), c.Root.Names...)
		c.Root = &root
	}
	return c
}

func cloneGenerateConfig(c *GenerateConfig) *GenerateConfig {
	if c == nil {
		return nil
	}
	g := *c
	if c.Go != nil {
		goCfg := *c.Go
		goCfg.Tags = append([]string(nil), c.Go.Tags...)
		goCfg.Capitalizations = append([]string(nil), c.Go.Capitalizations...)
		g.Go = &goCfg
	}
	return &g
}

// Validate checks configuration invariants shared by all pipeline operations.
func (c Config) Validate() error {
	if c.Input.Path == "" {
		return &ConfigError{Field: "input.path", Message: ErrInputRequired.Error(), Cause: ErrInputRequired}
	}
	if c.Generate != nil && c.Generate.Go == nil {
		return &ConfigError{Field: "generate.go", Message: "generate.go config is required", Cause: ErrInvalidConfig}
	}
	return nil
}

// InputConfig identifies the source schema document.
type InputConfig struct {
	// Path is the JSON input path. A path of "-" reads stdin in the CLI.
	Path string `json:"path" yaml:"path"`
}

// OutputConfig controls projected schema output.
type OutputConfig struct {
	// Path is the projected schema output path. A path of "-" writes stdout.
	Path string `json:"path" yaml:"path"`
	// Pretty enables indented JSON output.
	Pretty bool `json:"pretty" yaml:"pretty"`
	// FinalNewline appends a newline to JSON output.
	FinalNewline bool `json:"finalNewline" yaml:"finalNewline"`
}

// ProjectConfig controls schema projection.
type ProjectConfig struct {
	// ID is the optional identifier assigned to the projected schema.
	ID string `json:"id,omitempty" yaml:"id,omitempty"`
	// Title is the optional title assigned to the projected schema.
	Title string `json:"title,omitempty" yaml:"title,omitempty"`

	// IncludeDefinitions lists source objects whose children are copied into $defs.
	IncludeDefinitions []DefinitionSource `json:"includeDefinitions,omitempty" yaml:"includeDefinitions,omitempty"`
	// RewriteRefs applies ordered prefix rewrites to $ref values.
	RewriteRefs []RefRewrite `json:"rewriteRefs,omitempty" yaml:"rewriteRefs,omitempty"`
	// Root optionally synthesizes a root schema from a source object.
	Root *RootSynthesis `json:"root,omitempty" yaml:"root,omitempty"`

	// OverwriteDefinitions permits later definition sources to replace earlier ones.
	OverwriteDefinitions bool `json:"overwriteDefinitions" yaml:"overwriteDefinitions"`
	// CheckRefs checks local JSON Pointer references after projection.
	CheckRefs bool `json:"checkRefs" yaml:"checkRefs"`
}

// DefinitionSource selects a definition source by JSON Pointer.
type DefinitionSource struct {
	// Pointer identifies an object whose direct children are copied into $defs.
	Pointer string `json:"pointer" yaml:"pointer"`
}

// RefRewrite describes one ordered $ref prefix replacement.
type RefRewrite struct {
	// From is the reference prefix to replace.
	From string `json:"from" yaml:"from"`
	// To is the replacement reference prefix.
	To string `json:"to" yaml:"to"`
}

// RootSynthesis describes an optional generated root union.
type RootSynthesis struct {
	// Kind is currently limited to oneOf.
	Kind string `json:"kind" yaml:"kind"`
	// From identifies the object whose entries form the root union.
	From string `json:"from" yaml:"from"`
	// Names optionally fixes the order and subset of root entries.
	Names []string `json:"names,omitempty" yaml:"names,omitempty"`
}

// ValidationConfig controls JSON Schema compilation.
type ValidationConfig struct {
	// Schema enables compilation of the projected schema.
	Schema bool `json:"schema" yaml:"schema"`
	// DefaultDraft selects the draft used when $schema is absent.
	DefaultDraft string `json:"defaultDraft,omitempty" yaml:"defaultDraft,omitempty"`
	// AssertFormat enables format assertion during compilation.
	AssertFormat bool `json:"assertFormat" yaml:"assertFormat"`
	// AssertContent enables content assertion during compilation.
	AssertContent bool `json:"assertContent" yaml:"assertContent"`
}

// GenerateConfig controls code generation.
type GenerateConfig struct {
	// Go configures native Go DTO generation.
	Go *GoGenerateConfig `json:"go,omitempty" yaml:"go,omitempty"`
}

// GoGenerateConfig controls native Go DTO generation.
type GoGenerateConfig struct {
	// Package is the generated Go package name.
	Package string `json:"package" yaml:"package"`
	// Output is the generated Go source path, or "-" for stdout.
	Output string `json:"output" yaml:"output"`
	// RootType optionally names the generated root type.
	RootType string `json:"rootType,omitempty" yaml:"rootType,omitempty"`
	// Tags are struct tag keys emitted on generated fields.
	Tags []string `json:"tags,omitempty" yaml:"tags,omitempty"`
	// Capitalizations contains additional words treated as initialisms.
	Capitalizations []string `json:"capitalizations,omitempty" yaml:"capitalizations,omitempty"`
	// Unions controls generated interface dispatch.
	Unions UnionGenerateConfig `json:"unions" yaml:"unions,omitempty"`
}

// UnionGenerateConfig controls generated union decoders.
type UnionGenerateConfig struct {
	// Dispatch is "token" (the default) or "schema-validation". Token
	// dispatch is a DTO routing heuristic; it does not replace validation.
	Dispatch string `json:"dispatch,omitempty" yaml:"dispatch,omitempty"`
	// Ambiguous controls overlapping candidates. The default is "error".
	Ambiguous string `json:"ambiguous,omitempty" yaml:"ambiguous,omitempty"`
}

// DefaultConfig returns the configuration defaults used by the CLI and APIs.
func DefaultConfig() Config {
	return Config{
		Output: OutputConfig{
			Pretty:       true,
			FinalNewline: true,
		},
		Project: ProjectConfig{
			IncludeDefinitions: []DefinitionSource{
				{Pointer: "#/$defs"},
			},
			CheckRefs: true,
		},
		Validation: ValidationConfig{
			Schema:       true,
			DefaultDraft: "2020-12",
		},
	}
}

// LoadConfig reads YAML configuration from path and resolves relative paths.
func LoadConfig(path string) (Config, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return Config{}, err
	}

	file, err := os.Open(abs)
	if err != nil {
		return Config{}, &FileError{Operation: "open", Path: abs, Err: err}
	}
	defer file.Close()

	cfg, err := ReadConfig(file)
	if err != nil {
		return Config{}, err
	}

	resolveConfigPaths(&cfg, filepath.Dir(abs))
	return cfg, nil
}

// ReadConfig decodes YAML configuration from r without resolving paths.
func ReadConfig(r io.Reader) (Config, error) {
	if r == nil {
		return Config{}, &ConfigError{Field: "config", Message: "config reader is required", Cause: ErrInputRequired}
	}

	cfg := DefaultConfig()

	dec := yaml.NewDecoder(r)
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return Config{}, &ConfigError{Field: "config", Message: "configuration must contain a single YAML document", Cause: ErrInvalidConfig}
		}
		return Config{}, err
	}

	return cfg, nil
}

func resolveConfigPaths(cfg *Config, baseDir string) {
	cfg.Input.Path = resolvePath(baseDir, cfg.Input.Path)
	cfg.Output.Path = resolvePath(baseDir, cfg.Output.Path)
	if cfg.Generate != nil && cfg.Generate.Go != nil {
		cfg.Generate.Go.Output = resolvePath(baseDir, cfg.Generate.Go.Output)
	}
}

func resolvePath(baseDir, path string) string {
	if path == "" || path == "-" || filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(baseDir, path)
}
