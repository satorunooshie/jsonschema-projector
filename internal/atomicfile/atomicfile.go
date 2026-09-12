// Package atomicfile writes a set of files with same-directory staging and
// best-effort rollback.
package atomicfile

import (
	"fmt"
	"os"
	"path/filepath"
)

type File struct {
	Path string
	Data []byte
}

type Error struct {
	Path      string
	Operation string
	Err       error
}

func (e *Error) Error() string { return fmt.Sprintf("%s %q: %v", e.Operation, e.Path, e.Err) }
func (e *Error) Unwrap() error { return e.Err }

func WriteFiles(files ...File) error {
	type stagedFile struct {
		target, temp, backup string
		existed              bool
	}
	staged := make([]stagedFile, 0, len(files))
	cleanup := func() {
		for _, file := range staged {
			_ = os.Remove(file.temp)
			_ = os.Remove(file.backup)
		}
	}
	defer cleanup()

	seen := make(map[string]struct{}, len(files))
	for _, file := range files {
		if file.Path == "" {
			return &Error{Operation: "validate output", Err: os.ErrInvalid}
		}
		if _, ok := seen[file.Path]; ok {
			return &Error{Operation: "validate output", Path: file.Path, Err: os.ErrExist}
		}
		seen[file.Path] = struct{}{}

		dir := filepath.Dir(file.Path)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return &Error{Operation: "create output directory", Path: file.Path, Err: err}
		}
		mode := os.FileMode(0o644)
		existed := false
		if info, err := os.Stat(file.Path); err == nil {
			mode, existed = info.Mode().Perm(), true
		} else if !os.IsNotExist(err) {
			return &Error{Operation: "stat output", Path: file.Path, Err: err}
		}

		tmp, err := os.CreateTemp(dir, ".jsonschema-projector-stage-*")
		if err != nil {
			return &Error{Operation: "create temporary output", Path: file.Path, Err: err}
		}
		tmpPath := tmp.Name()
		closeAndRemove := func() {
			_ = tmp.Close()
			_ = os.Remove(tmpPath)
		}
		if err := tmp.Chmod(mode); err != nil {
			closeAndRemove()
			return &Error{Operation: "chmod temporary output", Path: file.Path, Err: err}
		}
		if _, err := tmp.Write(file.Data); err != nil {
			closeAndRemove()
			return &Error{Operation: "write temporary output", Path: file.Path, Err: err}
		}
		if err := tmp.Sync(); err != nil {
			closeAndRemove()
			return &Error{Operation: "sync temporary output", Path: file.Path, Err: err}
		}
		if err := tmp.Close(); err != nil {
			_ = os.Remove(tmpPath)
			return &Error{Operation: "close temporary output", Path: file.Path, Err: err}
		}
		staged = append(staged, stagedFile{target: file.Path, temp: tmpPath, existed: existed})
	}

	committed := 0
	rollback := func() {
		for i := committed - 1; i >= 0; i-- {
			_ = os.Remove(staged[i].target)
			if staged[i].backup != "" {
				_ = os.Rename(staged[i].backup, staged[i].target)
			}
		}
	}
	for i := range staged {
		file := &staged[i]
		if file.existed {
			backup, err := os.CreateTemp(filepath.Dir(file.target), ".jsonschema-projector-backup-*")
			if err != nil {
				rollback()
				return &Error{Operation: "create backup", Path: file.target, Err: err}
			}
			file.backup = backup.Name()
			if err := backup.Close(); err != nil {
				rollback()
				return &Error{Operation: "close backup", Path: file.target, Err: err}
			}
			_ = os.Remove(file.backup)
			if err := os.Rename(file.target, file.backup); err != nil {
				rollback()
				return &Error{Operation: "backup output", Path: file.target, Err: err}
			}
		}
		if err := os.Rename(file.temp, file.target); err != nil {
			if file.backup != "" {
				_ = os.Rename(file.backup, file.target)
				file.backup = ""
			}
			rollback()
			return &Error{Operation: "commit output", Path: file.target, Err: err}
		}
		committed++
	}
	return nil
}
