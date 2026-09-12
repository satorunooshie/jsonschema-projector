# Release Process

`jsonschema-projector` uses Semantic Versioning.

- Patch releases fix bugs without changing supported config or generated API.
- Minor releases add compatible config, projection, validation, or generation
  behavior.
- Major releases may change config shape, CLI behavior, diagnostic contracts, or
  generated Go API.

## Checklist

1. Ensure `main` is green in CI.
2. Run local checks:

   ```sh
   go test ./...
   go vet ./...
   ```

3. Confirm `README.md` documents any new behavior.
4. Tag the release:

   ```sh
   git tag vX.Y.Z
   git push origin vX.Y.Z
   ```

5. Push a `vX.Y.Z` tag. The release workflow uses GoReleaser to publish
   platform binaries, checksums, and SBOMs with the version embedded.

Source installs with `go install` can report the module version from Go build
info when installed from a tagged module version. Run `make bench` when a
change may affect large-schema performance and compare results with `benchstat`.
