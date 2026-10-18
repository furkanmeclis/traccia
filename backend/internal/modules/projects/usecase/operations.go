package usecase

import (
	"encoding/json"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/google/uuid"
)

const MaxContributingTeams = 1000

func valid[T any](in T) []operations.FieldError {
	raw, _ := json.Marshal(in)
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	out := []operations.FieldError{}
	for key, value := range fields {
		bad := false
		var str string
		_ = json.Unmarshal(value, &str)
		switch key {
		case "project_id", "milestone_id", "resource_id", "after":
			if str != "" {
				id, e := uuid.Parse(str)
				bad = e != nil || id == uuid.Nil
			} else {
				bad = key != "after"
			}
		case "lead_user_id":
			if string(value) != "null" {
				id, e := uuid.Parse(str)
				bad = e != nil || id == uuid.Nil
			}
		case "name", "title":
			bad = str != strings.TrimSpace(str) || utf8.RuneCountInString(str) < 1 || utf8.RuneCountInString(str) > 100 || strings.ContainsAny(str, "\x00\r\n")
		case "summary", "description":
			bad = utf8.RuneCountInString(str) > 2000 || strings.ContainsRune(str, 0)
		case "provider", "reference":
			max := 100
			if key == "reference" {
				max = 200
			}
			bad = utf8.RuneCountInString(str) > max || str != strings.TrimSpace(str) || strings.ContainsAny(str, "\x00\r\n")
		case "url":
			u, e := url.Parse(str)
			bad = e != nil || len(str) > 2048 || str != strings.TrimSpace(str)
			if e == nil {
				bad = bad || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || strings.ContainsAny(str, "\x00\r\n")
			}
		case "kind":
			bad = str != "document" && str != "repository" && str != "link"
		case "start_date", "target_date":
			if string(value) != "null" {
				d, e := time.Parse("2006-01-02", str)
				bad = e != nil || d.Format("2006-01-02") != str
			}
		case "limit":
			var n int32
			_ = json.Unmarshal(value, &n)
			bad = n < 1 || n > 100
		case "expected_version", "expected_project_version":
			var n int64
			_ = json.Unmarshal(value, &n)
			bad = n < 1
		case "team_ids":
			var ids []string
			_ = json.Unmarshal(value, &ids)
			bad = len(ids) < 1 || len(ids) > MaxContributingTeams
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
	var start, target *string
	_ = json.Unmarshal(fields["start_date"], &start)
	_ = json.Unmarshal(fields["target_date"], &target)
	if start != nil && target != nil && *start > *target {
		out = append(out, operations.FieldError{Field: "target_date", Code: "invalid"})
	}
	return out
}
func refine(input, output *jsonschema.Schema) error {
	var visit func(*jsonschema.Schema)
	visit = func(s *jsonschema.Schema) {
		if s == nil {
			return
		}
		for key, c := range s.Properties {
			switch key {
			case "id", "workspace_id", "project_id", "milestone_id", "resource_id":
				*c = jsonschema.Schema{Type: "string", Format: "uuid"}
			case "lead_user_id":
				*c = jsonschema.Schema{AnyOf: []*jsonschema.Schema{{Type: "string", Format: "uuid"}, {Type: "null"}}}
			case "start_date", "target_date":
				*c = jsonschema.Schema{AnyOf: []*jsonschema.Schema{{Type: "string", Format: "date"}, {Type: "null"}}}
			case "name", "title":
				a, b := 1, 100
				c.MinLength = &a
				c.MaxLength = &b
			case "summary", "description":
				b := 2000
				c.MaxLength = &b
			case "provider":
				b := 100
				c.MaxLength = &b
			case "reference":
				b := 200
				c.MaxLength = &b
			case "url":
				a, b := 1, 2048
				c.MinLength = &a
				c.MaxLength = &b
				c.Format = "uri"
			case "kind":
				c.Enum = []any{"document", "repository", "link"}
			case "role":
				c.Enum = []any{"owner", "member"}
			case "version", "project_version", "expected_version", "expected_project_version", "workflow_version":
				a := float64(1)
				c.Minimum = &a
			case "limit":
				a, b := float64(1), float64(100)
				c.Minimum = &a
				c.Maximum = &b
			case "team_ids":
				a, b := 1, MaxContributingTeams
				c.MinItems = &a
				c.MaxItems = &b
				c.UniqueItems = true
				c.Items = &jsonschema.Schema{Type: "string", Format: "uuid"}
			case "created_at", "updated_at":
				c.Format = "date-time"
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
	meta := func(id, method, path, tool string) operations.Metadata {
		effect := operations.Read
		permission := "workspace.read"
		if method != "GET" {
			effect = operations.Write
			permission = "workspace.write"
		}
		return operations.Metadata{ID: id, Version: 1, Scope: operations.WorkspaceScope, Effect: effect, Permission: permission, Idempotent: effect == operations.Write, HTTPMethod: method, HTTPPath: "/v1/workspaces/{workspaceID}" + path, MCPName: tool}
	}
	if e := add(operations.New(meta("projects.create", "POST", "/projects", "create_project"), valid[CreateProjectInput], s.createProject, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("projects.list", "GET", "/projects", "list_projects"), valid[PageInput], s.listProjects, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("projects.get", "GET", "/projects/{projectID}", "get_project"), valid[ProjectInput], s.getProject, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("projects.update", "PATCH", "/projects/{projectID}", "update_project"), valid[UpdateProjectInput], s.updateProject, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("projects.archive", "POST", "/projects/{projectID}/archive", "archive_project"), valid[ArchiveProjectInput], s.archiveProject, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("projects.restore", "POST", "/projects/{projectID}/restore", "restore_project"), valid[ArchiveProjectInput], s.restoreProject, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("project_teams.list", "GET", "/projects/{projectID}/teams", "list_project_teams"), valid[ProjectPageInput], s.listTeams, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("project_teams.set", "PUT", "/projects/{projectID}/teams", "set_project_teams"), valid[SetTeamsInput], s.setTeams, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("workspace_members.list", "GET", "/members", "list_workspace_members"), valid[MemberPageInput], s.listMembers, refineMembers)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("project_milestones.create", "POST", "/projects/{projectID}/milestones", "create_project_milestone"), valid[CreateMilestoneInput], s.createMilestone, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("project_milestones.list", "GET", "/projects/{projectID}/milestones", "list_project_milestones"), valid[ProjectPageInput], s.listMilestones, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("project_milestones.get", "GET", "/projects/{projectID}/milestones/{milestoneID}", "get_project_milestone"), valid[MilestoneInput], s.getMilestone, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("project_milestones.update", "PATCH", "/projects/{projectID}/milestones/{milestoneID}", "update_project_milestone"), valid[UpdateMilestoneInput], s.updateMilestone, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("project_milestones.archive", "POST", "/projects/{projectID}/milestones/{milestoneID}/archive", "archive_project_milestone"), valid[ArchiveMilestoneInput], s.archiveMilestone, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("project_milestones.restore", "POST", "/projects/{projectID}/milestones/{milestoneID}/restore", "restore_project_milestone"), valid[ArchiveMilestoneInput], s.restoreMilestone, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("project_resources.create", "POST", "/projects/{projectID}/resources", "create_project_resource"), valid[CreateResourceInput], s.createResource, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("project_resources.list", "GET", "/projects/{projectID}/resources", "list_project_resources"), valid[ProjectPageInput], s.listResources, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("project_resources.get", "GET", "/projects/{projectID}/resources/{resourceID}", "get_project_resource"), valid[ResourceInput], s.getResource, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("project_resources.update", "PATCH", "/projects/{projectID}/resources/{resourceID}", "update_project_resource"), valid[UpdateResourceInput], s.updateResource, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("project_resources.archive", "POST", "/projects/{projectID}/resources/{resourceID}/archive", "archive_project_resource"), valid[ArchiveResourceInput], s.archiveResource, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("project_resources.restore", "POST", "/projects/{projectID}/resources/{resourceID}/restore", "restore_project_resource"), valid[ArchiveResourceInput], s.restoreResource, refine)); e != nil {
		return nil, e
	}
	return ops, nil
}

// Workspace member display names follow the existing user model, which permits
// empty names. Do not impose project-name constraints on this safe projection.
func refineMembers(input, output *jsonschema.Schema) error {
	if e := refine(input, output); e != nil {
		return e
	}
	if items := output.Properties["items"]; items != nil && items.Items != nil {
		if name := items.Items.Properties["name"]; name != nil {
			name.MinLength = nil
		}
	}
	return nil
}
