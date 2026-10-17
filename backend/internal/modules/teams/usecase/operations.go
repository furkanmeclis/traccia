package usecase

import (
	"encoding/json"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/google/uuid"
)

var codePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]{1,9}$`)
var colorPattern = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

const MaxWorkflowStatuses = 10000

var categories = []any{"backlog", "unstarted", "started", "completed", "canceled"}

func valid[T any](in T) []operations.FieldError {
	raw, _ := json.Marshal(in)
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	out := []operations.FieldError{}
	for key, value := range fields {
		bad := false
		switch key {
		case "name":
			var v string
			_ = json.Unmarshal(value, &v)
			bad = v != strings.TrimSpace(v) || utf8.RuneCountInString(v) < 1 || utf8.RuneCountInString(v) > 100 || strings.ContainsAny(v, "\x00\r\n")
		case "code":
			var v string
			_ = json.Unmarshal(value, &v)
			bad = !codePattern.MatchString(v)
		case "color":
			var v string
			_ = json.Unmarshal(value, &v)
			bad = !colorPattern.MatchString(v)
		case "category":
			var v string
			_ = json.Unmarshal(value, &v)
			bad = true
			for _, c := range categories {
				if v == c {
					bad = false
				}
			}
		case "team_id", "status_id", "label_id", "after":
			var v string
			_ = json.Unmarshal(value, &v)
			if v != "" {
				id, e := uuid.Parse(v)
				bad = e != nil || id == uuid.Nil
			} else {
				bad = key != "after"
				if _, ok := any(in).(CreateLabelInput); ok {
					bad = false
				}
				if _, ok := any(in).(LabelListInput); ok {
					bad = false
				}
			}
		case "expected_version", "expected_workflow_version":
			var v int64
			_ = json.Unmarshal(value, &v)
			bad = v < 1
		case "limit":
			var v int32
			_ = json.Unmarshal(value, &v)
			bad = v < 1 || v > 100
		case "status_ids":
			var ids []string
			_ = json.Unmarshal(value, &ids)
			bad = len(ids) == 0 || len(ids) > MaxWorkflowStatuses
			seen := map[uuid.UUID]bool{}
			for _, v := range ids {
				id, e := uuid.Parse(v)
				if e != nil || id == uuid.Nil || seen[id] {
					bad = true
				}
				seen[id] = true
			}
		}
		if bad {
			out = append(out, operations.FieldError{Field: key, Code: "invalid"})
		}
	}
	return out
}
func refine(input, output *jsonschema.Schema) error {
	var visit func(*jsonschema.Schema)
	visit = func(s *jsonschema.Schema) {
		if s == nil {
			return
		}
		for k, c := range s.Properties {
			if k == "id" || k == "workspace_id" || k == "status_id" || k == "label_id" {
				*c = jsonschema.Schema{Type: "string", Format: "uuid"}
			}
			if k == "team_id" {
				*c = jsonschema.Schema{Type: "string"}
			}
			if k == "name" {
				a, b := 1, 100
				c.MinLength = &a
				c.MaxLength = &b
			}
			if k == "code" {
				c.Pattern = `^[A-Za-z][A-Za-z0-9]{1,9}$`
			}
			if k == "color" {
				c.Pattern = `^#[0-9A-Fa-f]{6}$`
			}
			if k == "category" {
				c.Enum = categories
			}
			if k == "version" || k == "expected_version" || k == "workflow_version" || k == "expected_workflow_version" {
				v := float64(1)
				c.Minimum = &v
			}
			if k == "limit" {
				a, b := float64(1), float64(100)
				c.Minimum = &a
				c.Maximum = &b
			}
			if k == "status_ids" {
				a, b := 1, MaxWorkflowStatuses
				c.MinItems = &a
				c.MaxItems = &b
				c.UniqueItems = true
			}
			visit(c)
		}
		visit(s.Items)
		for _, c := range s.AnyOf {
			visit(c)
		}
	}
	visit(input)
	visit(output)
	return nil
}
func (s *Service) Operations() ([]*operations.Operation, error) {
	var ops []*operations.Operation
	add := func(op *operations.Operation, e error) error {
		if e != nil {
			return e
		}
		ops = append(ops, op)
		return nil
	}
	if e := add(operations.New(operations.Metadata{ID: "teams.create", Version: 1, Scope: operations.WorkspaceScope, Effect: operations.Write, Permission: "workspace.write", Idempotent: true, HTTPMethod: "POST", HTTPPath: "/v1/workspaces/{workspaceID}/teams", MCPName: "create_team"}, valid[CreateTeamInput], s.createTeam, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(operations.Metadata{ID: "teams.list", Version: 1, Scope: operations.WorkspaceScope, Effect: operations.Read, Permission: "workspace.read", Idempotent: false, HTTPMethod: "GET", HTTPPath: "/v1/workspaces/{workspaceID}/teams", MCPName: "list_teams"}, valid[TeamListInput], s.listTeams, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(operations.Metadata{ID: "teams.get", Version: 1, Scope: operations.WorkspaceScope, Effect: operations.Read, Permission: "workspace.read", Idempotent: false, HTTPMethod: "GET", HTTPPath: "/v1/workspaces/{workspaceID}/teams/{teamID}", MCPName: "get_team"}, valid[TeamInput], s.getTeam, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(operations.Metadata{ID: "teams.update", Version: 1, Scope: operations.WorkspaceScope, Effect: operations.Write, Permission: "workspace.write", Idempotent: true, HTTPMethod: "PATCH", HTTPPath: "/v1/workspaces/{workspaceID}/teams/{teamID}", MCPName: "update_team"}, valid[UpdateTeamInput], s.updateTeam, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(operations.Metadata{ID: "teams.archive", Version: 1, Scope: operations.WorkspaceScope, Effect: operations.Write, Permission: "workspace.write", Idempotent: true, HTTPMethod: "POST", HTTPPath: "/v1/workspaces/{workspaceID}/teams/{teamID}/archive", MCPName: "archive_team"}, valid[ArchiveTeamInput], s.archiveTeam, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(operations.Metadata{ID: "workflow_statuses.list", Version: 1, Scope: operations.WorkspaceScope, Effect: operations.Read, Permission: "workspace.read", Idempotent: false, HTTPMethod: "GET", HTTPPath: "/v1/workspaces/{workspaceID}/teams/{teamID}/workflow-statuses", MCPName: "list_workflow_statuses"}, valid[StatusListInput], s.listStatuses, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(operations.Metadata{ID: "workflow_statuses.create", Version: 1, Scope: operations.WorkspaceScope, Effect: operations.Write, Permission: "workspace.write", Idempotent: true, HTTPMethod: "POST", HTTPPath: "/v1/workspaces/{workspaceID}/teams/{teamID}/workflow-statuses", MCPName: "create_workflow_status"}, valid[CreateStatusInput], s.createStatus, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(operations.Metadata{ID: "workflow_statuses.update", Version: 1, Scope: operations.WorkspaceScope, Effect: operations.Write, Permission: "workspace.write", Idempotent: true, HTTPMethod: "PATCH", HTTPPath: "/v1/workspaces/{workspaceID}/teams/{teamID}/workflow-statuses/{statusID}", MCPName: "update_workflow_status"}, valid[UpdateStatusInput], s.updateStatus, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(operations.Metadata{ID: "workflow_statuses.archive", Version: 1, Scope: operations.WorkspaceScope, Effect: operations.Write, Permission: "workspace.write", Idempotent: true, HTTPMethod: "POST", HTTPPath: "/v1/workspaces/{workspaceID}/teams/{teamID}/workflow-statuses/{statusID}/archive", MCPName: "archive_workflow_status"}, valid[ArchiveStatusInput], s.archiveStatus, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(operations.Metadata{ID: "workflow_statuses.reorder", Version: 1, Scope: operations.WorkspaceScope, Effect: operations.Write, Permission: "workspace.write", Idempotent: true, HTTPMethod: "POST", HTTPPath: "/v1/workspaces/{workspaceID}/teams/{teamID}/workflow-statuses/reorder", MCPName: "reorder_workflow_statuses"}, valid[ReorderInput], s.reorder, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(operations.Metadata{ID: "labels.list", Version: 1, Scope: operations.WorkspaceScope, Effect: operations.Read, Permission: "workspace.read", Idempotent: false, HTTPMethod: "GET", HTTPPath: "/v1/workspaces/{workspaceID}/labels", MCPName: "list_labels"}, valid[LabelListInput], s.listLabels, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(operations.Metadata{ID: "labels.create", Version: 1, Scope: operations.WorkspaceScope, Effect: operations.Write, Permission: "workspace.write", Idempotent: true, HTTPMethod: "POST", HTTPPath: "/v1/workspaces/{workspaceID}/labels", MCPName: "create_label"}, valid[CreateLabelInput], s.createLabel, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(operations.Metadata{ID: "labels.get", Version: 1, Scope: operations.WorkspaceScope, Effect: operations.Read, Permission: "workspace.read", Idempotent: false, HTTPMethod: "GET", HTTPPath: "/v1/workspaces/{workspaceID}/labels/{labelID}", MCPName: "get_label"}, valid[LabelInput], s.getLabel, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(operations.Metadata{ID: "labels.update", Version: 1, Scope: operations.WorkspaceScope, Effect: operations.Write, Permission: "workspace.write", Idempotent: true, HTTPMethod: "PATCH", HTTPPath: "/v1/workspaces/{workspaceID}/labels/{labelID}", MCPName: "update_label"}, valid[UpdateLabelInput], s.updateLabel, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(operations.Metadata{ID: "labels.archive", Version: 1, Scope: operations.WorkspaceScope, Effect: operations.Write, Permission: "workspace.write", Idempotent: true, HTTPMethod: "POST", HTTPPath: "/v1/workspaces/{workspaceID}/labels/{labelID}/archive", MCPName: "archive_label"}, valid[ArchiveLabelInput], s.archiveLabel, refine)); e != nil {
		return nil, e
	}
	return ops, nil
}
