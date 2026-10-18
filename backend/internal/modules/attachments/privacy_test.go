package attachments_test

import (
	"encoding/json"
	"github.com/furkanmeclis/traccia/backend/internal/database/db"
	attachment "github.com/furkanmeclis/traccia/backend/internal/modules/attachments/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/authctx"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/furkanmeclis/traccia/backend/internal/platform/outbox"
	"github.com/google/uuid"
	"strings"
	"testing"
	"time"
)

func TestAttachmentPrivateHistoryAndPublicViews(t *testing.T) {
	f := newAttachmentFixture(t)
	other := uuid.New()
	_, e := f.pool.Exec(f.ctx, "INSERT INTO users(uuid,email,password_hash,name,surname,status) VALUES($1,$2,'unused','Reader','Member','active')", other, other.String()+"@example.test")
	if e != nil {
		t.Fatal(e)
	}
	_, e = f.pool.Exec(f.ctx, "INSERT INTO workspace_members(workspace_id,user_id,role) SELECT w.id,u.id,'member' FROM workspaces w,users u WHERE w.uuid=$1 AND u.uuid=$2", f.w, other)
	if e != nil {
		t.Fatal(e)
	}
	member := f
	member.ctx = authctx.WithPrincipal(f.ctx, authctx.Principal{UserID: other})
	u := f.upload(t, []byte("private"))
	file := f.complete(t, u, []byte("private")).Attachment
	t.Run("unbound_metadata_and_history_require_creator_and_draft", func(t *testing.T) {
		for _, actor := range []attachmentFixture{f, member} {
			draft := ""
			if actor.ctx == member.ctx {
				draft = u.DraftID.String()
			}
			_, e := actor.call("attachments.get", "", f.w, attachment.AttachmentInput{AttachmentID: file.ID, DraftID: draft})
			checkCode(t, e, operations.NotFound)
			_, e = actor.call("attachments.history", "", f.w, attachment.HistoryInput{AttachmentID: file.ID, DraftID: draft, Limit: 100})
			checkCode(t, e, operations.NotFound)
		}
	})
	issue := f.create(t, f.createTeam(t, "PRIV").ID)
	file = result[attachment.AttachmentOutput](t, f, "attachments.bind", attachment.BindInput{AttachmentID: file.ID, DraftID: u.DraftID, IssueID: issue.ID, ExpectedVersion: 1}).Attachment
	t.Run("bound_public_get_and_list_redact_command_context", func(t *testing.T) {
		for _, actor := range []attachmentFixture{f, member} {
			view := result[attachment.AttachmentOutput](t, actor, "attachments.get", attachment.AttachmentInput{AttachmentID: file.ID}).Attachment
			if view.UploadID != nil || view.DraftID != nil || view.Capabilities.History {
				t.Fatal("public get exposes private context")
			}
			list := result[attachment.AttachmentList](t, actor, "attachments.list", attachment.ListInput{IssueID: issue.ID, Limit: 100})
			if len(list.Items) != 1 || list.Items[0].UploadID != nil || list.Items[0].DraftID != nil || list.Items[0].Capabilities.History {
				t.Fatal("public list exposes private context")
			}
		}
	})
	t.Run("bound_private_history_remains_owner_and_draft_scoped", func(t *testing.T) {
		for _, draft := range []string{"", u.DraftID.String(), uuid.NewString()} {
			_, e := member.call("attachments.history", "", f.w, attachment.HistoryInput{AttachmentID: file.ID, DraftID: draft, Limit: 100})
			checkCode(t, e, operations.NotFound)
			view := result[attachment.AttachmentOutput](t, member, "attachments.get", attachment.AttachmentInput{AttachmentID: file.ID, DraftID: draft}).Attachment
			if view.DraftID != nil || view.UploadID != nil || view.Capabilities.History {
				t.Fatal("member learned context")
			}
		}
		private := result[attachment.AttachmentOutput](t, f, "attachments.get", attachment.AttachmentInput{AttachmentID: file.ID, DraftID: u.DraftID.String()}).Attachment
		if private.DraftID == nil || private.UploadID == nil || !private.Capabilities.History {
			t.Fatal("owner capability missing")
		}
		history := result[attachment.HistoryList](t, f, "attachments.history", attachment.HistoryInput{AttachmentID: file.ID, DraftID: u.DraftID.String(), Limit: 100})
		if len(history.Items) != 2 || history.Items[0].Snapshot.DraftID == nil || *history.Items[0].Snapshot.DraftID != u.DraftID {
			t.Fatal("lossless owner history lost")
		}
	})
	t.Run("remove_response_and_durable_projection_are_safe", func(t *testing.T) {
		removed := result[attachment.AttachmentOutput](t, f, "attachments.remove", attachment.RemoveInput{AttachmentID: file.ID, ExpectedVersion: 2}).Attachment
		if removed.DraftID != nil || removed.UploadID != nil || removed.Capabilities.History {
			t.Fatal("remove leaked context")
		}
		store := outbox.NewStore(f.pool, db.New(f.pool))
		consumers, e := f.workspaceRepo.DurableConsumers()
		if e != nil {
			t.Fatal(e)
		}
		for {
			claims, e := store.Claim(f.ctx, 100, time.Minute)
			if e != nil {
				t.Fatal(e)
			}
			if len(claims) == 0 {
				break
			}
			for _, claim := range claims {
				if e = store.Process(f.ctx, claim, consumers); e != nil {
					t.Fatal(e)
				}
			}
		}
		activity, e := member.workspaceRepo.ListActivity(member.ctx, f.w, uuid.Nil, 100)
		if e != nil {
			t.Fatal(e)
		}
		found := 0
		for _, row := range activity {
			if !strings.HasPrefix(row.EventName, "attachment") {
				continue
			}
			found++
			raw, _ := json.Marshal(row)
			if strings.Contains(string(raw), u.DraftID.String()) {
				t.Fatal("derived activity leaks draft identity")
			}
			if strings.Contains(string(raw), u.ID.String()) && row.ResourceType != "attachment_uploads" {
				t.Fatal("derived activity leaks upload command context")
			}
			if row.EventName == "attachments.bound" || row.EventName == "attachments.removed" {
				if row.ResourceType != "issues" || row.IssueID != issue.ID.String() || row.Identifier != issue.Identifier {
					t.Fatal("issue context projection missing")
				}
			}
		}
		if found != 8 {
			t.Fatalf("projected lifecycle %d", found)
		}
		rows, e := f.pool.Query(f.ctx, "SELECT payload::text FROM outbox_events WHERE workspace_uuid=$1 AND event_name LIKE 'attachment%'", f.w)
		if e != nil {
			t.Fatal(e)
		}
		defer rows.Close()
		for rows.Next() {
			var raw string
			if e = rows.Scan(&raw); e != nil {
				t.Fatal(e)
			}
			if strings.Contains(raw, u.DraftID.String()) || strings.Contains(raw, "snapshot") || strings.Contains(raw, "data_base64") {
				t.Fatal("outbox leaks private history")
			}
		}
		if e = rows.Err(); e != nil {
			t.Fatal(e)
		}
	})
}
