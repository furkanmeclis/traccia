import assert from "node:assert/strict";
import { test } from "node:test";
import { collectRelationPages, expectedVersions } from "./snapshot.ts";
const page = (items, extra = {}) => ({
  items: items.map((id) => ({ id })),
  has_more: false,
  next_cursor: "",
  version: 3,
  capabilities: { manage: true },
  ...extra,
});
const revision = (p) => p.version;
test("complete relation choices include final cursor and intersect current permissions without response mutation", async () => {
  const first = page(
    Array.from({ length: 100 }, (_, i) => String(i)),
    {
      has_more: true,
      next_cursor: "last",
    },
  );
  const seen = [];
  const result = await collectRelationPages(async (after) => {
    seen.push(after);
    return after
      ? page(["99", "100", "101"], { capabilities: { manage: false } })
      : first;
  }, revision);
  assert.deepEqual(seen, [undefined, "last"]);
  assert.equal(result.items.length, 102);
  assert.equal(result.items.at(-1).id, "101");
  assert.equal(result.capabilities.manage, false);
  assert.equal(first.capabilities.manage, true);
});
test("incomplete cursors, changed revisions and failed later pages never yield a writable relation snapshot", async () => {
  await assert.rejects(
    collectRelationPages(async () => page([], { has_more: true }), revision),
    /INCOMPLETE_PAGINATION/,
  );
  await assert.rejects(
    collectRelationPages(
      async () => page([], { has_more: true, next_cursor: "same" }),
      revision,
    ),
    /INCOMPLETE_PAGINATION/,
  );
  await assert.rejects(
    collectRelationPages(
      async (after) =>
        page(
          [],
          after ? { version: 4 } : { has_more: true, next_cursor: "last" },
        ),
      revision,
    ),
    /RELATION_SNAPSHOT_CHANGED/,
  );
  await assert.rejects(
    collectRelationPages(async (after) => {
      if (after) throw Error("read denied");
      return page([], { has_more: true, next_cursor: "last" });
    }, revision),
    /read denied/,
  );
});

test("replacement CAS includes source, old target and new target exactly once with no contradictory revision", () => {
  assert.deepEqual(
    expectedVersions([
      { id: "source", relation_version: 3 },
      { id: "old", relation_version: 4 },
      { id: "new", relation_version: 8 },
      { id: "old", relation_version: 4 },
    ]),
    [
      { issue_id: "new", expected_version: 8 },
      { issue_id: "old", expected_version: 4 },
      { issue_id: "source", expected_version: 3 },
    ],
  );
  assert.throws(
    () =>
      expectedVersions([
        { id: "same", relation_version: 3 },
        { id: "same", relation_version: 4 },
      ]),
    /RELATION_SNAPSHOT_CHANGED/,
  );
});

test("nested graph capability snapshots intersect every page without mutating fetched state", async () => {
  const first = {
    items: [{ id: "a" }],
    has_more: true,
    next_cursor: "last",
    state: {
      issue: { relation_version: 2 },
      capabilities: { link: true, unlink: true },
    },
  };
  const result = await collectRelationPages(
    async (after) =>
      after
        ? {
            items: [{ id: "b" }],
            has_more: false,
            next_cursor: "",
            state: {
              issue: { relation_version: 2 },
              capabilities: { link: false, unlink: true },
            },
          }
        : first,
    (p) => p.state.issue.relation_version,
    (p) => p.state.capabilities,
  );
  assert.equal(result.state.capabilities.link, false);
  assert.equal(result.state.capabilities.unlink, true);
  assert.equal(first.state.capabilities.link, true);
});
