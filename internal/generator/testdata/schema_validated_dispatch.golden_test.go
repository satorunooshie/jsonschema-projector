package component

import (
	"errors"
	"testing"
)

func TestSchemaValidatedDispatch(t *testing.T) {
	cases := []struct {
		name      string
		validate  func(string, []byte) (bool, error)
		wantError error
	}{
		{
			name: "single match",
			validate: func(name string, _ []byte) (bool, error) {
				return name == "Text", nil
			},
		},
		{
			name: "ambiguous",
			validate: func(_ string, _ []byte) (bool, error) {
				return true, nil
			},
			wantError: ErrAmbiguousVariant,
		},
		{
			name: "unknown",
			validate: func(_ string, _ []byte) (bool, error) {
				return false, nil
			},
			wantError: ErrUnknownVariant,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			value, err := UnmarshalComponentSchemaValidated([]byte("{}"), tc.validate)
			if tc.wantError != nil {
				if !errors.Is(err, tc.wantError) {
					t.Fatalf("got %v, want %v", err, tc.wantError)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := value.(Text); !ok {
				t.Fatalf("got %T, want Text", value)
			}
		})
	}
}
