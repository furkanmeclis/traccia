package usecase

import "testing"

func TestConcreteRegistry(t *testing.T) {
	registry, err := New(nil).Registry()
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Catalog()) != 10 {
		t.Fatal("expected ten operations")
	}
}
