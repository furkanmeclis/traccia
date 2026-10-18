package usecase

import (
	"encoding/json"
	"github.com/google/jsonschema-go/jsonschema"
	"sort"
)

type schemaObject = map[string]any

func schemaRef(name string) schemaObject { return schemaObject{"$ref": "#/$defs/" + name} }
func closedSchema(props schemaObject, required ...string) schemaObject {
	if required == nil {
		required = make([]string, 0, len(props))
		for key := range props {
			required = append(required, key)
		}
	}
	sort.Strings(required)
	return schemaObject{"type": "object", "additionalProperties": false, "properties": props, "required": required}
}
func arraySchema(item any, min, max int) schemaObject {
	return schemaObject{"type": "array", "items": item, "minItems": min, "maxItems": max}
}
func uuidSchema() schemaObject { return schemaObject{"type": "string", "format": "uuid"} }
func bounded(min, max float64) schemaObject {
	return schemaObject{"type": "number", "minimum": min, "maximum": max}
}
func integer(min, max int) schemaObject {
	return schemaObject{"type": "integer", "minimum": min, "maximum": max}
}
func colorSchema() schemaObject {
	return schemaObject{"type": "string", "pattern": `^(#[0-9a-fA-F]{3}|#[0-9a-fA-F]{4}|#[0-9a-fA-F]{6}|#[0-9a-fA-F]{8}|transparent|currentColor)$`}
}
func constant(value any) schemaObject { return schemaObject{"const": value} }
func plateDefinitions() schemaObject {
	textProps := schemaObject{"text": schemaObject{"type": "string"}}
	for _, mark := range []string{"bold", "italic", "underline", "strikethrough", "code"} {
		textProps[mark] = schemaObject{"type": "boolean"}
	}
	defs := schemaObject{"PlateText": closedSchema(textProps, "text"), "PlateEmptyText": closedSchema(schemaObject{"text": constant("")})}
	element := func(kind string, children any, extras schemaObject) schemaObject {
		props := schemaObject{"id": uuidSchema(), "type": schemaObject{"type": "string", "const": kind}, "children": children}
		for key, value := range extras {
			props[key] = value
		}
		return closedSchema(props, "id", "type", "children")
	}
	inline := arraySchema(schemaRef("PlateInline"), 1, MaxNodes)
	empty := arraySchema(schemaRef("PlateEmptyText"), 1, 1)
	p := element("p", inline, schemaObject{"indent": integer(0, 16), "listStyleType": schemaObject{"type": "string", "enum": []any{"decimal", "disc", "todo"}}, "checked": schemaObject{"type": "boolean"}, "listStart": integer(1, 1000000), "listRestart": integer(1, 1000000)})
	defs["PlateParagraph"] = p
	for _, kind := range []string{"h1", "h2", "h3", "blockquote"} {
		defs["Plate"+kind] = element(kind, inline, nil)
	}
	defs["PlateRule"] = element("hr", empty, nil)
	defs["PlateCode"] = element("code_block", arraySchema(schemaRef("PlateCodeLine"), 1, MaxNodes), schemaObject{"lang": schemaObject{"type": "string", "pattern": `^[A-Za-z0-9_+.-]{0,64}$`}})
	defs["PlateCodeLine"] = element("code_line", arraySchema(schemaRef("PlateText"), 1, MaxNodes), nil)
	link := element("a", arraySchema(schemaRef("PlateText"), 1, MaxNodes), schemaObject{"url": schemaObject{"type": "string", "maxLength": 2048}, "target": schemaObject{"type": "string", "enum": []any{"_blank", "_self"}}})
	link["required"] = []string{"id", "type", "children", "url"}
	defs["PlateLink"] = link
	mention := element("mention", empty, schemaObject{"key": schemaObject{"type": "string"}, "value": schemaObject{"type": "string", "maxLength": 255}, "target_kind": schemaObject{"type": "string", "enum": []any{"issue", "project", "user"}}, "target_id": uuidSchema()})
	mention["required"] = []string{"id", "type", "children", "key", "value", "target_kind", "target_id"}
	defs["PlateMention"] = mention
	defs["PlateTable"] = element("table", arraySchema(schemaRef("PlateRow"), 1, 100), schemaObject{"colSizes": arraySchema(bounded(0, 10000), 1, 100), "marginLeft": bounded(0, 10000)})
	defs["PlateRow"] = element("tr", arraySchema(schemaRef("PlateCell"), 0, 100), schemaObject{"size": bounded(0, 10000)})
	border := closedSchema(schemaObject{"color": colorSchema(), "style": schemaObject{"type": "string", "enum": []any{"dashed", "dotted", "none", "solid"}}, "size": bounded(0, 10000)})
	borderProps := schemaObject{}
	for _, side := range []string{"top", "right", "bottom", "left"} {
		borderProps[side] = border
	}
	extras := schemaObject{"colSpan": integer(1, 100), "rowSpan": integer(1, 100), "size": bounded(0, 10000), "background": colorSchema(), "borders": closedSchema(borderProps, []string{}...)}
	defs["PlateDataCell"] = element("td", arraySchema(schemaRef("PlateBlock"), 1, MaxNodes), extras)
	defs["PlateHeaderCell"] = element("th", arraySchema(schemaRef("PlateBlock"), 1, MaxNodes), extras)
	union := func(names ...string) schemaObject {
		variants := []any{}
		for _, name := range names {
			variants = append(variants, schemaRef(name))
		}
		return schemaObject{"oneOf": variants}
	}
	defs["PlateCell"] = union("PlateDataCell", "PlateHeaderCell")
	defs["PlateInline"] = union("PlateText", "PlateLink", "PlateMention")
	defs["PlateBlock"] = union("PlateParagraph", "Plateh1", "Plateh2", "Plateh3", "Plateblockquote", "PlateRule", "PlateCode", "PlateTable")
	defs["PlateElement"] = union("PlateParagraph", "Plateh1", "Plateh2", "Plateh3", "Plateblockquote", "PlateRule", "PlateCode", "PlateCodeLine", "PlateLink", "PlateMention", "PlateTable", "PlateRow", "PlateDataCell", "PlateHeaderCell")
	nullableUUID := schemaObject{"anyOf": []any{uuidSchema(), schemaObject{"type": "null"}}}
	patch := func(op string, fields schemaObject) schemaObject {
		props := schemaObject{"op": schemaObject{"type": "string", "const": op}}
		for key, value := range fields {
			props[key] = value
		}
		return closedSchema(props)
	}
	defs["PlatePatch"] = schemaObject{"oneOf": []any{
		patch("replace_node", schemaObject{"node_id": uuidSchema(), "node": schemaRef("PlateElement")}),
		patch("insert_nodes", schemaObject{"parent_id": nullableUUID, "before_id": nullableUUID, "nodes": arraySchema(schemaRef("PlateElement"), 1, MaxNodes)}),
		patch("remove_node", schemaObject{"node_id": uuidSchema()}),
		patch("move_node", schemaObject{"node_id": uuidSchema(), "parent_id": nullableUUID, "before_id": nullableUUID}),
		patch("replace_section", schemaObject{"heading_id": uuidSchema(), "expected_member_ids": arraySchema(uuidSchema(), 0, MaxNodes), "nodes": arraySchema(schemaRef("PlateBlock"), 0, MaxNodes)}),
	}}
	return defs
}
func assignSchema(value any) *jsonschema.Schema {
	raw, _ := json.Marshal(value)
	var schema jsonschema.Schema
	_ = json.Unmarshal(raw, &schema)
	return &schema
}
func refine(input, output *jsonschema.Schema) error {
	for _, root := range []*jsonschema.Schema{input, output} {
		if root.Defs == nil {
			root.Defs = map[string]*jsonschema.Schema{}
		}
		for name, value := range plateDefinitions() {
			root.Defs[name] = assignSchema(value)
		}
		var visit func(*jsonschema.Schema)
		visit = func(s *jsonschema.Schema) {
			if s == nil {
				return
			}
			for key, child := range s.Properties {
				switch key {
				case "id", "workspace_id", "owner_id", "document_id", "revision", "revision_id", "expected_revision", "actor_id", "node_id", "target_id":
					if key != "revision" || (child.Type != "object" && child.Ref == "") {
						s.Properties[key] = assignSchema(uuidSchema())
					}
				case "parent_revision":
					s.Properties[key] = assignSchema(schemaObject{"anyOf": []any{uuidSchema(), schemaObject{"type": "null"}}})
				case "content":
					if child.Type != "string" {
						s.Properties[key] = assignSchema(arraySchema(schemaRef("PlateBlock"), 1, MaxNodes))
					}
				case "patches":
					s.Properties[key] = assignSchema(arraySchema(schemaRef("PlatePatch"), 1, 100))
				case "kind":
					child.Enum = []any{"create", "replace", "patch", "restore"}
				case "owner_kind":
					child.Enum = []any{"issue", "project"}
				case "target_kind":
					child.Enum = []any{"issue", "project", "user"}
				case "limit":
					child.Minimum = floatNumber(1)
					child.Maximum = floatNumber(100)
				case "created_at", "updated_at", "occurred_at":
					child.Format = "date-time"
				}
				if key != "content" && key != "patches" {
					visit(s.Properties[key])
				}
			}
			if s.Properties["schema_version"] != nil {
				s.Properties["schema_version"] = assignSchema(schemaObject{"type": "integer", "const": SchemaVersion})
				s.Properties["format"] = assignSchema(schemaObject{"type": "string", "const": Format})
			}
			visit(s.Items)
			for _, child := range s.AnyOf {
				visit(child)
			}
			for _, child := range s.OneOf {
				visit(child)
			}
		}
		visit(root)
		// Reflection may intern repeated DTOs. Visit those definitions, but leave the
		// explicitly closed Plate graph untouched.
		for name, definition := range root.Defs {
			if len(name) < 5 || name[:5] != "Plate" {
				visit(definition)
			}
		}
	}
	return nil
}
func floatNumber(n float64) *float64 { return &n }
