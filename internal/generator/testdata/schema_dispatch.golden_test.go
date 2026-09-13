package component

import "testing"

func TestSchemaDispatchIsUsedByDefault(t *testing.T) {
	RegisterComponentSchemaValidator(func(name string, _ []byte) (bool, error) {
		return name == "Text", nil
	})
	defer RegisterComponentSchemaValidator(nil)

	value, err := UnmarshalComponent([]byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := value.(Text); !ok {
		t.Fatalf("got %T, want Text", value)
	}
}
