// Command product-contracts prints the bound product operation contracts without
// starting services or connecting to storage. Transport adapters consume the same catalog.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/furkanmeclis/traccia/backend/internal/modules/workspaces/usecase"
)

func main() {
	registry, err := usecase.New(nil).Registry()
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
