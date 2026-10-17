package workspaces

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/furkanmeclis/traccia/backend/internal/modules/workspaces/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/authctx"
	coverage "github.com/furkanmeclis/traccia/backend/internal/platform/contractcoverage"
	"github.com/furkanmeclis/traccia/backend/internal/platform/jwt"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/google/uuid"
)

type routeAuthorizer func(context.Context, operations.AuthorizationRequest) (operations.Identity, error)

func (f routeAuthorizer) Authorize(c context.Context, r operations.AuthorizationRequest) (operations.Identity, error) {
	return f(c, r)
}

type routeRunner func(context.Context, operations.Mutation, func(context.Context) ([]byte, error)) ([]byte, error)

func (f routeRunner) RunMutation(c context.Context, m operations.Mutation, run func(context.Context) ([]byte, error)) ([]byte, error) {
	return f(c, m, run)
}

type routeLoader struct{ actor uuid.UUID }

func (l routeLoader) LoadPrincipal(*http.Request, jwt.Claims) (authctx.Principal, error) {
	return authctx.Principal{UserID: l.actor}, nil
}

type routeOutput struct {
	Accepted bool `json:"accepted"`
}

func routeOperation[I any](t *testing.T, meta operations.Metadata, runError error) *operations.Operation {
	t.Helper()
	op, err := operations.New(meta, func(I) []operations.FieldError { return nil }, func(context.Context, operations.Scope, I) (routeOutput, error) { return routeOutput{true}, runError }, func(in, out *jsonschema.Schema) error {
		if field := in.Properties["credential_id"]; field != nil {
			*field = jsonschema.Schema{Type: "string", Format: "uuid"}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return op
}

func TestActualHTTPAdapterMapsBindingOptionsAndDTOs(t *testing.T) {
	registry, err := usecase.New(nil).Registry()
	if err != nil {
		t.Fatal(err)
	}
	catalog := map[string]operations.Metadata{}
	for _, contract := range registry.Catalog() {
		catalog[contract.Metadata.ID] = contract.Metadata
	}
	actor := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	workspace := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	credential := uuid.MustParse("33333333-3333-4333-8333-333333333333")
	tokens, err := jwt.NewManager("synthetic-routing-test-secret", time.Hour, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := tokens.IssueAccess(jwt.AccessInput{UserID: actor})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, id, method, path, body, expected string
		status                                 int
		noAuth                                 bool
		media                                  string
		key                                    []string
	}{
		{name: "query", id: "workspaces.list", method: "GET", path: "/v1/workspaces?limit=3&after=" + credential.String(), expected: `{"limit":3,"after":"` + credential.String() + `"}`, status: 200},
		{name: "query defaults", id: "workspaces.list", method: "GET", path: "/v1/workspaces", expected: `{"limit":20,"after":""}`, status: 200},
		{name: "unexpected query", id: "workspaces.list", method: "GET", path: "/v1/workspaces?ignored=yes", status: 400},
		{name: "duplicate query", id: "workspaces.list", method: "GET", path: "/v1/workspaces?limit=1&limit=2", status: 400},
		{name: "workspace path outside DTO", id: "workspaces.get", method: "GET", path: "/v1/workspaces/" + workspace.String(), expected: `{}`, status: 200},
		{name: "credential path inside DTO", id: "credentials.revoke", method: "DELETE", path: "/v1/workspaces/" + workspace.String() + "/credentials/" + credential.String(), expected: `{"credential_id":"` + credential.String() + `"}`, status: 200, key: []string{"stable-retry-key"}},
		{name: "exact body and header", id: "workspaces.update", method: "PATCH", path: "/v1/workspaces/" + workspace.String(), body: `{"name":"İstanbul 🧭","expected_version":9007199254740993}`, expected: `{"name":"İstanbul 🧭","expected_version":9007199254740993}`, status: 200, key: []string{"stable-retry-key"}},
		{name: "missing key", id: "workspaces.update", method: "PATCH", path: "/v1/workspaces/" + workspace.String(), body: `{"name":"name","expected_version":1}`, status: 400},
		{name: "duplicate key", id: "workspaces.update", method: "PATCH", path: "/v1/workspaces/" + workspace.String(), body: `{"name":"name","expected_version":1}`, status: 400, key: []string{"a", "b"}},
		{name: "comma key", id: "workspaces.update", method: "PATCH", path: "/v1/workspaces/" + workspace.String(), body: `{"name":"name","expected_version":1}`, status: 400, key: []string{"a,b"}},
		{name: "body identity cannot grant", id: "workspaces.update", method: "PATCH", path: "/v1/workspaces/" + workspace.String(), body: `{"name":"name","expected_version":1,"actor_id":"` + actor.String() + `"}`, status: 400, key: []string{"key"}},
		{name: "media failure", id: "workspaces.update", method: "PATCH", path: "/v1/workspaces/" + workspace.String(), body: `{}`, status: 415, media: "text/plain", key: []string{"key"}},
		{name: "auth failure", id: "workspaces.get", method: "GET", path: "/v1/workspaces/" + workspace.String(), status: 401, noAuth: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			meta := catalog[tc.id]
			var op *operations.Operation
			switch tc.id {
			case "workspaces.list":
				op = routeOperation[usecase.ListInput](t, meta, nil)
			case "workspaces.get":
				op = routeOperation[usecase.EmptyInput](t, meta, nil)
			case "credentials.revoke":
				op = routeOperation[usecase.RevokeInput](t, meta, nil)
			case "workspaces.update":
				op = routeOperation[usecase.UpdateInput](t, meta, nil)
			}
			var input json.RawMessage
			var selected uuid.UUID
			var mutationKey string
			bound, err := operations.NewRegistry(routeAuthorizer(func(_ context.Context, r operations.AuthorizationRequest) (operations.Identity, error) {
				input = append(json.RawMessage(nil), r.Input...)
				selected = r.WorkspaceID
				if r.Channel != operations.API {
					t.Error("wrong channel")
				}
				return operations.Identity{ActorID: actor, WorkspaceID: r.WorkspaceID}, nil
			}), op)
			if err != nil {
				t.Fatal(err)
			}
			bound = bound.WithMutationRunner(routeRunner(func(c context.Context, m operations.Mutation, run func(context.Context) ([]byte, error)) ([]byte, error) {
				mutationKey = m.Key
				if err := m.Authorize(c); err != nil {
					return nil, err
				}
				return run(c)
			}))
			mux := http.NewServeMux()
			if err := RegisterRegistryRoutes(mux, bound, tokens, routeLoader{actor}); err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			if !tc.noAuth {
				request.Header.Set("Authorization", "Bearer "+token)
			}
			media := tc.media
			if media == "" {
				media = "application/json"
			}
			request.Header.Set("Content-Type", media)
			for _, key := range tc.key {
				request.Header.Add("Idempotency-Key", key)
			}
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, request)
			if response.Code != tc.status {
				t.Fatalf("status %d, expected %d: %s", response.Code, tc.status, response.Body.String())
			}
			if tc.expected != "" {
				got, err := operations.DecodeJSON(input)
				if err != nil {
					t.Fatal(err)
				}
				want, err := operations.DecodeJSON([]byte(tc.expected))
				if err != nil {
					t.Fatal(err)
				}
				if diff := coverage.Difference(want, got, "input"); diff != "" {
					t.Fatalf("%s: %s", diff, input)
				}
				if meta.Scope == operations.WorkspaceScope && selected != workspace {
					t.Fatal("workspace option lost")
				}
				if meta.Idempotent && mutationKey != "stable-retry-key" {
					t.Fatal("idempotency key not forwarded")
				}
				var envelope struct {
					Data json.RawMessage `json:"data"`
				}
				if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(envelope.Data, []byte(`{"accepted":true}`)) {
					t.Fatal("typed output changed")
				}
			}
		})
	}
}

func TestHTTPDomainErrorStatusAndEnvelopeCoverage(t *testing.T) {
	doc, err := coverage.ReadOpenAPI("../../../docs/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	registry, err := usecase.New(nil).Registry()
	if err != nil {
		t.Fatal(err)
	}
	var meta operations.Metadata
	for _, c := range registry.Catalog() {
		if c.Metadata.ID == "workspaces.get" {
			meta = c.Metadata
		}
	}
	actor, workspace := uuid.New(), uuid.New()
	tokens, _ := jwt.NewManager("synthetic-error-test-secret", time.Hour, time.Hour)
	token, _, _ := tokens.IssueAccess(jwt.AccessInput{UserID: actor})
	for code, status := range map[operations.Code]int{operations.Unauthenticated: 401, operations.Forbidden: 403, operations.Validation: 400, operations.NotFound: 404, operations.Conflict: 409, operations.IdempotencyConflict: 409, operations.VersionConflict: 409, operations.Unavailable: 503, operations.Canceled: 408, operations.Internal: 500} {
		t.Run(string(code), func(t *testing.T) {
			op := routeOperation[usecase.EmptyInput](t, meta, &operations.Error{Code: code, Details: []operations.FieldError{{Field: "name", Code: "invalid"}}})
			bound, err := operations.NewRegistry(routeAuthorizer(func(context.Context, operations.AuthorizationRequest) (operations.Identity, error) {
				return operations.Identity{ActorID: actor, WorkspaceID: workspace}, nil
			}), op)
			if err != nil {
				t.Fatal(err)
			}
			mux := http.NewServeMux()
			if err := RegisterRegistryRoutes(mux, bound, tokens, routeLoader{actor}); err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest("GET", "/v1/workspaces/"+workspace.String(), nil)
			request.Header.Set("Authorization", "Bearer "+token)
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, request)
			if response.Code != status {
				t.Fatalf("expected %d got %d", status, response.Code)
			}
			if err := coverage.ValidateHTTP(doc, meta.ID, status, response.Body.Bytes()); err != nil {
				t.Fatalf("wire error not documented: %v", err)
			}
		})
	}
}
