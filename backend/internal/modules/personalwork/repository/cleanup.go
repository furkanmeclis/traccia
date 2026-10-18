package repository

import (
	"context"
	"github.com/jackc/pgx/v5"
	"time"
)

// Cleanup commits a bounded batch independently of user captures. Header locks
// fence active page readers; permanent request tombstones survive item expiry.
func (r *Postgres) Cleanup(ctx context.Context, limit int32) (int64, error) {
	if limit < 1 || limit > 1000 {
		return 0, invalidCursor()
	}
	tx, e := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if e != nil {
		return 0, e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	n, e := r.q.WithTx(tx).CleanupPortfolioItems(ctx, limit)
	if e != nil {
		return 0, e
	}
	if _, e = r.q.WithTx(tx).CompactPortfolioSessions(ctx); e != nil {
		return 0, e
	}
	if e = tx.Commit(ctx); e != nil {
		return 0, e
	}
	return n, nil
}
func (r *Postgres) RunCleanup(ctx context.Context, onError func(error)) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		if _, e := r.Cleanup(ctx, 1000); e != nil && ctx.Err() == nil && onError != nil {
			onError(e)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
