package comments_test

import (
	"crypto/sha256"
	"fmt"
	"github.com/furkanmeclis/traccia/backend/internal/database/db"
	attachment "github.com/furkanmeclis/traccia/backend/internal/modules/attachments/usecase"
	comment "github.com/furkanmeclis/traccia/backend/internal/modules/comments/usecase"
	doc "github.com/furkanmeclis/traccia/backend/internal/modules/documents/usecase"
	relation "github.com/furkanmeclis/traccia/backend/internal/modules/relations/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/dbtx"
	"github.com/furkanmeclis/traccia/backend/internal/platform/events"
	"github.com/furkanmeclis/traccia/backend/internal/platform/outbox"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"strings"
	"testing"
)

func TestTimelineAllTypedVariantsAndPrivateAttachmentBoundary(t *testing.T) {
	f := newCommentFixture(t)
	i := f.create(t, f.createTeam(t, "TML").ID)
	target := f.create(t, i.TeamID)
	createComment(t, f, i.ID, []doc.Node{paragraph(uuid.NewString(), "comment")})
	raw, e := f.call("documents.create", uuid.NewString(), f.w, doc.CreateInput{OwnerKind: "issue", OwnerID: i.ID, Format: "plate", SchemaVersion: 1, Content: []doc.Node{paragraph(uuid.NewString(), "document")}})
	document := decodeResult[doc.Output](t, raw, e).Document
	targetID := target.ID.String()
	if _, e = f.call("issue_relations.set_parent", uuid.NewString(), f.w, relation.SetInput{IssueID: i.ID, TargetIssueID: &targetID, ExpectedVersions: []relation.ExpectedVersion{{IssueID: i.ID, ExpectedVersion: 1}, {IssueID: target.ID, ExpectedVersion: 1}}}); e != nil {
		t.Fatal(e)
	}
	draft := uuid.New()
	hash := fmt.Sprintf("%x", sha256.Sum256(nil))
	raw, e = f.call("attachment_uploads.create", uuid.NewString(), f.w, attachment.CreateInput{DraftID: draft, Filename: "private-before-bind.txt", ContentType: "text/plain", SHA256: hash})
	upload := decodeResult[attachment.UploadOutput](t, raw, e).Upload
	raw, e = f.call("attachment_uploads.finalize", uuid.NewString(), f.w, attachment.VersionInput{UploadID: upload.ID, DraftID: draft, ExpectedVersion: upload.Version})
	file := decodeResult[attachment.FinalizeOutput](t, raw, e).Attachment
	raw, e = f.call("issue_timeline.list", "", f.w, comment.TimelineInput{IssueID: i.ID, Limit: 100})
	before := decodeResult[comment.TimelineList](t, raw, e)
	if strings.Contains(string(raw), "private-before-bind") || strings.Contains(string(raw), draft.String()) || strings.Contains(string(raw), file.ID.String()) {
		t.Fatal("prebind private context leaked")
	}
	raw, e = f.call("attachments.bind", uuid.NewString(), f.w, attachment.BindInput{AttachmentID: file.ID, DraftID: draft, IssueID: i.ID, ExpectedVersion: file.Version})
	file = decodeResult[attachment.AttachmentOutput](t, raw, e).Attachment
	tx, e := f.pool.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = tx.Rollback(f.ctx) }()
	ctx := dbtx.WithTx(f.ctx, tx)
	ev := events.New("issues.legacy_imported")
	ev.WorkspaceID = f.w
	ev.ActorID = f.actor
	ev.EntityType = "issues"
	ev.EntityUUID = &i.ID
	ev.Payload = map[string]any{"private_arbitrary": "must never be exposed"}
	if e = outbox.NewStore(f.pool, db.New(f.pool)).Enqueue(ctx, tx, ev); e != nil {
		t.Fatal(e)
	}
	q := db.New(tx)
	stored, e := q.GetOutboxEventByUUID(ctx, ev.EventID)
	if e != nil {
		t.Fatal(e)
	}
	if e = q.InsertIssueWorkspaceHistory(ctx, db.InsertIssueWorkspaceHistoryParams{EventID: stored.ID, Uuid: ev.EventID, WorkspaceUuid: f.w, ActorUuid: f.actor, EventName: ev.Name, Name: "Legacy import", Version: 1, OccurredAt: pgtype.Timestamptz{Time: ev.OccurredAt, Valid: true}, ResourceUuid: pgtype.UUID{Bytes: i.ID, Valid: true}, TeamUuid: pgtype.UUID{Bytes: i.TeamID, Valid: true}, IssueIdentifier: i.Identifier}); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(f.ctx); e != nil {
		t.Fatal(e)
	}
	raw, e = f.call("issue_timeline.list", "", f.w, comment.TimelineInput{IssueID: i.ID, Limit: 100})
	all := decodeResult[comment.TimelineList](t, raw, e)
	counts := map[string]int{}
	for _, v := range all.Items {
		counts[v.Detail.Kind]++
		if v.Detail.Document != nil && v.Detail.Document.Revision != document.Revision {
			t.Fatal("document revision changed")
		}
		if v.Detail.Attachment != nil && (v.Detail.Attachment.AttachmentID != file.ID || v.Detail.Attachment.FileRevision != file.FileRevision) {
			t.Fatal("file byte identity changed")
		}
		if v.Detail.Legacy != nil && v.Channel != "unknown" {
			t.Fatal("legacy channel guessed")
		}
	}
	for _, kind := range []string{"issue", "document", "comment", "relation", "attachment", "legacy"} {
		if counts[kind] != 1 {
			t.Fatalf("%s count %d", kind, counts[kind])
		}
	}
	if len(all.Items) != len(before.Items)+2 || strings.Contains(string(raw), draft.String()) || strings.Contains(string(raw), upload.ID.String()) || strings.Contains(string(raw), "private_arbitrary") {
		t.Fatal("timeline wrong count/private fields")
	}
	_, e = f.call("attachments.remove", uuid.NewString(), f.w, attachment.RemoveInput{AttachmentID: file.ID, ExpectedVersion: file.Version})
	if e != nil {
		t.Fatal(e)
	}
	raw, e = f.call("issue_timeline.list", "", f.w, comment.TimelineInput{IssueID: i.ID, Limit: 100})
	removed := decodeResult[comment.TimelineList](t, raw, e)
	if len(removed.Items) != 7 || !removed.Items[6].Detail.Attachment.Removed {
		t.Fatal("removed event missing")
	}
}
