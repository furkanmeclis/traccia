package usecase

import (
	"fmt"
	"math"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/uuid"
)

var colorPattern = regexp.MustCompile(`^(#[0-9a-fA-F]{3}|#[0-9a-fA-F]{4}|#[0-9a-fA-F]{6}|#[0-9a-fA-F]{8}|transparent|currentColor)$`)
var languagePattern = regexp.MustCompile(`^[A-Za-z0-9_+.-]{0,64}$`)

func field(path, code string) []operations.FieldError {
	return []operations.FieldError{{Field: path, Code: code}}
}
func FormatErrors(format string, version int32) []operations.FieldError {
	if format != Format {
		return field("format", "unsupported")
	}
	if version != SchemaVersion {
		return field("schema_version", "unsupported")
	}
	return nil
}
func canonicalID(s string) (uuid.UUID, bool) {
	if len(s) != 36 || s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return uuid.Nil, false
	}
	id, e := uuid.Parse(s)
	return id, e == nil && id != uuid.Nil
}
func numeric(v *LayoutNumber) bool {
	return v == nil || (!math.IsNaN(float64(*v)) && !math.IsInf(float64(*v), 0) && *v >= 0 && *v <= 10000)
}
func SafeURL(value string) bool {
	if value == "" || len(value) > 2048 || strings.TrimSpace(value) != value || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return false
	}
	u, e := url.Parse(value)
	if e != nil || u.User != nil {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "https", "http":
		return u.Host != ""
	case "mailto":
		return u.Opaque != "" && !strings.ContainsAny(u.Opaque, "\r\n")
	}
	return false
}
func isBlock(kind string) bool {
	switch kind {
	case "p", "h1", "h2", "h3", "blockquote", "hr", "code_block", "table":
		return true
	}
	return false
}
func isInline(n Node) bool {
	if _, ok := n.Value.(*Text); ok {
		return true
	}
	el := n.element()
	return el != nil && (el.Type == "a" || el.Type == "mention")
}
func emptyVoid(children []Node) bool {
	if len(children) != 1 {
		return false
	}
	leaf, ok := children[0].Value.(*Text)
	return ok && leaf.Text == "" && leaf.Bold == nil && leaf.Italic == nil && leaf.Underline == nil && leaf.Strikethrough == nil && leaf.Code == nil
}
func childrenMatch(children []Node, kind string) bool {
	for _, child := range children {
		element := child.element()
		switch kind {
		case "inline":
			if !isInline(child) {
				return false
			}
		case "text":
			if _, ok := child.Value.(*Text); !ok {
				return false
			}
		case "block":
			if element == nil || !isBlock(element.Type) {
				return false
			}
		default:
			if element == nil {
				return false
			}
			if kind == "cell" {
				if element.Type != "td" && element.Type != "th" {
					return false
				}
			} else if element.Type != kind {
				return false
			}
		}
	}
	return true
}
func ValidateContent(content []Node) []operations.FieldError {
	if len(content) == 0 || !childrenMatch(content, "block") {
		return field("content", "invalid_shape")
	}
	seen := map[uuid.UUID]bool{}
	count := 0
	var walk func(Node, string, int) []operations.FieldError
	walk = func(node Node, path string, depth int) []operations.FieldError {
		count++
		if count > MaxNodes {
			return field("content", "node_limit")
		}
		if depth > MaxDepth {
			return field(path, "depth_limit")
		}
		if leaf, ok := node.Value.(*Text); ok {
			if !utf8.ValidString(leaf.Text) {
				return field(path+".text", "invalid_unicode")
			}
			return nil
		}
		el := node.element()
		if el == nil {
			return field(path, "invalid_node")
		}
		id, ok := canonicalID(el.ID)
		if !ok {
			return field(path+".id", "invalid")
		}
		if seen[id] {
			return field(path+".id", "duplicate_id")
		}
		seen[id] = true
		if el.Children == nil {
			return field(path+".children", "required")
		}
		shape := ""
		allowEmpty := false
		switch value := node.Value.(type) {
		case *Paragraph:
			if el.Type != "p" {
				return field(path, "invalid_type")
			}
			shape = "inline"
			if value.Indent != nil && (*value.Indent < 0 || *value.Indent > 16) {
				return field(path+".indent", "invalid")
			}
			style := ""
			if value.ListStyleType != nil {
				style = *value.ListStyleType
				if style != "disc" && style != "decimal" && style != "todo" {
					return field(path+".listStyleType", "invalid")
				}
			}
			if value.Checked != nil && style != "todo" {
				return field(path+".checked", "invalid")
			}
			for _, number := range []*int32{value.ListStart, value.ListRestart} {
				if number != nil && (*number < 1 || *number > 1000000 || style != "decimal") {
					return field(path, "invalid_list")
				}
			}
		case *Heading:
			if el.Type != "h1" && el.Type != "h2" && el.Type != "h3" {
				return field(path, "invalid_type")
			}
			shape = "inline"
		case *Blockquote:
			if el.Type != "blockquote" {
				return field(path, "invalid_type")
			}
			shape = "inline"
		case *HorizontalRule:
			if el.Type != "hr" || !emptyVoid(el.Children) {
				return field(path, "invalid_void")
			}
			shape = "text"
		case *CodeBlock:
			if el.Type != "code_block" {
				return field(path, "invalid_type")
			}
			if value.Lang != nil && !languagePattern.MatchString(*value.Lang) {
				return field(path+".lang", "invalid")
			}
			shape = "code_line"
		case *CodeLine:
			if el.Type != "code_line" {
				return field(path, "invalid_type")
			}
			shape = "text"
		case *Link:
			if el.Type != "a" || !SafeURL(value.URL) {
				return field(path+".url", "invalid")
			}
			if value.Target != nil && *value.Target != "_blank" && *value.Target != "_self" {
				return field(path+".target", "invalid")
			}
			shape = "text"
		case *Mention:
			if el.Type != "mention" || !emptyVoid(el.Children) {
				return field(path, "invalid_void")
			}
			shape = "text"
			if value.TargetKind != "user" && value.TargetKind != "issue" && value.TargetKind != "project" {
				return field(path+".target_kind", "invalid")
			}
			target, ok := canonicalID(value.TargetID)
			if !ok {
				return field(path+".target_id", "invalid")
			}
			prefix := value.TargetKind + ":"
			if !strings.HasPrefix(value.Key, prefix) {
				return field(path+".key", "invalid")
			}
			key, ok := canonicalID(strings.TrimPrefix(value.Key, prefix))
			if !ok || key != target {
				return field(path+".key", "invalid")
			}
			if !utf8.ValidString(value.Value) || utf8.RuneCountInString(value.Value) > 255 || value.Value == "" {
				return field(path+".value", "invalid")
			}
		case *Table:
			if el.Type != "table" || !numeric(value.MarginLeft) {
				return field(path, "invalid_table")
			}
			shape = "tr"
			if value.ColSizes != nil {
				if len(*value.ColSizes) < 1 || len(*value.ColSizes) > 100 {
					return field(path+".colSizes", "invalid")
				}
				for _, width := range *value.ColSizes {
					if !numeric(&width) {
						return field(path+".colSizes", "invalid")
					}
				}
			}
			if !validGeometry(value) {
				return field(path, "invalid_table_geometry")
			}
		case *TableRow:
			if el.Type != "tr" || !numeric(value.Size) {
				return field(path, "invalid_row")
			}
			shape = "cell"
			allowEmpty = true
		case *TableCell:
			if (el.Type != "td" && el.Type != "th") || !numeric(value.Size) {
				return field(path, "invalid_cell")
			}
			shape = "block"
			for _, span := range []*int32{value.ColSpan, value.RowSpan} {
				if span != nil && (*span < 1 || *span > 100) {
					return field(path, "invalid_span")
				}
			}
			if value.Background != nil && !colorPattern.MatchString(*value.Background) {
				return field(path+".background", "invalid")
			}
			if value.Borders != nil {
				for _, border := range []*Border{value.Borders.Top, value.Borders.Right, value.Borders.Bottom, value.Borders.Left} {
					if border != nil {
						if !colorPattern.MatchString(border.Color) || !numeric(&border.Size) || (border.Style != "solid" && border.Style != "dashed" && border.Style != "dotted" && border.Style != "none") {
							return field(path+".borders", "invalid")
						}
					}
				}
			}
		default:
			return field(path, "invalid_node")
		}
		if (!allowEmpty && len(el.Children) == 0) || !childrenMatch(el.Children, shape) {
			return field(path+".children", "invalid_shape")
		}
		for i, child := range el.Children {
			if errors := walk(child, fmt.Sprintf("%s.children[%d]", path, i), depth+1); len(errors) > 0 {
				return errors
			}
		}
		return nil
	}
	for i, node := range content {
		if errors := walk(node, fmt.Sprintf("content[%d]", i), 1); len(errors) > 0 {
			return errors
		}
	}
	size, e := contentBytes(content)
	if e != nil {
		return field("content", "invalid")
	}
	if size > MaxContentBytes {
		return field("content", "bytes_limit")
	}
	return nil
}
func validGeometry(table *Table) bool {
	rows := table.Children
	if len(rows) < 1 || len(rows) > 100 {
		return false
	}
	occupied := map[[2]int]bool{}
	width := 0
	for r, node := range rows {
		row, ok := node.Value.(*TableRow)
		if !ok || len(row.Children) > 100 {
			return false
		}
		column := 0
		for _, child := range row.Children {
			cell, ok := child.Value.(*TableCell)
			if !ok {
				return false
			}
			for occupied[[2]int{r, column}] {
				column++
			}
			colSpan, rowSpan := 1, 1
			if cell.ColSpan != nil {
				colSpan = int(*cell.ColSpan)
			}
			if cell.RowSpan != nil {
				rowSpan = int(*cell.RowSpan)
			}
			if colSpan < 1 || colSpan > 100 || rowSpan < 1 || rowSpan > 100 || column+colSpan > 100 || r+rowSpan > len(rows) {
				return false
			}
			for y := r; y < r+rowSpan; y++ {
				for x := column; x < column+colSpan; x++ {
					key := [2]int{y, x}
					if occupied[key] {
						return false
					}
					occupied[key] = true
				}
			}
			column += colSpan
			if column > width {
				width = column
			}
		}
	}
	if width == 0 {
		return false
	}
	for r := range rows {
		for column := 0; column < width; column++ {
			if !occupied[[2]int{r, column}] {
				return false
			}
		}
	}
	return table.ColSizes == nil || len(*table.ColSizes) == width
}
func contentFailure(errors []operations.FieldError) error {
	if len(errors) == 0 {
		return nil
	}
	return &operations.Error{Code: operations.Validation, Details: errors}
}
