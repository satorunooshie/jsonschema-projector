package toolchain

import (
	"context"
	"strings"

	"github.com/satorunooshie/jsonschema-projector/projector"
)

func ExampleCheckReader() {
	cfg := projector.DefaultConfig()
	cfg.Input.Path = "-"
	result, err := CheckReader(context.Background(), cfg, strings.NewReader(`{"type":"string"}`))
	if err != nil {
		panic(err)
	}
	if err := result.Diagnostics.Err(); err != nil {
		panic(err)
	}
}
