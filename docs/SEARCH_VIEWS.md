# Product search and personal views

Issues, projects and teams have separate typed SQL query operations. They share
workspace authorization with normal entity reads. These operations do not use
the inherited platform search service or a browser search index.

## Capture and traverse one dataset

`issue_queries.create`, `project_queries.create` and `team_queries.create` are
read-authorized POST operations at
`/v1/workspaces/{workspaceID}/search/{issues|projects|teams}/sessions`.
A workspace member or a credential with `workspace.read` can capture a query.
They require a `request_id` UUID, not a business `Idempotency-Key` header.

```json
{
  "request_id": "01234567-89ab-cdef-0123-456789abcdef",
  "query": {
    "version": 1,
    "archived": "active",
    "search": "İstanbul",
    "filter": {
      "kind": "not",
      "child": {"kind": "null", "field": "project_id", "op": "is_null"}
    },
    "sort": [{"field": "priority", "direction": "desc", "nulls": "last"}]
  },
  "ttl_seconds": null,
  "limit": 50
}
```

The response contains `session`, the accepted typed `query`, concrete `items`,
`has_more` and `next_cursor`. The session exposes `id`, `request_id`,
`workspace_id`, `resource`, `as_of`, `expires_at` and `total_count`.
Counts and numeric metadata versions/issue numbers are canonical decimal strings.
Rows contain captured metadata and no actionable capability grants. Navigate to
an entity to obtain its current state and permissions.

The first capture inserts every matching row, its immutable ordinal and its typed
snapshot in one SQL statement. The count is computed from those inserted rows.
There is no 5,000-row scan cap or client-side filtering/counting. Later pages read
only the captured rows through the resource-specific `.get` operation at
`/search/{issues|projects|teams}/sessions/{sessionID}`, with `limit` and `after`.
Changing live names, dates, priorities, membership fields or archive state cannot
reorder, add or remove rows in that dataset. The response is explicitly as-of,
not a claim about current entity state. A complete export can traverse these same
pages without mixing live data.

Request identity is private to the actor and workspace. The same `request_id`
and normalized request return the original session and first page after a lost
response, including after the configured default TTL changes. Changing the query,
resource, requested TTL or first-page limit conflicts. Concurrent retries publish
one complete dataset. Capture rechecks live authorization after waiting and before
commit; every page checks current membership and credential grants.

The default TTL is 30 minutes. `QUERY_SESSION_TTL` configures whole seconds from
60 seconds through 24 hours; a request may supply `ttl_seconds` in that range.
`search_queries.config` at `/search/config` publishes the effective limits and a
fresh `capabilities.manage` flag. Expiration produces HTTP 409 `CONFLICT` with
`details: [{"field":"session_id","code":"expired"}]`. It never silently starts a
new query. Explicit refresh uses a new `request_id` while retaining the caller's
query draft.

The standalone worker removes at most 1,000 expired item rows per pass, using
shared header locks to fence active pages. Cleanup commits independently of user
captures. After complete drain it clears the large query payload, retaining a
small scoped request/fingerprint/expiry tombstone. The same expired request ID
continues to fail explicitly. If a page crosses expiry, it fails explicitly rather
than returning a partially deleted page. Tombstone and snapshot identities cannot
be rebound.

## Closed filter language

A query has schema version 1, `archived` (`active`, `archived`, `any`), literal
`search`, nullable `filter` and an ordered `sort` array. Archive selection refers
to the queried entity's own archive flag.

Filters are a closed recursive union:

- `all` and `any` contain nonempty `children`; `not` contains one `child`.
- `uuid` selects a known UUID field with `in` or `not_in` and nonempty `ids`.
- `text` uses a known text field, `contains` or `eq`, and nonempty `value`.
- `enum` supports issue `priority`, `in` or `not_in`, and `values`.
- `number` supports issue `effort`, an int32 `number`, and `eq/lt/lte/gt/gte`.
- `date` supports start/target dates, an ISO date `value`, and those comparisons.
- `null` selects a known nullable field and `is_null` or `is_not_null`.

Issue UUID fields are id, team, status, assignee, project, milestone and labels.
Project UUID fields are id, lead and contributor teams. Team UUID filtering uses
id. Label/contributor `in` means any intersection; `not_in` means no intersection.
Their membership aggregation cannot duplicate result rows or inflate counts.
Text fields are issue title/identifier, project name/summary and team name/code.
The generated contracts enumerate the exact field names and valid combinations.

Evaluation is two-valued. Equality, membership and range predicates are false on
NULL. `not` is Boolean complement, so `not(in)` includes NULL; `not_in` has that
same meaning. Use an explicit null predicate to restrict it. Empty groups/sets,
unknown fields/properties/operators and invalid dates are rejected.

Matching uses PostgreSQL 18's built-in `pg_catalog.pg_unicode_fast` casefolding and
literal substring/equality operations. `%`, `_` and backslashes are literal.
This is Unicode default caseless matching, not Turkish-locale folding: I/i match,
İ folds to i plus combining dot, and dotless ı stays distinct. Straße/STRASSE match.
No Unicode normalization occurs; composed/decomposed forms remain distinct.
Original query and result text, including valid CR/LF in project summaries, remain
unchanged. Actual NUL is rejected before PostgreSQL persistence.

Sort entries choose a known field, `asc/desc` and `nulls:first/last`. Text ordering
uses deterministic C byte order, with a final UUID tie-break. It does not promise
locale alphabetic order. Priority uses the ascending domain rank
none → low → normal → high → urgent. Numeric effort and timestamps use numeric
ordering. An empty sort selects UUID ascending.

Limits are depth 8, 128 filter nodes, 5 sort keys, 100 UUIDs per set, 100 rows per
page and 256 Unicode code points per literal query/leaf. The canonical domain input limit is 128 KiB of JSON.stringify-compatible UTF-8
JSON, including path-owned domain fields. HTML characters and real U+2028/U+2029
do not consume extra escape bytes; literal backslash-u text remains distinct.
Integer negative zero measures as zero without changing the input. The separate
raw transport limit is 1 MiB. Config exposes `limits.max_input_bytes`,
`limits.max_raw_input_bytes` and `input_byte_policy:json_stringify_utf8_domain`.
HTTP and MCP apply iterative AST budgets before canonical size encoding and
recursive schema validation. Semantic overflow returns `input/semantic_bytes_limit`;
raw overflow returns `input/bytes_limit`.
Over-limit input fails visibly; it is never trimmed or truncated.

## Private saved views and columns

`saved_views.create/get/list/update/delete/restore` store a name, exactly one typed
resource query and ordered columns. Columns have a known `key` and width 80–800;
keys are unique and resource-specific. A view stores a definition, never an
expiring session ID. Applying it deliberately captures a new dataset.

Views are private to actor and workspace, including between two workspace owners.
Deletion retains the immutable UUID and content for explicit restore. Updates,
delete and restore use UUID `expected_revision` CAS. Lists use full SQL cursors
and retain anchors across soft deletion.

`column_preferences.get/set` use `/column-preferences` with a required `resource`
query/body field. An unset preference returns `revision:null` and `columns:[]`.
A set with `expected_revision:null` is create-only CAS; concurrent first sets
cannot overwrite one another. Later sets require the current UUID revision.

Every preference mutation requires current `workspace.write` and `CanManage`,
uses normal business idempotency receipts and rechecks permissions after blocking.
Members and read-only credentials can search but cannot mutate preferences.
Private snapshots/history, CAS, receipt and outbox commit together. Generic
workspace activity/realtime events contain only opaque identity/revision/version
and a generic label; they never expose personal names, filters or columns.

All operations share actual HTTP and official MCP SDK bindings. Network MCP
transport and palette integration remain separate roadmap work. Released entity
list, document and comment contracts are unchanged.
