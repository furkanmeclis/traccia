// Package workspaces mounts the product API independently of platform administration.
package workspaces

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/furkanmeclis/traccia/backend/internal/middleware"
	"github.com/furkanmeclis/traccia/backend/internal/modules/workspaces/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/jwt"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/furkanmeclis/traccia/backend/pkg/response"
	"github.com/google/uuid"
)

// RouteRegistrar permits observing the routes actually mounted by the product module.
// Every product registration goes through this boundary; the live server uses ServeMux.
type RouteRegistrar interface {
	Handle(string, http.Handler)
}

type operationRoute struct {
	http.Handler
	id string
}

// OperationID identifies the application handler actually mounted at a route.
func (h operationRoute) OperationID() string { return h.id }

func RegisterRoutes(mux RouteRegistrar, s *usecase.Service, tokens *jwt.Manager, loader middleware.IdentityLoader) error {
	registry, err := s.Registry()
	if err != nil {
		return err
	}
	return RegisterRegistryRoutes(mux, registry, tokens, loader)
}

// RegisterRegistryRoutes mounts the same production adapter with an explicitly
// bound registry. Tests can verify transport mapping without starting storage.
func RegisterRegistryRoutes(mux RouteRegistrar, registry *operations.Registry, tokens *jwt.Manager, loader middleware.IdentityLoader) error {
	human := middleware.Authenticate(tokens, loader)
	for _, contract := range registry.Catalog() {
		meta := contract.Metadata
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { serveOperation(w, r, registry, contract) })
		authenticate := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			parts := strings.Fields(r.Header.Get("Authorization"))
			if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") && strings.HasPrefix(parts[1], usecase.TokenPrefix) {
				handler.ServeHTTP(w, r.WithContext(usecase.WithToolCredential(r.Context(), parts[1])))
				return
			}
			human(handler).ServeHTTP(w, r)
		})
		mux.Handle(meta.HTTPMethod+" "+meta.HTTPPath, operationRoute{Handler: authenticate, id: meta.ID})
	}
	return nil
}
func serveOperation(w http.ResponseWriter, r *http.Request, registry *operations.Registry, contract operations.Contract) {
	meta := contract.Metadata
	w.Header().Set("Cache-Control", "no-store")
	var workspace uuid.UUID
	if meta.Scope == operations.WorkspaceScope {
		id, err := uuid.Parse(r.PathValue("workspaceID"))
		if err != nil {
			writeError(w, r, &operations.Error{Code: operations.Validation})
			return
		}
		workspace = id
	}
	var key string
	if meta.Idempotent {
		values := r.Header.Values("Idempotency-Key")
		if len(values) != 1 || strings.Contains(values[0], ",") {
			writeError(w, r, &operations.Error{Code: operations.Validation})
			return
		}
		key = values[0]
	}
	var schema struct {
		Properties map[string]struct {
			Type any `json:"type"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(contract.Input, &schema); err != nil {
		writeError(w, r, err)
		return
	}
	fields := map[string]any{}
	paths := operations.HTTPPathFields(meta)
	switch r.Method {
	case "GET", "DELETE":
		query := r.URL.Query()
		for key, values := range query {
			_, known := schema.Properties[key]
			_, pathField := paths[key]
			if !known || pathField || len(values) != 1 {
				writeError(w, r, &operations.Error{Code: operations.Validation})
				return
			}
		}
		for key, prop := range schema.Properties {
			if _, ok := paths[key]; ok {
				continue
			}
			value := query.Get(key)
			kind, _ := prop.Type.(string)
			switch kind {
			case "integer":
				if value == "" && key == "limit" {
					value = "20"
				}
				v, err := strconv.ParseInt(value, 10, 32)
				if err != nil {
					writeError(w, r, &operations.Error{Code: operations.Validation})
					return
				}
				fields[key] = v
			case "boolean":
				if value == "" {
					value = "false"
				}
				if value != "true" && value != "false" {
					writeError(w, r, &operations.Error{Code: operations.Validation})
					return
				}
				fields[key] = value == "true"
			default:
				fields[key] = value
			}
		}
	default:
		if r.Header.Get("Content-Type") != "application/json" && !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json;") {
			response.Error(w, r, 415, "UNSUPPORTED_MEDIA_TYPE", "application/json is required")
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, registry.InputReadLimit(meta.ID)))
		if err != nil {
			writeError(w, r, registry.InputReadError(meta.ID))
			return
		}
		value, err := operations.DecodeJSON(body)
		if err != nil {
			writeError(w, r, &operations.Error{Code: operations.Validation})
			return
		}
		var ok bool
		fields, ok = value.(map[string]any)
		if !ok {
			writeError(w, r, &operations.Error{Code: operations.Validation})
			return
		}
		for key := range paths {
			if _, present := fields[key]; present {
				writeError(w, r, &operations.Error{Code: operations.Validation})
				return
			}
		}
	}
	for key, name := range paths {
		if name == "identifier" {
			fields[key] = r.PathValue(name)
			continue
		}
		id, err := uuid.Parse(r.PathValue(name))
		if err != nil || id == uuid.Nil {
			writeError(w, r, &operations.Error{Code: operations.Validation})
			return
		}
		fields[key] = id.String()
	}
	raw, err := json.Marshal(fields)
	if err != nil {
		writeError(w, r, err)
		return
	}
	output, err := registry.Execute(r.Context(), operations.Call{OperationID: meta.ID, Channel: operations.API, WorkspaceID: workspace, Input: raw, IdempotencyKey: key})
	if err != nil {
		writeError(w, r, err)
		return
	}
	if meta.ID == "attachments.download_chunk" {
		var transfer struct {
			Disposition string `json:"content_disposition"`
		}
		if e := json.Unmarshal(output, &transfer); e != nil {
			writeError(w, r, e)
			return
		}
		kind, _, e := mime.ParseMediaType(transfer.Disposition)
		if e != nil || kind != "attachment" || strings.ContainsAny(transfer.Disposition, "\r\n") {
			writeError(w, r, &operations.Error{Code: operations.Internal})
			return
		}
		w.Header().Set("Content-Disposition", transfer.Disposition)
		w.Header().Set("X-Content-Type-Options", "nosniff")
	}
	status := http.StatusOK
	if r.Method == "POST" {
		status = http.StatusCreated
	}
	response.JSON(w, r, status, json.RawMessage(output))
}
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	var operationError *operations.Error
	if !errors.As(err, &operationError) {
		response.Internal(w, r, "Operation failed")
		return
	}
	status := http.StatusInternalServerError
	switch operationError.Code {
	case operations.Unauthenticated:
		status = 401
	case operations.Forbidden:
		status = 403
	case operations.NotFound:
		status = 404
	case operations.Validation:
		status = 400
	case operations.Conflict, operations.IdempotencyConflict, operations.VersionConflict:
		status = 409
	case operations.Unavailable:
		status = 503
	case operations.Canceled:
		status = 408
	}
	details := make([]response.Detail, 0, len(operationError.Details))
	for _, d := range operationError.Details {
		details = append(details, response.Detail{Field: d.Field, Code: d.Code, Message: d.Code})
	}
	response.ErrorWithDetails(w, r, status, string(operationError.Code), string(operationError.Code), details)
}
