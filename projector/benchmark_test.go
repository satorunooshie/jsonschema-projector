package projector

import (
	"context"
	"fmt"
	"testing"
)

// BenchmarkProjectSchemaLarge measures projection throughput for a catalog
// with 1,000 definitions and 10 properties per definition.
func BenchmarkProjectSchemaLarge(b *testing.B) {
	doc := benchmarkSchema(1000, 10)
	cfg := ProjectConfig{
		IncludeDefinitions: []DefinitionSource{{Pointer: "#/components"}},
		RewriteRefs:        []RefRewrite{{From: "#/components/", To: "#/$defs/"}},
		CheckRefs:          true,
	}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, err := ProjectSchema(ctx, doc, cfg)
		if err != nil || result.Diagnostics.HasErrors() {
			b.Fatalf("projection failed: err=%v diagnostics=%v", err, result.Diagnostics)
		}
	}
}

func benchmarkSchema(definitions, properties int) map[string]any {
	components := make(map[string]any, definitions)
	for i := range definitions {
		fields := make(map[string]any, properties)
		for j := range properties {
			fields[fmt.Sprintf("field%d", j)] = map[string]any{
				"type":        "string",
				"description": "benchmark field",
			}
		}
		components[fmt.Sprintf("Component%d", i)] = map[string]any{
			"type":       "object",
			"properties": fields,
			"required":   []any{"field0"},
			"x-metadata": map[string]any{"source": "benchmark"},
		}
	}
	return map[string]any{"components": components}
}
