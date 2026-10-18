package documents_test

import (
	"fmt"
	"github.com/furkanmeclis/traccia/backend/internal/database/db"
	doc "github.com/furkanmeclis/traccia/backend/internal/modules/documents/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/furkanmeclis/traccia/backend/internal/platform/outbox"
	"github.com/google/uuid"
	"reflect"
	"testing"
	"time"
)

func paragraph(id, text string) doc.Node {
	return doc.Node{Value: &doc.Paragraph{Element: doc.Element{ID: id, Type: "p", Children: []doc.Node{{Value: &doc.Text{Text: text}}}}}}
}
func createDocument(t *testing.T, f documentFixture, owner uuid.UUID, nodes []doc.Node) doc.Document {
	t.Helper()
	raw, e := f.call("documents.create", uuid.NewString(), f.w, doc.CreateInput{OwnerKind: "issue", OwnerID: owner, Format: "plate", SchemaVersion: 1, Content: nodes})
	return decodeResult[doc.Output](t, raw, e).Document
}
func TestDocumentJournalRoundtrip(t *testing.T) {
	f := newDocumentFixture(t)
	team := f.createTeam(t, "DOC")
	issue := f.create(t, team.ID)
	id, untouched := uuid.NewString(), uuid.NewString()
	initial := []doc.Node{paragraph(id, "  Türkçe 🧭\r\nline\n\r"), paragraph(untouched, "unchanged")}
	d := createDocument(t, f, issue.ID, initial)
	raw, e := f.call("documents.get", "", f.w, doc.DocumentInput{DocumentID: d.ID})
	got := decodeResult[doc.Output](t, raw, e).Document
	if !reflect.DeepEqual(got.Content, initial) {
		t.Fatal("canonical content changed")
	}
	patch := doc.PatchInput{DocumentID: d.ID, ExpectedRevision: d.Revision, Format: "plate", SchemaVersion: 1, Patches: []doc.Patch{{Value: &doc.ReplaceNode{Op: "replace_node", NodeID: id, Node: paragraph(id, "new\r\nvalue")}}}}
	key := uuid.NewString()
	first, e := f.call("documents.patch", key, f.w, patch)
	next := decodeResult[doc.Output](t, first, e).Document
	replay, e := f.call("documents.patch", key, f.w, patch)
	if e != nil || string(first) != string(replay) {
		t.Fatalf("replay changed: %v", e)
	}
	if !reflect.DeepEqual(next.Content[1], initial[1]) {
		t.Fatal("untouched subtree changed")
	}
	_, e = f.call("documents.patch", uuid.NewString(), f.w, patch)
	checkCode(t, e, operations.VersionConflict)
	var checkpoints, deltas, events int
	if e = f.pool.QueryRow(f.ctx, "SELECT count(*) FILTER(WHERE checkpoint IS NOT NULL),count(*) FILTER(WHERE kind='patch' AND checkpoint IS NULL AND jsonb_array_length(patches)=1) FROM document_revisions").Scan(&checkpoints, &deltas); e != nil {
		t.Fatal(e)
	}
	if checkpoints != 1 || deltas != 1 {
		t.Fatalf("journal storage: %d checkpoints %d deltas", checkpoints, deltas)
	}
	if e = f.pool.QueryRow(f.ctx, "SELECT count(*) FROM outbox_events WHERE event_name='documents.patched' AND (payload->'Payload') ? 'patches' AND NOT (payload->'Payload') ? 'content'").Scan(&events); e != nil {
		t.Fatal(e)
	}
	if events != 1 {
		t.Fatal("patch outbox must retain delta only")
	}
	raw, e = f.call("documents.get_revision", "", f.w, doc.RevisionInput{DocumentID: d.ID, RevisionID: d.Revision})
	historical := decodeResult[doc.RevisionOutput](t, raw, e)
	if !reflect.DeepEqual(historical.Document.Content, initial) {
		t.Fatal("historical checkpoint changed")
	}
	raw, e = f.call("documents.restore", uuid.NewString(), f.w, doc.RestoreInput{DocumentID: d.ID, ExpectedRevision: next.Revision, RevisionID: d.Revision})
	restored := decodeResult[doc.Output](t, raw, e).Document
	if restored.Revision == d.Revision || !reflect.DeepEqual(restored.Content, initial) {
		t.Fatal("restore did not create lossless new revision")
	}
	for _, format := range []string{"json", "html", "markdown"} {
		t.Run(format, func(t *testing.T) {
			raw, e := f.call("documents.export", "", f.w, doc.ExportInput{DocumentID: d.ID, Format: format})
			out := decodeResult[doc.ExportOutput](t, raw, e)
			if out.Content == "" || out.Revision != restored.Revision {
				t.Fatal("invalid export")
			}
		})
	}
}
func TestDocumentMigrationAndRichAtomicity(t *testing.T) {
	f := newDocumentFixture(t)
	if len(f.registry.Catalog()) != 129 {
		t.Fatalf("registry %d", len(f.registry.Catalog()))
	}
	team := f.createTeam(t, "RCH")
	base := f.create(t, team.ID)
	in := doc.RichCreateInput{Format: "plate", SchemaVersion: 1, Content: []doc.Node{paragraph(uuid.NewString(), "rich")}}
	in.Issue.TeamID = team.ID
	in.Issue.StatusID = base.StatusID
	in.Issue.Title = "atomic rich"
	in.Issue.Priority = "none"
	in.Issue.LabelIDs = []string{}
	raw, e := f.call("issues.create_rich", uuid.NewString(), f.w, in)
	out := decodeResult[doc.RichOutput](t, raw, e)
	if out.Document.OwnerID != out.Issue.Issue.ID {
		t.Fatal("rich owner mismatch")
	}
	counts := func() string {
		t.Helper()
		var value string
		if err := f.pool.QueryRow(f.ctx, "SELECT jsonb_build_array((SELECT count(*) FROM issues),(SELECT count(*) FROM issue_aliases),(SELECT count(*) FROM issue_history),(SELECT count(*) FROM documents),(SELECT count(*) FROM document_revisions),(SELECT count(*) FROM workspace_history),(SELECT count(*) FROM outbox_events),(SELECT count(*) FROM operation_receipts),(SELECT sum(next_issue_number) FROM teams))::text").Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	before := counts()
	if _, e = f.pool.Exec(f.ctx, "CREATE FUNCTION reject_document_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'forced document revision failure'; END $$; CREATE TRIGGER reject_document_test BEFORE INSERT ON document_revisions FOR EACH ROW EXECUTE FUNCTION reject_document_test()"); e != nil {
		t.Fatal(e)
	}
	_, e = f.call("issues.create_rich", uuid.NewString(), f.w, in)
	if e == nil {
		t.Fatal("forced failure succeeded")
	}
	if counts() != before {
		t.Fatal("rich-create rollback leaked issue, number, receipt, document or events")
	}
}

func TestDocumentRevisionAndBacklinkPagination(t *testing.T) {
	f := newDocumentFixture(t)
	team := f.createTeam(t, "PGN")
	i := f.create(t, team.ID)
	nodeID := uuid.NewString()
	d := createDocument(t, f, i.ID, []doc.Node{paragraph(nodeID, "zero")})
	for n := 0; n < 102; n++ {
		raw, e := f.call("documents.patch", uuid.NewString(), f.w, doc.PatchInput{DocumentID: d.ID, ExpectedRevision: d.Revision, Format: "plate", SchemaVersion: 1, Patches: []doc.Patch{{Value: &doc.ReplaceNode{Op: "replace_node", NodeID: nodeID, Node: paragraph(nodeID, fmt.Sprint(n))}}}})
		d = decodeResult[doc.Output](t, raw, e).Document
	}
	seen := map[uuid.UUID]bool{}
	after := ""
	for {
		raw, e := f.call("documents.revisions", "", f.w, doc.RevisionsInput{DocumentID: d.ID, Limit: 17, After: after})
		page := decodeResult[doc.RevisionList](t, raw, e)
		for _, revision := range page.Items {
			if seen[revision.ID] {
				t.Fatal("duplicate revision")
			}
			seen[revision.ID] = true
		}
		if !page.HasMore {
			break
		}
		after = page.NextCursor
	}
	if len(seen) != 103 {
		t.Fatalf("revisions %d", len(seen))
	}
	mentions := []doc.Node{}
	for range 102 {
		mentions = append(mentions, mentionContent("user", f.actor)...)
	}
	raw, e := f.call("documents.replace", uuid.NewString(), f.w, doc.ReplaceInput{DocumentID: d.ID, ExpectedRevision: d.Revision, Format: "plate", SchemaVersion: 1, Content: mentions})
	d = decodeResult[doc.Output](t, raw, e).Document
	seen = map[uuid.UUID]bool{}
	after = ""
	for {
		raw, e = f.call("documents.backlinks", "", f.w, doc.BacklinksInput{TargetKind: "user", TargetID: f.actor, Limit: 13, After: after})
		page := decodeResult[doc.BacklinkList](t, raw, e)
		for _, ref := range page.Items {
			if seen[ref.ID] {
				t.Fatal("duplicate backlink")
			}
			seen[ref.ID] = true
		}
		if !page.HasMore {
			break
		}
		after = page.NextCursor
	}
	if len(seen) != 102 {
		t.Fatalf("backlinks %d", len(seen))
	}
}

func TestDocumentAtomicRollbackAndDurableOwnerProjection(t *testing.T) {
	f := newDocumentFixture(t)
	team := f.createTeam(t, "ATM")
	i := f.create(t, team.ID)
	d := createDocument(t, f, i.ID, []doc.Node{paragraph(uuid.NewString(), "initial")})
	counts := func() string {
		t.Helper()
		var out string
		if e := f.pool.QueryRow(f.ctx, "SELECT jsonb_build_array((SELECT count(*) FROM documents),(SELECT count(*) FROM document_revisions),(SELECT count(*) FROM document_references),(SELECT count(*) FROM workspace_history),(SELECT count(*) FROM outbox_events),(SELECT count(*) FROM operation_receipts))::text").Scan(&out); e != nil {
			t.Fatal(e)
		}
		return out
	}
	for _, table := range []string{"document_revisions", "document_references", "workspace_history", "outbox_events"} {
		t.Run(table, func(t *testing.T) {
			before := counts()
			if _, e := f.pool.Exec(f.ctx, fmt.Sprintf("CREATE FUNCTION fail_document_write() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'forced document failure'; END $$; CREATE TRIGGER fail_document_write BEFORE INSERT ON %s FOR EACH ROW EXECUTE FUNCTION fail_document_write()", table)); e != nil {
				t.Fatal(e)
			}
			_, e := f.call("documents.replace", uuid.NewString(), f.w, doc.ReplaceInput{DocumentID: d.ID, ExpectedRevision: d.Revision, Format: "plate", SchemaVersion: 1, Content: mentionContent("user", f.actor)})
			if e == nil {
				t.Fatal("forced failure ignored")
			}
			if _, e = f.pool.Exec(f.ctx, fmt.Sprintf("DROP TRIGGER fail_document_write ON %s; DROP FUNCTION fail_document_write()", table)); e != nil {
				t.Fatal(e)
			}
			if counts() != before {
				t.Fatal("atomic rollback leaked durable state")
			}
			raw, e := f.call("documents.get", "", f.w, doc.DocumentInput{DocumentID: d.ID})
			if decodeResult[doc.Output](t, raw, e).Document.Revision != d.Revision {
				t.Fatal("failed write advanced head")
			}
		})
	}
	p := f.createProject(t, team.ID)
	raw, e := f.call("documents.create", uuid.NewString(), f.w, doc.CreateInput{OwnerKind: "project", OwnerID: p.ID, Format: "plate", SchemaVersion: 1, Content: []doc.Node{paragraph(uuid.NewString(), "project")}})
	decodeResult[doc.Output](t, raw, e)
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
	rows, e := f.workspaceRepo.ListActivity(f.ctx, f.w, uuid.Nil, 100)
	if e != nil {
		t.Fatal(e)
	}
	found := 0
	for _, row := range rows {
		if row.EventName != "documents.created" {
			continue
		}
		found++
		if row.ResourceType == "issues" && (row.IssueID != i.ID.String() || row.Identifier != i.Identifier || row.TeamID != team.ID.String()) {
			t.Fatal("missing issue owner context")
		}
		if row.ResourceType == "projects" && row.ProjectID != p.ID.String() {
			t.Fatal("missing project owner context")
		}
	}
	if found != 2 {
		t.Fatalf("projected document events %d", found)
	}
}
