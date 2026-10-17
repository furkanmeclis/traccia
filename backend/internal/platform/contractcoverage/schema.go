// Package contractcoverage compares observed transport bindings with application
// contracts. It never treats a declared transport name as a registered endpoint.
package contractcoverage

import (
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"sort"
	"strings"

	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
)

type Object = map[string]any

func Decode(raw []byte) (Object, error) {
	value, err := operations.DecodeJSON(raw)
	if err != nil {
		return nil, err
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("expected JSON object")
	}
	return object, nil
}

// Normalize resolves local JSON pointers from the document in which a schema
// occurs. Only presentation annotations are ignored; validation keywords remain.
func Normalize(schema any, document Object) (any, error) {
	return normalizeAt(schema, document, operations.SchemaBaseURI)
}

func normalizeAt(schema any, document Object, base string) (any, error) {
	resolver, err := newSchemaResolver(document, base)
	if err != nil {
		return nil, err
	}
	return resolver.normalize(schema, map[string]bool{})
}

func (r *schemaResolver) normalize(value any, active map[string]bool) (any, error) {
	schema, ok := value.(Object)
	if !ok {
		return value, nil
	}
	if ref, ok := schema["$ref"].(string); ok {
		target, reference, err := r.resolve(schema, ref)
		if err != nil {
			return nil, err
		}
		if active[reference] {
			return nil, fmt.Errorf("recursive schema reference %s requires explicit coverage", ref)
		}
		active[reference] = true
		resolved, err := r.normalize(target, active)
		delete(active, reference)
		if err != nil {
			return nil, err
		}
		siblings := Object{}
		for key, child := range schema {
			if key != "$ref" {
				siblings[key] = child
			}
		}
		if len(siblings) == 0 {
			return resolved, nil
		}
		r.contexts[fmt.Sprintf("%p", siblings)] = r.contexts[fmt.Sprintf("%p", schema)]
		normalized, err := r.normalize(siblings, active)
		if err != nil {
			return nil, err
		}
		base, ok := resolved.(Object)
		if !ok {
			return nil, fmt.Errorf("non-object schema reference with siblings: %s", ref)
		}
		for key, child := range normalized.(Object) {
			if old, exists := base[key]; exists && !reflect.DeepEqual(old, child) {
				return nil, fmt.Errorf("conflicting reference sibling %s", key)
			}
			base[key] = child
		}
		return base, nil
	}
	if len(schema) == 1 {
		if child, ok := schema["not"].(Object); ok && len(child) == 0 {
			return false, nil
		}
	}
	out := Object{}
	for key, child := range schema {
		switch key {
		case "$schema", "$id", "$anchor", "$defs", "definitions", "description", "title", "examples":
			continue
		}
		normalized := child
		var err error
		switch key {
		case "properties", "patternProperties", "dependentSchemas":
			if fields, ok := child.(Object); ok {
				result := Object{}
				for name, field := range fields {
					result[name], err = r.normalize(field, active)
					if err != nil {
						return nil, err
					}
				}
				normalized = result
			}
		case "items", "contains", "additionalProperties", "unevaluatedProperties", "propertyNames", "not", "if", "then", "else", "additionalItems", "contentSchema":
			normalized, err = r.normalize(child, active)
		case "anyOf", "oneOf", "allOf", "prefixItems":
			if variants, ok := child.([]any); ok {
				result := make([]any, len(variants))
				for i, variant := range variants {
					result[i], err = r.normalize(variant, active)
					if err != nil {
						return nil, err
					}
				}
				normalized = result
			}
		case "required", "enum", "type":
			if items, ok := child.([]any); ok {
				result := append([]any(nil), items...)
				sort.Slice(result, func(i, j int) bool {
					a, _ := json.Marshal(result[i])
					b, _ := json.Marshal(result[j])
					return string(a) < string(b)
				})
				normalized = result
			}
		}
		if err != nil {
			return nil, err
		}
		out[key] = normalized
	}
	return out, nil
}

func Pointer(document Object, ref string) (any, error) {
	if ref == "#" {
		return document, nil
	}
	if !strings.HasPrefix(ref, "#/") {
		return nil, fmt.Errorf("external or invalid schema reference %s", ref)
	}
	var current any = document
	for _, segment := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
		segment = strings.ReplaceAll(strings.ReplaceAll(segment, "~1", "/"), "~0", "~")
		object, ok := current.(Object)
		if !ok {
			return nil, fmt.Errorf("invalid schema reference %s", ref)
		}
		current, ok = object[segment]
		if !ok {
			return nil, fmt.Errorf("unresolved schema reference %s", ref)
		}
	}
	return current, nil
}

// Difference returns a useful first differing field, including nested fields.
func Difference(expected, actual any, path string) string {
	if reflect.DeepEqual(expected, actual) {
		return ""
	}
	a, aok := expected.(Object)
	b, bok := actual.(Object)
	if aok && bok {
		keys := make([]string, 0, len(a)+len(b))
		seen := map[string]bool{}
		for k := range a {
			seen[k] = true
			keys = append(keys, k)
		}
		for k := range b {
			if !seen[k] {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		for _, key := range keys {
			av, ap := a[key]
			bv, bp := b[key]
			if !ap || !bp {
				return path + "/" + key
			}
			if d := Difference(av, bv, path+"/"+key); d != "" {
				return d
			}
		}
	}
	av, aok := expected.([]any)
	bv, bok := actual.([]any)
	if aok && bok && len(av) == len(bv) {
		for i := range av {
			if d := Difference(av[i], bv[i], fmt.Sprintf("%s/%d", path, i)); d != "" {
				return d
			}
		}
	}
	return path
}

// Closed rejects opaque schemas recursively, including array items and composed
// object alternatives. Empty domain DTOs remain valid closed object schemas.
func Closed(value any, path string) error {
	schema, ok := value.(Object)
	if !ok {
		if value == false {
			return nil
		}
		return fmt.Errorf("%s: opaque schema", path)
	}
	if len(schema) == 0 {
		return fmt.Errorf("%s: opaque schema", path)
	}
	if hasType(schema, "object") || schema["properties"] != nil {
		if schema["additionalProperties"] != false {
			return fmt.Errorf("%s/additionalProperties: closed object required", path)
		}
		if props, ok := schema["properties"].(Object); ok {
			for name, child := range props {
				if err := Closed(child, path+"/properties/"+name); err != nil {
					return err
				}
			}
		}
	}
	if props, ok := schema["properties"].(Object); ok && schema["type"] != "object" {
		for name, child := range props {
			if err := Closed(child, path+"/properties/"+name); err != nil {
				return err
			}
		}
	}
	if hasType(schema, "array") {
		if schema["items"] == nil {
			return fmt.Errorf("%s/items: concrete items required", path)
		}
		if err := Closed(schema["items"], path+"/items"); err != nil {
			return err
		}
	}
	for _, key := range []string{"anyOf", "oneOf", "allOf"} {
		if variants, ok := schema[key].([]any); ok {
			for i, child := range variants {
				if err := Closed(child, fmt.Sprintf("%s/%s/%d", path, key, i)); err != nil {
					return err
				}
			}
		}
	}
	if schema["type"] == nil && schema["const"] == nil && schema["enum"] == nil && schema["anyOf"] == nil && schema["oneOf"] == nil && schema["allOf"] == nil {
		return fmt.Errorf("%s: concrete schema required", path)
	}
	return nil
}

type schemaResolver struct {
	contexts  map[string]*url.URL
	resources map[string]Object
	anchors   map[string]Object
}

func newSchemaResolver(document Object, base string) (*schemaResolver, error) {
	uri, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	r := &schemaResolver{contexts: map[string]*url.URL{}, resources: map[string]Object{base: document}, anchors: map[string]Object{}}
	if err := r.index(document, uri); err != nil {
		return nil, err
	}
	return r, nil
}
func (r *schemaResolver) index(value any, base *url.URL) error {
	switch node := value.(type) {
	case Object:
		if id, ok := node["$id"].(string); ok {
			relative, err := url.Parse(id)
			if err != nil {
				return err
			}
			base = base.ResolveReference(relative)
			r.resources[withoutFragment(base)] = node
		}
		r.contexts[fmt.Sprintf("%p", node)] = base
		if anchor, ok := node["$anchor"].(string); ok {
			r.anchors[withoutFragment(base)+"#"+anchor] = node
		}
		for key, child := range node {
			switch key {
			case "enum", "const", "default", "examples":
				continue
			case "properties", "patternProperties", "$defs", "definitions", "dependentSchemas":
				if fields, ok := child.(Object); ok {
					for _, field := range fields {
						if err := r.index(field, base); err != nil {
							return err
						}
					}
				}
				continue
			}
			if err := r.index(child, base); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range node {
			if err := r.index(child, base); err != nil {
				return err
			}
		}
	}
	return nil
}
func withoutFragment(uri *url.URL) string {
	copy := *uri
	copy.Fragment = ""
	copy.RawFragment = ""
	return copy.String()
}
func (r *schemaResolver) resolve(schema Object, ref string) (any, string, error) {
	base := r.contexts[fmt.Sprintf("%p", schema)]
	if base == nil {
		return nil, "", fmt.Errorf("unindexed schema reference %s", ref)
	}
	relative, err := url.Parse(ref)
	if err != nil {
		return nil, "", err
	}
	absolute := base.ResolveReference(relative)
	reference := absolute.String()
	root, ok := r.resources[withoutFragment(absolute)]
	if !ok {
		return nil, "", fmt.Errorf("external or unresolved schema resource %s", reference)
	}
	if absolute.Fragment == "" {
		return root, reference, nil
	}
	if !strings.HasPrefix(absolute.Fragment, "/") {
		target, ok := r.anchors[reference]
		if !ok {
			return nil, "", fmt.Errorf("unresolved schema anchor %s", reference)
		}
		return target, reference, nil
	}
	target, err := Pointer(root, "#"+absolute.Fragment)
	return target, reference, err
}

func hasType(schema Object, kind string) bool {
	if schema["type"] == kind {
		return true
	}
	if variants, ok := schema["type"].([]any); ok {
		for _, variant := range variants {
			if variant == kind {
				return true
			}
		}
	}
	return false
}
