package searchviews_test

import (
	"encoding/json"
	issue "github.com/furkanmeclis/traccia/backend/internal/modules/issues/usecase"
	uc "github.com/furkanmeclis/traccia/backend/internal/modules/searchviews/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/uuid"
	"testing"
)

func query() uc.Query { return uc.Query{Version: 1, Archived: "active", Sort: []uc.Sort{}} }
func TestSearchSessionCaptureSnapshotAndPreferences(t *testing.T) {
	f := newSearchFixture(t)
	team := f.createTeam(t, "QRY")
	i := f.create(t, team.ID)
	p := f.createProject(t, team.ID)
	q := query()
	in := uc.IssueCreateInput{RequestID: uuid.New(), Query: uc.IssueQuery(q), Limit: 1}
	raw, e := f.call("issue_queries.create", "", f.w, in)
	page := decodeResult[uc.IssuePage](t, raw, e)
	if len(page.Items) != 1 || page.Items[0].ID != i.ID || page.Session.TotalCount != "1" {
		t.Fatalf("page %#v", page)
	}
	replay, e := f.call("issue_queries.create", "", f.w, in)
	if e != nil || string(raw) != string(replay) {
		t.Fatalf("replay %v", e)
	}
	in.Query.Search = "changed"
	_, e = f.call("issue_queries.create", "", f.w, in)
	checkCode(t, e, operations.IdempotencyConflict)
	update := issue.UpdateInput{IssueID: i.ID, ExpectedVersion: i.Version, Title: "Changed after capture", Priority: i.Priority, StatusID: i.StatusID, LabelIDs: []string{}}
	if _, e = f.call("issues.update", uuid.NewString(), f.w, update); e != nil {
		t.Fatal(e)
	}
	raw, e = f.call("issue_queries.get", "", f.w, uc.PageInput{SessionID: page.Session.ID, Limit: 100})
	again := decodeResult[uc.IssuePage](t, raw, e)
	if again.Items[0].Title != i.Title {
		t.Fatal("snapshot changed")
	}
	raw, e = f.call("project_queries.create", "", f.w, uc.ProjectCreateInput{RequestID: uuid.New(), Query: uc.ProjectQuery(q), Limit: 100})
	projects := decodeResult[uc.ProjectPage](t, raw, e)
	if len(projects.Items) != 1 || projects.Items[0].ID != p.ID || len(projects.Items[0].TeamIDs) != 1 {
		t.Fatal(projects)
	}
	raw, e = f.call("team_queries.create", "", f.w, uc.TeamCreateInput{RequestID: uuid.New(), Query: uc.TeamQuery(q), Limit: 100})
	teams := decodeResult[uc.TeamPage](t, raw, e)
	if len(teams.Items) != 1 || teams.Items[0].ID != team.ID {
		t.Fatal(teams)
	}
	def := uc.ViewQuery{Resource: "issue", Issue: (*uc.IssueQuery)(&q)}
	columns := []uc.Column{{Key: "title", Width: 240}}
	raw, e = f.call("saved_views.create", uuid.NewString(), f.w, uc.ViewCreateInput{Name: "Private İ view", Definition: def, Columns: columns})
	view := decodeResult[uc.ViewOutput](t, raw, e).View
	raw, e = f.call("saved_views.update", uuid.NewString(), f.w, uc.ViewUpdateInput{ViewID: view.ID, ExpectedRevision: view.Revision, Name: "New", Definition: def, Columns: columns})
	view = decodeResult[uc.ViewOutput](t, raw, e).View
	raw, e = f.call("saved_views.delete", uuid.NewString(), f.w, uc.ViewVersionInput{ViewID: view.ID, ExpectedRevision: view.Revision})
	view = decodeResult[uc.ViewOutput](t, raw, e).View
	if !view.Deleted {
		t.Fatal("not deleted")
	}
	raw, e = f.call("saved_views.restore", uuid.NewString(), f.w, uc.ViewVersionInput{ViewID: view.ID, ExpectedRevision: view.Revision})
	view = decodeResult[uc.ViewOutput](t, raw, e).View
	if view.Deleted {
		t.Fatal("not restored")
	}
	raw, e = f.call("column_preferences.get", "", f.w, uc.ColumnsInput{Resource: "issue"})
	empty := decodeResult[uc.ColumnPreferences](t, raw, e)
	if empty.Revision != nil || len(empty.Columns) != 0 {
		t.Fatal(empty)
	}
	raw, e = f.call("column_preferences.set", uuid.NewString(), f.w, uc.ColumnsSetInput{Resource: "issue", Columns: columns})
	prefs := decodeResult[uc.ColumnPreferences](t, raw, e)
	if prefs.Revision == nil {
		t.Fatal("missing revision")
	}
	_, e = f.call("column_preferences.set", uuid.NewString(), f.w, uc.ColumnsSetInput{Resource: "issue", Columns: columns})
	checkCode(t, e, operations.VersionConflict)
	var count int
	if e = f.pool.QueryRow(f.ctx, "SELECT count(*) FROM private_preference_history").Scan(&count); e != nil || count != 5 {
		t.Fatal(count, e)
	}
	_ = json.Valid(raw)
}
