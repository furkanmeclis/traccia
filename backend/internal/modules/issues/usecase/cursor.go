package usecase

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/uuid"
	"strings"
)

type pageCursor struct {
	Anchor uuid.UUID `json:"anchor"`
	Scope  string    `json:"scope"`
}

func parseCursor(s string) (pageCursor, error) {
	var out pageCursor
	raw, e := base64.RawURLEncoding.DecodeString(s)
	if e != nil || len(raw) > 2048 {
		return out, fmt.Errorf("invalid cursor")
	}
	e = json.Unmarshal(raw, &out)
	if e != nil || out.Anchor == uuid.Nil || len(out.Scope) != 64 {
		return out, fmt.Errorf("invalid cursor")
	}
	return out, nil
}
func cursorScope(w uuid.UUID, in any) string {
	raw, _ := json.Marshal(in)
	m := map[string]any{}
	_ = json.Unmarshal(raw, &m)
	for k, v := range m {
		if str, ok := v.(string); ok && strings.HasSuffix(k, "_id") {
			if id, e := uuid.Parse(str); e == nil {
				m[k] = id.String()
			}
		}
	}
	delete(m, "after")
	delete(m, "limit")
	m["workspace"] = w.String()
	// Different list operations never share cursor identities, even with equal filters.
	m["type"] = fmt.Sprintf("%T", in)
	raw, _ = json.Marshal(m)
	sum := sha256.Sum256(raw)
	return fmt.Sprintf("%x", sum)
}
func nextCursor(w uuid.UUID, in any, id uuid.UUID) string {
	raw, _ := json.Marshal(pageCursor{Anchor: id, Scope: cursorScope(w, in)})
	return base64.RawURLEncoding.EncodeToString(raw)
}

// CursorAnchor binds a stable keyset to its workspace, aggregate and complete filters.
// It does not require a mutable archived anchor row to remain in the result set.
func CursorAnchor(w uuid.UUID, in any, after string) (uuid.UUID, error) {
	if after == "" {
		return uuid.Nil, nil
	}
	c, e := parseCursor(after)
	if e != nil || c.Scope != cursorScope(w, in) {
		return uuid.Nil, &operations.Error{Code: operations.Validation, Details: []operations.FieldError{{Field: "after", Code: "invalid"}}}
	}
	return c.Anchor, nil
}
