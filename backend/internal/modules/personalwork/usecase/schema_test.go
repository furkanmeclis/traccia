package usecase

import (
	search "github.com/furkanmeclis/traccia/backend/internal/modules/searchviews/usecase"
	"github.com/google/uuid"
	"testing"
)

func TestPersonalWorkContractConstruction(t *testing.T) {
	ops, e := New(nil, nil).Operations()
	if e != nil {
		t.Fatal(e)
	}
	if len(ops) != 5 {
		t.Fatal(len(ops))
	}
}

func TestPersonalInputBudgetsAndClosedSemanticBranches(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*PortfolioCreateInput)
	}{
		{"empty selection", func(v *PortfolioCreateInput) { v.WorkspaceIDs = []uuid.UUID{} }},
		{"duplicate selection", func(v *PortfolioCreateInput) { v.WorkspaceIDs = append(v.WorkspaceIDs, v.WorkspaceIDs[0]) }},
		{"too many workspaces", func(v *PortfolioCreateInput) {
			for range 32 {
				v.WorkspaceIDs = append(v.WorkspaceIDs, uuid.New())
			}
		}},
		{"nil request", func(v *PortfolioCreateInput) { v.RequestID = uuid.Nil }},
		{"nul search", func(v *PortfolioCreateInput) { v.Definition.Issue.Search = "kept\x00draft" }},
		{"empty group", func(v *PortfolioCreateInput) {
			v.Definition.Issue.Filter = &search.Filter{Kind: "all", Children: []search.Filter{}}
		}},
		{"invalid date", func(v *PortfolioCreateInput) {
			v.Definition.Issue.Filter = &search.Filter{Kind: "date", Field: "start_date", Op: "eq", Value: "0000-01-01"}
		}},
		{"ttl lower bound", func(v *PortfolioCreateInput) { n := int32(59); v.TTLSeconds = &n }},
		{"page upper bound", func(v *PortfolioCreateInput) { v.Limit = 101 }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			q := search.IssueQuery{Version: 1, Archived: "any", Sort: []search.Sort{}}
			v := PortfolioCreateInput{RequestID: uuid.New(), WorkspaceIDs: []uuid.UUID{uuid.New()}, Definition: search.ViewQuery{Resource: "issue", Issue: &q}, Limit: 100}
			tc.mutate(&v)
			if len(validateCreate(v)) == 0 {
				t.Fatal("accepted invalid input")
			}
		})
	}
	t.Run("malformed recursive preflight", func(t *testing.T) {
		var filter any = map[string]any{}
		for range 8 {
			filter = map[string]any{"child": filter}
		}
		v := map[string]any{"definition": map[string]any{"issue": map[string]any{"filter": filter}}}
		got := (PortfolioCreateInput{}).ValidateWire(v, nil)
		if len(got) != 1 || got[0].Code != "depth_limit" {
			t.Fatal(got)
		}
	})
	t.Run("raw guard", func(t *testing.T) {
		got := (PortfolioCreateInput{}).ValidateWire(nil, make([]byte, (1<<20)+1))
		if len(got) != 1 || got[0].Code != "bytes_limit" {
			t.Fatal(got)
		}
	})
}
