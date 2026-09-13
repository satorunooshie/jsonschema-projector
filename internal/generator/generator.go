package generator

import (
	"bytes"
	"context"
	"fmt"
	"go/format"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"unicode"

	"github.com/satorunooshie/jsonschema-projector/internal/pointer"
	"github.com/satorunooshie/jsonschema-projector/projector"
)

func ValidateGoConfig(cfg *projector.GoGenerateConfig) projector.Diagnostics {
	if cfg == nil {
		return projector.Diagnostics{{
			Severity: projector.SeverityError,
			Code:     projector.CodeInvalidGenerateConfig,
			Message:  "generate.go config is required",
			Pointer:  "generate.go",
		}}
	}
	if cfg.Package == "" {
		return projector.Diagnostics{{
			Severity: projector.SeverityError,
			Code:     projector.CodeInvalidGenerateConfig,
			Message:  "generate.go.package is required",
			Pointer:  "generate.go.package",
		}}
	}
	if !token.IsIdentifier(cfg.Package) || token.Lookup(cfg.Package).IsKeyword() {
		return projector.Diagnostics{{
			Severity: projector.SeverityError,
			Code:     projector.CodeInvalidGenerateConfig,
			Message:  fmt.Sprintf("generate.go.package must be a Go package identifier, got %q", cfg.Package),
			Pointer:  "generate.go.package",
		}}
	}
	if cfg.Output == "" {
		return projector.Diagnostics{{
			Severity: projector.SeverityError,
			Code:     projector.CodeInvalidGenerateConfig,
			Message:  "generate.go.output is required",
			Pointer:  "generate.go.output",
		}}
	}
	if cfg.Unions.Dispatch != "" && cfg.Unions.Dispatch != "token" && cfg.Unions.Dispatch != "schema-validation" {
		return projector.Diagnostics{{Severity: projector.SeverityError, Code: projector.CodeInvalidGenerateConfig, Message: "generate.go.unions.dispatch must be token or schema-validation", Pointer: "generate.go.unions.dispatch"}}
	}
	if cfg.Unions.Ambiguous != "" && cfg.Unions.Ambiguous != "error" && cfg.Unions.Ambiguous != "first" {
		return projector.Diagnostics{{Severity: projector.SeverityError, Code: projector.CodeInvalidGenerateConfig, Message: "generate.go.unions.ambiguous must be error or first", Pointer: "generate.go.unions.ambiguous"}}
	}
	for i, tag := range tags(*cfg) {
		if invalidStructTagKey(tag) {
			return projector.Diagnostics{{
				Severity: projector.SeverityError,
				Code:     projector.CodeInvalidGenerateConfig,
				Message:  fmt.Sprintf("generate.go.tags.%d must be a valid struct tag key, got %q", i, tag),
				Pointer:  fmt.Sprintf("generate.go.tags.%d", i),
			}}
		}
	}

	return nil
}

func GenerateGo(ctx context.Context, cfg projector.GoGenerateConfig, schema map[string]any, stdout io.Writer) (projector.Diagnostics, error) {
	source, diagnostics, err := GenerateGoSource(ctx, cfg, schema)
	if err != nil || diagnostics.HasErrors() {
		return diagnostics, err
	}
	if err := WriteGeneratedSource(cfg.Output, source, stdout); err != nil {
		return projector.Diagnostics{{
			Severity: projector.SeverityError,
			Code:     projector.CodeGeneratedOutputFailed,
			Message:  fmt.Sprintf("failed to write generated Go source: %v", err),
			Pointer:  "generate.go.output",
			Ref:      cfg.Output,
		}}, nil
	}
	return diagnostics, ctx.Err()
}

// GenerateGoSource validates cfg and returns formatted generated Go source without writing files.
func GenerateGoSource(ctx context.Context, cfg projector.GoGenerateConfig, schema map[string]any) ([]byte, projector.Diagnostics, error) {
	if diagnostics := ValidateGoConfig(&cfg); diagnostics.HasErrors() {
		return nil, diagnostics, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	gen := newNativeGenerator(cfg, schema)
	source := gen.Source()
	if gen.diagnostics.HasErrors() {
		return nil, gen.diagnostics, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	return source, gen.diagnostics, ctx.Err()
}

func newNativeGenerator(cfg projector.GoGenerateConfig, schema map[string]any) *nativeGenerator {
	gen := &nativeGenerator{
		cfg:                cfg,
		schema:             schema,
		defs:               map[string]*schemaNode{},
		typeNames:          map[string]string{},
		usedTypeNames:      map[string]string{},
		generatedByPointer: map[string]string{},
		markerMethods:      map[string]map[string]struct{}{},
	}
	gen.root = gen.parseSchema(schema, "#")
	gen.parseDefinitions()
	gen.normalizeInlineUnions()
	gen.normalizeAllOf()
	gen.reserveDefinitionTypeNames()
	return gen
}

// normalizeInlineUnions gives every inline union a stable definition name and
// replaces the original occurrence with a local $ref. This makes root, field,
// and array-item unions share exactly the same downstream code path.
func (g *nativeGenerator) normalizeInlineUnions() {
	for _, name := range append([]string(nil), g.defOrder...) {
		g.normalizeInlineNode(g.defs[name], g.exportedIdentifier(name), map[*schemaNode]bool{})
	}
	g.normalizeInlineNode(g.root, firstNonEmpty(g.root.Title, "Root"), map[*schemaNode]bool{})
	g.defOrder = sortedStringKeys(g.defs)
}

func (g *nativeGenerator) normalizeInlineNode(node *schemaNode, context string, stack map[*schemaNode]bool) {
	if node == nil || stack[node] {
		return
	}
	stack[node] = true
	defer delete(stack, node)
	for _, name := range sortedKeysNode(node.Properties) {
		prop := node.Properties[name]
		if prop != nil && (len(prop.OneOf) > 0 || len(prop.AnyOf) > 0) && prop.Ref == "" {
			defName := g.syntheticDefinitionName(context + g.exportedIdentifier(name))
			g.defs[defName] = prop
			node.Properties[name] = g.parseSchema(map[string]any{"$ref": "#/$defs/" + defName}, prop.Pointer)
			g.normalizeInlineNode(prop, defName, stack)
			continue
		}
		g.normalizeInlineNode(prop, context+g.exportedIdentifier(name), stack)
	}
	if node.Items != nil && (len(node.Items.OneOf) > 0 || len(node.Items.AnyOf) > 0) && node.Items.Ref == "" {
		item := node.Items
		defName := g.syntheticDefinitionName(context + "Item")
		g.defs[defName] = item
		node.Items = g.parseSchema(map[string]any{"$ref": "#/$defs/" + defName}, item.Pointer)
		g.normalizeInlineNode(item, defName, stack)
	} else {
		g.normalizeInlineNode(node.Items, context+"Item", stack)
	}
	for _, variant := range append(append([]*schemaNode{}, node.OneOf...), node.AnyOf...) {
		g.normalizeInlineNode(variant, context+"Variant", stack)
	}
	for _, member := range node.AllOf {
		g.normalizeInlineNode(member, context+"Base", stack)
	}
}

func (g *nativeGenerator) syntheticDefinitionName(base string) string {
	base = g.exportedIdentifier(base)
	if _, exists := g.defs[base]; !exists {
		return base
	}
	for i := 2; ; i++ {
		candidate := base + strconv.Itoa(i)
		if _, exists := g.defs[candidate]; !exists {
			return candidate
		}
	}
}

func (g *nativeGenerator) Source() []byte {
	g.generateRootUnion()
	for _, name := range g.defOrder {
		g.generateDefinition(name, g.defs[name])
	}

	data := sourceTemplateData{Package: g.cfg.Package, Decls: g.decls}
	if g.needsJSON || g.needsErrors {
		data.NeedsErrors = g.needsErrors
		data.NeedsJSON = g.needsJSON
		if g.needsErrors {
			data.Imports = append(data.Imports, "errors")
		}
		if g.needsJSON {
			data.Imports = append(data.Imports, "encoding/json")
		}
		if g.needsFmt {
			data.Imports = append(data.Imports, "fmt")
		}
		if g.needsSync {
			data.Imports = append(data.Imports, "sync")
		}
		data.Preamble = g.renderEmbeddedFragment("preamble", data)
	}
	for _, receiver := range sortedMarkerReceivers(g.markerMethods) {
		methods := sortedSet(g.markerMethods[receiver])
		for _, method := range methods {
			data.Markers = append(data.Markers, fmt.Sprintf("func (%s) %s() {}", receiver, method))
		}
	}
	for _, union := range g.unions {
		data.Decoders = append(data.Decoders, g.unionDecoder(union))
	}
	sourceText, err := embeddedTemplate("source")
	if err != nil {
		g.addError(projector.CodeGenerationFailed, err.Error(), "generate.go", "")
		return nil
	}
	tmpl, err := template.New("go-source").Parse(sourceText)
	if err != nil {
		g.addError(projector.CodeGenerationFailed, err.Error(), "generate.go", "")
		return nil
	}
	var rendered bytes.Buffer
	if err := tmpl.Execute(&rendered, data); err != nil {
		g.addError(projector.CodeGenerationFailed, err.Error(), "generate.go", "")
		return rendered.Bytes()
	}
	source := rendered.Bytes()
	formatted, err := format.Source(source)
	if err != nil {
		g.addError(
			projector.CodeGenerationFailed,
			fmt.Sprintf("generated Go source is not gofmt-able: %v", err),
			"generate.go",
			"",
		)
		return source
	}
	return formatted
}

func (g *nativeGenerator) reserveTypeName(name, ptr string, explicit bool) string {
	if name == "" {
		name = "Value"
	}
	if !token.IsIdentifier(name) || token.Lookup(name).IsKeyword() {
		name = "Value"
	}

	if previous, exists := g.usedTypeNames[name]; exists {
		if explicit {
			g.addError(
				projector.CodeNameConflict,
				fmt.Sprintf("Go type name %s is used by both %s and %s", name, previous, ptr),
				ptr,
				"use x-go-name or generate.go.rootType to make generated type names unique",
			)
			return name
		}
		base := name
		for i := 2; ; i++ {
			candidate := base + strconv.Itoa(i)
			if _, exists := g.usedTypeNames[candidate]; !exists {
				name = candidate
				break
			}
		}
	}

	g.usedTypeNames[name] = ptr
	return name
}

func (g *nativeGenerator) addDecl(name, ptr, body string) {
	if _, exists := g.generatedByPointer[ptr]; exists {
		return
	}
	g.generatedByPointer[ptr] = name
	g.decls = append(g.decls, decl{Name: name, Pointer: ptr, Body: body})
}

// renderEmbeddedFragment is the single rendering boundary for generated Go
// fragments. Every fragment is an embedded file so generated output has one
// reviewable source of truth.
func (g *nativeGenerator) renderEmbeddedFragment(name string, data any) string {
	source, err := embeddedTemplate(name)
	if err != nil {
		g.addError(projector.CodeGenerationFailed, fmt.Sprintf("load %s template: %v", name, err), "generate.go", "")
		return ""
	}
	tmpl, err := template.New(name).Parse(source)
	if err != nil {
		g.addError(projector.CodeGenerationFailed, fmt.Sprintf("parse %s template: %v", name, err), "generate.go", "")
		return ""
	}
	var out bytes.Buffer
	if err := tmpl.Execute(&out, data); err != nil {
		g.addError(projector.CodeGenerationFailed, fmt.Sprintf("execute %s template: %v", name, err), "generate.go", "")
		return ""
	}
	return out.String()
}

func (g *nativeGenerator) alreadyGenerated(ptr string) bool {
	_, ok := g.generatedByPointer[ptr]
	return ok
}

func (g *nativeGenerator) addMarker(receiver, method string) {
	if g.markerMethods[receiver] == nil {
		g.markerMethods[receiver] = map[string]struct{}{}
	}
	g.markerMethods[receiver][method] = struct{}{}
}

func (g *nativeGenerator) addError(code, message, ptr, hint string) {
	g.diagnostics = append(g.diagnostics, projector.Diagnostic{
		Severity: projector.SeverityError,
		Code:     code,
		Message:  message,
		Pointer:  ptr,
		Hint:     hint,
	})
}

func (g *nativeGenerator) exportedIdentifier(value string) string {
	words := identifierWords(value)
	if len(words) == 0 {
		return "Value"
	}

	caps := capitalizationMap(g.cfg.Capitalizations)
	var out bytes.Buffer
	for _, word := range words {
		lower := strings.ToLower(word)
		if cap, ok := caps[lower]; ok {
			out.WriteString(cap)
			continue
		}
		runes := []rune(lower)
		if len(runes) == 0 {
			continue
		}
		out.WriteRune(unicode.ToUpper(runes[0]))
		for _, r := range runes[1:] {
			out.WriteRune(r)
		}
	}

	ident := out.String()
	if ident == "" {
		return "Value"
	}
	first := []rune(ident)[0]
	if !unicode.IsLetter(first) && first != '_' {
		ident = "N" + ident
	}
	if token.Lookup(ident).IsKeyword() {
		ident += "Value"
	}
	return ident
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedStructPropertyNames(node *schemaNode) []string {
	set := map[string]struct{}{}
	for name := range node.Properties {
		set[name] = struct{}{}
	}
	for name := range node.Required {
		set[name] = struct{}{}
	}

	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func unconstrainedProperty(parent *schemaNode, name string) *schemaNode {
	return &schemaNode{
		Pointer:                     childPointer(childPointer(parent.Pointer, "properties"), name),
		Properties:                  map[string]*schemaNode{},
		Required:                    map[string]bool{},
		AdditionalPropertiesAllowed: true,
	}
}

func sortedSet(set map[string]struct{}) []string {
	values := make([]string, 0, len(set))
	for value := range set {
		values = append(values, value)
	}
	sort.Strings(values)
	return values
}

func sortedMarkerReceivers(markers map[string]map[string]struct{}) []string {
	receivers := make([]string, 0, len(markers))
	for receiver := range markers {
		receivers = append(receivers, receiver)
	}
	sort.Strings(receivers)
	return receivers
}

func localDefinitionRef(ref string) (string, bool) {
	p, err := pointer.Parse(ref)
	if err != nil || len(p.Tokens) != 2 || p.Tokens[0] != "$defs" {
		return "", false
	}
	return p.Tokens[1], true
}

func withoutNull(types []string) []string {
	out := make([]string, 0, len(types))
	for _, typ := range types {
		if typ != "null" {
			out = append(out, typ)
		}
	}
	return out
}

func nullableGoType(typ goType) goType {
	if typ.Nilable || strings.HasPrefix(typ.Expr, "*") {
		return typ
	}
	return goType{Expr: "*" + typ.Expr, Nilable: true}
}

func tags(cfg projector.GoGenerateConfig) []string {
	if cfg.Tags != nil {
		return cfg.Tags
	}
	return []string{"json"}
}

func invalidStructTagKey(tag string) bool {
	return tag == "" || strings.ContainsAny(tag, " \t\r\n\"`:")
}

func stringValue(obj map[string]any, key string) string {
	value, _ := obj[key].(string)
	return value
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func markerMethodName(typeName string) string {
	return "is" + typeName
}

func docComment(name, description string) string {
	text := strings.TrimSpace(strings.ReplaceAll(description, "\n", " "))
	if text == "" {
		return name + " is generated from JSON Schema."
	}
	if strings.HasPrefix(text, name+" ") || strings.HasPrefix(text, name+".") {
		return text
	}
	return name + " " + text
}

func identifierWords(value string) []string {
	var words []string
	var current []rune
	runes := []rune(value)
	flush := func() {
		if len(current) == 0 {
			return
		}
		words = append(words, string(current))
		current = nil
	}

	for i, r := range runes {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if len(current) > 0 && unicode.IsUpper(r) {
				prev := current[len(current)-1]
				var next rune
				if i+1 < len(runes) {
					next = runes[i+1]
				}
				if unicode.IsLower(prev) || unicode.IsDigit(prev) || (unicode.IsUpper(prev) && next != 0 && unicode.IsLower(next)) {
					flush()
				}
			}
			current = append(current, r)
			continue
		}
		flush()
	}
	flush()
	return words
}

func capitalizationMap(extra []string) map[string]string {
	caps := map[string]string{
		"api":   "API",
		"http":  "HTTP",
		"https": "HTTPS",
		"id":    "ID",
		"json":  "JSON",
		"ui":    "UI",
		"url":   "URL",
		"a2ui":  "A2UI",
	}
	for _, value := range extra {
		if value == "" {
			continue
		}
		caps[strings.ToLower(value)] = value
	}
	return caps
}

// WriteGeneratedSource atomically writes generated Go source or sends it to stdout.
func WriteGeneratedSource(fileName string, source []byte, stdout io.Writer) error {
	if fileName == "-" {
		if stdout == nil {
			stdout = io.Discard
		}
		n, err := stdout.Write(source)
		if err != nil {
			return err
		}
		if n != len(source) {
			return io.ErrShortWrite
		}
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(fileName), 0o755); err != nil {
		return err
	}

	mode := os.FileMode(0o644)
	if info, err := os.Stat(fileName); err == nil {
		mode = info.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(fileName), ".jsonschema-projector-go-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	remove := true
	defer func() {
		_ = tmp.Close()
		if remove {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(mode); err != nil {
		return err
	}
	if n, err := tmp.Write(source); err != nil {
		return err
	} else if n != len(source) {
		return io.ErrShortWrite
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, fileName); err != nil {
		return err
	}
	remove = false
	return nil
}
