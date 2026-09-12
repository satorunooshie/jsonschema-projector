package projector

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPublicErrorsSupportIsAndAs(t *testing.T) {
	_, err := Project(context.Background(), Config{})
	var configErr *ConfigError
	if !errors.As(err, &configErr) || !errors.Is(err, ErrInvalidConfig) || !errors.Is(err, ErrInputRequired) {
		t.Fatalf("expected ConfigError and ErrInvalidConfig, got %T %v", err, err)
	}

	_, err = Project(context.Background(), Config{Input: InputConfig{Path: filepath.Join(t.TempDir(), "missing.json")}})
	var fileErr *FileError
	if !errors.As(err, &fileErr) || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected FileError and os.ErrNotExist, got %T %v", err, err)
	}
}
