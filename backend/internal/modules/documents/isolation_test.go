package documents_test

import (
	"errors"
	doc "github.com/furkanmeclis/traccia/backend/internal/modules/documents/usecase"
	issue "github.com/furkanmeclis/traccia/backend/internal/modules/issues/usecase"
	ws "github.com/furkanmeclis/traccia/backend/internal/modules/workspaces/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/uuid"
	"testing"
)

func TestDocumentConcurrentCASAndArchiveRetention(t *testing.T) {
	f := newDocumentFixture(t)
	team := f.createTeam(t, "CAS")
	i := f.create(t, team.ID)
	d := createDocument(t, f, i.ID, []doc.Node{paragraph(uuid.NewString(), "old")})
	results := make(chan error, 2)
	for range 2 {
		go func() {
			_, e := f.call("documents.replace", uuid.NewString(), f.w, doc.ReplaceInput{DocumentID: d.ID, ExpectedRevision: d.Revision, Format: "plate", SchemaVersion: 1, Content: []doc.Node{paragraph(uuid.NewString(), "new")}})
			results <- e
		}()
	}
	success, conflict := 0, 0
	for range 2 {
		e := <-results
		if e == nil {
			success++
			continue
		}
		var op *operations.Error
		if errors.As(e, &op) && op.Code == operations.VersionConflict {
			conflict++
		} else {
			t.Fatal(e)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("CAS %d successes %d conflicts", success, conflict)
	}
	raw, e := f.call("documents.get", "", f.w, doc.DocumentInput{DocumentID: d.ID})
	current := decodeResult[doc.Output](t, raw, e).Document
	_, e = f.call("issues.archive", uuid.NewString(), f.w, issue.VersionInput{IssueID: i.ID, ExpectedVersion: i.Version})
	if e != nil {
		t.Fatal(e)
	}
	raw, e = f.call("documents.get", "", f.w, doc.DocumentInput{DocumentID: d.ID})
	archived := decodeResult[doc.Output](t, raw, e).Document
	if archived.Revision != current.Revision || archived.Capabilities.Manage {
		t.Fatal("archive changed content or retained write capability")
	}
	_, e = f.call("documents.replace", uuid.NewString(), f.w, doc.ReplaceInput{DocumentID: d.ID, ExpectedRevision: current.Revision, Format: "plate", SchemaVersion: 1, Content: []doc.Node{paragraph(uuid.NewString(), "blocked")}})
	checkCode(t, e, operations.Conflict)
}
func TestDocumentWorkspaceReferenceIsolation(t *testing.T) {
	f := newDocumentFixture(t)
	team := f.createTeam(t, "ONE")
	i := f.create(t, team.ID)
	d := createDocument(t, f, i.ID, []doc.Node{paragraph(uuid.NewString(), "local")})
	raw, e := f.call("workspaces.create", uuid.NewString(), uuid.Nil, ws.CreateInput{Name: "Other"})
	other := f
	other.w = decodeResult[ws.WorkspaceOutput](t, raw, e).Workspace.ID
	foreignTeam := other.createTeam(t, "TWO")
	foreign := other.create(t, foreignTeam.ID)
	for _, target := range []uuid.UUID{foreign.ID, uuid.New()} {
		_, e = f.call("documents.replace", uuid.NewString(), f.w, doc.ReplaceInput{DocumentID: d.ID, ExpectedRevision: d.Revision, Format: "plate", SchemaVersion: 1, Content: mentionContent("issue", target)})
		checkCode(t, e, operations.Validation)
	}
	_, e = f.call("documents.get", "", other.w, doc.DocumentInput{DocumentID: d.ID})
	checkCode(t, e, operations.NotFound)
	raw, e = f.call("documents.get", "", f.w, doc.DocumentInput{DocumentID: d.ID})
	if decodeResult[doc.Output](t, raw, e).Document.Revision != d.Revision {
		t.Fatal("cross-workspace input changed head")
	}
}
