package usecase

import (
	"encoding/json"
	"github.com/google/uuid"
	"strings"
	"testing"
)

func parse(t *testing.T, s string) []Node {
	t.Helper()
	var nodes []Node
	if e := json.Unmarshal([]byte(s), &nodes); e != nil {
		t.Fatal(e)
	}
	return nodes
}
func TestCanonicalMixedContent(t *testing.T) {
	id := func() string { return uuid.NewString() }
	raw := `[{"id":"` + id() + `","type":"p","indent":1,"listStyleType":"todo","checked":false,"children":[{"text":"  Türkçe 🧭\r\n\n\r","bold":false}]},{"id":"` + id() + `","type":"code_block","lang":"go","children":[{"id":"` + id() + `","type":"code_line","children":[{"text":"first\r"}]},{"id":"` + id() + `","type":"code_line","children":[{"text":""}]}]},{"id":"` + id() + `","type":"table","colSizes":[100.1,200],"children":[{"id":"` + id() + `","type":"tr","children":[{"id":"` + id() + `","type":"td","colSpan":2,"rowSpan":2,"background":"#f00","children":[{"id":"` + id() + `","type":"p","children":[{"text":"merged"}]}]}]},{"id":"` + id() + `","type":"tr","children":[]}]}]`
	nodes := parse(t, raw)
	if e := ValidateContent(nodes); len(e) > 0 {
		t.Fatal(e)
	}
	encoded, e := json.Marshal(nodes)
	if e != nil {
		t.Fatal(e)
	}
	var a, b any
	if e := json.Unmarshal([]byte(raw), &a); e != nil {
		t.Fatal(e)
	}
	if e := json.Unmarshal(encoded, &b); e != nil {
		t.Fatal(e)
	}
	aa, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	if string(aa) != string(bb) {
		t.Fatal("lossy canonical roundtrip")
	}
	html := renderHTML(nodes)
	if !strings.Contains(html, "first\r\n") || !strings.Contains(html, "rowspan=\"2\"") {
		t.Fatal("merged table/code export", html)
	}
}
func TestCanonicalInvalidContent(t *testing.T) {
	id := uuid.NewString()
	for name, raw := range map[string]string{"unknown_property": `[{"id":"` + id + `","type":"p","x":1,"children":[{"text":"a"}]}]`, "leaf_id": `[{"id":"` + id + `","type":"p","children":[{"id":"` + uuid.NewString() + `","text":"a"}]}]`, "precision": `[{"id":"` + id + `","type":"table","marginLeft":0.10000000000000001,"children":[]}]`} {
		t.Run(name, func(t *testing.T) {
			var nodes []Node
			if e := json.Unmarshal([]byte(raw), &nodes); e == nil {
				t.Fatal("invalid structural input decoded")
			}
		})
	}
	for name, raw := range map[string]string{"duplicate_case": `[{"id":"` + id + `","type":"p","children":[{"text":"a"}]},{"id":"` + strings.ToUpper(id) + `","type":"p","children":[{"text":"b"}]}]`, "compact_id": `[{"id":"` + strings.ReplaceAll(id, "-", "") + `","type":"p","children":[{"text":"a"}]}]`, "empty_row": `[{"id":"` + id + `","type":"table","children":[{"id":"` + uuid.NewString() + `","type":"tr","children":[]}]}]`} {
		t.Run(name, func(t *testing.T) {
			if e := ValidateContent(parse(t, raw)); len(e) == 0 {
				t.Fatal("semantic input accepted")
			}
		})
	}
}
func TestSafeCanonicalColors(t *testing.T) {
	for _, color := range []string{"#abc", "#aabbccdd", "transparent", "currentColor"} {
		if !colorPattern.MatchString(color) {
			t.Fatal(color)
		}
	}
	for _, color := range []string{"red;position:fixed", "url(https://evil.invalid)", "var(--unsafe)", "expression(x)", "#fff\n"} {
		if colorPattern.MatchString(color) {
			t.Fatal(color)
		}
	}
}
func TestPatchStableSections(t *testing.T) {
	h, p, next := uuid.NewString(), uuid.NewString(), uuid.NewString()
	nodes := parse(t, `[{"id":"`+h+`","type":"h1","children":[{"text":"heading"}]},{"id":"`+p+`","type":"p","children":[{"text":"body"}]},{"id":"`+next+`","type":"h1","children":[{"text":"next"}]}]`)
	replacement := parse(t, `[{"id":"`+uuid.NewString()+`","type":"p","children":[{"text":"new\r\n"}]}]`)
	patch := Patch{Value: &ReplaceSection{Op: "replace_section", HeadingID: h, ExpectedMemberIDs: []string{p}, Nodes: replacement}}
	out, e := ApplyPatches(nodes, []Patch{patch})
	if e != nil {
		t.Fatal(e)
	}
	if out[0].element().ID != h || out[2].element().ID != next {
		t.Fatal("section boundary changed")
	}
	patch.Value.(*ReplaceSection).ExpectedMemberIDs = []string{next}
	if _, e = ApplyPatches(nodes, []Patch{patch}); e == nil {
		t.Fatal("stale section accepted")
	}
}

func TestContentByteBudgetMatchesBrowserJSON(t *testing.T) {
	text := strings.Repeat("<>&\u2028\u2029", 25000) + `\u2028`
	nodes := []Node{{Value: &Paragraph{Element: Element{ID: uuid.NewString(), Type: "p", Children: []Node{{Value: &Text{Text: text}}}}}}}
	expected := len(text) + len(`[{"id":"","type":"p","children":[{"text":""}]}]`) + 36 + 1
	if size, e := contentBytes(nodes); e != nil || size != expected {
		t.Fatalf("browser byte size %d expected %d error %v", size, expected, e)
	}
	if details := ValidateContent(nodes); len(details) > 0 {
		t.Fatal(details)
	}
	nodes[0].element().Children[0].Value.(*Text).Text = strings.Repeat("<", MaxContentBytes)
	if details := ValidateContent(nodes); len(details) == 0 {
		t.Fatal("over-limit content accepted")
	}
}
