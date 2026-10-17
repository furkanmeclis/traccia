package workspaces_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/furkanmeclis/traccia/backend/internal/database/db"
	module "github.com/furkanmeclis/traccia/backend/internal/modules/workspaces"
	"github.com/furkanmeclis/traccia/backend/internal/modules/workspaces/repository"
	"github.com/furkanmeclis/traccia/backend/internal/modules/workspaces/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/authctx"
	"github.com/furkanmeclis/traccia/backend/internal/platform/dbtx"
	"github.com/furkanmeclis/traccia/backend/internal/platform/events"
	"github.com/furkanmeclis/traccia/backend/internal/platform/jwt"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/furkanmeclis/traccia/backend/internal/platform/outbox"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type deliveryFixture struct {
	pool     *pgxpool.Pool
	ctx      context.Context
	actor    uuid.UUID
	repo     *repository.Postgres
	registry *operations.Registry
	store    *outbox.Store
}

func newDeliveryFixture(t *testing.T) deliveryFixture {
	t.Helper()
	pool := isolatedDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	t.Cleanup(cancel)
	actor := uuid.New()
	if _, err := pool.Exec(ctx, "INSERT INTO users(uuid,email,password_hash,name,surname,status) VALUES($1,$2,'unused','Delivery','Owner','active')", actor, actor.String()+"@example.test"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.New(pool).BootstrapProductOwner(ctx, actor); err != nil {
		t.Fatal(err)
	}
	ctx = authctx.WithPrincipal(ctx, authctx.Principal{UserID: actor})
	repo := repository.New(pool, db.New(pool))
	registry, err := usecase.New(repo).Registry()
	if err != nil {
		t.Fatal(err)
	}
	return deliveryFixture{pool: pool, ctx: ctx, actor: actor, repo: repo, registry: registry, store: outbox.NewStore(pool, db.New(pool))}
}
func (f deliveryFixture) call(op, key string, w uuid.UUID, in string) ([]byte, error) {
	return f.registry.Execute(f.ctx, operations.Call{OperationID: op, Channel: operations.API, WorkspaceID: w, IdempotencyKey: key, Input: []byte(in)})
}
func (f deliveryFixture) create(t *testing.T, key string) uuid.UUID {
	t.Helper()
	raw, err := f.call("workspaces.create", key, uuid.Nil, `{"name":"Durable workspace"}`)
	if err != nil {
		t.Fatal(err)
	}
	var out usecase.WorkspaceOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out.Workspace.ID
}
func (f deliveryFixture) count(t *testing.T, table string, want int) {
	t.Helper()
	var n int
	if err := f.pool.QueryRow(f.ctx, "SELECT count(*) FROM "+table).Scan(&n); err != nil || n != want {
		t.Fatalf("%s count=%d want=%d err=%v", table, n, want, err)
	}
}
func (f deliveryFixture) claim(t *testing.T) db.EventDelivery {
	t.Helper()
	rows, err := f.store.Claim(f.ctx, 1, time.Minute)
	if err != nil || len(rows) != 1 {
		t.Fatalf("claim rows=%d err=%v", len(rows), err)
	}
	return rows[0]
}
func (f deliveryFixture) expire(t *testing.T, id int64) {
	t.Helper()
	if _, err := f.pool.Exec(f.ctx, "UPDATE event_deliveries SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1", id); err != nil {
		t.Fatal(err)
	}
	if err := f.store.Reclaim(f.ctx, 8); err != nil {
		t.Fatal(err)
	}
	f.ready(t)
}
func (f deliveryFixture) ready(t *testing.T) {
	t.Helper()
	if _, err := f.pool.Exec(f.ctx, "UPDATE event_deliveries SET available_at=clock_timestamp()-interval '1 second' WHERE status='pending'"); err != nil {
		t.Fatal(err)
	}
}
func (f deliveryFixture) consumers(t *testing.T) *outbox.Registry {
	t.Helper()
	r, err := f.repo.DurableConsumers()
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func checkOperationCode(t *testing.T, err error, code operations.Code) {
	t.Helper()
	var e *operations.Error
	if !errors.As(err, &e) || e.Code != code {
		t.Fatalf("wanted %s got %v", code, err)
	}
}

func TestWorkspaceMutationHistoryOutboxAtomic(t *testing.T) {
	f := newDeliveryFixture(t)
	const n = 8
	results := make([][]byte, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = f.call("workspaces.create", "same", uuid.Nil, `{"name":"Durable workspace"}`)
		}()
	}
	wg.Wait()
	for i := range n {
		if errs[i] != nil || !bytes.Equal(results[0], results[i]) {
			t.Fatalf("concurrent mutation differs: %v", errs[i])
		}
	}
	for _, table := range []string{"workspaces", "workspace_history", "outbox_events", "event_deliveries", "operation_receipts"} {
		f.count(t, table, 1)
	}
	var output usecase.WorkspaceOutput
	if err := json.Unmarshal(results[0], &output); err != nil {
		t.Fatal(err)
	}
	w := output.Workspace.ID
	_, err := f.call("workspaces.create", "same", uuid.Nil, `{"name":"Different"}`)
	checkOperationCode(t, err, operations.IdempotencyConflict)
	if _, err := f.call("workspaces.update", "update", w, `{"name":"Changed","expected_version":1}`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.call("workspaces.update", "update", w, `{"name":"Changed","expected_version":1}`); err != nil {
		t.Fatal(err)
	}
	_, err = f.call("workspaces.update", "stale", w, `{"name":"Stale","expected_version":1}`)
	checkOperationCode(t, err, operations.VersionConflict)
	for _, table := range []string{"workspace_history", "outbox_events", "event_deliveries", "operation_receipts"} {
		f.count(t, table, 2)
	}
	f.count(t, "workspace_activity", 0)
	worker := outbox.NewWorker(f.store, f.consumers(t), nil)
	for range 2 {
		if n, err := worker.Drain(f.ctx); err != nil || n != 1 {
			t.Fatalf("drain n=%d err=%v", n, err)
		}
	}
	f.count(t, "workspace_activity", 2)
	raw, err := f.call("activity.list", "", w, `{"limit":1,"after":""}`)
	if err != nil {
		t.Fatal(err)
	}
	var page usecase.ActivityList
	if err := json.Unmarshal(raw, &page); err != nil {
		t.Fatal(err)
	}
	if !page.HasMore || len(page.Items) != 1 || page.Items[0].Name != "Changed" || page.Items[0].ActorID != f.actor {
		t.Fatalf("activity page: %s", raw)
	}
	raw, err = f.call("activity.list", "", w, fmt.Sprintf(`{"limit":1,"after":%q}`, page.NextCursor))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &page); err != nil {
		t.Fatal(err)
	}
	if page.HasMore || len(page.Items) != 1 || page.Items[0].EventName != "workspaces.created" {
		t.Fatalf("activity next page: %s", raw)
	}
}
func TestWorkspaceEventInsertFailuresRollbackEverything(t *testing.T) {
	for _, table := range []string{"workspace_history", "outbox_events", "event_deliveries"} {
		t.Run(table, func(t *testing.T) {
			f := newDeliveryFixture(t)
			statement := fmt.Sprintf(`CREATE FUNCTION t04_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected failure'; END $$; CREATE TRIGGER t04_fail BEFORE INSERT ON %s FOR EACH ROW EXECUTE FUNCTION t04_fail()`, table)
			if _, err := f.pool.Exec(f.ctx, statement); err != nil {
				t.Fatal(err)
			}
			if _, err := f.call("workspaces.create", "failed", uuid.Nil, `{"name":"Rollback"}`); err == nil {
				t.Fatal("expected failure")
			}
			for _, name := range []string{"workspaces", "workspace_members", "workspace_history", "outbox_events", "event_deliveries", "operation_receipts"} {
				f.count(t, name, 0)
			}
		})
	}
}
func TestDurableDeliveryFencingRetryReplayAndIndependentConsumers(t *testing.T) {
	f := newDeliveryFixture(t)
	if _, err := f.pool.Exec(f.ctx, "INSERT INTO event_consumers(name,version,event_names) VALUES('test.failure',1,ARRAY['workspaces.created'])"); err != nil {
		t.Fatal(err)
	}
	w := f.create(t, "create")
	production := f.repo.ActivityConsumer()
	registry, err := outbox.NewRegistry(production, outbox.Consumer{Name: "test.failure", Version: 1, Handle: func(context.Context, events.Event) error {
		return errors.New("provider secret must never reach status")
	}})
	if err != nil {
		t.Fatal(err)
	}
	worker := outbox.NewWorker(f.store, registry, nil)
	worker.MaxAttempts = 2
	// Both consumers have independent rows, and a failed handler cannot erase success.
	for range 2 {
		if _, err := worker.Drain(f.ctx); err != nil {
			t.Fatal(err)
		}
	}
	f.count(t, "workspace_activity", 1)
	var succeeded, pending int
	if err := f.pool.QueryRow(f.ctx, "SELECT count(*) FILTER(WHERE status='succeeded'),count(*) FILTER(WHERE status='pending') FROM event_deliveries").Scan(&succeeded, &pending); err != nil || succeeded != 1 || pending != 1 {
		t.Fatalf("independent status %d %d %v", succeeded, pending, err)
	}
	var deferred bool
	if err := f.pool.QueryRow(f.ctx, "SELECT available_at>updated_at FROM event_deliveries WHERE consumer_name='test.failure'").Scan(&deferred); err != nil || !deferred {
		t.Fatal("retry did not persist DB backoff")
	}
	if _, err := f.pool.Exec(f.ctx, "UPDATE event_deliveries SET available_at=clock_timestamp()+interval '1 hour' WHERE status='pending'"); err != nil {
		t.Fatal(err)
	}
	if rows, err := f.store.Claim(f.ctx, 1, time.Minute); err != nil || len(rows) != 0 {
		t.Fatal("backoff ignored")
	}
	f.ready(t)
	restarted := outbox.NewWorker(outbox.NewStore(f.pool, db.New(f.pool)), registry, nil)
	restarted.MaxAttempts = 2
	if _, err := restarted.Drain(f.ctx); err != nil {
		t.Fatal(err)
	}
	var failure db.EventDelivery
	rows, err := db.New(f.pool).ListEventDeliveries(f.ctx, db.ListEventDeliveriesParams{WorkspaceUuid: w, RowLimit: 10})
	if err != nil {
		t.Fatal(err)
	}
	var successID uuid.UUID
	for _, row := range rows {
		if row.ConsumerName == "test.failure" {
			failure = db.EventDelivery{ID: row.ID, Uuid: row.Uuid, Status: row.Status}
		} else {
			successID = row.Uuid
		}
	}
	if failure.Status != "quarantined" {
		t.Fatal("failed consumer was not quarantined")
	}
	raw, err := f.call("deliveries.list", "", w, `{"limit":1,"after":""}`)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("provider secret")) || bytes.Contains(raw, []byte("lease_token")) || bytes.Contains(raw, []byte("payload")) {
		t.Fatal("status leaked private data")
	}
	var listed usecase.DeliveryList
	if err := json.Unmarshal(raw, &listed); err != nil {
		t.Fatal(err)
	}
	if !listed.HasMore {
		t.Fatal("delivery pagination missing")
	}
	raw, err = f.call("deliveries.list", "", w, fmt.Sprintf(`{"limit":1,"after":%q}`, listed.NextCursor))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &listed); err != nil || listed.HasMore || len(listed.Items) != 1 {
		t.Fatal("delivery next page incorrect")
	}
	_, err = f.call("deliveries.replay", "success-replay", w, fmt.Sprintf(`{"delivery_id":%q}`, successID))
	checkOperationCode(t, err, operations.Conflict)
	replayBody := fmt.Sprintf(`{"delivery_id":%q}`, failure.Uuid)
	replayed, err := f.call("deliveries.replay", "replay", w, replayBody)
	if err != nil {
		t.Fatal(err)
	}
	d := f.claim(t)
	if d.ConsumerName != "test.failure" {
		t.Fatal("successful sibling was replayed")
	}
	// A repeated replay submission returns its receipt and cannot reset an active lease.
	again, err := f.call("deliveries.replay", "replay", w, replayBody)
	if err != nil || !bytes.Equal(replayed, again) {
		t.Fatal("replay operation did not retain receipt")
	}
	_, err = f.call("deliveries.replay", "active-new-key", w, replayBody)
	checkOperationCode(t, err, operations.Conflict)
	f.expire(t, d.ID)
	next := f.claim(t)
	if d.LeaseToken == next.LeaseToken {
		t.Fatal("reclaim did not rotate fencing token")
	}
	if err := f.store.Process(f.ctx, d, registry); !errors.Is(err, outbox.ErrLeaseLost) {
		t.Fatalf("stale worker processed: %v", err)
	}
	if err := f.store.Retry(f.ctx, d, 8, "consumer_failed"); !errors.Is(err, outbox.ErrLeaseLost) {
		t.Fatalf("stale retry accepted: %v", err)
	}
	n, err := db.New(f.pool).CompleteEventDelivery(f.ctx, db.CompleteEventDeliveryParams{ID: d.ID, LeaseToken: d.LeaseToken})
	if err != nil || n != 0 {
		t.Fatal("stale ACK accepted")
	}
	// The second consumer recovers without changing the production receipt/effect.
	recovered, err := outbox.NewRegistry(production, outbox.Consumer{Name: "test.failure", Version: 1, Handle: func(context.Context, events.Event) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.Process(f.ctx, next, recovered); err != nil {
		t.Fatal(err)
	}
	if err := f.store.Process(f.ctx, next, recovered); !errors.Is(err, outbox.ErrLeaseLost) {
		t.Fatal("completed delivery ran again")
	}
	f.count(t, "workspace_activity", 1)
	if err := f.pool.QueryRow(f.ctx, "SELECT count(*) FROM event_deliveries WHERE status='succeeded'").Scan(&succeeded); err != nil || succeeded != 2 {
		t.Fatal("consumer receipts lost")
	}
}

func TestDurableConsumerRollbackAndExpiredAcknowledgement(t *testing.T) {
	f := newDeliveryFixture(t)
	f.create(t, "create")
	production := f.repo.ActivityConsumer()
	failing := production
	failing.Handle = func(ctx context.Context, ev events.Event) error {
		if err := production.Handle(ctx, ev); err != nil {
			return err
		}
		return errors.New("after real projection")
	}
	r, err := outbox.NewRegistry(failing)
	if err != nil {
		t.Fatal(err)
	}
	d := f.claim(t)
	if err := f.store.Process(f.ctx, d, r); err == nil {
		t.Fatal("expected consumer failure")
	}
	f.count(t, "workspace_activity", 0)
	// Expiration before ACK rolls back the real projection as well.
	expiring := production
	expiring.Handle = func(ctx context.Context, ev events.Event) error {
		if err := production.Handle(ctx, ev); err != nil {
			return err
		}
		tx, _ := dbtx.Tx(ctx)
		_, err := tx.Exec(ctx, "UPDATE event_deliveries SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1", d.ID)
		return err
	}
	r, err = outbox.NewRegistry(expiring)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.Process(f.ctx, d, r); !errors.Is(err, outbox.ErrLeaseLost) {
		t.Fatalf("expired ACK: %v", err)
	}
	f.count(t, "workspace_activity", 0)
	if err := f.store.Process(f.ctx, d, f.consumers(t)); err != nil {
		t.Fatal(err)
	}
	f.count(t, "workspace_activity", 1)
}

func TestConcurrentDeliveryClaimsAndImmutableEventIdentity(t *testing.T) {
	f := newDeliveryFixture(t)
	for i := range 6 {
		f.create(t, fmt.Sprint(i))
	}
	var wg sync.WaitGroup
	claims := make(chan db.EventDelivery, 6)
	errs := make(chan error, 6)
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rows, err := f.store.Claim(f.ctx, 1, time.Minute)
			if err != nil {
				errs <- err
				return
			}
			if len(rows) != 1 {
				errs <- errors.New("missing claim")
				return
			}
			claims <- rows[0]
		}()
	}
	wg.Wait()
	close(claims)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	seen := map[int64]bool{}
	for d := range claims {
		if seen[d.ID] {
			t.Fatal("duplicate claim")
		}
		seen[d.ID] = true
		if err := f.store.Process(f.ctx, d, f.consumers(t)); err != nil {
			t.Fatal(err)
		}
	}
	f.count(t, "workspace_activity", 6)
	rows, err := f.pool.Query(f.ctx, "SELECT payload FROM outbox_events ORDER BY id LIMIT 1")
	if err != nil {
		t.Fatal(err)
	}
	if !rows.Next() {
		t.Fatal("missing event")
	}
	var raw []byte
	if err := rows.Scan(&raw); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	var ev events.Event
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&ev); err != nil {
		t.Fatal(err)
	}
	tx, err := f.pool.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(f.ctx) }()
	if err := f.store.Enqueue(f.ctx, tx, ev); err != nil {
		t.Fatal(err)
	}
	// PostgreSQL equality retains adjacent integers above JavaScript precision.
	ev.Payload["number"] = json.Number("9007199254740992")
	ev.EventID = uuid.New()
	if err := f.store.Enqueue(f.ctx, tx, ev); err != nil {
		t.Fatal(err)
	}
	ev.Payload["number"] = json.Number("9007199254740993")
	if err := f.store.Enqueue(f.ctx, tx, ev); !errors.Is(err, outbox.ErrIdentityConflict) {
		t.Fatal("large integer collision was accepted")
	}
	ev.Payload["name"] = "tampered"
	if err := f.store.Enqueue(f.ctx, tx, ev); !errors.Is(err, outbox.ErrIdentityConflict) {
		t.Fatalf("altered event accepted: %v", err)
	}
	if err := tx.Rollback(f.ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, "UPDATE outbox_events SET actor_uuid=NULL"); err == nil {
		t.Fatal("immutable actor changed")
	}
}

// The subprocess intentionally exits without running Go defers. The production
// handler writes a real projection before the exit in effect mode.
func TestDeliveryCrashChild(t *testing.T) {
	mode := os.Getenv("T04_CRASH_MODE")
	if mode == "" {
		return
	}
	ctx := context.Background()
	cfg, err := pgxpool.ParseConfig(os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = os.Getenv("T04_CRASH_SCHEMA") + ",public"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	store := outbox.NewStore(pool, db.New(pool))
	rows, err := store.Claim(ctx, 1, time.Minute)
	if err != nil || len(rows) != 1 {
		t.Fatalf("child claim: %v", err)
	}
	if mode == "claim" {
		os.Exit(29)
	}
	repo := repository.New(pool, db.New(pool))
	production := repo.ActivityConsumer()
	handler := production.Handle
	if mode != "commit" {
		production.Handle = func(ctx context.Context, ev events.Event) error {
			if err := handler(ctx, ev); err != nil {
				return err
			}
			os.Exit(29)
			return nil
		}
	}
	registry, err := outbox.NewRegistry(production)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Process(ctx, rows[0], registry); err != nil {
		t.Fatal(err)
	}
	os.Exit(29)
}
func TestWorkerProcessLossReclaimsWithoutDuplicateEffects(t *testing.T) {
	for _, mode := range []string{"claim", "effect", "commit"} {
		t.Run(mode, func(t *testing.T) {
			f := newDeliveryFixture(t)
			f.create(t, "create")
			var schema string
			if err := f.pool.QueryRow(f.ctx, "SELECT current_schema()").Scan(&schema); err != nil {
				t.Fatal(err)
			}
			child := exec.CommandContext(f.ctx, os.Args[0], "-test.run=^TestDeliveryCrashChild$")
			child.Env = append(os.Environ(), "T04_CRASH_MODE="+mode, "T04_CRASH_SCHEMA="+schema)
			output, err := child.CombinedOutput()
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) || exitErr.ExitCode() != 29 {
				t.Fatalf("child did not crash at intended boundary: %v %s", err, output)
			}
			if mode == "commit" {
				f.count(t, "workspace_activity", 1)
				restarted := outbox.NewWorker(f.store, f.consumers(t), nil)
				if n, err := restarted.Drain(f.ctx); n != 0 || err != nil {
					t.Fatal("post-commit crash redelivered")
				}
				return
			}
			f.count(t, "workspace_activity", 0)
			var id int64
			if err := f.pool.QueryRow(f.ctx, "SELECT id FROM event_deliveries WHERE status='processing'").Scan(&id); err != nil {
				t.Fatal(err)
			}
			// Database clock transitions avoid host/DB clock skew and wall-time sleeps.
			f.expire(t, id)
			d := f.claim(t)
			if d.Attempts != 2 {
				t.Fatal("claim attempt did not survive process death")
			}
			if err := f.store.Process(f.ctx, d, f.consumers(t)); err != nil {
				t.Fatal(err)
			}
			f.count(t, "workspace_activity", 1)
			restarted := outbox.NewWorker(outbox.NewStore(f.pool, db.New(f.pool)), f.consumers(t), nil)
			if n, err := restarted.Drain(f.ctx); n != 0 || err != nil {
				t.Fatal("completed process work redelivered")
			}
			f.count(t, "workspace_activity", 1)
		})
	}
}

func TestActivityDeliverySharedAuthorizationAndIsolation(t *testing.T) {
	f := newDeliveryFixture(t)
	w := f.create(t, "one")
	other := f.create(t, "two")
	worker := outbox.NewWorker(f.store, f.consumers(t), nil)
	for range 2 {
		if _, err := worker.Drain(f.ctx); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := f.call("activity.list", "", w, `{"limit":20,"after":""}`)
	if err != nil {
		t.Fatal(err)
	}
	var list usecase.ActivityList
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 1 || list.Items[0].WorkspaceID != w {
		t.Fatal("activity mixed workspaces")
	}
	foreignCursor := list.Items[0].ID
	raw, err = f.call("activity.list", "", other, fmt.Sprintf(`{"limit":20,"after":%q}`, foreignCursor))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &list); err != nil || len(list.Items) != 0 {
		t.Fatal("foreign cursor exposed activity")
	}
	var delivery uuid.UUID
	if err := f.pool.QueryRow(f.ctx, "SELECT uuid FROM event_deliveries WHERE workspace_uuid=$1", w).Scan(&delivery); err != nil {
		t.Fatal(err)
	}
	_, err = f.call("deliveries.replay", "foreign", other, fmt.Sprintf(`{"delivery_id":%q}`, delivery))
	checkOperationCode(t, err, operations.Conflict)
	outsider := uuid.New()
	if _, err := f.pool.Exec(f.ctx, "INSERT INTO users(uuid,email,password_hash,name,surname,status) VALUES($1,$2,'unused','Member','Only','active')", outsider, outsider.String()+"@example.test"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, "INSERT INTO workspace_members(workspace_id,user_id,role) SELECT w.id,u.id,'member' FROM workspaces w,users u WHERE w.uuid=$1 AND u.uuid=$2", w, outsider); err != nil {
		t.Fatal(err)
	}
	member := authctx.WithPrincipal(f.ctx, authctx.Principal{UserID: outsider})
	for _, channel := range []operations.Channel{operations.Panel, operations.API, operations.MCP} {
		if _, err := f.registry.Execute(member, operations.Call{OperationID: "activity.list", Channel: channel, WorkspaceID: w, Input: []byte(`{"limit":20,"after":""}`)}); err != nil {
			t.Fatal(err)
		}
		for _, op := range []string{"deliveries.list", "deliveries.replay"} {
			_, err := f.registry.Execute(member, operations.Call{OperationID: op, Channel: channel, WorkspaceID: w, Input: []byte(`{"limit":20,"after":""}`), IdempotencyKey: "member"})
			checkOperationCode(t, err, operations.Forbidden)
		}
	}
	// Actual HTTP adapter has the same list/query contract and owner policy.
	tokens, err := jwt.NewManager("delivery-test-signing-secret", time.Hour, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := tokens.IssueAccess(jwt.AccessInput{UserID: f.actor})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	if err := module.RegisterRoutes(mux, usecase.New(f.repo), tokens, loader{}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"activity", "deliveries"} {
		req := httptest.NewRequest("GET", "/v1/workspaces/"+w.String()+"/"+path+"?limit=1", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		res := httptest.NewRecorder()
		mux.ServeHTTP(res, req)
		if res.Code != 200 || !strings.Contains(res.Body.String(), `"items"`) {
			t.Fatalf("HTTP %s: %d %s", path, res.Code, res.Body.String())
		}
	}
	if _, err := f.pool.Exec(f.ctx, "UPDATE workspace_members SET active=false WHERE user_id=(SELECT id FROM users WHERE uuid=$1)", f.actor); err != nil {
		t.Fatal(err)
	}
	for _, op := range []string{"activity.list", "deliveries.list"} {
		_, err := f.call(op, "", w, `{"limit":20,"after":""}`)
		checkOperationCode(t, err, operations.Forbidden)
	}
}

func TestExpiredLeaseBudgetAndMissingConsumerNeverSucceed(t *testing.T) {
	f := newDeliveryFixture(t)
	f.create(t, "one")
	d := f.claim(t)
	for attempt := 1; attempt <= 2; attempt++ {
		if _, err := f.pool.Exec(f.ctx, "UPDATE event_deliveries SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1", d.ID); err != nil {
			t.Fatal(err)
		}
		if err := f.store.Reclaim(f.ctx, 2); err != nil {
			t.Fatal(err)
		}
		var status, code string
		var backoff bool
		if err := f.pool.QueryRow(f.ctx, "SELECT status,error_code,available_at>updated_at FROM event_deliveries WHERE id=$1", d.ID).Scan(&status, &code, &backoff); err != nil {
			t.Fatal(err)
		}
		if code != "lease_expired" || !backoff {
			t.Fatal("expired processing lost durable failure/backoff")
		}
		if attempt == 1 {
			if status != "pending" {
				t.Fatal("first lease expiry not retried")
			}
			f.ready(t)
			d = f.claim(t)
			if d.Attempts != 2 {
				t.Fatal("expired claim attempt budget reset")
			}
		} else if status != "quarantined" {
			t.Fatal("repeated expired handlers can retry forever")
		}
	}
	f.count(t, "workspace_activity", 0)
	f.create(t, "two")
	d = f.claim(t)
	if err := f.store.Process(f.ctx, d, nil); !errors.Is(err, outbox.ErrConsumerMissing) {
		t.Fatal("missing consumer succeeded")
	}
	if err := f.store.Retry(f.ctx, d, 1, "consumer_unavailable"); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := f.pool.QueryRow(f.ctx, "SELECT status FROM event_deliveries WHERE id=$1", d.ID).Scan(&status); err != nil || status != "quarantined" {
		t.Fatal("missing consumer not quarantined")
	}
	f.count(t, "workspace_activity", 0)
}
