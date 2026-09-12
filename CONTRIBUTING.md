# Contributing

Thanks for working on `jsonschema-projector`.

This project is intentionally small: the projector prepares schema artifacts,
`jsonschema/v6` validates those artifacts, and the native generator emits Go
DTOs for a conservative schema subset.

## Development

Run the full local check before sending changes:

```sh
go test ./...
go vet ./...
```

The generator tests include golden-file checks and compile generated Go source
inside a temporary module. If a change intentionally modifies generated output,
update the matching file in `internal/generator/testdata/` in the same commit.

## Design Rules

- Keep projection independent from validation and code generation.
- Keep the supported import surface limited to `projector` and `toolchain`
  unless a new public package has a clear compatibility contract.
- Preserve unknown schema keywords during projection.
- Prefer structured diagnostics over returned strings or silent fallbacks.
- Do not widen unsupported generator shapes to `any` unless the schema is truly
  unconstrained.
- Add tests when changing reference rewriting, root synthesis, diagnostics, or
  generated Go source.

## Generator Compatibility

The native generator supports only the subset documented in `README.md`. When
adding support for a new schema keyword:

1. Add a focused generator test.
2. Add or update a golden file if generated source changes.
3. Add an unsupported-schema regression test if related unsupported forms must
   remain rejected.
4. Update the compatibility table in `README.md`.

## Commit And PR Checklist

- Tests pass with `go test ./...`.
- Static checks pass with `go vet ./...`.
- Public behavior is documented in `README.md`.
- New exported packages or broad APIs have package documentation.
- Diagnostics include useful codes, pointers, and hints where possible.
