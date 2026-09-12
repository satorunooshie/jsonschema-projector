package loader

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type Loader struct {
	cache map[string]map[string]any
}

func NewCached() *Loader {
	return &Loader{
		cache: make(map[string]map[string]any),
	}
}

func LoadJSONFile(path string) (map[string]any, error) {
	if path == "" {
		return nil, fmt.Errorf("input path is required")
	}

	if path == "-" {
		return LoadJSON(os.Stdin)
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	return LoadJSON(file)
}

func (l *Loader) LoadJSONFile(path string) (map[string]any, error) {
	if l == nil {
		return LoadJSONFile(path)
	}
	if path == "-" {
		return LoadJSONFile(path)
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}

	if doc, ok := l.cache[abs]; ok {
		return DeepCopyObject(doc), nil
	}

	doc, err := LoadJSONFile(abs)
	if err != nil {
		return nil, err
	}
	l.cache[abs] = DeepCopyObject(doc)

	return DeepCopyObject(doc), nil
}

func LoadJSON(r io.Reader) (map[string]any, error) {
	if r == nil {
		return nil, fmt.Errorf("JSON reader is required")
	}

	dec := json.NewDecoder(r)
	dec.UseNumber()

	var value any
	if err := dec.Decode(&value); err != nil {
		return nil, err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("JSON document must contain a single top-level value")
		}
		return nil, err
	}

	obj, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("top-level JSON value must be an object")
	}

	return obj, nil
}

func DeepCopy(value any) any {
	switch v := value.(type) {
	case map[string]any:
		return DeepCopyObject(v)
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = DeepCopy(item)
		}
		return out
	default:
		return v
	}
}

func DeepCopyObject(value map[string]any) map[string]any {
	out := make(map[string]any, len(value))
	for k, v := range value {
		out[k] = DeepCopy(v)
	}
	return out
}
