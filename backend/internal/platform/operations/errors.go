package operations

import (
	"context"
	"errors"
)

type Code string

const (
	Unauthenticated     Code = "UNAUTHORIZED"
	Forbidden           Code = "FORBIDDEN"
	Validation          Code = "VALIDATION_ERROR"
	NotFound            Code = "NOT_FOUND"
	Conflict            Code = "CONFLICT"
	IdempotencyConflict Code = "IDEMPOTENCY_CONFLICT"
	VersionConflict     Code = "VERSION_CONFLICT"
	Unavailable         Code = "OPERATION_UNAVAILABLE"
	Canceled            Code = "CANCELED"
	Internal            Code = "INTERNAL_ERROR"
)

type FieldError struct {
	Field string `json:"field"`
	Code  string `json:"code"`
}

// Error contains stable public machine codes. Message strings are not a protocol;
// adapters render localized copy by code. Unwrap retains diagnostics for callers.
type Error struct {
	Code    Code         `json:"code"`
	Details []FieldError `json:"details,omitempty"`
	cause   error
}

func (e *Error) Error() string { return string(e.Code) }
func (e *Error) Unwrap() error { return e.cause }

func failure(code Code, cause error) *Error { return &Error{Code: code, cause: cause} }

// publicError never serializes arbitrary downstream error messages.
func publicError(err error) *Error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return failure(Canceled, err)
	}
	var known *Error
	if errors.As(err, &known) && known != nil {
		switch known.Code {
		case Unauthenticated, Forbidden, Validation, NotFound, Conflict, IdempotencyConflict, VersionConflict, Unavailable, Canceled, Internal:
			return &Error{Code: known.Code, Details: append([]FieldError(nil), known.Details...), cause: err}
		}
	}
	return failure(Internal, err)
}
