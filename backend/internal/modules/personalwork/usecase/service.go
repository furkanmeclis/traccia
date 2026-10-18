package usecase

import (
	"context"
	"errors"
	search "github.com/furkanmeclis/traccia/backend/internal/modules/searchviews/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/uuid"
	"slices"
)

type Service struct {
	repo       Repository
	policy     Policy
	defaultTTL int32
}

func New(r Repository, p Policy) *Service { return &Service{r, p, search.DefaultTTL} }
func (s *Service) WithDefaultTTL(v int32) *Service {
	if v < search.MinTTL || v > search.MaxTTL {
		panic("invalid personal portfolio TTL")
	}
	return &Service{s.repo, s.policy, v}
}
func (s *Service) config(ctx context.Context, k operations.Scope, _ ConfigInput) (ConfigOutput, error) {
	m, e := s.policy.CanManage(ctx, k.WorkspaceID())
	return ConfigOutput{Limits: Limits{MaxWorkspaces, 100, s.defaultTTL, search.MinTTL, search.MaxTTL, 1 << 20, 128 << 10}, Capabilities: Capabilities{m}, InputBytePolicy: "json_stringify_utf8_domain", TextMatching: "unicode_default_casefold_literal_no_normalization", TextOrdering: "C_byte_order_uuid_tie"}, e
}
func (s *Service) authorization(k operations.Scope) func(context.Context, []uuid.UUID, bool) error {
	return func(ctx context.Context, workspaces []uuid.UUID, replay bool) error {
		scopes := slices.Clone(workspaces)
		if !slices.Contains(scopes, k.WorkspaceID()) {
			scopes = append(scopes, k.WorkspaceID())
		}
		for _, w := range scopes {
			identity, e := s.policy.Authorize(ctx, operations.AuthorizationRequest{Operation: operations.Metadata{ID: "personal_portfolios.read", Permission: "workspace.read", Effect: operations.Read}, WorkspaceID: w, Channel: k.Channel()})
			if e == nil && identity.ActorID != k.ActorID() {
				e = &operations.Error{Code: operations.Forbidden}
			}
			if e != nil {
				var op *operations.Error
				if errors.As(e, &op) && (op.Code == operations.Forbidden || op.Code == operations.NotFound) {
					if replay {
						return &operations.Error{Code: operations.Forbidden, Details: invalid("session_id", "scope_changed")}
					}
					return &operations.Error{Code: operations.Forbidden}
				}
				return e
			}
		}
		return nil
	}
}
func (s *Service) create(ctx context.Context, k operations.Scope, v PortfolioCreateInput) (PortfolioOutput, error) {
	ids := slices.Clone(v.WorkspaceIDs)
	slices.SortFunc(ids, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
	v.WorkspaceIDs = ids
	ttl := s.defaultTTL
	if v.TTLSeconds != nil {
		ttl = *v.TTLSeconds
	}
	return s.repo.Capture(ctx, k.WorkspaceID(), k.ActorID(), Capture{v, ttl}, s.authorization(k))
}
func (s *Service) get(ctx context.Context, k operations.Scope, v PortfolioGetInput) (PortfolioOutput, error) {
	return s.repo.Page(ctx, k.WorkspaceID(), k.ActorID(), v, s.authorization(k))
}
func (s *Service) list(ctx context.Context, k operations.Scope, v FavoriteListInput) (FavoriteListOutput, error) {
	rows, e := s.repo.ListFavorites(ctx, k.WorkspaceID(), k.ActorID(), v)
	if e != nil {
		return FavoriteListOutput{}, e
	}
	m, e := s.policy.CanManage(ctx, k.WorkspaceID())
	if e != nil {
		return FavoriteListOutput{}, e
	}
	out := FavoriteListOutput{Items: rows, Capabilities: Capabilities{m}}
	if len(rows) > int(v.Limit) {
		out.HasMore = true
		out.Items = rows[:v.Limit]
	}
	if out.HasMore {
		out.NextCursor = EncodeFavoriteCursor(k.WorkspaceID(), k.ActorID(), v.IncludeInactive, out.Items[len(out.Items)-1].ID)
	}
	return out, nil
}
func (s *Service) set(ctx context.Context, k operations.Scope, v FavoriteSetInput) (FavoriteSetOutput, error) {
	m, e := s.policy.CanManage(ctx, k.WorkspaceID())
	if e != nil {
		return FavoriteSetOutput{}, e
	}
	if !m {
		return FavoriteSetOutput{}, &operations.Error{Code: operations.Forbidden}
	}
	f, e := s.repo.SetFavorite(ctx, k.WorkspaceID(), k.ActorID(), v)
	if e != nil {
		return FavoriteSetOutput{}, e
	}
	m, e = s.policy.CanManage(ctx, k.WorkspaceID())
	if e != nil || !m {
		if e == nil {
			e = &operations.Error{Code: operations.Forbidden}
		}
		return FavoriteSetOutput{}, e
	}
	return FavoriteSetOutput{f, Capabilities{m}}, nil
}
