package projector

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestProjectSchemaSupportsURIFragmentsAndCircularRefs(t *testing.T) {
	for _, name := range []string{"uri-fragment.json", "circular.json"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("..", "testdata", "fixtures", name))
			if err != nil {
				t.Fatal(err)
			}
			var schema map[string]any
			if err := json.Unmarshal(data, &schema); err != nil {
				t.Fatal(err)
			}
			result, err := ProjectSchema(context.Background(), schema, ProjectConfig{CheckRefs: true})
			if err != nil {
				t.Fatal(err)
			}
			if result.Diagnostics.HasErrors() {
				t.Fatalf("fixture produced diagnostics: %v", result.Diagnostics)
			}
		})
	}
}
