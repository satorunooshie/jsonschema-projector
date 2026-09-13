package generator

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/satorunooshie/jsonschema-projector/projector"
)

func (g *nativeGenerator) generateRootUnion() {
	if len(g.root.OneOf) == 0 && len(g.root.AnyOf) == 0 {
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
	case len(node.OneOf) > 0 || len(node.AnyOf) > 0:
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

	variants, ok := g.unionVariants(name, node)
	if !ok {
		return
	}
	if field, _ := g.unionDiscriminator(variants); field == "" {
		dispatchable := false
		for _, variant := range variants {
			if primitiveSchema(variant.Node) != "" || len(variant.Node.Required) > 0 || len(variant.Node.Properties) > 0 {
				dispatchable = true
				break
			}
		}
		if !dispatchable {
			g.addError(projector.CodeUnsupportedSchema, "union has no discriminator, token, required property, or structural match rule", node.Pointer, "add a discriminator const or make object variants structurally exclusive")
		}
	}

	methodName := markerMethodName(name)
	for _, variant := range variants {
		member := variant.Node
		if len(member.OneOf) > 0 {
			g.addError(
				projector.CodeUnsupportedSchema,
				"oneOf members must be concrete $defs entries, not another oneOf union",
				member.Pointer,
				"",
			)
			continue
		}
		g.addMarker(variant.Type, methodName)
	}

	description := ""
	if node.Description != "" {
		description = docComment(name, node.Description)
	}
	body := g.renderEmbeddedFragment("union", unionTemplateData{Name: name, Description: description, Method: methodName})
	g.addDecl(name, node.Pointer, body)
	g.unions = append(g.unions, unionInfo{Name: name, Node: node, Variants: variants})
	g.needsJSON, g.needsErrors = true, true
	g.needsFmt = true
	if g.cfg.Unions.Dispatch == "schema-validation" {
		g.needsSync = true
	}
	if field, _ := g.unionDiscriminator(variants); field != "" {
		g.needsSync = true
	}

	_ = root
}

func (g *nativeGenerator) unionDecoder(union unionInfo) string {
	d := unionDecoderData{
		Name:           union.Name,
		AmbiguousFirst: g.cfg.Unions.Ambiguous == "first",
		SchemaDispatch: g.cfg.Unions.Dispatch == "schema-validation",
	}
	if d.SchemaDispatch {
		g.needsSync = true
	}
	for _, variant := range union.Variants {
		d.ValidationCases = append(d.ValidationCases, unionDecoderBranch{
			Match: variant.Name,
			Body:  g.renderEmbeddedFragment("union-case", variant),
		})
	}
	if field, values := g.unionDiscriminator(union.Variants); field != "" {
		d.Discriminator = field
		d.Registration = true
		g.needsSync = true
		for _, value := range sortedStringKeys(values) {
			d.DiscriminatorCases = append(d.DiscriminatorCases, unionDecoderBranch{Match: value, Body: g.renderEmbeddedFragment("union-case", values[value])})
		}
	} else {
		allPrimitive := len(union.Variants) > 0
		hasPrimitive := false
		seen := map[string]bool{}
		seenCandidateCases := map[string]bool{}
		primitiveCounts := map[string]int{}
		for _, variant := range union.Variants {
			if typ := primitiveSchema(variant.Node); typ != "" {
				primitiveCounts[typ]++
			}
		}
		for _, variant := range union.Variants {
			typ := primitiveSchema(variant.Node)
			if typ == "" {
				allPrimitive = false
				continue
			}
			hasPrimitive = true
			if primitiveCounts[typ] > 1 {
				candidateCases := tokenCases(typ)
				if seenCandidateCases[typ] {
					candidateCases = ""
				}
				seenCandidateCases[typ] = true
				d.TokenCandidates = append(d.TokenCandidates, unionDecoderTokenCandidate{
					Cases: candidateCases, Type: variant.Type,
					Condition: g.variantCondition(variant),
				})
				continue
			}
			if seen[typ] {
				continue
			}
			seen[typ] = true
			cases := map[string]string{"string": `case '"':`, "number": `case '-', '0','1','2','3','4','5','6','7','8','9':`, "boolean": `case 't','f':`, "null": `case 'n':`}
			if c, ok := cases[typ]; ok {
				d.TokenCases = append(d.TokenCases, unionDecoderTokenBranch{Cases: c, Body: g.renderEmbeddedFragment("union-case", variant)})
			}
		}
		if !allPrimitive {
			d.Mixed = hasPrimitive && (len(d.TokenCases) > 0 || len(d.TokenCandidates) > 0)
			for _, variant := range union.Variants {
				if primitiveSchema(variant.Node) == "" && len(variant.Node.Required) > 0 {
					d.ObjectBranches = append(d.ObjectBranches, unionDecoderBranch{Match: jsonMatchCondition(variant), Body: g.renderEmbeddedFragment("union-case", variant)})
				}
			}
		}
	}
	return g.renderEmbeddedFragment("union-decoder", d)
}

func tokenCases(typ string) string {
	switch typ {
	case "string":
		return `'"'`
	case "number":
		return `'-', '0','1','2','3','4','5','6','7','8','9'`
	case "boolean":
		return `'t','f'`
	case "null":
		return `'n'`
	default:
		return ""
	}
}

// variantCondition is intentionally limited to constraints that can be
// evaluated without a full JSON Schema engine. If no such constraint exists,
// the candidate remains a valid match and the generated decoder reports
// ambiguity when another candidate also matches.
func (g *nativeGenerator) variantCondition(variant unionVariant) string {
	node := variant.Node
	parts := []string{"true"}
	typ := primitiveSchema(node)
	if typ == "string" {
		if node.Pattern != "" {
			parts = append(parts, fmt.Sprintf("%s.MatchString(string(candidate))", g.regexpMatcher(node.Pattern)))
		}
		if node.Format != "" {
			g.needsRegexp = true
			patterns := map[string]string{"email": `^[^@\s]+@[^@\s]+\.[^@\s]+$`, "date": `^\d{4}-\d{2}-\d{2}$`, "time": `^\d{2}:\d{2}:\d{2}`, "date-time": `^\d{4}-\d{2}-\d{2}T`}
			if pattern, ok := patterns[node.Format]; ok {
				parts = append(parts, fmt.Sprintf("%s.MatchString(string(candidate))", g.regexpMatcher(pattern)))
			}
		}
		if len(node.Enum) > 0 {
			choices := make([]string, 0, len(node.Enum))
			for _, value := range node.Enum {
				if stringValue, ok := value.(string); ok {
					choices = append(choices, fmt.Sprintf("string(candidate) == %q", stringValue))
				}
			}
			if len(choices) > 0 {
				parts = append(parts, "("+strings.Join(choices, " || ")+")")
			}
		}
	}
	if typ == "number" {
		if node.Minimum != nil {
			parts = append(parts, fmt.Sprintf("float64(candidate) >= %v", *node.Minimum))
		}
		if node.Maximum != nil {
			parts = append(parts, fmt.Sprintf("float64(candidate) <= %v", *node.Maximum))
		}
		if node.ExclusiveMinimum != nil {
			parts = append(parts, fmt.Sprintf("float64(candidate) > %v", *node.ExclusiveMinimum))
		}
		if node.ExclusiveMaximum != nil {
			parts = append(parts, fmt.Sprintf("float64(candidate) < %v", *node.ExclusiveMaximum))
		}
		if len(node.Enum) > 0 {
			choices := make([]string, 0, len(node.Enum))
			for _, value := range node.Enum {
				switch number := value.(type) {
				case float64:
					choices = append(choices, fmt.Sprintf("float64(candidate) == %v", number))
				case json.Number:
					choices = append(choices, fmt.Sprintf("float64(candidate) == %s", number.String()))
				}
			}
			if len(choices) > 0 {
				parts = append(parts, "("+strings.Join(choices, " || ")+")")
			}
		}
	}
	return strings.Join(parts, " && ")
}

func (g *nativeGenerator) regexpMatcher(pattern string) string {
	if name, ok := g.regexpNames[pattern]; ok {
		return name
	}
	name := fmt.Sprintf("unionPattern%d", len(g.regexpDecls)+1)
	g.regexpNames[pattern] = name
	g.regexpDecls = append(g.regexpDecls, fmt.Sprintf("var %s = regexp.MustCompile(%q)", name, pattern))
	g.needsRegexp = true
	return name
}

func jsonMatchCondition(variant unionVariant) string {
	parts := make([]string, 0, len(variant.Node.Required)+len(variant.Node.Properties))
	for _, field := range sortedSetBool(variant.Node.Required) {
		parts = append(parts, fmt.Sprintf("hasJSONField(envelope, %q)", field))
	}
	for _, field := range sortedKeysNode(variant.Node.Properties) {
		if typ := primitiveSchema(variant.Node.Properties[field]); typ != "" {
			parts = append(parts, fmt.Sprintf("(!hasJSONField(envelope, %q) || jsonTypeIs(envelope[%q], %q))", field, field, typ))
		}
	}
	if len(parts) == 0 {
		return "true"
	}
	return strings.Join(parts, " && ")
}

func (g *nativeGenerator) unionDiscriminator(variants []unionVariant) (string, map[string]unionVariant) {
	values := map[string]unionVariant{}
	field := ""
	for _, v := range variants {
		node := g.resolvedNode(v.Node, map[*schemaNode]bool{})
		if node == nil {
			return "", nil
		}
		for name, prop := range node.Properties {
			prop = g.resolvedNode(prop, map[*schemaNode]bool{})
			if prop == nil {
				continue
			}
			if prop.HasConst {
				if value, ok := prop.Const.(string); ok {
					if field == "" {
						field = name
					}
					if field != name || !node.Required[name] {
						return "", nil
					}
					if _, exists := values[value]; exists {
						return "", nil
					}
					values[value] = v
				}
			}
		}
	}
	if len(values) != len(variants) {
		return "", nil
	}
	return field, values
}

func (g *nativeGenerator) resolvedNode(node *schemaNode, seen map[*schemaNode]bool) *schemaNode {
	for node != nil && node.Ref != "" {
		if seen[node] {
			return nil
		}
		seen[node] = true
		name, ok := localDefinitionRef(node.Ref)
		if !ok {
			return nil
		}
		node = g.defs[name]
	}
	return node
}

func primitiveSchema(node *schemaNode) string {
	if node == nil {
		return ""
	}
	types := withoutNull(node.Types)
	if len(node.Types) == 1 && node.Types[0] == "null" {
		return "null"
	}
	if len(types) == 1 {
		switch types[0] {
		case "string":
			return "string"
		case "number", "integer":
			return "number"
		case "boolean":
			return "boolean"
		}
	}
	return ""
}

func sortedSetBool(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedKeysNode(m map[string]*schemaNode) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedStringKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
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

	var unionFields []structUnmarshalFieldData
	for _, propertyName := range sortedStructPropertyNames(node) {
		prop := node.Properties[propertyName]
		if prop == nil || !g.containsUnion(prop) {
			continue
		}
		fieldName := g.exportedIdentifier(firstNonEmpty(prop.XGoName, propertyName))
		unionName, mode := unionFieldName(g, prop, name+fieldName)
		g.needsJSON = true
		decoder := "Unmarshal" + unionName
		if mode == "slice" {
			decoder += "Slice"
		}
		unionFields = append(unionFields, structUnmarshalFieldData{
			Field: fieldName, Tag: mustJSONTag(propertyName), Decoder: decoder, UnionType: unionName, Mode: mode,
		})
	}
	methods := []string{}
	if len(unionFields) > 0 {
		methods = append(methods, g.renderEmbeddedFragment("struct-unmarshal", structUnmarshalTemplateData{Name: name, Fields: unionFields}))
	}
	description := ""
	if node.Description != "" {
		description = docComment(name, node.Description)
	}
	body := g.renderEmbeddedFragment("struct", structTemplateData{
		Name: name, Description: description, Fields: fields, Methods: methods,
	})
	g.addDecl(name, node.Pointer, body)
}

func (g *nativeGenerator) containsUnion(node *schemaNode) bool {
	if node == nil {
		return false
	}
	if node.Ref != "" {
		if name, ok := localDefinitionRef(node.Ref); ok {
			node = g.defs[name]
		}
	}
	return node != nil && (g.isUnionNode(node) ||
		(g.isUnionNode(node.Items)) || g.isUnionNode(node.AdditionalProperties))
}

func unionFieldName(g *nativeGenerator, node *schemaNode, context string) (string, string) {
	if node != nil && g.isUnionNode(node.Items) {
		return g.schemaGoType(node.Items, context+"Item").Expr, "slice"
	}
	if node != nil && g.isUnionNode(node.AdditionalProperties) {
		return g.schemaGoType(node.AdditionalProperties, context+"Value").Expr, "map"
	}
	return g.schemaGoType(node, context).Expr, "value"
}

func (g *nativeGenerator) isUnionNode(node *schemaNode) bool {
	node = g.resolvedNode(node, map[*schemaNode]bool{})
	return node != nil && (len(node.OneOf) > 0 || len(node.AnyOf) > 0)
}

func (g *nativeGenerator) addAliasDecl(name, underlying string, node *schemaNode) {
	if g.alreadyGenerated(node.Pointer) {
		return
	}
	if underlying == "" {
		underlying = "any"
	}

	description := ""
	if node.Description != "" {
		description = docComment(name, node.Description)
	}
	body := g.renderEmbeddedFragment("alias", aliasTemplateData{Name: name, Description: description, Underlying: underlying})
	if constants := g.enumConstants(name, node); len(constants) > 0 {
		constantLines := make([]string, 0, len(constants))
		for _, constant := range constants {
			constantLines = append(constantLines, fmt.Sprintf("\t%s %s = %s", constant.Name, name, constant.Value))
		}
		body += g.renderEmbeddedFragment("constants", constantLines)
	}
	g.addDecl(name, node.Pointer, body)
}
