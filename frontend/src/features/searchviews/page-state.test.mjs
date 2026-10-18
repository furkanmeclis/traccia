import test from "node:test";
import assert from "node:assert/strict";
import { collectSnapshotRows } from "./page-state.ts";
const session = {
  id: "session",
  request_id: "request",
  workspace_id: "workspace",
  resource: "issue",
  as_of: "as-of",
  expires_at: "expires",
  total_count: "103",
};
const q = {
  version: 1,
  archived: "active",
  search: "İ",
  filter: null,
  sort: [],
};
const first = {
  session,
  query: q,
  items: Array.from({ length: 100 }, (_, i) => ({
    id: `issue-${i}`,
    version: "9007199254740993",
  })),
  has_more: true,
  next_cursor: "opaque+first/==",
};
const last = {
  session: { ...session },
  query: {
    sort: [],
    filter: null,
    search: "İ",
    archived: "active",
    version: 1,
  },
  items: Array.from({ length: 3 }, (_, i) => ({
    id: `issue-${100 + i}`,
    version: "9007199254740994",
  })),
  has_more: false,
  next_cursor: "",
};
test("complete 103-row capture retains server order, opaque cursor and decimal strings across reordered object keys", () => {
  const rows = collectSnapshotRows([first, last]);
  assert.equal(rows.length, 103);
  assert.equal(rows[100].id, "issue-100");
  assert.equal(rows[0].version, "9007199254740993");
  assert.equal(first.next_cursor, "opaque+first/==");
});
test("snapshot drift, repeat cursor/UUID, premature end and count mismatch are visible errors rather than quiet deduplication/truncation", () => {
  for (const change of [
    { session: { ...session, as_of: "new" } },
    { session: { ...session, total_count: "104" } },
    { query: { ...q, search: "other" } },
    { items: [first.items[0], ...last.items.slice(1)] },
    { has_more: true, next_cursor: first.next_cursor },
    { items: last.items.slice(0, 2) },
    { session: { ...session, total_count: 103 } },
  ])
    assert.throws(() => collectSnapshotRows([first, { ...last, ...change }]));
  assert.throws(() =>
    collectSnapshotRows([{ ...first, has_more: false, next_cursor: "" }, last]),
  );
  assert.throws(() => collectSnapshotRows([{ ...first, next_cursor: "" }]));
  assert.deepEqual(
    collectSnapshotRows([
      { ...last, session: { ...session, total_count: "0" }, items: [] },
    ]),
    [],
  );
});
