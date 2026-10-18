package searchviews_test

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/furkanmeclis/traccia/backend/internal/database/db"
	repo "github.com/furkanmeclis/traccia/backend/internal/modules/searchviews/repository"
	uc "github.com/furkanmeclis/traccia/backend/internal/modules/searchviews/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/uuid"
	"strings"
	"testing"
)

func TestSavedViewsFullTraversalDeletedAnchorAndCurrentPermissions(t *testing.T) {
	f := newSearchFixture(t)
	q := uc.TeamQuery(query())
	definition := uc.ViewQuery{Resource: "team", Team: &q}
	columns := []uc.Column{{Key: "name", Width: 240}}
	var first uc.SavedView
	for n := 0; n < 107; n++ {
		raw, e := f.call("saved_views.create", uuid.NewString(), f.w, uc.ViewCreateInput{Name: "private " + strings.Repeat("v", n%20), Definition: definition, Columns: columns})
		v := decodeResult[uc.ViewOutput](t, raw, e).View
		if n == 0 {
			first = v
		}
	}
	in := uc.ViewListInput{Resource: "team", Limit: 17}
	raw, e := f.call("saved_views.list", "", f.w, in)
	page := decodeResult[uc.ViewList](t, raw, e)
	anchor := page.Items[len(page.Items)-1]
	if _, e = f.call("saved_views.delete", uuid.NewString(), f.w, uc.ViewVersionInput{ViewID: anchor.ID, ExpectedRevision: anchor.Revision}); e != nil {
		t.Fatal(e)
	}
	seen := map[uuid.UUID]bool{}
	for {
		for _, v := range page.Items {
			if seen[v.ID] {
				t.Fatal("duplicate view")
			}
			seen[v.ID] = true
		}
		if !page.HasMore {
			break
		}
		in.After = page.NextCursor
		raw, e = f.call("saved_views.list", "", f.w, in)
		page = decodeResult[uc.ViewList](t, raw, e)
	}
	if len(seen) != 107 {
		t.Fatal(len(seen))
	}
	if _, e = f.pool.Exec(f.ctx, "UPDATE workspace_members SET role='member' WHERE workspace_id=(SELECT id FROM workspaces WHERE uuid=$1) AND user_id=(SELECT id FROM users WHERE uuid=$2)", f.w, f.actor); e != nil {
		t.Fatal(e)
	}
	raw, e = f.call("saved_views.get", "", f.w, uc.ViewInput{ViewID: first.ID})
	v := decodeResult[uc.ViewOutput](t, raw, e).View
	if v.Capabilities.Manage || v.Capabilities.Update {
		t.Fatal("stale capabilities")
	}
	_, e = f.call("saved_views.delete", uuid.NewString(), f.w, uc.ViewVersionInput{ViewID: v.ID, ExpectedRevision: v.Revision})
	checkCode(t, e, operations.Forbidden)
}
func TestQueryCaptureRollbackAndPostCaptureAuthorization(t *testing.T) {
	f := newSearchFixture(t)
	f.createTeam(t, "ATM")
	in := uc.TeamCreateInput{RequestID: uuid.New(), Query: uc.TeamQuery(query()), Limit: 100}
	if _, e := f.pool.Exec(f.ctx, `CREATE FUNCTION fail_query_item() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'forced query item failure';END $$;CREATE TRIGGER forced_query_item BEFORE INSERT ON search_query_items FOR EACH ROW EXECUTE FUNCTION fail_query_item()`); e != nil {
		t.Fatal(e)
	}
	_, e := f.call("team_queries.create", "", f.w, in)
	checkCode(t, e, operations.Internal)
	var n int
	if e = f.pool.QueryRow(f.ctx, "SELECT count(*) FROM search_query_sessions").Scan(&n); e != nil || n != 0 {
		t.Fatal(n, e)
	}
	if _, e = f.pool.Exec(f.ctx, "DROP TRIGGER forced_query_item ON search_query_items;DROP FUNCTION fail_query_item()"); e != nil {
		t.Fatal(e)
	}
	raw, e := f.call("team_queries.create", "", f.w, in)
	p := decodeResult[uc.TeamPage](t, raw, e)
	if len(p.Items) != 1 {
		t.Fatal(p)
	}
	again, e := f.call("team_queries.create", "", f.w, in)
	if e != nil || !bytes.Equal(raw, again) {
		t.Fatal("retry identity", e)
	}
	calls := 0
	r := repo.New(f.pool, db.New(f.pool))
	id := uuid.New()
	_, e = r.Capture(f.ctx, f.w, f.actor, "team", uc.Capture{RequestID: id, Query: query(), TTLSeconds: 1800, Limit: 100}, func(context.Context) error {
		calls++
		if calls == 2 {
			return &operations.Error{Code: operations.Forbidden}
		}
		return nil
	})
	checkCode(t, e, operations.Forbidden)
	if calls != 2 {
		t.Fatal(calls)
	}
	if e = f.pool.QueryRow(f.ctx, "SELECT count(*) FROM search_query_sessions WHERE request_uuid=$1", id).Scan(&n); e != nil || n != 0 {
		t.Fatal("post-capture denial committed", n, e)
	}
}
func TestSearchNewInputValidationPreservesState(t *testing.T) {
	f := newSearchFixture(t)
	base := uc.TeamCreateInput{RequestID: uuid.New(), Query: uc.TeamQuery(query()), Limit: 100}
	cases := []struct {
		name   string
		change func(map[string]any)
	}{{"unknown", func(v map[string]any) { v["unexpected"] = true }}, {"search_nul", func(v map[string]any) { v["query"].(map[string]any)["search"] = "keep\x00text" }}, {"query_version", func(v map[string]any) { v["query"].(map[string]any)["version"] = 2 }}, {"wrong_field", func(v map[string]any) {
		v["query"].(map[string]any)["filter"] = map[string]any{"kind": "text", "field": "title", "op": "eq", "value": "literal"}
	}}, {"empty_group", func(v map[string]any) {
		v["query"].(map[string]any)["filter"] = map[string]any{"kind": "all", "children": []any{}}
	}}, {"empty_set", func(v map[string]any) {
		v["query"].(map[string]any)["filter"] = map[string]any{"kind": "uuid", "field": "id", "op": "in", "ids": []any{}}
	}}, {"depth", func(v map[string]any) {
		var node any = map[string]any{"kind": "text", "field": "name", "op": "eq", "value": "literal"}
		for range 8 {
			node = map[string]any{"kind": "not", "child": node}
		}
		v["query"].(map[string]any)["filter"] = node
	}}, {"nodes", func(v map[string]any) {
		children := []any{}
		for range 128 {
			children = append(children, map[string]any{"kind": "text", "field": "name", "op": "eq", "value": "literal"})
		}
		v["query"].(map[string]any)["filter"] = map[string]any{"kind": "all", "children": children}
	}}, {"sort_duplicate", func(v map[string]any) {
		sort := map[string]any{"field": "name", "direction": "asc", "nulls": "last"}
		v["query"].(map[string]any)["sort"] = []any{sort, sort}
	}}, {"ttl", func(v map[string]any) { v["ttl_seconds"] = 59 }}, {"raw_bytes", func(v map[string]any) { v["query"].(map[string]any)["search"] = strings.Repeat("x", 131073) }}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(base)
			v, e := operations.DecodeJSON(raw)
			if e != nil {
				t.Fatal(e)
			}
			tc.change(v.(map[string]any))
			_, e = f.call("team_queries.create", "", f.w, v)
			checkCode(t, e, operations.Validation)
		})
	}
	var count int
	if e := f.pool.QueryRow(f.ctx, "SELECT count(*) FROM search_query_sessions").Scan(&count); e != nil || count != 0 {
		t.Fatal("invalid input persisted", count, e)
	}
}
