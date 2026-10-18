package repository

import (
	"context"
	"errors"
	"github.com/furkanmeclis/traccia/backend/internal/database/db"
	"github.com/furkanmeclis/traccia/backend/internal/platform/dbtx"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Postgres struct {
	pool *pgxpool.Pool
	q    *db.Queries
}

func New(p *pgxpool.Pool, q *db.Queries) *Postgres { return &Postgres{p, q} }
func (r *Postgres) queries(c context.Context) *db.Queries {
	if tx, ok := dbtx.Tx(c); ok {
		return r.q.WithTx(tx)
	}
	return r.q
}
func fail(code operations.Code) error { return &operations.Error{Code: code} }
func mapped(e error) error {
	if errors.Is(e, pgx.ErrNoRows) {
		return fail(operations.NotFound)
	}
	return e
}
func expired() error {
	return &operations.Error{Code: operations.Conflict, Details: []operations.FieldError{{Field: "session_id", Code: "expired"}}}
}
func invalidCursor() error {
	return &operations.Error{Code: operations.Validation, Details: []operations.FieldError{{Field: "after", Code: "invalid"}}}
}
