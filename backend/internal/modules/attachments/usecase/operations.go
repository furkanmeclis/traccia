package usecase

import (
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/jsonschema-go/jsonschema"
	"strings"
	"unicode/utf8"
)

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
		return operations.Metadata{ID: id, Version: 1, Scope: operations.WorkspaceScope, Effect: effect, Permission: permission, Idempotent: effect == operations.Write, HTTPMethod: method, HTTPPath: "/v1/workspaces/{workspaceID}" + path, MCPName: tool}
	}
	if e := add(operations.New(meta("attachment_uploads.config", "GET", "/attachment-uploads/config", "get_attachment_upload_config"), func(Empty) []operations.FieldError { return nil }, s.config, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("attachment_uploads.create", "POST", "/attachment-uploads", "create_attachment_upload"), validateCreate, s.create, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("attachment_uploads.get", "GET", "/attachment-uploads/{uploadID}", "get_attachment_upload"), validateUpload, s.getUpload, refine)); e != nil {
		return nil, e
	}
	listRefine := func(in, out *jsonschema.Schema) error {
		if e := refine(in, out); e != nil {
			return e
		}
		in.Properties["state"].Enum = []any{"", "aborted", "any", "bound", "expired", "finalized", "removed", "uploading"}
		return nil
	}
	if e := add(operations.New(meta("attachment_uploads.list", "GET", "/attachment-uploads", "list_attachment_uploads"), func(in UploadListInput) []operations.FieldError {
		if e := validateDraft(in.DraftID); len(e) > 0 {
			return e
		}
		switch in.State {
		case "", "any", "uploading", "finalized", "bound", "removed", "aborted", "expired":
		default:
			return field("state", "invalid")
		}
		return validatePage(in.Limit, in.After)
	}, s.listUploads, listRefine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("attachment_uploads.chunk", "POST", "/attachment-uploads/{uploadID}/chunks", "upload_attachment_chunk"), func(in ChunkInput) []operations.FieldError {
		if e := validateMutation(VersionInput{in.UploadID, in.DraftID, in.ExpectedVersion}); len(e) > 0 {
			return e
		}
		if in.Offset < 0 || in.TotalBytes < 0 || in.TotalBytes > MaxFileBytes {
			return field("offset", "invalid")
		}
		_, e := DecodeChunk(in)
		return e
	}, s.chunk, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("attachment_uploads.finalize", "POST", "/attachment-uploads/{uploadID}/finalize", "finalize_attachment_upload"), validateMutation, s.finalize, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("attachment_uploads.abort", "POST", "/attachment-uploads/{uploadID}/abort", "abort_attachment_upload"), validateMutation, s.abort, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("attachment_uploads.history", "GET", "/attachment-uploads/{uploadID}/history", "list_attachment_upload_history"), func(in UploadHistoryInput) []operations.FieldError {
		if e := validateUpload(UploadInput{in.UploadID, in.DraftID}); len(e) > 0 {
			return e
		}
		return validatePage(in.Limit, in.After)
	}, s.uploadHistory, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("attachments.get", "GET", "/attachments/{attachmentID}", "get_attachment"), validateFile, s.get, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("attachments.bind", "POST", "/attachments/{attachmentID}/bind", "bind_issue_attachment"), func(in BindInput) []operations.FieldError {
		if e := ID(in.AttachmentID, "attachment_id"); len(e) > 0 {
			return e
		}
		if e := ID(in.DraftID, "draft_id"); len(e) > 0 {
			return e
		}
		if e := ID(in.IssueID, "issue_id"); len(e) > 0 {
			return e
		}
		return validateVersion(in.ExpectedVersion)
	}, s.bind, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("attachments.remove", "POST", "/attachments/{attachmentID}/remove", "remove_issue_attachment"), func(in RemoveInput) []operations.FieldError {
		if e := ID(in.AttachmentID, "attachment_id"); len(e) > 0 {
			return e
		}
		return validateVersion(in.ExpectedVersion)
	}, s.remove, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("attachments.list", "GET", "/issues/{issueID}/attachments", "list_issue_attachments"), func(in ListInput) []operations.FieldError {
		if strings.ContainsRune(in.Search, 0) || !utf8.ValidString(in.Search) || utf8.RuneCountInString(in.Search) > 255 {
			return field("search", "invalid")
		}
		if e := ID(in.IssueID, "issue_id"); len(e) > 0 {
			return e
		}
		return validatePage(in.Limit, in.After)
	}, s.list, refine)); e != nil {
		return nil, e
	}
	downloadRefine := func(in, out *jsonschema.Schema) error {
		if e := refine(in, out); e != nil {
			return e
		}
		in.Properties["limit"].Maximum = number(float64(MaxChunkBytes))
		out.Properties["content_type"] = shaped(map[string]any{"type": "string", "const": "application/octet-stream"})
		return nil
	}
	if e := add(operations.New(meta("attachments.download_chunk", "GET", "/attachments/{attachmentID}/download-chunk", "download_attachment_chunk"), func(in DownloadInput) []operations.FieldError {
		if e := validateFile(AttachmentInput{in.AttachmentID, in.DraftID}); len(e) > 0 {
			return e
		}
		if e := ID(in.FileRevision, "file_revision"); len(e) > 0 {
			return e
		}
		if in.Offset < 0 || in.Offset > MaxFileBytes {
			return field("offset", "invalid")
		}
		if in.Limit < 1 || in.Limit > s.options.ChunkBytes {
			return field("limit", "invalid")
		}
		return nil
	}, s.download, downloadRefine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("attachments.history", "GET", "/attachments/{attachmentID}/history", "list_attachment_history"), func(in HistoryInput) []operations.FieldError {
		if e := validateFile(AttachmentInput{in.AttachmentID, in.DraftID}); len(e) > 0 {
			return e
		}
		return validatePage(in.Limit, in.After)
	}, s.history, refine)); e != nil {
		return nil, e
	}
	return out, nil
}
