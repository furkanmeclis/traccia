package usecase

import "testing"

func TestClosedContracts(t *testing.T) {
	ops, e := New(nil, nil).Operations()
	if e != nil {
		t.Fatal(e)
	}
	if len(ops) != 12 {
		t.Fatalf("operations=%d", len(ops))
	}
}
