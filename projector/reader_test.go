package projector

import (
	"context"
	"strings"
	"testing"
)

func TestProjectReaderProjectsInjectedInput(t *testing.T) {
	result, err := ProjectReader(context.Background(), strings.NewReader(`{"type":"string"}`), ProjectConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Schema["$defs"] == nil {
		t.Fatal("expected projected defs")
	}
}

func TestDiagnosticsErrAdaptsStructuredErrors(t *testing.T) {
	diagnostics := Diagnostics{{Severity: SeverityError, Code: CodeInvalidConfig, Message: "bad"}}
	err := diagnostics.Err()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "invalid_config") {
		t.Fatalf("unexpected error: %v", err)
	}
	if (Diagnostics{{Severity: SeverityWarning}}).Err() != nil {
		t.Fatal("warnings should not become errors")
	}
}
