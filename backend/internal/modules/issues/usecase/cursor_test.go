package usecase

import (
	"github.com/google/uuid"
	"strings"
	"testing"
)

func TestCursorCanonicalizesIdentitiesButPreservesSearchSemantics(t *testing.T) {
	w, id := uuid.New(), uuid.New()
	in := PageInput{TeamID: id.String(), Q: id.String()}
	upper := in
	upper.TeamID = strings.ToUpper(in.TeamID)
	if cursorScope(w, in) != cursorScope(w, upper) {
		t.Fatal("equivalent UUID filters changed scope")
	}
	differentSearch := in
	differentSearch.Q = "urn:uuid:" + in.Q
	if cursorScope(w, in) == cursorScope(w, differentSearch) {
		t.Fatal("different literal searches shared cursor scope")
	}
	in.After = "ignored"
	in.Limit = 2
	upper.Limit = 99
	if cursorScope(w, in) != cursorScope(w, upper) {
		t.Fatal("pagination fields changed immutable cursor scope")
	}
}
