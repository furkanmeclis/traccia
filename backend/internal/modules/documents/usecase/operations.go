package usecase

import (
	issue "github.com/furkanmeclis/traccia/backend/internal/modules/issues/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/google/uuid"
)

func validateOwner(kind string, id uuid.UUID) []operations.FieldError {
	if kind != "issue" && kind != "project" {
		return field("owner_kind", "invalid")
	}
	if id == uuid.Nil {
		return field("owner_id", "invalid")
	}
	return nil
}
func validateDocument(id uuid.UUID) []operations.FieldError {
	if id == uuid.Nil {
		return field("document_id", "invalid")
	}
	return nil
}
func validateRevision(id uuid.UUID) []operations.FieldError {
	if id == uuid.Nil {
		return field("expected_revision", "invalid")
	}
	return nil
}
func validateCreate(in CreateInput) []operations.FieldError {
	if e := validateOwner(in.OwnerKind, in.OwnerID); len(e) > 0 {
		return e
	}
	if e := FormatErrors(in.Format, in.SchemaVersion); len(e) > 0 {
		return e
	}
	return ValidateContent(in.Content)
}
func validateReplace(in ReplaceInput) []operations.FieldError {
	if e := validateDocument(in.DocumentID); len(e) > 0 {
		return e
	}
	if e := validateRevision(in.ExpectedRevision); len(e) > 0 {
		return e
	}
	if e := FormatErrors(in.Format, in.SchemaVersion); len(e) > 0 {
		return e
	}
	return ValidateContent(in.Content)
}
func validatePatch(in PatchInput) []operations.FieldError {
	if e := validateDocument(in.DocumentID); len(e) > 0 {
		return e
	}
	if e := validateRevision(in.ExpectedRevision); len(e) > 0 {
		return e
	}
	if e := FormatErrors(in.Format, in.SchemaVersion); len(e) > 0 {
		return e
	}
	if len(in.Patches) < 1 || len(in.Patches) > 100 {
		return field("patches", "invalid")
	}
	return nil
}
func validatePage(id uuid.UUID, limit int32, after string) []operations.FieldError {
	if e := validateDocument(id); len(e) > 0 {
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
		return operations.Metadata{ID: "documents." + id, Version: 1, Scope: operations.WorkspaceScope, Effect: effect, Permission: permission, Idempotent: effect == operations.Write, HTTPMethod: method, HTTPPath: "/v1/workspaces/{workspaceID}/documents" + path, MCPName: tool}
	}
	if e := add(operations.New(meta("resolve", "GET", "/by-owner", "resolve_document"), func(in ResolveInput) []operations.FieldError { return validateOwner(in.OwnerKind, in.OwnerID) }, s.resolve, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("create", "POST", "", "create_document"), validateCreate, s.create, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("get", "GET", "/{documentID}", "get_document"), func(in DocumentInput) []operations.FieldError { return validateDocument(in.DocumentID) }, s.get, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("replace", "PUT", "/{documentID}", "replace_document"), validateReplace, s.replace, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("patch", "POST", "/{documentID}/patch", "patch_document"), validatePatch, s.patch, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("revisions", "GET", "/{documentID}/revisions", "list_document_revisions"), func(in RevisionsInput) []operations.FieldError {
		return validatePage(in.DocumentID, in.Limit, in.After)
	}, s.revisions, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("get_revision", "GET", "/{documentID}/revisions/{revisionID}", "get_document_revision"), func(in RevisionInput) []operations.FieldError {
		if e := validateDocument(in.DocumentID); len(e) > 0 {
			return e
		}
		if in.RevisionID == uuid.Nil {
			return field("revision_id", "invalid")
		}
		return nil
	}, s.getRevision, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("restore", "POST", "/{documentID}/restore", "restore_document"), func(in RestoreInput) []operations.FieldError {
		if e := validateDocument(in.DocumentID); len(e) > 0 {
			return e
		}
		if e := validateRevision(in.ExpectedRevision); len(e) > 0 {
			return e
		}
		if in.RevisionID == uuid.Nil {
			return field("revision_id", "invalid")
		}
		return nil
	}, s.restore, refine)); e != nil {
		return nil, e
	}
	exportRefine := func(input, output *jsonschema.Schema) error {
		if e := refine(input, output); e != nil {
			return e
		}
		input.Properties["format"].Enum = []any{"html", "json", "markdown"}
		output.Properties["format"].Enum = []any{"html", "json", "markdown"}
		return nil
	}
	if e := add(operations.New(meta("export", "GET", "/{documentID}/export", "export_document"), func(in ExportInput) []operations.FieldError {
		if e := validateDocument(in.DocumentID); len(e) > 0 {
			return e
		}
		if in.Format != "json" && in.Format != "html" && in.Format != "markdown" {
			return field("format", "invalid")
		}
		return nil
	}, s.export, exportRefine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("backlinks", "GET", "/backlinks", "list_document_backlinks"), func(in BacklinksInput) []operations.FieldError {
		if in.TargetKind != "user" && in.TargetKind != "issue" && in.TargetKind != "project" {
			return field("target_kind", "invalid")
		}
		return validatePage(in.TargetID, in.Limit, in.After)
	}, s.backlinks, refine)); e != nil {
		return nil, e
	}
	rich := meta("create_rich", "POST", "", "create_rich_issue")
	rich.ID = "issues.create_rich"
	rich.HTTPPath = "/v1/workspaces/{workspaceID}/issues/rich"
	richRefine := func(input, output *jsonschema.Schema) error {
		if e := refine(input, output); e != nil {
			return e
		}
		if e := issue.RefineCreateSchema(input.Properties["issue"]); e != nil {
			return e
		}
		return issue.RefineCreateSchema(output.Properties["issue"])
	}
	if e := add(operations.New(rich, func(in RichCreateInput) []operations.FieldError {
		if e := issue.ValidateCreateInput(in.Issue); len(e) > 0 {
			for index := range e {
				e[index].Field = "issue." + e[index].Field
			}
			return e
		}
		if e := FormatErrors(in.Format, in.SchemaVersion); len(e) > 0 {
			return e
		}
		return ValidateContent(in.Content)
	}, s.createRich, richRefine)); e != nil {
		return nil, e
	}
	return out, nil
}
