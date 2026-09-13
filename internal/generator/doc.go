// Package generator emits Go DTO source from projected JSON Schema documents.
//
// The generator supports a conservative but typed subset of JSON Schema,
// including primitive type-array unions, named union variants, recursive
// references, and interface fields in objects, arrays, and maps. Unsupported
// shapes are reported as diagnostics instead of silently widening them to any.
// Runtime JSON validation remains the responsibility of jsonschema/v6 or the
// calling application.
package generator
