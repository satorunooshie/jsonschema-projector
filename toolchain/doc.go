// Package toolchain orchestrates the development-time JSON Schema workflow.
//
// It runs projection, jsonschema/v6 compilation, native Go DTO generation, and
// annotates each diagnostic with the stage that produced it.
package toolchain
