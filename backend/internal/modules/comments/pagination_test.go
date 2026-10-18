package comments_test

import (
	comment "github.com/furkanmeclis/traccia/backend/internal/modules/comments/usecase"
	issue "github.com/furkanmeclis/traccia/backend/internal/modules/issues/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/uuid"
	"testing"
)

func TestCommentFullPaginationAndImmutableAnchors(t *testing.T) {
	f := newCommentFixture(t)
	team := f.createTeam(t, "PAG")
	i := f.create(t, team.ID)
	comments := []comment.Comment{}
	for range 103 {
		comments = append(comments, createComment(t, f, i.ID, mentionContent("user", f.actor)))
	}
	raw, e := f.call("comments.list", "", f.w, comment.ListInput{IssueID: i.ID, Limit: 1})
	first := decodeResult[comment.List](t, raw, e)
	if !first.HasMore {
		t.Fatal("missing continuation")
	}
	anchor := first.Items[0]
	_, e = f.call("comments.delete", uuid.NewString(), f.w, comment.DeleteInput{CommentID: anchor.ID, ExpectedRevision: anchor.Revision})
	if e != nil {
		t.Fatal(e)
	}
	count := 0
	seen := map[uuid.UUID]bool{}
	after := first.NextCursor
	for {
		raw, e = f.call("comments.list", "", f.w, comment.ListInput{IssueID: i.ID, Limit: 17, After: after})
		page := decodeResult[comment.List](t, raw, e)
		for _, c := range page.Items {
			if seen[c.ID] || c.ID == anchor.ID {
				t.Fatal("duplicate/deleted row")
			}
			seen[c.ID] = true
			count++
		}
		if !page.HasMore {
			break
		}
		if page.NextCursor == after {
			t.Fatal("cursor stalled")
		}
		after = page.NextCursor
	}
	if count != 102 {
		t.Fatalf("comments %d", count)
	}
	current := comments[1]
	for range 105 {
		raw, e = f.call("comments.replace", uuid.NewString(), f.w, comment.ReplaceInput{CommentID: current.ID, ExpectedRevision: current.Revision, Format: "plate", SchemaVersion: 1, Content: mentionContent("user", f.actor)})
		current = decodeResult[comment.Output](t, raw, e).Comment
	}
	revisions := 0
	after = ""
	for {
		raw, e = f.call("comments.revisions", "", f.w, comment.RevisionsInput{CommentID: current.ID, Limit: 19, After: after})
		page := decodeResult[comment.RevisionList](t, raw, e)
		revisions += len(page.Items)
		if !page.HasMore {
			break
		}
		after = page.NextCursor
	}
	if revisions != 106 {
		t.Fatalf("revisions %d", revisions)
	}
	backlinks := 0
	after = ""
	for {
		raw, e = f.call("comments.backlinks", "", f.w, comment.BacklinksInput{TargetKind: "user", TargetID: f.actor, Limit: 13, After: after})
		page := decodeResult[comment.BacklinkList](t, raw, e)
		backlinks += len(page.Items)
		if !page.HasMore {
			break
		}
		after = page.NextCursor
	}
	if backlinks != 102 {
		t.Fatalf("backlinks %d", backlinks)
	}
	raw, e = f.call("issue_timeline.list", "", f.w, comment.TimelineInput{IssueID: i.ID, Limit: 1})
	timelineFirst := decodeResult[comment.TimelineList](t, raw, e)
	nextTeam := f.createTeam(t, "NEW")
	raw, e = f.call("issues.move", uuid.NewString(), f.w, issue.MoveInput{IssueID: i.ID, TeamID: nextTeam.ID, ExpectedVersion: i.Version, StatusID: f.status(t, nextTeam.ID), LabelIDs: []string{}})
	moved := decodeResult[issue.Output](t, raw, e).Issue
	_, e = f.call("issues.archive", uuid.NewString(), f.w, issue.VersionInput{IssueID: i.ID, ExpectedVersion: moved.Version})
	if e != nil {
		t.Fatal(e)
	}
	total := 1
	after = timelineFirst.NextCursor
	seen = map[uuid.UUID]bool{timelineFirst.Items[0].ID: true}
	for {
		raw, e = f.call("issue_timeline.list", "", f.w, comment.TimelineInput{IssueID: i.ID, Limit: 23, After: after})
		page := decodeResult[comment.TimelineList](t, raw, e)
		for _, v := range page.Items {
			if seen[v.ID] || v.IssueID != i.ID {
				t.Fatal("duplicate/wrong issue event")
			}
			seen[v.ID] = true
			total++
		}
		if !page.HasMore {
			break
		}
		after = page.NextCursor
	}
	if total != 212 {
		t.Fatalf("timeline %d", total)
	}
	foreign := f.create(t, nextTeam.ID)
	_, e = f.call("issue_timeline.list", "", f.w, comment.TimelineInput{IssueID: foreign.ID, Limit: 23, After: timelineFirst.NextCursor})
	checkCode(t, e, operations.Validation)
}
