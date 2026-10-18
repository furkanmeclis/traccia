package usecase

import "testing"

func TestQueryContractsClosed(t *testing.T) {
	ops, e := New(nil, nil).Operations()
	if e != nil {
		t.Fatal(e)
	}
	if len(ops) != 15 {
		t.Fatal(len(ops))
	}
}

func TestWirePreflightCountsMalformedFilterShapes(t *testing.T) {
	var node any = map[string]any{"field": "name"}
	for range 8 {
		node = map[string]any{"child": node}
	}
	value := map[string]any{"query": map[string]any{"filter": node}}
	errors := wireLimits(value, nil)
	if len(errors) != 1 || errors[0].Code != "depth_limit" {
		t.Fatal(errors)
	}
	children := []any{}
	for range 128 {
		children = append(children, map[string]any{})
	}
	value["query"] = map[string]any{"filter": map[string]any{"children": children}}
	errors = wireLimits(value, nil)
	if len(errors) != 1 || errors[0].Code != "nodes_limit" {
		t.Fatal(errors)
	}
}
