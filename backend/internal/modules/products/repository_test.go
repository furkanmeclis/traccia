package products_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/furkanmeclis/traccia/backend/internal/database/db"
	catalog "github.com/furkanmeclis/traccia/backend/internal/modules/products/usecase"
	project "github.com/furkanmeclis/traccia/backend/internal/modules/projects/usecase"
	ws "github.com/furkanmeclis/traccia/backend/internal/modules/workspaces/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/dbtx"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/furkanmeclis/traccia/backend/internal/platform/outbox"
	"github.com/google/uuid"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCatalogLifecycleReceiptsCanonicalProjectionRollback(t *testing.T) {
	f := newProductFixture(t)
	p := f.createProduct(t, "İstanbul ürünü 🧭")
	input := catalog.CreateEnvironmentInput{ProductID: p.ID, Name: "production", Kind: "production", URL: "https://app.example.test", DeployURL: "https://deploy.example.test/run/1", DeployProvider: "Dokploy", DeployReference: "run/1", ExpectedProductVersion: 1}
	key := uuid.NewString()
	const n = 8
	var wg sync.WaitGroup
	outs := make(chan []byte, n)
	errs := make(chan error, n)
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			raw, e := f.call("product_environments.create", key, f.w, input)
			outs <- raw
			errs <- e
		}()
	}
	wg.Wait()
	close(outs)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	var first []byte
	for raw := range outs {
		if first == nil {
			first = raw
		} else if !bytes.Equal(first, raw) {
			t.Fatal("receipt result changed")
		}
	}
	env := decodeResult[catalog.EnvironmentOutput](t, first, nil)
	input.Name = "Changed"
	_, e := f.call("product_environments.create", key, f.w, input)
	checkCode(t, e, operations.IdempotencyConflict)
	_, e = f.call("product_environments.update", uuid.NewString(), f.w, catalog.UpdateEnvironmentInput{ProductID: p.ID, EnvironmentID: env.Environment.ID, Name: "Wrong child CAS", Kind: "local", URL: "http://localhost:3000", ExpectedVersion: 99, ExpectedProductVersion: 2})
	checkCode(t, e, operations.VersionConflict)
	raw, e := f.call("products.archive", uuid.NewString(), f.w, catalog.ArchiveProductInput{ProductID: p.ID, ExpectedVersion: 2})
	arch := decodeResult[catalog.ProductOutput](t, raw, e)
	if arch.Capabilities.Manage || !arch.Capabilities.Restore {
		t.Fatal("archive caps")
	}
	_, e = f.call("product_environments.archive", uuid.NewString(), f.w, catalog.ArchiveEnvironmentInput{ProductID: p.ID, EnvironmentID: env.Environment.ID, ExpectedVersion: 1, ExpectedProductVersion: 3})
	checkCode(t, e, operations.Conflict)
	raw, e = f.call("product_environments.get", "", f.w, catalog.EnvironmentInput{ProductID: p.ID, EnvironmentID: env.Environment.ID})
	hidden := decodeResult[catalog.EnvironmentOutput](t, raw, e)
	if hidden.Capabilities.Manage || hidden.Capabilities.Restore || hidden.Environment.Archived {
		t.Fatal("parent archive changed child or capabilities")
	}
	raw, e = f.call("products.restore", uuid.NewString(), f.w, catalog.ArchiveProductInput{ProductID: p.ID, ExpectedVersion: 3})
	decodeResult[catalog.ProductOutput](t, raw, e)
	raw, e = f.call("product_environments.archive", uuid.NewString(), f.w, catalog.ArchiveEnvironmentInput{ProductID: p.ID, EnvironmentID: env.Environment.ID, ExpectedVersion: 1, ExpectedProductVersion: 4})
	ea := decodeResult[catalog.EnvironmentOutput](t, raw, e)
	if !ea.Capabilities.Restore || ea.Capabilities.Manage || ea.ProductVersion != 5 {
		t.Fatal("child archive caps/CAS")
	}
	raw, e = f.call("product_environments.restore", uuid.NewString(), f.w, catalog.ArchiveEnvironmentInput{ProductID: p.ID, EnvironmentID: env.Environment.ID, ExpectedVersion: 2, ExpectedProductVersion: 5})
	er := decodeResult[catalog.EnvironmentOutput](t, raw, e)
	if er.ProductVersion != 6 || er.Environment.Version != 3 {
		t.Fatal("restore CAS")
	}
	var snap string
	if e = f.pool.QueryRow(f.ctx, "SELECT payload->'Payload'->'environment'->>'deploy_url' FROM outbox_events WHERE event_name='product_environments.created'").Scan(&snap); e != nil || snap != input.DeployURL {
		t.Fatal("missing reference snapshot", e)
	}
	for _, table := range []string{"workspace_history", "outbox_events", "event_deliveries", "operation_receipts"} {
		var count int
		if e = f.pool.QueryRow(f.ctx, "SELECT count(*) FROM "+table).Scan(&count); e != nil || count != 7 {
			t.Fatalf("%s=%d %v", table, count, e)
		}
	}
	consumers, e := f.workspaceRepo.DurableConsumers()
	if e != nil {
		t.Fatal(e)
	}
	worker := outbox.NewWorker(outbox.NewStore(f.pool, db.New(f.pool)), consumers, nil)
	for range 7 {
		if _, e = worker.Drain(f.ctx); e != nil {
			t.Fatal(e)
		}
	}
	raw, e = f.call("activity.list", "", f.w, ws.ListInput{Limit: 100})
	activity := decodeResult[ws.ActivityList](t, raw, e)
	if len(activity.Items) != 7 || activity.Items[0].ProductID != p.ID.String() || activity.Items[0].EnvironmentID != env.Environment.ID.String() {
		t.Fatalf("missing activity context %s", raw)
	}
	if _, e = f.pool.Exec(f.ctx, "CREATE FUNCTION reject_catalog_history() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.event_name='product_environments.created' THEN RAISE EXCEPTION 'synthetic history failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_catalog_history BEFORE INSERT ON workspace_history FOR EACH ROW EXECUTE FUNCTION reject_catalog_history()"); e != nil {
		t.Fatal(e)
	}
	input.Name = "Rollback"
	input.ExpectedProductVersion = 6
	_, e = f.call("product_environments.create", uuid.NewString(), f.w, input)
	if e == nil {
		t.Fatal("history failure ignored")
	}
	raw, e = f.call("products.get", "", f.w, catalog.ProductInput{ProductID: p.ID})
	after := decodeResult[catalog.ProductOutput](t, raw, e)
	if after.Product.Version != 6 {
		t.Fatal("history failure changed CAS")
	}
	for _, table := range []string{"workspace_history", "outbox_events", "event_deliveries", "operation_receipts"} {
		var count int
		if e = f.pool.QueryRow(f.ctx, "SELECT count(*) FROM "+table).Scan(&count); e != nil || count != 7 {
			t.Fatalf("rollback %s=%d %v", table, count, e)
		}
	}
}
func TestCatalogConcurrentParentCAS(t *testing.T) {
	f := newProductFixture(t)
	p := f.createProduct(t, "CAS")
	const n = 10
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, e := f.call("product_environments.create", uuid.NewString(), f.w, catalog.CreateEnvironmentInput{ProductID: p.ID, Name: fmt.Sprintf("env%d", i), Kind: "local", URL: "http://127.0.0.1:3000", ExpectedProductVersion: 1})
			errs <- e
		}(i)
	}
	wg.Wait()
	close(errs)
	success := 0
	for e := range errs {
		if e == nil {
			success++
		} else {
			checkCode(t, e, operations.VersionConflict)
		}
	}
	if success != 1 {
		t.Fatal("expected one CAS winner", success)
	}
	var count, version int
	if e := f.pool.QueryRow(f.ctx, "SELECT count(*) FROM product_environments").Scan(&count); e != nil || count != 1 {
		t.Fatal(count, e)
	}
	if e := f.pool.QueryRow(f.ctx, "SELECT version FROM products WHERE uuid=$1", p.ID).Scan(&version); e != nil || version != 2 {
		t.Fatal(version, e)
	}
}
func TestCatalogHistoricalProjectLinksAndReferenceLocks(t *testing.T) {
	f := newProductFixture(t)
	team := f.createTeam(t, "AA")
	a := f.createProject(t, team.ID)
	b := f.createProject(t, team.ID)
	p := f.createProduct(t, "Linked")
	raw, e := f.call("product_projects.set", uuid.NewString(), f.w, catalog.SetProjectsInput{ProductID: p.ID, ProjectIDs: []string{a.ID.String(), b.ID.String()}, ExpectedProductVersion: 1})
	decodeResult[catalog.ProductVersionOutput](t, raw, e)
	raw, e = f.call("projects.archive", uuid.NewString(), f.w, project.ArchiveProjectInput{ProjectID: a.ID, ExpectedVersion: 1})
	decodeResult[project.ProjectOutput](t, raw, e)
	raw, e = f.call("product_projects.set", uuid.NewString(), f.w, catalog.SetProjectsInput{ProductID: p.ID, ProjectIDs: []string{strings.ToUpper(a.ID.String()), b.ID.String()}, ExpectedProductVersion: 2})
	decodeResult[catalog.ProductVersionOutput](t, raw, e)
	raw, e = f.call("product_projects.set", uuid.NewString(), f.w, catalog.SetProjectsInput{ProductID: p.ID, ProjectIDs: []string{b.ID.String()}, ExpectedProductVersion: 3})
	decodeResult[catalog.ProductVersionOutput](t, raw, e)
	_, e = f.call("product_projects.set", uuid.NewString(), f.w, catalog.SetProjectsInput{ProductID: p.ID, ProjectIDs: []string{a.ID.String(), b.ID.String()}, ExpectedProductVersion: 4})
	checkCode(t, e, operations.Conflict)
	raw, e = f.call("product_projects.list", "", f.w, catalog.ProductPageInput{ProductID: p.ID, Limit: 100, Archived: true})
	old := decodeResult[catalog.ProductProjects](t, raw, e)
	if len(old.Items) != 1 || old.Items[0].ID != a.ID || !old.Items[0].Archived {
		t.Fatal("archived historical association lost")
	}
	raw, e = f.call("product_projects.set", uuid.NewString(), f.w, catalog.SetProjectsInput{ProductID: p.ID, ProjectIDs: []string{}, ExpectedProductVersion: 4})
	decodeResult[catalog.ProductVersionOutput](t, raw, e)
	var retained int
	if e = f.pool.QueryRow(f.ctx, "SELECT count(*) FROM product_projects WHERE archived").Scan(&retained); e != nil || retained != 2 {
		t.Fatal("history links deleted", e)
	}
	// The actual project row lock prevents concurrent archival between reference validation and commit.
	tx, e := f.pool.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(f.ctx)) }()
	if _, e = f.catalogRepo.SetProjects(dbtx.WithTx(f.ctx, tx), f.w, catalog.SetProjectsInput{ProductID: p.ID, ProjectIDs: []string{b.ID.String()}, ExpectedProductVersion: 5}); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(f.ctx, 150*time.Millisecond)
	defer cancel()
	_, e = f.pool.Exec(ctx, "UPDATE projects SET archived=true WHERE uuid=$1", b.ID)
	if e == nil {
		t.Fatal("archive bypassed reference SQL lock")
	}
}
func TestCatalogCompositeFKImmutableIdentityAndNames(t *testing.T) {
	f := newProductFixture(t)
	p := f.createProduct(t, "Catalog")
	q := f.createProduct(t, "Other")
	env := f.createEnvironment(t, p, "production")
	f.createEnvironment(t, q, "production") // Same environment name is valid across products.
	_, e := f.call("products.create", uuid.NewString(), f.w, catalog.CreateProductInput{Name: "catalog"})
	checkCode(t, e, operations.Conflict)
	_, e = f.call("product_environments.create", uuid.NewString(), f.w, catalog.CreateEnvironmentInput{ProductID: p.ID, Name: "PRODUCTION", Kind: "staging", URL: "https://staging.example.test", ExpectedProductVersion: 2})
	checkCode(t, e, operations.Conflict)
	raw, e := f.call("workspaces.create", uuid.NewString(), uuid.Nil, ws.CreateInput{Name: "Foreign"})
	other := decodeResult[ws.WorkspaceOutput](t, raw, e)
	var wid, pid, qid, eid, foreign int64
	if e = f.pool.QueryRow(f.ctx, "SELECT workspace_id,id FROM products WHERE uuid=$1", p.ID).Scan(&wid, &pid); e != nil {
		t.Fatal(e)
	}
	_ = f.pool.QueryRow(f.ctx, "SELECT id FROM products WHERE uuid=$1", q.ID).Scan(&qid)
	_ = f.pool.QueryRow(f.ctx, "SELECT id FROM product_environments WHERE uuid=$1", env.Environment.ID).Scan(&eid)
	_ = f.pool.QueryRow(f.ctx, "SELECT id FROM workspaces WHERE uuid=$1", other.Workspace.ID).Scan(&foreign)
	for _, probe := range []struct {
		sql  string
		args []any
	}{
		{"INSERT INTO product_environments(workspace_id,product_id,name,kind,url) VALUES($1,$2,'foreign','local','http://localhost')", []any{foreign, pid}},
		{"UPDATE products SET workspace_id=$1 WHERE id=$2", []any{foreign, pid}},
		{"UPDATE product_environments SET product_id=$1 WHERE id=$2", []any{qid, eid}},
	} {
		if _, e = f.pool.Exec(f.ctx, probe.sql, probe.args...); e == nil {
			t.Fatal("accepted foreign identity", probe.sql)
		}
	}
	_, e = f.pool.Exec(f.ctx, "CREATE TABLE widget_context_probe(workspace_id BIGINT NOT NULL,product_id BIGINT NOT NULL,environment_id BIGINT NOT NULL,FOREIGN KEY(workspace_id,product_id,environment_id) REFERENCES product_environments(workspace_id,product_id,id))")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.pool.Exec(f.ctx, "INSERT INTO widget_context_probe VALUES($1,$2,$3)", wid, pid, eid); e != nil {
		t.Fatal(e)
	}
	if _, e = f.pool.Exec(f.ctx, "INSERT INTO widget_context_probe VALUES($1,$2,$3)", wid, qid, eid); e == nil {
		t.Fatal("future widget FK accepted mismatched environment")
	}
	_, e = f.call("product_environments.get", "", f.w, catalog.EnvironmentInput{ProductID: q.ID, EnvironmentID: env.Environment.ID})
	checkCode(t, e, operations.NotFound)
	_, e = f.call("products.get", "", other.Workspace.ID, catalog.ProductInput{ProductID: p.ID})
	checkCode(t, e, operations.NotFound)
}
func TestCatalogStablePaginationFiltersAndAnchorOwnership(t *testing.T) {
	f := newProductFixture(t)
	p := f.createProduct(t, "Pages")
	q := f.createProduct(t, "Foreign parent")
	if _, e := f.pool.Exec(f.ctx, "INSERT INTO product_environments(workspace_id,product_id,name,kind,url) SELECT workspace_id,id,'Page '||n,'staging','http://localhost:3000' FROM products CROSS JOIN generate_series(1,125) n WHERE uuid=$1", p.ID); e != nil {
		t.Fatal(e)
	}
	raw, e := f.call("product_environments.list", "", f.w, catalog.EnvironmentPageInput{ProductID: p.ID, Limit: 20, Q: "Page", Kind: "staging"})
	first := decodeResult[catalog.EnvironmentList](t, raw, e)
	anchor := first.Items[len(first.Items)-1].ID
	// A mutable anchor leaves its original archived/search filters but still anchors continuation.
	if _, e = f.pool.Exec(f.ctx, "UPDATE product_environments SET archived=true,name='Renamed anchor' WHERE uuid=$1", anchor); e != nil {
		t.Fatal(e)
	}
	seen := map[uuid.UUID]bool{}
	for _, row := range first.Items {
		seen[row.ID] = true
	}
	after := first.NextCursor
	for after != "" {
		raw, e = f.call("product_environments.list", "", f.w, catalog.EnvironmentPageInput{ProductID: p.ID, Limit: 20, After: after, Q: "Page", Kind: "staging"})
		page := decodeResult[catalog.EnvironmentList](t, raw, e)
		for _, row := range page.Items {
			if seen[row.ID] {
				t.Fatal("duplicate")
			}
			seen[row.ID] = true
		}
		after = page.NextCursor
	}
	if len(seen) != 125 {
		t.Fatal("truncated cursor", len(seen))
	}
	_, e = f.call("product_environments.list", "", f.w, catalog.EnvironmentPageInput{ProductID: q.ID, Limit: 20, After: first.NextCursor, Q: "Page", Kind: "staging"})
	checkCode(t, e, operations.Validation)
	// Retain the correct public scope hash but substitute a foreign or unknown immutable anchor.
	bytes, _ := base64.RawURLEncoding.DecodeString(first.NextCursor)
	cursor := map[string]any{}
	_ = json.Unmarshal(bytes, &cursor)
	cursor["anchor"] = uuid.NewString()
	bytes, _ = json.Marshal(cursor)
	fabricated := base64.RawURLEncoding.EncodeToString(bytes)
	_, e = f.call("product_environments.list", "", f.w, catalog.EnvironmentPageInput{ProductID: p.ID, Limit: 20, After: fabricated, Q: "Page", Kind: "staging"})
	checkCode(t, e, operations.Validation)
	_, e = f.call("product_environments.list", "", f.w, catalog.EnvironmentPageInput{ProductID: p.ID, Limit: 20, After: first.NextCursor, Q: "Changed", Kind: "staging"})
	checkCode(t, e, operations.Validation)
}
func TestCatalogMetadataValidationAndReplayAuthorization(t *testing.T) {
	f := newProductFixture(t)
	for _, bad := range []string{"ftp://example.test", "https://user:password@example.test", "https://example.test/?token=secret", "javascript:alert(1)", "http://"} {
		_, e := f.call("products.create", uuid.NewString(), f.w, catalog.CreateProductInput{Name: "Bad URL", RepositoryURL: bad})
		checkCode(t, e, operations.Validation)
	}
	key := uuid.NewString()
	in := catalog.CreateProductInput{Name: "Public metadata", RepositoryURL: "http://localhost:8080/repository"}
	raw, e := f.call("products.create", key, f.w, in)
	decodeResult[catalog.ProductOutput](t, raw, e)
	if _, e = f.pool.Exec(f.ctx, "UPDATE workspace_members SET role='member' WHERE workspace_id=(SELECT id FROM workspaces WHERE uuid=$1) AND user_id=(SELECT id FROM users WHERE uuid=$2)", f.w, f.actor); e != nil {
		t.Fatal(e)
	}
	_, e = f.call("products.create", key, f.w, in)
	checkCode(t, e, operations.Forbidden)
	raw, e = f.call("products.list", "", f.w, catalog.PageInput{Limit: 100})
	list := decodeResult[catalog.ProductList](t, raw, e)
	if len(list.Items) != 1 || list.Capabilities.Manage {
		t.Fatal("member read/write capabilities")
	}
}

func TestCatalogServerContextUsesScopedCurrentReferences(t *testing.T) {
	f := newProductFixture(t)
	p := f.createProduct(t, "Context")
	q := f.createProduct(t, "Other context")
	env := f.createEnvironment(t, p, "local")
	tx, e := f.pool.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(f.ctx)) }()
	ctx := dbtx.WithTx(f.ctx, tx)
	if e = f.catalogRepo.ValidateContext(ctx, f.w, p.ID, env.Environment.ID); e != nil {
		t.Fatal(e)
	}
	checkCode(t, f.catalogRepo.ValidateContext(ctx, f.w, q.ID, env.Environment.ID), operations.NotFound)
	checkCode(t, f.catalogRepo.ValidateContext(ctx, f.w, uuid.Nil, env.Environment.ID), operations.Validation)
	locked, cancel := context.WithTimeout(f.ctx, 150*time.Millisecond)
	defer cancel()
	_, e = f.pool.Exec(locked, "UPDATE products SET archived=true WHERE uuid=$1", p.ID)
	if e == nil {
		t.Fatal("archive bypassed context lock")
	}
}
