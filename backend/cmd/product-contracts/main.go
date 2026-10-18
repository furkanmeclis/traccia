// Command product-contracts prints the bound product operation contracts without
// starting services or connecting to storage. Transport adapters consume the same catalog.
package main

import (
	"encoding/json"
	"fmt"
	products "github.com/furkanmeclis/traccia/backend/internal/modules/products/usecase"
	"os"

	projects "github.com/furkanmeclis/traccia/backend/internal/modules/projects/usecase"
	teams "github.com/furkanmeclis/traccia/backend/internal/modules/teams/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/modules/workspaces/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/product"
)

func main() {
	workspace := usecase.New(nil)
	registry, err := product.Registry(workspace, teams.New(nil, workspace), projects.New(nil, workspace), products.New(nil, workspace))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(registry.Catalog()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
