import assert from "node:assert/strict";
import { test } from "node:test";
import {
  collectIssues,
  validIssueTitle,
  validIssueDates,
  validIssueEffort,
} from "./snapshot.ts";
const page = (items, extra = {}) => ({
  items: items.map((id) => ({ id })),
  has_more: false,
  next_cursor: "",
  workflow_version: 1,
  capabilities: { manage: true },
  ...extra,
});
test("selectors exhaust all pages, dedupe UUIDs and intersect capabilities without mutating responses", async () => {
  const first = page(["a"], { has_more: true, next_cursor: "next" });
  const calls = [];
  const result = await collectIssues(async (after) => {
    calls.push(after);
    return after
      ? page(["a", "b"], { capabilities: { manage: false } })
      : first;
  });
  assert.deepEqual(calls, [undefined, "next"]);
  assert.deepEqual(
    result.items.map((item) => item.id),
    ["a", "b"],
  );
  assert.equal(result.capabilities.manage, false);
  assert.equal(first.capabilities.manage, true);
});
test("missing/repeated cursors, failed later pages and changed snapshots never yield a writable selector", async () => {
  await assert.rejects(
    collectIssues(async () => page([], { has_more: true })),
    /INCOMPLETE_PAGINATION/,
  );
  await assert.rejects(
    collectIssues(async () =>
      page([], { has_more: true, next_cursor: "repeat" }),
    ),
    /INCOMPLETE_PAGINATION/,
  );
  await assert.rejects(
    collectIssues(async (after) =>
      page(
        [],
        after
          ? { workflow_version: 2 }
          : { has_more: true, next_cursor: "next" },
      ),
    ),
    /ISSUE_SELECTION_CHANGED/,
  );
  await assert.rejects(
    collectIssues(async (after) => {
      if (after) throw Error("offline");
      return page([], { has_more: true, next_cursor: "next" });
    }),
    /offline/,
  );
});
test("issue metadata accepts Unicode code points and preserves nullable dates/effort boundaries", () => {
  assert.equal(validIssueTitle("😀".repeat(255)), true);
  assert.equal(validIssueTitle("😀".repeat(256)), false);
  assert.equal(validIssueTitle(" "), false);
  for (const value of ["a\nb", "a\tb", "a\u2028b", "a\u2029b"])
    assert.equal(validIssueTitle(value), false);
  assert.equal(validIssueDates("2026-01-02", "2026-01-01"), false);
  assert.equal(validIssueDates("", "2026-01-01"), true);
  for (const value of ["", "0", "1000"])
    assert.equal(validIssueEffort(value), true);
  for (const value of ["-1", "1.5", "1e3", "1001"])
    assert.equal(validIssueEffort(value), false);
});
