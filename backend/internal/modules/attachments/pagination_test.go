package attachments_test

import (
	attachment "github.com/furkanmeclis/traccia/backend/internal/modules/attachments/usecase"
	"github.com/google/uuid"
	"testing"
)

func TestAttachmentCompletePagination(t *testing.T) {
	options := attachment.DefaultOptions()
	options.ChunkBytes = 1
	options.MaxFileBytes = 4096
	f := newAttachmentFixtureOptions(t, options, nil)
	issue := f.create(t, f.createTeam(t, "PAGE").ID)
	for i := 0; i < 107; i++ {
		u := f.upload(t, nil)
		file := f.complete(t, u, nil).Attachment
		result[attachment.AttachmentOutput](t, f, "attachments.bind", attachment.BindInput{AttachmentID: file.ID, DraftID: *file.DraftID, IssueID: issue.ID, ExpectedVersion: 1})
	}
	t.Run("uploads_107_and_mutated_anchor", func(t *testing.T) {
		in := attachment.UploadListInput{State: "any", Limit: 13}
		seen := map[uuid.UUID]bool{}
		for {
			page := result[attachment.UploadList](t, f, "attachment_uploads.list", in)
			for _, u := range page.Items {
				if seen[u.ID] {
					t.Fatal("duplicate")
				}
				seen[u.ID] = true
			}
			if !page.HasMore {
				break
			}
			if page.NextCursor == "" {
				t.Fatal("missing continuation")
			}
			in.After = page.NextCursor
		}
		if len(seen) != 107 {
			t.Fatal("truncated uploads", len(seen))
		}
	})
	t.Run("attachments_107_removed_anchor_and_literal_search", func(t *testing.T) {
		in := attachment.ListInput{IssueID: issue.ID, Limit: 17}
		first := result[attachment.AttachmentList](t, f, "attachments.list", in)
		anchor := first.Items[len(first.Items)-1]
		result[attachment.AttachmentOutput](t, f, "attachments.remove", attachment.RemoveInput{AttachmentID: anchor.ID, ExpectedVersion: anchor.Version})
		seen := map[uuid.UUID]bool{}
		for _, file := range first.Items {
			seen[file.ID] = true
		}
		in.After = first.NextCursor
		for {
			page := result[attachment.AttachmentList](t, f, "attachments.list", in)
			for _, file := range page.Items {
				if seen[file.ID] {
					t.Fatal("duplicate")
				}
				seen[file.ID] = true
			}
			if !page.HasMore {
				break
			}
			in.After = page.NextCursor
		}
		if len(seen) != 107 {
			t.Fatal("mutable anchor truncated", len(seen))
		}
		literal := result[attachment.AttachmentList](t, f, "attachments.list", attachment.ListInput{IssueID: issue.ID, Search: "%_", Limit: 100})
		if literal.HasMore || len(literal.Items) != 0 {
			t.Fatal("search treated text as pattern")
		}
	})
	t.Run("upload_history_over100", func(t *testing.T) {
		data := make([]byte, 106)
		u := f.upload(t, data)
		done := f.complete(t, u, data)
		in := attachment.UploadHistoryInput{UploadID: u.ID, DraftID: u.DraftID, Limit: 11}
		count := 0
		version := int64(0)
		for {
			page := result[attachment.UploadHistoryList](t, f, "attachment_uploads.history", in)
			for _, row := range page.Items {
				if row.Version != version+1 {
					t.Fatal("history gap")
				}
				version = row.Version
				count++
			}
			if !page.HasMore {
				break
			}
			in.After = page.NextCursor
		}
		if count != 108 || version != done.Upload.Version {
			t.Fatal("history truncated", count)
		}
	})
}
