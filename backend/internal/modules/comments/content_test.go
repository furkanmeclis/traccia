package comments_test

import (
	comment "github.com/furkanmeclis/traccia/backend/internal/modules/comments/usecase"
	doc "github.com/furkanmeclis/traccia/backend/internal/modules/documents/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/uuid"
	"testing"
)

func TestCommentRejectsUnsupportedDatabaseTextWithoutPartialWrite(t *testing.T) {
	f := newCommentFixture(t)
	i := f.create(t, f.createTeam(t, "TXT").ID)
	_, e := f.call("comments.create", uuid.NewString(), f.w, comment.CreateInput{IssueID: i.ID, Format: "plate", SchemaVersion: 1, Content: []doc.Node{paragraph(uuid.NewString(), "preserved\x00text")}})
	checkCode(t, e, operations.Validation)
	var n int
	if e = f.pool.QueryRow(f.ctx, "SELECT count(*) FROM issue_comments").Scan(&n); e != nil || n != 0 {
		t.Fatal("invalid text created state")
	}
	raw, e := f.call("comments.create", uuid.NewString(), f.w, comment.CreateInput{IssueID: i.ID, Format: "plate", SchemaVersion: 1, Content: []doc.Node{paragraph(uuid.NewString(), `literal \u0000 remains text`)}})
	got := decodeResult[comment.Output](t, raw, e).Comment
	if got.Content[0].Value.(*doc.Paragraph).Children[0].Value.(*doc.Text).Text != `literal \u0000 remains text` {
		t.Fatal("literal escape altered")
	}
}
