import test from "node:test";
import assert from "node:assert/strict";
import { createPortfolioStore, portfolioScope } from "./portfolio-state.ts";
const a = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
  b = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
  w = "cccccccc-cccc-cccc-cccc-cccccccccccc",
  other = "dddddddd-dddd-dddd-dddd-dddddddddddd",
  scope = { actorID: a, workspaceID: w, resource: "issue" };
function begin(store) {
  return store.begin(scope, (request_id) => ({
    path: "/personal-work/sessions",
    body: {
      request_id,
      workspace_ids: [w, other],
      definition: {
        resource: "issue",
        issue: {
          version: 1,
          archived: "active",
          search: "  İ e\u0301  ",
          filter: null,
          sort: [],
        },
        project: null,
        team: null,
      },
      assigned_to_me: true,
    },
  }));
}
function page(command) {
  return {
    session: {
      id: crypto.randomUUID(),
      request_id: command.body.request_id,
      anchor_workspace_id: w,
      resource: "issue",
      as_of: "2026-10-18T00:00:00Z",
      expires_at: "2026-10-18T01:00:00Z",
      total_count: "9007199254740993",
      workspaces: [
        { id: w, name: "First", total_count: "9007199254740992" },
        { id: other, name: "Second", total_count: "1" },
      ],
    },
    definition: structuredClone(command.body.definition),
    assigned_to_me: command.body.assigned_to_me,
    items: [],
  };
}
test("portfolio read retries immutable explicit scopes/request without a receipt and fences A-B-A old completions", () => {
  const store = createPortfolioStore();
  store.activate(a, w);
  const command = begin(store),
    id = portfolioScope(scope),
    old = store.ticket(id);
  command.body.workspace_ids.pop();
  store.failed(id, old, "lost", "uncertain");
  const retried = store.retry(id);
  assert.equal(retried.body.workspace_ids.length, 2);
  assert.equal(retried.body.request_id, old.requestID);
  assert.equal(retried.body.definition.project, null);
  assert(!("key" in retried));
  const current = store.ticket(id);
  assert(!store.success(id, old, page(retried)));
  assert(store.success(id, current, page(retried)));
  assert.equal(
    store.get(id).applied.page.session.total_count,
    "9007199254740993",
  );
  store.activate(b, w);
  store.activate(a, w);
  assert.equal(store.get(id).applied, null);
  assert(!store.success(id, current, page(retried)));
  assert.equal(store.get(id).status, "idle");
});
test("portfolio response verifies exact selected workspace set/counts and scope-change removes entire cached snapshot", () => {
  const store = createPortfolioStore();
  store.activate(a, w);
  const command = begin(store),
    id = portfolioScope(scope),
    ticket = store.ticket(id),
    valid = page(command);
  const foreign = structuredClone(valid);
  foreign.session.workspaces[1].id = b;
  assert.throws(() => store.success(id, ticket, foreign), /SCOPE/);
  const rounded = structuredClone(valid);
  rounded.session.total_count = "9007199254740992";
  assert.throws(() => store.success(id, ticket, rounded), /COUNT/);
  const changedQuery = structuredClone(valid);
  changedQuery.definition.issue.search = "changed text";
  assert.throws(() => store.success(id, ticket, changedQuery), /QUERY/);
  const changedAssigned = structuredClone(valid);
  changedAssigned.assigned_to_me = false;
  assert.throws(() => store.success(id, ticket, changedAssigned), /QUERY/);
  assert(store.success(id, ticket, valid));
  const next = begin(store),
    nextTicket = store.ticket(id);
  store.failed(id, nextTicket, "scope changed", "scope_changed");
  assert.equal(store.get(id).applied, null);
  assert.equal(store.get(id).status, "scope_changed");
  assert.deepEqual(store.get(id).command, next);
  assert.throws(() => store.retry(id), /NOT_RETRYABLE/);
});

test("fresh selected-scope omission aborts uncertain or pending reads and fences queued success while preserving command intent", () => {
  const store = createPortfolioStore();
  store.activate(a, w);
  const original = begin(store),
    id = portfolioScope(scope),
    old = store.ticket(id);
  assert(store.denyScopes(id, original.body.request_id));
  assert(old.signal.aborted);
  assert.equal(store.get(id).status, "scope_changed");
  assert.equal(store.get(id).applied, null);
  assert(!store.success(id, old, page(original)));
  assert.deepEqual(store.get(id).command, original);
  const next = begin(store),
    ticket = store.ticket(id);
  assert(!store.denyScopes(id, original.body.request_id));
  assert(!ticket.signal.aborted);
  store.failed(id, ticket, "lost", "uncertain");
  assert(store.denyScopes(id, next.body.request_id));
  assert.throws(() => store.retry(id), /NOT_RETRYABLE/);
});
