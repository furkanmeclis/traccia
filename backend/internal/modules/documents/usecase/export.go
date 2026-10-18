package usecase

import (
	"encoding/json"
	"html"
	"strconv"
	"strings"
)

func listNumbers(content []Node) map[string]int64 {
	out := map[string]int64{}
	type count struct {
		style string
		value int64
	}
	var visit func([]Node)
	visit = func(nodes []Node) {
		counters := map[int32]count{}
		for _, node := range nodes {
			element := node.element()
			if element == nil {
				continue
			}
			if paragraph, ok := node.Value.(*Paragraph); ok && paragraph.ListStyleType != nil {
				indent := int32(0)
				if paragraph.Indent != nil {
					indent = *paragraph.Indent
				}
				for key := range counters {
					if key > indent {
						delete(counters, key)
					}
				}
				number := int64(1)
				if previous := counters[indent]; previous.style == "decimal" {
					number = previous.value + 1
				}
				if paragraph.ListStart != nil {
					number = int64(*paragraph.ListStart)
				}
				if paragraph.ListRestart != nil {
					number = int64(*paragraph.ListRestart)
				}
				counters[indent] = count{*paragraph.ListStyleType, number}
				if *paragraph.ListStyleType == "decimal" {
					out[element.ID] = number
				}
			} else {
				counters = map[int32]count{}
			}
			switch element.Type {
			case "p", "h1", "h2", "h3", "blockquote", "a", "mention", "code_line":
			default:
				visit(element.Children)
			}
		}
	}
	visit(content)
	return out
}
func renderText(value *Text) string {
	text := html.EscapeString(value.Text)
	for _, mark := range []struct {
		enabled *bool
		tag     string
	}{{value.Code, "code"}, {value.Bold, "strong"}, {value.Italic, "em"}, {value.Underline, "u"}, {value.Strikethrough, "s"}} {
		if mark.enabled != nil && *mark.enabled {
			text = "<" + mark.tag + ">" + text + "</" + mark.tag + ">"
		}
	}
	return text
}
func px(number LayoutNumber) string { return strconv.FormatFloat(float64(number), 'g', -1, 64) + "px" }
func layout(node Node) string {
	style := []string{}
	switch value := node.Value.(type) {
	case *Table:
		if value.MarginLeft != nil {
			style = append(style, "margin-left:"+px(*value.MarginLeft))
		}
	case *TableRow:
		if value.Size != nil {
			style = append(style, "height:"+px(*value.Size))
		}
	case *TableCell:
		if value.Size != nil {
			style = append(style, "width:"+px(*value.Size))
		}
		if value.Background != nil {
			style = append(style, "background-color:"+*value.Background)
		}
		if value.Borders != nil {
			for _, side := range []struct {
				name   string
				border *Border
			}{{"top", value.Borders.Top}, {"right", value.Borders.Right}, {"bottom", value.Borders.Bottom}, {"left", value.Borders.Left}} {
				if side.border != nil {
					style = append(style, "border-"+side.name+":"+px(side.border.Size)+" "+side.border.Style+" "+side.border.Color)
				}
			}
		}
	case *Paragraph:
		if value.Indent != nil {
			style = append(style, "padding-left:"+strconv.Itoa(int(*value.Indent)*16)+"px")
		}
	}
	if len(style) == 0 {
		return ""
	}
	return ` style="` + html.EscapeString(strings.Join(style, ";")) + `"`
}

// HTML is built only from a closed node union and a fixed tag/attribute policy.
// Text/attributes are escaped. CSS values are typed bounds or the color grammar.
func renderHTML(content []Node) string {
	numbers := listNumbers(content)
	var render func(Node) string
	children := func(nodes []Node) string {
		var b strings.Builder
		for _, node := range nodes {
			b.WriteString(render(node))
		}
		return b.String()
	}
	render = func(node Node) string {
		if text, ok := node.Value.(*Text); ok {
			return renderText(text)
		}
		element := node.element()
		if element == nil {
			return ""
		}
		inner := children(element.Children)
		attributes := ` data-node-id="` + html.EscapeString(element.ID) + `"` + layout(node)
		switch value := node.Value.(type) {
		case *Paragraph:
			prefix := ""
			if value.ListStyleType != nil {
				switch *value.ListStyleType {
				case "decimal":
					prefix = strconv.FormatInt(numbers[element.ID], 10) + ". "
				case "disc":
					prefix = "• "
				case "todo":
					prefix = "☐ "
					if value.Checked != nil && *value.Checked {
						prefix = "☑ "
					}
				}
			}
			return "<p" + attributes + ">" + prefix + inner + "</p>"
		case *Heading:
			return "<" + element.Type + attributes + ">" + inner + "</" + element.Type + ">"
		case *Blockquote:
			return "<blockquote" + attributes + ">" + inner + "</blockquote>"
		case *HorizontalRule:
			return "<hr" + attributes + ">"
		case *CodeBlock:
			lines := []string{}
			for _, line := range element.Children {
				lines = append(lines, children(line.element().Children))
			}
			language := ""
			if value.Lang != nil {
				language = ` data-language="` + html.EscapeString(*value.Lang) + `"`
			}
			return "<pre" + attributes + "><code" + language + ">" + strings.Join(lines, "\n") + "</code></pre>"
		case *CodeLine:
			return inner
		case *Link:
			target := ""
			if value.Target != nil {
				target = ` target="` + html.EscapeString(*value.Target) + `"`
				if *value.Target == "_blank" {
					target += ` rel="noopener noreferrer"`
				}
			}
			return "<a" + attributes + ` href="` + html.EscapeString(value.URL) + `"` + target + ">" + inner + "</a>"
		case *Mention:
			return "<span" + attributes + ` data-target-kind="` + value.TargetKind + `" data-target-id="` + html.EscapeString(value.TargetID) + `">` + html.EscapeString(value.Value) + "</span>"
		case *Table:
			columns := ""
			if value.ColSizes != nil {
				columns = "<colgroup>"
				for _, width := range *value.ColSizes {
					columns += `<col style="width:` + px(width) + `">`
				}
				columns += "</colgroup>"
			}
			return "<table" + attributes + ">" + columns + "<tbody>" + inner + "</tbody></table>"
		case *TableRow:
			return "<tr" + attributes + ">" + inner + "</tr>"
		case *TableCell:
			spans := ""
			if value.ColSpan != nil {
				spans += ` colspan="` + strconv.Itoa(int(*value.ColSpan)) + `"`
			}
			if value.RowSpan != nil {
				spans += ` rowspan="` + strconv.Itoa(int(*value.RowSpan)) + `"`
			}
			return "<" + element.Type + attributes + spans + ">" + inner + "</" + element.Type + ">"
		}
		return ""
	}
	return `<div style="white-space:pre-wrap">` + children(content) + "</div>"
}
func plain(nodes []Node) string {
	var b strings.Builder
	for _, node := range nodes {
		if text, ok := node.Value.(*Text); ok {
			b.WriteString(text.Text)
		} else if mention, ok := node.Value.(*Mention); ok {
			b.WriteString(mention.Value)
		} else if element := node.element(); element != nil {
			b.WriteString(plain(element.Children))
		}
	}
	return b.String()
}
func markdownEscape(text string) string {
	return strings.NewReplacer("\\", "\\\\", "*", "\\*", "_", "\\_", "[", "\\[", "]", "\\]", "<", "&lt;", ">", "&gt;", "|", "\\|").Replace(text)
}
func renderMarkdown(content []Node) string {
	numbers := listNumbers(content)
	var render func(Node) string
	var renderInline func([]Node) string
	renderInline = func(nodes []Node) string {
		var b strings.Builder
		for _, node := range nodes {
			if value, ok := node.Value.(*Text); ok {
				b.WriteString(markdownEscape(value.Text))
			} else if value, ok := node.Value.(*Mention); ok {
				b.WriteString(markdownEscape(value.Value))
			} else if value, ok := node.Value.(*Link); ok {
				b.WriteString("[" + renderInline(value.Children) + "](" + strings.NewReplacer("\\", "%5C", " ", "%20", "(", "%28", ")", "%29", "<", "%3C", ">", "%3E", "\"", "%22", "'", "%27", "[", "%5B", "]", "%5D", "`", "%60").Replace(value.URL) + ")")
			}
		}
		return b.String()
	}
	render = func(node Node) string {
		element := node.element()
		if element == nil {
			return ""
		}
		switch value := node.Value.(type) {
		case *Paragraph:
			prefix := ""
			if value.ListStyleType != nil {
				indent := 0
				if value.Indent != nil {
					indent = int(*value.Indent)
				}
				prefix = strings.Repeat("  ", indent)
				switch *value.ListStyleType {
				case "decimal":
					prefix += strconv.FormatInt(numbers[element.ID], 10) + ". "
				case "disc":
					prefix += "- "
				case "todo":
					prefix += "- [ ] "
					if value.Checked != nil && *value.Checked {
						prefix = strings.Repeat("  ", indent) + "- [x] "
					}
				}
			}
			return prefix + renderInline(element.Children)
		case *Heading:
			return strings.Repeat("#", headingRank(element.Type)) + " " + renderInline(element.Children)
		case *Blockquote:
			return "> " + renderInline(element.Children)
		case *HorizontalRule:
			return "---"
		case *CodeBlock:
			lines := []string{}
			for _, line := range element.Children {
				lines = append(lines, plain(line.element().Children))
			}
			text := strings.Join(lines, "\n")
			fence := "```"
			for strings.Contains(text, fence) {
				fence += "`"
			}
			language := ""
			if value.Lang != nil {
				language = *value.Lang
			}
			return fence + language + "\n" + text + "\n" + fence
		case *Table:
			rows := []string{}
			for index, row := range element.Children {
				cells := []string{}
				for _, cell := range row.element().Children {
					cells = append(cells, markdownEscape(plain(cell.element().Children)))
				}
				rows = append(rows, "| "+strings.Join(cells, " | ")+" |")
				if index == 0 {
					separators := make([]string, len(cells))
					for i := range separators {
						separators[i] = "---"
					}
					rows = append(rows, "| "+strings.Join(separators, " | ")+" |")
				}
			}
			return strings.Join(rows, "\n")
		}
		return plain(element.Children)
	}
	parts := []string{}
	for _, node := range content {
		parts = append(parts, render(node))
	}
	return strings.Join(parts, "\n\n")
}
func RenderExport(document Document, format string) (ExportOutput, error) {
	if e := contentFailure(ValidateContent(document.Content)); e != nil {
		return ExportOutput{}, e
	}
	out := ExportOutput{DocumentID: document.ID, Revision: document.Revision, Format: format, Losses: []string{}, RendererVersion: 1}
	switch format {
	case "json":
		raw, e := json.Marshal(struct {
			Format        string `json:"format"`
			SchemaVersion int32  `json:"schema_version"`
			Content       []Node `json:"content"`
		}{Format, SchemaVersion, document.Content})
		if e != nil {
			return out, e
		}
		out.Content = string(raw)
	case "html":
		out.Content = renderHTML(document.Content)
		out.Losses = []string{"canonical_json_required"}
	case "markdown":
		out.Content = renderMarkdown(document.Content)
		out.Losses = []string{"node_ids", "reference_metadata", "table_geometry", "styling", "exact_whitespace"}
	default:
		return out, contentFailure(field("format", "invalid"))
	}
	return out, nil
}
