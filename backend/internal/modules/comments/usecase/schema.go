package usecase

import (
	"encoding/json"
	plate "github.com/furkanmeclis/traccia/backend/internal/modules/documents/usecase"
	issue "github.com/furkanmeclis/traccia/backend/internal/modules/issues/usecase"
	"github.com/google/jsonschema-go/jsonschema"
	"slices"
	"strings"
)

func shaped(v any) *jsonschema.Schema {
	raw, _ := json.Marshal(v)
	var s jsonschema.Schema
	_ = json.Unmarshal(raw, &s)
	return &s
}
func num(n float64) *float64 { return &n }
func size(n int) *int        { return &n }
func refine(input, output *jsonschema.Schema) error {
	for _, root := range []*jsonschema.Schema{input, output} {
		plate.RefinePlateContent(root)
		var refineError error
		var visit func(*jsonschema.Schema)
		visit = func(s *jsonschema.Schema) {
			if s == nil {
				return
			}
			for key, c := range s.Properties {
				switch {
				case key == "id" || strings.HasSuffix(key, "_id") || key == "revision" || key == "expected_revision" || key == "file_revision":
					if c.Type == "object" || c.Ref != "" {
						break
					}
					if len(c.AnyOf) > 0 || slices.Contains(c.Types, "null") {
						s.Properties[key] = shaped(map[string]any{"anyOf": []any{map[string]any{"type": "string", "format": "uuid"}, map[string]any{"type": "null"}}})
					} else {
						s.Properties[key] = shaped(map[string]any{"type": "string", "format": "uuid"})
					}
				case key == "parent_revision" || key == "source_revision":
					s.Properties[key] = shaped(map[string]any{"anyOf": []any{map[string]any{"type": "string", "format": "uuid"}, map[string]any{"type": "null"}}})
				case key == "created_channel" || key == "channel":
					c.Enum = []any{"api", "mcp", "panel", "unknown"}
				case key == "kind":
					switch {
					case s.Properties["source"] != nil && s.Properties["target"] != nil:
						c.Enum = []any{"parent", "duplicate", "blocks", "related"}
					case s.Properties["document_id"] != nil:
						c.Enum = []any{"create", "replace", "patch", "restore"}
					case s.Properties["comment_id"] != nil:
						c.Enum = []any{"create", "replace", "patch", "delete", "restore"}
					}
				case key == "target_kind":
					c.Enum = []any{"issue", "project", "user"}
				case key == "limit":
					c.Minimum = num(1)
					c.Maximum = num(100)
				case key == "after":
					c.MaxLength = size(2048)
				case key == "items" || key == "changed_relations" || key == "affected_issues" || key == "label_ids" || key == "losses":
					c.Type = "array"
					c.Types = nil
					if key == "items" {
						c.MaxItems = size(100)
					}
				case key == "created_at" || key == "updated_at" || key == "occurred_at":
					c.Format = "date-time"
				case key == "resource_type":
					c.MaxLength = size(128)
				case key == "event_name":
					c.MaxLength = size(255)
				case key == "name":
					c.MaxLength = size(1024)
				}
				if key != "content" && key != "patches" {
					visit(s.Properties[key])
				}
			}
			visit(s.Items)
			for _, c := range s.AnyOf {
				visit(c)
			}
			for _, c := range s.OneOf {
				visit(c)
			}
			if s.Properties["priority"] != nil && s.Properties["status_id"] != nil && s.Properties["title"] != nil {
				if e := issue.RefineCreateSchema(s); e != nil {
					refineError = e
					return
				}
				s.Properties["label_ids"].Type = "array"
				s.Properties["label_ids"].Types = nil
			}
			if s.Properties["issue"] != nil && s.Properties["legacy"] != nil && s.Properties["kind"] != nil {
				variants := []*jsonschema.Schema{}
				for _, kind := range []string{"issue", "document", "comment", "relation", "attachment", "legacy"} {
					props := map[string]*jsonschema.Schema{"kind": shaped(map[string]any{"type": "string", "const": kind})}
					for _, name := range []string{"issue", "document", "comment", "relation", "attachment", "legacy"} {
						props[name] = shaped(map[string]any{"type": "null"})
						if name == kind {
							v := s.Properties[name]
							if len(v.AnyOf) > 0 {
								for _, x := range v.AnyOf {
									if x.Type != "null" {
										v = x
										break
									}
								}
							} else if len(v.Types) > 0 {
								raw, _ := json.Marshal(v)
								_ = json.Unmarshal(raw, &v)
								v.Types = nil
								v.Type = "object"
							}
							props[name] = v
						}
					}
					variants = append(variants, &jsonschema.Schema{Type: "object", Properties: props, AdditionalProperties: shaped(false), Required: []string{"kind", "issue", "document", "comment", "relation", "attachment", "legacy"}})
				}
				*s = jsonschema.Schema{OneOf: variants}
			}
		}
		visit(root)
		for name, def := range root.Defs {
			if !strings.HasPrefix(name, "Plate") {
				visit(def)
			}
		}
		if refineError != nil {
			return refineError
		}
	}
	return nil
}
