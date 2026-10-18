package usecase

import (
	"context"
	"encoding/json"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/uuid"
)

type Service struct {
	repo       Repository
	policy     Policy
	defaultTTL int32
}

func New(r Repository, p Policy) *Service { return &Service{r, p, DefaultTTL} }

// WithDefaultTTL returns a separately configured service. Limits remain explicit.
func (s *Service) WithDefaultTTL(v int32) *Service {
	if v < MinTTL || v > MaxTTL {
		panic("invalid query session default TTL")
	}
	return &Service{s.repo, s.policy, v}
}
func (s *Service) config(ctx context.Context, scope operations.Scope, _ ConfigInput) (ConfigOutput, error) {
	m, e := s.policy.CanManage(ctx, scope.WorkspaceID())
	return ConfigOutput{Limits: Limits{MaxInputBytes: 128 * 1024, MaxRawInputBytes: 1 << 20, DefaultTTLSeconds: s.defaultTTL, MinTTLSeconds: MinTTL, MaxTTLSeconds: MaxTTL, MaxDepth: MaxDepth, MaxNodes: MaxNodes, MaxSorts: MaxSorts, MaxPage: MaxPage, MaxSet: MaxSet, MaxText: MaxText}, Capabilities: ManageCaps{m}, InputBytePolicy: "json_stringify_utf8_domain", TextMatching: "unicode_default_casefold_literal_no_normalization", TextOrdering: "C_byte_order_uuid_tie"}, e
}
func (s *Service) capture(ctx context.Context, scope operations.Scope, resource string, id uuid.UUID, q Query, ttl *int32, limit int32) (StoredPage, error) {
	v := s.defaultTTL
	if ttl != nil {
		v = *ttl
	}
	return s.repo.Capture(ctx, scope.WorkspaceID(), scope.ActorID(), resource, Capture{RequestID: id, Query: q, TTLSeconds: v, Limit: limit, RequestedTTL: ttl}, func(ctx context.Context) error { return s.policy.CanRead(ctx, scope.WorkspaceID()) })
}
func decodeRows[T any](rows [][]byte) ([]T, error) {
	out := make([]T, 0, len(rows))
	for _, b := range rows {
		var v T
		if e := json.Unmarshal(b, &v); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, nil
}
func issuePage(p StoredPage, e error) (IssuePage, error) {
	if e != nil {
		return IssuePage{}, e
	}
	rows, e := decodeRows[IssueRow](p.Rows)
	return IssuePage{p.Session, IssueQuery(p.Query), rows, p.HasMore, p.NextCursor}, e
}
func projectPage(p StoredPage, e error) (ProjectPage, error) {
	if e != nil {
		return ProjectPage{}, e
	}
	rows, e := decodeRows[ProjectRow](p.Rows)
	return ProjectPage{p.Session, ProjectQuery(p.Query), rows, p.HasMore, p.NextCursor}, e
}
func teamPage(p StoredPage, e error) (TeamPage, error) {
	if e != nil {
		return TeamPage{}, e
	}
	rows, e := decodeRows[TeamRow](p.Rows)
	return TeamPage{p.Session, TeamQuery(p.Query), rows, p.HasMore, p.NextCursor}, e
}
func (s *Service) createIssues(c context.Context, k operations.Scope, v IssueCreateInput) (IssuePage, error) {
	p, e := s.capture(c, k, "issue", v.RequestID, Query(v.Query), v.TTLSeconds, v.Limit)
	return issuePage(p, e)
}
func (s *Service) createProjects(c context.Context, k operations.Scope, v ProjectCreateInput) (ProjectPage, error) {
	p, e := s.capture(c, k, "project", v.RequestID, Query(v.Query), v.TTLSeconds, v.Limit)
	return projectPage(p, e)
}
func (s *Service) createTeams(c context.Context, k operations.Scope, v TeamCreateInput) (TeamPage, error) {
	p, e := s.capture(c, k, "team", v.RequestID, Query(v.Query), v.TTLSeconds, v.Limit)
	return teamPage(p, e)
}
func (s *Service) page(c context.Context, k operations.Scope, r string, v PageInput) (StoredPage, error) {
	p, e := s.repo.Page(c, k.WorkspaceID(), k.ActorID(), r, v)
	if e == nil {
		e = s.policy.CanRead(c, k.WorkspaceID())
	}
	return p, e
}
func (s *Service) getIssues(c context.Context, k operations.Scope, v PageInput) (IssuePage, error) {
	p, e := s.page(c, k, "issue", v)
	return issuePage(p, e)
}
func (s *Service) getProjects(c context.Context, k operations.Scope, v PageInput) (ProjectPage, error) {
	p, e := s.page(c, k, "project", v)
	return projectPage(p, e)
}
func (s *Service) getTeams(c context.Context, k operations.Scope, v PageInput) (TeamPage, error) {
	p, e := s.page(c, k, "team", v)
	return teamPage(p, e)
}
func (s *Service) viewCaps(ctx context.Context, w uuid.UUID, v *SavedView) error {
	m, e := s.policy.CanManage(ctx, w)
	v.Capabilities = ViewCaps{m, m && !v.Deleted, m && !v.Deleted, m && v.Deleted}
	return e
}
func (s *Service) getView(c context.Context, k operations.Scope, v ViewInput) (ViewOutput, error) {
	row, e := s.repo.GetView(c, k.WorkspaceID(), k.ActorID(), v.ViewID)
	if e == nil {
		e = s.viewCaps(c, k.WorkspaceID(), &row)
	}
	return ViewOutput{row}, e
}
func (s *Service) listViews(c context.Context, k operations.Scope, v ViewListInput) (ViewList, error) {
	rows, e := s.repo.ListViews(c, k.WorkspaceID(), k.ActorID(), v)
	out := ViewList{Items: rows}
	if e != nil {
		return out, e
	}
	m, e := s.policy.CanManage(c, k.WorkspaceID())
	out.Capabilities = ManageCaps{m}
	if len(rows) > int(v.Limit) {
		out.HasMore = true
		out.Items = rows[:v.Limit]
		out.NextCursor = ViewCursor(k.WorkspaceID(), k.ActorID(), v, out.Items[len(out.Items)-1].ID)
	}
	for i := range out.Items {
		if e = s.viewCaps(c, k.WorkspaceID(), &out.Items[i]); e != nil {
			break
		}
	}
	return out, e
}
func (s *Service) mutateView(c context.Context, k operations.Scope, v ViewMutation) (ViewOutput, error) {
	m, e := s.policy.CanManage(c, k.WorkspaceID())
	if e != nil {
		return ViewOutput{}, e
	}
	if !m {
		return ViewOutput{}, &operations.Error{Code: operations.Forbidden}
	}
	row, e := s.repo.MutateView(c, k.WorkspaceID(), k.ActorID(), v)
	if e == nil {
		e = s.viewCaps(c, k.WorkspaceID(), &row)
		if e == nil && !row.Capabilities.Manage {
			e = &operations.Error{Code: operations.Forbidden}
		}
	}
	return ViewOutput{row}, e
}
func (s *Service) createView(c context.Context, k operations.Scope, v ViewCreateInput) (ViewOutput, error) {
	return s.mutateView(c, k, ViewMutation{Kind: "create", Name: v.Name, Definition: v.Definition, Columns: v.Columns})
}
func (s *Service) updateView(c context.Context, k operations.Scope, v ViewUpdateInput) (ViewOutput, error) {
	return s.mutateView(c, k, ViewMutation{Kind: "update", ViewID: v.ViewID, ExpectedRevision: v.ExpectedRevision, Name: v.Name, Definition: v.Definition, Columns: v.Columns})
}
func (s *Service) deleteView(c context.Context, k operations.Scope, v ViewVersionInput) (ViewOutput, error) {
	return s.mutateView(c, k, ViewMutation{Kind: "delete", ViewID: v.ViewID, ExpectedRevision: v.ExpectedRevision})
}
func (s *Service) restoreView(c context.Context, k operations.Scope, v ViewVersionInput) (ViewOutput, error) {
	return s.mutateView(c, k, ViewMutation{Kind: "restore", ViewID: v.ViewID, ExpectedRevision: v.ExpectedRevision})
}
func (s *Service) getColumns(c context.Context, k operations.Scope, v ColumnsInput) (ColumnPreferences, error) {
	row, e := s.repo.GetColumns(c, k.WorkspaceID(), k.ActorID(), v.Resource)
	if e == nil {
		row.Capabilities.Manage, e = s.policy.CanManage(c, k.WorkspaceID())
	}
	return row, e
}
func (s *Service) setColumns(c context.Context, k operations.Scope, v ColumnsSetInput) (ColumnPreferences, error) {
	m, e := s.policy.CanManage(c, k.WorkspaceID())
	if e != nil {
		return ColumnPreferences{}, e
	}
	if !m {
		return ColumnPreferences{}, &operations.Error{Code: operations.Forbidden}
	}
	row, e := s.repo.SetColumns(c, k.WorkspaceID(), k.ActorID(), v)
	if e == nil {
		row.Capabilities.Manage, e = s.policy.CanManage(c, k.WorkspaceID())
		if e == nil && !row.Capabilities.Manage {
			e = &operations.Error{Code: operations.Forbidden}
		}
	}
	return row, e
}
