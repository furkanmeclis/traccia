// Command bootstrap-product-owner explicitly assigns the single Traccia owner.
// An existing active identity is required. Re-running for the same user is safe;
// assigning a different user requires an explicit, reviewed database migration.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/furkanmeclis/traccia/backend/internal/config"
	"github.com/furkanmeclis/traccia/backend/internal/database"
	"github.com/google/uuid"
)

func main() {
	userFlag := flag.String("user", "", "existing active user UUID")
	flag.Parse()
	id, err := uuid.Parse(*userFlag)
	if err != nil || id == uuid.Nil {
		fatal("an existing active user UUID is required")
	}
	cfg, err := config.Load()
	if err != nil {
		fatal("configuration could not be loaded")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := database.NewPostgresPool(ctx, cfg.DB)
	if err != nil {
		fatal("database connection failed")
	}
	defer pool.Close()
	q := database.NewQueries(pool)
	if _, err := q.GetProductActor(ctx, id); err != nil {
		fatal("active identity not found")
	}
	if _, err := q.BootstrapProductOwner(ctx, id); err != nil {
		fatal("owner bootstrap failed")
	}
	if _, err := q.GetProductOwner(ctx, id); err != nil {
		fatal("another owner is already assigned; no changes made")
	}
	fmt.Println("Traccia installation owner is ready")
}
func fatal(message string) { fmt.Fprintln(os.Stderr, "bootstrap-product-owner: "+message); os.Exit(1) }
