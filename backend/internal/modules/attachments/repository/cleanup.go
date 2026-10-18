package repository

import (
	"context"
	"github.com/furkanmeclis/traccia/backend/internal/database/db"
	"github.com/furkanmeclis/traccia/backend/internal/platform/dbtx"
	"github.com/furkanmeclis/traccia/backend/internal/platform/storage"
	"time"
)

// Reconcile expires a bounded batch, then retries permanently retained tombstones.
// The SQL visibility fence commits before object deletion. Successful sweeps are
// scheduled again: a canceled remote Put may finish after any earlier sweep.
func (r *Postgres) Reconcile(ctx context.Context, batch int32) error {
	if batch < 1 || batch > 100 {
		return invalid("batch", "invalid")
	}
	if e := r.expire(ctx, batch); e != nil {
		return e
	}
	return r.clean(ctx, batch)
}
func (r *Postgres) expire(ctx context.Context, batch int32) error {
	tx, e := r.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	ctx = dbtx.WithTx(ctx, tx)
	q := r.q.WithTx(tx)
	rows, e := q.ClaimExpiredAttachmentUploads(ctx, batch)
	if e != nil {
		return e
	}
	for _, row := range rows {
		row, e = q.TombstoneAttachmentUpload(ctx, db.TombstoneAttachmentUploadParams{WorkspaceID: row.WorkspaceID, ID: row.ID, State: "expired"})
		if e != nil {
			return mapped(e)
		}
		p, e := q.ReadAttachmentUpload(ctx, db.ReadAttachmentUploadParams{WorkspaceID: row.WorkspaceID, ID: row.ID})
		if e != nil {
			return e
		}
		now, e := r.now(ctx)
		if e != nil {
			return e
		}
		if e = r.recordUpload(ctx, p.WorkspaceUuid, p.ActorUuid, row, "attachment_uploads.expired", uploadDTO(p, now)); e != nil {
			return e
		}
	}
	return tx.Commit(ctx)
}

type cleanupTask struct {
	workspace int64
	id        int64
	prefix    string
}

func (r *Postgres) clean(ctx context.Context, batch int32) error {
	tx, e := r.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := r.q.WithTx(tx)
	rows, e := q.ClaimAttachmentCleanup(ctx, batch)
	if e != nil {
		return e
	}
	now, e := q.DatabaseTime(ctx)
	if e != nil {
		return e
	}
	tasks := make([]cleanupTask, 0, len(rows))
	for _, row := range rows {
		p, e := q.ReadAttachmentUpload(ctx, db.ReadAttachmentUploadParams{WorkspaceID: row.WorkspaceID, ID: row.ID})
		if e != nil {
			return e
		}
		prefix := storage.AttachmentStagingPrefix(p.WorkspaceUuid.String(), row.Uuid.String())
		if row.State == "aborted" || row.State == "expired" {
			prefix = storage.AttachmentUploadPrefix(p.WorkspaceUuid.String(), row.Uuid.String())
		}
		tasks = append(tasks, cleanupTask{row.WorkspaceID, row.ID, prefix})
		if e = q.ScheduleAttachmentCleanup(ctx, db.ScheduleAttachmentCleanupParams{WorkspaceID: row.WorkspaceID, ID: row.ID, NextCleanupAt: stamp(now.Time.Add(r.options.CleanupInterval)), CleanupState: "pending", LastCleanupError: ""}); e != nil {
			return e
		}
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	for _, task := range tasks {
		deleteErr := r.store.DeletePrefix(ctx, task.prefix)
		if e = ctx.Err(); e != nil {
			return e
		}
		state, code, delay := "reconciled", "", r.options.CleanupInterval
		if deleteErr != nil {
			state, code, delay = "retrying", "storage_unavailable", time.Minute
		}
		tx, e := r.pool.Begin(ctx)
		if e != nil {
			return e
		}
		q := r.q.WithTx(tx)
		_, e = q.LockAttachmentUpload(ctx, db.LockAttachmentUploadParams{WorkspaceID: task.workspace, ID: task.id})
		if e == nil {
			now, e = q.DatabaseTime(ctx)
		}
		if e == nil {
			e = q.ScheduleAttachmentCleanup(ctx, db.ScheduleAttachmentCleanupParams{WorkspaceID: task.workspace, ID: task.id, NextCleanupAt: stamp(now.Time.Add(delay)), CleanupState: state, LastCleanupError: code})
		}
		if e == nil {
			e = tx.Commit(ctx)
		}
		_ = tx.Rollback(ctx)
		if e != nil {
			return e
		}
	}
	return nil
}

// RunCleanup does not own the object store or pool; the application owns their
// lifetime and cancellation. Every pass remains bounded and retries failures.
func (r *Postgres) RunCleanup(ctx context.Context, onError func(error)) {
	ticker := time.NewTicker(min(time.Minute, r.options.CleanupInterval))
	defer ticker.Stop()
	for {
		if e := r.Reconcile(ctx, 100); e != nil && ctx.Err() == nil && onError != nil {
			onError(e)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
