package contractcoverage

import (
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"reflect"
	"testing"
)

func TestHTTPBoundCompositionProjection(t *testing.T) {
	s, e := Decode([]byte(`{"type":"object","properties":{"view_id":{"type":"string"},"nested":{"type":"object","properties":{"view_id":{"type":"string"}},"required":["view_id"]}},"required":["view_id","nested"],"oneOf":[{"type":"object","properties":{"view_id":{"type":"string"},"name":{"type":"string"}},"required":["view_id","name"]},{"type":"object","properties":{"view_id":{"type":"string"},"name":{"type":"string"}},"required":["view_id","name"]}],"const":{"view_id":"literal"}}`))
	if e != nil {
		t.Fatal(e)
	}
	omitHTTPBoundFields(s, map[string]string{"view_id": "viewID"})
	if !reflect.DeepEqual(s["required"], []any{"nested"}) {
		t.Fatal(s)
	}
	branches := s["oneOf"].([]any)
	if len(branches) != 2 {
		t.Fatal("branch multiplicity changed")
	}
	for _, v := range branches {
		b := v.(Object)
		if !reflect.DeepEqual(b["required"], []any{"name"}) {
			t.Fatal(b)
		}
		if _, ok := b["properties"].(Object)["view_id"]; ok {
			t.Fatal("bound property retained")
		}
	}
	nested := s["properties"].(Object)["nested"].(Object)
	if !reflect.DeepEqual(nested["required"], []any{"view_id"}) || s["const"].(Object)["view_id"] != "literal" {
		t.Fatal("nested data changed")
	}
}

func TestHTTPBodyProjectionRejectsReintroducedBoundBranch(t *testing.T) {
	domain, e := Decode([]byte(`{"type":"object","additionalProperties":false,"properties":{"view_id":{"type":"string","format":"uuid"},"name":{"type":"string"},"nested":{"type":"object","additionalProperties":false,"properties":{"view_id":{"type":"string"}},"required":["view_id"]}},"required":["view_id","name","nested"],"allOf":[{"anyOf":[{"oneOf":[{"type":"object","additionalProperties":false,"properties":{"view_id":{"type":"string","format":"uuid"},"name":{"type":"string"},"nested":{"type":"object","additionalProperties":false,"properties":{"view_id":{"type":"string"}},"required":["view_id"]}},"required":["view_id","name","nested"]}]}]}]}`))
	if e != nil {
		t.Fatal(e)
	}
	// Independent expected wire declaration: only nested instance retains view_id.
	body, e := Decode([]byte(`{"type":"object","additionalProperties":false,"properties":{"name":{"type":"string"},"nested":{"type":"object","additionalProperties":false,"properties":{"view_id":{"type":"string"}},"required":["view_id"]}},"required":["name","nested"],"allOf":[{"anyOf":[{"oneOf":[{"type":"object","additionalProperties":false,"properties":{"name":{"type":"string"},"nested":{"type":"object","additionalProperties":false,"properties":{"view_id":{"type":"string"}},"required":["view_id"]}},"required":["name","nested"]}]}]}]}`))
	if e != nil {
		t.Fatal(e)
	}
	metadata := operations.Metadata{ID: "fixture.views_update", HTTPMethod: "POST", HTTPPath: "/v1/workspaces/{workspaceID}/views/{viewID}"}
	op := Object{"parameters": []any{Object{"name": "workspaceID", "in": "path", "required": true, "schema": Object{"type": "string", "format": "uuid"}}, Object{"name": "viewID", "in": "path", "required": true, "schema": Object{"type": "string", "format": "uuid"}}}, "x-traccia-input-binding": Object{"view_id": "path:viewID", "body": "body"}, "requestBody": Object{"required": true, "content": Object{"application/json": Object{"schema": body}}}}
	compare := func() string {
		diff := "not compared"
		checkHTTP(metadata, op, Object{}, domain, object(Object{}), func(string, ...any) {}, func(_ string, surface string, want, got any, _ Object) {
			if surface == "HTTP/body" {
				diff = Difference(want, got, "body")
			}
		})
		return diff
	}
	if diff := compare(); diff != "" {
		t.Fatal(diff)
	}
	branch := body["allOf"].([]any)[0].(Object)["anyOf"].([]any)[0].(Object)["oneOf"].([]any)[0].(Object)
	branch["properties"].(Object)["view_id"] = Object{"type": "string", "format": "uuid"}
	branch["required"] = append(branch["required"].([]any), "view_id")
	if compare() == "" {
		t.Fatal("HTTP comparator accepted a required path-owned field inside body branch")
	}
}
