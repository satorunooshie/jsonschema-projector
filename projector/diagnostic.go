package projector

import (
	"fmt"
	"strings"
)

// Severity classifies the seriousness of a diagnostic.
type Severity string

const (
	// SeverityError marks diagnostics that should stop the current stage.
	SeverityError Severity = "error"
	// SeverityWarning marks diagnostics that report recoverable issues.
	SeverityWarning Severity = "warning"
	// SeverityInfo marks informational diagnostics.
	SeverityInfo Severity = "info"
)

// Diagnostic code constants are stable values for Diagnostic.Code.
const (
	// Projection diagnostics.
	CodeDuplicateDefinition = "duplicate_definition"
	CodeInvalidConfig       = "invalid_config"
	CodeInvalidPointer      = "invalid_pointer"
	CodeMissingPointer      = "missing_pointer"
	CodeSourceNotObject     = "source_not_object"
	CodeUnresolvedRef       = "unresolved_ref"
	CodeUnsupportedRef      = "unsupported_ref"

	// Validation diagnostics.
	CodeInvalidValidateConfig = "invalid_validate_config"
	CodeSchemaCompileFailed   = "schema_compile_failed"

	// Generation diagnostics.
	CodeGeneratedOutputFailed = "generated_output_failed"
	CodeGenerationFailed      = "generation_failed"
	CodeInvalidGenerateConfig = "invalid_generate_config"
	CodeNameConflict          = "name_conflict"
	CodeUnsupportedSchema     = "unsupported_schema"
)

// Diagnostic describes one projection, validation, or generation issue.
type Diagnostic struct {
	// Severity is the diagnostic severity.
	Severity Severity `json:"severity"`
	// Stage identifies the pipeline stage that emitted the diagnostic.
	Stage string `json:"stage,omitempty"`
	// Code is a stable machine-readable diagnostic code.
	Code string `json:"code"`
	// Message is a human-readable diagnostic description.
	Message string `json:"message"`
	// Pointer is the schema or configuration location, when available.
	Pointer string `json:"pointer,omitempty"`
	// Ref is the reference involved, when available.
	Ref string `json:"ref,omitempty"`
	// Hint suggests a possible correction, when available.
	Hint string `json:"hint,omitempty"`
}

// Diagnostics is an ordered list of structured diagnostics.
type Diagnostics []Diagnostic

// Err returns a DiagnosticError when the list contains errors, or nil otherwise.
func (l Diagnostics) Err() error {
	if !l.HasErrors() {
		return nil
	}
	return &DiagnosticError{Diagnostics: l.Errors()}
}

// HasErrors reports whether any diagnostic has error severity.
func (l Diagnostics) HasErrors() bool {
	for _, d := range l {
		if d.Severity == SeverityError {
			return true
		}
	}
	return false
}

// String renders the diagnostic list as a single human-readable string.
func (l Diagnostics) String() string {
	if len(l) == 0 {
		return ""
	}

	var b strings.Builder
	for i, d := range l {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(d.String())
	}
	return b.String()
}

// Errors returns only diagnostics with error severity.
func (l Diagnostics) Errors() Diagnostics {
	out := make(Diagnostics, 0, len(l))
	for _, d := range l {
		if d.Severity == SeverityError {
			out = append(out, d)
		}
	}
	return out
}

// WithStage returns a copy of the diagnostic list with Stage set on each item.
func (l Diagnostics) WithStage(stage string) Diagnostics {
	out := make(Diagnostics, len(l))
	for i, d := range l {
		d.Stage = stage
		out[i] = d
	}
	return out
}

// Add appends a diagnostic without pointer or reference metadata.
func (l *Diagnostics) add(severity Severity, code, message string) {
	*l = append(*l, Diagnostic{
		Severity: severity,
		Code:     code,
		Message:  message,
	})
}

// AddAt appends a diagnostic associated with a schema or config pointer.
func (l *Diagnostics) addAt(severity Severity, code, message, pointer string) {
	*l = append(*l, Diagnostic{
		Severity: severity,
		Code:     code,
		Message:  message,
		Pointer:  pointer,
	})
}

// AddRef appends a diagnostic associated with a pointer, reference, and hint.
func (l *Diagnostics) addRef(severity Severity, code, message, pointer, ref, hint string) {
	*l = append(*l, Diagnostic{
		Severity: severity,
		Code:     code,
		Message:  message,
		Pointer:  pointer,
		Ref:      ref,
		Hint:     hint,
	})
}

// String renders the diagnostic as a human-readable string.
func (d Diagnostic) String() string {
	parts := []string{fmt.Sprintf("%s %s: %s", d.Severity, d.Code, d.Message)}
	if d.Stage != "" {
		parts = append(parts, "stage "+d.Stage)
	}
	if d.Pointer != "" {
		parts = append(parts, "at "+d.Pointer)
	}
	if d.Ref != "" {
		parts = append(parts, "ref "+d.Ref)
	}
	if d.Hint != "" {
		parts = append(parts, "hint: "+d.Hint)
	}
	return strings.Join(parts, " ")
}
