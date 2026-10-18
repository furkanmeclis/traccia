package usecase

import (
	"encoding/json"
	"github.com/google/jsonschema-go/jsonschema"
	"slices"
	"sort"
	"strings"
)

func shape(v any) *jsonschema.Schema {
	b, _ := json.Marshal(v)
	var s jsonschema.Schema
	_ = json.Unmarshal(b, &s)
	return &s
}
func en(v []string) *jsonschema.Schema { return shape(map[string]any{"type": "string", "enum": v}) }
func integer(min, max int) *jsonschema.Schema {
	return shape(map[string]any{"type": "integer", "minimum": min, "maximum": max})
}
func str(max int) *jsonschema.Schema {
	return shape(map[string]any{"type": "string", "maxLength": max})
}
func object(p map[string]*jsonschema.Schema) *jsonschema.Schema {
	keys := make([]string, 0, len(p))
	for k := range p {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return &jsonschema.Schema{Type: "object", Properties: p, Required: keys, AdditionalProperties: shape(false)}
}
func arr(items *jsonschema.Schema, min, max int) *jsonschema.Schema {
	return shape(map[string]any{"type": "array", "items": items, "minItems": min, "maxItems": max})
}
func nullable(s *jsonschema.Schema) *jsonschema.Schema {
	return &jsonschema.Schema{AnyOf: []*jsonschema.Schema{s, shape(map[string]any{"type": "null"})}}
}
func uuidSchema() *jsonschema.Schema {
	return shape(map[string]any{"type": "string", "format": "uuid"})
}
func filterSchema(resource, name string) *jsonschema.Schema {
	ref := &jsonschema.Schema{Ref: "#/$defs/" + name}
	variants := []*jsonschema.Schema{}
	add := func(kind string, p map[string]*jsonschema.Schema) {
		p["kind"] = shape(map[string]any{"type": "string", "const": kind})
		variants = append(variants, object(p))
	}
	for _, kind := range []string{"all", "any"} {
		add(kind, map[string]*jsonschema.Schema{"children": arr(ref, 1, MaxNodes)})
	}
	add("not", map[string]*jsonschema.Schema{"child": ref})
	add("uuid", map[string]*jsonschema.Schema{"field": en(UUIDFields[resource]), "op": en([]string{"in", "not_in"}), "ids": arr(uuidSchema(), 1, MaxSet)})
	value := str(MaxText)
	value.MinLength = new(int)
	*value.MinLength = 1
	add("text", map[string]*jsonschema.Schema{"field": en(TextFields[resource]), "op": en([]string{"contains", "eq"}), "value": value})
	if resource == "issue" {
		add("enum", map[string]*jsonschema.Schema{"field": en([]string{"priority"}), "op": en([]string{"in", "not_in"}), "values": arr(en(Priorities), 1, MaxSet)})
		add("number", map[string]*jsonschema.Schema{"field": en([]string{"effort"}), "op": en([]string{"eq", "lt", "lte", "gt", "gte"}), "number": integer(-2147483648, 2147483647)})
	}
	if len(DateFields[resource]) > 0 {
		add("date", map[string]*jsonschema.Schema{"field": en(DateFields[resource]), "op": en([]string{"eq", "lt", "lte", "gt", "gte"}), "value": shape(map[string]any{"type": "string", "format": "date"})})
	}
	if len(NullFields[resource]) > 0 {
		add("null", map[string]*jsonschema.Schema{"field": en(NullFields[resource]), "op": en([]string{"is_null", "is_not_null"})})
	}
	return &jsonschema.Schema{OneOf: variants}
}
func refine(resource string) func(*jsonschema.Schema, *jsonschema.Schema) error {
	return func(in, out *jsonschema.Schema) error {
		for _, root := range []*jsonschema.Schema{in, out} {
			if root.Defs == nil {
				root.Defs = map[string]*jsonschema.Schema{}
			}
			delete(root.Defs, "Filter")
			for _, r := range Resources {
				name := "Search" + strings.ToUpper(r[:1]) + r[1:] + "Filter"
				root.Defs[name] = filterSchema(r, name)
			}
			var walk func(*jsonschema.Schema, string)
			walk = func(s *jsonschema.Schema, r string) {
				if s == nil {
					return
				}
				view := s.Properties["resource"] != nil && s.Properties["issue"] != nil && s.Properties["project"] != nil && s.Properties["team"] != nil
				if view {
					variants := []*jsonschema.Schema{}
					for _, selected := range Resources {
						p := map[string]*jsonschema.Schema{"resource": shape(map[string]any{"type": "string", "const": selected})}
						for _, k := range Resources {
							p[k] = shape(map[string]any{"type": "null"})
							if k == selected {
								v := s.Properties[k].CloneSchemas()
								if len(v.AnyOf) > 0 {
									for _, b := range v.AnyOf {
										if b.Type != "null" {
											v = b
											break
										}
									}
								}
								v.Types = nil
								v.Type = "object"
								walk(v, k)
								p[k] = v
							}
						}
						variants = append(variants, object(p))
					}
					*s = jsonschema.Schema{OneOf: variants}
					return
				}
				if s.Properties["filter"] != nil && s.Properties["sort"] != nil && s.Properties["search"] != nil {
					s.Properties["version"] = shape(map[string]any{"type": "integer", "const": 1})
					s.Properties["archived"] = en([]string{"active", "archived", "any"})
					s.Properties["search"] = str(MaxText)
					name := "Search" + strings.ToUpper(r[:1]) + r[1:] + "Filter"
					s.Properties["filter"] = nullable(&jsonschema.Schema{Ref: "#/$defs/" + name})
					s.Properties["sort"] = arr(object(map[string]*jsonschema.Schema{"field": en(SortFields[r]), "direction": en([]string{"asc", "desc"}), "nulls": en([]string{"first", "last"})}), 0, MaxSorts)
					return
				}
				for k, c := range s.Properties {
					switch {
					case k == "id" || strings.HasSuffix(k, "_id") || k == "revision" || k == "expected_revision":
						if c.Type != "object" && c.Ref == "" {
							if len(c.AnyOf) > 0 || len(c.Types) > 0 {
								s.Properties[k] = nullable(uuidSchema())
							} else {
								s.Properties[k] = uuidSchema()
							}
						}
					case k == "version" || k == "workflow_version" || k == "number":
						if c.Type == "string" {
							s.Properties[k] = shape(map[string]any{"type": "string", "pattern": "^[1-9][0-9]*$"})
						}
					case k == "total_count":
						s.Properties[k] = shape(map[string]any{"type": "string", "pattern": "^(0|[1-9][0-9]*)$"})
					case k == "resource":
						if resource != "" && s.Properties["as_of"] != nil {
							s.Properties[k] = shape(map[string]any{"type": "string", "const": resource})
						} else {
							s.Properties[k] = en(Resources)
						}
					case k == "default_ttl_seconds":
						s.Properties[k] = integer(MinTTL, MaxTTL)
					case k == "min_ttl_seconds" || k == "max_ttl_seconds" || k == "max_depth" || k == "max_nodes" || k == "max_sorts" || k == "max_page" || k == "max_set" || k == "max_text" || k == "max_input_bytes" || k == "max_raw_input_bytes":
						values := map[string]int{"min_ttl_seconds": MinTTL, "max_ttl_seconds": MaxTTL, "max_depth": MaxDepth, "max_nodes": MaxNodes, "max_sorts": MaxSorts, "max_page": MaxPage, "max_set": MaxSet, "max_text": MaxText, "max_input_bytes": 128 * 1024, "max_raw_input_bytes": 1 << 20}
						s.Properties[k] = shape(map[string]any{"type": "integer", "const": values[k]})
					case k == "input_byte_policy":
						s.Properties[k] = shape(map[string]any{"type": "string", "const": "json_stringify_utf8_domain"})

					case k == "priority":
						s.Properties[k] = en(Priorities)
					case k == "items" || k == "columns" || k == "label_ids" || k == "team_ids":
						c.Type = "array"
						c.Types = nil
						c.AnyOf = nil
						if k == "label_ids" || k == "team_ids" {
							c.Items = uuidSchema()
							c.UniqueItems = true
						}
						if k == "label_ids" {
							c.MaxItems = new(int)
							*c.MaxItems = 1000
						}
						if k == "items" {
							c.MaxItems = new(int)
							*c.MaxItems = MaxPage
						}
						if k == "columns" {
							c.MinItems = new(int)
							*c.MinItems = 0
							c.MaxItems = new(int)
							*c.MaxItems = 30
						}
					case k == "name":
						max := 100
						if s.Properties["definition"] != nil {
							max = 120
						}
						s.Properties[k] = str(max)
						s.Properties[k].MinLength = new(int)
						*s.Properties[k].MinLength = 1
					case k == "title":
						s.Properties[k] = str(255)
						s.Properties[k].MinLength = new(int)
						*s.Properties[k].MinLength = 1
					case k == "summary":
						s.Properties[k] = str(2000)
					case k == "code":
						s.Properties[k] = shape(map[string]any{"type": "string", "pattern": "^[A-Za-z][A-Za-z0-9]{1,9}$"})
					case k == "identifier":
						s.Properties[k] = shape(map[string]any{"type": "string", "pattern": "^[A-Za-z][A-Za-z0-9]{1,9}-[1-9][0-9]*$"})
					case k == "start_date" || k == "target_date":
						s.Properties[k] = nullable(shape(map[string]any{"type": "string", "format": "date"}))
					case k == "effort":
						s.Properties[k] = nullable(integer(0, 1000))
					case k == "created_at" || k == "updated_at" || k == "as_of" || k == "expires_at":
						s.Properties[k] = shape(map[string]any{"type": "string", "format": "date-time"})
					case k == "limit":
						s.Properties[k] = integer(1, MaxPage)
					case k == "ttl_seconds":
						s.Properties[k] = nullable(integer(MinTTL, MaxTTL))
					case k == "after" || k == "next_cursor":
						s.Properties[k] = str(2048)
					case k == "width":
						s.Properties[k] = integer(80, 800)
					case k == "key":
						keys := []string{}
						for _, res := range Resources {
							for _, key := range ColumnFields[res] {
								if !slices.Contains(keys, key) {
									keys = append(keys, key)
								}
							}
						}
						s.Properties[k] = en(keys)
					}
					walk(s.Properties[k], r)
				}

				if s.Properties["columns"] != nil && (s.Properties["resource"] != nil || s.Properties["definition"] != nil) {
					branches := []*jsonschema.Schema{}
					for i, res := range Resources {
						props := map[string]*jsonschema.Schema{}
						for key, v := range s.Properties {
							props[key] = v.CloneSchemas()
						}
						min := 1
						if s.Properties["expected_revision"] == nil && s.Properties["definition"] == nil {
							min = 0
						}
						props["columns"] = arr(object(map[string]*jsonschema.Schema{"key": en(ColumnFields[res]), "width": integer(80, 800)}), min, len(ColumnFields[res]))
						if props["resource"] != nil {
							props["resource"] = shape(map[string]any{"type": "string", "const": res})
						} else {
							props["definition"] = s.Properties["definition"].OneOf[i].CloneSchemas()
						}
						branch := object(props)
						branch.Required = append([]string(nil), s.Required...)
						branches = append(branches, branch)
					}
					s.OneOf = branches
					return
				}
				walk(s.Items, r)
				for _, v := range s.AnyOf {
					walk(v, r)
				}
				for _, v := range s.OneOf {
					walk(v, r)
				}
			}
			chosen := resource
			if chosen == "" {
				chosen = "issue"
			}
			walk(root, chosen)
			for name, def := range root.Defs {
				if !strings.HasPrefix(name, "Search") {
					walk(def, chosen)
				}
			}
		}
		return nil
	}
}
