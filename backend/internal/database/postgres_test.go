package database_test

import (
	"testing"

	"github.com/furkanmeclis/traccia/backend/internal/database/db"
)

func TestQuerierInterfaceCompiles(t *testing.T) {
	t.Parallel()

	var _ db.Querier = (*db.Queries)(nil)
}
