package usecase

import (
	"encoding/json"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/google/uuid"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"
)

const MaxProjects = 1000

func safeURL(s string, optional bool) bool {
	if s == "" {
		return optional
	}
	u, e := url.Parse(s)
	if e != nil || len(s) > 2048 || strings.TrimSpace(s) != s || strings.IndexFunc(s, unicode.IsControl) >= 0 {
		return false
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Opaque != "" {
		return false
	}
	// Catalog links are public reference metadata, never a credential store.
	for key := range u.Query() {
		switch strings.ToLower(key) {
		case "token", "access_token", "refresh_token", "api_key", "apikey", "secret", "password", "signature", "x-amz-signature", "x-amz-credential", "authorization":
			return false
		}
	}
	return true
}
func valid[T any](in T) []operations.FieldError {
	raw, _ := json.Marshal(in)
	fields := map[string]json.RawMessage{}
	_ = json.Unmarshal(raw, &fields)
	out := []operations.FieldError{}
	for key, value := range fields {
		bad := false
		var str string
		_ = json.Unmarshal(value, &str)
		switch key {
		case "product_id", "environment_id", "project_id":
			if str != "" {
				id, e := uuid.Parse(str)
				bad = e != nil || id == uuid.Nil
			} else {
				bad = key != "project_id"
			}
		case "after":
			if str != "" {
				_, e := parseCursor(str)
				bad = e != nil
			}
		case "name":
			bad = str != strings.TrimSpace(str) || utf8.RuneCountInString(str) < 1 || utf8.RuneCountInString(str) > 100 || strings.IndexFunc(str, unicode.IsControl) >= 0
		case "summary":
			bad = utf8.RuneCountInString(str) > 2000 || strings.ContainsRune(str, 0)
		case "q":
			bad = utf8.RuneCountInString(str) > 100 || strings.IndexFunc(str, unicode.IsControl) >= 0
		case "repository_provider", "deploy_provider", "repository_reference", "deploy_reference":
			max := 100
			if strings.HasSuffix(key, "reference") {
				max = 200
			}
			bad = utf8.RuneCountInString(str) > max || str != strings.TrimSpace(str) || strings.IndexFunc(str, unicode.IsControl) >= 0
		case "url", "repository_url", "deploy_url":
			bad = !safeURL(str, key != "url")
		case "kind":
			bad = str != "production" && str != "staging" && str != "local" && str != "custom"
			if _, ok := any(in).(EnvironmentPageInput); ok && str == "" {
				bad = false
			}
		case "limit":
			var n int32
			_ = json.Unmarshal(value, &n)
			bad = n < 1 || n > 100
		case "expected_version", "expected_product_version":
			var n int64
			_ = json.Unmarshal(value, &n)
			bad = n < 1
		case "project_ids":
			var ids []string
			_ = json.Unmarshal(value, &ids)
			bad = len(ids) > MaxProjects
			seen := map[uuid.UUID]bool{}
			for _, v := range ids {
				id, e := uuid.Parse(v)
				if e != nil || id == uuid.Nil || seen[id] {
					bad = true
				}
				seen[id] = true
			}
		}
		if bad {
			out = append(out, operations.FieldError{Field: key, Code: "invalid"})
		}
	}
	return out
}
func refine(input, output *jsonschema.Schema) error {
	var visit func(*jsonschema.Schema)
	visit = func(s *jsonschema.Schema) {
		if s == nil {
			return
		}
		for key, c := range s.Properties {
			switch key {
			case "id", "workspace_id", "product_id", "environment_id":
				*c = jsonschema.Schema{Type: "string", Format: "uuid"}
			case "project_id":
				*c = jsonschema.Schema{Type: "string", AnyOf: []*jsonschema.Schema{{Type: "string", Enum: []any{""}}, {Type: "string", Format: "uuid"}}}
			case "lead_user_id":
				*c = jsonschema.Schema{AnyOf: []*jsonschema.Schema{{Type: "string", Format: "uuid"}, {Type: "null"}}}
			case "start_date", "target_date":
				*c = jsonschema.Schema{AnyOf: []*jsonschema.Schema{{Type: "string", Format: "date"}, {Type: "null"}}}
			case "name":
				a, b := 1, 100
				c.MinLength = &a
				c.MaxLength = &b
			case "summary":
				b := 2000
				c.MaxLength = &b
			case "q", "repository_provider", "deploy_provider":
				b := 100
				c.MaxLength = &b
			case "repository_reference", "deploy_reference":
				b := 200
				c.MaxLength = &b
			case "url", "repository_url", "deploy_url":
				b := 2048
				c.MaxLength = &b
				if key == "url" {
					a := 1
					c.MinLength = &a
					c.Format = "uri"
				}
			case "kind":
				c.Enum = []any{"custom", "local", "production", "staging"}
			case "version", "product_version", "expected_version", "expected_product_version":
				a := float64(1)
				c.Minimum = &a
			case "limit":
				a, b := float64(1), float64(100)
				c.Minimum = &a
				c.Maximum = &b
			case "project_ids":
				a, b := 0, MaxProjects
				c.MinItems = &a
				c.MaxItems = &b
				c.UniqueItems = true
				c.Items = &jsonschema.Schema{Type: "string", Format: "uuid"}
			case "created_at", "updated_at":
				c.Format = "date-time"
			}
			visit(c)
		}
		visit(s.Items)
		for _, c := range s.AnyOf {
			visit(c)
		}
	}
	visit(input)
	visit(output)
	if c := input.Properties["kind"]; c != nil {
		if _, ok := input.Properties["after"]; ok {
			c.Enum = []any{"", "custom", "local", "production", "staging"}
		}
	}
	return nil
}
func (s *Service) Operations() ([]*operations.Operation, error) {
	ops := []*operations.Operation{}
	add := func(op *operations.Operation, e error) error {
		if e != nil {
			return e
		}
		ops = append(ops, op)
		return nil
	}
	meta := func(id, method, path, tool string) operations.Metadata {
		effect := operations.Read
		permission := "workspace.read"
		if method != "GET" {
			effect = operations.Write
			permission = "workspace.write"
		}
		return operations.Metadata{ID: id, Version: 1, Scope: operations.WorkspaceScope, Effect: effect, Permission: permission, Idempotent: effect == operations.Write, HTTPMethod: method, HTTPPath: "/v1/workspaces/{workspaceID}" + path, MCPName: tool}
	}
	if e := add(operations.New(meta("products.create", "POST", "/products", "create_product"), valid[CreateProductInput], s.createProduct, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("products.list", "GET", "/products", "list_products"), valid[PageInput], s.listProducts, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("products.get", "GET", "/products/{productID}", "get_product"), valid[ProductInput], s.getProduct, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("products.update", "PATCH", "/products/{productID}", "update_product"), valid[UpdateProductInput], s.updateProduct, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("products.archive", "POST", "/products/{productID}/archive", "archive_product"), valid[ArchiveProductInput], s.archiveProduct, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("products.restore", "POST", "/products/{productID}/restore", "restore_product"), valid[ArchiveProductInput], s.restoreProduct, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("product_projects.list", "GET", "/products/{productID}/projects", "list_product_projects"), valid[ProductPageInput], s.listProjects, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("product_projects.set", "PUT", "/products/{productID}/projects", "set_product_projects"), valid[SetProjectsInput], s.setProjects, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("product_environments.create", "POST", "/products/{productID}/environments", "create_product_environment"), valid[CreateEnvironmentInput], s.createEnvironment, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("product_environments.list", "GET", "/products/{productID}/environments", "list_product_environment"), valid[EnvironmentPageInput], s.listEnvironments, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("product_environments.get", "GET", "/products/{productID}/environments/{environmentID}", "get_product_environment"), valid[EnvironmentInput], s.getEnvironment, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("product_environments.update", "PATCH", "/products/{productID}/environments/{environmentID}", "update_product_environment"), valid[UpdateEnvironmentInput], s.updateEnvironment, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("product_environments.archive", "POST", "/products/{productID}/environments/{environmentID}/archive", "archive_product_environment"), valid[ArchiveEnvironmentInput], s.archiveEnvironment, refine)); e != nil {
		return nil, e
	}
	if e := add(operations.New(meta("product_environments.restore", "POST", "/products/{productID}/environments/{environmentID}/restore", "restore_product_environment"), valid[ArchiveEnvironmentInput], s.restoreEnvironment, refine)); e != nil {
		return nil, e
	}
	return ops, nil
}
