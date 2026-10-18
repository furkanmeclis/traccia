# Personal workspace

T14 is complete. Notification entry in the shell does not implement the later notification-delivery scope.

The personal-work module adds five shared API/MCP operations. Every route uses `/v1/workspaces/{workspaceID}` as its authorization and ownership context:

| Operation | HTTP |
| --- | --- |
| `personal_work.config` | `GET /personal-work/config` |
| `favorites.list` | `GET /favorites` |
| `favorites.set` | `POST /favorites` |
| `personal_portfolios.create` | `POST /personal-work/sessions` |
| `personal_portfolios.get` | `GET /personal-work/sessions/{sessionID}` |

Favorites are private to the actor and workspace. Targets are typed issue, project, team or workspace UUIDs. Each target must resolve inside the route workspace; a workspace target must equal that workspace. No caller URL or storage key is accepted. Archived targets remain readable and can be unfavorited. A first set requires `expected_revision: null` and `active: true`; later sets carry the current revision UUID. A stable favorite UUID survives toggles. Every write uses current `workspace.write`/management authorization and a normal idempotency receipt. Public activity carries only an opaque favorite identity and generic event metadata. Target labels and bindings stay in the private journal and actor-scoped reads.

Portfolio creation is a logical read POST without an Idempotency-Key. It accepts a required request UUID, an explicit nonempty set of at most 32 workspace UUIDs, a closed issue/project/team query definition, `assigned_to_me`, nullable TTL and page limit. The 32-workspace budget is explicit validation, never a truncation of workspace discovery. `assigned_to_me` is issue-only and adds a separate authenticated-actor predicate to the complete query. It cannot be overridden by caller filter branches.

The anchor may be absent from the selected set. It remains an authorization context and is never silently added to captured rows or counts. Each selected scope and the anchor are checked using the original principal, credential, audience and dispatcher channel after waits and before publication or returning a page. A workspace-bound credential never gains cross-workspace access.

One SQL statement captures all selected scopes into immutable typed metadata rows under one database snapshot. Empty selected workspaces have explicit zero counts. Counts and any exposed numbers/versions are decimal strings; actionable capabilities are not captured. The definition preserves T13 literal Unicode default casefold matching, no normalization, two-valued nullable filters, domain priority rank and C byte-order text sorting with UUID ties. The released T13 operation and resource enums remain unchanged.

The normalized selected set, query, assigned-to-me flag, requested nullable TTL and first-page limit define request identity. Concurrent retries publish once; altered requests conflict. A changed configured default TTL does not alter an existing null-TTL request. Expired request UUIDs retain small permanent tombstones and cannot silently start a new snapshot. Explicit refresh uses a new UUID.

Initial unavailable selected scopes fail closed without distinguishing nonexistent from unauthorized workspaces. Later selected-scope access changes return forbidden with `session_id/scope_changed`; registry rejection of the anchor or credential may return ordinary unauthorized/forbidden first. These failures return no snapshot rows, labels or counts. The UI must hide all cached server payload while keeping local unsaved drafts mounted.

Sessions default to 30 minutes, configurable from 60 seconds through 24 hours, and expose `as_of`/`expires_at`. Expiry returns conflict with `session_id/expired`. Pages contain at most 100 items and use immutable scoped ordinal cursors, surviving live entity edits, moves, archives and favorite toggles. An independent worker removes at most 1,000 expired item rows per pass and compacts at most ten drained headers. Shared header locks fence pages against cleanup; a page crossing expiry fails explicitly instead of succeeding with missing rows.

Portfolio inputs permit at most 1 MiB raw JSON and 128 KiB canonical UTF-8 domain JSON measured like `JSON.stringify`, including path-owned fields where applicable. HTML characters and real Unicode separators do not receive Go encoder escape inflation. Literal backslash escapes remain distinct. T13 depth/node/sort/set/text budgets and strict closed fields apply before recursive schema validation. Inputs are never silently trimmed, normalized or truncated.
