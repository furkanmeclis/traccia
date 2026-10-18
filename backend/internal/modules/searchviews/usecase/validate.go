package usecase

import (
	"encoding/json"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/uuid"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

var Resources = []string{"issue", "project", "team"}
var UUIDFields = map[string][]string{"issue": {"id", "team_id", "status_id", "assignee_id", "project_id", "milestone_id", "label_ids"}, "project": {"id", "lead_user_id", "team_ids"}, "team": {"id"}}
var TextFields = map[string][]string{"issue": {"title", "identifier"}, "project": {"name", "summary"}, "team": {"name", "code"}}
var NullFields = map[string][]string{"issue": {"assignee_id", "project_id", "milestone_id", "start_date", "target_date", "effort"}, "project": {"lead_user_id", "start_date", "target_date"}, "team": {}}
var DateFields = map[string][]string{"issue": {"start_date", "target_date"}, "project": {"start_date", "target_date"}, "team": {}}
var SortFields = map[string][]string{"issue": {"id", "identifier", "title", "priority", "effort", "start_date", "target_date", "created_at", "updated_at"}, "project": {"id", "name", "start_date", "target_date", "created_at", "updated_at"}, "team": {"id", "name", "code", "created_at", "updated_at"}}
var ColumnFields = map[string][]string{"issue": {"identifier", "title", "team_id", "status_id", "priority", "assignee_id", "project_id", "milestone_id", "label_ids", "effort", "start_date", "target_date", "created_at", "updated_at", "archived"}, "project": {"name", "summary", "lead_user_id", "team_ids", "start_date", "target_date", "created_at", "updated_at", "archived"}, "team": {"name", "code", "created_at", "updated_at", "archived"}}
var Priorities = []string{"none", "urgent", "high", "normal", "low"}

func field(name, code string) []operations.FieldError {
	return []operations.FieldError{{Field: name, Code: code}}
}
func validText(v string, max int) bool {
	return utf8.ValidString(v) && !strings.ContainsRune(v, 0) && utf8.RuneCountInString(v) <= max
}
func ValidateQuery(resource string, q Query) []operations.FieldError {
	if !slices.Contains(Resources, resource) {
		return field("resource", "invalid")
	}
	if q.Version != 1 {
		return field("query.version", "unsupported")
	}
	if !slices.Contains([]string{"active", "archived", "any"}, q.Archived) {
		return field("query.archived", "invalid")
	}
	if !validText(q.Search, MaxText) {
		return field("query.search", "invalid")
	}
	if q.Sort == nil || len(q.Sort) > MaxSorts {
		return field("query.sort", "invalid")
	}
	seen := map[string]bool{}
	for _, s := range q.Sort {
		if !slices.Contains(SortFields[resource], s.Field) || seen[s.Field] || !slices.Contains([]string{"asc", "desc"}, s.Direction) || !slices.Contains([]string{"first", "last"}, s.Nulls) {
			return field("query.sort", "invalid")
		}
		seen[s.Field] = true
	}
	count := 0
	var walk func(*Filter, int) bool
	walk = func(f *Filter, d int) bool {
		if f == nil {
			return true
		}
		count++
		if d > MaxDepth || count > MaxNodes {
			return false
		}
		switch f.Kind {
		case "all", "any":
			if len(f.Children) == 0 || len(f.Children) > MaxNodes {
				return false
			}
			for i := range f.Children {
				if !walk(&f.Children[i], d+1) {
					return false
				}
			}
		case "not":
			if f.Child == nil || !walk(f.Child, d+1) {
				return false
			}
		case "uuid":
			if !slices.Contains(UUIDFields[resource], f.Field) || !slices.Contains([]string{"in", "not_in"}, f.Op) || len(f.IDs) == 0 || len(f.IDs) > MaxSet {
				return false
			}
			seen := map[uuid.UUID]bool{}
			for _, id := range f.IDs {
				if id == uuid.Nil || seen[id] {
					return false
				}
				seen[id] = true
			}
		case "text":
			if !slices.Contains(TextFields[resource], f.Field) || !slices.Contains([]string{"contains", "eq"}, f.Op) || f.Value == "" || !validText(f.Value, MaxText) {
				return false
			}
		case "enum":
			if resource != "issue" || f.Field != "priority" || !slices.Contains([]string{"in", "not_in"}, f.Op) || len(f.Values) == 0 || len(f.Values) > MaxSet {
				return false
			}
			seen := map[string]bool{}
			for _, v := range f.Values {
				if !slices.Contains(Priorities, v) || seen[v] {
					return false
				}
				seen[v] = true
			}
		case "number":
			if resource != "issue" || f.Field != "effort" || f.Number == nil || !slices.Contains([]string{"eq", "lt", "lte", "gt", "gte"}, f.Op) {
				return false
			}
		case "date":
			if !slices.Contains(DateFields[resource], f.Field) || !slices.Contains([]string{"eq", "lt", "lte", "gt", "gte"}, f.Op) {
				return false
			}
			t, e := time.Parse("2006-01-02", f.Value)
			if e != nil || t.Year() < 1 || t.Format("2006-01-02") != f.Value {
				return false
			}
		case "null":
			if !slices.Contains(NullFields[resource], f.Field) || !slices.Contains([]string{"is_null", "is_not_null"}, f.Op) {
				return false
			}
		default:
			return false
		}
		return true
	}
	if !walk(q.Filter, 1) {
		return field("query.filter", "invalid")
	}
	return nil
}
func ValidateColumns(resource string, c []Column) []operations.FieldError {
	if !slices.Contains(Resources, resource) || len(c) == 0 || len(c) > len(ColumnFields[resource]) {
		return field("columns", "invalid")
	}
	seen := map[string]bool{}
	for _, v := range c {
		if !slices.Contains(ColumnFields[resource], v.Key) || seen[v.Key] || v.Width < 80 || v.Width > 800 {
			return field("columns", "invalid")
		}
		seen[v.Key] = true
	}
	return nil
}
func (v ViewQuery) QueryValue() (Query, bool) {
	switch v.Resource {
	case "issue":
		if v.Issue != nil && v.Project == nil && v.Team == nil {
			return Query(*v.Issue), true
		}
	case "project":
		if v.Project != nil && v.Issue == nil && v.Team == nil {
			return Query(*v.Project), true
		}
	case "team":
		if v.Team != nil && v.Issue == nil && v.Project == nil {
			return Query(*v.Team), true
		}
	}
	return Query{}, false
}
func validateView(name string, d ViewQuery, c []Column) []operations.FieldError {
	if name == "" || !validText(name, 120) {
		return field("name", "invalid")
	}
	q, ok := d.QueryValue()
	if !ok {
		return field("definition", "invalid")
	}
	if e := ValidateQuery(d.Resource, q); len(e) > 0 {
		return e
	}
	return ValidateColumns(d.Resource, c)
}
func validateCapture(resource string, id uuid.UUID, q Query, ttl *int32, limit int32) []operations.FieldError {
	if id == uuid.Nil {
		return field("request_id", "invalid")
	}
	if limit < 1 || limit > MaxPage {
		return field("limit", "invalid")
	}
	if ttl != nil && (*ttl < MinTTL || *ttl > MaxTTL) {
		return field("ttl_seconds", "invalid")
	}
	return ValidateQuery(resource, q)
}
func wireLimits(v any, raw json.RawMessage) []operations.FieldError {
	if len(raw) > 1<<20 {
		return field("input", "bytes_limit")
	}
	type item struct {
		v      any
		depth  int
		filter bool
	}
	pending := []item{{v: v}}
	nodes := 0
	for len(pending) > 0 {
		cur := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		switch x := cur.v.(type) {
		case map[string]any:
			depth := cur.depth
			if cur.filter {
				depth++
				nodes++
				if depth > MaxDepth {
					return field("query.filter", "depth_limit")
				}
				if nodes > MaxNodes {
					return field("query.filter", "nodes_limit")
				}
			}
			keys := make([]string, 0, len(x))
			for key := range x {
				keys = append(keys, key)
			}
			slices.Sort(keys)
			for _, key := range keys {
				child := x[key]
				isFilter := key == "filter" || (cur.filter && (key == "child" || key == "children"))
				nextDepth := 0
				if cur.filter && (key == "child" || key == "children") {
					nextDepth = depth
				}
				pending = append(pending, item{child, nextDepth, isFilter})
			}
		case []any:
			for _, child := range x {
				pending = append(pending, item{child, cur.depth, cur.filter})
			}
		}
	}

	n, e := domainBytes(v)
	if e != nil {
		return field("input", "invalid")
	}
	if n > 128*1024 {
		return field("input", "semantic_bytes_limit")
	}
	return nil
}
func (IssueCreateInput) ValidateWire(v any, r json.RawMessage) []operations.FieldError {
	return wireLimits(v, r)
}
func (ProjectCreateInput) ValidateWire(v any, r json.RawMessage) []operations.FieldError {
	return wireLimits(v, r)
}
func (TeamCreateInput) ValidateWire(v any, r json.RawMessage) []operations.FieldError {
	return wireLimits(v, r)
}
func (ViewCreateInput) ValidateWire(v any, r json.RawMessage) []operations.FieldError {
	return wireLimits(v, r)
}
func (ViewUpdateInput) ValidateWire(v any, r json.RawMessage) []operations.FieldError {
	return wireLimits(v, r)
}
func (IssueCreateInput) InputBudgetBytes() int64   { return 1 << 20 }
func (ProjectCreateInput) InputBudgetBytes() int64 { return 1 << 20 }
func (TeamCreateInput) InputBudgetBytes() int64    { return 1 << 20 }
func (ViewCreateInput) InputBudgetBytes() int64    { return 1 << 20 }
func (ViewUpdateInput) InputBudgetBytes() int64    { return 1 << 20 }
