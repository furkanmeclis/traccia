package usecase

import (
	"encoding/json"
	"github.com/google/jsonschema-go/jsonschema"
	"slices"
	"strings"
)

func shaped(value any) *jsonschema.Schema {
	raw, _ := json.Marshal(value)
	var out jsonschema.Schema
	_ = json.Unmarshal(raw, &out)
	return &out
}
func number(n float64) *float64 { return &n }
func length(n int) *int         { return &n }
func refine(input, output *jsonschema.Schema) error {
	for _, root := range []*jsonschema.Schema{input, output} {
		var visit func(*jsonschema.Schema)
		visit = func(s *jsonschema.Schema) {
			if s == nil {
				return
			}
			for key, child := range s.Properties {
				switch {
				case key == "id" || key == "file_revision" || strings.HasSuffix(key, "_id"):
					nullable := len(child.AnyOf) > 0 || slices.Contains(child.Types, "null")
					if nullable {
						s.Properties[key] = shaped(map[string]any{"anyOf": []any{map[string]any{"type": "string", "format": "uuid"}, map[string]any{"type": "null"}}})
					} else if key == "draft_id" && child.Type == "string" {
						s.Properties[key] = shaped(map[string]any{"oneOf": []any{map[string]any{"type": "string", "const": ""}, map[string]any{"type": "string", "format": "uuid", "pattern": "^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}$"}}})
					} else {
						s.Properties[key] = shaped(map[string]any{"type": "string", "format": "uuid"})
					}
				case key == "items":
					child.Type = "array"
					child.Types = nil
					child.MaxItems = length(100)
				case key == "sha256" || key == "chunk_sha256":
					child.Pattern = "^[a-f0-9]{64}$"
				case key == "filename" || key == "download_filename":
					child.Pattern = "^[^\x00]*$"
					child.MinLength = length(1)
					child.MaxLength = length(255)
				case key == "search":
					child.MaxLength = length(255)
					child.Pattern = "^[^\x00]*$"
				case key == "content_type":
					child.MaxLength = length(255)
					child.Pattern = "^[^\x00]*$"
				case key == "data_base64":
					child.MaxLength = length(4 * ((int(MaxChunkBytes) + 2) / 3))
					child.Pattern = `^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$`
				case key == "size_bytes" || key == "offset" || key == "next_offset" || key == "total_bytes":
					child.Minimum = number(0)
					child.Maximum = number(float64(MaxFileBytes))
				case key == "version" || key == "expected_version":
					child.Minimum = number(1)
				case key == "limit":
					child.Minimum = number(1)
					child.Maximum = number(100)
				case key == "state":
					if s.Properties["last_error"] != nil {
						child.Enum = []any{"none", "pending", "retrying", "reconciled"}
					} else {
						child.Enum = []any{"uploading", "finalized", "bound", "removed", "aborted", "expired"}
						if s.Properties["file_revision"] != nil {
							child.Enum = []any{"finalized", "bound", "removed", "aborted", "expired"}
						}
					}
				case key == "created_at" || key == "updated_at" || key == "finalized_at" || key == "occurred_at":
					child.Format = "date-time"
				case key == "expires_at" || key == "next_check_at":
					s.Properties[key] = shaped(map[string]any{"anyOf": []any{map[string]any{"type": "string", "format": "date-time"}, map[string]any{"type": "null"}}})
				}
				visit(s.Properties[key])
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
		for _, definition := range root.Defs {
			visit(definition)
		}
	}
	return nil
}
