import test from "node:test";
import assert from "node:assert/strict";
import {
  createViewDraftStore,
  draftScope,
  viewDraftChanged,
} from "./draft-state.ts";
import { emptyQuery, defaultColumns } from "./query.ts";
import { createViewOperationStore } from "./operation-state.ts";
const scope = {
  actorID: "actor-a",
  workspaceID: "workspace-a",
  target: "columns:issue",
};
const command = {
  action: "columns.set",
  path: "/workspaces/workspace-a/column-preferences",
  body: {
    resource: "issue",
    expected_revision: null,
    columns: defaultColumns("issue"),
  },
};
function setup() {
  const store = createViewOperationStore();
  store.activate(scope.actorID, scope.workspaceID);
  return store;
}
test("concurrent first column set rejection retains null CAS and dirty ordered widths without rebasing to new read revision", async () => {
  const drafts = createViewDraftStore(),
    id = draftScope("actor-a", "workspace-a", "issue", "columns");
  drafts.ensure(id, "issue");
  drafts.patch(id, {
    columns: [
      { key: "title", width: 800 },
      { key: "identifier", width: 80 },
    ],
  });
  assert(viewDraftChanged(drafts.get(id)));
  const store = setup(),
    entry = store.begin(scope, "set", "set", command),
    frozen = structuredClone(entry.submission);
  store.failed(entry.id, store.ticket(entry.id), "VERSION_CONFLICT", 409);
  await store.review(entry.id, async () => true);
  assert(!store.get(entry.id).canRetry);
  assert.throws(() => store.retry(entry.id));
  assert.deepEqual(store.get(entry.id).submission, frozen);
  drafts.ensure(id, "issue", {
    name: "",
    query: emptyQuery(),
    columns: defaultColumns("issue"),
    revision: "concurrent-first-revision",
  });
  assert.equal(drafts.get(id).revision, null);
  assert.equal(drafts.get(id).columns[0].width, 800);
  store.discard(entry.id);
  assert(viewDraftChanged(drafts.get(id)));
});
test("two uncertain saved-view writes require fresh current review and keep exact action/path/body/revision/key", async () => {
  const store = setup(),
    entry = store.begin(
      { ...scope, target: "view:stable-uuid" },
      "update",
      "update",
      {
        action: "view.update",
        path: "/saved-views/stable-uuid/update",
        body: {
          expected_revision: "old",
          name: "  İ 👩🏽‍💻 ",
          definition: {
            resource: "issue",
            issue: emptyQuery(),
            project: null,
            team: null,
          },
          columns: defaultColumns("issue"),
        },
      },
    ),
    frozen = structuredClone(entry.submission);
  for (let i = 0; i < 2; i++) {
    store.failed(entry.id, store.ticket(entry.id), "lost");
    assert.throws(() => store.retry(entry.id));
    await store.review(entry.id, async () => true);
    assert.deepEqual(store.retry(entry.id), frozen);
    assert.throws(() => store.retry(entry.id));
  }
  assert(store.success(entry.id, store.ticket(entry.id)));
});
test("late completions and context-switched review cannot clear or authorize current view/column command", async () => {
  const store = setup(),
    entry = store.begin(scope, "set", "set", command),
    old = store.ticket(entry.id);
  store.activate("actor-b", "workspace-a");
  store.activate(scope.actorID, scope.workspaceID);
  await store.review(entry.id, async () => true);
  store.retry(entry.id);
  const active = store.ticket(entry.id);
  assert(!store.success(entry.id, old));
  assert(!store.failed(entry.id, old, "late", 409));
  assert.equal(store.signal(entry.id), active.signal);
  store.failed(entry.id, active, "lost");
  let release;
  const reviewed = store.review(
    entry.id,
    () => new Promise((resolve) => (release = resolve)),
  );
  store.activate(scope.actorID, "workspace-b");
  store.activate(scope.actorID, scope.workspaceID);
  release(true);
  assert.equal(await reviewed, false);
  assert(!store.get(entry.id).canRetry);
});
test("same view UUID excludes writes across resource changes; private draft scopes retain other resource and actor drafts", () => {
  const store = setup(),
    viewScope = { ...scope, target: "view:stable-uuid" };
  store.begin(viewScope, "update", "update", command);
  assert.throws(() => store.begin(viewScope, "delete", "delete", command));
  assert(
    store.begin({ ...scope, target: "columns:project" }, "set", "set", command),
  );
  const drafts = createViewDraftStore(),
    a = draftScope("a", "w", "issue"),
    b = draftScope("b", "w", "issue"),
    project = draftScope("a", "w", "project");
  for (const [id, resource] of [
    [a, "issue"],
    [b, "issue"],
    [project, "project"],
  ])
    drafts.ensure(id, resource);
  drafts.patch(a, {
    query: { ...emptyQuery(), search: "  retained\r\n" },
    raw: "invalid retained JSON {",
    error: "invalid",
  });
  assert.equal(drafts.get(b).query.search, "");
  assert.equal(drafts.get(project).query.search, "");
  assert.equal(drafts.get(a).raw, "invalid retained JSON {");
  assert(viewDraftChanged(drafts.get(a)));
});

test("invalid numeric draft spelling survives actor/resource return; explicit load clears auxiliary input without rebasing CAS", () => {
  const drafts = createViewDraftStore(),
    id = draftScope("a", "w", "issue", "view:uuid");
  drafts.ensure(id, "issue", {
    name: "saved",
    query: emptyQuery(),
    columns: defaultColumns("issue"),
    revision: "original",
  });
  drafts.patch(id, {
    query: {
      ...emptyQuery(),
      filter: {
        kind: "not",
        child: { kind: "number", field: "effort", op: "eq", number: NaN },
      },
    },
    numericInputs: { "filter.not": "-not-a-number" },
  });
  drafts.ensure(draftScope("b", "w", "issue"), "issue");
  drafts.ensure(draftScope("a", "w", "team"), "team");
  assert.equal(
    drafts.ensure(id, "issue").numericInputs["filter.not"],
    "-not-a-number",
  );
  const oldGeneration = drafts.get(id).generation;
  drafts.loadQuery(id, emptyQuery());
  assert.deepEqual(drafts.get(id).numericInputs, {});
  assert.equal(drafts.get(id).revision, "original");
  assert.equal(drafts.get(id).generation, oldGeneration + 1);
});

test("concurrent saved-view resource change cannot select a new draft or rebase its original CAS/query/columns", () => {
  const drafts = createViewDraftStore(),
    original = draftScope("actor", "workspace", "issue", "view:stable-uuid"),
    changed = draftScope("actor", "workspace", "project", "view:stable-uuid");
  assert.equal(original, changed);
  drafts.ensure(original, "issue", {
    name: "issue view",
    query: emptyQuery(),
    columns: defaultColumns("issue"),
    revision: "original-revision",
  });
  drafts.patch(original, {
    query: { ...emptyQuery(), search: "retained issue draft" },
    name: "dirty private name",
    columns: [
      { key: "title", width: 800 },
      { key: "identifier", width: 80 },
    ],
    raw: "retained invalid JSON {",
    numericInputs: { filter: "-" },
    error: "retained error",
    ttl: "60",
  });
  const before = structuredClone(drafts.get(original));
  const retained = drafts.ensure(changed, "project", {
    name: "concurrently changed",
    query: emptyQuery(),
    columns: defaultColumns("project"),
    revision: "new-revision",
  });
  assert.deepEqual(retained, before);
  assert.equal(retained.resource, "issue");
  assert.equal(retained.revision, "original-revision");
  assert.equal(retained.query.search, "retained issue draft");
  assert.equal(retained.columns[0].key, "title");
  drafts.reset(changed, "project", {
    name: "explicit reload",
    query: emptyQuery(),
    columns: defaultColumns("project"),
    revision: "new-revision",
  });
  assert.equal(drafts.get(original).resource, "project");
  assert.equal(drafts.get(original).revision, "new-revision");
});
