import test from "node:test";
import assert from "node:assert/strict";
import { createHomeDrafts, homeDraftKey } from "./draft-state.ts";
test("personal drafts retain explicit selected scopes and complete dirty query independently of applied sessions, resource/actor/anchor changes", () => {
  const store = createHomeDrafts(),
    id = homeDraftKey("actorA", "anchorA", "issue"),
    initial = store.ensure(id, "anchorA", "issue");
  assert.deepEqual(initial.workspaceIDs, ["anchorA"]);
  const patch = {
    workspaceIDs: ["explicitB", "explicitC"],
    query: { ...initial.query, search: "  İ e\u0301\r\n " },
    ttl: "60",
    raw: "retained bad raw",
    numericInputs: { "filter.number": "2147483648" },
    error: "localized",
  };
  store.patch(id, patch);
  patch.workspaceIDs.pop();
  const dirty = structuredClone(store.get(id));
  store.ensure(homeDraftKey("actorB", "anchorA", "issue"), "anchorA", "issue");
  store.ensure(homeDraftKey("actorA", "anchorB", "issue"), "anchorB", "issue");
  store.ensure(
    homeDraftKey("actorA", "anchorA", "project"),
    "anchorA",
    "project",
  );
  assert.deepEqual(store.ensure(id, "anchorA", "issue"), dirty);
  assert.deepEqual(dirty.workspaceIDs, ["explicitB", "explicitC"]);
  store.loadQuery(id, { ...dirty.query, search: "explicit import" });
  assert.deepEqual(store.get(id).workspaceIDs, dirty.workspaceIDs);
  assert.equal(store.get(id).generation, 1);
  assert.deepEqual(store.get(id).numericInputs, {});
});
