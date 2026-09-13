# jsonschema-projector

`jsonschema-projector` is a development-time toolchain for reshaping JSON
Schemas into application-friendly documents, validating them, and generating
Go DTOs through one repeatable workflow.

## Why this exists

Real-world JSON Schemas do not always keep every reusable type under `$defs` in
a single, generator-ready structure. Teams often organize definitions in
custom sections such as `components` or `types`, while references still point
to the original layout.

This project bridges the gap between a schema that is convenient to maintain
and a schema that is convenient to validate and generate code from.

```text
Managed JSON Schema
        │  import definitions / rewrite $refs / compose root types
        ▼
Projected JSON Schema ── validate ── Go DTOs
```

It turns the manual copying and editing of schema fragments into a configured,
repeatable process that works consistently in local development and CI.

## What it does

- Collects selected JSON Schema sections into `$defs`
- Rewrites local `$ref` prefixes
- Builds generator-friendly root schemas such as `oneOf`
- Validates the projected schema with `jsonschema/v6`
- Generates deterministic Go DTOs from supported schema shapes
- Regenerates schemas and source code from a configuration file

Unknown keywords in copied schema fragments are preserved. Projection and
validation are separate stages, so a schema shape that the native generator
cannot handle can still be validated as standard JSON Schema.

## Quick start

Go 1.27 or later is required.

```sh
go install github.com/satorunooshie/jsonschema-projector/cmd/jsonschema-projector@latest
```

If you have the repository checked out, run the included example:

```sh
go run ./cmd/jsonschema-projector generate -c examples/basic/projector.yaml
```

The example projects and validates the catalog in `examples/basic`, then
generates:

- `examples/basic/build/component.projected.schema.json`
- `examples/basic/internal/component/types.gen.go`

See [`examples/basic`](examples/basic) for the relationship between the input,
configuration, and generated outputs.

## CLI

```sh
jsonschema-projector project -c projector.yaml
jsonschema-projector check -c projector.yaml
jsonschema-projector check -c projector.yaml --format json
jsonschema-projector generate -c projector.yaml
jsonschema-projector version
```

- `project` writes the projected JSON Schema to `output.path`.
- `check` runs projection and validation without writing files. Use `--format json` for diagnostics as a JSON array.
- `generate` runs projection, validation, schema output, and Go DTO generation in order.
- `version` prints the CLI version.

Use `jsonschema-projector <command> -h` to see command-specific options. Input
and output paths may be `-` when the command supports stdin/stdout. For
example:

```sh
cat schema.json | jsonschema-projector project -c projector-stdin.yaml --output -
jsonschema-projector project -c projector.yaml --output projected.schema.json
```

In the first example, `projector-stdin.yaml` sets `input.path: "-"`.

CLI exit codes are `0` for success, `1` for input, configuration, validation,
or generation failures, and `2` for invalid command-line usage.

## Configuration

```yaml
input:
  path: schema/catalog.schema.json

output:
  path: build/component.projected.schema.json
  pretty: true
  finalNewline: true

project:
  includeDefinitions:
    - pointer: "#/$defs"
    - pointer: "#/components"
  rewriteRefs:
    - from: "#/components/"
      to: "#/$defs/"
  root:
    kind: oneOf
    from: "#/components"

validate:
  schema: true
  defaultDraft: "2020-12"

generate:
  go:
    package: component
    output: internal/component/types.gen.go
    rootType: Component
```

Paths are resolved relative to the directory containing the configuration
file. The configuration schema is available at
[`schema/projector.config.schema.json`](schema/projector.config.schema.json)
for editor completion and CI validation.

## Generation scope

The Go generator converts supported JSON Schema shapes into typed DTOs. It
supports objects, arrays, maps, primitives, enums, direct local references,
and `oneOf`/`anyOf` unions. Ambiguous union matches return
`ErrAmbiguousVariant` by default. Schema validation remains separate from DTO
decoding, and unsupported shapes are reported as diagnostics.

For union generation details, see
[`docs/union-generation.md`](docs/union-generation.md).

Generated DTOs are typed representations, not replacements for runtime
validation. Use `jsonschema/v6` when you need JSON Schema semantics at runtime.

The native generator currently supports this subset:

| Schema shape | Support |
| --- | --- |
| Objects and properties | Supported |
| Arrays and maps | Supported |
| Primitives and enums | Supported |
| Direct local `$ref` values | Supported |
| `oneOf` / `anyOf` unions | Supported, with documented dispatch rules |
| `allOf` | Limited; depends on the composed shape |
| `patternProperties` and conditional schemas | Unsupported |

Unsupported generator shapes produce diagnostics. They remain available for
projection and standard JSON Schema validation.

## Go API

The public API is intentionally limited to two packages:

- [`projector`](projector): configuration, projection, diagnostics, and artifact writing
- [`toolchain`](toolchain): the composed projection, validation, and generation pipeline

```go
cfg := projector.DefaultConfig()
cfg.Input.Path = "schema/catalog.schema.json"
cfg.Output.Path = "build/component.projected.schema.json"

result, err := projector.Project(context.Background(), cfg)
if err != nil {
	return err
}
if result.Diagnostics.HasErrors() {
	return fmt.Errorf("projection failed: %s", result.Diagnostics.Errors())
}
```

## License

MIT License. See [`LICENSE`](LICENSE).
