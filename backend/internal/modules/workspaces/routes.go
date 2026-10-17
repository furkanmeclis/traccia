// Package workspaces mounts the product API independently of platform administration.
package workspaces

import (
	"encoding/json"
	"errors"
	"io"
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
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { serveOperation(w, r, registry, meta) })
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
func serveOperation(w http.ResponseWriter, r *http.Request, registry *operations.Registry, meta operations.Metadata) {
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
	var raw []byte
	switch {
	case r.Method == "GET" && strings.HasSuffix(meta.ID, ".list"):
		query := r.URL.Query()
		for key := range query {
			if (key != "limit" && key != "after") || len(query[key]) != 1 {
				writeError(w, r, &operations.Error{Code: operations.Validation})
				return
			}
		}
		limit := int64(20)
		if val, ok := query["limit"]; ok {
			parsed, err := strconv.ParseInt(val[0], 10, 32)
			if err != nil {
				writeError(w, r, &operations.Error{Code: operations.Validation})
				return
			}
			limit = parsed
		}
		raw, _ = json.Marshal(usecase.ListInput{Limit: int32(limit), After: query.Get("after")})
	case r.Method == "GET":
		raw = []byte("{}")
	case r.Method == "DELETE":
		id, err := uuid.Parse(r.PathValue("credentialID"))
		if err != nil {
			writeError(w, r, &operations.Error{Code: operations.Validation})
			return
		}
		raw, _ = json.Marshal(usecase.RevokeInput{CredentialID: id})
	default:
		if r.Header.Get("Content-Type") != "application/json" && !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json;") {
			response.Error(w, r, 415, "UNSUPPORTED_MEDIA_TYPE", "application/json is required")
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 16<<10))
		if err != nil {
			writeError(w, r, &operations.Error{Code: operations.Validation})
			return
		}
		raw = body
	}
	output, err := registry.Execute(r.Context(), operations.Call{OperationID: meta.ID, Channel: operations.API, WorkspaceID: workspace, Input: raw, IdempotencyKey: key})
	if err != nil {
		writeError(w, r, err)
		return
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
