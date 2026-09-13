package generator

import "github.com/satorunooshie/jsonschema-projector/projector"

type schemaNode struct {
	Pointer string
	Bool    *bool

	Ref              string
	Title            string
	Description      string
	XGoName          string
	Types            []string
	Format           string
	Pattern          string
	Minimum          *float64
	Maximum          *float64
	ExclusiveMinimum *float64
	ExclusiveMaximum *float64

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

type enumConstant struct {
	Name  string
	Value string
}

type sourceTemplateData struct {
	Package                string
	Imports                []string
	Preamble               string
	NeedsErrors, NeedsJSON bool
	RegexpDecls            []string
	Decls                  []decl
	Markers                []string
	Decoders               []string
}

type structTemplateData struct {
	Name, Description string
	Fields, Methods   []string
}

type unionTemplateData struct {
	Name, Description, Method string
}

type structUnmarshalTemplateData struct {
	Name, Field, Tag, Decoder, UnionType, Mode string
}

type aliasTemplateData struct {
	Name, Description, Underlying string
}

// nativeGenerator owns the generation state shared by schema analysis and
// rendering. The representation types above are independent of template data
// types in union_model.go.
type nativeGenerator struct {
	cfg    projector.GoGenerateConfig
	schema map[string]any

	root     *schemaNode
	defs     map[string]*schemaNode
	defOrder []string

	typeNames          map[string]string
	usedTypeNames      map[string]string
	generatedByPointer map[string]string
	unionVariantTypes  map[string]string
	decls              []decl
	markerMethods      map[string]map[string]struct{}
	unions             []unionInfo
	needsJSON          bool
	needsErrors        bool
	needsFmt           bool
	needsSync          bool
	needsRegexp        bool
	regexpDecls        []string
	regexpNames        map[string]string
	diagnostics        projector.Diagnostics
}
