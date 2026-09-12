package projector

import (
	"errors"
	"testing"
)

func TestAtomicFileWriteRejectsDuplicatePaths(t *testing.T) {
	err := writeFilesAtomically(
		atomicFile{Path: "same", Data: []byte("one")},
		atomicFile{Path: "same", Data: []byte("two")},
	)
	var configErr *ConfigError
	if !errors.As(err, &configErr) || configErr.Message != "duplicate atomic output path" {
		t.Fatalf("expected duplicate path config error, got %v", err)
	}
}
