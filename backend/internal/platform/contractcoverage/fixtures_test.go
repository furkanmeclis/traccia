package contractcoverage_test

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	coverage "github.com/furkanmeclis/traccia/backend/internal/platform/contractcoverage"
)

func readFixture(t *testing.T) coverage.Object {
	t.Helper()
	raw, err := os.ReadFile("testdata/complete.json")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := coverage.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}
func surfaces(t *testing.T, doc coverage.Object) coverage.Surfaces {
	t.Helper()
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	var result coverage.Surfaces
	if err = json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	// Generic SDK schema values must be decoded with exact numbers, not the
	// encoding/json default any path. The production discovery does this on wire.
	for i, value := range doc["Tools"].([]any) {
		tool := value.(coverage.Object)
		result.Tools[i].Input = tool["Input"]
		result.Tools[i].Output = tool["Output"]
		result.Tools[i].Error = tool["Error"]
	}
	result.OpenAPI = doc["OpenAPI"].(coverage.Object)
	return result
}

func TestImmutableConformanceFixtures(t *testing.T) {
	if failures := coverage.Check(surfaces(t, readFixture(t))); len(failures) > 0 {
		t.Fatal(strings.Join(failures, "\n"))
	}
	raw, err := os.ReadFile("testdata/mutations.json")
	if err != nil {
		t.Fatal(err)
	}
	value, err := coverage.Decode([]byte(`{"cases":` + string(raw) + `}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range value["cases"].([]any) {
		mutation := item.(coverage.Object)
		t.Run(mutation["name"].(string), func(t *testing.T) {
			doc := readFixture(t)
			if err := patch(doc, mutation["path"].(string), mutation["value"], mutation["remove"] == true); err != nil {
				t.Fatal(err)
			}
			failures := coverage.Check(surfaces(t, doc))
			want := mutation["want"].(string)
			if len(failures) == 0 || !strings.Contains(strings.Join(failures, "\n"), want) {
				t.Fatalf("wanted diagnostic %q, got %v", want, failures)
			}
		})
	}
}

func patch(doc coverage.Object, path string, value any, remove bool) error {
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	var current any = doc
	for i, part := range parts {
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		last := i == len(parts)-1
		switch node := current.(type) {
		case map[string]any:
			if last {
				if remove {
					delete(node, part)
				} else {
					node[part] = value
				}
				return nil
			}
			next, ok := node[part]
			if !ok {
				return fmt.Errorf("missing patch path %s", path)
			}
			current = next
		case []any:
			index, err := strconv.Atoi(part)
			if err != nil {
				return err
			}
			if index == len(node) && last && !remove {
				// Append fixtures replace their containing array rather than aliasing slices.
				return patch(doc, "/"+strings.Join(parts[:i], "/"), append(node, value), false)
			}
			if index < 0 || index >= len(node) {
				return fmt.Errorf("invalid patch index %s", path)
			}
			if last {
				if remove {
					return fmt.Errorf("remove arrays by replacing the whole array")
				}
				node[index] = value
				return nil
			}
			current = node[index]
		default:
			return fmt.Errorf("invalid patch path %s", path)
		}
	}
	return nil
}

func TestLocalRefsAndClosedNestedSchemas(t *testing.T) {
	doc, err := coverage.Decode([]byte(`{"$defs":{"Detail":{"type":"object","additionalProperties":false,"properties":{"count":{"type":"integer","maximum":9007199254740993}},"required":["count"]}},"type":"object","additionalProperties":false,"properties":{"detail":{"$ref":"#/$defs/Detail"}},"required":["detail"]}`))
	if err != nil {
		t.Fatal(err)
	}
	normalized, err := coverage.Normalize(doc, doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := coverage.Closed(normalized, "schema"); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(normalized)
	if !strings.Contains(string(raw), "9007199254740993") {
		t.Fatal("schema constraint rounded")
	}
	if err := coverage.Validate(normalized, []byte(`{"detail":{"count":9007199254740994}}`)); err == nil {
		t.Fatal("maximum constraint not enforced")
	}
	if _, err := coverage.Normalize(coverage.Object{"$ref": "https://example.invalid/schema"}, doc); err == nil {
		t.Fatal("external reference accepted")
	}
}

func TestResourceIDsAndLiteralKeywords(t *testing.T) {
	doc, err := coverage.Decode([]byte(`{"$id":"relative.json","$defs":{"value":{"type":"integer"},"child":{"$id":"child.json","$anchor":"child","type":"object","additionalProperties":false,"properties":{"value":{"$ref":"relative.json#/$defs/value"}},"required":["value"]}},"type":"object","additionalProperties":false,"properties":{"title":{"type":"string"},"child":{"$ref":"child.json#child"}},"required":["child","title"],"default":{"$ref":"#literal"}}`))
	if err != nil {
		t.Fatal(err)
	}
	normalized, err := coverage.Normalize(doc, doc)
	if err != nil {
		t.Fatal(err)
	}
	props := normalized.(coverage.Object)["properties"].(coverage.Object)
	if props["title"] == nil {
		t.Fatal("property named title erased")
	}
	if props["child"].(coverage.Object)["properties"].(coverage.Object)["value"].(coverage.Object)["type"] != "integer" {
		t.Fatal("cross-resource reference did not resolve")
	}
	if normalized.(coverage.Object)["default"].(coverage.Object)["$ref"] != "#literal" {
		t.Fatal("literal default rewritten")
	}
}
