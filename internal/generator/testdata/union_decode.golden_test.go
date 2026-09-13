package component

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestGeneratedDecode(t *testing.T) {
	value, err := UnmarshalComponent([]byte("{\"value\":\"hello\"}"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := value.(Text); !ok {
		t.Fatalf("got %T, want Text", value)
	}
	if _, err := UnmarshalComponent([]byte("{\"value\":\"hello\",\"url\":\"x\"}")); !errors.Is(err, ErrAmbiguousVariant) {
		t.Fatalf("got %v, want ambiguous error", err)
	}
	var text Text
	if err := json.Unmarshal([]byte("{\"value\":\"child\"}"), &text); err != nil {
		t.Fatal(err)
	}
}
