// Package generator emits Go DTO source from projected JSON Schema documents.
//
// The generator intentionally supports a conservative subset of JSON Schema and
// reports unsupported shapes as diagnostics instead of silently widening them to
// any. Runtime JSON validation remains the responsibility of jsonschema/v6 or
// the calling application.
package generator
