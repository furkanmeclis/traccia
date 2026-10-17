package usecase

import "testing"

func TestConcreteRegistry(t *testing.T) {
	registry, err := New(nil).Registry()
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Catalog()) != 7 {
		t.Fatal("expected seven operations")
	}
}
