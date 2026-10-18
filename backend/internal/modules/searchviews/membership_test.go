package searchviews_test

import (
	project "github.com/furkanmeclis/traccia/backend/internal/modules/projects/usecase"
	uc "github.com/furkanmeclis/traccia/backend/internal/modules/searchviews/usecase"
	team "github.com/furkanmeclis/traccia/backend/internal/modules/teams/usecase"
	"github.com/google/uuid"
	"testing"
)

func TestQuerySetMembershipHasNoFanoutAndLiteralSummary(t *testing.T) {
	f := newSearchFixture(t)
	a, b := f.createTeam(t, "SET"), f.createTeam(t, "TWO")
	i := f.create(t, a.ID)
	p := f.createProject(t, a.ID, b.ID)
	labels := []uuid.UUID{}
	for _, name := range []string{"First", "Second"} {
		raw, e := f.call("labels.create", uuid.NewString(), f.w, team.CreateLabelInput{Name: name, Color: "#123456"})
		labels = append(labels, decodeResult[team.LabelOutput](t, raw, e).Label.ID)
	}
	for _, id := range labels {
		if _, e := f.pool.Exec(f.ctx, `INSERT INTO issue_labels(workspace_id,issue_id,label_id) SELECT i.workspace_id,i.id,l.id FROM issues i JOIN workspaces w ON w.id=i.workspace_id JOIN labels l ON l.workspace_id=i.workspace_id WHERE w.uuid=$1 AND i.uuid=$2 AND l.uuid=$3`, f.w, i.ID, id); e != nil {
			t.Fatal(e)
		}
	}
	q := query()
	q.Filter = &uc.Filter{Kind: "uuid", Field: "label_ids", Op: "in", IDs: labels}
	raw, e := f.call("issue_queries.create", "", f.w, uc.IssueCreateInput{RequestID: uuid.New(), Query: uc.IssueQuery(q), Limit: 100})
	issues := decodeResult[uc.IssuePage](t, raw, e)
	if issues.Session.TotalCount != "1" || len(issues.Items) != 1 || len(issues.Items[0].LabelIDs) != 2 {
		t.Fatal(issues)
	}
	q.Filter = &uc.Filter{Kind: "uuid", Field: "team_ids", Op: "in", IDs: []uuid.UUID{a.ID, b.ID}}
	raw, e = f.call("project_queries.create", "", f.w, uc.ProjectCreateInput{RequestID: uuid.New(), Query: uc.ProjectQuery(q), Limit: 100})
	projects := decodeResult[uc.ProjectPage](t, raw, e)
	if projects.Session.TotalCount != "1" || len(projects.Items) != 1 || len(projects.Items[0].TeamIDs) != 2 {
		t.Fatal(projects)
	}
	q.Filter.Op = "not_in"
	raw, e = f.call("project_queries.create", "", f.w, uc.ProjectCreateInput{RequestID: uuid.New(), Query: uc.ProjectQuery(q), Limit: 100})
	projects = decodeResult[uc.ProjectPage](t, raw, e)
	if projects.Session.TotalCount != "0" {
		t.Fatal(projects)
	}
	summary := "untouched\r\n%_\\ İstanbul e\u0301"
	_, e = f.call("projects.update", uuid.NewString(), f.w, project.UpdateProjectInput{ProjectID: p.ID, Name: p.Name, Summary: summary, ExpectedVersion: p.Version})
	if e != nil {
		t.Fatal(e)
	}
	q = query()
	q.Search = "\r\n%_\\"
	raw, e = f.call("project_queries.create", "", f.w, uc.ProjectCreateInput{RequestID: uuid.New(), Query: uc.ProjectQuery(q), Limit: 100})
	projects = decodeResult[uc.ProjectPage](t, raw, e)
	if projects.Session.TotalCount != "1" || projects.Items[0].Summary != summary || string(projects.Query.Search) != q.Search {
		t.Fatal("literal summary changed", projects)
	}
}
