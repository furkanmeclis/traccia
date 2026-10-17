# Shared operation contracts

This is the first executable contract slice for T01 (TEC-788), not a completed product operation inventory. `backend/internal/platform/operations` is a library. The HTTP server, panel and MCP server do not yet register product operations through it. Test fixture mappings are not live endpoints.

## Contract and dispatch

`New[Input, Output]` takes concrete struct DTOs, stable metadata, a typed semantic validator and an optional typed use case handler. Input, output and public error contracts are concrete JSON Schema 2020-12 documents. A schema refiner can add constraints shared by both transports. A missing handler declares an operation with `available: false`; it never becomes callable because it has a mapping.

`NewRegistry` checks identifiers, positive versions, scope, effects, permissions and HTTP/MCP mappings, rejecting duplicate IDs, HTTP method/path pairs and MCP names. `Catalog` returns independent schema copies. The version describes the current contract; incompatible revisions require an explicit adapter migration rather than silently changing existing clients.

`Execute` verifies the selected channel and required scope, then invokes the injected authorizer on every call. The authorizer resolves the authenticated actor from context and verifies token status, membership and the operation permission. A requested workspace ID is never a grant. Missing actors and mismatched workspaces fail before reaching the handler. The handler receives a scope whose fields cannot be populated by transport code. Account operations reject a workspace selection; workspace operations require one.

The same binding executes for panel, API and MCP. It checks exactly one JSON value, valid UTF-8, duplicate object keys at every nesting level, required fields, unknown fields, schema constraints and typed domain validation. Duplicate keys are rejected before schema validation and typed decoding so repeated struct objects cannot merge into a different value in the use case. Output is serialized and validated before it is returned. Validation uses `json.Number`, preserving integers beyond 2^53; results are never truncated. Public errors carry stable codes and field codes rather than downstream messages. The original cause remains available through `errors.Unwrap` for diagnostics; adapters must not expose it to clients.

## Schemas and dependencies

- `github.com/google/jsonschema-go v0.4.3` infers schemas from the actual DTO types. Required fields follow JSON tags. It is also used by the Go MCP ecosystem.
- `github.com/santhosh-tekuri/jsonschema/v6 v6.0.3` compiles and validates schemas and JSON values. The compiler disables external resource loading. The second library is deliberate: the pinned inference library treats `json.Number` as a string during type validation, which would reject exact numeric input. Converting numbers to float64 would lose fidelity.
- Schema `format` and content annotations are not enabled as assertions. Semantic validators must enforce UUIDs, dates, domain relationships and other semantic rules. Do not use an annotation as authorization or validation.
- Nested dynamic data needs a deliberately reviewed schema. Struct inference is not proof that a domain contract is complete, and a generic map is not an acceptable top-level operation DTO.

## Adapter requirements before production wiring

1. Resolve authenticated context through the real session or tool-token adapter. Never accept an actor or membership grant from a JSON body.
2. Implement the authorizer with current token revocation, workspace membership and permissions (T02/T41). Inherited global platform permissions and client-selected organization headers do not establish this product scope.
3. Bind real application services. Preserve workspace scope in repositories, object storage, search, jobs and realtime. The registry is not a substitute for scoped queries and constraints.
4. Map HTTP path/query/body and MCP arguments to the same DTO. Emit the documented HTTP envelope, request IDs and localized user-facing messages. Register real routes in OpenAPI and generate frontend types in the same change.
5. Reconcile declared mappings with the actual HTTP and MCP registrations in CI (T05). A catalog entry alone proves no transport coverage. Add real domain operations, preferences and job lifecycle contracts as those services are implemented.
6. Add idempotency, optimistic concurrency, audit and durable delivery at the shared application boundary (T03/T04). A write effect in metadata does not implement these policies.

Production authentication, workspace persistence, the full approved operation inventory, HTTP/MCP adapters and channel coverage CI remain outstanding. Framework tests exercise shared dispatch, authorization/revocation, numeric fidelity, schema failures, error sanitization and unbound operations with test-only use cases.
