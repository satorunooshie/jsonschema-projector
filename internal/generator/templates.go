package generator

import (
	"embed"
	"fmt"
)

//go:embed templates/*.tmpl
var generatorTemplates embed.FS

func embeddedTemplate(name string) (string, error) {
	data, err := generatorTemplates.ReadFile("templates/" + name + ".tmpl")
	if err != nil {
		return "", fmt.Errorf("read generator template %q: %w", name, err)
	}
	return string(data), nil
}
