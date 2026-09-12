package projector

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
)

// WriteOptions controls JSON schema encoding.
type WriteOptions struct {
	// Pretty enables indented JSON output.
	Pretty bool
	// FinalNewline appends a newline after the JSON document.
	FinalNewline bool
}

// atomicFile describes one file in a multi-file atomic write.
type atomicFile struct {
	// Path is the destination path.
	Path string
	// Data is the complete file content.
	Data []byte
	// Mode is used for new files; existing file modes are preserved.
	Mode os.FileMode
}

// encodeSchema returns the JSON encoding used by WriteSchema.
func encodeSchema(schema map[string]any, opts WriteOptions) ([]byte, error) {
	if opts.Pretty {
		data, err := json.MarshalIndent(schema, "", "  ")
		if err != nil {
			return nil, err
		}
		if opts.FinalNewline {
			data = append(data, '\n')
		}
		return data, nil
	}
	data, err := json.Marshal(schema)
	if err != nil {
		return nil, err
	}
	if opts.FinalNewline {
		data = append(data, '\n')
	}
	return data, nil
}

// WriteSchema encodes a schema as deterministic JSON to w.
func WriteSchema(w io.Writer, schema map[string]any, opts WriteOptions) error {
	data, err := encodeSchema(schema, opts)
	if err != nil {
		return err
	}
	n, err := w.Write(data)
	if err != nil {
		return err
	}
	if n != len(data) {
		return io.ErrShortWrite
	}
	return nil
}

// writeFilesAtomically stages and commits multiple files as one logical write.
// If a commit fails, already replaced files are restored when possible.
func writeFilesAtomically(files ...atomicFile) error {
	type stagedFile struct {
		target, temp string
		mode         os.FileMode
		existed      bool
	}
	staged := make([]stagedFile, 0, len(files))
	defer func() {
		for _, item := range staged {
			_ = os.Remove(item.temp)
		}
	}()
	seen := make(map[string]struct{}, len(files))
	for _, file := range files {
		if file.Path == "" {
			return ErrOutputRequired
		}
		if _, ok := seen[file.Path]; ok {
			return &ConfigError{Field: "files.path", Message: "duplicate atomic output path", Cause: ErrInvalidConfig}
		}
		seen[file.Path] = struct{}{}
		dir := filepath.Dir(file.Path)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return &FileError{Operation: "create output directory", Path: file.Path, Err: err}
		}
		mode := file.Mode
		if mode == 0 {
			mode = 0o644
		}
		existed := false
		if info, err := os.Stat(file.Path); err == nil {
			mode, existed = info.Mode().Perm(), true
		} else if !os.IsNotExist(err) {
			return &FileError{Operation: "stat output", Path: file.Path, Err: err}
		}
		tmp, err := os.CreateTemp(dir, ".jsonschema-projector-stage-*")
		if err != nil {
			return &FileError{Operation: "create temporary output", Path: file.Path, Err: err}
		}
		tmpPath := tmp.Name()
		cleanup := func() { _ = tmp.Close(); _ = os.Remove(tmpPath) }
		if err := tmp.Chmod(mode); err != nil {
			cleanup()
			return &FileError{Operation: "chmod temporary output", Path: file.Path, Err: err}
		}
		if _, err := tmp.Write(file.Data); err != nil {
			cleanup()
			return &FileError{Operation: "write temporary output", Path: file.Path, Err: err}
		}
		if err := tmp.Sync(); err != nil {
			cleanup()
			return &FileError{Operation: "sync temporary output", Path: file.Path, Err: err}
		}
		if err := tmp.Close(); err != nil {
			_ = os.Remove(tmpPath)
			return &FileError{Operation: "close temporary output", Path: file.Path, Err: err}
		}
		staged = append(staged, stagedFile{target: file.Path, temp: tmpPath, mode: mode, existed: existed})
	}
	committed := 0
	backups := make([]string, len(staged))
	rollback := func() {
		for i := committed - 1; i >= 0; i-- {
			_ = os.Remove(staged[i].target)
			if backups[i] != "" {
				_ = os.Rename(backups[i], staged[i].target)
			}
		}
		for i := committed; i < len(staged); i++ {
			_ = os.Remove(staged[i].temp)
		}
	}
	for i, item := range staged {
		if item.existed {
			backup, err := os.CreateTemp(filepath.Dir(item.target), ".jsonschema-projector-backup-*")
			if err != nil {
				rollback()
				return &FileError{Operation: "create backup", Path: item.target, Err: err}
			}
			backupPath := backup.Name()
			_ = backup.Close()
			_ = os.Remove(backupPath)
			if err := os.Rename(item.target, backupPath); err != nil {
				rollback()
				return &FileError{Operation: "backup output", Path: item.target, Err: err}
			}
			backups[i] = backupPath
		}
		if err := os.Rename(item.temp, item.target); err != nil {
			if backups[i] != "" {
				_ = os.Rename(backups[i], item.target)
				backups[i] = ""
			}
			rollback()
			return &FileError{Operation: "commit output", Path: item.target, Err: err}
		}
		committed++
	}
	for _, backup := range backups {
		if backup != "" {
			_ = os.Remove(backup)
		}
	}
	return nil
}

// WriteSchemaFile atomically writes a schema JSON document to path.
func WriteSchemaFile(path string, schema map[string]any, opts WriteOptions) error {
	if path == "" {
		return ErrOutputRequired
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return &FileError{Operation: "create output directory", Path: path, Err: err}
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".jsonschema-projector-*")
	if err != nil {
		return &FileError{Operation: "create temporary output", Path: path, Err: err}
	}
	tmpPath := tmp.Name()
	removeTemp := true
	defer func() {
		_ = tmp.Close()
		if removeTemp {
			_ = os.Remove(tmpPath)
		}
	}()
	if info, statErr := os.Stat(path); statErr == nil {
		if err := tmp.Chmod(info.Mode().Perm()); err != nil {
			return &FileError{Operation: "chmod temporary output", Path: path, Err: err}
		}
	} else if !os.IsNotExist(statErr) {
		return &FileError{Operation: "stat output", Path: path, Err: statErr}
	}
	if err := WriteSchema(tmp, schema, opts); err != nil {
		return &FileError{Operation: "write", Path: path, Err: err}
	}
	if err := tmp.Sync(); err != nil {
		return &FileError{Operation: "sync", Path: path, Err: err}
	}
	if err := tmp.Close(); err != nil {
		return &FileError{Operation: "close", Path: path, Err: err}
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return &FileError{Operation: "rename", Path: path, Err: err}
	}
	removeTemp = false
	return nil
}
