package projector

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"
)

// Keep the editor-facing JSON Schema and the YAML/JSON API in lockstep. This
// catches fields that the schema advertises but ReadConfig rejects via
// yaml.Decoder.KnownFields.
func TestConfigSchemaMatchesConfigTypes(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "..", "schema", "projector.config.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Defs       map[string]struct {
			Properties map[string]json.RawMessage `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}

	assertSchemaPropertiesMatchType(t, "root", document.Properties, reflect.TypeOf(Config{}))
	for name, typ := range map[string]reflect.Type{
		"input":      reflect.TypeOf(InputConfig{}),
		"output":     reflect.TypeOf(OutputConfig{}),
		"project":    reflect.TypeOf(ProjectConfig{}),
		"validate":   reflect.TypeOf(ValidationConfig{}),
		"generate":   reflect.TypeOf(GenerateConfig{}),
		"goGenerate": reflect.TypeOf(GoGenerateConfig{}),
		"unions":     reflect.TypeOf(UnionGenerateConfig{}),
		"root":       reflect.TypeOf(RootSynthesis{}),
	} {
		def, ok := document.Defs[name]
		if !ok {
			t.Errorf("schema is missing $defs.%s", name)
			continue
		}
		assertSchemaPropertiesMatchType(t, "$defs."+name, def.Properties, typ)
	}
}

func TestConfigSchemaAndReaderAgree(t *testing.T) {
	schema := loadConfigSchema(t)
	tests := []struct {
		name       string
		yaml       string
		readOK     bool
		schemaOK   bool
		checkRoot  bool
		rootKindIs string
	}{
		{
			name:   "valid config with default root kind",
			yaml:   "input:\n  path: schema.json\nproject:\n  root:\n    from: '#/components'\n",
			readOK: true, schemaOK: true, checkRoot: true, rootKindIs: "",
		},
		{name: "unknown field", yaml: "input:\n  path: schema.json\nunknown: true\n", schemaOK: false},
		{name: "removed backend field", yaml: "input:\n  path: schema.json\ngenerate:\n  go:\n    backend: native\n", schemaOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, readErr := ReadConfig(strings.NewReader(tt.yaml))
			if (readErr == nil) != tt.readOK {
				t.Fatalf("ReadConfig success=%v, want %v; err=%v", readErr == nil, tt.readOK, readErr)
			}
			if tt.checkRoot && (cfg.Project.Root == nil || cfg.Project.Root.Kind != tt.rootKindIs) {
				var got string
				if cfg.Project.Root != nil {
					got = cfg.Project.Root.Kind
				}
				t.Fatalf("root kind = %q, want %q", got, tt.rootKindIs)
			}
			var raw any
			if err := yaml.Unmarshal([]byte(tt.yaml), &raw); err != nil {
				t.Fatal(err)
			}
			value := yamlToJSON(t, raw)
			if err := schema.Validate(value); (err == nil) != tt.schemaOK {
				t.Fatalf("schema validation success=%v, want %v; err=%v", err == nil, tt.schemaOK, err)
			}
		})
	}
}

func loadConfigSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "..", "schema", "projector.config.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var document any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("config.schema.json", document); err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile("config.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

func yamlToJSON(t *testing.T, value any) any {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var result any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func assertSchemaPropertiesMatchType(t *testing.T, name string, properties map[string]json.RawMessage, typ reflect.Type) {
	t.Helper()
	expected := map[string]bool{}
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		key := strings.Split(field.Tag.Get("yaml"), ",")[0]
		if key != "" && key != "-" {
			expected[key] = true
		}
	}
	for key := range properties {
		if !expected[key] {
			t.Errorf("%s advertises unknown property %q", name, key)
		}
		delete(expected, key)
	}
	for key := range expected {
		t.Errorf("%s is missing property %q", name, key)
	}
}
