package searchviews_test

import (
	uc "github.com/furkanmeclis/traccia/backend/internal/modules/searchviews/usecase"
	"github.com/google/uuid"
	"strings"
	"testing"
)

func TestQueryFilterTruthUnicodeAndPriorityRank(t *testing.T) {
	f := newSearchFixture(t)
	seedIssues(t, f, 11)
	titles := []string{"I", "i", "İ", "i\u0307", "ı", "Straße", "STRASSE", "é", "e\u0301", "%_\\", "line plain"}
	for n, title := range titles {
		if _, e := f.pool.Exec(f.ctx, "UPDATE issues SET title=$2,effort=CASE WHEN number%2=0 THEN 0 ELSE NULL END WHERE workspace_id=(SELECT id FROM workspaces WHERE uuid=$1) AND number=$3", f.w, title, n+1); e != nil {
			t.Fatal(e)
		}
	}
	eq := func(v string) *uc.Filter { return &uc.Filter{Kind: "text", Field: "title", Op: "eq", Value: v} }
	zero := int32(0)
	unknown := uuid.New()
	cases := []struct {
		name   string
		filter *uc.Filter
		search string
		count  string
	}{{"ascii", eq("I"), "", "2"}, {"dotted", eq("İ"), "", "2"}, {"dotless", eq("ı"), "", "1"}, {"sharp_s", eq("STRASSE"), "", "2"}, {"accent", eq("É"), "", "1"}, {"decomposed", eq("e\u0301"), "", "1"}, {"literal_wildcards", nil, "%_\\", "1"}, {"literal_plain", eq("line plain"), "", "1"}, {"nullable_in_false", &uc.Filter{Kind: "uuid", Field: "project_id", Op: "in", IDs: []uuid.UUID{unknown}}, "", "0"}, {"not_in_complement", &uc.Filter{Kind: "uuid", Field: "project_id", Op: "not_in", IDs: []uuid.UUID{unknown}}, "", "11"}, {"not_complement", &uc.Filter{Kind: "not", Child: &uc.Filter{Kind: "uuid", Field: "project_id", Op: "in", IDs: []uuid.UUID{unknown}}}, "", "11"}, {"null", &uc.Filter{Kind: "null", Field: "project_id", Op: "is_null"}, "", "11"}, {"not_null", &uc.Filter{Kind: "null", Field: "project_id", Op: "is_not_null"}, "", "0"}, {"zero_numeric", &uc.Filter{Kind: "number", Field: "effort", Op: "eq", Number: &zero}, "", "5"}, {"not_numeric_includes_null", &uc.Filter{Kind: "not", Child: &uc.Filter{Kind: "number", Field: "effort", Op: "eq", Number: &zero}}, "", "6"}, {"all", &uc.Filter{Kind: "all", Children: []uc.Filter{*eq("I"), {Kind: "enum", Field: "priority", Op: "in", Values: []string{"high"}}}}, "", "1"}, {"any", &uc.Filter{Kind: "any", Children: []uc.Filter{*eq("ı"), *eq("STRASSE")}}, "", "3"}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := query()
			q.Filter = tc.filter
			q.Search = tc.search
			raw, e := f.call("issue_queries.create", "", f.w, uc.IssueCreateInput{RequestID: uuid.New(), Query: uc.IssueQuery(q), Limit: 100})
			p := decodeResult[uc.IssuePage](t, raw, e)
			if p.Session.TotalCount != tc.count || len(p.Items) != atoi(tc.count) {
				t.Fatalf("count %s want%s", p.Session.TotalCount, tc.count)
			}
		})
	}
	q := query()
	q.Sort = []uc.Sort{{Field: "priority", Direction: "asc", Nulls: "last"}, {Field: "title", Direction: "asc", Nulls: "last"}}
	raw, e := f.call("issue_queries.create", "", f.w, uc.IssueCreateInput{RequestID: uuid.New(), Query: uc.IssueQuery(q), Limit: 100})
	p := decodeResult[uc.IssuePage](t, raw, e)
	seenHigh := false
	for _, v := range p.Items {
		if v.Priority == "high" {
			seenHigh = true
		}
		if seenHigh && v.Priority == "low" {
			t.Fatal("priority lexical order")
		}
	}
	upper := strings.ToUpper(p.Items[0].ID.String())
	body := map[string]any{"request_id": uuid.NewString(), "query": map[string]any{"version": 1, "archived": "active", "search": "", "sort": []any{}, "filter": map[string]any{"kind": "uuid", "field": "id", "op": "in", "ids": []string{upper}}}, "ttl_seconds": nil, "limit": 100}
	raw, e = f.call("issue_queries.create", "", f.w, body)
	p = decodeResult[uc.IssuePage](t, raw, e)
	if p.Session.TotalCount != "1" {
		t.Fatal("UUID identity mismatch")
	}
}
func atoi(v string) int {
	n := 0
	for _, c := range v {
		n = n*10 + int(c-'0')
	}
	return n
}
