package generator

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/satorunooshie/jsonschema-projector/projector"
)

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

	if len(node.OneOf) > 0 || len(node.AnyOf) > 0 {
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
		return goType{Expr: "struct{}", Pointerable: true}
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

func (g *nativeGenerator) unionVariants(unionName string, node *schemaNode) ([]unionVariant, bool) {
	items, keyword := node.OneOf, "oneOf"
	if len(items) == 0 {
		items, keyword = node.AnyOf, "anyOf"
	}
	if len(items) == 0 {
		g.addError(projector.CodeUnsupportedSchema, keyword+" must contain at least one member", node.Pointer, "")
		return nil, false
	}
	variants := make([]unionVariant, 0, len(items))
	seen := map[string]struct{}{}
	for i, item := range items {
		if item.Ref == "" {
			base := unionName + "Variant" + strconv.Itoa(i+1)
			switch primitiveSchema(item) {
			case "string":
				base = "StringValue"
			case "number":
				base = "NumberValue"
			case "boolean":
				base = "BooleanValue"
			case "null":
				base = "NullValue"
			}
			variantName := g.reserveTypeName(g.exportedIdentifier(base), item.Pointer, false)
			underlying := g.schemaGoType(item, variantName)
			if underlying.Expr == "any" || strings.HasPrefix(underlying.Expr, "map[") {
				g.addError(projector.CodeUnsupportedSchema, "union inline member is not representable", item.Pointer, "use a primitive, object, or local $ref")
				continue
			}
			g.addAliasDecl(variantName, underlying.Expr, item)
			variants = append(variants, unionVariant{Name: variantName, Type: variantName, Node: item})
			continue
		}
		defName, ok := localDefinitionRef(item.Ref)
		if !ok {
			g.addError(projector.CodeUnsupportedSchema, fmt.Sprintf("native %s support requires local direct $defs refs, got %q", keyword, item.Ref), item.Pointer, "")
			continue
		}
		def, ok := g.defs[defName]
		if !ok {
			g.addError(projector.CodeUnsupportedSchema, fmt.Sprintf("%s member definition %q was not found", keyword, defName), item.Pointer, "")
			continue
		}
		if _, ok := seen[defName]; ok {
			continue
		}
		seen[defName] = struct{}{}
		variants = append(variants, unionVariant{Name: defName, Type: g.typeNames[defName], Node: def})
	}
	if g.cfg.Unions.Ambiguous != "first" {
		seenTokens := map[string]string{}
		for _, variant := range variants {
			token := primitiveSchema(variant.Node)
			if token == "" {
				continue
			}
			if previous, exists := seenTokens[token]; exists {
				g.addError(projector.CodeUnsupportedSchema, fmt.Sprintf("union variants %s and %s have overlapping JSON token matches", previous, variant.Name), node.Pointer, "set generate.go.unions.ambiguous to first or make the schemas disjoint")
				break
			}
			seenTokens[token] = variant.Name
		}
	}
	return variants, len(variants) > 0 && !g.diagnostics.HasErrors()
}

func (g *nativeGenerator) isStructSchema(node *schemaNode) bool {
	if node == nil || node.Bool != nil || node.Ref != "" || len(node.OneOf) > 0 || len(node.AnyOf) > 0 {
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
		tagValue += ",omitzero"
	}

	var parts []string
	for _, tag := range tags(g.cfg) {
		parts = append(parts, tag+":"+strconv.Quote(tagValue))
	}
	return strings.Join(parts, " "), true
}

func mustJSONTag(propertyName string) string {
	return "`json:\"" + propertyName + "\"`"
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
