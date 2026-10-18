import test from "node:test";
import assert from "node:assert/strict";
import { createDrawerDrafts, drawerScope } from "./drafts.ts";
import { adjacentIssue, plainIssueClick } from "./navigation.ts";
test("drawer snapshots keep original aggregate baselines and UUIDs across close, issue switch and live head refresh, isolated by actor/workspace", () => {
  const store = createDrawerDrafts(),
    key = drawerScope("actor", "workspace", "issue-a");
  const original = {
    baseline: "old-revision",
    nodes: [{ id: "stable-node", children: [{ text: "İ😀\r\n" }] }],
    raw: "invalid draft",
  };
  store.write(key, "description", original);
  store.write(drawerScope("actor", "workspace", "issue-b"), "description", {
    baseline: "other",
  });
  assert.deepEqual(
    store.read(key, "description", () => ({ baseline: "fresh-head" })),
    original,
  );
  assert.equal(
    store.read(
      drawerScope("actor-b", "workspace", "issue-a"),
      "description",
      () => "new-actor",
    ),
    "new-actor",
  );
  assert.equal(
    store.read(
      drawerScope("actor", "workspace-b", "issue-a"),
      "description",
      () => "new-workspace",
    ),
    "new-workspace",
  );
  // Independent slots retain separate aggregate versions; no count-based eviction.
  store.write(key, "metadata", { baselineVersion: 4, title: "unsaved" });
  for (let i = 0; i < 300; i++)
    store.write(drawerScope("actor", "workspace", String(i)), "metadata", i);
  assert.deepEqual(
    store.read(key, "metadata", () => null),
    { baselineVersion: 4, title: "unsaved" },
  );
  assert.deepEqual(
    store.read(key, "description", () => null),
    original,
  );
});
test("live list order is only navigation context, never selected issue identity; canonical modified links remain native", () => {
  assert.equal(adjacentIssue(["a", "b", "c"], "b", 1), "c");
  assert.equal(adjacentIssue(["c", "b", "a"], "b", 1), "a");
  assert.equal(adjacentIssue(["a", "c"], "b", 1), null);
  const click = {
    button: 0,
    defaultPrevented: false,
    metaKey: false,
    ctrlKey: false,
    shiftKey: false,
    altKey: false,
  };
  assert(plainIssueClick(click));
  for (const key of [
    "defaultPrevented",
    "metaKey",
    "ctrlKey",
    "shiftKey",
    "altKey",
  ])
    assert(!plainIssueClick({ ...click, [key]: true }));
  assert(!plainIssueClick({ ...click, button: 1 }));
});
