package usecase

import (
	"encoding/json"
	search "github.com/furkanmeclis/traccia/backend/internal/modules/searchviews/usecase"
	"github.com/google/jsonschema-go/jsonschema"
	"sort"
	"strings"
)

func shape(v any) *jsonschema.Schema {
	b, _ := json.Marshal(v)
	var s jsonschema.Schema
	_ = json.Unmarshal(b, &s)
	return &s
}
func object(p map[string]*jsonschema.Schema) *jsonschema.Schema {
	keys := make([]string, 0, len(p))
	for k := range p {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return &jsonschema.Schema{Type: "object", Properties: p, Required: keys, AdditionalProperties: shape(false)}
}
func enum(v ...string) *jsonschema.Schema { return shape(map[string]any{"type": "string", "enum": v}) }
func idSchema() *jsonschema.Schema        { return shape(map[string]any{"type": "string", "format": "uuid"}) }
func nullable(s *jsonschema.Schema) *jsonschema.Schema {
	return &jsonschema.Schema{AnyOf: []*jsonschema.Schema{s, shape(map[string]any{"type": "null"})}}
}
func mergeDefinitions(root, s *jsonschema.Schema) {
	if root.Defs == nil {
		root.Defs = map[string]*jsonschema.Schema{}
	}
	for k, v := range s.Defs {
		root.Defs[k] = v
	}
	s.Defs = nil
}
func refine(in, out *jsonschema.Schema) error {
	for _, root := range []*jsonschema.Schema{in, out} {
		var walk func(*jsonschema.Schema) error
		walk = func(s *jsonschema.Schema) error {
			if s == nil {
				return nil
			}
			for k, c := range s.Properties {
				switch {
				case k == "definition":
					v, e := search.DefinitionSchema()
					if e != nil {
						return e
					}
					mergeDefinitions(root, v)
					s.Properties[k] = v
					continue
				case k == "resource":
					s.Properties[k] = enum("issue", "project", "team")
				case k == "kind":
					s.Properties[k] = enum(TargetKinds...)
				case k == "id" || strings.HasSuffix(k, "_id") || k == "revision" || k == "expected_revision":
					if c.Ref == "" && c.Type != "object" {
						if len(c.AnyOf) > 0 || len(c.Types) > 0 {
							s.Properties[k] = nullable(idSchema())
						} else {
							s.Properties[k] = idSchema()
						}
					}
				case k == "workspace_ids":
					s.Properties[k] = shape(map[string]any{"type": "array", "items": idSchema(), "minItems": 1, "maxItems": MaxWorkspaces, "uniqueItems": true})
				case k == "total_count":
					s.Properties[k] = shape(map[string]any{"type": "string", "pattern": "^(0|[1-9][0-9]*)$"})
				case k == "limit":
					s.Properties[k] = shape(map[string]any{"type": "integer", "minimum": 1, "maximum": 100})
				case k == "ttl_seconds":
					s.Properties[k] = nullable(shape(map[string]any{"type": "integer", "minimum": search.MinTTL, "maximum": search.MaxTTL}))
				case k == "next_cursor" || k == "after":
					s.Properties[k] = shape(map[string]any{"type": "string", "maxLength": 2048})
				case k == "label" || k == "name":
					s.Properties[k] = shape(map[string]any{"type": "string", "minLength": 1, "maxLength": 255})
				case k == "identifier":
					s.Properties[k] = nullable(shape(map[string]any{"type": "string", "maxLength": 100}))
				case k == "workspaces":
					s.Properties[k] = shape(map[string]any{"type": "array", "items": c.Items, "minItems": 1, "maxItems": MaxWorkspaces})
				}
				if e := walk(s.Properties[k]); e != nil {
					return e
				}
			}
			for _, v := range s.AnyOf {
				if e := walk(v); e != nil {
					return e
				}
			}
			if e := walk(s.Items); e != nil {
				return e
			}
			return nil
		}
		if e := walk(root); e != nil {
			return e
		}
		if items := root.Properties["items"]; items != nil {
			items.Types = nil
			items.Type = "array"
			items.MaxItems = new(int)
			*items.MaxItems = 100
			if items.Items != nil && items.Items.Properties["issue"] != nil {
				variants := []*jsonschema.Schema{}
				for _, r := range search.Resources {
					p := map[string]*jsonschema.Schema{"workspace_id": idSchema(), "resource": shape(map[string]any{"type": "string", "const": r})}
					for _, k := range search.Resources {
						p[k] = shape(map[string]any{"type": "null"})
						if k == r {
							v, e := search.SnapshotRowSchema(r)
							if e != nil {
								return e
							}
							mergeDefinitions(root, v)
							p[k] = v
						}
					}
					variants = append(variants, object(p))
				}
				items.Items = &jsonschema.Schema{OneOf: variants}
			}
		}
		if limits := root.Properties["limits"]; limits != nil {
			values := map[string]int{"max_workspaces": MaxWorkspaces, "max_page": 100, "min_ttl_seconds": search.MinTTL, "max_ttl_seconds": search.MaxTTL, "max_raw_input_bytes": 1 << 20, "max_input_bytes": 128 << 10}
			for k, v := range values {
				limits.Properties[k] = shape(map[string]any{"type": "integer", "const": v})
			}
			limits.Properties["default_ttl_seconds"] = shape(map[string]any{"type": "integer", "minimum": search.MinTTL, "maximum": search.MaxTTL})
		}
		for k, v := range map[string]string{"input_byte_policy": "json_stringify_utf8_domain", "text_matching": "unicode_default_casefold_literal_no_normalization", "text_ordering": "C_byte_order_uuid_tie"} {
			if root.Properties[k] != nil {
				root.Properties[k] = shape(map[string]any{"type": "string", "const": v})
			}
		}
	}
	return nil
}
