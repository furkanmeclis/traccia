package personalwork_test

import (
	pw "github.com/furkanmeclis/traccia/backend/internal/modules/personalwork/usecase"
	search "github.com/furkanmeclis/traccia/backend/internal/modules/searchviews/usecase"
	"github.com/google/uuid"
	"testing"
)

func TestPersonalTypedResourcesAndAssignedActorConjunction(t *testing.T) {
	f := newPersonalFixture(t)
	team := f.createTeam(t, "ASSIGN")
	f.createProject(t, team.ID)
	a := f.create(t, team.ID)
	b := f.create(t, team.ID)
	_ = f.create(t, team.ID)
	other := otherActor(t, f, "member")
	if _, e := f.pool.Exec(f.ctx, "UPDATE issues SET assignee_id=(SELECT id FROM users WHERE uuid=$2),version=version+1 WHERE uuid=$1", a.ID, f.actor); e != nil {
		t.Fatal(e)
	}
	if _, e := f.pool.Exec(f.ctx, "UPDATE issues SET assignee_id=(SELECT id FROM users WHERE uuid=$2),version=version+1 WHERE uuid=$1", b.ID, other.actor); e != nil {
		t.Fatal(e)
	}
	in := pw.PortfolioCreateInput{RequestID: uuid.New(), WorkspaceIDs: []uuid.UUID{f.w}, Definition: query(), AssignedToMe: true, Limit: 100}
	raw, e := f.call("personal_portfolios.create", "", f.w, in)
	page := decodeResult[pw.PortfolioOutput](t, raw, e)
	if len(page.Items) != 1 || page.Items[0].Issue.ID != a.ID {
		t.Fatal(page)
	}
	// Caller ANY/NOT/assignee predicates cannot replace the server actor predicate.
	in.RequestID = uuid.New()
	in.Definition.Issue.Filter = &search.Filter{Kind: "any", Children: []search.Filter{{Kind: "uuid", Field: "assignee_id", Op: "in", IDs: []uuid.UUID{other.actor}}, {Kind: "not", Child: &search.Filter{Kind: "uuid", Field: "id", Op: "in", IDs: []uuid.UUID{a.ID}}}}}
	raw, e = f.call("personal_portfolios.create", "", f.w, in)
	page = decodeResult[pw.PortfolioOutput](t, raw, e)
	if page.Session.TotalCount != "0" {
		t.Fatal(page)
	}
	for _, resource := range []string{"project", "team"} {
		in.RequestID = uuid.New()
		in.AssignedToMe = false
		in.Definition = search.ViewQuery{Resource: resource}
		q := search.Query{Version: 1, Archived: "any", Search: "", Sort: []search.Sort{}}
		if resource == "project" {
			v := search.ProjectQuery(q)
			in.Definition.Project = &v
		} else {
			v := search.TeamQuery(q)
			in.Definition.Team = &v
		}
		raw, e = f.call("personal_portfolios.create", "", f.w, in)
		page = decodeResult[pw.PortfolioOutput](t, raw, e)
		if page.Session.TotalCount != "1" || page.Items[0].Resource != resource || page.Items[0].Issue != nil {
			t.Fatal(page)
		}
		in.RequestID = uuid.New()
		in.AssignedToMe = true
		_, e = f.call("personal_portfolios.create", "", f.w, in)
		if e == nil {
			t.Fatal("nonissue assigned flag")
		}
	}
}
