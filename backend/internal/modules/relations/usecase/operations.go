package usecase

import (
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/google/uuid"
)

func invalid(field string) []operations.FieldError {
	return []operations.FieldError{{Field: field, Code: "invalid"}}
}
func validVersions(id uuid.UUID, versions []ExpectedVersion) []operations.FieldError {
	if id == uuid.Nil {
		return invalid("issue_id")
	}
	if len(versions) < 1 || len(versions) > 3 {
		return invalid("expected_versions")
	}
	seen := map[uuid.UUID]bool{}
	for _, v := range versions {
		if v.IssueID == uuid.Nil || v.ExpectedVersion < 1 || seen[v.IssueID] {
			return invalid("expected_versions")
		}
		seen[v.IssueID] = true
	}
	if !seen[id] {
		return invalid("expected_versions")
	}
	return nil
}
func validSet(in SetInput) []operations.FieldError {
	if in.TargetIssueID != nil {
		id, e := uuid.Parse(*in.TargetIssueID)
		if e != nil || id == uuid.Nil || id == in.IssueID {
			return invalid("target_issue_id")
		}
	}
	return validVersions(in.IssueID, in.ExpectedVersions)
}
func validLink(in LinkInput) []operations.FieldError {
	if in.TargetIssueID == uuid.Nil || in.TargetIssueID == in.IssueID {
		return invalid("target_issue_id")
	}
	return validVersions(in.IssueID, in.ExpectedVersions)
}
func validPage(in PageInput) []operations.FieldError {
	if in.IssueID == uuid.Nil {
		return invalid("issue_id")
	}
	switch in.Kind {
	case "", "parent", "blocks", "duplicate", "related":
	default:
		return invalid("kind")
	}
	switch in.Direction {
	case "all", "incoming", "outgoing":
	default:
		return invalid("direction")
	}
	return validHistory(HistoryInput{IssueID: in.IssueID, Limit: in.Limit, After: in.After})
}
func validHistory(in HistoryInput) []operations.FieldError {
	if in.IssueID == uuid.Nil {
		return invalid("issue_id")
	}
	if in.Limit < 1 || in.Limit > 100 {
		return invalid("limit")
	}
	if in.After != "" {
		if _, e := parseCursor(in.After); e != nil {
			return invalid("after")
		}
	}
	return nil
}
func refine(input, output *jsonschema.Schema) error {
	var visit func(*jsonschema.Schema)
	visit = func(s *jsonschema.Schema) {
		if s == nil {
			return
		}
		for k, c := range s.Properties {
			switch k {
			case "id", "workspace_id", "issue_id", "actor_id", "team_id":
				*c = jsonschema.Schema{Type: "string", Format: "uuid"}
			case "target_issue_id":
				if c.AnyOf != nil {
					*c = jsonschema.Schema{AnyOf: []*jsonschema.Schema{{Type: "string", Format: "uuid"}, {Type: "null"}}}
				} else {
					*c = jsonschema.Schema{Type: "string", Format: "uuid"}
				}
			case "version", "relation_version", "expected_version":
				c.Minimum = number(1)
			case "limit":
				c.Minimum = number(1)
				c.Maximum = number(100)
			case "created_at", "removed_at", "occurred_at":
				if c.Type == "string" {
					c.Format = "date-time"
				}
			case "expected_versions":
				a, b := 1, 3
				c.MinItems = &a
				c.MaxItems = &b
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
	if p := input.Properties["kind"]; p != nil {
		p.Enum = []any{"", "blocks", "duplicate", "parent", "related"}
	}
	if p := input.Properties["direction"]; p != nil {
		p.Enum = []any{"all", "incoming", "outgoing"}
	}
	return nil
}
func number(n float64) *float64 { return &n }
func (s *Service) Operations() ([]*operations.Operation, error) {
	out := []*operations.Operation{}
	add := func(o *operations.Operation, e error) error {
		if e == nil {
			out = append(out, o)
		}
		return e
	}
	meta := func(id, method, path, tool string) operations.Metadata {
		effect, permission := operations.Write, "workspace.write"
		if method == "GET" {
			effect, permission = operations.Read, "workspace.read"
		}
		return operations.Metadata{ID: "issue_relations." + id, Version: 1, Scope: operations.WorkspaceScope, Effect: effect, Permission: permission, Idempotent: effect == operations.Write, HTTPMethod: method, HTTPPath: "/v1/workspaces/{workspaceID}/issues/{issueID}/relations" + path, MCPName: tool}
	}
	if e := add(operations.New(meta("set_parent", "POST", "/parent", "set_issue_parent"), validSet, s.setParent, refineSet)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("set_duplicate", "POST", "/duplicate", "set_issue_duplicate"), validSet, s.setDuplicate, refineSet)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("link_block", "POST", "/blocks", "link_issue_block"), validLink, s.linkBlock, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("unlink_block", "POST", "/blocks/unlink", "unlink_issue_block"), validLink, s.unlinkBlock, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("link_related", "POST", "/related", "link_related_issue"), validLink, s.linkRelated, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("unlink_related", "POST", "/related/unlink", "unlink_related_issue"), validLink, s.unlinkRelated, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("list", "GET", "", "list_issue_relations"), validPage, s.list, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("history", "GET", "/history", "history_issue_relations"), validHistory, s.history, refine)); e != nil {
		return nil, e
	}
	return out, nil
}

func refineSet(input, output *jsonschema.Schema) error {
	if e := refine(input, output); e != nil {
		return e
	}
	input.Properties["target_issue_id"] = &jsonschema.Schema{AnyOf: []*jsonschema.Schema{{Type: "string", Format: "uuid"}, {Type: "null"}}}
	return nil
}
