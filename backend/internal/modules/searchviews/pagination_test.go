package searchviews_test

import (
	"context"
	"encoding/json"
	"github.com/furkanmeclis/traccia/backend/internal/database/db"
	repo "github.com/furkanmeclis/traccia/backend/internal/modules/searchviews/repository"
	uc "github.com/furkanmeclis/traccia/backend/internal/modules/searchviews/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func seedIssues(t *testing.T, f searchFixture, n int) {
	t.Helper()
	team := f.createTeam(t, "BIG")
	status := f.status(t, team.ID)
	if _, e := f.pool.Exec(f.ctx, `INSERT INTO issues(workspace_id,team_id,number,status_id,title,priority) SELECT w.id,t.id,g,s.id,'row '||lpad(g::text,5,'0'),CASE WHEN g%2=0 THEN 'high' ELSE 'low' END FROM workspaces w JOIN teams t ON t.workspace_id=w.id JOIN workflow_statuses s ON s.team_id=t.id AND s.workspace_id=w.id CROSS JOIN generate_series(1,$4::int) g WHERE w.uuid=$1 AND t.uuid=$2 AND s.uuid=$3`, f.w, team.ID, status, n); e != nil {
		t.Fatal(e)
	}
}
func TestQueryManifestBeyondLegacyCapAndMutableSources(t *testing.T) {
	f := newSearchFixture(t)
	seedIssues(t, f, 6103)
	q := query()
	q.Sort = []uc.Sort{{Field: "title", Direction: "desc", Nulls: "last"}}
	raw, e := f.call("issue_queries.create", "", f.w, uc.IssueCreateInput{RequestID: uuid.New(), Query: uc.IssueQuery(q), Limit: 100})
	page := decodeResult[uc.IssuePage](t, raw, e)
	if page.Session.TotalCount != "6103" {
		t.Fatal(page.Session.TotalCount)
	}
	first := page.Items[0]
	if _, e = f.pool.Exec(f.ctx, "UPDATE issues SET archived=true,title='mutated',version=version+1 WHERE workspace_id=(SELECT id FROM workspaces WHERE uuid=$1)", f.w); e != nil {
		t.Fatal(e)
	}
	seen := map[uuid.UUID]bool{}
	last := ""
	for {
		for _, v := range page.Items {
			if seen[v.ID] || v.Archived || v.Title == "mutated" || last != "" && last < v.Title {
				t.Fatal("snapshot/order duplicated or live hydrated")
			}
			seen[v.ID] = true
			last = v.Title
		}
		if !page.HasMore {
			break
		}
		raw, e = f.call("issue_queries.get", "", f.w, uc.PageInput{SessionID: page.Session.ID, Limit: 100, After: page.NextCursor})
		page = decodeResult[uc.IssuePage](t, raw, e)
	}
	if len(seen) != 6103 {
		t.Fatal(len(seen))
	}
	raw, e = f.call("issue_queries.get", "", f.w, uc.PageInput{SessionID: page.Session.ID, Limit: 1})
	again := decodeResult[uc.IssuePage](t, raw, e)
	a, _ := json.Marshal(again.Items[0])
	b, _ := json.Marshal(first)
	if string(a) != string(b) {
		t.Fatal("first snapshot changed")
	}
}
func seedExpiredManifest(t *testing.T, f searchFixture, n int) uuid.UUID {
	t.Helper()
	q, _ := json.Marshal(query())
	id, request := uuid.New(), uuid.New()
	var internal, workspace int64
	if e := f.pool.QueryRow(f.ctx, `INSERT INTO search_query_sessions(uuid,workspace_id,actor_uuid,request_uuid,resource,fingerprint,query,first_limit,as_of,expires_at) SELECT $1,w.id,$3,$4,'issue',repeat('a',64),$5,100,clock_timestamp()-interval '2 hours',clock_timestamp()-interval '1 hour' FROM workspaces w WHERE w.uuid=$2 RETURNING id,workspace_id`, id, f.w, f.actor, request, q).Scan(&internal, &workspace); e != nil {
		t.Fatal(e)
	}
	if _, e := f.pool.Exec(f.ctx, `INSERT INTO search_query_items(workspace_id,session_id,ordinal,resource_uuid,snapshot) SELECT $1,$2,row_number() OVER(ORDER BY uuid),uuid,document FROM search_source_rows WHERE workspace_uuid=$3 AND resource='issue' ORDER BY uuid LIMIT $4`, workspace, internal, f.w, n); e != nil {
		t.Fatal(e)
	}
	if _, e := f.pool.Exec(f.ctx, "UPDATE search_query_sessions SET state='ready',total_count=$2 WHERE id=$1", internal, n); e != nil {
		t.Fatal(e)
	}
	return id
}
func TestQueryCleanupBoundedDrainRollbackAndExpiredTombstone(t *testing.T) {
	f := newSearchFixture(t)
	seedIssues(t, f, 2107)
	id := seedExpiredManifest(t, f, 2107)
	r := repo.New(f.pool, db.New(f.pool))
	if _, e := f.pool.Exec(f.ctx, `CREATE FUNCTION fail_query_cleanup() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'forced cleanup rollback';END $$;CREATE TRIGGER forced_query_cleanup BEFORE DELETE ON search_query_items FOR EACH ROW EXECUTE FUNCTION fail_query_cleanup()`); e != nil {
		t.Fatal(e)
	}
	if _, e := r.Cleanup(f.ctx, 1000); e == nil {
		t.Fatal("cleanup failure hidden")
	}
	var left int
	if e := f.pool.QueryRow(f.ctx, "SELECT count(*) FROM search_query_items").Scan(&left); e != nil || left != 2107 {
		t.Fatal(left, e)
	}
	if _, e := f.pool.Exec(f.ctx, "DROP TRIGGER forced_query_cleanup ON search_query_items;DROP FUNCTION fail_query_cleanup()"); e != nil {
		t.Fatal(e)
	}
	for _, want := range []int64{1000, 1000, 107, 0} {
		n, e := r.Cleanup(f.ctx, 1000)
		if e != nil || n != want {
			t.Fatal(n, want, e)
		}
	}
	var state string
	var compact bool
	var request uuid.UUID
	if e := f.pool.QueryRow(f.ctx, "SELECT state,query IS NULL,request_uuid FROM search_query_sessions WHERE uuid=$1", id).Scan(&state, &compact, &request); e != nil || state != "expired" || !compact {
		t.Fatal("large query not compacted", state, compact, e)
	}
	_, err := f.call("issue_queries.create", "", f.w, uc.IssueCreateInput{RequestID: request, Query: uc.IssueQuery(query()), Limit: 100})
	checkCode(t, err, operations.Conflict)
	_, e := f.call("issue_queries.get", "", f.w, uc.PageInput{SessionID: id, Limit: 100})
	checkCode(t, e, operations.Conflict)
	var count int
	if e = f.pool.QueryRow(f.ctx, "SELECT count(*) FROM search_query_sessions WHERE uuid=$1", id).Scan(&count); e != nil || count != 1 {
		t.Fatal("expired identity lost", e)
	}
}

type pausePage struct {
	armed            atomic.Bool
	entered, release chan struct{}
}

func (p *pausePage) TraceQueryStart(c context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	if strings.HasPrefix(d.SQL, "-- name: ListSearchItems") && p.armed.CompareAndSwap(true, false) {
		close(p.entered)
		select {
		case <-p.release:
		case <-c.Done():
		}
	}
	return c
}
func (*pausePage) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}
func TestQueryPageExpiryCleanupFence(t *testing.T) {
	f := newSearchFixture(t)
	f.createTeam(t, "FNC")
	r := repo.New(f.pool, db.New(f.pool))
	q := query()
	page, e := r.Capture(f.ctx, f.w, f.actor, "team", uc.Capture{RequestID: uuid.New(), Query: q, TTLSeconds: 1, Limit: 100}, func(context.Context) error { return nil })
	if e != nil {
		t.Fatal(e)
	}
	tracer := &pausePage{entered: make(chan struct{}), release: make(chan struct{})}
	tracer.armed.Store(true)
	cfg := f.pool.Config()
	cfg.ConnConfig.Tracer = tracer
	pool, e := pgxpool.NewWithConfig(f.ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	done := make(chan error, 1)
	go func() {
		_, e := repo.New(pool, db.New(pool)).Page(f.ctx, f.w, f.actor, "team", uc.PageInput{SessionID: page.Session.ID, Limit: 100})
		done <- e
	}()
	select {
	case <-tracer.entered:
	case <-time.After(time.Second):
		t.Fatal("page did not acquire header")
	}
	delay := time.Until(page.Session.ExpiresAt) + 20*time.Millisecond
	if delay > 0 {
		time.Sleep(delay)
	}
	n, e := r.Cleanup(f.ctx, 1000)
	if e != nil || n != 0 {
		t.Fatal("cleanup crossed active header", n, e)
	}
	close(tracer.release)
	select {
	case e = <-done:
		checkCode(t, e, operations.Conflict)
	case <-time.After(3 * time.Second):
		t.Fatal("page stuck")
	}
	n, e = r.Cleanup(f.ctx, 1000)
	if e != nil || n != 1 {
		t.Fatal(n, e)
	}
}
