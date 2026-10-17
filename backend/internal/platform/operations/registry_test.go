package operations_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/google/uuid"

	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
)

type authorizerFunc func(context.Context, operations.AuthorizationRequest) (operations.Identity, error)

func (f authorizerFunc) Authorize(ctx context.Context, r operations.AuthorizationRequest) (operations.Identity, error) {
	return f(ctx, r)
}

// These fixtures are not product operations or registered endpoints. The fake
// service records which verified actor/workspace receives each shared call.
type noteInput struct {
	Title    string `json:"title"`
	Revision int64  `json:"revision"`
}
type noteOutput struct {
	Title     string `json:"title"`
	Revision  int64  `json:"revision"`
	Workspace string `json:"workspace"`
}

func metadata() operations.Metadata {
	return operations.Metadata{ID: "fixture.note_update", Version: 1, Scope: operations.WorkspaceScope, Effect: operations.Write,
		Permission: "fixture.notes.write", HTTPMethod: "PATCH", HTTPPath: "/v1/fixture/notes/{id}", MCPName: "fixture_note_update"}
}

func validator(in noteInput) []operations.FieldError {
	if strings.TrimSpace(in.Title) == "" {
		return []operations.FieldError{{Field: "title", Code: "required"}}
	}
	return nil
}

func fixture(t *testing.T, meta operations.Metadata, handler operations.Handler[noteInput, noteOutput]) *operations.Operation {
	t.Helper()
	op, err := operations.New(meta, operations.Validator[noteInput](validator), handler, func(in, out *jsonschema.Schema) error {
		in.Properties["title"].MinLength = jsonschema.Ptr(1)
		out.Properties["title"].MinLength = jsonschema.Ptr(1)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return op
}

func code(t *testing.T, err error, want operations.Code) {
	t.Helper()
	var actual *operations.Error
	if !errors.As(err, &actual) || actual.Code != want {
		t.Fatalf("got %v, want %s", err, want)
	}
}

func TestChannelParityAndExactIntegerRoundTrip(t *testing.T) {
	actor, workspace := uuid.New(), uuid.New()
	authorized, handled := 0, 0
	auth := authorizerFunc(func(_ context.Context, req operations.AuthorizationRequest) (operations.Identity, error) {
		authorized++
		if req.Operation.Permission != "fixture.notes.write" || req.WorkspaceID != workspace {
			t.Fatal("incorrect authorization request")
		}
		return operations.Identity{ActorID: actor, WorkspaceID: workspace}, nil
	})
	op := fixture(t, metadata(), func(_ context.Context, scope operations.Scope, in noteInput) (noteOutput, error) {
		handled++
		if scope.ActorID() != actor || scope.WorkspaceID() != workspace {
			t.Fatal("unverified handler scope")
		}
		return noteOutput{Title: in.Title, Revision: in.Revision, Workspace: scope.WorkspaceID().String()}, nil
	})
	r, err := operations.NewRegistry(auth, op)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"9007199254740993", "9223372036854775807"} {
		var previous string
		for _, channel := range []operations.Channel{operations.Panel, operations.API, operations.MCP} {
			raw, err := r.Execute(context.Background(), operations.Call{OperationID: metadata().ID, Channel: channel, WorkspaceID: workspace,
				Input: json.RawMessage(`{"title":"Türkçe içerik 🧭","revision":` + n + `}`)})
			if err != nil {
				t.Fatalf("%v / cause: %v", err, errors.Unwrap(err))
			}
			if !strings.Contains(string(raw), `"revision":`+n) {
				t.Fatalf("precision lost: %s", raw)
			}
			if previous != "" && previous != string(raw) {
				t.Fatalf("channel divergence: %s", raw)
			}
			previous = string(raw)
		}
	}
	if authorized != 6 || handled != 6 {
		t.Fatalf("calls auth=%d service=%d", authorized, handled)
	}
}

func TestInputBoundaryRejectsInvalidJSONWithoutCallingUsecase(t *testing.T) {
	workspace := uuid.New()
	op := fixture(t, metadata(), func(context.Context, operations.Scope, noteInput) (noteOutput, error) {
		t.Fatal("invalid input reached usecase")
		return noteOutput{}, nil
	})
	r, err := operations.NewRegistry(authorizerFunc(func(context.Context, operations.AuthorizationRequest) (operations.Identity, error) {
		return operations.Identity{ActorID: uuid.New(), WorkspaceID: workspace}, nil
	}), op)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		`{"title":"x","revision":1,"workspace_id":"forged"}`,
		`{"title":"x"}`, `{"title":null,"revision":1}`, `null`, `[]`,
		`{"title":"x","revision":1} {}`, `{"title":"x","revision":1} broken`,
		`{"title":"x","revision":9223372036854775808}`, `{"title":"","revision":1}`,
		`{"title":"   ","revision":1}`, `{"title":`,
	} {
		t.Run(raw, func(t *testing.T) {
			_, err := r.Execute(context.Background(), operations.Call{OperationID: metadata().ID, Channel: operations.MCP, WorkspaceID: workspace, Input: json.RawMessage(raw)})
			code(t, err, operations.Validation)
		})
	}
}

func TestContentIsNotTruncated(t *testing.T) {
	workspace := uuid.New()
	content := strings.Repeat("Türkçe 🧭\n", 10000)
	op := fixture(t, metadata(), func(_ context.Context, scope operations.Scope, in noteInput) (noteOutput, error) {
		return noteOutput{Title: in.Title, Revision: in.Revision, Workspace: scope.WorkspaceID().String()}, nil
	})
	r, err := operations.NewRegistry(authorizerFunc(func(context.Context, operations.AuthorizationRequest) (operations.Identity, error) {
		return operations.Identity{ActorID: uuid.New(), WorkspaceID: workspace}, nil
	}), op)
	if err != nil {
		t.Fatal(err)
	}
	input, err := json.Marshal(noteInput{Title: content, Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := r.Execute(context.Background(), operations.Call{OperationID: metadata().ID, Channel: operations.MCP, WorkspaceID: workspace, Input: input})
	if err != nil {
		t.Fatal(err)
	}
	var output noteOutput
	if err := json.Unmarshal(raw, &output); err != nil {
		t.Fatal(err)
	}
	if output.Title != content {
		t.Fatal("content changed or truncated")
	}
}

func TestAuthorizationIsResolvedOnEveryCall(t *testing.T) {
	workspace := uuid.New()
	calls := 0
	op := fixture(t, metadata(), func(_ context.Context, scope operations.Scope, in noteInput) (noteOutput, error) {
		return noteOutput{Title: in.Title, Revision: in.Revision, Workspace: scope.WorkspaceID().String()}, nil
	})
	r, err := operations.NewRegistry(authorizerFunc(func(context.Context, operations.AuthorizationRequest) (operations.Identity, error) {
		calls++
		if calls > 1 {
			return operations.Identity{}, &operations.Error{Code: operations.Forbidden}
		}
		return operations.Identity{ActorID: uuid.New(), WorkspaceID: workspace}, nil
	}), op)
	if err != nil {
		t.Fatal(err)
	}
	call := operations.Call{OperationID: metadata().ID, Channel: operations.API, WorkspaceID: workspace, Input: json.RawMessage(`{"title":"x","revision":1}`)}
	if _, err := r.Execute(context.Background(), call); err != nil {
		t.Fatal(err)
	}
	_, err = r.Execute(context.Background(), call)
	code(t, err, operations.Forbidden)
}

func TestScopeAndIdentityFailClosed(t *testing.T) {
	workspace := uuid.New()
	for _, tc := range []struct {
		name     string
		identity operations.Identity
		err      error
		want     operations.Code
	}{
		{"no actor", operations.Identity{WorkspaceID: workspace}, nil, operations.Unauthenticated},
		{"wrong workspace", operations.Identity{ActorID: uuid.New(), WorkspaceID: uuid.New()}, nil, operations.Forbidden},
		{"denied", operations.Identity{}, &operations.Error{Code: operations.Forbidden}, operations.Forbidden},
		{"backend failure", operations.Identity{}, errors.New("private database connection string"), operations.Internal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			op := fixture(t, metadata(), func(context.Context, operations.Scope, noteInput) (noteOutput, error) {
				t.Fatal("unauthorized usecase call")
				return noteOutput{}, nil
			})
			r, err := operations.NewRegistry(authorizerFunc(func(context.Context, operations.AuthorizationRequest) (operations.Identity, error) {
				return tc.identity, tc.err
			}), op)
			if err != nil {
				t.Fatal(err)
			}
			_, err = r.Execute(context.Background(), operations.Call{OperationID: metadata().ID, Channel: operations.MCP, WorkspaceID: workspace})
			code(t, err, tc.want)
			encoded, _ := json.Marshal(err)
			if strings.Contains(string(encoded), "private") {
				t.Fatal("diagnostic leaked")
			}
		})
	}
	r, err := operations.NewRegistry(authorizerFunc(func(context.Context, operations.AuthorizationRequest) (operations.Identity, error) {
		t.Fatal("missing workspace reached authorizer")
		return operations.Identity{}, nil
	}), fixture(t, metadata(), nil))
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.Execute(context.Background(), operations.Call{OperationID: metadata().ID, Channel: operations.API})
	code(t, err, operations.Validation)
}

func TestOutputSchemaFailureAndDownstreamErrorAreSanitized(t *testing.T) {
	workspace := uuid.New()
	for _, handler := range []operations.Handler[noteInput, noteOutput]{
		func(context.Context, operations.Scope, noteInput) (noteOutput, error) {
			return noteOutput{Title: ""}, nil
		},
		func(context.Context, operations.Scope, noteInput) (noteOutput, error) {
			return noteOutput{}, errors.New("secret SQL")
		},
	} {
		r, err := operations.NewRegistry(authorizerFunc(func(context.Context, operations.AuthorizationRequest) (operations.Identity, error) {
			return operations.Identity{ActorID: uuid.New(), WorkspaceID: workspace}, nil
		}), fixture(t, metadata(), handler))
		if err != nil {
			t.Fatal(err)
		}
		_, err = r.Execute(context.Background(), operations.Call{OperationID: metadata().ID, Channel: operations.MCP, WorkspaceID: workspace, Input: json.RawMessage(`{"title":"x","revision":1}`)})
		code(t, err, operations.Internal)
		serialized, _ := json.Marshal(err)
		if strings.Contains(string(serialized), "secret") || strings.Contains(err.Error(), "secret") {
			t.Fatal("error leaked")
		}
	}
}

func TestCatalogRejectsDuplicateOrMissingMappingsAndKeepsSchemasOwned(t *testing.T) {
	auth := authorizerFunc(func(context.Context, operations.AuthorizationRequest) (operations.Identity, error) {
		return operations.Identity{ActorID: uuid.New()}, nil
	})
	for _, field := range []string{"id", "version", "permission", "scope", "effect", "method", "path", "mcp"} {
		m := metadata()
		switch field {
		case "id":
			m.ID = ""
		case "version":
			m.Version = 0
		case "permission":
			m.Permission = ""
		case "scope":
			m.Scope = ""
		case "effect":
			m.Effect = ""
		case "method":
			m.HTTPMethod = ""
		case "path":
			m.HTTPPath = ""
		case "mcp":
			m.MCPName = ""
		}
		if _, err := operations.NewRegistry(auth, fixture(t, m, nil)); err == nil {
			t.Fatalf("accepted missing %s", field)
		}
	}
	first := fixture(t, metadata(), nil)
	for _, duplicate := range []operations.Metadata{
		metadata(),
		{ID: "fixture.other", Version: 1, Scope: operations.WorkspaceScope, Effect: operations.Read, Permission: "fixture.read", HTTPMethod: "PATCH", HTTPPath: metadata().HTTPPath, MCPName: "other"},
		{ID: "fixture.other", Version: 1, Scope: operations.WorkspaceScope, Effect: operations.Read, Permission: "fixture.read", HTTPMethod: "GET", HTTPPath: "/v1/other", MCPName: metadata().MCPName},
	} {
		if _, err := operations.NewRegistry(auth, first, fixture(t, duplicate, nil)); err == nil {
			t.Fatal("accepted duplicate mapping")
		}
	}
	r, err := operations.NewRegistry(auth, first)
	if err != nil {
		t.Fatal(err)
	}
	c := r.Catalog()[0]
	if c.Available {
		t.Fatal("unbound operation advertised available")
	}
	for _, raw := range []json.RawMessage{c.Input, c.Output, c.Error} {
		var schema jsonschema.Schema
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatal(err)
		}
		if _, err := schema.Resolve(nil); err != nil {
			t.Fatal(err)
		}
	}
	c.Input[0] = '!'
	if !json.Valid(r.Catalog()[0].Input) {
		t.Fatal("catalog mutation changed internal schema")
	}
}

func TestUnavailableAccountScopeAndCanceledCall(t *testing.T) {
	m := metadata()
	m.Scope = operations.AccountScope
	authCalls := 0
	r, err := operations.NewRegistry(authorizerFunc(func(_ context.Context, req operations.AuthorizationRequest) (operations.Identity, error) {
		authCalls++
		return operations.Identity{ActorID: uuid.New()}, nil
	}), fixture(t, m, nil))
	if err != nil {
		t.Fatal(err)
	}
	call := operations.Call{OperationID: m.ID, Channel: operations.API}
	_, err = r.Execute(context.Background(), call)
	code(t, err, operations.Unavailable)
	if authCalls != 1 {
		t.Fatal("unbound operation bypassed authorization")
	}
	call.WorkspaceID = uuid.New()
	_, err = r.Execute(context.Background(), call)
	code(t, err, operations.Validation)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = r.Execute(ctx, call)
	code(t, err, operations.Canceled)
	if !errors.Is(err, context.Canceled) {
		t.Fatal("lost cancellation cause")
	}
}

func TestSchemaInferenceAndResolutionFailAtConstruction(t *testing.T) {
	_, genericErr := operations.New[noteInput, noteOutput](metadata(), validator, nil, func(in, _ *jsonschema.Schema) error {
		in.AdditionalProperties = &jsonschema.Schema{}
		return nil
	})
	if genericErr == nil {
		t.Fatal("accepted generic open schema")
	}
	if _, err := operations.New[map[string]any, noteOutput](metadata(), func(map[string]any) []operations.FieldError { return nil }, nil, nil); err == nil {
		t.Fatal("accepted generic DTO")
	}
	if _, err := operations.New[noteInput, noteOutput](metadata(), nil, nil, nil); err == nil {
		t.Fatal("accepted missing validator")
	}
	_, err := operations.New[noteInput, noteOutput](metadata(), validator, nil, func(in, _ *jsonschema.Schema) error {
		in.Properties["title"].Pattern = "["
		return nil
	})
	if err == nil {
		t.Fatal("accepted invalid schema")
	}
	_, err = operations.New[noteInput, noteOutput](metadata(), validator, nil, func(_, _ *jsonschema.Schema) error { return fmt.Errorf("invalid domain schema") })
	if err == nil {
		t.Fatal("ignored schema refinement error")
	}
}

func TestDuplicateObjectKeysCannotBypassNestedSchema(t *testing.T) {
	type config struct {
		Name  string `json:"name"`
		Level int    `json:"level,omitempty"`
	}
	type input struct {
		Config  config   `json:"config"`
		Configs []config `json:"configs,omitempty"`
	}
	workspace := uuid.New()
	handled := 0
	op, err := operations.New(metadata(), func(input) []operations.FieldError { return nil },
		func(_ context.Context, _ operations.Scope, in input) (noteOutput, error) {
			handled++
			if in.Config.Level > 1 {
				t.Fatal("schema bypass: handler received merged unvalidated level")
			}
			return noteOutput{Title: in.Config.Name}, nil
		}, func(in, _ *jsonschema.Schema) error {
			in.Properties["config"].Properties["level"].Maximum = jsonschema.Ptr(float64(1))
			return nil
		})
	if err != nil {
		t.Fatal(err)
	}
	r, err := operations.NewRegistry(authorizerFunc(func(context.Context, operations.AuthorizationRequest) (operations.Identity, error) {
		return operations.Identity{ActorID: uuid.New(), WorkspaceID: workspace}, nil
	}), op)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		`{"config":{"name":"old","level":9},"config":{"name":"new"}}`,
		`{"config":{"name":"old","name":"new"}}`,
		`{"config":{"name":"old","\u006eame":"new"}}`,
		`{"config":{"name":"ok"},"configs":[{"name":"old","level":9,"level":1}]}`,
		"{\"config\":{\"name\":\"" + string([]byte{0xff}) + "\"}}",
	} {
		t.Run(fmt.Sprintf("invalid_%x", raw), func(t *testing.T) {
			_, err := r.Execute(context.Background(), operations.Call{OperationID: metadata().ID, Channel: operations.MCP, WorkspaceID: workspace, Input: json.RawMessage(raw)})
			code(t, err, operations.Validation)
		})
	}
	if handled != 0 {
		t.Fatalf("invalid input reached handler %d times", handled)
	}
	// Reusing property names in separate objects is valid, including array items.
	for _, raw := range []string{
		`{"config":{"name":"ok"}}`,
		`{"config":{"name":"ok","level":1},"configs":[{"name":"first","level":1},{"name":"second","level":1}]}`,
		`{"config":{"name":"ok"},"configs":[]}`,
	} {
		_, err := r.Execute(context.Background(), operations.Call{OperationID: metadata().ID, Channel: operations.MCP, WorkspaceID: workspace, Input: json.RawMessage(raw)})
		if err != nil {
			t.Fatalf("valid independent object keys rejected: %v", err)
		}
	}
	if handled != 3 {
		t.Fatalf("valid handler calls: %d", handled)
	}
}
