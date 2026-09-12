package projector

import (
	"errors"
	"fmt"
)

// Sentinel errors returned by the public API. Use errors.Is to classify them.
var (
	// ErrInvalidConfig indicates invalid public configuration.
	ErrInvalidConfig = errors.New("invalid projector config")
	// ErrInputRequired indicates that an input path was not supplied.
	ErrInputRequired = errors.New("input path is required")
	// ErrOutputRequired indicates that an output path was not supplied.
	ErrOutputRequired = errors.New("output path is required")
)

// ConfigError describes an invalid configuration field.
// It unwraps to ErrInvalidConfig for errors.Is checks.
type ConfigError struct {
	// Field is the configuration field that failed validation.
	Field string
	// Message describes why the field is invalid.
	Message string
	// Cause is an optional sentinel describing the specific configuration error.
	Cause error
}

func (e *ConfigError) Error() string {
	if e.Field == "" {
		return e.Message
	}
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// Unwrap makes ConfigError compatible with errors.Is and errors.As.
func (e *ConfigError) Unwrap() []error {
	if e.Cause == nil {
		return []error{ErrInvalidConfig}
	}
	return []error{ErrInvalidConfig, e.Cause}
}

// FileError describes an error while reading or writing a named file.
type FileError struct {
	// Operation is the filesystem operation that failed.
	Operation string
	// Path is the affected filesystem path.
	Path string
	// Err is the underlying filesystem error.
	Err error
}

func (e *FileError) Error() string {
	return fmt.Sprintf("%s %q: %v", e.Operation, e.Path, e.Err)
}

// Unwrap exposes the underlying filesystem error.
func (e *FileError) Unwrap() error { return e.Err }

// DiagnosticError adapts error diagnostics to the standard error interface.
// The original structured diagnostics remain available in Diagnostics.
type DiagnosticError struct {
	// Diagnostics contains the error diagnostics represented by this error.
	Diagnostics Diagnostics
}

// Error returns the human-readable diagnostic list.
func (e *DiagnosticError) Error() string { return e.Diagnostics.String() }

// Unwrap returns nil; use errors.As to recover DiagnosticError and its details.
func (e *DiagnosticError) Unwrap() error { return nil }
