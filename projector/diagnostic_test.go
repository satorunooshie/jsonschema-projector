package projector

import "testing"

func TestDiagnosticsDoesNotImplementError(t *testing.T) {
	var diagnostics any = Diagnostics{}
	if _, ok := diagnostics.(error); ok {
		t.Fatal("Diagnostics should remain structured data, not an error")
	}

	diagnostics = &Diagnostics{}
	if _, ok := diagnostics.(error); ok {
		t.Fatal("*Diagnostics should remain structured data, not an error")
	}
}

func TestDiagnosticsStringRendersAllDiagnostics(t *testing.T) {
	diagnostics := Diagnostics{
		{
			Severity: SeverityError,
			Code:     CodeInvalidConfig,
			Message:  "bad config",
		},
		{
			Severity: SeverityWarning,
			Code:     CodeUnsupportedRef,
			Message:  "unsupported ref",
			Pointer:  "#/$defs/Thing/$ref",
		},
	}

	got := diagnostics.String()
	want := "error invalid_config: bad config; warning unsupported_ref: unsupported ref at #/$defs/Thing/$ref"
	if got != want {
		t.Fatalf("unexpected diagnostics string:\nwant: %s\ngot:  %s", want, got)
	}
}
