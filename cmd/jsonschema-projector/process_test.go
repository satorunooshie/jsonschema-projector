package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIProcessContract(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "jsonschema-projector")
	build := exec.Command("go", "build", "-o", bin, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}

	t.Run("success", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "schema.json"), `{"$defs":{"Person":{"type":"object","properties":{"name":{"type":"string"}}}}}`)
		writeFile(t, filepath.Join(dir, "projector.yaml"), `input:
  path: schema.json
output:
  path: projected.json
project:
  includeDefinitions:
    - pointer: "#/$defs"
`)
		result := runProcess(t, bin, dir, "project", "-c", "projector.yaml")
		if result.err != nil || result.code != 0 {
			t.Fatalf("unexpected failure: %v code=%d\nstderr=%s", result.err, result.code, result.stderr)
		}
		if _, err := os.Stat(filepath.Join(dir, "projected.json")); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("invalid input", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "projector.yaml"), "input:\n  path: missing.json\n")
		result := runProcess(t, bin, dir, "check", "-c", "projector.yaml")
		if result.code != 1 || !strings.Contains(result.stderr, "missing.json") {
			t.Fatalf("unexpected result: %+v", result)
		}
	})

	t.Run("unresolved ref", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "schema.json"), `{"components":{"Thing":{"$ref":"#/components/Missing"}}}`)
		writeFile(t, filepath.Join(dir, "projector.yaml"), `input:
  path: schema.json
project:
  includeDefinitions:
    - pointer: "#/components"
  rewriteRefs:
    - from: "#/components/"
      to: "#/$defs/"
`)
		result := runProcess(t, bin, dir, "check", "-c", "projector.yaml", "--format", "json")
		if result.code != 1 || !strings.Contains(result.stdout, "unresolved_ref") {
			t.Fatalf("unexpected result: %+v", result)
		}
	})

	t.Run("generation failure", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "schema.json"), `{"$defs":{"Broken":{"not":{"type":"string"}}}}`)
		writeFile(t, filepath.Join(dir, "projector.yaml"), `input:
  path: schema.json
output:
  path: projected.json
project:
  includeDefinitions:
    - pointer: "#/$defs"
generate:
  go:
    package: generated
    output: types.gen.go
`)
		result := runProcess(t, bin, dir, "generate", "-c", "projector.yaml")
		if result.code != 1 || !strings.Contains(result.stderr, "unsupported_schema") {
			t.Fatalf("unexpected result: %+v", result)
		}
	})

	t.Run("existing output is protected", func(t *testing.T) {
		dir := t.TempDir()
		output := filepath.Join(dir, "projected.json")
		writeFile(t, output, "keep me\n")
		writeFile(t, filepath.Join(dir, "schema.json"), `{"$defs":{"Broken":{"type":123}}}`)
		writeFile(t, filepath.Join(dir, "projector.yaml"), `input:
  path: schema.json
output:
  path: projected.json
project:
  includeDefinitions:
    - pointer: "#/$defs"
`)
		result := runProcess(t, bin, dir, "generate", "-c", "projector.yaml")
		if result.code != 1 {
			t.Fatalf("expected failure: %+v", result)
		}
		data, err := os.ReadFile(output)
		if err != nil || string(data) != "keep me\n" {
			t.Fatalf("output was modified: %q err=%v", data, err)
		}
	})

	t.Run("stdout output", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "schema.json"), `{"type":"string"}`)
		writeFile(t, filepath.Join(dir, "projector.yaml"), `input:
  path: schema.json
output:
  path: "-"
project:
  includeDefinitions: []
`)
		result := runProcess(t, bin, dir, "project", "-c", "projector.yaml")
		if result.code != 0 {
			t.Fatalf("unexpected failure: %+v", result)
		}
		var schema map[string]any
		if err := json.Unmarshal([]byte(result.stdout), &schema); err != nil {
			t.Fatalf("stdout is not JSON: %v\n%s", err, result.stdout)
		}
	})
}

type processResult struct {
	code           int
	stdout, stderr string
	err            error
}

func runProcess(t *testing.T, bin, dir string, args ...string) processResult {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			code = exit.ExitCode()
		} else {
			code = -1
		}
	}
	return processResult{code: code, stdout: stdout.String(), stderr: stderr.String(), err: err}
}
