package generator

import (
	"context"
	"encoding/json"
	"fmt"
	"go/format"
	"go/token"
	"io"
	"maps"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/satorunooshie/jsonschema-projector/internal/pointer"
	"github.com/satorunooshie/jsonschema-projector/projector"
)

type schemaNode struct {
	Pointer string
	Bool    *bool

	Ref         string
	Title       string
	Description string
	XGoName     string
	Types       []string
	Format      string

	Properties        map[string]*schemaNode
	Required          map[string]bool
	PatternProperties map[string]*schemaNode

	Items                         *schemaNode
	TupleItems                    []*schemaNode
	TuplePointer                  string
	AdditionalProperties          *schemaNode
	AdditionalPropertiesSet       bool
	AdditionalPropertiesAllowed   bool
	AdditionalPropertiesIsBoolean bool

	OneOf []*schemaNode
	AnyOf []*schemaNode
	AllOf []*schemaNode

	Enum     []any
	Const    any
	HasConst bool
}

type goType struct {
	Expr         string
	Pointerable  bool
	Nilable      bool
	ForcePointer bool
}

type decl struct {
	Name    string
	Pointer string
	Body    string
}

type nativeGenerator struct {
	cfg    projector.GoGenerateConfig
	schema map[string]any

	root     *schemaNode
	defs     map[string]*schemaNode
	defOrder []string

	typeNames          map[string]string
	usedTypeNames      map[string]string
	generatedByPointer map[string]string
	decls              []decl
	markerMethods      map[string]map[string]struct{}
	diagnostics        projector.Diagnostics
}

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
	gen.normalizeAllOf()
	gen.reserveDefinitionTypeNames()
	return gen
}

func (g *nativeGenerator) Source() []byte {
	g.generateRootUnion()
	for _, name := range g.defOrder {
		g.generateDefinition(name, g.defs[name])
	}

	var b strings.Builder
	b.WriteString("// Code generated by jsonschema-projector; DO NOT EDIT.\n\n")
	fmt.Fprintf(&b, "package %s\n", g.cfg.Package)

	for _, d := range g.decls {
		b.WriteString("\n")
		b.WriteString(d.Body)
		if !strings.HasSuffix(d.Body, "\n") {
			b.WriteString("\n")
		}
	}

	for _, receiver := range sortedMarkerReceivers(g.markerMethods) {
		methods := sortedSet(g.markerMethods[receiver])
		for _, method := range methods {
			fmt.Fprintf(&b, "\nfunc (%s) %s() {}\n", receiver, method)
		}
	}

	source := []byte(b.String())
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

func (g *nativeGenerator) parseDefinitions() {
	rawDefs, ok := g.schema["$defs"]
	if !ok {
		return
	}

	defs, ok := rawDefs.(map[string]any)
	if !ok {
		g.addError(projector.CodeUnsupportedSchema, "$defs must be an object", "#/$defs", "")
		return
	}

	g.defOrder = sortedKeys(defs)
	for _, name := range g.defOrder {
		g.defs[name] = g.parseSchema(defs[name], childPointer("#/$defs", name))
	}
}

func (g *nativeGenerator) reserveDefinitionTypeNames() {
	for _, name := range g.defOrder {
		node := g.defs[name]
		typeName := g.exportedIdentifier(firstNonEmpty(node.XGoName, node.Title, name))
		g.typeNames[name] = g.reserveTypeName(typeName, node.Pointer, true)
	}
}

func (g *nativeGenerator) parseSchema(value any, ptr string) *schemaNode {
	node := &schemaNode{
		Pointer:                     ptr,
		Properties:                  map[string]*schemaNode{},
		Required:                    map[string]bool{},
		PatternProperties:           map[string]*schemaNode{},
		AdditionalPropertiesAllowed: true,
	}

	switch v := value.(type) {
	case bool:
		node.Bool = &v
		return node
	case map[string]any:
		g.parseSchemaObject(node, v)
		return node
	default:
		g.addError(projector.CodeUnsupportedSchema, "schema must be an object or boolean", ptr, "")
		return node
	}
}

func (g *nativeGenerator) parseSchemaObject(node *schemaNode, obj map[string]any) {
	for _, key := range []string{
		"not",
		"if",
		"then",
		"else",
		"propertyNames",
		"dependentSchemas",
		"dependentRequired",
		"dependencies",
		"unevaluatedProperties",
		"unevaluatedItems",
	} {
		if _, ok := obj[key]; ok {
			g.addError(
				projector.CodeUnsupportedSchema,
				fmt.Sprintf("%s is not supported by the native Go generator", key),
				childPointer(node.Pointer, key),
				"use jsonschema/v6 for runtime validation or simplify this schema before generation",
			)
		}
	}

	node.Ref = stringValue(obj, "$ref")
	node.Title = stringValue(obj, "title")
	node.Description = stringValue(obj, "description")
	node.XGoName = stringValue(obj, "x-go-name")
	node.Format = stringValue(obj, "format")

	if raw, ok := obj["type"]; ok {
		node.Types = g.parseTypeList(raw, childPointer(node.Pointer, "type"))
	}
	if raw, ok := obj["required"]; ok {
		node.Required = g.parseRequired(raw, childPointer(node.Pointer, "required"))
	}
	if raw, ok := obj["properties"]; ok {
		props, ok := raw.(map[string]any)
		if !ok {
			g.addError(projector.CodeUnsupportedSchema, "properties must be an object", childPointer(node.Pointer, "properties"), "")
		} else {
			for _, name := range sortedKeys(props) {
				node.Properties[name] = g.parseSchema(props[name], childPointer(childPointer(node.Pointer, "properties"), name))
			}
		}
	}
	if raw, ok := obj["patternProperties"]; ok {
		patterns, ok := raw.(map[string]any)
		if !ok {
			g.addError(projector.CodeUnsupportedSchema, "patternProperties must be an object", childPointer(node.Pointer, "patternProperties"), "")
		} else {
			for _, pattern := range sortedKeys(patterns) {
				node.PatternProperties[pattern] = g.parseSchema(patterns[pattern], childPointer(childPointer(node.Pointer, "patternProperties"), pattern))
			}
		}
	}
	if raw, ok := obj["items"]; ok {
		switch items := raw.(type) {
		case []any:
			node.TuplePointer = childPointer(node.Pointer, "items")
			node.TupleItems = make([]*schemaNode, 0, len(items))
			for i, item := range items {
				node.TupleItems = append(node.TupleItems, g.parseSchema(item, childPointer(childPointer(node.Pointer, "items"), strconv.Itoa(i))))
			}
		default:
			node.Items = g.parseSchema(raw, childPointer(node.Pointer, "items"))
		}
	}
	if raw, ok := obj["prefixItems"]; ok {
		items, ok := raw.([]any)
		if !ok {
			g.addError(projector.CodeUnsupportedSchema, "prefixItems must be an array", childPointer(node.Pointer, "prefixItems"), "")
		} else if len(node.TupleItems) > 0 {
			g.addError(projector.CodeUnsupportedSchema, "items and prefixItems cannot both define tuple elements", childPointer(node.Pointer, "prefixItems"), "use one tuple keyword")
		} else if node.Items != nil && (node.Items.Bool == nil || *node.Items.Bool) {
			g.addError(projector.CodeUnsupportedSchema, "prefixItems with schema-valued items is not supported by the native Go generator", childPointer(node.Pointer, "items"), "use prefixItems with items:false or a homogeneous items schema")
		} else {
			node.TuplePointer = childPointer(node.Pointer, "prefixItems")
			node.TupleItems = make([]*schemaNode, 0, len(items))
			for i, item := range items {
				node.TupleItems = append(node.TupleItems, g.parseSchema(item, childPointer(childPointer(node.Pointer, "prefixItems"), strconv.Itoa(i))))
			}
		}
	}
	if raw, ok := obj["additionalItems"]; ok {
		if allowed, ok := raw.(bool); !ok {
			g.addError(projector.CodeUnsupportedSchema, "additionalItems must be a boolean", childPointer(node.Pointer, "additionalItems"), "")
		} else if allowed && len(node.TupleItems) > 0 {
			g.addError(projector.CodeUnsupportedSchema, "open tuple arrays are not supported by the native Go generator", childPointer(node.Pointer, "additionalItems"), "use a fixed tuple or a homogeneous items schema")
		}
	}
	if raw, ok := obj["additionalProperties"]; ok {
		node.AdditionalPropertiesSet = true
		switch v := raw.(type) {
		case bool:
			node.AdditionalPropertiesAllowed = v
			node.AdditionalPropertiesIsBoolean = true
		default:
			node.AdditionalPropertiesAllowed = true
			node.AdditionalProperties = g.parseSchema(v, childPointer(node.Pointer, "additionalProperties"))
		}
	}
	if raw, ok := obj["oneOf"]; ok {
		items, ok := raw.([]any)
		if !ok {
			g.addError(projector.CodeUnsupportedSchema, "oneOf must be an array", childPointer(node.Pointer, "oneOf"), "")
		} else {
			node.OneOf = make([]*schemaNode, 0, len(items))
			for i, item := range items {
				node.OneOf = append(node.OneOf, g.parseSchema(item, childPointer(childPointer(node.Pointer, "oneOf"), strconv.Itoa(i))))
			}
		}
	}
	if raw, ok := obj["anyOf"]; ok {
		items, ok := raw.([]any)
		if !ok {
			g.addError(projector.CodeUnsupportedSchema, "anyOf must be an array", childPointer(node.Pointer, "anyOf"), "")
		} else {
			node.AnyOf = make([]*schemaNode, 0, len(items))
			for i, item := range items {
				node.AnyOf = append(node.AnyOf, g.parseSchema(item, childPointer(childPointer(node.Pointer, "anyOf"), strconv.Itoa(i))))
			}
		}
	}
	if raw, ok := obj["allOf"]; ok {
		items, ok := raw.([]any)
		if !ok {
			g.addError(projector.CodeUnsupportedSchema, "allOf must be an array", childPointer(node.Pointer, "allOf"), "")
		} else {
			node.AllOf = make([]*schemaNode, 0, len(items))
			for i, item := range items {
				node.AllOf = append(node.AllOf, g.parseSchema(item, childPointer(childPointer(node.Pointer, "allOf"), strconv.Itoa(i))))
			}
		}
	}
	if raw, ok := obj["enum"]; ok {
		items, ok := raw.([]any)
		if !ok {
			g.addError(projector.CodeUnsupportedSchema, "enum must be an array", childPointer(node.Pointer, "enum"), "")
		} else {
			node.Enum = append([]any(nil), items...)
		}
	}
	if raw, ok := obj["const"]; ok {
		node.Const = raw
		node.HasConst = true
	}
}

// normalizeAllOf flattens the object-only allOf form supported by the native
// DTO generator. The full JSON Schema semantics are still checked separately
// by jsonschema/v6; this pass only builds the equivalent Go struct shape.
func (g *nativeGenerator) normalizeAllOf() {
	for _, name := range g.defOrder {
		g.normalizeNode(g.defs[name], map[*schemaNode]bool{})
	}
	g.normalizeNode(g.root, map[*schemaNode]bool{})
}

// normalizeAnyOf collapses anyOf only when every branch has the same DTO
// shape. A heterogeneous anyOf needs runtime dispatch and cannot be represented
// safely by the native DTO generator.
func (g *nativeGenerator) normalizeAnyOf(node *schemaNode) {
	if node == nil || len(node.AnyOf) == 0 {
		return
	}
	first := node.AnyOf[0]
	for i, variant := range node.AnyOf[1:] {
		if !schemaNodesEquivalent(first, variant, map[[2]*schemaNode]bool{}) {
			g.addError(
				projector.CodeUnsupportedSchema,
				"anyOf members do not have a single representable DTO shape",
				childPointer(childPointer(node.Pointer, "anyOf"), strconv.Itoa(i+1)),
				"use oneOf with named $defs entries for a closed union",
			)
			return
		}
	}
	if len(node.Types) == 0 {
		node.Types = slices.Clone(first.Types)
	}
	if node.Ref == "" {
		node.Ref = first.Ref
	}
	if len(node.Properties) == 0 && len(first.Properties) > 0 {
		node.Properties = first.Properties
	}
	if len(node.Required) == 0 && len(first.Required) > 0 {
		node.Required = maps.Clone(first.Required)
	}
	if node.Items == nil {
		node.Items = first.Items
	}
	node.AnyOf = nil
}

func (g *nativeGenerator) normalizeNode(node *schemaNode, stack map[*schemaNode]bool) {
	if node == nil || stack[node] {
		if node != nil && stack[node] {
			g.addError(
				projector.CodeUnsupportedSchema,
				"recursive allOf composition is not supported",
				node.Pointer,
				"break the allOf cycle or keep recursion outside allOf",
			)
		}
		return
	}
	stack[node] = true
	defer delete(stack, node)

	for _, prop := range node.Properties {
		g.normalizeNode(prop, stack)
	}
	g.normalizeNode(node.Items, stack)
	for _, item := range node.TupleItems {
		g.normalizeNode(item, stack)
	}
	g.normalizeNode(node.AdditionalProperties, stack)
	for _, variant := range node.OneOf {
		g.normalizeNode(variant, stack)
	}
	for _, variant := range node.AnyOf {
		g.normalizeNode(variant, stack)
	}
	for _, member := range node.AllOf {
		g.normalizeNode(member, stack)
	}

	if len(node.AllOf) == 0 {
		g.normalizeAnyOf(node)
		return
	}
	if node.Ref != "" {
		g.addError(
			projector.CodeUnsupportedSchema,
			"$ref combined with allOf is not supported by the native Go generator",
			node.Pointer,
			"put the reference in an allOf member instead",
		)
		return
	}
	if len(node.OneOf) > 0 {
		g.addError(
			projector.CodeUnsupportedSchema,
			"allOf combined with oneOf is not supported by the native Go generator",
			childPointer(node.Pointer, "allOf"),
			"use a named oneOf definition outside the allOf composition",
		)
		return
	}

	merged := node
	for i, member := range node.AllOf {
		resolved := member
		if member.Ref != "" {
			if len(member.Types) > 0 || len(member.Properties) > 0 || len(member.Required) > 0 || len(member.PatternProperties) > 0 || member.Items != nil || len(member.TupleItems) > 0 || member.AdditionalPropertiesSet || len(member.OneOf) > 0 || len(member.AnyOf) > 0 || len(member.AllOf) > 0 || len(member.Enum) > 0 || member.HasConst {
				g.addError(projector.CodeUnsupportedSchema, "$ref with sibling keywords in allOf is not supported", member.Pointer, "put sibling keywords in a separate object schema")
				continue
			}
			name, ok := localDefinitionRef(member.Ref)
			if !ok {
				g.addError(projector.CodeUnsupportedSchema, fmt.Sprintf("allOf only supports local direct $defs refs, got %q", member.Ref), member.Pointer, "project external or nested references into top-level $defs before generation")
				continue
			}
			resolved, ok = g.defs[name]
			if !ok {
				g.addError(projector.CodeUnsupportedSchema, fmt.Sprintf("referenced definition %q was not found", name), member.Pointer, "")
				continue
			}
			g.normalizeNode(resolved, stack)
		}

		if resolved.Bool != nil {
			if !*resolved.Bool {
				g.addError(projector.CodeUnsupportedSchema, "false schemas cannot be represented in an allOf composition", resolved.Pointer, "")
			}
			continue
		}
		resolvedTypes := withoutNull(resolved.Types)
		if len(resolvedTypes) > 0 && (len(resolvedTypes) != 1 || resolvedTypes[0] != "object") {
			if !g.mergeNonObjectAllOf(merged, resolved) {
				g.addError(projector.CodeUnsupportedSchema, "allOf members have incompatible generated shapes", childPointer(childPointer(node.Pointer, "allOf"), strconv.Itoa(i)), "allOf members must describe the same Go DTO shape")
			}
			continue
		}
		if !g.isObjectComposition(resolved) {
			if !g.mergeNonObjectAllOf(merged, resolved) {
				g.addError(projector.CodeUnsupportedSchema, "allOf members have incompatible generated shapes", childPointer(childPointer(node.Pointer, "allOf"), strconv.Itoa(i)), "allOf members must describe the same Go DTO shape")
			}
			continue
		}

		for name, prop := range resolved.Properties {
			if existing, exists := merged.Properties[name]; exists && !schemaNodesEquivalent(existing, prop, map[[2]*schemaNode]bool{}) {
				g.addError(projector.CodeUnsupportedSchema, fmt.Sprintf("allOf defines property %q more than once", name), prop.Pointer, "merge the property definitions before generation")
				continue
			}
			merged.Properties[name] = prop
		}
		for pattern, prop := range resolved.PatternProperties {
			if len(merged.Properties) > 0 || len(merged.PatternProperties) > 0 {
				g.addError(projector.CodeUnsupportedSchema, "patternProperties cannot be combined with named properties or another pattern in an allOf composition", prop.Pointer, "use one uniform patternProperties schema")
				continue
			}
			merged.PatternProperties[pattern] = prop
		}
		for name := range resolved.Required {
			merged.Required[name] = true
		}
		if resolved.AdditionalPropertiesSet {
			if resolved.AdditionalProperties != nil {
				g.addError(projector.CodeUnsupportedSchema, "schema-valued additionalProperties in allOf is not supported", resolved.Pointer, "")
			} else if !resolved.AdditionalPropertiesAllowed {
				merged.AdditionalPropertiesSet = true
				merged.AdditionalPropertiesAllowed = false
				merged.AdditionalPropertiesIsBoolean = true
			}
		}
		if merged.Description == "" {
			merged.Description = resolved.Description
		}
	}

	if len(merged.Types) == 0 {
		merged.Types = []string{"object"}
	}
	merged.AllOf = nil
	g.normalizeAnyOf(merged)
}

func (g *nativeGenerator) isObjectComposition(node *schemaNode) bool {
	return g.schemaKind(node) == "object"
}

func (g *nativeGenerator) schemaKind(node *schemaNode) string {
	if node == nil || node.Bool != nil || len(node.OneOf) > 0 {
		return ""
	}
	types := withoutNull(node.Types)
	if len(types) > 1 {
		return ""
	}
	if len(types) == 1 {
		return types[0]
	}
	if len(node.Properties) > 0 || len(node.Required) > 0 || len(node.PatternProperties) > 0 || node.AdditionalPropertiesSet {
		return "object"
	}
	if node.Items != nil || len(node.TupleItems) > 0 {
		return "array"
	}
	return ""
}

// mergeNonObjectAllOf merges only the generated shape of non-object members.
// Validation keywords remain in the projected schema and are enforced by the
// separate JSON Schema validator; the native generator only needs a compatible
// Go representation.
func (g *nativeGenerator) mergeNonObjectAllOf(merged, member *schemaNode) bool {
	memberKind := g.schemaKind(member)
	if memberKind == "" {
		return len(member.Types) == 0 && len(member.Properties) == 0 && len(member.PatternProperties) == 0 && member.Items == nil && len(member.TupleItems) == 0 && !member.AdditionalPropertiesSet
	}
	mergedKind := g.schemaKind(merged)
	if mergedKind != "" && mergedKind != memberKind {
		return false
	}
	if mergedKind == "" {
		merged.Types = slices.Clone(member.Types)
		if member.Items != nil {
			merged.Items = member.Items
		}
		if member.HasConst {
			merged.Const = member.Const
			merged.HasConst = true
		}
		if len(member.Enum) > 0 {
			merged.Enum = slices.Clone(member.Enum)
		}
		return true
	}
	if memberKind == "array" {
		if len(merged.TupleItems) > 0 || len(member.TupleItems) > 0 {
			if len(merged.TupleItems) != len(member.TupleItems) {
				return false
			}
			for i := range merged.TupleItems {
				if !schemaNodesEquivalent(merged.TupleItems[i], member.TupleItems[i], map[[2]*schemaNode]bool{}) {
					return false
				}
			}
		}
		if merged.Items != nil && member.Items != nil && !schemaNodesEquivalent(merged.Items, member.Items, map[[2]*schemaNode]bool{}) {
			return false
		}
		if merged.Items == nil {
			merged.Items = member.Items
		}
	}
	if merged.HasConst && member.HasConst && !jsonValuesEqual(merged.Const, member.Const) {
		return false
	}
	if !merged.HasConst && member.HasConst {
		merged.Const = member.Const
		merged.HasConst = true
	}
	return true
}

// schemaNodesEquivalent compares the parts of a schema node that affect the
// generated DTO shape. Source pointers and documentation are intentionally not
// compared, so the same property can be contributed by two equivalent refs.
func schemaNodesEquivalent(a, b *schemaNode, seen map[[2]*schemaNode]bool) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a == b {
		return true
	}
	pair := [2]*schemaNode{a, b}
	if seen[pair] {
		return true
	}
	seen[pair] = true

	if !boolPointersEqual(a.Bool, b.Bool) || a.Ref != b.Ref || a.XGoName != b.XGoName || a.Format != b.Format || !slices.Equal(a.Types, b.Types) ||
		!maps.Equal(a.Required, b.Required) || a.AdditionalPropertiesSet != b.AdditionalPropertiesSet ||
		a.AdditionalPropertiesAllowed != b.AdditionalPropertiesAllowed || a.AdditionalPropertiesIsBoolean != b.AdditionalPropertiesIsBoolean ||
		a.HasConst != b.HasConst || !jsonValuesEqual(a.Const, b.Const) || !jsonValuesEqual(a.Enum, b.Enum) {
		return false
	}
	if !schemaNodesEquivalent(a.Items, b.Items, seen) || !schemaNodesEquivalent(a.AdditionalProperties, b.AdditionalProperties, seen) || len(a.TupleItems) != len(b.TupleItems) {
		return false
	}
	for i := range a.TupleItems {
		if !schemaNodesEquivalent(a.TupleItems[i], b.TupleItems[i], seen) {
			return false
		}
	}
	if len(a.Properties) != len(b.Properties) {
		return false
	}
	if len(a.PatternProperties) != len(b.PatternProperties) {
		return false
	}
	for pattern, prop := range a.PatternProperties {
		if !schemaNodesEquivalent(prop, b.PatternProperties[pattern], seen) {
			return false
		}
	}
	for name, prop := range a.Properties {
		if !schemaNodesEquivalent(prop, b.Properties[name], seen) {
			return false
		}
	}
	if len(a.OneOf) != len(b.OneOf) || len(a.AnyOf) != len(b.AnyOf) || len(a.AllOf) != len(b.AllOf) {
		return false
	}
	for i := range a.OneOf {
		if !schemaNodesEquivalent(a.OneOf[i], b.OneOf[i], seen) {
			return false
		}
	}
	for i := range a.AnyOf {
		if !schemaNodesEquivalent(a.AnyOf[i], b.AnyOf[i], seen) {
			return false
		}
	}
	for i := range a.AllOf {
		if !schemaNodesEquivalent(a.AllOf[i], b.AllOf[i], seen) {
			return false
		}
	}
	return true
}

func boolPointersEqual(a, b *bool) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func jsonValuesEqual(a, b any) bool {
	return reflect.DeepEqual(a, b)
}

func (g *nativeGenerator) parseTypeList(raw any, ptr string) []string {
	switch value := raw.(type) {
	case string:
		return []string{value}
	case []any:
		types := make([]string, 0, len(value))
		for i, item := range value {
			s, ok := item.(string)
			if !ok {
				g.addError(projector.CodeUnsupportedSchema, "type array entries must be strings", childPointer(ptr, strconv.Itoa(i)), "")
				continue
			}
			types = append(types, s)
		}
		return types
	default:
		g.addError(projector.CodeUnsupportedSchema, "type must be a string or array of strings", ptr, "")
		return nil
	}
}

func (g *nativeGenerator) parseRequired(raw any, ptr string) map[string]bool {
	required := map[string]bool{}
	items, ok := raw.([]any)
	if !ok {
		g.addError(projector.CodeUnsupportedSchema, "required must be an array", ptr, "")
		return required
	}
	for i, item := range items {
		name, ok := item.(string)
		if !ok {
			g.addError(projector.CodeUnsupportedSchema, "required entries must be strings", childPointer(ptr, strconv.Itoa(i)), "")
			continue
		}
		required[name] = true
	}
	return required
}

func (g *nativeGenerator) generateRootUnion() {
	if len(g.root.OneOf) == 0 {
		return
	}

	name := g.rootUnionTypeName()
	g.addUnionDecl(name, g.root, true)
}

func (g *nativeGenerator) rootUnionTypeName() string {
	if g.cfg.RootType != "" {
		return g.reserveTypeName(g.exportedIdentifier(g.cfg.RootType), "#", true)
	}

	for _, candidate := range []string{g.root.Title, "Root"} {
		if candidate == "" {
			continue
		}
		name := g.exportedIdentifier(candidate)
		if _, exists := g.usedTypeNames[name]; !exists {
			return g.reserveTypeName(name, "#", false)
		}
	}

	return g.reserveTypeName("Root", "#", false)
}

func (g *nativeGenerator) generateDefinition(name string, node *schemaNode) {
	typeName := g.typeNames[name]
	if g.alreadyGenerated(node.Pointer) {
		return
	}

	switch {
	case len(node.OneOf) > 0:
		g.addUnionDecl(typeName, node, false)
	case g.isStructSchema(node):
		g.addStructDecl(typeName, node)
	default:
		underlying := g.schemaGoType(node, typeName)
		g.addAliasDecl(typeName, underlying.Expr, node)
	}
}

func (g *nativeGenerator) addUnionDecl(name string, node *schemaNode, root bool) {
	if g.alreadyGenerated(node.Pointer) {
		return
	}

	members, ok := g.oneOfMembers(node)
	if !ok {
		return
	}

	methodName := markerMethodName(name)
	for _, defName := range members {
		member := g.defs[defName]
		if len(member.OneOf) > 0 {
			g.addError(
				projector.CodeUnsupportedSchema,
				"oneOf members must be concrete $defs entries, not another oneOf union",
				member.Pointer,
				"",
			)
			continue
		}
		g.addMarker(g.typeNames[defName], methodName)
	}

	var b strings.Builder
	if node.Description != "" {
		fmt.Fprintf(&b, "// %s\n", docComment(name, node.Description))
	}
	fmt.Fprintf(&b, "type %s interface {\n\t%s()\n}\n", name, methodName)
	g.addDecl(name, node.Pointer, b.String())

	_ = root
}

func (g *nativeGenerator) addStructDecl(name string, node *schemaNode) {
	if g.alreadyGenerated(node.Pointer) {
		return
	}

	var fields []string
	usedFields := map[string]string{}
	for _, propertyName := range sortedStructPropertyNames(node) {
		prop := node.Properties[propertyName]
		if prop == nil {
			prop = unconstrainedProperty(node, propertyName)
		}
		fieldName := g.exportedIdentifier(firstNonEmpty(prop.XGoName, propertyName))
		if previous, exists := usedFields[fieldName]; exists {
			g.addError(
				projector.CodeNameConflict,
				fmt.Sprintf("properties %q and %q both generate field name %s", previous, propertyName, fieldName),
				prop.Pointer,
				"",
			)
			continue
		}
		usedFields[fieldName] = propertyName

		fieldType := g.fieldGoType(prop, name+fieldName, node.Required[propertyName])
		tag, ok := g.structTag(propertyName, node.Required[propertyName], prop.Pointer)
		if !ok {
			continue
		}

		line := fmt.Sprintf("\t%s %s", fieldName, fieldType)
		if tag != "" {
			line += fmt.Sprintf(" `%s`", tag)
		}
		if prop.Description != "" {
			fields = append(fields, fmt.Sprintf("\t// %s\n%s", docComment(fieldName, prop.Description), line))
		} else {
			fields = append(fields, line)
		}
	}

	var b strings.Builder
	if node.Description != "" {
		fmt.Fprintf(&b, "// %s\n", docComment(name, node.Description))
	}
	fmt.Fprintf(&b, "type %s struct {\n", name)
	if len(fields) > 0 {
		b.WriteString(strings.Join(fields, "\n"))
		b.WriteString("\n")
	}
	b.WriteString("}\n")
	g.addDecl(name, node.Pointer, b.String())
}

func (g *nativeGenerator) addAliasDecl(name, underlying string, node *schemaNode) {
	if g.alreadyGenerated(node.Pointer) {
		return
	}
	if underlying == "" {
		underlying = "any"
	}

	var b strings.Builder
	if node.Description != "" {
		fmt.Fprintf(&b, "// %s\n", docComment(name, node.Description))
	}
	fmt.Fprintf(&b, "type %s %s\n", name, underlying)
	if constants := g.enumConstants(name, node); len(constants) > 0 {
		b.WriteString("\nconst (\n")
		for _, constant := range constants {
			fmt.Fprintf(&b, "\t%s %s = %s\n", constant.Name, name, constant.Value)
		}
		b.WriteString(")\n")
	}
	g.addDecl(name, node.Pointer, b.String())
}

func (g *nativeGenerator) schemaGoType(node *schemaNode, contextName string) goType {
	if node.Bool != nil {
		if *node.Bool {
			return goType{Expr: "any", Nilable: true}
		}
		g.addError(projector.CodeUnsupportedSchema, "false schemas cannot be represented as Go DTO types", node.Pointer, "")
		return goType{Expr: "any", Nilable: true}
	}

	if node.Ref != "" {
		return g.refGoType(node.Ref, node.Pointer)
	}

	if len(node.OneOf) > 0 {
		return g.inlineUnionType(node, contextName)
	}

	types := withoutNull(node.Types)
	nullable := len(types) != len(node.Types)
	if len(types) > 1 {
		g.addError(
			projector.CodeUnsupportedSchema,
			fmt.Sprintf("multiple non-null JSON types are not supported: %s", strings.Join(types, ", ")),
			childPointer(node.Pointer, "type"),
			"use oneOf with named $defs entries if this should be a closed union",
		)
		return goType{Expr: "any", Nilable: true}
	}
	if len(types) == 0 && len(node.Types) > 0 {
		g.addError(projector.CodeUnsupportedSchema, "null-only schemas cannot be represented as useful Go DTO types", node.Pointer, "")
		return goType{Expr: "any", Nilable: true}
	}

	var out goType
	switch {
	case len(types) == 0:
		out = g.inferredGoType(node, contextName)
	case types[0] == "object":
		out = g.objectGoType(node, contextName)
	case types[0] == "array":
		out = g.arrayGoType(node, contextName)
	case types[0] == "string":
		out = g.stringGoType(node)
	case types[0] == "integer":
		out = goType{Expr: "int", Pointerable: true}
	case types[0] == "number":
		out = goType{Expr: "float64", Pointerable: true}
	case types[0] == "boolean":
		out = goType{Expr: "bool", Pointerable: true}
	default:
		g.addError(projector.CodeUnsupportedSchema, fmt.Sprintf("JSON type %q is not supported", types[0]), childPointer(node.Pointer, "type"), "")
		out = goType{Expr: "any", Nilable: true}
	}

	if nullable {
		out = nullableGoType(out)
	}
	return out
}

func (g *nativeGenerator) inferredGoType(node *schemaNode, contextName string) goType {
	switch {
	case len(node.Properties) > 0 || len(node.Required) > 0 || len(node.PatternProperties) > 0 || node.AdditionalPropertiesSet:
		return g.objectGoType(node, contextName)
	case len(node.Enum) > 0:
		return g.enumGoType(node)
	case node.HasConst:
		return g.constGoType(node.Const, node.Pointer)
	default:
		return goType{Expr: "any", Nilable: true}
	}
}

func (g *nativeGenerator) objectGoType(node *schemaNode, contextName string) goType {
	if len(node.PatternProperties) > 0 {
		if len(node.Properties) > 0 || len(node.PatternProperties) > 1 {
			g.addError(projector.CodeUnsupportedSchema, "native Go generation supports at most one uniform patternProperties schema", node.Pointer, "use a struct for named properties or a single pattern schema for a map")
			return goType{Expr: "any", Nilable: true}
		}
		var pattern *schemaNode
		for _, candidate := range node.PatternProperties {
			pattern = candidate
		}
		if node.AdditionalProperties != nil && !schemaNodesEquivalent(node.AdditionalProperties, pattern, map[[2]*schemaNode]bool{}) {
			g.addError(projector.CodeUnsupportedSchema, "patternProperties and additionalProperties have incompatible value shapes", node.Pointer, "use one uniform map value schema")
			return goType{Expr: "any", Nilable: true}
		}
		return goType{Expr: "map[string]" + g.schemaGoType(pattern, contextName+"Value").Expr, Nilable: true}
	}
	if len(node.Properties) > 0 || len(node.Required) > 0 || (node.AdditionalPropertiesSet && !node.AdditionalPropertiesAllowed) {
		name := g.reserveTypeName(g.exportedIdentifier(contextName), node.Pointer, false)
		g.addStructDecl(name, node)
		return goType{Expr: name, Pointerable: true}
	}

	if node.AdditionalProperties != nil {
		valueType := g.schemaGoType(node.AdditionalProperties, contextName+"Value")
		return goType{Expr: "map[string]" + valueType.Expr, Nilable: true}
	}

	return goType{Expr: "map[string]any", Nilable: true}
}

func (g *nativeGenerator) arrayGoType(node *schemaNode, contextName string) goType {
	if len(node.TupleItems) > 0 {
		first := node.TupleItems[0]
		for i, item := range node.TupleItems[1:] {
			if !schemaNodesEquivalent(first, item, map[[2]*schemaNode]bool{}) {
				g.addError(projector.CodeUnsupportedSchema, "heterogeneous tuple arrays are not supported by the native Go generator", childPointer(node.TuplePointer, strconv.Itoa(i+1)), "use a homogeneous tuple or a single-schema items array")
				return goType{Expr: "any", Nilable: true}
			}
		}
		itemType := g.schemaGoType(first, contextName+"Item")
		return goType{Expr: fmt.Sprintf("[%d]%s", len(node.TupleItems), itemType.Expr), Nilable: true}
	}
	if node.Items == nil {
		return goType{Expr: "[]any", Nilable: true}
	}
	itemType := g.schemaGoType(node.Items, contextName+"Item")
	return goType{Expr: "[]" + itemType.Expr, Nilable: true}
}

func (g *nativeGenerator) stringGoType(node *schemaNode) goType {
	if len(node.Enum) > 0 {
		return g.enumGoType(node)
	}
	return goType{Expr: "string", Pointerable: true}
}

func (g *nativeGenerator) enumGoType(node *schemaNode) goType {
	expected := ""
	if types := withoutNull(node.Types); len(types) == 1 {
		expected = types[0]
	}
	for i, item := range node.Enum {
		kind, ok := enumValueKind(item)
		if !ok || (expected != "" && !enumKindsCompatible(expected, kind)) {
			g.addError(projector.CodeUnsupportedSchema, "enum values are not compatible with the schema type", childPointer(childPointer(node.Pointer, "enum"), strconv.Itoa(i)), "use values of one JSON primitive type")
			return goType{Expr: "any", Nilable: true}
		}
	}

	kind := ""
	if len(node.Enum) > 0 {
		kind, _ = enumValueKind(node.Enum[0])
	}
	switch firstNonEmpty(expected, kind) {
	case "boolean":
		return goType{Expr: "bool", Pointerable: true}
	case "integer":
		return goType{Expr: "int", Pointerable: true}
	case "number":
		return goType{Expr: "float64", Pointerable: true}
	default:
		return goType{Expr: "string", Pointerable: true}
	}
}

func enumValueKind(value any) (string, bool) {
	switch v := value.(type) {
	case string:
		return "string", true
	case bool:
		return "boolean", true
	case json.Number:
		if strings.ContainsAny(v.String(), ".eE") {
			return "number", true
		}
		return "integer", true
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return "integer", true
	case float32:
		if math.Trunc(float64(v)) == float64(v) {
			return "integer", true
		}
		return "number", true
	case float64:
		if math.Trunc(v) == v {
			return "integer", true
		}
		return "number", true
	default:
		return "", false
	}
}

func enumKindsCompatible(schemaType, valueKind string) bool {
	if schemaType == "number" && valueKind == "integer" {
		return true
	}
	return schemaType == valueKind
}

func (g *nativeGenerator) constGoType(value any, ptr string) goType {
	switch v := value.(type) {
	case string:
		return goType{Expr: "string", Pointerable: true}
	case bool:
		return goType{Expr: "bool", Pointerable: true}
	case json.Number:
		if strings.ContainsAny(v.String(), ".eE") {
			return goType{Expr: "float64", Pointerable: true}
		}
		return goType{Expr: "int", Pointerable: true}
	case float64, float32:
		return goType{Expr: "float64", Pointerable: true}
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return goType{Expr: "int", Pointerable: true}
	case nil:
		g.addError(projector.CodeUnsupportedSchema, "null const values cannot be represented as useful Go DTO types", childPointer(ptr, "const"), "")
		return goType{Expr: "any", Nilable: true}
	default:
		g.addError(projector.CodeUnsupportedSchema, fmt.Sprintf("const value of type %T is not supported", value), childPointer(ptr, "const"), "")
		return goType{Expr: "any", Nilable: true}
	}
}

func (g *nativeGenerator) fieldGoType(node *schemaNode, contextName string, required bool) string {
	typ := g.schemaGoType(node, contextName)
	if typ.ForcePointer && !strings.HasPrefix(typ.Expr, "*") {
		return "*" + typ.Expr
	}
	if required {
		return typ.Expr
	}
	if typ.Nilable {
		return typ.Expr
	}
	if typ.Pointerable {
		return "*" + typ.Expr
	}
	return typ.Expr
}

func (g *nativeGenerator) refGoType(ref, ptr string) goType {
	defName, ok := localDefinitionRef(ref)
	if !ok {
		g.addError(
			projector.CodeUnsupportedSchema,
			fmt.Sprintf("only local direct $defs refs are supported, got %q", ref),
			ptr,
			"project external or nested references into top-level $defs before generation",
		)
		return goType{Expr: "any", Nilable: true}
	}

	def, ok := g.defs[defName]
	if !ok {
		g.addError(projector.CodeUnsupportedSchema, fmt.Sprintf("referenced definition %q was not found", defName), ptr, "")
		return goType{Expr: "any", Nilable: true}
	}

	typeName := g.typeNames[defName]
	if len(def.OneOf) > 0 {
		return goType{Expr: typeName, Nilable: true}
	}
	if g.isStructSchema(def) {
		return goType{Expr: typeName, Pointerable: true, ForcePointer: true}
	}
	return goType{Expr: typeName, Pointerable: true}
}

func (g *nativeGenerator) inlineUnionType(node *schemaNode, contextName string) goType {
	if name, ok := g.generatedByPointer[node.Pointer]; ok {
		return goType{Expr: name, Nilable: true}
	}

	name := g.reserveTypeName(g.exportedIdentifier(contextName), node.Pointer, false)
	g.addUnionDecl(name, node, false)
	return goType{Expr: name, Nilable: true}
}

func (g *nativeGenerator) oneOfMembers(node *schemaNode) ([]string, bool) {
	if len(node.OneOf) == 0 {
		g.addError(projector.CodeUnsupportedSchema, "oneOf must contain at least one member", node.Pointer, "")
		return nil, false
	}

	members := make([]string, 0, len(node.OneOf))
	seen := map[string]struct{}{}
	for _, item := range node.OneOf {
		if item.Ref == "" {
			g.addError(
				projector.CodeUnsupportedSchema,
				"native oneOf support requires every member to be a $ref",
				item.Pointer,
				"move inline oneOf members into $defs and reference them from oneOf",
			)
			continue
		}
		defName, ok := localDefinitionRef(item.Ref)
		if !ok {
			g.addError(
				projector.CodeUnsupportedSchema,
				fmt.Sprintf("native oneOf support requires local direct $defs refs, got %q", item.Ref),
				item.Pointer,
				"",
			)
			continue
		}
		if _, ok := g.defs[defName]; !ok {
			g.addError(projector.CodeUnsupportedSchema, fmt.Sprintf("oneOf member definition %q was not found", defName), item.Pointer, "")
			continue
		}
		if _, ok := seen[defName]; ok {
			continue
		}
		seen[defName] = struct{}{}
		members = append(members, defName)
	}

	return members, len(members) > 0 && !g.diagnostics.HasErrors()
}

func (g *nativeGenerator) isStructSchema(node *schemaNode) bool {
	if node == nil || node.Bool != nil || node.Ref != "" || len(node.OneOf) > 0 {
		return false
	}
	types := withoutNull(node.Types)
	if len(types) > 0 && !(len(types) == 1 && types[0] == "object") {
		return false
	}
	return len(node.Properties) > 0 || len(node.Required) > 0 || (node.AdditionalPropertiesSet && !node.AdditionalPropertiesAllowed)
}

func (g *nativeGenerator) structTag(propertyName string, required bool, ptr string) (string, bool) {
	if strings.Contains(propertyName, "`") {
		g.addError(projector.CodeUnsupportedSchema, "property names containing backticks cannot be represented in Go struct tags", ptr, "")
		return "", false
	}

	tagValue := propertyName
	if !required {
		tagValue += ",omitempty"
	}

	var parts []string
	for _, tag := range tags(g.cfg) {
		parts = append(parts, tag+":"+strconv.Quote(tagValue))
	}
	return strings.Join(parts, " "), true
}

type enumConstant struct {
	Name  string
	Value string
}

func (g *nativeGenerator) enumConstants(typeName string, node *schemaNode) []enumConstant {
	if len(node.Enum) == 0 {
		return nil
	}

	constants := make([]enumConstant, 0, len(node.Enum))
	used := map[string]int{}
	for i, item := range node.Enum {
		value, kind, ok := enumValueLiteral(item)
		if !ok {
			return nil
		}
		name := typeName + g.exportedIdentifier(firstNonEmpty(value, "Empty"))
		if kind != "string" {
			name = typeName + "Value" + strconv.Itoa(i+1)
		}
		if count := used[name]; count > 0 {
			used[name] = count + 1
			name += strconv.Itoa(count + 1)
		} else {
			used[name] = 1
		}
		constants = append(constants, enumConstant{
			Name:  name,
			Value: value,
		})
	}
	return constants
}

func enumValueLiteral(value any) (literal, kind string, ok bool) {
	kind, ok = enumValueKind(value)
	if !ok {
		return "", "", false
	}
	switch v := value.(type) {
	case string:
		return strconv.Quote(v), kind, true
	case bool:
		return strconv.FormatBool(v), kind, true
	case json.Number:
		return v.String(), kind, true
	case int:
		return strconv.Itoa(v), kind, true
	case int8:
		return strconv.FormatInt(int64(v), 10), kind, true
	case int16:
		return strconv.FormatInt(int64(v), 10), kind, true
	case int32:
		return strconv.FormatInt(int64(v), 10), kind, true
	case int64:
		return strconv.FormatInt(v, 10), kind, true
	case uint:
		return strconv.FormatUint(uint64(v), 10), kind, true
	case uint8:
		return strconv.FormatUint(uint64(v), 10), kind, true
	case uint16:
		return strconv.FormatUint(uint64(v), 10), kind, true
	case uint32:
		return strconv.FormatUint(uint64(v), 10), kind, true
	case uint64:
		return strconv.FormatUint(v, 10), kind, true
	case float32:
		return strconv.FormatFloat(float64(v), 'g', -1, 32), kind, true
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64), kind, true
	default:
		return "", "", false
	}
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
	var out strings.Builder
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
