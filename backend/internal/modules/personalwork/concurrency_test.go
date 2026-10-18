package personalwork_test

import (
	"bytes"
	"encoding/json"
	"github.com/furkanmeclis/traccia/backend/internal/database/db"
	repo "github.com/furkanmeclis/traccia/backend/internal/modules/personalwork/repository"
	pw "github.com/furkanmeclis/traccia/backend/internal/modules/personalwork/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/google/uuid"
	"testing"
)

func TestPersonalDefaultTTLReplayAndConcurrentCapture(t *testing.T) {
	f := newPersonalFixture(t)
	in := pw.PortfolioCreateInput{RequestID: uuid.New(), WorkspaceIDs: []uuid.UUID{f.w}, Definition: query(), Limit: 100}
	first, e := f.call("personal_portfolios.create", "", f.w, in)
	if e != nil {
		t.Fatal(e)
	}
	ops, e := pw.New(repo.New(f.pool, db.New(f.pool)), f.workspace).WithDefaultTTL(3600).Operations()
	if e != nil {
		t.Fatal(e)
	}
	reg, e := f.workspace.Registry(ops...)
	if e != nil {
		t.Fatal(e)
	}
	body, _ := json.Marshal(in)
	again, e := reg.Execute(f.ctx, operations.Call{OperationID: "personal_portfolios.create", Channel: operations.API, WorkspaceID: f.w, Input: body})
	if e != nil || !bytes.Equal(first, again) {
		t.Fatal("default changed read identity", e)
	}
	ttl := int32(3600)
	in.TTLSeconds = &ttl
	_, e = f.call("personal_portfolios.create", "", f.w, in)
	checkCode(t, e, operations.IdempotencyConflict)
	in.TTLSeconds = nil
	in.RequestID = uuid.New()
	type result struct {
		raw []byte
		e   error
	}
	done := make(chan result, 2)
	for range 2 {
		go func() { b, e := f.call("personal_portfolios.create", "", f.w, in); done <- result{b, e} }()
	}
	a, b := <-done, <-done
	if a.e != nil || b.e != nil || !bytes.Equal(a.raw, b.raw) {
		t.Fatal(a.e, b.e)
	}
	var n int
	if e = f.pool.QueryRow(f.ctx, "SELECT count(*) FROM personal_portfolio_sessions WHERE request_uuid=$1", in.RequestID).Scan(&n); e != nil || n != 1 {
		t.Fatal(n, e)
	}
}
func TestPersonalFavoriteFirstCASAndAtomicJournalRollback(t *testing.T) {
	f := newPersonalFixture(t)
	in := pw.FavoriteSetInput{Target: pw.Target{Kind: "workspace", ID: f.w}, Active: true}
	done := make(chan error, 2)
	for range 2 {
		go func() { _, e := f.call("favorites.set", uuid.NewString(), f.w, in); done <- e }()
	}
	a, b := <-done, <-done
	if (a == nil) == (b == nil) {
		t.Fatal(a, b)
	}
	if a != nil {
		checkCode(t, a, operations.VersionConflict)
	} else {
		checkCode(t, b, operations.VersionConflict)
	}
	raw, e := f.call("favorites.list", "", f.w, pw.FavoriteListInput{Limit: 100})
	fav := decodeResult[pw.FavoriteListOutput](t, raw, e).Items[0]
	var beforeEvents int
	if e = f.pool.QueryRow(f.ctx, "SELECT count(*) FROM outbox_events").Scan(&beforeEvents); e != nil {
		t.Fatal(e)
	}
	if _, e = f.pool.Exec(f.ctx, `CREATE FUNCTION fail_favorite_journal() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'forced favorite rollback';END $$;CREATE TRIGGER fail_favorite_journal BEFORE INSERT ON personal_favorite_history FOR EACH ROW EXECUTE FUNCTION fail_favorite_journal()`); e != nil {
		t.Fatal(e)
	}
	in.Active = false
	in.ExpectedRevision = &fav.Revision
	key := uuid.NewString()
	_, e = f.call("favorites.set", key, f.w, in)
	checkCode(t, e, operations.Internal)
	raw, e = f.call("favorites.list", "", f.w, pw.FavoriteListInput{Limit: 100})
	retained := decodeResult[pw.FavoriteListOutput](t, raw, e).Items[0]
	if retained.Revision != fav.Revision || !retained.Active {
		t.Fatal("journal failure leaked favorite")
	}
	var events, journals, receipts int
	if e = f.pool.QueryRow(f.ctx, "SELECT (SELECT count(*) FROM outbox_events),(SELECT count(*) FROM personal_favorite_history),(SELECT count(*) FROM operation_receipts WHERE idempotency_key=$1)", key).Scan(&events, &journals, &receipts); e != nil || events != beforeEvents || journals != 1 || receipts != 0 {
		t.Fatal(events, journals, receipts, e)
	}
	if _, e = f.pool.Exec(f.ctx, "DROP TRIGGER fail_favorite_journal ON personal_favorite_history;DROP FUNCTION fail_favorite_journal()"); e != nil {
		t.Fatal(e)
	}
	raw, e = f.call("favorites.set", key, f.w, in)
	after := decodeResult[pw.FavoriteSetOutput](t, raw, e).Favorite
	if after.Active || after.Revision == fav.Revision {
		t.Fatal(after)
	}
}
