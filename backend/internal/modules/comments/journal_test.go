package comments_test

import (
	"encoding/json"
	comment "github.com/furkanmeclis/traccia/backend/internal/modules/comments/usecase"
	doc "github.com/furkanmeclis/traccia/backend/internal/modules/documents/usecase"
	relation "github.com/furkanmeclis/traccia/backend/internal/modules/relations/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/uuid"
	"reflect"
	"testing"
)

func paragraph(id, text string) doc.Node {
	return doc.Node{Value: &doc.Paragraph{Element: doc.Element{ID: id, Type: "p", Children: []doc.Node{{Value: &doc.Text{Text: text}}}}}}
}
func createComment(t *testing.T, f commentFixture, id uuid.UUID, nodes []doc.Node) comment.Comment {
	t.Helper()
	raw, e := f.call("comments.create", uuid.NewString(), f.w, comment.CreateInput{IssueID: id, Format: "plate", SchemaVersion: 1, Content: nodes})
	return decodeResult[comment.Output](t, raw, e).Comment
}
func TestCommentJournalAndTimeline(t *testing.T) {
	f := newCommentFixture(t)
	team := f.createTeam(t, "COM")
	issue := f.create(t, team.ID)
	id, untouched := uuid.NewString(), uuid.NewString()
	initial := []doc.Node{paragraph(id, "  Türkçe 🧭\r\nline\n\r"), paragraph(untouched, "unchanged")}
	c := createComment(t, f, issue.ID, initial)
	if c.AuthorID != f.actor || c.CreatedChannel != "api" {
		t.Fatal("immutable provenance")
	}
	input := comment.PatchInput{CommentID: c.ID, ExpectedRevision: c.Revision, Format: "plate", SchemaVersion: 1, Patches: []doc.Patch{{Value: &doc.ReplaceNode{Op: "replace_node", NodeID: id, Node: paragraph(id, "changed")}}}}
	key := uuid.NewString()
	first, e := f.call("comments.patch", key, f.w, input)
	next := decodeResult[comment.Output](t, first, e).Comment
	raw, _ := json.Marshal(input)
	replay, e := f.registry.Execute(f.ctx, operations.Call{OperationID: "comments.patch", Channel: operations.MCP, WorkspaceID: f.w, Input: raw, IdempotencyKey: key})
	if e != nil || string(replay) != string(first) {
		t.Fatalf("cross-channel replay: %v", e)
	}
	if !reflect.DeepEqual(next.Content[1], initial[1]) {
		t.Fatal("untouched node changed")
	}
	_, e = f.call("comments.patch", uuid.NewString(), f.w, input)
	checkCode(t, e, operations.VersionConflict)
	raw, e = f.call("comments.delete", uuid.NewString(), f.w, comment.DeleteInput{CommentID: c.ID, ExpectedRevision: next.Revision})
	deleted := decodeResult[comment.Output](t, raw, e).Comment
	if !deleted.Deleted || !reflect.DeepEqual(deleted.Content, next.Content) {
		t.Fatal("delete lost content")
	}
	raw, e = f.call("comments.get_revision", "", f.w, comment.RevisionInput{CommentID: c.ID, RevisionID: c.Revision})
	historic := decodeResult[comment.RevisionOutput](t, raw, e)
	if !reflect.DeepEqual(historic.Comment.Content, initial) || historic.Revision.Channel != "api" {
		t.Fatal("history/provenance changed")
	}
	raw, e = f.call("comments.restore", uuid.NewString(), f.w, comment.RestoreInput{CommentID: c.ID, ExpectedRevision: deleted.Revision, RevisionID: c.Revision})
	restored := decodeResult[comment.Output](t, raw, e).Comment
	if restored.Deleted || restored.Revision == c.Revision || !reflect.DeepEqual(restored.Content, initial) {
		t.Fatal("restore failed")
	}
	raw, e = f.call("comments.list", "", f.w, comment.ListInput{IssueID: issue.ID, Limit: 100})
	list := decodeResult[comment.List](t, raw, e)
	if len(list.Items) != 1 || !list.Capabilities.Create {
		t.Fatal("list")
	}
	raw, e = f.call("issue_timeline.list", "", f.w, comment.TimelineInput{IssueID: issue.ID, Limit: 100})
	timeline := decodeResult[comment.TimelineList](t, raw, e)
	counts := map[string]int{}
	for _, v := range timeline.Items {
		counts[v.Detail.Kind]++
		if v.Channel != "api" {
			t.Fatal("current channel")
		}
	}
	if counts["issue"] != 1 || counts["comment"] != 4 {
		t.Fatalf("timeline %#v", counts)
	}
	var checkpoints, deltas int
	if e = f.pool.QueryRow(f.ctx, "SELECT count(*) FILTER(WHERE checkpoint IS NOT NULL),count(*) FILTER(WHERE kind='patch' AND checkpoint IS NULL AND jsonb_array_length(patches)=1) FROM comment_revisions").Scan(&checkpoints, &deltas); e != nil {
		t.Fatal(e)
	}
	if checkpoints != 3 || deltas != 1 {
		t.Fatal("delta journal")
	}
}
func TestRelationTimelineKinds(t *testing.T) {
	f := newCommentFixture(t)
	team := f.createTeam(t, "REL")
	a, b := f.create(t, team.ID), f.create(t, team.ID)
	expected := func(t *testing.T) []relation.ExpectedVersion {
		var av, bv int64
		if e := f.pool.QueryRow(f.ctx, "SELECT (document->>'relation_version')::bigint FROM relation_issue_reference WHERE workspace_uuid=$1 AND uuid=$2", f.w, a.ID).Scan(&av); e != nil {
			t.Fatal(e)
		}
		if e := f.pool.QueryRow(f.ctx, "SELECT (document->>'relation_version')::bigint FROM relation_issue_reference WHERE workspace_uuid=$1 AND uuid=$2", f.w, b.ID).Scan(&bv); e != nil {
			t.Fatal(e)
		}
		return []relation.ExpectedVersion{{IssueID: a.ID, ExpectedVersion: av}, {IssueID: b.ID, ExpectedVersion: bv}}
	}
	for _, kind := range []string{"parent", "duplicate", "blocks", "related"} {
		t.Run(kind, func(t *testing.T) {
			var in any
			op := "issue_relations."
			switch kind {
			case "parent", "duplicate":
				target := b.ID.String()
				in = relation.SetInput{IssueID: a.ID, TargetIssueID: &target, ExpectedVersions: expected(t)}
				op += "set_" + map[string]string{"parent": "parent", "duplicate": "duplicate"}[kind]
			case "blocks":
				in = relation.LinkInput{IssueID: a.ID, TargetIssueID: b.ID, ExpectedVersions: expected(t)}
				op += "link_block"
			default:
				in = relation.LinkInput{IssueID: a.ID, TargetIssueID: b.ID, ExpectedVersions: expected(t)}
				op += "link_related"
			}
			if _, e := f.call(op, uuid.NewString(), f.w, in); e != nil {
				t.Fatal(e)
			}
			for _, id := range []uuid.UUID{a.ID, b.ID} {
				raw, e := f.call("issue_timeline.list", "", f.w, comment.TimelineInput{IssueID: id, Limit: 100})
				list := decodeResult[comment.TimelineList](t, raw, e)
				found := false
				for _, v := range list.Items {
					if v.Detail.Relation != nil {
						for _, r := range v.Detail.Relation.Snapshot.ChangedRelations {
							if r.Kind == kind {
								found = true
							}
						}
					}
				}
				if !found {
					t.Fatal("relation missing")
				}
			}
		})
	}
}
