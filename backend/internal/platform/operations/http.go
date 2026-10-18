package operations

import "strings"

// HTTPPathFields lists domain DTO fields supplied by the resource path. The
// workspace path selects authenticated scope and never enters a domain DTO.
func HTTPPathFields(m Metadata) map[string]string {
	fields := map[string]string{}
	for _, part := range strings.Split(m.HTTPPath, "/") {
		if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
			name := strings.TrimSuffix(strings.TrimPrefix(part, "{"), "}")
			if name == "identifier" {
				fields[name] = name
			}
			if name != "workspaceID" && strings.HasSuffix(name, "ID") {
				fields[strings.TrimSuffix(name, "ID")+"_id"] = name
			}
		}
	}
	return fields
}
