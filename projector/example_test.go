package projector

import (
	"context"
	"strings"
)

func ExampleProjectReader() {
	result, err := ProjectReader(context.Background(), strings.NewReader(`{"type":"object"}`), ProjectConfig{})
	if err != nil {
		panic(err)
	}
	_ = result.Schema
	// The projected schema is now available in memory.
}
