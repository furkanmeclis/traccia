package comments_test

import (
	"context"
	"github.com/furkanmeclis/traccia/backend/internal/database/db"
	comment "github.com/furkanmeclis/traccia/backend/internal/modules/comments/usecase"
	doc "github.com/furkanmeclis/traccia/backend/internal/modules/documents/usecase"
	issue "github.com/furkanmeclis/traccia/backend/internal/modules/issues/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/dbtx"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/uuid"
	"testing"
	"time"
)

func waitLocks(t *testing.T, f commentFixture, n int) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		var count int
		e := f.pool.QueryRow(f.ctx, "SELECT count(*) FROM pg_stat_activity WHERE application_name=current_setting('application_name') AND wait_event_type='Lock'").Scan(&count)
		if e != nil {
			t.Fatal(e)
		}
		if count >= n {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("coordinated operations did not reach lock boundary")
}
func TestReciprocalCommentMentionLocks(t *testing.T) {
	f := newCommentFixture(t)
	team := f.createTeam(t, "LCK")
	a, b := f.create(t, team.ID), f.create(t, team.ID)
	da := createComment(t, f, a.ID, []doc.Node{paragraph(uuid.NewString(), "a")})
	dbody := createComment(t, f, b.ID, []doc.Node{paragraph(uuid.NewString(), "b")})
	first := a.ID
	if b.ID.String() < first.String() {
		first = b.ID
	}
	tx, e := f.pool.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = tx.Rollback(f.ctx) }()
	if _, e = db.New(tx).LockIssue(f.ctx, db.LockIssueParams{Uuid: f.w, Uuid_2: first}); e != nil {
		t.Fatal(e)
	}
	results := make(chan error, 2)
	for _, job := range []struct {
		d      comment.Comment
		target uuid.UUID
	}{{da, b.ID}, {dbody, a.ID}} {
		go func(d comment.Comment, target uuid.UUID) {
			_, e := f.call("comments.replace", uuid.NewString(), f.w, comment.ReplaceInput{CommentID: d.ID, ExpectedRevision: d.Revision, Format: "plate", SchemaVersion: 1, Content: mentionContent("issue", target)})
			results <- e
		}(job.d, job.target)
	}
	waitLocks(t, f, 2)
	if e = tx.Commit(f.ctx); e != nil {
		t.Fatal(e)
	}
	for range 2 {
		select {
		case e := <-results:
			if e != nil {
				t.Fatal(e)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("reciprocal mentions stalled")
		}
	}
}
func TestCommentMentionRespectsMetadataParentOrder(t *testing.T) {
	f := newCommentFixture(t)
	team := f.createTeam(t, "MDL")
	p := f.createProject(t, team.ID)
	i := f.create(t, team.ID)
	d := createComment(t, f, i.ID, []doc.Node{paragraph(uuid.NewString(), "old")})
	tx, e := f.pool.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = tx.Rollback(f.ctx) }()
	if _, e = db.New(tx).LockProject(f.ctx, db.LockProjectParams{Uuid: f.w, Uuid_2: p.ID}); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() {
		_, err := f.call("comments.replace", uuid.NewString(), f.w, comment.ReplaceInput{CommentID: d.ID, ExpectedRevision: d.Revision, Format: "plate", SchemaVersion: 1, Content: mentionContent("project", p.ID)})
		done <- err
	}()
	waitLocks(t, f, 1)
	projectID := p.ID.String()
	ctx, cancel := context.WithTimeout(dbtx.WithTx(f.ctx, tx), 3*time.Second)
	defer cancel()
	_, e = f.repo.Update(ctx, f.w, issue.UpdateInput{IssueID: i.ID, ExpectedVersion: i.Version, Title: i.Title, Priority: i.Priority, StatusID: i.StatusID, LabelIDs: i.LabelIDs, ProjectID: &projectID})
	if e != nil {
		t.Fatalf("metadata update blocked behind reverse FK lock: %v", e)
	}
	if e = tx.Commit(f.ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case e = <-done:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("mention write stalled")
	}
}

func TestCommentRechecksManageAfterBlockedWrite(t *testing.T) {
	f := newCommentFixture(t)
	team := f.createTeam(t, "REV")
	i := f.create(t, team.ID)
	tx, e := f.pool.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = tx.Rollback(f.ctx) }()
	if _, e = db.New(tx).LockTeam(f.ctx, db.LockTeamParams{Uuid: f.w, Uuid_2: team.ID}); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() {
		_, e := f.call("comments.create", uuid.NewString(), f.w, comment.CreateInput{IssueID: i.ID, Format: "plate", SchemaVersion: 1, Content: []doc.Node{paragraph(uuid.NewString(), "retained caller draft")}})
		done <- e
	}()
	waitLocks(t, f, 1)
	if _, e = f.pool.Exec(f.ctx, "UPDATE workspace_members SET role='member' WHERE workspace_id=(SELECT id FROM workspaces WHERE uuid=$1) AND user_id=(SELECT id FROM users WHERE uuid=$2)", f.w, f.actor); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(f.ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case e = <-done:
		checkCode(t, e, operations.Forbidden)
	case <-time.After(5 * time.Second):
		t.Fatal("revocation write stalled")
	}
	var comments, receipts int
	if e = f.pool.QueryRow(f.ctx, "SELECT (SELECT count(*) FROM issue_comments),(SELECT count(*) FROM operation_receipts WHERE operation_id='comments.create')").Scan(&comments, &receipts); e != nil || comments != 0 || receipts != 0 {
		t.Fatalf("revoked write committed %d/%d: %v", comments, receipts, e)
	}
}
