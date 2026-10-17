# Product workspace and tool credential foundation

This slice implements the persistent workspace boundary and ten shared application operations, including durable activity and delivery management. It is a partial delivery of T02/T41: OAuth client lifecycle, MCP transport, teams/projects and downstream storage/search/job/realtime isolation remain tracked roadmap work.

## Installation owner

The installation has one explicitly assigned owner in `product_installation`. Public registration settings and global platform administrator roles do not create product ownership. The inherited password and OAuth registration flows retain their existing `auth_settings` policy checks; their default migration disables registration.

Create or select an existing active human identity using the existing local account setup, then run from `backend/` with the normal private environment loaded:

```sh
go run ./cmd/bootstrap-product-owner -user <existing-active-user-uuid>
```

The command is repeatable for that same user and refuses to replace another owner. It does not print credentials. There is no HTTP bootstrap endpoint or first-registered-user ownership race. Only this owner can create workspaces. Ordinary users can list their explicit memberships and read workspaces where they are active members. A platform administrator still needs a workspace membership.

Workspace creation and initial owner membership commit in one transaction. Product workspaces are independent of inherited commercial organizations; no access expiration window, billing model or branch structure is imported. No member management UI or permission matrix is introduced here.

## Shared operations and routes

| Operation | HTTP route | Authorization |
| --- | --- | --- |
| `workspaces.create` | `POST /v1/workspaces` | Installation owner human identity |
| `workspaces.list` | `GET /v1/workspaces` | Active human identity; own memberships only |
| `workspaces.get` | `GET /v1/workspaces/{workspaceID}` | Current member; `workspace.read` for credentials |
| `workspaces.update` | `PATCH /v1/workspaces/{workspaceID}` | Current owner; `workspace.write` for credentials |
| `credentials.create` | `POST /v1/workspaces/{workspaceID}/credentials` | Current owner; `credentials.manage` for credentials |
| `credentials.list` | `GET /v1/workspaces/{workspaceID}/credentials` | Current owner; `credentials.read` for credentials |
| `credentials.revoke` | `DELETE /v1/workspaces/{workspaceID}/credentials/{credentialID}` | Current owner; `credentials.manage` for credentials |
| `activity.list` | `GET /v1/workspaces/{workspaceID}/activity` | Current member; `workspace.read` for credentials |
| `deliveries.list` | `GET /v1/workspaces/{workspaceID}/deliveries` | Current owner; `workspace.read` for credentials |
| `deliveries.replay` | `POST /v1/workspaces/{workspaceID}/deliveries/replay` | Current owner; `workspace.write` for credentials |

All routes execute the same typed operation registry. The five write routes require one `Idempotency-Key`; workspace PATCH requires `{name, expected_version}` and workspace results include `version`. The same key and complete input replay the original result. Changed input with that key returns `IDEMPOTENCY_CONFLICT`; a fresh key with a stale expected version returns `VERSION_CONFLICT` (both HTTP 409). The product authorizer reads current database state on every registry invocation, so future MCP adapters must call the registry with verified human context or untrusted bearer material through `WithToolCredential`, never construct their own membership grant. HTTP uses the API channel. MCP audience is accepted only when executing the MCP channel through the shared service; the network MCP server is not shipped in this slice.

Product capability names are local application policy, separate from global inherited `platform.*` RBAC grants. They are not platform permission slugs and do not grant administration access.

Use `limit` (1–100, default 20) and `after` (last `next_cursor` UUID) for list routes. Workspace and credential lists sort public UUIDs. Activity lists sort canonical history sequence numbers descending; delivery lists sort delivery sequence numbers descending. Their UUID cursors resolve the corresponding sequence only inside the authorized workspace. Activity ordering follows canonical history even when consumers finish out of order. No list imposes a total result cap. A final page has `has_more: false` and empty `next_cursor`. The workspace list includes `can_create`; workspace detail/create/update include `capabilities.update`, `capabilities.read_credentials` and `capabilities.manage_credentials`. These are current presentation hints; writes authorize again.

## Independent tool access

The raw `trc_` token contains 256 random bits. Only its SHA-256 hash is stored. A raw token appears in the create result, including authorized retries with the same idempotency key and identical input; list responses return metadata. Issuance happens once. Exact create results are encrypted in durable receipts using `APP_ENCRYPTION_KEY`, with authenticated context binding; see [operation retry and key retention policy](OPERATIONS.md#sensitive-results-key-retention-and-disclosure). Product responses use `Cache-Control: no-store`. Never put these tokens into frontend query caches, local storage, URLs or widget configuration.

Each credential binds one workspace and actor, an audience (`api` or `mcp`), explicit scopes, expiration and an independent revocation record. Allowed scopes in this slice are `workspace.read`, `workspace.write`, `credentials.read`, `credentials.manage`. The owner membership, active user, active workspace, expiration and revocation are checked from authoritative storage on every operation. The credential/member composite foreign key prevents attaching a credential to a user outside its workspace.

A credential with `credentials.manage` can create another credential without panel approval, provided every child scope is a subset of its own grants, audience is unchanged and expiration does not exceed its own. The maximum lifetime for any newly issued token is 365 days. Credentials have independent revocation lifecycles: revoking a parent does not revoke previously created credentials; workspace membership removal, user disabling and workspace archival invalidate all affected credentials immediately on the next operation.

Workspace-bound credentials cannot create workspaces or enumerate unrelated account workspaces, and they are never accepted by inherited platform routes. Future OAuth connections will carry separate identity/grant semantics rather than expanding a PAT into platform administration.

## Contract generation and acceptance

```sh
node scripts/sync-product-openapi.mjs
cd frontend && pnpm api:generate
```

The first command derives the product OpenAPI blocks from the actual bound Go catalog without starting services. Typed input/output schemas are closed; UUID wire representations are explicitly strings. Inherited platform OpenAPI blocks are preserved.

Run `go test ./internal/modules/workspaces/...` from `backend/`. Database acceptance requires `TEST_DATABASE_URL`; it creates a random private test schema, applies all migrations, round-trips migration 33 and cleans up only that schema. The test does not drop or migrate the shared database schema. Without the variable, the DB acceptance is explicitly skipped.

Coverage includes human JWT and scoped credentials, two workspaces, explicit installation ownership, a read-only member, global-administrator isolation, pagination, unknown actor fields, grant/audience/expiry non-escalation, hash-only storage, independent revocation and current user/member invalidation.

## Activity and delivery

Creation and renaming record canonical history and durable consumer deliveries in the mutation transaction. The workspace activity panel uses shared `activity.list`; owners can inspect and replay eligible deliveries through shared API/MCP-described operations without a panel approval step. See [DELIVERY.md](DELIVERY.md) for full contracts and guarantees.
