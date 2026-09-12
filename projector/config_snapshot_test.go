package projector

import "testing"

func TestConfigSnapshotDoesNotAliasMutableConfig(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Input.Path = "schema.json"
	cfg.Project.Root = &RootSynthesis{From: "#/components", Names: []string{"First"}}
	cfg.Generate = &GenerateConfig{Go: &GoGenerateConfig{Package: "component", Output: "types.go", Tags: []string{"json"}}}
	prepared, err := snapshotConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Project.Root.Names[0] = "Changed"
	cfg.Generate.Go.Tags[0] = "yaml"
	project := prepared.Project
	generate := prepared.Generate
	if project.Root.Names[0] != "First" || generate.Go.Tags[0] != "json" {
		t.Fatal("config snapshot aliases source config")
	}
}
