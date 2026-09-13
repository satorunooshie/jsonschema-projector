# Union generation

This document records the design decisions, guarantees, and known boundaries
of union generation in the native Go backend. It is an implementation and
schema-authoring reference; the README contains the user-facing summary.

## Design goals

The generator has three goals:

1. preserve a closed JSON Schema union as a type-safe Go interface;
2. generate decoders that are deterministic and explicit about uncertainty; and
3. keep DTO generation separate from full JSON Schema validation.

The generator must never resolve an undecidable union by silently widening it
to `map[string]any`. A schema that cannot be represented or dispatched is a
generation diagnostic, and data that cannot be dispatched is a runtime error.

This is deliberately a DTO generator, not a general-purpose JSON Schema
compiler. Validation keywords that do not affect DTO shape remain the
responsibility of the application validator.

## Non-goals

The native backend does not attempt to implement all JSON Schema semantics in
generated code. In particular, full support for conditional schemas, external
references, arbitrary composition, and every format or constraint is outside
the DTO dispatch layer. Applications requiring those semantics should use the
runtime validator integration.

## Supported representations

Both `oneOf` and heterogeneous `anyOf` are represented as a Go interface with
one concrete type per variant:

```go
type Value interface { isValue() }

type StringValue string
type NumberValue float64
type BooleanValue bool
type NullValue struct{}
```

An array-valued `type` is treated as an implicit union. For example,
`["string", "number", "boolean", "null"]` follows the same path as an
explicit `anyOf`.

Inline object variants are promoted to local named definitions before union
generation. Names are derived from the containing schema path, so an object
variant under `Parent.value` receives a stable name such as
`ParentValueObject`.

## Normalization pipeline

Union analysis runs after schema parsing and inline normalization:

1. parse schemas into an internal graph, preserving JSON Pointer locations;
2. convert heterogeneous `type` arrays into implicit union members;
3. promote inline union objects to deterministic local definitions;
4. reserve definition names and resolve local references; and
5. generate interfaces, concrete variants, and decoders.

The pointer is retained for diagnostics even when a node is replaced by a
synthetic `$ref`. This makes generated names deterministic without losing the
source location of an error.

## Discriminator dispatch

The generator resolves local `$ref` values before inspecting variants. A
discriminator is used when every variant has:

- the same property name;
- a string `const` value for that property;
- that property listed in `required`; and
- a unique const value.

Generated decoders dispatch on the discriminator first and return
`ErrUnknownVariant` for an unknown value.

Discriminator discovery is intentionally strict. A candidate field is not a
discriminator unless all variants agree on its name, every variant requires it,
and all values are unique. This prevents a partial discriminator from masking
an ambiguous structural union.

## Fallback dispatch

When no discriminator is available, dispatch uses JSON token and structural
information. Primitive token dispatch supports strings, numbers, booleans, and
null. Object variants can be selected by required properties and primitive
property shapes. If multiple candidates match, the default result is
`ErrAmbiguousVariant`; `generate.go.unions.ambiguous: first` opts into
first-match behavior.

The generated decoder is a DTO router, not a complete JSON Schema validator.
Use `Unmarshal<Name>Validated`, `dispatch: schema-validation`, or an
application validator when full Schema semantics, including formats and
constraints, are required.

The intended precedence is:

1. discriminator;
2. primitive JSON token;
3. structural object match;
4. application-provided schema validation; and
5. unknown or ambiguous result.

The token and structural stages are routing heuristics. They must not be
described as proof that the complete JSON Schema matched.

## `oneOf` and `anyOf`

Both keywords use the same generated interface representation, but their Schema
semantics differ. `oneOf` requires exactly one matching branch, while `anyOf`
allows one or more matching branches. The native decoder therefore treats an
overlap as ambiguous by default and exposes `first` only as an explicit
configuration choice. Callers that need strict keyword semantics should use
schema-validation dispatch.

## Recursive unions

Recursive references are represented through the interface type itself. The
generator reserves definition names before emitting variants, which allows
shapes such as `[]Value` and `map[string]Value` to refer back to the interface.
Traversal uses an active-node set so recursive graphs are visited finitely;
the graph is not expanded by copying nodes indefinitely.

## Nested fields

Union decoders are generated for:

- root unions;
- interface-valued struct fields;
- slices of union values; and
- maps whose values are union values.

The generated struct unmarshaler decodes each nested value through its union
decoder, so malformed JSON, unknown variants, and ambiguous variants are
returned to the caller instead of being widened to `map[string]any`.

## Compatibility and operational guidance

Generated variant names are part of the generated Go API. Schema authors should
use stable titles or `x-go-name` where a public name must remain unchanged.
Synthetic names are deterministic, but changing a property path can change a
synthetic type name and therefore requires normal API compatibility review.

When introducing a new union schema:

- prefer a unique required discriminator for object variants;
- make primitive variants disjoint by token or constraints;
- use schema-validation dispatch when routing heuristics are insufficient; and
- test root, field, slice, map, null, malformed, unknown, ambiguous, and
  round-trip cases.

## Verification matrix

The generator test suite verifies source generation and compilation for root and
field unions, primitive wrappers, discriminator dispatch, nested references,
recursive references, ambiguous matches, and generated decoder fixtures. The
fixture tests are intentionally separate from schema-validation tests so a
successful compile cannot be mistaken for successful runtime dispatch.

## Open design extensions

The following extensions should be treated as explicit design work rather than
implicit behavior changes:

- separate ambiguity policies for `oneOf` and `anyOf`;
- generated match rules for `format`, `pattern`, numeric ranges, `enum`, and
  non-discriminator `const` values; and
- a first-class runtime-validator contract for complete Schema semantics.
