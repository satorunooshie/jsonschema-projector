package pointer

import "testing"

func TestEvalLocalFragmentWithEscapesAndArray(t *testing.T) {
	doc := map[string]any{
		"a/b": []any{
			map[string]any{"m~n": "ok"},
		},
	}

	got, ok, err := Eval(doc, "#/a~1b/0/m~0n")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected pointer to resolve")
	}
	if got != "ok" {
		t.Fatalf("expected ok, got %v", got)
	}
}

func TestEvalLocalFragmentPercentDecodesURIFragment(t *testing.T) {
	doc := map[string]any{
		"a b": "space",
	}

	got, ok, err := Eval(doc, "#/a%20b")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected pointer to resolve")
	}
	if got != "space" {
		t.Fatalf("expected space, got %v", got)
	}
}

func TestJoinEncodesURIFragment(t *testing.T) {
	got := Join("$defs", "a b", "m~n")
	if got != "#/$defs/a%20b/m~0n" {
		t.Fatalf("unexpected joined pointer: %s", got)
	}
}

func TestParseRejectsInvalidEscape(t *testing.T) {
	_, err := Parse("#/a~2b")
	if err == nil {
		t.Fatal("expected invalid escape error")
	}
}
