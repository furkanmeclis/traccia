package documents_test

import (
	"context"
	"github.com/furkanmeclis/traccia/backend/internal/database/db"
	doc "github.com/furkanmeclis/traccia/backend/internal/modules/documents/usecase"
	issue "github.com/furkanmeclis/traccia/backend/internal/modules/issues/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/dbtx"
	"github.com/google/uuid"
	"testing"
	"time"
)

func mentionContent(kind string, id uuid.UUID) []doc.Node {
	return []doc.Node{{Value: &doc.Paragraph{Element: doc.Element{ID: uuid.NewString(), Type: "p", Children: []doc.Node{{Value: &doc.Mention{Element: doc.Element{ID: uuid.NewString(), Type: "mention", Children: []doc.Node{{Value: &doc.Text{Text: ""}}}}, Key: kind + ":" + id.String(), Value: "reference", TargetKind: kind, TargetID: id.String()}}}}}}}
}
func waitLocks(t *testing.T, f documentFixture, n int) {
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
func TestReciprocalDocumentMentionLocks(t *testing.T) {
	f := newDocumentFixture(t)
	team := f.createTeam(t, "LCK")
	a, b := f.create(t, team.ID), f.create(t, team.ID)
	da := createDocument(t, f, a.ID, []doc.Node{paragraph(uuid.NewString(), "a")})
	dbody := createDocument(t, f, b.ID, []doc.Node{paragraph(uuid.NewString(), "b")})
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
		d      doc.Document
		target uuid.UUID
	}{{da, b.ID}, {dbody, a.ID}} {
		go func(d doc.Document, target uuid.UUID) {
			_, e := f.call("documents.replace", uuid.NewString(), f.w, doc.ReplaceInput{DocumentID: d.ID, ExpectedRevision: d.Revision, Format: "plate", SchemaVersion: 1, Content: mentionContent("issue", target)})
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
func TestDocumentMentionRespectsMetadataParentOrder(t *testing.T) {
	f := newDocumentFixture(t)
	team := f.createTeam(t, "MDL")
	p := f.createProject(t, team.ID)
	i := f.create(t, team.ID)
	d := createDocument(t, f, i.ID, []doc.Node{paragraph(uuid.NewString(), "old")})
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
		_, err := f.call("documents.replace", uuid.NewString(), f.w, doc.ReplaceInput{DocumentID: d.ID, ExpectedRevision: d.Revision, Format: "plate", SchemaVersion: 1, Content: mentionContent("project", p.ID)})
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

func TestRichCreateLocksCrossProjectReferencesBeforeTeam(t *testing.T) {
	f := newDocumentFixture(t)
	team := f.createTeam(t, "RCL")
	p, q := f.createProject(t, team.ID), f.createProject(t, team.ID)
	status := f.status(t, team.ID)
	first := p.ID
	if q.ID.String() < first.String() {
		first = q.ID
	}
	tx, e := f.pool.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = tx.Rollback(f.ctx) }()
	if _, e = db.New(tx).LockProject(f.ctx, db.LockProjectParams{Uuid: f.w, Uuid_2: first}); e != nil {
		t.Fatal(e)
	}
	results := make(chan error, 2)
	for _, pair := range [][2]uuid.UUID{{p.ID, q.ID}, {q.ID, p.ID}} {
		go func(own, target uuid.UUID) {
			project := own.String()
			in := doc.RichCreateInput{Issue: issue.CreateInput{TeamID: team.ID, ProjectID: &project, Title: "rich parallel", StatusID: status, Priority: "normal", LabelIDs: []string{}}, Format: "plate", SchemaVersion: 1, Content: mentionContent("project", target)}
			_, err := f.call("issues.create_rich", uuid.NewString(), f.w, in)
			results <- err
		}(pair[0], pair[1])
	}
	waitLocks(t, f, 2)
	if e = tx.Commit(f.ctx); e != nil {
		t.Fatal(e)
	}
	for range 2 {
		select {
		case e = <-results:
			if e != nil {
				t.Fatal(e)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("rich create lock order stalled")
		}
	}
}
