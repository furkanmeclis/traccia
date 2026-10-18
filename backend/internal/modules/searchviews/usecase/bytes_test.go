package usecase

import (
	"encoding/json"
	"fmt"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"strings"
	"testing"
)

func TestSemanticDomainBytePolicy(t *testing.T) {
	cases := []struct{ name, raw, expected string }{{"html", `{"text":"<>&"}`, `{"text":"<>&"}`}, {"separators", `{"text":"\u2028\u2029"}`, "{\"text\":\"\u2028\u2029\"}"}, {"literal_backslash", `{"text":"\\u2028\\u2029"}`, `{"text":"\\u2028\\u2029"}`}, {"negative_zero", `{"number":-0,"text":"-0"}`, `{"number":0,"text":"-0"}`}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v, e := operations.DecodeJSON([]byte(tc.raw))
			if e != nil {
				t.Fatal(e)
			}
			before, _ := json.Marshal(v)
			n, e := domainBytes(v)
			after, _ := json.Marshal(v)
			if e != nil || n != len(tc.expected) || string(before) != string(after) {
				t.Fatalf("size%d want%d mutation%v err%v", n, len(tc.expected), string(before) != string(after), e)
			}
		})
	}
	t.Run("ast_before_encoding", func(t *testing.T) {
		var node any = map[string]any{"text": strings.Repeat("x", 131073)}
		for range 9 {
			node = map[string]any{"child": node}
		}
		errors := wireLimits(map[string]any{"query": map[string]any{"filter": node}}, nil)
		if len(errors) != 1 || errors[0].Code != "depth_limit" {
			t.Fatal(errors)
		}
	})
}

func TestSemanticBoundaryNormalizesNegativeZeroOnlyForMeasurement(t *testing.T) {
	ids := []any{}
	for n := 1; n <= 100; n++ {
		ids = append(ids, fmt.Sprintf("00000000-0000-0000-0000-%012x", n))
	}
	number := map[string]any{"kind": "number", "field": "effort", "op": "eq", "number": json.Number("-0")}
	children := []any{number}
	filter := map[string]any{"kind": "all", "children": children}
	query := map[string]any{"version": 1, "archived": "active", "search": "", "filter": filter, "sort": []any{}}
	value := map[string]any{"request_id": "01234567-89ab-cdef-0123-456789abcdef", "query": query, "ttl_seconds": nil, "limit": 100}
	leaf := func(values []any) any {
		return map[string]any{"kind": "uuid", "field": "id", "op": "in", "ids": values}
	}
	for {
		candidate := append(append([]any{}, children...), leaf(ids))
		filter["children"] = candidate
		n, e := domainBytes(value)
		if e != nil {
			t.Fatal(e)
		}
		if n > 128*1024 {
			filter["children"] = children
			break
		}
		children = candidate
	}
	for n := 1; n <= 100; n++ {
		candidate := append(append([]any{}, children...), leaf(ids[:n]))
		filter["children"] = candidate
		size, e := domainBytes(value)
		if e != nil {
			t.Fatal(e)
		}
		if size > 128*1024 {
			if n == 1 {
				filter["children"] = children
			} else {
				filter["children"] = append(append([]any{}, children...), leaf(ids[:n-1]))
			}
			break
		}
	}
	n, e := domainBytes(value)
	if e != nil {
		t.Fatal(e)
	}
	gap := 128*1024 - n
	if gap < 0 || gap > 256 {
		t.Fatal("fixture cannot fill boundary", gap)
	}
	query["search"] = strings.Repeat("x", gap)
	for _, spelling := range []string{"-0", "0"} {
		number["number"] = json.Number(spelling)
		raw, e := json.Marshal(value)
		if e != nil {
			t.Fatal(e)
		}
		var typed IssueCreateInput
		if e = json.Unmarshal(raw, &typed); e != nil {
			t.Fatal(e)
		}
		if errors := ValidateQuery("issue", Query(typed.Query)); len(errors) > 0 {
			t.Fatal("invalid boundary fixture", errors)
		}
		before := string(raw)
		if errors := wireLimits(value, raw); len(errors) > 0 {
			t.Fatal(spelling, errors)
		}
		size, e := domainBytes(value)
		after, _ := json.Marshal(value)
		if e != nil || size != 128*1024 || before != string(after) {
			t.Fatal(size, e, "input mutated")
		}
		query["search"] = strings.Repeat("x", gap+1)
		if errors := wireLimits(value, nil); len(errors) != 1 || errors[0].Code != "semantic_bytes_limit" {
			t.Fatal(errors)
		}
		query["search"] = strings.Repeat("x", gap)
	}
}
