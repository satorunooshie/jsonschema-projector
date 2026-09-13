package component

import "testing"

func TestGeneratedContainerDecode(t *testing.T) {
	var value Container
	if err := value.UnmarshalJSON([]byte("{\"child\":{\"value\":\"hello\"}}")); err != nil {
		t.Fatal(err)
	}
	if _, ok := value.Child.(Text); !ok {
		t.Fatalf("got %T, want Text", value.Child)
	}
	items, err := UnmarshalContainerChildSlice([]byte("[{\"value\":\"a\"},{\"url\":\"b\"}]"))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}
}
