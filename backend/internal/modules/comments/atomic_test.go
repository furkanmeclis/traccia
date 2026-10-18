package comments_test

import (
	"fmt"
	"github.com/furkanmeclis/traccia/backend/internal/database/db"
	comment "github.com/furkanmeclis/traccia/backend/internal/modules/comments/usecase"
	doc "github.com/furkanmeclis/traccia/backend/internal/modules/documents/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/outbox"
	"github.com/google/uuid"
	"testing"
	"time"
)

func TestCommentAtomicRollbackAndDurableProjection(t *testing.T) {
	f := newCommentFixture(t)
	i := f.create(t, f.createTeam(t, "ATM").ID)
	c := createComment(t, f, i.ID, []doc.Node{paragraph(uuid.NewString(), "old")})
	counts := func() [6]int64 {
		t.Helper()
		var out [6]int64
		e := f.pool.QueryRow(f.ctx, "SELECT (SELECT count(*) FROM issue_comments),(SELECT count(*) FROM comment_revisions),(SELECT count(*) FROM comment_references),(SELECT count(*) FROM workspace_history),(SELECT count(*) FROM outbox_events),(SELECT count(*) FROM operation_receipts)").Scan(&out[0], &out[1], &out[2], &out[3], &out[4], &out[5])
		if e != nil {
			t.Fatal(e)
		}
		return out
	}
	for _, table := range []string{"comment_revisions", "comment_references", "workspace_history", "outbox_events"} {
		t.Run(table, func(t *testing.T) {
			before := counts()
			sql := fmt.Sprintf("CREATE FUNCTION fail_comment_write() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'forced'; END $$; CREATE TRIGGER fail_comment_write BEFORE INSERT ON %s FOR EACH ROW EXECUTE FUNCTION fail_comment_write()", table)
			if _, e := f.pool.Exec(f.ctx, sql); e != nil {
				t.Fatal(e)
			}
			key := uuid.NewString()
			input := comment.ReplaceInput{CommentID: c.ID, ExpectedRevision: c.Revision, Format: "plate", SchemaVersion: 1, Content: mentionContent("user", f.actor)}
			if _, e := f.call("comments.replace", key, f.w, input); e == nil {
				t.Fatal("forced failure ignored")
			}
			if _, e := f.pool.Exec(f.ctx, fmt.Sprintf("DROP TRIGGER fail_comment_write ON %s; DROP FUNCTION fail_comment_write()", table)); e != nil {
				t.Fatal(e)
			}
			if counts() != before {
				t.Fatal("partial durable mutation")
			}
			raw, e := f.call("comments.get", "", f.w, comment.CommentInput{CommentID: c.ID})
			if decodeResult[comment.Output](t, raw, e).Comment.Revision != c.Revision {
				t.Fatal("head advanced")
			}
			raw, e = f.call("comments.replace", key, f.w, input)
			c = decodeResult[comment.Output](t, raw, e).Comment
		})
	}
	store := outbox.NewStore(f.pool, db.New(f.pool))
	consumers, e := f.workspaceRepo.DurableConsumers()
	if e != nil {
		t.Fatal(e)
	}
	claims, e := store.Claim(f.ctx, 100, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	for _, claim := range claims {
		if e = store.Process(f.ctx, claim, consumers); e != nil {
			t.Fatal(e)
		}
	}
	var canonical, projected int64
	if e = f.pool.QueryRow(f.ctx, "SELECT (SELECT count(*) FROM workspace_history),(SELECT count(*) FROM workspace_activity)").Scan(&canonical, &projected); e != nil || canonical != projected {
		t.Fatalf("projection %d/%d: %v", canonical, projected, e)
	}
	rows, e := f.workspaceRepo.ListActivity(f.ctx, f.w, uuid.Nil, 100)
	if e != nil {
		t.Fatal(e)
	}
	found := 0
	for _, row := range rows {
		if row.EventName == "comments.created" || row.EventName == "comments.replaced" {
			found++
			if row.IssueID != i.ID.String() || row.Identifier != i.Identifier {
				t.Fatal("owner context missing")
			}
		}
	}
	if found != 5 {
		t.Fatalf("comment events %d", found)
	}
}
