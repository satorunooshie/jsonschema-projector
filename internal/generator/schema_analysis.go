package generator

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"

	"github.com/satorunooshie/jsonschema-projector/projector"
)

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
	for _, variant := range node.AnyOf[1:] {
		if !schemaNodesEquivalent(first, variant, map[[2]*schemaNode]bool{}) {
			// Heterogeneous anyOf is handled as an interface union.
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
	if node == nil || node.Bool != nil || len(node.OneOf) > 0 || len(node.AnyOf) > 0 {
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
