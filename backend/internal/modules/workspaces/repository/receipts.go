package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/furkanmeclis/traccia/backend/internal/database/db"
	"github.com/furkanmeclis/traccia/backend/internal/platform/dbtx"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// RunMutation commits only a completed, validated receipt and its domain changes.
// The separate SELECT after INSERT ON CONFLICT observes the winner at READ COMMITTED.
func (r *Postgres) RunMutation(ctx context.Context, m operations.Mutation, run func(context.Context) ([]byte, error)) ([]byte, error) {
	if _, ok := dbtx.Tx(ctx); ok {
		return nil, fmt.Errorf("operation receipt cannot own an existing transaction")
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	ctx = dbtx.WithTx(ctx, tx)
	if err := m.Authorize(ctx); err != nil {
		return nil, err
	}
	q := r.queries(ctx)
	w := pgtype.UUID{Bytes: m.Scope.WorkspaceID(), Valid: m.Scope.WorkspaceID() != uuid.Nil}
	n, err := q.ClaimOperationReceipt(ctx, db.ClaimOperationReceiptParams{ActorID: m.Scope.ActorID(), WorkspaceID: w, OperationID: m.Metadata.ID, OperationVersion: int64(m.Metadata.Version), IdempotencyKey: m.Key, Fingerprint: m.Fingerprint, Encrypted: m.Metadata.SensitiveResult})
	if err != nil {
		return nil, err
	}
	receipt, err := q.GetOperationReceipt(ctx, db.GetOperationReceiptParams{ActorID: m.Scope.ActorID(), WorkspaceID: w, OperationID: m.Metadata.ID, OperationVersion: int64(m.Metadata.Version), IdempotencyKey: m.Key})
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(receipt.Fingerprint, m.Fingerprint) {
		return nil, &operations.Error{Code: operations.IdempotencyConflict}
	}
	// Recheck after a potentially blocked claim, before exposing any saved bytes.
	if err := m.Authorize(ctx); err != nil {
		return nil, err
	}
	aad, err := json.Marshal(struct {
		Actor, Workspace uuid.UUID
		Operation        string
		Version          int
		Key              string
		Fingerprint      []byte
	}{m.Scope.ActorID(), m.Scope.WorkspaceID(), m.Metadata.ID, m.Metadata.Version, m.Key, m.Fingerprint})
	if err != nil {
		return nil, err
	}
	var output []byte
	if n == 0 {
		if len(receipt.Result) == 0 || receipt.Encrypted != m.Metadata.SensitiveResult {
			return nil, &operations.Error{Code: operations.Unavailable}
		}
		output = receipt.Result
		if receipt.Encrypted {
			if r.box == nil {
				return nil, &operations.Error{Code: operations.Unavailable}
			}
			plain, err := r.box.DecryptBound(string(output), aad)
			if err != nil {
				return nil, &operations.Error{Code: operations.Unavailable}
			}
			output = []byte(plain)
		}
	} else {
		if m.Metadata.SensitiveResult && r.box == nil {
			return nil, &operations.Error{Code: operations.Unavailable}
		}
		output, err = run(ctx)
		if err != nil {
			return nil, err
		}
		stored := output
		if m.Metadata.SensitiveResult {
			sealed, err := r.box.EncryptBound(string(output), aad)
			if err != nil {
				return nil, err
			}
			stored = []byte(sealed)
		}
		if err := q.CompleteOperationReceipt(ctx, db.CompleteOperationReceiptParams{ID: receipt.ID, Result: stored}); err != nil {
			return nil, err
		}
	}
	if m.ValidateResult != nil {
		if err := m.ValidateResult(output); err != nil {
			return nil, &operations.Error{Code: operations.Unavailable}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return output, nil
}

var _ operations.MutationRunner = (*Postgres)(nil)
