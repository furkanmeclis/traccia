package usecase

import (
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/google/uuid"
	"slices"
)

func meta(id, method, path, tool string, write bool) operations.Metadata {
	effect, permission := operations.Read, "workspace.read"
	if write {
		effect, permission = operations.Write, "workspace.write"
	}
	return operations.Metadata{ID: id, Version: 1, Scope: operations.WorkspaceScope, Effect: effect, Permission: permission, Idempotent: write, HTTPMethod: method, HTTPPath: "/v1/workspaces/{workspaceID}" + path, MCPName: tool}
}
func idError(k string, id uuid.UUID) []operations.FieldError {
	if id == uuid.Nil {
		return field(k, "invalid")
	}
	return nil
}
func pageError(v PageInput) []operations.FieldError {
	if e := idError("session_id", v.SessionID); len(e) > 0 {
		return e
	}
	if v.Limit < 1 || v.Limit > MaxPage {
		return field("limit", "invalid")
	}
	if v.After != "" {
		if _, e := DecodeCursor(v.After); e != nil {
			return field("after", "invalid")
		}
	}
	return nil
}
func (s *Service) Operations() ([]*operations.Operation, error) {
	out := []*operations.Operation{}
	add := func(op *operations.Operation, e error) error {
		if e == nil {
			out = append(out, op)
		}
		return e
	}
	if e := add(operations.New(meta("search_queries.config", "GET", "/search/config", "get_search_config", false), func(ConfigInput) []operations.FieldError { return nil }, s.config, refine(""))); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("issue_queries.create", "POST", "/search/issues/sessions", "create_issue_query", false), func(v IssueCreateInput) []operations.FieldError {
		return validateCapture("issue", v.RequestID, Query(v.Query), v.TTLSeconds, v.Limit)
	}, s.createIssues, refine("issue"))); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("issue_queries.get", "GET", "/search/issues/sessions/{sessionID}", "get_issue_query", false), pageError, s.getIssues, refine("issue"))); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("project_queries.create", "POST", "/search/projects/sessions", "create_project_query", false), func(v ProjectCreateInput) []operations.FieldError {
		return validateCapture("project", v.RequestID, Query(v.Query), v.TTLSeconds, v.Limit)
	}, s.createProjects, refine("project"))); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("project_queries.get", "GET", "/search/projects/sessions/{sessionID}", "get_project_query", false), pageError, s.getProjects, refine("project"))); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("team_queries.create", "POST", "/search/teams/sessions", "create_team_query", false), func(v TeamCreateInput) []operations.FieldError {
		return validateCapture("team", v.RequestID, Query(v.Query), v.TTLSeconds, v.Limit)
	}, s.createTeams, refine("team"))); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("team_queries.get", "GET", "/search/teams/sessions/{sessionID}", "get_team_query", false), pageError, s.getTeams, refine("team"))); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("saved_views.create", "POST", "/saved-views", "create_saved_view", true), func(v ViewCreateInput) []operations.FieldError { return validateView(v.Name, v.Definition, v.Columns) }, s.createView, refine(""))); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("saved_views.get", "GET", "/saved-views/{viewID}", "get_saved_view", false), func(v ViewInput) []operations.FieldError { return idError("view_id", v.ViewID) }, s.getView, refine(""))); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("saved_views.list", "GET", "/saved-views", "list_saved_views", false), func(v ViewListInput) []operations.FieldError {
		if v.Resource != "any" && !slices.Contains(Resources, v.Resource) {
			return field("resource", "invalid")
		}
		if v.Limit < 1 || v.Limit > MaxPage || len(v.After) > 2048 {
			return field("limit", "invalid")
		}
		return nil
	}, s.listViews, func(i, o *jsonschema.Schema) error {
		if e := refine("")(i, o); e != nil {
			return e
		}
		i.Properties["resource"] = en([]string{"any", "issue", "project", "team"})
		return nil
	})); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("saved_views.update", "POST", "/saved-views/{viewID}/update", "update_saved_view", true), func(v ViewUpdateInput) []operations.FieldError {
		if e := idError("view_id", v.ViewID); len(e) > 0 {
			return e
		}
		if e := idError("expected_revision", v.ExpectedRevision); len(e) > 0 {
			return e
		}
		return validateView(v.Name, v.Definition, v.Columns)
	}, s.updateView, refine(""))); e != nil {
		return nil, e
	}
	validateVersion := func(v ViewVersionInput) []operations.FieldError {
		if e := idError("view_id", v.ViewID); len(e) > 0 {
			return e
		}
		return idError("expected_revision", v.ExpectedRevision)
	}
	if e := add(operations.New(meta("saved_views.delete", "POST", "/saved-views/{viewID}/delete", "delete_saved_view", true), validateVersion, s.deleteView, refine(""))); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("saved_views.restore", "POST", "/saved-views/{viewID}/restore", "restore_saved_view", true), validateVersion, s.restoreView, refine(""))); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("column_preferences.get", "GET", "/column-preferences", "get_column_preferences", false), func(v ColumnsInput) []operations.FieldError {
		if !slices.Contains(Resources, v.Resource) {
			return field("resource", "invalid")
		}
		return nil
	}, s.getColumns, refine(""))); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("column_preferences.set", "POST", "/column-preferences", "set_column_preferences", true), func(v ColumnsSetInput) []operations.FieldError {
		if v.ExpectedRevision != nil && *v.ExpectedRevision == uuid.Nil {
			return field("expected_revision", "invalid")
		}
		return ValidateColumns(v.Resource, v.Columns)
	}, s.setColumns, refine(""))); e != nil {
		return nil, e
	}
	return out, nil
}
