package usecase

import (
	"context"
	issue "github.com/furkanmeclis/traccia/backend/internal/modules/issues/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/uuid"
)

type IssueCreator interface {
	CreateInTransaction(context.Context, operations.Scope, issue.CreateInput) (issue.Output, error)
}
type Service struct {
	repo   Repository
	policy Policy
	issues IssueCreator
}

func New(repo Repository, policy Policy, issues IssueCreator) *Service {
	return &Service{repo, policy, issues}
}
func (s *Service) caps(ctx context.Context, w uuid.UUID, document *Document) error {
	manage, e := s.policy.CanManage(ctx, w)
	if e != nil {
		return e
	}
	active, e := s.repo.OwnerActive(ctx, w, document.OwnerKind, document.OwnerID)
	document.Capabilities.Manage = manage && active
	return e
}
func (s *Service) resolve(ctx context.Context, scope operations.Scope, in ResolveInput) (ResolveOutput, error) {
	document, active, e := s.repo.Resolve(ctx, scope.WorkspaceID(), in)
	if e != nil {
		return ResolveOutput{}, e
	}
	manage, e := s.policy.CanManage(ctx, scope.WorkspaceID())
	out := ResolveOutput{Document: document, Capabilities: Capabilities{Manage: manage && active}}
	if document != nil {
		document.Capabilities = out.Capabilities
	}
	return out, e
}
func (s *Service) get(ctx context.Context, scope operations.Scope, in DocumentInput) (Output, error) {
	document, _, e := s.repo.Get(ctx, scope.WorkspaceID(), in.DocumentID, uuid.Nil)
	if e == nil {
		e = s.caps(ctx, scope.WorkspaceID(), &document)
	}
	return Output{document}, e
}
func (s *Service) write(ctx context.Context, scope operations.Scope, in Mutation) (Output, error) {
	document, e := s.repo.Mutate(ctx, scope.WorkspaceID(), scope.ActorID(), in)
	if e == nil {
		e = s.caps(ctx, scope.WorkspaceID(), &document)
	}
	return Output{document}, e
}
func (s *Service) create(ctx context.Context, scope operations.Scope, in CreateInput) (Output, error) {
	return s.write(ctx, scope, Mutation{OwnerKind: in.OwnerKind, OwnerID: in.OwnerID, Kind: "create", Content: in.Content})
}
func (s *Service) replace(ctx context.Context, scope operations.Scope, in ReplaceInput) (Output, error) {
	return s.write(ctx, scope, Mutation{DocumentID: in.DocumentID, ExpectedRevision: in.ExpectedRevision, Kind: "replace", Content: in.Content})
}
func (s *Service) patch(ctx context.Context, scope operations.Scope, in PatchInput) (Output, error) {
	return s.write(ctx, scope, Mutation{DocumentID: in.DocumentID, ExpectedRevision: in.ExpectedRevision, Kind: "patch", Patches: in.Patches})
}
func (s *Service) restore(ctx context.Context, scope operations.Scope, in RestoreInput) (Output, error) {
	return s.write(ctx, scope, Mutation{DocumentID: in.DocumentID, ExpectedRevision: in.ExpectedRevision, SourceRevision: in.RevisionID, Kind: "restore"})
}
func (s *Service) revisions(ctx context.Context, scope operations.Scope, in RevisionsInput) (RevisionList, error) {
	rows, e := s.repo.Revisions(ctx, scope.WorkspaceID(), in)
	out := RevisionList{Items: rows}
	if len(rows) > int(in.Limit) {
		out.Items = rows[:in.Limit]
		out.HasMore = true
		out.NextCursor = nextCursor(scope.WorkspaceID(), in, out.Items[len(out.Items)-1].ID)
	}
	return out, e
}
func (s *Service) getRevision(ctx context.Context, scope operations.Scope, in RevisionInput) (RevisionOutput, error) {
	document, revision, e := s.repo.Get(ctx, scope.WorkspaceID(), in.DocumentID, in.RevisionID)
	if e == nil {
		e = s.caps(ctx, scope.WorkspaceID(), &document)
	}
	return RevisionOutput{Document: document, Revision: revision}, e
}
func (s *Service) export(ctx context.Context, scope operations.Scope, in ExportInput) (ExportOutput, error) {
	document, _, e := s.repo.Get(ctx, scope.WorkspaceID(), in.DocumentID, uuid.Nil)
	if e != nil {
		return ExportOutput{}, e
	}
	return RenderExport(document, in.Format)
}
func (s *Service) backlinks(ctx context.Context, scope operations.Scope, in BacklinksInput) (BacklinkList, error) {
	rows, e := s.repo.Backlinks(ctx, scope.WorkspaceID(), in)
	out := BacklinkList{Items: rows}
	if len(rows) > int(in.Limit) {
		out.Items = rows[:in.Limit]
		out.HasMore = true
		out.NextCursor = nextCursor(scope.WorkspaceID(), in, out.Items[len(out.Items)-1].ID)
	}
	return out, e
}
func (s *Service) createRich(ctx context.Context, scope operations.Scope, in RichCreateInput) (RichOutput, error) {
	if e := s.repo.PrepareRich(ctx, scope.WorkspaceID(), in.Issue, in.Content); e != nil {
		return RichOutput{}, e
	}
	created, e := s.issues.CreateInTransaction(ctx, scope, in.Issue)
	if e != nil {
		return RichOutput{}, e
	}
	document, e := s.write(ctx, scope, Mutation{OwnerKind: "issue", OwnerID: created.Issue.ID, Kind: "create", Content: in.Content})
	return RichOutput{Issue: created, Document: document.Document}, e
}
