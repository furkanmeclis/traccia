package contractcoverage_test

import (
	"encoding/json"
	coverage "github.com/furkanmeclis/traccia/backend/internal/platform/contractcoverage"
	"testing"
)

func recursiveSchema(name string) coverage.Object {
	return coverage.Object{"$ref": "#/$defs/" + name, "$defs": coverage.Object{name: coverage.Object{"type": "object", "additionalProperties": false, "required": []any{"value", "children"}, "properties": coverage.Object{"value": coverage.Object{"type": "string", "enum": []any{"a", "b"}}, "children": coverage.Object{"type": "array", "items": coverage.Object{"$ref": "#/$defs/" + name}}}}}}
}
func normalizeRecursive(t *testing.T, s coverage.Object) any {
	t.Helper()
	n, e := coverage.Normalize(s, s)
	if e != nil {
		t.Fatal(e)
	}
	if e = coverage.Closed(n, "schema"); e != nil {
		t.Fatal(e)
	}
	if _, e = json.Marshal(n); e != nil {
		t.Fatal("normalized graph is not finite JSON", e)
	}
	return n
}
func TestRecursiveCoverageAlphaRenamingAndDeepChanges(t *testing.T) {
	a, b := recursiveSchema("Node"), recursiveSchema("Renamed")
	na, nb := normalizeRecursive(t, a), normalizeRecursive(t, b)
	if d := coverage.Difference(na, nb, "schema"); d != "" {
		t.Fatal(d)
	}
	for _, name := range []string{"enum", "required", "additionalProperties", "items"} {
		t.Run(name, func(t *testing.T) {
			changed := recursiveSchema("Other")
			node := changed["$defs"].(coverage.Object)["Other"].(coverage.Object)
			switch name {
			case "enum":
				node["properties"].(coverage.Object)["value"].(coverage.Object)["enum"] = []any{"a"}
			case "required":
				node["required"] = []any{"children"}
			case "additionalProperties":
				node["additionalProperties"] = true
			case "items":
				node["properties"].(coverage.Object)["children"].(coverage.Object)["items"] = true
			}
			normalized, e := coverage.Normalize(changed, changed)
			if e != nil {
				t.Fatal(e)
			}
			if name == "additionalProperties" || name == "items" {
				if coverage.Closed(normalized, "schema") == nil {
					t.Fatal("recursive opaque branch accepted")
				}
			}
			if coverage.Difference(na, normalized, "schema") == "" {
				t.Fatal("deep recursive mutation was missed")
			}
		})
	}
}
func TestRecursiveCoverageRejectsUnguardedReferences(t *testing.T) {
	for _, raw := range []string{`{"$ref":"#"}`, `{"anyOf":[{"$ref":"#"},{"type":"string"}]}`, `{"$defs":{"a":{"$ref":"#/$defs/b"},"b":{"$ref":"#/$defs/a"}},"$ref":"#/$defs/a"}`, `{"$dynamicRef":"#a"}`, `{"$defs":{"a":{"$id":"same","type":"string"},"b":{"$id":"same","type":"number"}},"type":"string"}`} {
		t.Run(raw, func(t *testing.T) {
			s, e := coverage.Decode([]byte(raw))
			if e != nil {
				t.Fatal(e)
			}
			if _, e = coverage.Normalize(s, s); e == nil {
				t.Fatal("unguarded/ambiguous schema accepted")
			}
		})
	}
}
func TestRecursiveCoverageEnvelopeAndConjunction(t *testing.T) {
	a, b := normalizeRecursive(t, recursiveSchema("A")), normalizeRecursive(t, recursiveSchema("B"))
	wrapper := coverage.Object{"type": "object", "additionalProperties": false, "required": []any{"a", "b"}, "properties": coverage.Object{"a": a, "b": b}}
	n := normalizeRecursive(t, wrapper)
	if d := coverage.Difference(n, n, "wrapper"); d != "" {
		t.Fatal(d)
	}
	raw := []byte(`{"$defs":{"s":{"type":"string","minLength":5}},"$ref":"#/$defs/s","minLength":8,"maxLength":3}`)
	s, e := coverage.Decode(raw)
	if e != nil {
		t.Fatal(e)
	}
	n, e = coverage.Normalize(s, s)
	if e != nil {
		t.Fatal(e)
	}
	if e = coverage.Closed(n, "schema"); e != nil {
		t.Fatal(e)
	}
	missing, e := coverage.Decode([]byte(`{"$defs":{"s":{"type":"string","minLength":5}},"$ref":"#/$defs/s","maxLength":3}`))
	if e != nil {
		t.Fatal(e)
	}
	m, e := coverage.Normalize(missing, missing)
	if e != nil {
		t.Fatal(e)
	}
	if coverage.Difference(n, m, "schema") == "" {
		t.Fatal("reference sibling constraint lost")
	}
}
func TestRecursiveCoverageInstancesAndRefSiblings(t *testing.T) {
	a := recursiveSchema("A")
	node := a["$defs"].(coverage.Object)["A"].(coverage.Object)
	node["properties"].(coverage.Object)["children"].(coverage.Object)["items"] = coverage.Object{"$ref": "#/$defs/A", "maxProperties": 2}
	b := recursiveSchema("B")
	bnode := b["$defs"].(coverage.Object)["B"].(coverage.Object)
	bnode["properties"].(coverage.Object)["value"].(coverage.Object)["enum"] = []any{"z"}
	na, nb := normalizeRecursive(t, a), normalizeRecursive(t, b)
	wrapper := coverage.Object{"type": "object", "additionalProperties": false, "required": []any{"a", "b"}, "properties": coverage.Object{"a": na, "b": nb}}
	n := normalizeRecursive(t, wrapper).(coverage.Object)
	for _, c := range []struct {
		name, raw string
		valid     bool
	}{{"valid", `{"a":{"value":"a","children":[{"value":"b","children":[]}]},"b":{"value":"z","children":[{"value":"z","children":[]}]}}`, true}, {"wrong-b-context", `{"a":{"value":"a","children":[]},"b":{"value":"z","children":[{"value":"a","children":[]}]}}`, false}, {"wrong-a-child", `{"a":{"value":"a","children":[{"value":"z","children":[]}]},"b":{"value":"z","children":[]}}`, false}} {
		t.Run(c.name, func(t *testing.T) {
			e := coverage.Validate(n, []byte(c.raw))
			if (e == nil) != c.valid {
				t.Fatalf("validation=%v want=%v", e, c.valid)
			}
		})
	}
	for _, value := range []string{"false", "true"} {
		t.Run("boolean-"+value, func(t *testing.T) {
			s, e := coverage.Decode([]byte(`{"$defs":{"v":` + value + `},"$ref":"#/$defs/v"}`))
			if e != nil {
				t.Fatal(e)
			}
			e = coverage.Closed(s, "schema")
			if (e == nil) != (value == "false") {
				t.Fatalf("boolean closure=%v", e)
			}
		})
	}
}
func TestRecursiveCoverageResourceAnchorsAndMultiplicity(t *testing.T) {
	local := recursiveSchema("N")
	anchored, e := coverage.Decode([]byte(`{"$ref":"https://example.test/tree#node","$defs":{"tree":{"$id":"https://example.test/tree","$anchor":"node","type":"object","additionalProperties":false,"required":["value","children"],"properties":{"value":{"type":"string","enum":["a","b"]},"children":{"type":"array","items":{"$ref":"#node"}}}}}}`))
	if e != nil {
		t.Fatal(e)
	}
	a, b := normalizeRecursive(t, local), normalizeRecursive(t, anchored)
	if d := coverage.Difference(a, b, "schema"); d != "" {
		t.Fatal("anchor-context mismatch", d)
	}
	wrap := func(tree any, reverse, duplicate bool) coverage.Object {
		branches := []any{tree, coverage.Object{"type": "null"}}
		if reverse {
			branches[0], branches[1] = branches[1], branches[0]
		}
		if duplicate {
			branches = append(branches, tree)
		}
		return coverage.Object{"oneOf": branches}
	}
	x := normalizeRecursive(t, wrap(a, false, false))
	y := normalizeRecursive(t, wrap(b, true, false))
	if d := coverage.Difference(x, y, "schema"); d != "" {
		t.Fatal("oneOf reordering changed equivalence", d)
	}
	z := normalizeRecursive(t, wrap(b, false, true))
	if coverage.Difference(x, z, "schema") == "" {
		t.Fatal("oneOf multiplicity lost")
	}
}
func TestRecursiveCoverageDistinctDefinitionAfterCycle(t *testing.T) {
	build := func(tag string) coverage.Object {
		return coverage.Object{"$ref": "#/$defs/N", "$defs": coverage.Object{"N": coverage.Object{"type": "object", "additionalProperties": false, "required": []any{"child"}, "properties": coverage.Object{"child": coverage.Object{"$ref": "#/$defs/C"}}}, "C": coverage.Object{"type": "object", "additionalProperties": false, "required": []any{"back", "tag"}, "properties": coverage.Object{"back": coverage.Object{"anyOf": []any{coverage.Object{"$ref": "#/$defs/N"}, coverage.Object{"type": "null"}}}, "tag": coverage.Object{"type": "string", "enum": []any{tag}}}}}}
	}
	a, b := normalizeRecursive(t, build("a")), normalizeRecursive(t, build("b"))
	if coverage.Difference(a, b, "schema") == "" {
		t.Fatal("distinct child after cycle was not compared")
	}
}
