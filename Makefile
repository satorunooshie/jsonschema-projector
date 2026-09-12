.PHONY: build check example test vet bench

build:
	go build ./cmd/jsonschema-projector

check: test vet

example:
	go run ./cmd/jsonschema-projector generate -c examples/basic/projector.yaml

test:
	go test ./...

vet:
	go vet ./...

bench:
	go test ./projector -run '^$$' -bench BenchmarkProjectSchemaLarge -benchmem
	go test ./internal/generator -run '^$$' -bench BenchmarkGenerateGoLarge -benchmem
