package projector

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/satorunooshie/jsonschema-projector/internal/loader"
	"github.com/satorunooshie/jsonschema-projector/internal/pointer"
)

type Result struct {
	// Schema is the projected JSON Schema document.
	Schema map[string]any `json:"schema"`
	// Diagnostics contains ordered projection issues.
	Diagnostics Diagnostics `json:"diagnostics"`
}

// Project loads the configured input file and projects it into a new schema.
func Project(ctx context.Context, cfg Config) (*Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cfg, err := snapshotConfig(cfg)
	if err != nil {
		return nil, err
	}
	if cfg.Input.Path == "-" {
		return ProjectReader(ctx, os.Stdin, cfg.Project)
	}

	doc, err := loader.LoadJSONFile(cfg.Input.Path)
	if err != nil {
		return nil, &FileError{Operation: "read", Path: cfg.Input.Path, Err: err}
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return ProjectSchema(ctx, doc, cfg.Project)
}

// ProjectReader projects a JSON Schema read from input. It is the injectable
// counterpart to Project and is intended for embedding and testing.
func ProjectReader(ctx context.Context, input io.Reader, cfg ProjectConfig) (*Result, error) {
	if input == nil {
		return nil, &ConfigError{Field: "input", Message: "input reader is required", Cause: ErrInputRequired}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	doc, err := loader.LoadJSON(input)
	if err != nil {
		return nil, err
	}
	return ProjectSchema(ctx, doc, cfg)
}

// ProjectSchema projects an in-memory JSON Schema document.
func ProjectSchema(ctx context.Context, doc map[string]any, cfg ProjectConfig) (*Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cfg = cloneProjectConfig(cfg)

	result := &Result{
		Schema: map[string]any{},
	}
	diagnostics := &result.Diagnostics

	validateRewriteConfig(cfg, diagnostics)

	defs := collectDefinitions(doc, cfg, diagnostics)
	result.Schema = synthesizeRoot(doc, defs, cfg, diagnostics)

	rewriteRefs(result.Schema, cfg.RewriteRefs)

	if cfg.CheckRefs {
		checkRefs(result.Schema, diagnostics)
	}

	return result, ctx.Err()
}

func validateRewriteConfig(cfg ProjectConfig, diagnostics *Diagnostics) {
	for i, rewrite := range cfg.RewriteRefs {
		if rewrite.From == "" {
			diagnostics.addAt(
				SeverityError,
				CodeInvalidConfig,
				"project.rewriteRefs.from is required",
				fmt.Sprintf("project.rewriteRefs.%d.from", i),
			)
		}
	}

}

func collectDefinitions(doc map[string]any, cfg ProjectConfig, diagnostics *Diagnostics) map[string]any {
	defs := make(map[string]any)
	sources := make(map[string]string)

	for _, source := range cfg.IncludeDefinitions {
		if source.Pointer == "" {
			diagnostics.add(SeverityError, CodeInvalidConfig, "includeDefinitions.pointer is required")
			continue
		}

		value, ok, err := pointer.Eval(doc, source.Pointer)
		if err != nil {
			diagnostics.addAt(SeverityError, CodeInvalidPointer, err.Error(), source.Pointer)
			continue
		}
		if !ok {
			diagnostics.addAt(SeverityError, CodeMissingPointer, "definition source does not exist", source.Pointer)
			continue
		}

		section, ok := value.(map[string]any)
		if !ok {
			diagnostics.addAt(SeverityError, CodeSourceNotObject, "definition source must resolve to an object", source.Pointer)
			continue
		}

		names := sortedKeys(section)
		for _, name := range names {
			sourcePointer := childPointer(source.Pointer, name)
			if previous, exists := sources[name]; exists && !cfg.OverwriteDefinitions {
				diagnostics.addRef(
					SeverityError,
					CodeDuplicateDefinition,
					fmt.Sprintf("definition name %q already exists", name),
					sourcePointer,
					"",
					fmt.Sprintf("previous definition came from %s; rename one entry or set project.overwriteDefinitions=true", previous),
				)
				continue
			}
			if previous, exists := sources[name]; exists {
				diagnostics.addRef(
					SeverityWarning,
					CodeDuplicateDefinition,
					fmt.Sprintf("definition name %q was overwritten", name),
					sourcePointer,
					"",
					fmt.Sprintf("previous definition came from %s", previous),
				)
			}

			defs[name] = loader.DeepCopy(section[name])
			sources[name] = sourcePointer
		}
	}

	return defs
}

func synthesizeRoot(doc map[string]any, defs map[string]any, cfg ProjectConfig, diagnostics *Diagnostics) map[string]any {
	out := make(map[string]any)

	if schema, ok := doc["$schema"].(string); ok {
		out["$schema"] = schema
	}

	if cfg.ID != "" {
		out["$id"] = cfg.ID
	}

	if cfg.Title != "" {
		out["title"] = cfg.Title
	} else if title, ok := doc["title"].(string); ok {
		out["title"] = title
	}

	if cfg.Root != nil {
		synthesizeRootUnion(out, doc, *cfg.Root, diagnostics)
	}

	out["$defs"] = defs
	return out
}

func synthesizeRootUnion(out map[string]any, doc map[string]any, root RootSynthesis, diagnostics *Diagnostics) {
	kind := root.Kind
	if kind == "" {
		kind = "oneOf"
	}
	if kind != "oneOf" {
		diagnostics.addAt(SeverityError, CodeInvalidConfig, fmt.Sprintf("unsupported root kind %q", root.Kind), "project.root.kind")
		return
	}
	if root.From == "" {
		diagnostics.addAt(SeverityError, CodeInvalidConfig, "project.root.from is required", "project.root.from")
		return
	}

	value, ok, err := pointer.Eval(doc, root.From)
	if err != nil {
		diagnostics.addAt(SeverityError, CodeInvalidPointer, err.Error(), root.From)
		return
	}
	if !ok {
		diagnostics.addAt(SeverityError, CodeMissingPointer, "root source does not exist", root.From)
		return
	}

	section, ok := value.(map[string]any)
	if !ok {
		diagnostics.addAt(SeverityError, CodeSourceNotObject, "root source must resolve to an object", root.From)
		return
	}

	names := selectRootNames(section, root)

	oneOf := make([]any, 0, len(names))
	for _, name := range names {
		if _, exists := section[name]; !exists {
			diagnostics.addAt(
				SeverityError,
				CodeMissingPointer,
				fmt.Sprintf("root entry %q does not exist", name),
				childPointer(root.From, name),
			)
			continue
		}
		oneOf = append(oneOf, map[string]any{
			"$ref": pointer.Join("$defs", name),
		})
	}

	out["oneOf"] = oneOf
}

func rewriteRefs(value any, rewrites []RefRewrite) {
	if len(rewrites) == 0 {
		return
	}

	switch v := value.(type) {
	case map[string]any:
		for key, item := range v {
			if key == "$ref" {
				ref, ok := item.(string)
				if !ok {
					continue
				}
				v[key] = rewriteRef(ref, rewrites)
				continue
			}
			rewriteRefs(item, rewrites)
		}
	case []any:
		for _, item := range v {
			rewriteRefs(item, rewrites)
		}
	}
}

func rewriteRef(ref string, rewrites []RefRewrite) string {
	for _, rewrite := range rewrites {
		if rewrite.From == "" {
			continue
		}
		if after, ok := strings.CutPrefix(ref, rewrite.From); ok {
			return rewrite.To + after
		}
	}
	return ref
}

func checkRefs(doc map[string]any, diagnostics *Diagnostics) {
	walkRefs(doc, "#", func(location string, ref string) {
		if pointer.IsLocalFragment(ref) {
			_, ok, err := pointer.Eval(doc, ref)
			if err != nil {
				diagnostics.addRef(SeverityError, CodeInvalidPointer, err.Error(), location, ref, "")
				return
			}
			if !ok {
				diagnostics.addRef(
					SeverityError,
					CodeUnresolvedRef,
					"local JSON Pointer reference does not resolve in projected schema",
					location,
					ref,
					"ensure the target is included in project.includeDefinitions or add an appropriate project.rewriteRefs entry",
				)
			}
			return
		}

		if strings.HasPrefix(ref, "#") {
			diagnostics.addRef(
				SeverityWarning,
				CodeUnsupportedRef,
				"fragment reference is not a JSON Pointer and was not checked",
				location,
				ref,
				"only local JSON Pointer refs are checked",
			)
		}
	})
}

func selectRootNames(section map[string]any, root RootSynthesis) []string {
	if len(root.Names) > 0 {
		return append([]string(nil), root.Names...)
	}

	return sortedKeys(section)
}

func walkRefs(value any, location string, visit func(location string, ref string)) {
	switch v := value.(type) {
	case map[string]any:
		keys := sortedKeys(v)
		for _, key := range keys {
			child := childPointer(location, key)
			item := v[key]
			if key == "$ref" {
				if ref, ok := item.(string); ok {
					visit(child, ref)
				}
				continue
			}
			walkRefs(item, child, visit)
		}
	case []any:
		for i, item := range v {
			walkRefs(item, childPointer(location, fmt.Sprintf("%d", i)), visit)
		}
	}
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func childPointer(parent, token string) string {
	escaped := pointer.EscapeToken(token)
	switch parent {
	case "", "#":
		return "#/" + escaped
	default:
		return strings.TrimRight(parent, "/") + "/" + escaped
	}
}
