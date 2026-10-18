// Package contractcoverage compares observed transport bindings with application
// contracts. It never treats a declared transport name as a registered endpoint.
package contractcoverage

import (
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"sort"
	"strconv"
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
	n := &normalizer{resolver: resolver, names: map[string]string{}, completed: map[string]any{}}
	value, err := n.normalize(schema, map[string]int{}, 0)
	if err != nil {
		return nil, err
	}
	if len(n.names) == 0 {
		return value, nil
	}
	root, ok := value.(Object)
	if !ok {
		return nil, fmt.Errorf("recursive root must be an object schema")
	}
	copyRoot := Object{}
	for key, child := range root {
		copyRoot[key] = child
	}
	root = copyRoot
	defs := Object{}
	for ref, name := range n.names {
		target, exists := n.completed[ref]
		if !exists {
			return nil, fmt.Errorf("unresolved recursive schema %s", ref)
		}
		defs[name] = target
	}
	root["$defs"] = defs
	root["$id"] = fmt.Sprintf("urn:traccia:coverage:%p:%p", document, root)
	return root, nil
}

type normalizer struct {
	resolver  *schemaResolver
	names     map[string]string
	completed map[string]any
}

func (n *normalizer) normalize(value any, active map[string]int, depth int) (any, error) {
	schema, ok := value.(Object)
	if !ok {
		return value, nil
	}
	if _, ok = schema["$dynamicRef"]; ok {
		return nil, fmt.Errorf("dynamic references are unsupported")
	}
	if _, ok = schema["$dynamicAnchor"]; ok {
		return nil, fmt.Errorf("dynamic anchors are unsupported")
	}
	if ref, ok := schema["$ref"].(string); ok {
		target, reference, err := n.resolver.resolve(schema, ref)
		if err != nil {
			return nil, err
		}
		var resolved any
		if at, onStack := active[reference]; onStack {
			if depth <= at {
				return nil, fmt.Errorf("unguarded recursive schema %s", ref)
			}
			name, exists := n.names[reference]
			if !exists {
				name = fmt.Sprintf("coverage%d", len(n.names))
				n.names[reference] = name
			}
			resolved = Object{"$ref": "#/$defs/" + name}
		} else {
			active[reference] = depth
			resolved, err = n.normalize(target, active, depth)
			delete(active, reference)
			if err != nil {
				return nil, err
			}
			n.completed[reference] = resolved
		}
		siblings := Object{}
		for key, child := range schema {
			if key != "$ref" {
				siblings[key] = child
			}
		}
		n.resolver.contexts[fmt.Sprintf("%p", siblings)] = n.resolver.contexts[fmt.Sprintf("%p", schema)]
		extra, err := n.normalize(siblings, active, depth)
		if err != nil {
			return nil, err
		}
		constraints, object := extra.(Object)
		if !object {
			return Object{"allOf": []any{resolved, extra}}, nil
		}
		if len(constraints) == 0 {
			return resolved, nil
		}
		if base, ok := resolved.(Object); ok {
			conflict := false
			for key, child := range constraints {
				if old, exists := base[key]; exists && !reflect.DeepEqual(old, child) {
					conflict = true
				}
			}
			if !conflict && base["$ref"] == nil {
				merged := Object{}
				for key, child := range base {
					merged[key] = child
				}
				for key, child := range constraints {
					merged[key] = child
				}
				return merged, nil
			}
		}
		return Object{"allOf": []any{resolved, constraints}}, nil
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
					next := depth
					if key != "dependentSchemas" {
						next++
					}
					result[name], err = n.normalize(field, active, next)
					if err != nil {
						return nil, err
					}
				}
				normalized = result
			}
		case "items", "contains", "additionalProperties", "unevaluatedProperties", "propertyNames", "not", "if", "then", "else", "additionalItems", "contentSchema":
			next := depth
			if key == "items" || key == "contains" || key == "additionalProperties" || key == "unevaluatedProperties" || key == "propertyNames" || key == "additionalItems" {
				next++
			}
			normalized, err = n.normalize(child, active, next)
		case "anyOf", "oneOf", "allOf", "prefixItems":
			if variants, ok := child.([]any); ok {
				result := make([]any, len(variants))
				for i, variant := range variants {
					next := depth
					if key == "prefixItems" {
						next++
					}
					result[i], err = n.normalize(variant, active, next)
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
		switch node := current.(type) {
		case Object:
			var exists bool
			current, exists = node[segment]
			if !exists {
				return nil, fmt.Errorf("unresolved schema reference %s", ref)
			}
		case []any:
			index, err := strconv.Atoi(segment)
			if err != nil || index < 0 || index >= len(node) || strconv.Itoa(index) != segment {
				return nil, fmt.Errorf("invalid schema reference %s", ref)
			}
			current = node[index]
		default:
			return nil, fmt.Errorf("invalid schema reference %s", ref)
		}
	}
	return current, nil
}

// Difference returns a useful first differing field, including nested fields.
func Difference(expected, actual any, path string) string {
	if schemaReferences(expected) || schemaReferences(actual) {
		return graphDifference(expected, actual, path)
	}
	return treeDifference(expected, actual, path)
}
func treeDifference(expected, actual any, path string) string {
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
			if d := treeDifference(av, bv, path+"/"+key); d != "" {
				return d
			}
		}
	}
	av, aok := expected.([]any)
	bv, bok := actual.([]any)
	if aok && bok && len(av) == len(bv) {
		for i := range av {
			if d := treeDifference(av[i], bv[i], fmt.Sprintf("%s/%d", path, i)); d != "" {
				return d
			}
		}
	}
	return path
}

// Closed rejects opaque schemas recursively, including array items and composed
// object alternatives. Empty domain DTOs remain valid closed object schemas.
func Closed(value any, path string) error { return closedGraph(value, path) }

func closedTree(value any, path string) error {
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
	if document, ok := value.(Object); ok && (document["openapi"] != nil || (document["components"] != nil && document["paths"] != nil)) {
		return r.indexDocument(document, base)
	}
	return r.indexSchema(value, base)
}
func (r *schemaResolver) indexDocument(value any, base *url.URL) error {
	switch node := value.(type) {
	case Object:
		r.contexts[fmt.Sprintf("%p", node)] = base
		for key, child := range node {
			switch key {
			case "enum", "const", "default", "examples", "example":
				continue
			case "schema", "x-traccia-input-schema", "x-traccia-error-schema":
				if err := r.indexSchema(child, base); err != nil {
					return err
				}
			case "schemas":
				if fields, ok := child.(Object); ok {
					for _, field := range fields {
						if err := r.indexSchema(field, base); err != nil {
							return err
						}
					}
				}
			default:
				if err := r.indexDocument(child, base); err != nil {
					return err
				}
			}
		}
	case []any:
		for _, child := range node {
			if err := r.indexDocument(child, base); err != nil {
				return err
			}
		}
	}
	return nil
}
func (r *schemaResolver) indexSchema(value any, base *url.URL) error {
	node, ok := value.(Object)
	if !ok {
		return nil
	}
	if node["$dynamicRef"] != nil || node["$dynamicAnchor"] != nil {
		return fmt.Errorf("dynamic schema references are unsupported")
	}
	if id, ok := node["$id"].(string); ok {
		relative, err := url.Parse(id)
		if err != nil {
			return err
		}
		base = base.ResolveReference(relative)
		key := withoutFragment(base)
		if previous, exists := r.resources[key]; exists && fmt.Sprintf("%p", previous) != fmt.Sprintf("%p", node) {
			return fmt.Errorf("duplicate schema resource %s", base)
		}
		r.resources[key] = node
	}
	r.contexts[fmt.Sprintf("%p", node)] = base
	if anchor, ok := node["$anchor"].(string); ok {
		key := withoutFragment(base) + "#" + anchor
		if previous, exists := r.anchors[key]; exists && fmt.Sprintf("%p", previous) != fmt.Sprintf("%p", node) {
			return fmt.Errorf("duplicate schema anchor %s", key)
		}
		r.anchors[key] = node
	}
	for key, child := range node {
		switch {
		case schemaMaps[key]:
			if fields, ok := child.(Object); ok {
				for _, field := range fields {
					if err := r.indexSchema(field, base); err != nil {
						return err
					}
				}
			}
		case schemaChildren[key]:
			if err := r.indexSchema(child, base); err != nil {
				return err
			}
		case schemaArrays[key]:
			if list, ok := child.([]any); ok {
				for _, field := range list {
					if err := r.indexSchema(field, base); err != nil {
						return err
					}
				}
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
