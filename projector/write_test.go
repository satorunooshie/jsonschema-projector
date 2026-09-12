package projector

import (
	"bytes"
	"errors"
	"io"
	"os"
	"testing"
)

func TestWriteSchemaIsDeterministic(t *testing.T) {
	left := map[string]any{
		"b": "second",
		"a": map[string]any{
			"z": true,
			"m": "middle",
		},
	}
	right := map[string]any{
		"a": map[string]any{
			"m": "middle",
			"z": true,
		},
		"b": "second",
	}

	var leftOut, rightOut bytes.Buffer
	opts := WriteOptions{Pretty: true, FinalNewline: true}
	if err := WriteSchema(&leftOut, left, opts); err != nil {
		t.Fatal(err)
	}
	if err := WriteSchema(&rightOut, right, opts); err != nil {
		t.Fatal(err)
	}

	if leftOut.String() != rightOut.String() {
		t.Fatalf("expected deterministic output:\nleft:\n%s\nright:\n%s", leftOut.String(), rightOut.String())
	}
}

func TestWriteSchemaRejectsShortWrite(t *testing.T) {
	err := WriteSchema(shortWriter{}, map[string]any{"ok": true}, WriteOptions{})
	if !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("expected io.ErrShortWrite, got %v", err)
	}
}

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	return len(p) - 1, nil
}

func TestAtomicFileWritePreservesExistingFiles(t *testing.T) {
	dir := t.TempDir()
	left := dir + "/left.json"
	right := dir + "/right.go"
	if err := os.WriteFile(left, []byte("old-left"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(right, []byte("old-right"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeFilesAtomically(
		atomicFile{Path: left, Data: []byte("new-left")},
		atomicFile{Path: right, Data: []byte("new-right")},
	); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		path, want string
		mode       os.FileMode
	}{{left, "new-left", 0o640}, {right, "new-right", 0o600}} {
		data, err := os.ReadFile(item.path)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != item.want {
			t.Fatalf("%s: got %q", item.path, data)
		}
		info, err := os.Stat(item.path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != item.mode {
			t.Fatalf("%s: mode %o, want %o", item.path, info.Mode().Perm(), item.mode)
		}
	}
}
