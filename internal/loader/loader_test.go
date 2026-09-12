package loader

import (
	"strings"
	"testing"
)

func TestLoadJSONRejectsTrailingTopLevelValue(t *testing.T) {
	_, err := LoadJSON(strings.NewReader(`{} {}`))
	if err == nil {
		t.Fatal("expected trailing value error")
	}
}
