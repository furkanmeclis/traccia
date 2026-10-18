package usecase

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// Node is a sealed, typed union. Its published schema enumerates every variant.
// Discrimination never permits an untyped fallback or extension properties.
type Node struct {
	Value NodeValue `json:"-"`
}
type NodeValue interface{ plateNode() }
type Text struct {
	Text          string `json:"text"`
	Bold          *bool  `json:"bold,omitempty"`
	Italic        *bool  `json:"italic,omitempty"`
	Underline     *bool  `json:"underline,omitempty"`
	Strikethrough *bool  `json:"strikethrough,omitempty"`
	Code          *bool  `json:"code,omitempty"`
}
type Element struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Children []Node `json:"children"`
}
type Paragraph struct {
	Element
	Indent        *int32  `json:"indent,omitempty"`
	ListStyleType *string `json:"listStyleType,omitempty"`
	Checked       *bool   `json:"checked,omitempty"`
	ListStart     *int32  `json:"listStart,omitempty"`
	ListRestart   *int32  `json:"listRestart,omitempty"`
}
type Heading struct{ Element }
type Blockquote struct{ Element }
type HorizontalRule struct{ Element }
type CodeBlock struct {
	Element
	Lang *string `json:"lang,omitempty"`
}
type CodeLine struct{ Element }
type Link struct {
	Element
	URL    string  `json:"url"`
	Target *string `json:"target,omitempty"`
}
type Mention struct {
	Element
	Key        string `json:"key"`
	Value      string `json:"value"`
	TargetKind string `json:"target_kind"`
	TargetID   string `json:"target_id"`
}
type Table struct {
	Element
	ColSizes   *[]LayoutNumber `json:"colSizes,omitempty"`
	MarginLeft *LayoutNumber   `json:"marginLeft,omitempty"`
}
type TableRow struct {
	Element
	Size *LayoutNumber `json:"size,omitempty"`
}
type Border struct {
	Color string       `json:"color"`
	Style string       `json:"style"`
	Size  LayoutNumber `json:"size"`
}
type Borders struct {
	Top    *Border `json:"top,omitempty"`
	Right  *Border `json:"right,omitempty"`
	Bottom *Border `json:"bottom,omitempty"`
	Left   *Border `json:"left,omitempty"`
}
type TableCell struct {
	Element
	ColSpan    *int32        `json:"colSpan,omitempty"`
	RowSpan    *int32        `json:"rowSpan,omitempty"`
	Size       *LayoutNumber `json:"size,omitempty"`
	Background *string       `json:"background,omitempty"`
	Borders    *Borders      `json:"borders,omitempty"`
}

func (*Text) plateNode()           {}
func (*Paragraph) plateNode()      {}
func (*Heading) plateNode()        {}
func (*Blockquote) plateNode()     {}
func (*HorizontalRule) plateNode() {}
func (*CodeBlock) plateNode()      {}
func (*CodeLine) plateNode()       {}
func (*Link) plateNode()           {}
func (*Mention) plateNode()        {}
func (*Table) plateNode()          {}
func (*TableRow) plateNode()       {}
func (*TableCell) plateNode()      {}
func strict(raw []byte, value any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if e := d.Decode(value); e != nil {
		return e
	}
	if e := d.Decode(new(any)); e != io.EOF {
		return fmt.Errorf("exactly one JSON value required")
	}
	return nil
}
func (n Node) MarshalJSON() ([]byte, error) {
	if n.Value == nil {
		return []byte("null"), nil
	}
	return json.Marshal(n.Value)
}
func (n *Node) UnmarshalJSON(raw []byte) error {
	var fields map[string]json.RawMessage
	if e := json.Unmarshal(raw, &fields); e != nil {
		return e
	}
	var kind string
	if value, ok := fields["type"]; ok {
		if e := json.Unmarshal(value, &kind); e != nil {
			return e
		}
	}
	var value NodeValue
	switch kind {
	case "":
		if fields["text"] == nil {
			return fmt.Errorf("unknown Plate node")
		}
		value = &Text{}
	case "p":
		value = &Paragraph{}
	case "h1", "h2", "h3":
		value = &Heading{}
	case "blockquote":
		value = &Blockquote{}
	case "hr":
		value = &HorizontalRule{}
	case "code_block":
		value = &CodeBlock{}
	case "code_line":
		value = &CodeLine{}
	case "a":
		value = &Link{}
	case "mention":
		value = &Mention{}
	case "table":
		value = &Table{}
	case "tr":
		value = &TableRow{}
	case "td", "th":
		value = &TableCell{}
	default:
		return fmt.Errorf("unsupported Plate node")
	}
	if e := strict(raw, value); e != nil {
		return e
	}
	n.Value = value
	return nil
}
func (n Node) element() *Element {
	switch v := n.Value.(type) {
	case *Paragraph:
		return &v.Element
	case *Heading:
		return &v.Element
	case *Blockquote:
		return &v.Element
	case *HorizontalRule:
		return &v.Element
	case *CodeBlock:
		return &v.Element
	case *CodeLine:
		return &v.Element
	case *Link:
		return &v.Element
	case *Mention:
		return &v.Element
	case *Table:
		return &v.Element
	case *TableRow:
		return &v.Element
	case *TableCell:
		return &v.Element
	}
	return nil
}
