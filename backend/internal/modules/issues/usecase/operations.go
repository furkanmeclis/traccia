package usecase

import (
	"encoding/json"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/google/uuid"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var identifierPattern = regexp.MustCompile(`^[A-Z][A-Z0-9]{1,9}-[1-9][0-9]{0,18}$`)

func valid[T any](in T) []operations.FieldError {
	raw, _ := json.Marshal(in)
	fields := map[string]json.RawMessage{}
	_ = json.Unmarshal(raw, &fields)
	out := []operations.FieldError{}
	_, page := any(in).(PageInput)
	for key, value := range fields {
		bad := false
		var str string
		_ = json.Unmarshal(value, &str)
		switch key {
		case "issue_id", "team_id", "status_id", "assignee_id", "project_id", "milestone_id", "label_id":
			nullable := key == "assignee_id" || key == "project_id" || key == "milestone_id"
			optional := page && (str == "" || (nullable && str == "unassigned"))
			if !optional && string(value) != "null" {
				id, e := uuid.Parse(str)
				bad = e != nil || id == uuid.Nil
			}
		case "identifier":
			bad = !identifierPattern.MatchString(str)
		case "title":
			n := utf8.RuneCountInString(str)
			bad = n < 1 || n > 255 || str != strings.TrimSpace(str) || strings.IndexFunc(str, func(r rune) bool { return unicode.IsControl(r) || r == '\u2028' || r == '\u2029' }) >= 0
		case "priority":
			bad = str != "none" && str != "urgent" && str != "high" && str != "normal" && str != "low"
			if page && str == "" {
				bad = false
			}
		case "start_date", "target_date":
			if string(value) != "null" {
				_, e := time.Parse("2006-01-02", str)
				bad = e != nil
			}
		case "effort":
			if string(value) != "null" {
				var n int32
				_ = json.Unmarshal(value, &n)
				bad = n < 0 || n > 1000
			}
		case "limit":
			var n int32
			_ = json.Unmarshal(value, &n)
			bad = n < 1 || n > 100
		case "expected_version":
			var n int64
			_ = json.Unmarshal(value, &n)
			bad = n < 1
		case "after":
			if str != "" {
				_, e := parseCursor(str)
				bad = e != nil
			}
		case "q":
			bad = utf8.RuneCountInString(str) > 255 || strings.IndexFunc(str, unicode.IsControl) >= 0
		case "label_ids":
			var ids []string
			_ = json.Unmarshal(value, &ids)
			bad = ids == nil || len(ids) > 1000
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
	if string(fields["milestone_id"]) != "" && string(fields["milestone_id"]) != "null" && string(fields["project_id"]) == "null" {
		out = append(out, operations.FieldError{Field: "milestone_id", Code: "invalid"})
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
			case "id", "workspace_id", "issue_id", "actor_id", "team_id", "status_id":
				*c = jsonschema.Schema{Type: "string", Format: "uuid"}
			case "assignee_id", "project_id", "milestone_id":
				*c = jsonschema.Schema{AnyOf: []*jsonschema.Schema{{Type: "string", Format: "uuid"}, {Type: "null"}}}
			case "start_date", "target_date":
				*c = jsonschema.Schema{AnyOf: []*jsonschema.Schema{{Type: "string", Format: "date"}, {Type: "null"}}}
			case "identifier":
				c.Pattern = identifierPattern.String()
			case "title":
				a, b := 1, 255
				c.MinLength = &a
				c.MaxLength = &b
			case "priority":
				c.Enum = []any{"high", "low", "none", "normal", "urgent"}
			case "effort":
				*c = jsonschema.Schema{AnyOf: []*jsonschema.Schema{{Type: "integer", Minimum: floatPtr(0), Maximum: floatPtr(1000)}, {Type: "null"}}}
			case "version", "number", "expected_version":
				c.Minimum = floatPtr(1)
			case "limit":
				c.Minimum = floatPtr(1)
				c.Maximum = floatPtr(100)
			case "label_ids":
				a, b := 0, 1000
				c.MinItems = &a
				c.MaxItems = &b
				c.UniqueItems = true
				c.Items = &jsonschema.Schema{Type: "string", Format: "uuid"}
			case "created_at", "updated_at", "occurred_at":
				c.Format = "date-time"
			}
			visit(c)
		}
		visit(s.Items)
		for _, a := range s.AnyOf {
			visit(a)
		}
	}
	visit(input)
	visit(output)
	if _, page := input.Properties["q"]; page {
		for _, k := range []string{"team_id", "status_id", "label_id", "assignee_id", "project_id", "milestone_id"} {
			opts := []*jsonschema.Schema{{Type: "string", Enum: []any{""}}, {Type: "string", Format: "uuid"}}
			if k == "assignee_id" || k == "project_id" || k == "milestone_id" {
				opts = append(opts, &jsonschema.Schema{Type: "string", Enum: []any{"unassigned"}})
			}
			input.Properties[k] = &jsonschema.Schema{AnyOf: opts}
		}
		input.Properties["priority"].Enum = []any{"", "high", "low", "none", "normal", "urgent"}
	}
	return nil
}
func floatPtr(n float64) *float64 { return &n }
func (s *Service) Operations() ([]*operations.Operation, error) {
	ops := []*operations.Operation{}
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
		base := "/v1/workspaces/{workspaceID}/issues"
		if id == "issues.resolve" {
			base = "/v1/workspaces/{workspaceID}"
		}
		return operations.Metadata{ID: id, Version: 1, Scope: operations.WorkspaceScope, Effect: effect, Permission: permission, Idempotent: effect == operations.Write, HTTPMethod: method, HTTPPath: base + path, MCPName: tool}
	}
	if e := add(operations.New(meta("issues.create", "POST", "", "create_issue"), valid[CreateInput], s.create, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("issues.list", "GET", "", "list_issue"), valid[PageInput], s.list, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("issues.get", "GET", "/{issueID}", "get_issue"), valid[IssueInput], s.get, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("issues.update", "PATCH", "/{issueID}", "update_issue"), valid[UpdateInput], s.update, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("issues.move", "POST", "/{issueID}/move", "move_issue"), valid[MoveInput], s.move, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("issues.archive", "POST", "/{issueID}/archive", "archive_issue"), valid[VersionInput], s.archive, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("issues.restore", "POST", "/{issueID}/restore", "restore_issue"), valid[VersionInput], s.restore, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("issues.resolve", "GET", "/issue-identifiers/{identifier}", "resolve_issue"), valid[ResolveInput], s.resolve, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("issues.history", "GET", "/{issueID}/history", "history_issue"), valid[HistoryInput], s.history, refine)); e != nil {
		return nil, e
	}
	return ops, nil
}
