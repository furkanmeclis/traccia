package issues_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/furkanmeclis/traccia/backend/internal/database/db"
	repo "github.com/furkanmeclis/traccia/backend/internal/modules/issues/repository"
	issue "github.com/furkanmeclis/traccia/backend/internal/modules/issues/usecase"
	projectrepo "github.com/furkanmeclis/traccia/backend/internal/modules/projects/repository"
	project "github.com/furkanmeclis/traccia/backend/internal/modules/projects/usecase"
	teamrepo "github.com/furkanmeclis/traccia/backend/internal/modules/teams/repository"
	team "github.com/furkanmeclis/traccia/backend/internal/modules/teams/usecase"
	wr "github.com/furkanmeclis/traccia/backend/internal/modules/workspaces/repository"
	ws "github.com/furkanmeclis/traccia/backend/internal/modules/workspaces/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/platform/authctx"
	"github.com/furkanmeclis/traccia/backend/internal/platform/crypto"
	"github.com/furkanmeclis/traccia/backend/internal/platform/operations"
	"github.com/furkanmeclis/traccia/backend/internal/product"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type issueFixture struct {
	pool          *pgxpool.Pool
	ctx           context.Context
	actor, w      uuid.UUID
	registry      *operations.Registry
	repo          *repo.Postgres
	workspace     *ws.Service
	workspaceRepo *wr.Postgres
}

func isolatedDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL required for project database acceptance")
	}
	ctx := context.Background()
	admin, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal("database configuration failed")
	}
	schema := "issue_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, e = admin.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _, _ = admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE"); admin.Close() })
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		t.Fatal("database configuration failed")
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	cfg.ConnConfig.RuntimeParams["application_name"] = schema
	cfg.MaxConns = 40
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal("database connection failed")
	}
	t.Cleanup(pool.Close)
	files, e := filepath.Glob("../../../migrations/*.up.sql")
	if e != nil || len(files) == 0 {
		t.Fatal("migrations unavailable")
	}
	for _, file := range files {
		raw, e := os.ReadFile(file)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = pool.Exec(ctx, string(raw)); e != nil {
			t.Fatalf("migration %s: %v", filepath.Base(file), e)
		}
	}
	for _, suffix := range []string{"down", "up"} {
		raw, e := os.ReadFile("../../../migrations/000037_issue_foundation." + suffix + ".sql")
		if e != nil {
			t.Fatal(e)
		}
		if _, e = pool.Exec(ctx, string(raw)); e != nil {
			t.Fatal(e)
		}
	}
	return pool
}
func newIssueFixture(t *testing.T) issueFixture {
	t.Helper()
	p := isolatedDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	actor := uuid.New()
	if _, e := p.Exec(ctx, "INSERT INTO users(uuid,email,password_hash,name,surname,status) VALUES($1,$2,'unused','Team','Owner','active')", actor, actor.String()+"@example.test"); e != nil {
		t.Fatal(e)
	}
	if _, e := db.New(p).BootstrapProductOwner(ctx, actor); e != nil {
		t.Fatal(e)
	}
	ctx = authctx.WithPrincipal(ctx, authctx.Principal{UserID: actor})
	box, _ := crypto.NewSecretBox("12345678901234567890123456789012")
	wrp := wr.New(p, db.New(p), box)
	wsvc := ws.New(wrp)
	r := repo.New(p, db.New(p))
	reg, e := product.Registry(wsvc, team.New(teamrepo.New(p, db.New(p)), wsvc), project.New(projectrepo.New(p, db.New(p)), wsvc), issue.New(r, wsvc))
	if e != nil {
		t.Fatal(e)
	}
	f := issueFixture{pool: p, ctx: ctx, actor: actor, registry: reg, repo: r, workspace: wsvc, workspaceRepo: wrp}
	raw, e := f.call("workspaces.create", uuid.NewString(), uuid.Nil, ws.CreateInput{Name: "Team tests"})
	if e != nil {
		t.Fatal(e)
	}
	var out ws.WorkspaceOutput
	if e = json.Unmarshal(raw, &out); e != nil {
		t.Fatal(e)
	}
	f.w = out.Workspace.ID
	return f
}
func (f issueFixture) call(op, key string, w uuid.UUID, in any) ([]byte, error) {
	raw, e := json.Marshal(in)
	if e != nil {
		return nil, e
	}
	return f.registry.Execute(f.ctx, operations.Call{OperationID: op, Channel: operations.API, WorkspaceID: w, IdempotencyKey: key, Input: raw})
}
func (f issueFixture) createTeam(t *testing.T, code string) team.Team {
	t.Helper()
	raw, e := f.call("teams.create", uuid.NewString(), f.w, team.CreateTeamInput{Name: "Team " + code, Code: code})
	if e != nil {
		t.Fatal(e)
	}
	var out team.TeamOutput
	if e = json.Unmarshal(raw, &out); e != nil {
		t.Fatal(e)
	}
	return out.Team
}
func checkCode(t *testing.T, e error, code operations.Code) {
	t.Helper()
	var op *operations.Error
	if !errors.As(e, &op) || op.Code != code {
		t.Fatalf("wanted %s got %v", code, e)
	}
}
func (f issueFixture) createProject(t *testing.T, ids ...uuid.UUID) project.Project {
	t.Helper()
	teams := make([]string, 0, len(ids))
	for _, id := range ids {
		teams = append(teams, id.String())
	}
	raw, e := f.call("projects.create", uuid.NewString(), f.w, project.CreateProjectInput{Name: "İstanbul projesi 🧭", Summary: "Project summary", TeamIDs: teams})
	if e != nil {
		t.Fatal(e)
	}
	var out project.ProjectOutput
	if e = json.Unmarshal(raw, &out); e != nil {
		t.Fatal(e)
	}
	return out.Project
}
func decodeResult[T any](t *testing.T, raw []byte, e error) T {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
	var out T
	if e = json.Unmarshal(raw, &out); e != nil {
		t.Fatal(e)
	}
	return out
}
