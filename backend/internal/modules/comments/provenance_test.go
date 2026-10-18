package comments_test

import (
	"context"
	"encoding/json"
	"github.com/furkanmeclis/traccia/backend/internal/database/db"
	repo "github.com/furkanmeclis/traccia/backend/internal/modules/comments/repository"
	comment "github.com/furkanmeclis/traccia/backend/internal/modules/comments/usecase"
	doc "github.com/furkanmeclis/traccia/backend/internal/modules/documents/usecase"
	team "github.com/furkanmeclis/traccia/backend/internal/modules/teams/usecase"
	wr "github.com/furkanmeclis/traccia/backend/internal/modules/workspaces/repository"
	ws "github.com/furkanmeclis/traccia/backend/internal/modules/workspaces/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/crypto"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/furkanmeclis/traccia/backend/internal/product"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"testing"
)

func TestTrustedChannelMigrationAndTransactionReset(t *testing.T) {
	f := newCommentFixture(t)
	i := f.create(t, f.createTeam(t, "CHA").ID)
	conn, e := f.pool.Acquire(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	for _, suffix := range []string{"down", "up"} {
		raw, e := os.ReadFile("../../../migrations/000041_issue_comments." + suffix + ".sql")
		if e != nil {
			t.Fatal(e)
		}
		if suffix == "up" {
			if _, e = conn.Exec(f.ctx, "SELECT set_config('traccia.source_channel','mcp',false)"); e != nil {
				t.Fatal(e)
			}
		}
		if _, e = conn.Exec(f.ctx, string(raw)); e != nil {
			t.Fatal(e)
		}
	}
	var mislabeled int
	if e = conn.QueryRow(f.ctx, "SELECT count(*) FROM workspace_history WHERE source_channel<>'unknown'").Scan(&mislabeled); e != nil || mislabeled != 0 {
		t.Fatalf("legacy backfill mislabeled %d: %v", mislabeled, e)
	}
	if _, e = conn.Exec(f.ctx, "SELECT set_config('traccia.source_channel','',false)"); e != nil {
		t.Fatal(e)
	}
	conn.Release()
	cfg := f.pool.Config()
	cfg.MaxConns = 1
	p, e := pgxpool.NewWithConfig(f.ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(p.Close)
	box, _ := crypto.NewSecretBox("12345678901234567890123456789012")
	workspace := ws.New(wr.New(p, db.New(p), box))
	reg, e := product.Registry(workspace, team.New(nil, workspace), comment.New(repo.New(p, db.New(p)), workspace))
	if e != nil {
		t.Fatal(e)
	}
	call := func(channel operations.Channel, key string, in comment.CreateInput) ([]byte, error) {
		raw, _ := json.Marshal(in)
		return reg.Execute(f.ctx, operations.Call{OperationID: "comments.create", Channel: channel, WorkspaceID: f.w, Input: raw, IdempotencyKey: key})
	}
	reset := func(t *testing.T) {
		t.Helper()
		var value string
		if e := p.QueryRow(f.ctx, "SELECT COALESCE(current_setting('traccia.source_channel',true),'')").Scan(&value); e != nil || value != "" {
			t.Fatalf("transaction-local channel leaked %q: %v", value, e)
		}
	}
	in := comment.CreateInput{IssueID: i.ID, Format: "plate", SchemaVersion: 1, Content: []doc.Node{paragraph(uuid.NewString(), "original")}}
	key := uuid.NewString()
	first, e := call(operations.API, key, in)
	c := decodeResult[comment.Output](t, first, e).Comment
	reset(t)
	replay, e := call(operations.MCP, key, in)
	if e != nil || string(first) != string(replay) {
		t.Fatal("cross-channel receipt changed")
	}
	reset(t)
	raw, e := call(operations.MCP, uuid.NewString(), in)
	mcpComment := decodeResult[comment.Output](t, raw, e).Comment
	reset(t)
	for _, sample := range []struct {
		id      uuid.UUID
		channel string
	}{{c.ID, "api"}, {mcpComment.ID, "mcp"}} {
		var revisionChannel, historyChannel string
		if e = p.QueryRow(f.ctx, "SELECT r.channel,h.source_channel FROM comment_revisions r JOIN issue_comments c ON c.id=r.comment_id AND c.workspace_id=r.workspace_id JOIN workspace_history h ON h.event_id=r.event_id WHERE c.uuid=$1", sample.id).Scan(&revisionChannel, &historyChannel); e != nil || revisionChannel != sample.channel || historyChannel != sample.channel {
			t.Fatalf("channel persistence: %s %s %v", revisionChannel, historyChannel, e)
		}
	}
	if _, e = p.Exec(f.ctx, "CREATE FUNCTION fail_channel_write() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'forced'; END $$; CREATE TRIGGER fail_channel_write BEFORE INSERT ON workspace_history FOR EACH ROW EXECUTE FUNCTION fail_channel_write()"); e != nil {
		t.Fatal(e)
	}
	failedKey := uuid.NewString()
	if _, e = call(operations.MCP, failedKey, in); e == nil {
		t.Fatal("forced rollback ignored")
	}
	reset(t)
	if _, e = p.Exec(f.ctx, "DROP TRIGGER fail_channel_write ON workspace_history;DROP FUNCTION fail_channel_write()"); e != nil {
		t.Fatal(e)
	}
	raw, e = call(operations.API, failedKey, in)
	if decodeResult[comment.Output](t, raw, e).Comment.CreatedChannel != "api" {
		t.Fatal("rolled-back channel retained")
	}
	reset(t)
	var receipts int
	if e = p.QueryRow(context.Background(), "SELECT count(*) FROM operation_receipts WHERE operation_id='comments.create' AND idempotency_key=$1", key).Scan(&receipts); e != nil || receipts != 1 {
		t.Fatal("replay duplicated receipt")
	}
}
