package generator

import (
	"context"
	"fmt"
	"io"
	"testing"

	"github.com/satorunooshie/jsonschema-projector/projector"
)

// BenchmarkGenerateGoLarge measures native generation for 1,000 DTO types.
func BenchmarkGenerateGoLarge(b *testing.B) {
	schema := largeGeneratedSchema(1000)
	cfg := projector.GoGenerateConfig{Package: "generated", Output: "-", Tags: []string{"json"}}
	b.ReportAllocs()
	for b.Loop() {
		diagnostics, err := GenerateGo(context.Background(), cfg, schema, io.Discard)
		if err != nil || diagnostics.HasErrors() {
			b.Fatalf("generation failed: err=%v diagnostics=%v", err, diagnostics)
		}
	}
}

func largeGeneratedSchema(definitions int) map[string]any {
	defs := make(map[string]any, definitions)
	for i := range definitions {
		defs[fmt.Sprintf("Component%d", i)] = map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name":  map[string]any{"type": "string"},
				"index": map[string]any{"type": "integer"},
			},
			"required": []any{"name"},
		}
	}
	return map[string]any{"$defs": defs}
}
