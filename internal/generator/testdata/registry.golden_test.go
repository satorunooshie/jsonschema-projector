package component

import (
	"errors"
	"sync"
	"testing"
)

func TestRegistry(t *testing.T) {
	decode := func(data []byte) (Component, error) { return Button{Component: "Future"}, nil }
	var group sync.WaitGroup
	for i := 0; i < 32; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			RegisterComponentVariant("Future", decode)
			_, _ = UnmarshalComponent([]byte("{\"component\":\"Future\"}"))
			RegisterComponentVariant("Future", nil)
		}()
	}
	group.Wait()
	_, err := UnmarshalComponent([]byte("{\"component\":\"Future\"}"))
	if !errors.Is(err, ErrUnknownVariant) {
		t.Fatalf("got %v, want unknown variant after unregister", err)
	}
}
