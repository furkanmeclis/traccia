package operations_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/google/uuid"
)

type captureRunner struct{ fingerprints [][]byte }

func (r *captureRunner) RunMutation(ctx context.Context, m operations.Mutation, run func(context.Context) ([]byte, error)) ([]byte, error) {
	r.fingerprints = append(r.fingerprints, bytes.Clone(m.Fingerprint))
	if err := m.Authorize(ctx); err != nil {
		return nil, err
	}
	return run(ctx)
}
func TestFingerprintFidelityAndPreclaimValidation(t *testing.T) {
	type input struct {
		Number json.Number `json:"number"`
		Text   string      `json:"text"`
		Items  []int       `json:"items"`
	}
	type output struct {
		OK bool `json:"ok"`
	}
	meta := metadata()
	meta.Idempotent = true
	op, err := operations.New(meta, func(input) []operations.FieldError { return nil }, func(context.Context, operations.Scope, input) (output, error) { return output{true}, nil }, func(in, out *jsonschema.Schema) error { in.Properties["number"].Type = "number"; return nil })
	if err != nil {
		t.Fatal(err)
	}
	actor, workspace := uuid.New(), uuid.New()
	r, err := operations.NewRegistry(authorizerFunc(func(context.Context, operations.AuthorizationRequest) (operations.Identity, error) {
		return operations.Identity{ActorID: actor, WorkspaceID: workspace}, nil
	}), op)
	if err != nil {
		t.Fatal(err)
	}
	capture := &captureRunner{}
	r = r.WithMutationRunner(capture)
	raw := []string{`{"number":9007199254740993,"text":"🐾 İstanbul","items":[1,2]}`, `{ "items": [1,2], "text": "\ud83d\udc3e İstanbul", "number": 9007199254740993 }`, `{"number":9007199254740992,"text":"🐾 İstanbul","items":[1,2]}`, `{"number":1,"text":"x","items":[1,2]}`, `{"number":1.0,"text":"x","items":[1,2]}`, `{"number":1,"text":"x","items":[2,1]}`, `{"number":1,"text":"\\ud800","items":[]}`}
	for _, body := range raw {
		_, err := r.Execute(context.Background(), operations.Call{OperationID: meta.ID, Channel: operations.API, WorkspaceID: workspace, IdempotencyKey: "fidelity", Input: []byte(body)})
		if err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(capture.fingerprints[0], capture.fingerprints[1]) {
		t.Fatal("canonical equivalent fingerprints differ")
	}
	for _, pair := range [][2]int{{0, 2}, {3, 4}, {3, 5}} {
		if bytes.Equal(capture.fingerprints[pair[0]], capture.fingerprints[pair[1]]) {
			t.Fatal("distinct exact inputs merged")
		}
	}
	for _, body := range []string{`{"number":1,"number":2,"text":"x","items":[]}`, `{"number":1,"text":"\ud800","items":[]}`, `{"number":1,"text":"\udfff","items":[]}`, `{"number":1,"text":"x","items":[],"actor":"spoof"}`, `{"number":1,"text":"x","items":[]} {}`} {
		_, err := r.Execute(context.Background(), operations.Call{OperationID: meta.ID, Channel: operations.API, WorkspaceID: workspace, IdempotencyKey: "invalid", Input: []byte(body)})
		code(t, err, operations.Validation)
	}
	if len(capture.fingerprints) != len(raw) {
		t.Fatal("invalid structure reached claim")
	}
}
