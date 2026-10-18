package usecase

import "github.com/furkanmeclis/traccia/backend/internal/platform/operations"

func meta(id, method, path, tool string, write bool) operations.Metadata {
	effect, permission := operations.Read, "workspace.read"
	if write {
		effect, permission = operations.Write, "workspace.write"
	}
	return operations.Metadata{ID: id, Version: 1, Scope: operations.WorkspaceScope, Effect: effect, Permission: permission, Idempotent: write, HTTPMethod: method, HTTPPath: "/v1/workspaces/{workspaceID}" + path, MCPName: tool}
}
func (s *Service) Operations() ([]*operations.Operation, error) {
	out := []*operations.Operation{}
	add := func(op *operations.Operation, e error) error {
		if e == nil {
			out = append(out, op)
		}
		return e
	}
	if e := add(operations.New(meta("personal_work.config", "GET", "/personal-work/config", "get_personal_work_config", false), func(ConfigInput) []operations.FieldError { return nil }, s.config, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("favorites.list", "GET", "/favorites", "list_personal_favorites", false), func(v FavoriteListInput) []operations.FieldError { return validatePage(v.Limit, v.After) }, s.list, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("favorites.set", "POST", "/favorites", "set_personal_favorite", true), validateSet, s.set, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("personal_portfolios.create", "POST", "/personal-work/sessions", "create_personal_portfolio", false), validateCreate, s.create, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("personal_portfolios.get", "GET", "/personal-work/sessions/{sessionID}", "get_personal_portfolio", false), func(v PortfolioGetInput) []operations.FieldError {
		if v.SessionID == [16]byte{} {
			return invalid("session_id", "invalid")
		}
		return validatePage(v.Limit, v.After)
	}, s.get, refine)); e != nil {
		return nil, e
	}
	return out, nil
}
