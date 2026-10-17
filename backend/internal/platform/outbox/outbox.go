// Package outbox persists immutable events and independently fenced consumer receipts.
// The in-process events.Bus is deliberately not an acknowledgement boundary.
package outbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/furkanmeclis/traccia/backend/internal/database/db"
	"github.com/furkanmeclis/traccia/backend/internal/platform/dbtx"
	"github.com/furkanmeclis/traccia/backend/internal/platform/events"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

const DefaultLease = 30 * time.Second
const DefaultInterval = 500 * time.Millisecond
const DefaultMaxAttempts = 8

var ErrLeaseLost = errors.New("outbox: delivery lease lost")
var ErrIdentityConflict = errors.New("outbox: event identity conflict")
var ErrConsumerMissing = errors.New("outbox: consumer unavailable")

// Handler must confine effects to repositories joining dbtx.Tx(ctx). Network calls
// cannot share this transaction and require destination event-ID deduplication.
type Handler func(context.Context, events.Event) error
type Consumer struct {
	Name    string
	Version int32
	Handle  Handler
}
type Registry struct{ handlers map[string]Handler }

func key(name string, version int32) string { return fmt.Sprintf("%s.v%d", name, version) }
func NewRegistry(consumers ...Consumer) (*Registry, error) {
	r := &Registry{handlers: map[string]Handler{}}
	for _, c := range consumers {
		k := key(c.Name, c.Version)
		if c.Name == "" || c.Version < 1 || c.Handle == nil {
			return nil, fmt.Errorf("outbox: invalid consumer %s", k)
		}
		if _, ok := r.handlers[k]; ok {
			return nil, fmt.Errorf("outbox: duplicate consumer %s", k)
		}
		r.handlers[k] = c.Handle
	}
	return r, nil
}

type Store struct {
	pool *pgxpool.Pool
	q    *db.Queries
}

func NewStore(pool *pgxpool.Pool, q *db.Queries) *Store { return &Store{pool: pool, q: q} }

// Enqueue requires the caller's transaction. Event ID, actor and workspace must
// already be resolved; retries never invent a replacement identity.
func (s *Store) Enqueue(ctx context.Context, tx pgx.Tx, ev events.Event) error {
	if tx == nil {
		tx, _ = dbtx.Tx(ctx)
	}
	if tx == nil {
		return errors.New("outbox: transaction required")
	}
	if err := events.ValidateEventName(ev.Name); err != nil {
		return err
	}
	if ev.EventID == uuid.Nil || ev.WorkspaceID == uuid.Nil || ev.ActorID == uuid.Nil || ev.Version < 1 || ev.OccurredAt.IsZero() {
		return errors.New("outbox: complete event identity required")
	}
	body, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("outbox: marshal: %w", err)
	}
	q := s.q.WithTx(tx)
	n, err := q.InsertOutboxEvent(ctx, db.InsertOutboxEventParams{Uuid: ev.EventID, EventName: ev.Name, Payload: body, WorkspaceUuid: pgtype.UUID{Bytes: ev.WorkspaceID, Valid: true}, ActorUuid: pgtype.UUID{Bytes: ev.ActorID, Valid: true}, EventVersion: ev.Version, OccurredAt: pgtype.Timestamptz{Time: ev.OccurredAt, Valid: true}})
	if err != nil {
		return fmt.Errorf("outbox: insert: %w", err)
	}
	stored, err := q.GetOutboxEventByUUID(ctx, ev.EventID)
	if err != nil {
		return fmt.Errorf("outbox: read identity: %w", err)
	}
	if n == 0 {
		matches, err := q.OutboxPayloadMatches(ctx, db.OutboxPayloadMatchesParams{Uuid: ev.EventID, Payload: body})
		if err != nil {
			return err
		}
		if !matches || stored.EventName != ev.Name || stored.WorkspaceUuid.Bytes != ev.WorkspaceID || stored.ActorUuid.Bytes != ev.ActorID || stored.EventVersion != ev.Version {
			return ErrIdentityConflict
		}
		return nil // A duplicate enqueue must not enroll later-added consumers.
	}
	if err := q.InsertEventDeliveries(ctx, db.InsertEventDeliveriesParams{EventID: stored.ID, WorkspaceUuid: ev.WorkspaceID, EventName: ev.Name}); err != nil {
		return fmt.Errorf("outbox: insert deliveries: %w", err)
	}
	return nil
}

// Claim commits ownership before processing. A dead process leaves an expiring
// lease in PostgreSQL; another process can reclaim it without Redis state.
func (s *Store) Claim(ctx context.Context, limit int32, lease time.Duration) ([]db.EventDelivery, error) {
	if limit < 1 || lease <= 0 {
		return nil, errors.New("outbox: positive claim limits required")
	}
	return s.claim(ctx, limit, lease, DefaultMaxAttempts)
}

// Reclaim persists backoff/quarantine even when a process died or a handler
// exceeded its lease. Successful receipts and locked live handlers are skipped.
func (s *Store) Reclaim(ctx context.Context, maxAttempts int32) error {
	if maxAttempts < 1 {
		return errors.New("outbox: positive max attempts required")
	}
	_, err := s.q.ReclaimExpiredEventDeliveries(ctx, maxAttempts)
	return err
}
func (s *Store) claim(ctx context.Context, limit int32, lease time.Duration, maxAttempts int32) ([]db.EventDelivery, error) {
	if err := s.Reclaim(ctx, maxAttempts); err != nil {
		return nil, err
	}
	return s.q.ClaimEventDeliveries(ctx, db.ClaimEventDeliveriesParams{BatchLimit: limit, LeaseMs: lease.Milliseconds()})
}
func (s *Store) Process(ctx context.Context, delivery db.EventDelivery, registry *Registry) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	q := s.q.WithTx(tx)
	locked, err := q.LockEventDelivery(ctx, db.LockEventDeliveryParams{ID: delivery.ID, LeaseToken: delivery.LeaseToken})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrLeaseLost
	}
	if err != nil {
		return err
	}
	row, err := q.GetOutboxEvent(ctx, locked.EventID)
	if err != nil {
		return err
	}
	var ev events.Event
	decoder := json.NewDecoder(bytes.NewReader(row.Payload))
	decoder.UseNumber()
	if err := decoder.Decode(&ev); err != nil {
		return fmt.Errorf("outbox: invalid envelope: %w", err)
	}
	if ev.EventID != row.Uuid || ev.WorkspaceID != locked.WorkspaceUuid || ev.WorkspaceID != uuid.UUID(row.WorkspaceUuid.Bytes) || ev.ActorID != uuid.UUID(row.ActorUuid.Bytes) || ev.Name != row.EventName || ev.Version != row.EventVersion {
		return ErrIdentityConflict
	}
	if registry == nil {
		return ErrConsumerMissing
	}
	handler, ok := registry.handlers[key(locked.ConsumerName, locked.ConsumerVersion)]
	if !ok {
		return ErrConsumerMissing
	}
	if err := handler(dbtx.WithTx(ctx, tx), ev); err != nil {
		return fmt.Errorf("outbox: consumer: %w", err)
	}
	n, err := q.CompleteEventDelivery(ctx, db.CompleteEventDeliveryParams{ID: locked.ID, LeaseToken: locked.LeaseToken})
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrLeaseLost
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *Store) Retry(ctx context.Context, d db.EventDelivery, maxAttempts int32, code string) error {
	// Only stable allowlisted errors reach product status; raw handler errors may
	// contain provider secrets and are intentionally never persisted here.
	if code != "consumer_unavailable" && code != "invalid_event" {
		code = "consumer_failed"
	}
	n, err := s.q.RetryEventDelivery(ctx, db.RetryEventDeliveryParams{ID: d.ID, LeaseToken: d.LeaseToken, MaxAttempts: maxAttempts, ErrorCode: code})
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrLeaseLost
	}
	return nil
}

type Worker struct {
	store       *Store
	registry    *Registry
	log         *slog.Logger
	Lease       time.Duration
	Interval    time.Duration
	MaxAttempts int32
}

func NewWorker(store *Store, registry *Registry, log *slog.Logger) *Worker {
	if log == nil {
		log = slog.Default()
	}
	return &Worker{store: store, registry: registry, log: log, Lease: DefaultLease, Interval: DefaultInterval, MaxAttempts: DefaultMaxAttempts}
}

// Drain claims one item at a time so batch waiting cannot consume another item's lease.
func (w *Worker) Drain(ctx context.Context) (int, error) {
	rows, err := w.store.claim(ctx, 1, w.Lease, w.MaxAttempts)
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}
	d := rows[0]
	if err := w.store.Process(ctx, d, w.registry); err != nil {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		if errors.Is(err, ErrLeaseLost) {
			return 0, nil
		}
		code := "consumer_failed"
		if errors.Is(err, ErrConsumerMissing) {
			code = "consumer_unavailable"
		}
		if errors.Is(err, ErrIdentityConflict) {
			code = "invalid_event"
		}
		if retryErr := w.store.Retry(ctx, d, w.MaxAttempts, code); retryErr != nil && !errors.Is(retryErr, ErrLeaseLost) {
			return 0, retryErr
		}
		w.log.Warn("event_delivery_retry", "delivery_id", d.Uuid, "consumer", d.ConsumerName, "error_code", code)
		return 0, nil
	}
	return 1, nil
}
func (w *Worker) Run(ctx context.Context) {
	for ctx.Err() == nil {
		n, err := w.Drain(ctx)
		if err != nil && ctx.Err() == nil {
			w.log.Error("event_delivery_drain_failed", "error", err)
		}
		if n > 0 {
			continue
		}
		timer := time.NewTimer(w.Interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
