package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"

	"github.com/satorunooshie/jsonschema-projector/projector"
	"github.com/satorunooshie/jsonschema-projector/toolchain"
)

var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stderr)
		return 2
	}

	switch args[0] {
	case "project":
		return runProject(args[1:], stdin, stdout, stderr)
	case "check":
		return runCheck(args[1:], stdin, stdout, stderr)
	case "generate":
		return runGenerate(args[1:], stdin, stdout, stderr)
	case "version", "-v", "--version":
		fmt.Fprintf(stdout, "jsonschema-projector %s\n", versionString())
		return 0
	case "help", "-h", "--help":
		printUsage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command %q\n", args[0])
		printUsage(stderr)
		return 2
	}
}

func runProject(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("project", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("c", "projector.yaml", "path to projector YAML config")
	outputOverride := fs.String("output", "", "override output.path")
	format := fs.String("format", "text", "diagnostic output format: text or json")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if !validFormat(*format) {
		fmt.Fprintf(stderr, "unsupported format %q\n", *format)
		return 2
	}

	cfg, code := loadCLIConfig(*configPath, stdin, stderr)
	if code != 0 {
		return code
	}
	applyOutputOverride(&cfg, *outputOverride)

	var result *projector.Result
	var err error
	if cfg.Input.Path == "-" {
		result, err = projector.ProjectReader(context.Background(), stdin, cfg.Project)
	} else {
		result, err = projector.Project(context.Background(), cfg)
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	diagnostics := result.Diagnostics.WithStage(toolchain.StageProject)
	if shouldPrintDiagnostics(diagnostics, *format) {
		printDiagnostics(stderr, diagnostics, *format)
	}
	if diagnostics.HasErrors() {
		return 1
	}

	if err := writeProjectedSchema(cfg, result.Schema, stdout); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func runCheck(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("c", "projector.yaml", "path to projector YAML config")
	format := fs.String("format", "text", "diagnostic output format: text or json")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if !validFormat(*format) {
		fmt.Fprintf(stderr, "unsupported format %q\n", *format)
		return 2
	}

	cfg, code := loadCLIConfig(*configPath, stdin, stderr)
	if code != 0 {
		return code
	}

	result, err := toolchain.CheckReader(context.Background(), cfg, stdin)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if *format == "json" {
		printDiagnostics(stdout, result.Diagnostics, *format)
	} else {
		printDiagnostics(stderr, result.Diagnostics, *format)
	}
	if result.Diagnostics.HasErrors() {
		return 1
	}

	if *format == "text" {
		fmt.Fprintln(stdout, "ok")
	}
	return 0
}

func runGenerate(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("generate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("c", "projector.yaml", "path to projector YAML config")
	outputOverride := fs.String("output", "", "override output.path")
	format := fs.String("format", "text", "diagnostic output format: text or json")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if !validFormat(*format) {
		fmt.Fprintf(stderr, "unsupported format %q\n", *format)
		return 2
	}

	cfg, code := loadCLIConfig(*configPath, stdin, stderr)
	if code != 0 {
		return code
	}
	applyOutputOverride(&cfg, *outputOverride)

	result, err := toolchain.GenerateReader(context.Background(), cfg, stdin, stdout)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if shouldPrintDiagnostics(result.Diagnostics, *format) {
		printDiagnostics(stderr, result.Diagnostics, *format)
	}
	if result.Diagnostics.HasErrors() {
		return 1
	}
	return 0
}

func loadCLIConfig(configPath string, stdin io.Reader, stderr io.Writer) (projector.Config, int) {
	var (
		cfg projector.Config
		err error
	)

	if configPath == "-" {
		cfg, err = projector.ReadConfig(stdin)
		if err == nil {
			var baseDir string
			baseDir, err = os.Getwd()
			if err == nil {
				resolveConfigPaths(&cfg, baseDir)
			}
		}
	} else {
		cfg, err = projector.LoadConfig(configPath)
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return projector.Config{}, 1
	}

	return cfg, 0
}

func resolveConfigPaths(cfg *projector.Config, baseDir string) {
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

func applyOutputOverride(cfg *projector.Config, outputPath string) {
	if outputPath == "" {
		return
	}
	if filepath.IsAbs(outputPath) || outputPath == "-" {
		cfg.Output.Path = outputPath
		return
	}
	cwd, err := os.Getwd()
	if err != nil {
		cfg.Output.Path = outputPath
		return
	}
	cfg.Output.Path = filepath.Join(cwd, outputPath)
}

func writeProjectedSchema(cfg projector.Config, schema map[string]any, stdout io.Writer) error {
	opts := projector.WriteOptions{
		Pretty:       cfg.Output.Pretty,
		FinalNewline: cfg.Output.FinalNewline,
	}

	if cfg.Output.Path == "-" {
		return projector.WriteSchema(stdout, schema, opts)
	}
	if cfg.Output.Path == "" {
		return fmt.Errorf("output.path is required")
	}

	return projector.WriteSchemaFile(cfg.Output.Path, schema, opts)
}

func validFormat(format string) bool {
	return format == "text" || format == "json"
}

func shouldPrintDiagnostics(diagnostics projector.Diagnostics, format string) bool {
	return format == "json" || len(diagnostics) > 0
}

func printDiagnostics(w io.Writer, diagnostics projector.Diagnostics, format string) {
	if format == "json" {
		if diagnostics == nil {
			diagnostics = projector.Diagnostics{}
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(diagnostics)
		return
	}

	for _, d := range diagnostics {
		fmt.Fprintln(w, d.String())
	}
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, `usage:
  jsonschema-projector project -c projector.yaml
  jsonschema-projector check -c projector.yaml
  jsonschema-projector generate -c projector.yaml
  jsonschema-projector version`)
}

func versionString() string {
	if version != "" && version != "dev" {
		return version
	}

	info, ok := debug.ReadBuildInfo()
	if ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}

	return version
}
