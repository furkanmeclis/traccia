package workspaces_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/furkanmeclis/traccia/backend/internal/database/db"
	"github.com/furkanmeclis/traccia/backend/internal/modules/workspaces/repository"
	"github.com/furkanmeclis/traccia/backend/internal/modules/workspaces/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/authctx"
	"github.com/furkanmeclis/traccia/backend/internal/platform/crypto"
	"github.com/furkanmeclis/traccia/backend/internal/platform/dbtx"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/google/uuid"
)

func TestDurableMutationReceipts(t *testing.T) {
	pool := isolatedDatabase(t)
	ctx, cancelTest := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelTest()
	owner := uuid.New()
	if _, err := pool.Exec(ctx, "INSERT INTO users(uuid,email,password_hash,name,surname,status) VALUES($1,$2,'unused','Test','Owner','active')", owner, owner.String()+"@example.test"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.New(pool).BootstrapProductOwner(ctx, owner); err != nil {
		t.Fatal(err)
	}
	ctx = authctx.WithPrincipal(ctx, authctx.Principal{UserID: owner})
	box, err := crypto.NewSecretBox("12345678901234567890123456789012")
	if err != nil {
		t.Fatal(err)
	}
	repo := repository.New(pool, db.New(pool), box)
	service := usecase.New(repo)
	registry, err := service.Registry()
	if err != nil {
		t.Fatal(err)
	}
	check := func(err error, code operations.Code) {
		t.Helper()
		var e *operations.Error
		if !errors.As(err, &e) || e.Code != code {
			t.Fatalf("expected %s, got %v", code, err)
		}
	}
	call := func(op, key string, w uuid.UUID, input string) ([]byte, error) {
		return registry.Execute(ctx, operations.Call{OperationID: op, Channel: operations.API, WorkspaceID: w, IdempotencyKey: key, Input: []byte(input)})
	}
	const parallel = 8
	results := make([][]byte, parallel)
	errs := make([]error, parallel)
	var wg sync.WaitGroup
	for i := range parallel {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = call("workspaces.create", "same-create", uuid.Nil, `{"name":"Concurrent 🐾"}`)
		}()
	}
	wg.Wait()
	for i := range parallel {
		if errs[i] != nil {
			t.Fatal(errs[i])
		}
		if !bytes.Equal(results[0], results[i]) {
			t.Fatal("concurrent responses differ")
		}
	}
	var out usecase.WorkspaceOutput
	if err := json.Unmarshal(results[0], &out); err != nil {
		t.Fatal(err)
	}
	w := out.Workspace.ID
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM workspaces").Scan(&count); err != nil || count != 1 {
		t.Fatalf("create count=%d err=%v", count, err)
	}
	_, err = call("workspaces.create", "same-create", uuid.Nil, `{"name":"Different"}`)
	check(err, operations.IdempotencyConflict)
	// Concurrent changed inputs yield exactly one durable winner and one explicit conflict.
	wg = sync.WaitGroup{}
	errs = make([]error, 2)
	for i := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := []string{"A", "B"}[i]
			_, errs[i] = call("workspaces.create", "different-create", uuid.Nil, `{"name":"`+name+`"}`)
		}()
	}
	wg.Wait()
	successes := 0
	for _, e := range errs {
		if e == nil {
			successes++
		} else {
			check(e, operations.IdempotencyConflict)
		}
	}
	if successes != 1 {
		t.Fatal("expected one different-input winner")
	}
	// Different keys race on one expected version; the database condition picks one.
	wg = sync.WaitGroup{}
	errs = make([]error, 2)
	versions := make([][]byte, 2)
	for i := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			versions[i], errs[i] = call("workspaces.update", []string{"update-a", "update-b"}[i], w, `{"name":"Edited","expected_version":1}`)
		}()
	}
	wg.Wait()
	winner := -1
	for i, e := range errs {
		if e == nil {
			if winner != -1 {
				t.Fatal("stale edit won")
			}
			winner = i
		} else {
			check(e, operations.VersionConflict)
		}
	}
	if winner < 0 {
		t.Fatal("no version winner")
	}
	_, err = call("workspaces.update", "later", w, `{"name":"Later","expected_version":2}`)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := call("workspaces.update", []string{"update-a", "update-b"}[winner], w, `{ "expected_version": 1, "name": "Edited" }`)
	if err != nil || !bytes.Equal(replay, versions[winner]) {
		t.Fatal("old result was not replayed exactly")
	}
	_, err = call("workspaces.update", "stale-new", w, `{"name":"Stale","expected_version":1}`)
	check(err, operations.VersionConflict)
	// Expiration while blocked on the receipt cannot use transaction-start time.
	expires := time.Now().Add(time.Hour).UTC()
	expiringBody, _ := json.Marshal(usecase.CreateCredentialInput{Name: "Expiring waiter", Audience: "api", Scopes: []string{"workspace.write"}, ExpiresAt: expires})
	expiringResult, err := call("credentials.create", "expiring-waiter", w, string(expiringBody))
	if err != nil {
		t.Fatal(err)
	}
	var expiring usecase.CreateCredentialOutput
	if err := json.Unmarshal(expiringResult, &expiring); err != nil {
		t.Fatal(err)
	}
	lock, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Rollback(context.Background()) }()
	winnerKey := []string{"update-a", "update-b"}[winner]
	if _, err := lock.Exec(ctx, "UPDATE operation_receipts SET result=result WHERE idempotency_key=$1", winnerKey); err != nil {
		t.Fatal(err)
	}
	waited := make(chan error, 1)
	go func() {
		_, e := registry.Execute(usecase.WithToolCredential(ctx, expiring.Token), operations.Call{OperationID: "workspaces.update", Channel: operations.API, WorkspaceID: w, IdempotencyKey: winnerKey, Input: []byte(`{"name":"Edited","expected_version":1}`)})
		waited <- e
	}()
	deadline := time.Now().Add(5 * time.Second)
	blocked := false
	for time.Now().Before(deadline) {
		if err := pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND application_name=current_setting('application_name') AND wait_event_type='Lock' AND query LIKE '-- name: ClaimOperationReceipt%')").Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !blocked {
		_ = lock.Rollback(ctx)
		t.Fatal("waiter never blocked on claim")
	}
	// Expire from the database clock while the exact application waiter is
	// still blocked. This deliberately happens after its transaction started:
	// NOW() would accept the token, whereas current clock_timestamp() must deny.
	if _, err := pool.Exec(ctx, "UPDATE workspace_tool_credentials SET expires_at=clock_timestamp() WHERE uuid=$1", expiring.Credential.ID); err != nil {
		t.Fatal(err)
	}
	var transitioned bool
	if err := pool.QueryRow(ctx, `SELECT c.expires_at <= clock_timestamp() AND EXISTS(
 SELECT 1 FROM pg_stat_activity a WHERE a.datname=current_database()
 AND a.application_name=current_setting('application_name') AND a.wait_event_type='Lock'
 AND a.query LIKE '-- name: ClaimOperationReceipt%' AND a.xact_start < c.expires_at)
 FROM workspace_tool_credentials c WHERE c.uuid=$1`, expiring.Credential.ID).Scan(&transitioned); err != nil {
		t.Fatal(err)
	}
	if !transitioned {
		t.Fatal("credential did not expire between waiter transaction start and receipt release")
	}

	if err := lock.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	check(<-waited, operations.Unauthenticated)
	// Corrupt or schema-invalid non-secret receipts fail closed too.
	for _, bad := range [][]byte{[]byte("{"), []byte("{}"), []byte{}} {
		if _, err := pool.Exec(ctx, "UPDATE operation_receipts SET result=$1 WHERE idempotency_key='same-create'", bad); err != nil {
			t.Fatal(err)
		}
		_, err = call("workspaces.create", "same-create", uuid.Nil, `{"name":"Concurrent 🐾"}`)
		check(err, operations.Unavailable)
	}
	if _, err := pool.Exec(ctx, "UPDATE operation_receipts SET result=$1 WHERE idempotency_key='same-create'", results[0]); err != nil {
		t.Fatal(err)
	}
	// A service restart retains exact sensitive bytes without plaintext in storage.
	credentialBody, _ := json.Marshal(usecase.CreateCredentialInput{Name: "Secret", Audience: "api", Scopes: []string{"workspace.read", "credentials.manage"}, ExpiresAt: time.Now().Add(time.Hour).UTC()})
	secret, err := call("credentials.create", "secret", w, string(credentialBody))
	if err != nil {
		t.Fatal(err)
	}
	var token usecase.CreateCredentialOutput
	if err := json.Unmarshal(secret, &token); err != nil {
		t.Fatal(err)
	}
	var stored []byte
	var encrypted bool
	if err := pool.QueryRow(ctx, "SELECT result,encrypted FROM operation_receipts WHERE idempotency_key='secret' AND workspace_id=$1", w).Scan(&stored, &encrypted); err != nil {
		t.Fatal(err)
	}
	if !encrypted || bytes.Contains(stored, []byte(token.Token)) {
		t.Fatal("raw secret persisted in receipt")
	}
	registry, err = usecase.New(repository.New(pool, db.New(pool), box)).Registry()
	if err != nil {
		t.Fatal(err)
	}
	replay, err = call("credentials.create", "secret", w, string(credentialBody))
	if err != nil || !bytes.Equal(secret, replay) {
		t.Fatal("restart secret replay differs")
	}
	// A narrower current credential cannot use the same actor's receipt to recover wider grants.
	limitedBody, _ := json.Marshal(usecase.CreateCredentialInput{Name: "Limited", Audience: "api", Scopes: []string{"credentials.manage"}, ExpiresAt: time.Now().Add(2 * time.Hour).UTC()})
	limited, err := call("credentials.create", "limited", w, string(limitedBody))
	if err != nil {
		t.Fatal(err)
	}
	var limitedToken usecase.CreateCredentialOutput
	if err := json.Unmarshal(limited, &limitedToken); err != nil {
		t.Fatal(err)
	}
	_, err = registry.Execute(usecase.WithToolCredential(ctx, limitedToken.Token), operations.Call{OperationID: "credentials.create", Channel: operations.API, WorkspaceID: w, IdempotencyKey: "secret", Input: credentialBody})
	check(err, operations.Forbidden)
	// Current token revocation, audience and workspace selection cannot recover secrets.
	_, err = registry.Execute(usecase.WithToolCredential(ctx, token.Token), operations.Call{OperationID: "credentials.create", Channel: operations.MCP, WorkspaceID: w, IdempotencyKey: "secret", Input: credentialBody})
	check(err, operations.Forbidden)
	otherResult, err := call("workspaces.create", "other-space", uuid.Nil, `{"name":"Other scope"}`)
	if err != nil {
		t.Fatal(err)
	}
	var other usecase.WorkspaceOutput
	if err := json.Unmarshal(otherResult, &other); err != nil {
		t.Fatal(err)
	}
	_, err = registry.Execute(usecase.WithToolCredential(ctx, token.Token), operations.Call{OperationID: "credentials.create", Channel: operations.API, WorkspaceID: other.Workspace.ID, IdempotencyKey: "secret", Input: credentialBody})
	check(err, operations.Forbidden)
	scoped, err := call("credentials.create", "secret", other.Workspace.ID, string(credentialBody))
	if err != nil || bytes.Equal(scoped, secret) {
		t.Fatal("workspace receipts merged")
	}
	if _, err := pool.Exec(ctx, "UPDATE workspace_tool_credentials SET revoked_at=clock_timestamp() WHERE uuid=$1", token.Credential.ID); err != nil {
		t.Fatal(err)
	}
	_, err = registry.Execute(usecase.WithToolCredential(ctx, token.Token), operations.Call{OperationID: "credentials.create", Channel: operations.API, WorkspaceID: w, IdempotencyKey: "secret", Input: credentialBody})
	check(err, operations.Unauthenticated)
	// Ciphertext and associated identity tampering fail closed, without issuance.
	if _, err := pool.Exec(ctx, "UPDATE operation_receipts SET result=$1 WHERE idempotency_key='secret' AND workspace_id=$2", []byte("tampered"), w); err != nil {
		t.Fatal(err)
	}
	_, err = call("credentials.create", "secret", w, string(credentialBody))
	check(err, operations.Unavailable)
	if _, err := pool.Exec(ctx, "UPDATE operation_receipts SET result=$1,idempotency_key='copied' WHERE idempotency_key='secret' AND workspace_id=$2", stored, w); err != nil {
		t.Fatal(err)
	}
	_, err = call("credentials.create", "copied", w, string(credentialBody))
	check(err, operations.Unavailable)
	if _, err := pool.Exec(ctx, "UPDATE operation_receipts SET idempotency_key='secret' WHERE idempotency_key='copied' AND workspace_id=$1", w); err != nil {
		t.Fatal(err)
	}
	wrongBox, err := crypto.NewSecretBox("abcdefghijklmnopqrstuvwxyz123456")
	if err != nil {
		t.Fatal(err)
	}
	wrongRegistry, err := usecase.New(repository.New(pool, db.New(pool), wrongBox)).Registry()
	if err != nil {
		t.Fatal(err)
	}
	_, err = wrongRegistry.Execute(ctx, operations.Call{OperationID: "credentials.create", Channel: operations.API, WorkspaceID: w, IdempotencyKey: "secret", Input: credentialBody})
	check(err, operations.Unavailable)
	// Current user/member invalidation and workspace boundaries apply to receipts too.
	if _, err := pool.Exec(ctx, "UPDATE workspace_members SET active=FALSE WHERE workspace_id=(SELECT id FROM workspaces WHERE uuid=$1)", w); err != nil {
		t.Fatal(err)
	}
	_, err = call("credentials.create", "secret", w, string(credentialBody))
	check(err, operations.Forbidden)
	if _, err := pool.Exec(ctx, "UPDATE workspace_members SET active=TRUE WHERE workspace_id=(SELECT id FROM workspaces WHERE uuid=$1)", w); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE users SET status='disabled' WHERE uuid=$1", owner); err != nil {
		t.Fatal(err)
	}
	_, err = call("workspaces.create", "same-create", uuid.Nil, `{"name":"Concurrent 🐾"}`)
	check(err, operations.Forbidden)
	if _, err := pool.Exec(ctx, "UPDATE users SET status='active' WHERE uuid=$1", owner); err != nil {
		t.Fatal(err)
	}
	// Invalid output and cancellation must roll back both claim and domain mutation.
	type input struct {
		Name string `json:"name"`
	}
	type output struct {
		Name string `json:"name"`
	}
	for _, mode := range []string{"output", "cancel", "failure"} {
		cancelCtx, cancel := context.WithCancel(ctx)
		op, err := operations.New(operations.Metadata{ID: "test." + mode, Version: 1, Scope: operations.AccountScope, Effect: operations.Write, Permission: "test", HTTPMethod: "POST", HTTPPath: "/v1/test/" + mode, MCPName: "test_" + mode, Idempotent: true}, func(input) []operations.FieldError { return nil }, func(c context.Context, _ operations.Scope, in input) (output, error) {
			tx, ok := dbtx.Tx(c)
			if !ok {
				t.Fatal("missing ambient transaction")
			}
			if _, err := tx.Exec(c, "INSERT INTO workspaces(name) VALUES($1)", in.Name); err != nil {
				return output{}, err
			}
			if mode == "cancel" {
				cancel()
			}
			if mode == "failure" {
				return output{}, errors.New("private failure")
			}
			return output{Name: "bad"}, nil
		}, func(_, out *jsonschema.Schema) error { out.Properties["name"].Enum = []any{"valid"}; return nil })
		if err != nil {
			t.Fatal(err)
		}
		r, err := operations.NewRegistry(testAuthorizer{owner}, op)
		if err != nil {
			t.Fatal(err)
		}
		r = r.WithMutationRunner(repo)
		_, err = r.Execute(cancelCtx, operations.Call{OperationID: "test." + mode, Channel: operations.API, IdempotencyKey: mode, Input: []byte(`{"name":"Rollback"}`)})
		cancel()
		if err == nil {
			t.Fatal("expected rollback failure")
		}
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM operation_receipts WHERE idempotency_key=$1", mode).Scan(&count); err != nil || count != 0 {
			t.Fatal("failed receipt committed")
		}
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM workspaces WHERE name='Rollback'").Scan(&count); err != nil || count != 0 {
		t.Fatal("failed mutation committed")
	}
}

type testAuthorizer struct{ actor uuid.UUID }

func (a testAuthorizer) Authorize(_ context.Context, r operations.AuthorizationRequest) (operations.Identity, error) {
	return operations.Identity{ActorID: a.actor, WorkspaceID: r.WorkspaceID}, nil
}
