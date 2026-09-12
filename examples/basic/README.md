# Basic Example

Run this from the repository root:

```sh
go run ./cmd/jsonschema-projector generate -c examples/basic/projector.yaml
```

The command writes:

- `examples/basic/build/component.projected.schema.json`
- `examples/basic/internal/component/types.gen.go`

The input schema keeps registry-style sections under `types` and `components`.
The projector copies them into top-level `$defs`, rewrites local refs, validates
the projected schema with `jsonschema/v6`, then generates Go DTOs.
