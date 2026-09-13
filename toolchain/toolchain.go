package toolchain

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/satorunooshie/jsonschema-projector/internal/atomicfile"
	"github.com/satorunooshie/jsonschema-projector/internal/generator"
	"github.com/satorunooshie/jsonschema-projector/internal/validator"
	"github.com/satorunooshie/jsonschema-projector/projector"
)

type projectOperation func(context.Context, projector.Config) (*projector.Result, error)

const (
	// StageProject identifies projection diagnostics.
	StageProject = "project"
	// StageValidate identifies schema validation diagnostics.
	StageValidate = "validate"
	// StageGenerate identifies Go generation diagnostics.
	StageGenerate = "generate"
)

type Result struct {
	// Schema is the projected schema produced by the pipeline.
	Schema map[string]any `json:"schema,omitempty"`
	// Diagnostics contains ordered stage-specific issues.
	Diagnostics projector.Diagnostics `json:"diagnostics"`
}

// Check projects and, when enabled, compiles a schema without writing output.
func Check(ctx context.Context, cfg projector.Config) (*Result, error) {
	return runCheck(ctx, cfg, projector.Project)
}

// CheckReader checks a schema using input as the schema reader when input.path is "-".
func CheckReader(ctx context.Context, cfg projector.Config, input io.Reader) (*Result, error) {
	if cfg.Input.Path != "-" {
		return Check(ctx, cfg)
	}
	return runCheck(ctx, cfg, func(ctx context.Context, cfg projector.Config) (*projector.Result, error) {
		return projector.ProjectReader(ctx, input, cfg.Project)
	})
}

func runCheck(ctx context.Context, cfg projector.Config, project projectOperation) (*Result, error) {
	var err error
	if cfg, err = snapshotConfig(cfg); err != nil {
		return nil, err
	}
	projectResult, err := project(ctx, cfg)
	if err != nil {
		return nil, err
	}

	result := &Result{
		Schema:      projectResult.Schema,
		Diagnostics: projectResult.Diagnostics.WithStage(StageProject),
	}
	if result.Diagnostics.HasErrors() {
		return result, ctx.Err()
	}

	validateDiagnostics, err := validator.CompileSchema(ctx, result.Schema, cfg.Validation, cfg.Output.Path)
	if err != nil {
		return result, err
	}
	result.Diagnostics = append(result.Diagnostics, validateDiagnostics.WithStage(StageValidate)...)

	return result, ctx.Err()
}

// Generate projects, validates, writes the schema artifact, and generates Go DTOs.
func Generate(ctx context.Context, cfg projector.Config, stdout io.Writer) (*Result, error) {
	return runGenerate(ctx, cfg, stdout, projector.Project)
}

// GenerateReader generates using input as the schema reader when input.path is "-".
func GenerateReader(ctx context.Context, cfg projector.Config, input io.Reader, stdout io.Writer) (*Result, error) {
	if cfg.Input.Path != "-" {
		return Generate(ctx, cfg, stdout)
	}
	return runGenerate(ctx, cfg, stdout, func(ctx context.Context, cfg projector.Config) (*projector.Result, error) {
		return projector.ProjectReader(ctx, input, cfg.Project)
	})
}

func runGenerate(ctx context.Context, cfg projector.Config, stdout io.Writer, project projectOperation) (*Result, error) {
	result := &Result{}
	if cfg.Generate == nil {
		result.Diagnostics = append(result.Diagnostics, projector.Diagnostic{
			Severity: projector.SeverityError,
			Stage:    StageGenerate,
			Code:     projector.CodeInvalidGenerateConfig,
			Message:  "generate config is required",
			Pointer:  "generate",
		})
		return result, ctx.Err()
	}
	var err error
	if cfg, err = snapshotConfig(cfg); err != nil {
		return result, err
	}
	if err := cfg.ValidateForWrite(); err != nil {
		return result, err
	}
	generateDiagnostics := generator.ValidateGoConfig(cfg.Generate.Go).WithStage(StageGenerate)
	result.Diagnostics = append(result.Diagnostics, generateDiagnostics...)
	if result.Diagnostics.HasErrors() {
		return result, ctx.Err()
	}

	checkResult, err := runCheck(ctx, cfg, project)
	if err != nil {
		return result, err
	}
	result.Schema = checkResult.Schema
	result.Diagnostics = append(result.Diagnostics, checkResult.Diagnostics...)
	if result.Diagnostics.HasErrors() {
		return result, ctx.Err()
	}

	if cfg.Output.Path == "" || cfg.Output.Path == "-" {
		result.Diagnostics = append(result.Diagnostics, projector.Diagnostic{
			Severity: projector.SeverityError,
			Stage:    StageGenerate,
			Code:     projector.CodeInvalidGenerateConfig,
			Message:  "output.path must be a file path for generate",
			Pointer:  "output.path",
			Hint:     "generate writes the projected schema as an artifact before writing generated Go types",
		})
		return result, ctx.Err()
	}
	source, generateDiagnostics, err := generator.GenerateGoSource(ctx, *cfg.Generate.Go, result.Schema)
	if err != nil {
		return result, err
	}
	result.Diagnostics = append(result.Diagnostics, generateDiagnostics.WithStage(StageGenerate)...)
	if result.Diagnostics.HasErrors() {
		return result, ctx.Err()
	}
	if cfg.Generate.Go.Output == "-" {
		if err := writeSchemaFile(cfg, result.Schema); err != nil {
			return result, err
		}
		if err := generator.WriteGeneratedSource(cfg.Generate.Go.Output, source, stdout); err != nil {
			return result, err
		}
		return result, ctx.Err()
	}
	if err := writeArtifacts(cfg, result.Schema, source); err != nil {
		return result, err
	}

	return result, ctx.Err()
}

func snapshotConfig(cfg projector.Config) (projector.Config, error) {
	if err := cfg.Validate(); err != nil {
		return projector.Config{}, err
	}
	cfg.Project.IncludeDefinitions = append([]projector.DefinitionSource(nil), cfg.Project.IncludeDefinitions...)
	cfg.Project.RewriteRefs = append([]projector.RefRewrite(nil), cfg.Project.RewriteRefs...)
	if cfg.Project.Root != nil {
		root := *cfg.Project.Root
		root.Names = append([]string(nil), root.Names...)
		cfg.Project.Root = &root
	}
	if cfg.Generate != nil && cfg.Generate.Go != nil {
		generate := *cfg.Generate
		goConfig := *cfg.Generate.Go
		goConfig.Tags = append([]string(nil), goConfig.Tags...)
		goConfig.Capitalizations = append([]string(nil), goConfig.Capitalizations...)
		generate.Go = &goConfig
		cfg.Generate = &generate
	}
	return cfg, nil
}

func writeArtifacts(cfg projector.Config, schema map[string]any, source []byte) error {
	var schemaBuffer bytes.Buffer
	opts := projector.WriteOptions{Pretty: cfg.Output.Pretty, FinalNewline: cfg.Output.FinalNewline}
	if err := projector.WriteSchema(&schemaBuffer, schema, opts); err != nil {
		return &projector.FileError{Operation: "encode", Path: cfg.Output.Path, Err: err}
	}
	if err := atomicfile.WriteFiles(
		atomicfile.File{Path: cfg.Output.Path, Data: schemaBuffer.Bytes()},
		atomicfile.File{Path: cfg.Generate.Go.Output, Data: source},
	); err != nil {
		fileErr, ok := err.(*atomicfile.Error)
		if !ok {
			return err
		}
		return &projector.FileError{Operation: fileErr.Operation, Path: fileErr.Path, Err: fileErr.Err}
	}
	return nil
}

func writeSchemaFile(cfg projector.Config, schema map[string]any) error {
	opts := projector.WriteOptions{
		Pretty:       cfg.Output.Pretty,
		FinalNewline: cfg.Output.FinalNewline,
	}

	if cfg.Output.Path == "" {
		return fmt.Errorf("output.path is required")
	}

	return projector.WriteSchemaFile(cfg.Output.Path, schema, opts)
}
