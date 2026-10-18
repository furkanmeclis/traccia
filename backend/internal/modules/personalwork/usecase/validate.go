package usecase

import (
	"encoding/base64"
	"encoding/json"
	search "github.com/furkanmeclis/traccia/backend/internal/modules/searchviews/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/uuid"
	"slices"
)

var TargetKinds = []string{"issue", "project", "team", "workspace"}

func invalid(field, code string) []operations.FieldError {
	return []operations.FieldError{{Field: field, Code: code}}
}
func validatePage(limit int32, after string) []operations.FieldError {
	if limit < 1 || limit > 100 {
		return invalid("limit", "out_of_range")
	}
	if len(after) > 2048 {
		return invalid("after", "invalid_cursor")
	}
	if after != "" {
		if _, e := base64.RawURLEncoding.DecodeString(after); e != nil {
			return invalid("after", "invalid_cursor")
		}
	}
	return nil
}
func validateSet(v FavoriteSetInput) []operations.FieldError {
	if !slices.Contains(TargetKinds, v.Target.Kind) || v.Target.ID == uuid.Nil {
		return invalid("target", "invalid")
	}
	if v.ExpectedRevision != nil && *v.ExpectedRevision == uuid.Nil {
		return invalid("expected_revision", "invalid")
	}
	if v.ExpectedRevision == nil && !v.Active {
		return invalid("active", "create_requires_active")
	}
	return nil
}
func validateCreate(v PortfolioCreateInput) []operations.FieldError {
	if v.RequestID == uuid.Nil {
		return invalid("request_id", "invalid")
	}
	if len(v.WorkspaceIDs) < 1 || len(v.WorkspaceIDs) > MaxWorkspaces {
		return invalid("workspace_ids", "out_of_range")
	}
	seen := map[uuid.UUID]bool{}
	for _, w := range v.WorkspaceIDs {
		if w == uuid.Nil || seen[w] {
			return invalid("workspace_ids", "invalid")
		}
		seen[w] = true
	}
	q, ok := v.Definition.QueryValue()
	if !ok {
		return invalid("definition", "invalid")
	}
	if v.AssignedToMe && v.Definition.Resource != "issue" {
		return invalid("assigned_to_me", "issue_only")
	}
	if e := search.ValidateQuery(v.Definition.Resource, q); len(e) > 0 {
		return e
	}
	if v.TTLSeconds != nil && (*v.TTLSeconds < search.MinTTL || *v.TTLSeconds > search.MaxTTL) {
		return invalid("ttl_seconds", "out_of_range")
	}
	return validatePage(v.Limit, "")
}
func (PortfolioCreateInput) InputBudgetBytes() int64 { return 1 << 20 }
func (PortfolioCreateInput) ValidateWire(v any, r json.RawMessage) []operations.FieldError {
	return search.ValidateQueryWire(v, r)
}
