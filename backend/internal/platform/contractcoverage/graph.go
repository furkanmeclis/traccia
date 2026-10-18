package contractcoverage

import (
	"fmt"
	"sort"

	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
)

var schemaMaps = map[string]bool{"properties": true, "patternProperties": true, "dependentSchemas": true, "$defs": true, "definitions": true}
var schemaChildren = map[string]bool{"items": true, "contains": true, "additionalProperties": true, "unevaluatedProperties": true, "propertyNames": true, "not": true, "if": true, "then": true, "else": true, "additionalItems": true, "contentSchema": true}
var schemaArrays = map[string]bool{"anyOf": true, "oneOf": true, "allOf": true, "prefixItems": true}

func schemaReferences(value any) bool {
	schema, ok := value.(Object)
	if !ok {
		return false
	}
	if schema["$ref"] != nil {
		return true
	}
	for key, child := range schema {
		if schemaMaps[key] {
			if fields, ok := child.(Object); ok {
				for _, field := range fields {
					if schemaReferences(field) {
						return true
					}
				}
			}
		} else if schemaChildren[key] {
			if schemaReferences(child) {
				return true
			}
		} else if schemaArrays[key] {
			if variants, ok := child.([]any); ok {
				for _, variant := range variants {
					if schemaReferences(variant) {
						return true
					}
				}
			}
		}
	}
	return false
}
func annotation(key string) bool {
	switch key {
	case "$schema", "$id", "$anchor", "$defs", "definitions", "description", "title", "examples":
		return true
	}
	return false
}

type graphComparator struct {
	left, right *schemaResolver
	seen        map[string]bool
}

func graphDifference(left, right any, path string) string {
	a, aok := left.(Object)
	b, bok := right.(Object)
	if !aok || !bok {
		return treeDifference(left, right, path)
	}
	lr, e := newSchemaResolver(a, operations.SchemaBaseURI)
	if e != nil {
		return path + "/left-reference"
	}
	rr, e := newSchemaResolver(b, operations.SchemaBaseURI)
	if e != nil {
		return path + "/right-reference"
	}
	g := graphComparator{lr, rr, map[string]bool{}}
	return g.compare(left, right, path)
}
func dereference(value any, r *schemaResolver) (any, error) {
	seen := map[string]bool{}
	for {
		schema, ok := value.(Object)
		if !ok {
			return value, nil
		}
		ref, ok := schema["$ref"].(string)
		if !ok {
			return value, nil
		}
		for key := range schema {
			if key != "$ref" && !annotation(key) {
				return nil, fmt.Errorf("un-normalized reference siblings")
			}
		}
		target, identity, e := r.resolve(schema, ref)
		if e != nil {
			return nil, e
		}
		if seen[identity] {
			return nil, fmt.Errorf("unguarded reference")
		}
		seen[identity] = true
		value = target
	}
}
func copyPairs(in map[string]bool) map[string]bool {
	out := map[string]bool{}
	for key, value := range in {
		out[key] = value
	}
	return out
}
func (g *graphComparator) compare(left, right any, path string) string {
	a, e := dereference(left, g.left)
	if e != nil {
		return path + "/$ref"
	}
	b, e := dereference(right, g.right)
	if e != nil {
		return path + "/$ref"
	}
	am, aok := a.(Object)
	bm, bok := b.(Object)
	if !aok || !bok {
		return treeDifference(a, b, path)
	}
	pair := fmt.Sprintf("%p|%p", am, bm)
	if g.seen[pair] {
		return ""
	}
	g.seen[pair] = true
	keys := []string{}
	for key := range am {
		if !annotation(key) {
			keys = append(keys, key)
		}
	}
	for key := range bm {
		if !annotation(key) {
			if _, exists := am[key]; !exists {
				keys = append(keys, key)
			}
		}
	}
	sort.Strings(keys)
	// Scalar constraints are checked before following recursive edges.
	for _, key := range keys {
		av, ap := am[key]
		bv, bp := bm[key]
		if !ap || !bp {
			return path + "/" + key
		}
		if !schemaMaps[key] && !schemaChildren[key] && !schemaArrays[key] {
			if d := treeDifference(av, bv, path+"/"+key); d != "" {
				return d
			}
		}
	}
	for _, key := range keys {
		av, bv := am[key], bm[key]
		switch {
		case schemaMaps[key]:
			x, xok := av.(Object)
			y, yok := bv.(Object)
			if !xok || !yok {
				return path + "/" + key
			}
			if len(x) != len(y) {
				return path + "/" + key
			}
			names := []string{}
			for name := range x {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				child, exists := y[name]
				if !exists {
					return path + "/" + key + "/" + name
				}
				if d := g.compare(x[name], child, path+"/"+key+"/"+name); d != "" {
					return d
				}
			}
		case schemaChildren[key]:
			if d := g.compare(av, bv, path+"/"+key); d != "" {
				return d
			}
		case schemaArrays[key]:
			x, xok := av.([]any)
			y, yok := bv.([]any)
			if !xok || !yok || len(x) != len(y) {
				return path + "/" + key
			}
			if key == "prefixItems" {
				for i := range x {
					if d := g.compare(x[i], y[i], fmt.Sprintf("%s/%s/%d", path, key, i)); d != "" {
						return d
					}
				}
				continue
			}
			if !g.match(x, y, make([]bool, len(y)), 0, path+"/"+key) {
				return path + "/" + key
			}
		}
	}
	return ""
}
func (g *graphComparator) match(left, right []any, used []bool, index int, path string) bool {
	if index == len(left) {
		return true
	}
	for j := range right {
		if used[j] {
			continue
		}
		trial := *g
		trial.seen = copyPairs(g.seen)
		if trial.compare(left[index], right[j], fmt.Sprintf("%s/%d", path, index)) != "" {
			continue
		}
		next := append([]bool(nil), used...)
		next[j] = true
		if trial.match(left, right, next, index+1, path) {
			g.seen = trial.seen
			return true
		}
	}
	return false
}

type closureContext struct{ concrete, object, array, closed bool }

func inheritedContext(value any, r *schemaResolver, seen map[string]bool) closureContext {
	value, e := dereference(value, r)
	if e != nil {
		return closureContext{}
	}
	s, ok := value.(Object)
	if !ok {
		return closureContext{}
	}
	key := fmt.Sprintf("%p", s)
	if seen[key] {
		return closureContext{}
	}
	seen[key] = true
	c := closureContext{concrete: s["type"] != nil || s["const"] != nil || s["enum"] != nil, object: hasType(s, "object") || s["properties"] != nil, array: hasType(s, "array"), closed: s["additionalProperties"] == false}
	if branches, ok := s["allOf"].([]any); ok {
		for _, branch := range branches {
			child := inheritedContext(branch, r, seen)
			c.concrete = c.concrete || child.concrete
			c.object = c.object || child.object
			c.array = c.array || child.array
			c.closed = c.closed || child.closed
		}
	}
	return c
}
func closedGraph(value any, path string) error {
	root, ok := value.(Object)
	if !ok {
		return closedTree(value, path)
	}
	normalized, e := Normalize(root, root)
	if e != nil {
		return fmt.Errorf("%s: %w", path, e)
	}
	var object bool
	root, object = normalized.(Object)
	if !object {
		return closedTree(normalized, path)
	}
	r, e := newSchemaResolver(root, operations.SchemaBaseURI)
	if e != nil {
		return e
	}
	return visitClosed(root, r, path, closureContext{}, false, map[string]bool{})
}

var fragmentKeywords = map[string]bool{"type": true, "const": true, "enum": true, "minimum": true, "maximum": true, "exclusiveMinimum": true, "exclusiveMaximum": true, "multipleOf": true, "minLength": true, "maxLength": true, "pattern": true, "format": true, "required": true, "minProperties": true, "maxProperties": true, "dependentRequired": true, "minItems": true, "maxItems": true, "uniqueItems": true, "minContains": true, "maxContains": true}

func visitClosed(value any, r *schemaResolver, path string, inherited closureContext, fragment bool, seen map[string]bool) error {
	value, e := dereference(value, r)
	if e != nil {
		return fmt.Errorf("%s: %w", path, e)
	}
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
	key := fmt.Sprintf("%p:%t:%t:%t:%t:%t", schema, inherited.concrete, inherited.object, inherited.array, inherited.closed, fragment)
	if seen[key] {
		return nil
	}
	seen[key] = true
	own := inheritedContext(schema, r, map[string]bool{})
	effective := closureContext{own.concrete || inherited.concrete, own.object || inherited.object, own.array || inherited.array, own.closed || inherited.closed}
	if fragment {
		for key := range schema {
			if !annotation(key) && !fragmentKeywords[key] && !schemaMaps[key] && !schemaChildren[key] && !schemaArrays[key] {
				return fmt.Errorf("%s/%s: unsupported conjunction fragment", path, key)
			}
		}
	}
	if effective.object && !effective.closed {
		return fmt.Errorf("%s/additionalProperties: closed object required", path)
	}
	if effective.array && schema["items"] == nil && schema["prefixItems"] == nil && (!fragment || !inherited.array) {
		return fmt.Errorf("%s/items: concrete items required", path)
	}
	for keyword, child := range schema {
		if annotation(keyword) {
			continue
		}
		switch {
		case schemaMaps[keyword]:
			if props, ok := child.(Object); ok {
				for name, property := range props {
					if e := visitClosed(property, r, path+"/"+keyword+"/"+name, closureContext{}, false, seen); e != nil {
						return e
					}
				}
			}
		case schemaChildren[keyword]:
			if child == false {
				continue
			}
			if e := visitClosed(child, r, path+"/"+keyword, closureContext{}, false, seen); e != nil {
				return e
			}
		case schemaArrays[keyword]:
			if branches, ok := child.([]any); ok {
				for i, branch := range branches {
					context := closureContext{}
					isFragment := false
					if keyword == "allOf" {
						context = effective
						isFragment = true
					}
					if e := visitClosed(branch, r, fmt.Sprintf("%s/%s/%d", path, keyword, i), context, isFragment, seen); e != nil {
						return e
					}
				}
			}
		}
	}
	if !own.concrete && schema["anyOf"] == nil && schema["oneOf"] == nil && schema["allOf"] == nil && (!fragment || !inherited.concrete) {
		return fmt.Errorf("%s: concrete schema required", path)
	}
	return nil
}
