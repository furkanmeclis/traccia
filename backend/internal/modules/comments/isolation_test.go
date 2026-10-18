package comments_test

import (
	"errors"
	comment "github.com/furkanmeclis/traccia/backend/internal/modules/comments/usecase"
	doc "github.com/furkanmeclis/traccia/backend/internal/modules/documents/usecase"
	issue "github.com/furkanmeclis/traccia/backend/internal/modules/issues/usecase"
	ws "github.com/furkanmeclis/traccia/backend/internal/modules/workspaces/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/authctx"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/uuid"
	"testing"
)

func mentionContent(kind string, id uuid.UUID) []doc.Node {
	return []doc.Node{{Value: &doc.Paragraph{Element: doc.Element{ID: uuid.NewString(), Type: "p", Children: []doc.Node{{Value: &doc.Mention{Element: doc.Element{ID: uuid.NewString(), Type: "mention", Children: []doc.Node{{Value: &doc.Text{Text: ""}}}}, Key: kind + ":" + id.String(), Value: "reference", TargetKind: kind, TargetID: id.String()}}}}}}}
}
func actorFixture(t *testing.T, f commentFixture, role string) commentFixture {
	t.Helper()
	id := uuid.New()
	if _, e := f.pool.Exec(f.ctx, "INSERT INTO users(uuid,email,password_hash,name,surname,status) VALUES($1,$2,'unused','Other','Actor','active')", id, id.String()+"@example.test"); e != nil {
		t.Fatal(e)
	}
	if _, e := f.pool.Exec(f.ctx, "INSERT INTO workspace_members(workspace_id,user_id,role) SELECT w.id,u.id,$3 FROM workspaces w,users u WHERE w.uuid=$1 AND u.uuid=$2", f.w, id, role); e != nil {
		t.Fatal(e)
	}
	f.ctx = authctx.WithPrincipal(f.ctx, authctx.Principal{UserID: id})
	f.actor = id
	return f
}
func TestCommentPolicyIsolationAndCAS(t *testing.T) {
	f := newCommentFixture(t)
	team := f.createTeam(t, "POL")
	i := f.create(t, team.ID)
	c := createComment(t, f, i.ID, []doc.Node{paragraph(uuid.NewString(), "old")})
	input := comment.ReplaceInput{CommentID: c.ID, ExpectedRevision: c.Revision, Format: "plate", SchemaVersion: 1, Content: []doc.Node{paragraph(uuid.NewString(), "new")}}
	t.Run("one_concurrent_CAS_winner", func(t *testing.T) {
		results := make(chan error, 2)
		for range 2 {
			go func() { _, e := f.call("comments.replace", uuid.NewString(), f.w, input); results <- e }()
		}
		success, conflict := 0, 0
		for range 2 {
			e := <-results
			if e == nil {
				success++
			} else {
				var op *operations.Error
				if errors.As(e, &op) && op.Code == operations.VersionConflict {
					conflict++
				} else {
					t.Fatal(e)
				}
			}
		}
		if success != 1 || conflict != 1 {
			t.Fatalf("CAS %d/%d", success, conflict)
		}
	})
	raw, e := f.call("comments.get", "", f.w, comment.CommentInput{CommentID: c.ID})
	c = decodeResult[comment.Output](t, raw, e).Comment
	member := actorFixture(t, f, "member")
	owner := actorFixture(t, f, "owner")
	t.Run("member_read_only_owner_moderates_with_original_author", func(t *testing.T) {
		raw, e := member.call("comments.get", "", f.w, comment.CommentInput{CommentID: c.ID})
		got := decodeResult[comment.Output](t, raw, e)
		if got.Comment.Capabilities.Manage {
			t.Fatal("member write caps")
		}
		input.ExpectedRevision = c.Revision
		_, e = member.call("comments.replace", uuid.NewString(), f.w, input)
		checkCode(t, e, operations.Forbidden)
		raw, e = owner.call("comments.replace", uuid.NewString(), f.w, input)
		c = decodeResult[comment.Output](t, raw, e).Comment
		if c.AuthorID != f.actor || c.CreatedChannel != "api" {
			t.Fatal("author overwritten")
		}
		raw, e = member.call("comments.get_revision", "", f.w, comment.RevisionInput{CommentID: c.ID, RevisionID: c.Revision})
		r := decodeResult[comment.RevisionOutput](t, raw, e)
		if r.Revision.ActorID != owner.actor {
			t.Fatal("editing actor lost")
		}
	})
	t.Run("workspace_and_reference_isolation", func(t *testing.T) {
		raw, e := f.call("workspaces.create", uuid.NewString(), uuid.Nil, ws.CreateInput{Name: "Other"})
		other := f
		other.w = decodeResult[ws.WorkspaceOutput](t, raw, e).Workspace.ID
		foreign := other.create(t, other.createTeam(t, "FOR").ID)
		_, e = f.call("comments.get", "", other.w, comment.CommentInput{CommentID: c.ID})
		checkCode(t, e, operations.NotFound)
		for _, target := range []uuid.UUID{foreign.ID, uuid.New()} {
			input.ExpectedRevision = c.Revision
			input.Content = mentionContent("issue", target)
			_, e = f.call("comments.replace", uuid.NewString(), f.w, input)
			checkCode(t, e, operations.Validation)
		}
	})
	t.Run("archived_target_retained_and_owner_delete_escape", func(t *testing.T) {
		target := f.create(t, team.ID)
		_, e := f.call("issues.archive", uuid.NewString(), f.w, issue.VersionInput{IssueID: target.ID, ExpectedVersion: target.Version})
		if e != nil {
			t.Fatal(e)
		}
		input.ExpectedRevision = c.Revision
		input.Content = mentionContent("issue", target.ID)
		raw, e := f.call("comments.replace", uuid.NewString(), f.w, input)
		c = decodeResult[comment.Output](t, raw, e).Comment
		_, e = f.call("issues.archive", uuid.NewString(), f.w, issue.VersionInput{IssueID: i.ID, ExpectedVersion: i.Version})
		if e != nil {
			t.Fatal(e)
		}
		input.ExpectedRevision = c.Revision
		_, e = f.call("comments.replace", uuid.NewString(), f.w, input)
		checkCode(t, e, operations.Conflict)
		raw, e = f.call("comments.get", "", f.w, comment.CommentInput{CommentID: c.ID})
		caps := decodeResult[comment.Output](t, raw, e).Comment.Capabilities
		if !caps.Manage || !caps.Delete || caps.Replace || caps.Restore {
			t.Fatal("archive caps")
		}
		raw, e = f.call("comments.delete", uuid.NewString(), f.w, comment.DeleteInput{CommentID: c.ID, ExpectedRevision: c.Revision})
		c = decodeResult[comment.Output](t, raw, e).Comment
		raw, e = member.call("comments.get", "", f.w, comment.CommentInput{CommentID: c.ID})
		if !decodeResult[comment.Output](t, raw, e).Comment.Deleted {
			t.Fatal("deleted content not workspace readable")
		}
		raw, e = member.call("comments.revisions", "", f.w, comment.RevisionsInput{CommentID: c.ID, Limit: 100})
		if len(decodeResult[comment.RevisionList](t, raw, e).Items) < 4 {
			t.Fatal("history hidden")
		}
	})
}
