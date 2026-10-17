package contractcoverage_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	teams "github.com/furkanmeclis/traccia/backend/internal/modules/teams/usecase"
	"github.com/furkanmeclis/traccia/backend/internal/modules/workspaces"
	"github.com/furkanmeclis/traccia/backend/internal/modules/workspaces/usecase"
	coverage "github.com/furkanmeclis/traccia/backend/internal/platform/contractcoverage"
	"github.com/furkanmeclis/traccia/backend/internal/platform/mcpbindings"
	"github.com/furkanmeclis/traccia/backend/internal/product"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type routeRecorder struct {
	mux    *http.ServeMux
	routes []coverage.Route
}

func (r *routeRecorder) Handle(pattern string, handler http.Handler) {
	parts := strings.SplitN(pattern, " ", 2)
	identity, ok := handler.(interface{ OperationID() string })
	if !ok {
		panic("unidentified product handler")
	}
	r.routes = append(r.routes, coverage.Route{OperationID: identity.OperationID(), Method: parts[0], Path: parts[1]})
	r.mux.Handle(pattern, handler)
}

// Capture before SDK client decoding into any. Schema bounds, not just results,
// may exceed 2^53 and must survive independent discovery without rounding.
type captureTransport struct {
	mcp.Transport
	pages chan json.RawMessage
}

func (t captureTransport) Connect(ctx context.Context) (mcp.Connection, error) {
	c, e := t.Transport.Connect(ctx)
	if e != nil {
		return nil, e
	}
	return &captureConnection{Connection: c, pages: t.pages}, nil
}

type captureConnection struct {
	mcp.Connection
	pages chan json.RawMessage
}

func (c *captureConnection) Read(ctx context.Context) (jsonrpc.Message, error) {
	msg, err := c.Connection.Read(ctx)
	if err == nil {
		if response, ok := msg.(*jsonrpc.Response); ok && len(response.Result) > 0 {
			doc, e := coverage.Decode(response.Result)
			if e == nil && doc["tools"] != nil {
				c.pages <- append(json.RawMessage(nil), response.Result...)
			}
		}
	}
	return msg, err
}

func TestProductionContractConformance(t *testing.T) {
	workspace := usecase.New(nil)
	registry, err := product.Registry(workspace, teams.New(nil, workspace))
	if err != nil {
		t.Fatal(err)
	}
	recorder := &routeRecorder{mux: http.NewServeMux()}
	if err := workspaces.RegisterRegistryRoutes(recorder, registry, nil, nil); err != nil {
		t.Fatal(err)
	}
	// Request matching is observed from the live ServeMux, not its catalog names.
	for _, route := range recorder.routes {
		path := strings.ReplaceAll(strings.ReplaceAll(route.Path, "{workspaceID}", "11111111-1111-4111-8111-111111111111"), "{credentialID}", "22222222-2222-4222-8222-222222222222")
		for _, name := range []string{"teamID", "statusID", "labelID"} {
			path = strings.ReplaceAll(path, "{"+name+"}", "33333333-3333-4333-8333-333333333333")
		}
		req, _ := http.NewRequest(route.Method, path, nil)
		_, pattern := recorder.mux.Handler(req)
		if pattern != route.Method+" "+route.Path {
			t.Fatalf("%s mounted route mismatch %q", route.OperationID, pattern)
		}
	}
	server, err := mcpbindings.NewServer(registry, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	st, ct := mcp.NewInMemoryTransports()
	ss, err := server.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ss.Close() }()
	pages := make(chan json.RawMessage, 20)
	client := mcp.NewClient(&mcp.Implementation{Name: "contract-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, captureTransport{Transport: ct, pages: pages}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close() }()
	var tools []coverage.Tool
	params := &mcp.ListToolsParams{}
	for {
		page, err := session.ListTools(ctx, params)
		if err != nil {
			t.Fatal(err)
		}
		raw := <-pages
		doc, err := coverage.Decode(raw)
		if err != nil {
			t.Fatal(err)
		}
		for _, value := range doc["tools"].([]any) {
			tool := value.(coverage.Object)
			meta := tool["_meta"].(coverage.Object)
			version, err := strconv.Atoi(fmt.Sprint(meta["traccia/contract_version"]))
			if err != nil {
				t.Fatal(err)
			}
			tools = append(tools, coverage.Tool{Name: tool["name"].(string), Input: tool["inputSchema"], Output: tool["outputSchema"], Error: meta["traccia/error_schema"], OperationID: meta["traccia/operation_id"].(string), Version: version})
		}
		if page.NextCursor == "" {
			break
		}
		params.Cursor = page.NextCursor
	}
	doc, err := coverage.ReadOpenAPI("../../../docs/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	exclusions, err := coverage.ReadExclusions("testdata/legacy-http-exclusions.json")
	if err != nil {
		t.Fatal(err)
	}
	failures := coverage.Check(coverage.Surfaces{Contracts: registry.Catalog(), Routes: recorder.routes, Tools: tools, OpenAPI: doc, Exclusions: exclusions})
	if len(failures) > 0 {
		t.Fatal(strings.Join(failures, "\n"))
	}
}
