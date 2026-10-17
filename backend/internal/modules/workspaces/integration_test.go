package workspaces_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/furkanmeclis/traccia/backend/internal/database/db"
	module "github.com/furkanmeclis/traccia/backend/internal/modules/workspaces"
	"github.com/furkanmeclis/traccia/backend/internal/modules/workspaces/repository"
	"github.com/furkanmeclis/traccia/backend/internal/modules/workspaces/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/authctx"
	"github.com/furkanmeclis/traccia/backend/internal/platform/crypto"
	"github.com/furkanmeclis/traccia/backend/internal/platform/jwt"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type loader struct{}

func (loader) LoadPrincipal(_ *http.Request, c jwt.Claims) (authctx.Principal, error) {
	id, err := c.UserUUID()
	return authctx.Principal{UserID: id, IsSuperAdmin: c.IsSuperAdmin}, err
}

func isolatedDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL required for workspace database acceptance")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal("database configuration failed")
	}
	schema := "workspace_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE"); admin.Close() })
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal("database configuration failed")
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	cfg.ConnConfig.RuntimeParams["application_name"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal("database connection failed")
	}
	t.Cleanup(pool.Close)
	files, err := filepath.Glob("../../../migrations/*.up.sql")
	if err != nil || len(files) == 0 {
		t.Fatal("migrations unavailable")
	}
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, string(raw)); err != nil {
			t.Fatalf("migration %s: %v", filepath.Base(file), err)
		}
	}
	// Verify latest migration down/up while this throwaway schema contains no product data.
	for _, suffix := range []string{"down", "up"} {
		raw, err := os.ReadFile("../../../migrations/000032_operation_receipts." + suffix + ".sql")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, string(raw)); err != nil {
			t.Fatal(err)
		}
	}
	return pool
}

func TestWorkspaceCredentialIsolation(t *testing.T) {
	pool := isolatedDatabase(t)
	ctx := context.Background()
	owner, outsider := uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{owner, outsider} {
		_, err := pool.Exec(ctx, "INSERT INTO users (uuid,email,password_hash,name,surname,status) VALUES ($1,$2,'unused','Test','Owner','active')", id, id.String()+"@example.test")
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.New(pool).BootstrapProductOwner(ctx, owner); err != nil {
		t.Fatal(err)
	}
	tokens, _ := jwt.NewManager("test-signing-secret-only", time.Hour, time.Hour)
	human := func(id uuid.UUID) string {
		raw, _, err := tokens.IssueAccess(jwt.AccessInput{UserID: id, IsSuperAdmin: true})
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	box, err := crypto.NewSecretBox("12345678901234567890123456789012")
	if err != nil {
		t.Fatal(err)
	}
	service := usecase.New(repository.New(pool, db.New(pool), box))
	mux := http.NewServeMux()
	if err := module.RegisterRoutes(mux, service, tokens, loader{}); err != nil {
		t.Fatal(err)
	}
	type result struct {
		Success bool            `json:"success"`
		Data    json.RawMessage `json:"data"`
		Error   struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	call := func(method, path, token string, body any, want int) result {
		t.Helper()
		var content []byte
		if body != nil {
			var err error
			content, err = json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(content))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", uuid.NewString())
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		res := httptest.NewRecorder()
		mux.ServeHTTP(res, req)
		if res.Code != want {
			t.Fatalf("%s %s: status=%d want=%d response=%s", method, path, res.Code, want, res.Body.String())
		}
		var out result
		if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	ownerJWT := human(owner)
	outsiderJWT := human(outsider)
	call("POST", "/v1/workspaces", outsiderJWT, map[string]any{"name": "Unapproved"}, 403)
	call("GET", "/v1/workspaces", "", nil, 401)
	create := func(name string) uuid.UUID {
		res := call("POST", "/v1/workspaces", ownerJWT, map[string]any{"name": name}, 201)
		var out usecase.WorkspaceOutput
		if err := json.Unmarshal(res.Data, &out); err != nil {
			t.Fatal(err)
		}
		return out.Workspace.ID
	}
	w1, w2 := create("First"), create("Second")
	path := "/v1/workspaces/" + w1.String()
	other := "/v1/workspaces/" + w2.String()
	call("GET", path, outsiderJWT, nil, 403) // Even platform super_admin is not a product member.
	call("PATCH", other, ownerJWT, map[string]any{"name": "Second updated", "expected_version": 1}, 200)
	// An explicitly added read-only member can read, but cannot grant themselves owner access.
	if _, err := pool.Exec(ctx, "INSERT INTO workspace_members (workspace_id,user_id,role) SELECT w.id,u.id,'member' FROM workspaces w CROSS JOIN users u WHERE w.uuid=$1 AND u.uuid=$2", w1, outsider); err != nil {
		t.Fatal(err)
	}
	memberResult := call("GET", path, outsiderJWT, nil, 200)
	var memberOut usecase.WorkspaceOutput
	if err := json.Unmarshal(memberResult.Data, &memberOut); err != nil {
		t.Fatal(err)
	}
	if memberOut.Capabilities.Update || memberOut.Capabilities.ReadCredentials || memberOut.Capabilities.ManageCredentials {
		t.Fatal("member gained owner capabilities")
	}
	call("PATCH", path, outsiderJWT, map[string]any{"name": "Rejected"}, 403)
	call("GET", path+"/credentials", outsiderJWT, nil, 403)

	page := call("GET", "/v1/workspaces?limit=1", ownerJWT, nil, 200)
	var listed usecase.WorkspaceList
	_ = json.Unmarshal(page.Data, &listed)
	if !listed.HasMore || len(listed.Items) != 1 || listed.NextCursor == "" {
		t.Fatal("first cursor page incomplete")
	}
	next := call("GET", "/v1/workspaces?limit=1&after="+listed.NextCursor, ownerJWT, nil, 200)
	var last usecase.WorkspaceList
	_ = json.Unmarshal(next.Data, &last)
	if last.HasMore || len(last.Items) != 1 || last.Items[0].ID == listed.Items[0].ID {
		t.Fatal("cursor skipped or repeated workspace")
	}
	call("GET", "/v1/workspaces?limit=0", ownerJWT, nil, 400)
	call("GET", "/v1/workspaces?limit=2&limit=3", ownerJWT, nil, 400)
	call("POST", "/v1/workspaces", ownerJWT, map[string]any{"name": "bad", "user_id": outsider}, 400)
	expiry := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	mint := func(audience string, scopes []string, token string) usecase.CreateCredentialOutput {
		res := call("POST", path+"/credentials", token, usecase.CreateCredentialInput{Name: "Tool", Audience: audience, Scopes: scopes, ExpiresAt: expiry}, 201)
		var c usecase.CreateCredentialOutput
		if err := json.Unmarshal(res.Data, &c); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(c.Token, usecase.TokenPrefix) {
			t.Fatal("missing one-time token")
		}
		return c
	}
	c1 := mint("api", []string{"workspace.read"}, ownerJWT)
	c2 := mint("api", []string{"workspace.read", "credentials.read", "credentials.manage"}, ownerJWT)
	mcp := mint("mcp", []string{"workspace.read"}, ownerJWT)
	call("GET", path, "trc_"+strings.Repeat("a", 43), nil, 401)
	limited := call("GET", path, c1.Token, nil, 200)
	var limitedOut usecase.WorkspaceOutput
	_ = json.Unmarshal(limited.Data, &limitedOut)
	if limitedOut.Capabilities.Update || limitedOut.Capabilities.ManageCredentials || limitedOut.Capabilities.ReadCredentials {
		t.Fatal("read-only token gained capabilities")
	}

	call("GET", other, c1.Token, nil, 403)
	call("GET", "/v1/workspaces", c1.Token, nil, 403)
	call("PATCH", path, c1.Token, map[string]any{"name": "escalation"}, 403)
	call("GET", path, mcp.Token, nil, 403)
	call("GET", path+"/credentials", c1.Token, nil, 403)
	credentials := call("GET", path+"/credentials", ownerJWT, nil, 200)
	if bytes.Contains(credentials.Data, []byte(c1.Token)) || bytes.Contains(credentials.Data, []byte("token_hash")) {
		t.Fatal("credential material leaked into list")
	}
	hash := sha256.Sum256([]byte(c1.Token))
	var stored []byte
	if err := pool.QueryRow(ctx, "SELECT token_hash FROM workspace_tool_credentials WHERE uuid=$1", c1.Credential.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stored, hash[:]) || bytes.Equal(stored, []byte(c1.Token)) {
		t.Fatal("expected hash-only credential storage")
	}
	// Delegation is bounded, but can be operated entirely through the API.
	child := mint("api", []string{"workspace.read"}, c2.Token)
	call("POST", path+"/credentials", c2.Token, usecase.CreateCredentialInput{Name: "Escalated", Audience: "api", Scopes: []string{"workspace.write"}, ExpiresAt: expiry}, 403)
	call("POST", path+"/credentials", c2.Token, usecase.CreateCredentialInput{Name: "Longer", Audience: "api", Scopes: []string{"workspace.read"}, ExpiresAt: expiry.Add(time.Hour)}, 403)
	call("POST", path+"/credentials", c2.Token, usecase.CreateCredentialInput{Name: "Audience", Audience: "mcp", Scopes: []string{"workspace.read"}, ExpiresAt: expiry}, 403)
	call("DELETE", other+"/credentials/"+c1.Credential.ID.String(), ownerJWT, nil, 404)
	call("GET", path, c1.Token, nil, 200)
	call("DELETE", path+"/credentials/"+c1.Credential.ID.String(), c2.Token, nil, 200)
	call("GET", path, c1.Token, nil, 401)
	call("GET", path, c2.Token, nil, 200)
	call("GET", path, child.Token, nil, 200) // Each credential has independent lifecycle.
	// Wrong audience is also rejected through the registry; MCP will use this same boundary.
	registry, _ := service.Registry()
	_, err = registry.Execute(usecase.WithToolCredential(ctx, mcp.Token), operations.Call{OperationID: "workspaces.get", Channel: operations.MCP, WorkspaceID: w1, Input: []byte("{}")})
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, "UPDATE workspace_tool_credentials SET expires_at=created_at + interval '1 microsecond' WHERE uuid=$1", child.Credential.ID)
	if err != nil {
		t.Fatal(err)
	}
	call("GET", path, child.Token, nil, 401)
	_, err = pool.Exec(ctx, "UPDATE workspace_members SET active=FALSE WHERE workspace_id=(SELECT id FROM workspaces WHERE uuid=$1)", w1)
	if err != nil {
		t.Fatal(err)
	}
	call("GET", path, c2.Token, nil, 401)
	call("GET", path, ownerJWT, nil, 403)
	call("GET", other, ownerJWT, nil, 200)
	_, err = pool.Exec(ctx, "UPDATE users SET status='disabled' WHERE uuid=$1", owner)
	if err != nil {
		t.Fatal(err)
	}
	call("GET", other, ownerJWT, nil, 403)
	call("GET", "/v1/workspaces", ownerJWT, nil, 401)
}
