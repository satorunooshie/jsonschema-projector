package generator

// unionVariant is the analyzed representation of one union member.
type unionVariant struct {
	Name string
	Type string
	Node *schemaNode
}

type unionInfo struct {
	Name     string
	Node     *schemaNode
	Variants []unionVariant
}

// unionDecoderBranch is deliberately renderer-neutral: Match is the emitted
// discriminator or variant name and Body is the already-rendered decode body.
type unionDecoderBranch struct {
	Match string
	Body  string
}

type unionDecoderTokenBranch struct {
	Cases string
	Body  string
}

type unionDecoderData struct {
	Name, Discriminator string
	DiscriminatorCases  []unionDecoderBranch
	TokenCases          []unionDecoderTokenBranch
	ObjectBranches      []unionDecoderBranch
	ValidationCases     []unionDecoderBranch
	AmbiguousFirst      bool
	Mixed               bool
	Registration        bool
	SchemaDispatch      bool
}
