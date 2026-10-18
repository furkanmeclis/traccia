package usecase

import (
	plate "github.com/furkanmeclis/traccia/backend/internal/modules/documents/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/google/uuid"
)

func field(name, code string) []operations.FieldError {
	return []operations.FieldError{{Field: name, Code: code}}
}
func idField(name string, id uuid.UUID) []operations.FieldError {
	if id == uuid.Nil {
		return field(name, "invalid")
	}
	return nil
}
func validateBody(id uuid.UUID, name, format string, version int32, content []plate.Node) []operations.FieldError {
	if e := idField(name, id); len(e) > 0 {
		return e
	}
	if e := plate.FormatErrors(format, version); len(e) > 0 {
		return e
	}
	return ValidateContent(content)
}
func validatePage(id uuid.UUID, name string, limit int32, after string) []operations.FieldError {
	if e := idField(name, id); len(e) > 0 {
		return e
	}
	if limit < 1 || limit > 100 {
		return field("limit", "invalid")
	}
	if after != "" {
		if _, e := parseCursor(after); e != nil {
			return field("after", "invalid")
		}
	}
	return nil
}
func validateCAS(id, revision uuid.UUID) []operations.FieldError {
	if e := idField("comment_id", id); len(e) > 0 {
		return e
	}
	return idField("expected_revision", revision)
}
func (s *Service) Operations() ([]*operations.Operation, error) {
	out := []*operations.Operation{}
	add := func(op *operations.Operation, e error) error {
		if e == nil {
			out = append(out, op)
		}
		return e
	}
	meta := func(id, method, path, tool string) operations.Metadata {
		effect, permission := operations.Read, "workspace.read"
		if method != "GET" {
			effect, permission = operations.Write, "workspace.write"
		}
		return operations.Metadata{ID: "comments." + id, Version: 1, Scope: operations.WorkspaceScope, Effect: effect, Permission: permission, Idempotent: effect == operations.Write, HTTPMethod: method, HTTPPath: "/v1/workspaces/{workspaceID}" + path, MCPName: tool}
	}
	if e := add(operations.New(meta("create", "POST", "/issues/{issueID}/comments", "create_comment"), func(in CreateInput) []operations.FieldError {
		return validateBody(in.IssueID, "issue_id", in.Format, in.SchemaVersion, in.Content)
	}, s.create, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("get", "GET", "/comments/{commentID}", "get_comment"), func(in CommentInput) []operations.FieldError { return idField("comment_id", in.CommentID) }, s.get, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("list", "GET", "/issues/{issueID}/comments", "list_comments"), func(in ListInput) []operations.FieldError {
		return validatePage(in.IssueID, "issue_id", in.Limit, in.After)
	}, s.list, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("replace", "POST", "/comments/{commentID}/replace", "replace_comment"), func(in ReplaceInput) []operations.FieldError {
		if e := validateCAS(in.CommentID, in.ExpectedRevision); len(e) > 0 {
			return e
		}
		return validateBody(in.CommentID, "comment_id", in.Format, in.SchemaVersion, in.Content)
	}, s.replace, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("patch", "POST", "/comments/{commentID}/patch", "patch_comment"), func(in PatchInput) []operations.FieldError {
		if e := validateCAS(in.CommentID, in.ExpectedRevision); len(e) > 0 {
			return e
		}
		if e := plate.FormatErrors(in.Format, in.SchemaVersion); len(e) > 0 {
			return e
		}
		if len(in.Patches) < 1 || len(in.Patches) > 100 {
			return field("patches", "invalid")
		}
		return nil
	}, s.patch, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("delete", "POST", "/comments/{commentID}/delete", "delete_comment"), func(in DeleteInput) []operations.FieldError { return validateCAS(in.CommentID, in.ExpectedRevision) }, s.remove, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("revisions", "GET", "/comments/{commentID}/revisions", "list_comment_revisions"), func(in RevisionsInput) []operations.FieldError {
		return validatePage(in.CommentID, "comment_id", in.Limit, in.After)
	}, s.revisions, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("get_revision", "GET", "/comments/{commentID}/revisions/{revisionID}", "get_comment_revision"), func(in RevisionInput) []operations.FieldError {
		if e := idField("comment_id", in.CommentID); len(e) > 0 {
			return e
		}
		return idField("revision_id", in.RevisionID)
	}, s.getRevision, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("restore", "POST", "/comments/{commentID}/restore", "restore_comment"), func(in RestoreInput) []operations.FieldError {
		if e := validateCAS(in.CommentID, in.ExpectedRevision); len(e) > 0 {
			return e
		}
		return idField("revision_id", in.RevisionID)
	}, s.restore, refine)); e != nil {
		return nil, e
	}
	exportRefine := func(in, out *jsonschema.Schema) error {
		if e := refine(in, out); e != nil {
			return e
		}
		in.Properties["format"].Enum = []any{"html", "json", "markdown"}
		out.Properties["format"].Enum = []any{"html", "json", "markdown"}
		return nil
	}
	if e := add(operations.New(meta("export", "GET", "/comments/{commentID}/export", "export_comment"), func(in ExportInput) []operations.FieldError {
		if e := idField("comment_id", in.CommentID); len(e) > 0 {
			return e
		}
		if in.Format != "html" && in.Format != "json" && in.Format != "markdown" {
			return field("format", "invalid")
		}
		return nil
	}, s.export, exportRefine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("backlinks", "GET", "/comment-backlinks", "list_comment_backlinks"), func(in BacklinksInput) []operations.FieldError {
		if in.TargetKind != "user" && in.TargetKind != "issue" && in.TargetKind != "project" {
			return field("target_kind", "invalid")
		}
		return validatePage(in.TargetID, "target_id", in.Limit, in.After)
	}, s.backlinks, refine)); e != nil {
		return nil, e
	}
	timeline := meta("timeline", "GET", "/issues/{issueID}/timeline", "list_issue_timeline")
	timeline.ID = "issue_timeline.list"
	if e := add(operations.New(timeline, func(in TimelineInput) []operations.FieldError {
		return validatePage(in.IssueID, "issue_id", in.Limit, in.After)
	}, s.timeline, refine)); e != nil {
		return nil, e
	}
	return out, nil
}
