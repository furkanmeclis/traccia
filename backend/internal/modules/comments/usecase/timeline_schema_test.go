package usecase

import (
	"encoding/json"
	coverage "github.com/furkanmeclis/traccia/backend/internal/platform/contractcoverage"
	"github.com/google/jsonschema-go/jsonschema"
	"testing"
)

func TestTimelineDiscriminatorRejectsMismatchedDetails(t *testing.T) {
	in, e := jsonschema.For[CommentInput](nil)
	if e != nil {
		t.Fatal(e)
	}
	out, e := jsonschema.For[TimelineList](nil)
	if e != nil {
		t.Fatal(e)
	}
	if e = refine(in, out); e != nil {
		t.Fatal(e)
	}
	raw, e := json.Marshal(out)
	if e != nil {
		t.Fatal(e)
	}
	schema, e := coverage.Decode(raw)
	if e != nil {
		t.Fatal(e)
	}
	valid := `{"items":[{"id":"11111111-1111-1111-1111-111111111111","workspace_id":"11111111-1111-1111-1111-111111111111","issue_id":"11111111-1111-1111-1111-111111111111","actor_id":"11111111-1111-1111-1111-111111111111","event_name":"legacy.imported","channel":"unknown","name":"Legacy","version":9007199254740993,"occurred_at":"2026-01-01T00:00:00Z","detail":{"kind":"legacy","issue":null,"document":null,"comment":null,"relation":null,"attachment":null,"legacy":{"resource_type":"issues","resource_id":null}}}],"has_more":false,"next_cursor":""}`
	if e = coverage.Validate(schema, []byte(valid)); e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		name   string
		change func(map[string]any)
	}{
		{"mismatched_kind", func(d map[string]any) { d["kind"] = "comment" }},
		{"unknown_kind", func(d map[string]any) { d["kind"] = "arbitrary" }},
		{"missing_detail", func(d map[string]any) { d["legacy"] = nil }},
		{"opaque_payload", func(d map[string]any) { d["legacy"].(map[string]any)["payload"] = map[string]any{"anything": true} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var value map[string]any
			if e := json.Unmarshal([]byte(valid), &value); e != nil {
				t.Fatal(e)
			}
			d := value["items"].([]any)[0].(map[string]any)["detail"].(map[string]any)
			tc.change(d)
			raw, _ := json.Marshal(value)
			if e := coverage.Validate(schema, raw); e == nil {
				t.Fatal("invalid detail accepted")
			}
		})
	}
}
