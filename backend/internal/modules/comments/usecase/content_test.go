package usecase

import (
	plate "github.com/furkanmeclis/traccia/backend/internal/modules/documents/usecase"
	"github.com/google/uuid"
	"testing"
)

func TestCanonicalStringsRejectNULWithoutChangingLiteralEscapes(t *testing.T) {
	for _, tc := range []struct {
		name, value    string
		mention, valid bool
	}{
		{name: "text_NUL", value: "a\x00b"},
		{name: "mention_display_NUL", value: "a\x00b", mention: true},
		{name: "literal_escape", value: `a\u0000b`, valid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			leaf := plate.Node{Value: &plate.Text{Text: tc.value}}
			if tc.mention {
				id := uuid.NewString()
				leaf = plate.Node{Value: &plate.Mention{Element: plate.Element{ID: uuid.NewString(), Type: "mention", Children: []plate.Node{{Value: &plate.Text{Text: ""}}}}, Key: "issue:" + id, Value: tc.value, TargetKind: "issue", TargetID: id}}
			}
			content := []plate.Node{{Value: &plate.Paragraph{Element: plate.Element{ID: uuid.NewString(), Type: "p", Children: []plate.Node{leaf}}}}}
			e := ValidateContent(content)
			if tc.valid && len(e) > 0 {
				t.Fatal(e)
			}
			if !tc.valid && (len(e) != 1 || e[0].Code != "unsupported_character") {
				t.Fatalf("NUL error %#v", e)
			}
		})
	}
}
