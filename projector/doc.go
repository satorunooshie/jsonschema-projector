// Package projector projects registry-style JSON Schema documents into a
// generator-friendly schema document with top-level $defs.
//
// It loads schema inputs, copies selected definition sections, rewrites local
// reference prefixes, optionally synthesizes a root oneOf, and reports
// diagnostics without validating JSON Schema semantics. Use the toolchain
// package when projection should be followed by JSON Schema compilation or Go
// DTO generation.
package projector
