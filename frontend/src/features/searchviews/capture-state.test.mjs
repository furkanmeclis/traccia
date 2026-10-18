import test from "node:test";
import assert from "node:assert/strict";
import { createCaptureStore, captureScope } from "./capture-state.ts";
const scope = {
    actorID: "actor-a",
    workspaceID: "workspace-a",
    resource: "issue",
  },
  id = captureScope(scope);
const query = {
  version: 1,
  archived: "active",
  search: "İ e\u0301 👩🏽‍💻\r\n",
  filter: null,
  sort: [],
};
function setup() {
  const store = createCaptureStore();
  store.activate(scope.actorID, scope.workspaceID);
  return store;
}
const build = (request_id) => ({
  path: "/workspaces/workspace-a/search/issues/sessions",
  body: { request_id, query, ttl_seconds: null, limit: 100 },
});
function page(command, count = "9007199254740993") {
  return {
    session: {
      id: crypto.randomUUID(),
      request_id: command.body.request_id,
      workspace_id: scope.workspaceID,
      resource: "issue",
      as_of: "2026-10-18T00:00:00Z",
      expires_at: "2026-10-18T00:30:00Z",
      total_count: count,
    },
    query: command.body.query,
    items: [],
    has_more: false,
    next_cursor: "",
  };
}
test("two lost logical read responses retain exact request UUID/path/query/TTL/page body, separate from editable query and without a business receipt", () => {
  const store = setup(),
    first = store.begin(scope, build),
    original = structuredClone(first);
  first.body.query.search = "external mutation";
  assert.equal(store.get(id).command.body.query.search, query.search);
  for (let n = 0; n < 2; n++) {
    assert(store.failed(id, store.ticket(id), "lost", "uncertain"));
    const retry = store.retry(id);
    assert.deepEqual(retry, original);
    assert(!("key" in retry));
    assert(!("headers" in retry));
  }
  const p = page(original);
  assert(store.success(id, store.ticket(id), p));
  p.session.total_count = "0";
  assert.equal(
    store.get(id).applied.page.session.total_count,
    "9007199254740993",
  );
  assert.deepEqual(query, {
    version: 1,
    archived: "active",
    search: "İ e\u0301 👩🏽‍💻\r\n",
    filter: null,
    sort: [],
  });
});
test("expiry keeps applied as-of rows/count and captured query, blocks same-ID retry, and only explicit begin creates a new UUID", () => {
  const store = setup(),
    first = store.begin(scope, build),
    p = page(first);
  store.success(id, store.ticket(id), p);
  assert(store.expire(id, p.session.id));
  assert.equal(store.get(id).status, "expired");
  assert.equal(
    store.get(id).applied.page.session.total_count,
    p.session.total_count,
  );
  assert.throws(() => store.retry(id), /NOT_RETRYABLE/);
  const fresh = store.begin(scope, build);
  assert.notEqual(fresh.body.request_id, first.body.request_id);
  assert.equal(store.get(id).applied.page.session.id, p.session.id);
});
test("delayed prior capture/retry completion and actor-workspace A→B→A cannot publish over a newer request or revive a stale grant", () => {
  const store = setup(),
    first = store.begin(scope, build),
    old = store.ticket(id);
  store.activate("actor-b", "workspace-b");
  assert(old.signal.aborted);
  store.activate(scope.actorID, scope.workspaceID);
  const retry = store.retry(id),
    current = store.ticket(id);
  assert.equal(retry.body.request_id, first.body.request_id);
  assert.equal(store.success(id, old, page(first)), false);
  assert.equal(store.failed(id, old, "late", "rejected"), false);
  assert.equal(store.get(id).status, "pending");
  assert(store.success(id, current, page(retry)));
  const second = store.begin(scope, build);
  assert.equal(store.failed(id, current, "late previous", "forbidden"), false);
  assert.equal(store.get(id).command.body.request_id, second.body.request_id);
});
test("response scope and decimal-count validation reject foreign resource/workspace/request or rounded/noncanonical count before publication", () => {
  const store = setup(),
    command = store.begin(scope, build),
    ticket = store.ticket(id);
  for (const change of [
    { workspace_id: "foreign" },
    { resource: "team" },
    { request_id: crypto.randomUUID() },
    { total_count: "1e3" },
    { total_count: "01" },
    { total_count: 9007199254740992 },
  ]) {
    const p = page(command);
    Object.assign(p.session, change);
    assert.throws(() => store.success(id, ticket, p));
    assert.equal(store.get(id).applied, null);
  }
  assert(store.success(id, ticket, page(command, "0")));
});
test("late expiry from old applied page cannot mark a newer uncertain capture expired or prevent recovery of its exact original request", () => {
  const store = setup(),
    first = store.begin(scope, build),
    old = page(first);
  store.success(id, store.ticket(id), old);
  const next = store.begin(scope, build);
  store.failed(id, store.ticket(id), "lost new capture", "uncertain");
  assert.equal(store.expire(id, old.session.id), false);
  assert.equal(store.get(id).status, "uncertain");
  assert.deepEqual(store.retry(id), next);
});
