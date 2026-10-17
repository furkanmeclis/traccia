# Shared operation contracts

The shared registry implements the T01 contract foundation, T03 durable mutation boundary and T04 workspace delivery path. Ten workspace, credential, activity and delivery operations are bound to the shared registry and HTTP with MCP-described mappings. The panel exposes workspace and credential controls plus the activity feed; delivery status and replay are available through the shared operations and HTTP API without a panel approval step. The complete product inventory and network MCP transport remain roadmap work. Test fixture mappings are not live endpoints.

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
6. Bind every retryable mutation to the durable application runner described below. Workspace history and durable delivery are described in [DELIVERY.md](DELIVERY.md); a write effect alone does not implement these policies.

Product authentication, workspace persistence and HTTP adapters are implemented for the ten workspace operations. The full approved operation inventory, network MCP adapter and channel coverage CI remain outstanding. Framework tests exercise shared dispatch, authorization/revocation, numeric fidelity, schema failures, error sanitization and unbound operations with test-only use cases.


## Durable mutation retries and concurrency (T03 / TEC-790)

The product catalog uses contract version 2 for the retry/version contracts. The four product writes declare `idempotent: true` and require one `Idempotency-Key` header (1–200 bytes, no leading/trailing whitespace, commas, NUL or line breaks). Registry callers, including future MCP adapters, supply the same key through `Call.IdempotencyKey`. A key belongs to one complete immutable submission; retry with the identical input. Read operations do not create receipts.

Receipt identity is actor + optional workspace + operation ID + contract version + key. Separate unique indexes enforce account and workspace scopes, including the nullable account workspace. SHA-256 covers length-delimited operation ID/version/workspace and canonical complete JSON input. Canonicalization sorts object keys and removes insignificant whitespace using `UseNumber`: integers above 2^53, Unicode strings and array ordering retain their exact values. Numeric lexical representations remain distinct: `1`, `1.0` and `1e0` have different fingerprints. Duplicate keys, invalid UTF-8, unpaired Unicode surrogate escapes, unknown fields and invalid input schemas fail before a receipt claim. Path values represented in DTOs, including credential IDs, are part of the input; expected versions are fingerprinted too.

The repository runner opens one PostgreSQL READ COMMITTED transaction and injects its `dbtx` context into all authorization and repository queries. It reauthorizes inside the transaction, claims with `INSERT ON CONFLICT DO NOTHING`, then uses a separate SELECT so a waiting competitor observes the committed winner. It reauthorizes again after that wait. The handler, serialization and output schema validation finish before receipt completion and transaction commit. Domain failures, output validation failures and cancellation roll back the claim and mutation together. No pending receipt is committed and no lease permits a later duplicate mutation. Workspace creation joins the ambient transaction rather than prematurely committing its owner membership.

A winner stores exact output JSON bytes in BYTEA; equal input replays those bytes even after a service restart or later resource edits. Transport envelopes may contain a fresh request ID. Changed input with the same key returns `IDEMPOTENCY_CONFLICT` (HTTP 409). New workspace edits require `expected_version`; UPDATE matches that version and increments it atomically. A new key with a stale version returns `VERSION_CONFLICT` (HTTP 409), while retrying an earlier successful key returns its original workspace version. Clients retain their draft after conflict and explicitly reload current state before a new submission.

Authorization is current on every replay: revoked/expired credentials, disabled users, inactive workspace members, workspace mismatch and wrong audience cannot recover old results. Delegated credential creation also compares the requested child audience, scope subset and lifetime to the current caller's grants before exposing saved bytes. Absolute creation-time expiry validation runs only for new issuance, so an old successful receipt remains a receipt rather than becoming a newly validated creation request.

### Sensitive results, key retention and disclosure

Credential creation is one issuance with authorized idempotent replay, rather than a promise of one response delivery. The credential table still stores only SHA-256 token hashes and list operations expose metadata only. The exact validated create result is encrypted in its receipt with AES-256-GCM using `APP_ENCRYPTION_KEY`; authenticated associated data binds actor, workspace, operation/version, key and fingerprint. No raw token is written to receipt storage, logs, frontend query caches or durable browser storage. Only the creating component retains returned secret state; responses remain `Cache-Control: no-store`.

Receipts have no automatic expiration or pruning in this slice. Operators must retain the encryption key while any receipt uses it. Key rotation requires an explicit migration that authenticates and reencrypts all affected receipts with old and new keys available before switching the configured key. Missing/wrong keys, modified ciphertext, context mismatch or incomplete receipts fail closed with `OPERATION_UNAVAILABLE`; they never rerun a successful mutation or mint a replacement token. Never delete successful receipts to recover from a lost key: doing so would violate one issuance. Encryption key loss prevents sensitive replay until the original key is restored. A future retention policy must retain a durable spent-key tombstone if response bytes are removed, and a multi-key rotation facility remains separate work.

## Durable delivery

Workspace create/update now commit canonical history and outbox ingestion with their operation receipt. Shared `activity.list`, owner-only `deliveries.list` and idempotent `deliveries.replay` use the same application boundary. See [DELIVERY.md](DELIVERY.md) for lease ownership, atomic effects, backoff, quarantine, replay and transport limits.
