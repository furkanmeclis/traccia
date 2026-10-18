package personalwork_test

import (
	"encoding/json"
	pw "github.com/furkanmeclis/traccia/backend/internal/modules/personalwork/usecase"
	search "github.com/furkanmeclis/traccia/backend/internal/modules/searchviews/usecase"
	ws "github.com/furkanmeclis/traccia/backend/internal/modules/workspaces/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/uuid"
	"testing"
)

func query() search.ViewQuery {
	q := search.IssueQuery{Version: 1, Archived: "any", Search: "", Sort: []search.Sort{}}
	return search.ViewQuery{Resource: "issue", Issue: &q}
}
func TestPersonalAtomicSnapshotAndFavorites(t *testing.T) {
	f := newPersonalFixture(t)
	team := f.createTeam(t, "PW")
	i := f.create(t, team.ID)
	project := f.createProject(t, team.ID)
	raw, e := f.call("workspaces.create", uuid.NewString(), uuid.Nil, ws.CreateInput{Name: "Second"})
	other := decodeResult[ws.WorkspaceOutput](t, raw, e).Workspace
	raw, e = f.call("personal_work.config", "", f.w, pw.ConfigInput{})
	config := decodeResult[pw.ConfigOutput](t, raw, e)
	if !config.Capabilities.Manage || config.Limits.MaxWorkspaces != 32 {
		t.Fatal(config)
	}
	input := pw.PortfolioCreateInput{RequestID: uuid.New(), WorkspaceIDs: []uuid.UUID{other.ID, f.w}, Definition: query(), Limit: 100}
	raw, e = f.call("personal_portfolios.create", "", f.w, input)
	out := decodeResult[pw.PortfolioOutput](t, raw, e)
	if out.Session.TotalCount != "1" || len(out.Items) != 1 || out.Items[0].Issue.ID != i.ID || len(out.Session.Workspaces) != 2 {
		t.Fatal(out)
	}
	replay, e := f.call("personal_portfolios.create", "", f.w, input)
	if e != nil || string(raw) != string(replay) {
		t.Fatal("read replay", e)
	}
	input.Limit = 99
	_, e = f.call("personal_portfolios.create", "", f.w, input)
	checkCode(t, e, operations.IdempotencyConflict)
	// The anchor is not silently captured when omitted from the explicit set.
	input.RequestID = uuid.New()
	input.WorkspaceIDs = []uuid.UUID{other.ID}
	raw, e = f.call("personal_portfolios.create", "", f.w, input)
	empty := decodeResult[pw.PortfolioOutput](t, raw, e)
	if empty.Session.TotalCount != "0" || len(empty.Session.Workspaces) != 1 || empty.Session.Workspaces[0].ID != other.ID {
		t.Fatal(empty)
	}
	for _, target := range []pw.Target{{Kind: "issue", ID: i.ID}, {Kind: "team", ID: team.ID}, {Kind: "project", ID: project.ID}, {Kind: "workspace", ID: f.w}} {
		v := pw.FavoriteSetInput{Target: target, Active: true}
		key := uuid.NewString()
		raw, e = f.call("favorites.set", key, f.w, v)
		fav := decodeResult[pw.FavoriteSetOutput](t, raw, e).Favorite
		again, e := f.call("favorites.set", key, f.w, v)
		if e != nil || string(raw) != string(again) {
			t.Fatal("favorite receipt", e)
		}
		v.ExpectedRevision = &fav.Revision
		v.Active = false
		raw, e = f.call("favorites.set", uuid.NewString(), f.w, v)
		removed := decodeResult[pw.FavoriteSetOutput](t, raw, e).Favorite
		if removed.ID != fav.ID || removed.Active || removed.Revision == fav.Revision {
			t.Fatal(removed)
		}
		_, e = f.call("favorites.set", uuid.NewString(), f.w, v)
		checkCode(t, e, operations.VersionConflict)
	}
	raw, e = f.call("favorites.list", "", f.w, pw.FavoriteListInput{IncludeInactive: true, Limit: 100})
	list := decodeResult[pw.FavoriteListOutput](t, raw, e)
	if len(list.Items) != 4 {
		t.Fatal(list)
	}
	_, e = f.call("favorites.set", uuid.NewString(), f.w, pw.FavoriteSetInput{Target: pw.Target{Kind: "workspace", ID: other.ID}, Active: true})
	checkCode(t, e, operations.NotFound)
	// Public activity contains no private target binding or labels.
	rows, e := f.pool.Query(f.ctx, "SELECT payload FROM outbox_events WHERE event_name='favorites.set'")
	if e != nil {
		t.Fatal(e)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var body []byte
		if e = rows.Scan(&body); e != nil {
			t.Fatal(e)
		}
		var payload map[string]any
		if e = json.Unmarshal(body, &payload); e != nil {
			t.Fatal(e)
		}
		if payload["target"] != nil || payload["active"] != nil || payload["label"] != nil {
			t.Fatal(payload)
		}
		n++
	}
	if n != 8 {
		t.Fatal(n)
	}
}
