import test from "node:test";
import assert from "node:assert/strict";
import { collectPortfolioRows } from "./page-state.ts";
const w = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
  id = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb";
const row = {
    workspace_id: w,
    resource: "issue",
    issue: { id, workspace_id: w },
    project: null,
    team: null,
  },
  first = {
    session: {
      id,
      request_id: id,
      anchor_workspace_id: w,
      resource: "issue",
      as_of: "date",
      expires_at: "later",
      total_count: "2",
      workspaces: [
        { id: w, name: "Original private workspace", total_count: "2" },
      ],
    },
    definition: {
      resource: "issue",
      issue: { search: "İ" },
      project: null,
      team: null,
    },
    assigned_to_me: true,
    items: [row],
    has_more: true,
    next_cursor: "opaque two",
  },
  second = {
    ...structuredClone(first),
    items: [{ ...row, issue: { ...row.issue, id: w } }],
    has_more: false,
    next_cursor: "",
  };
test("portfolio cursor pages retain one exact atomic multi-workspace header/definition and reject mixed resource, scope, row duplicates or count drift", () => {
  assert.equal(collectPortfolioRows([first, second]).length, 2);
  for (const mutate of [
    (page) => (page.session.workspaces[0].name = "Changed name"),
    (page) => (page.definition.issue.search = "different query"),
    (page) => (page.assigned_to_me = false),
    (page) => (page.items[0] = row),
    (page) => (page.items[0].workspace_id = id),
    (page) => (page.items[0].project = { id, workspace_id: w }),
    (page) => (page.next_cursor = "after terminal"),
  ]) {
    const changed = structuredClone(second);
    mutate(changed);
    assert.throws(() => collectPortfolioRows([first, changed]), /PORTFOLIO_/);
  }
  assert.throws(
    () =>
      collectPortfolioRows([
        {
          ...first,
          session: { ...first.session, total_count: "9007199254740993" },
          has_more: false,
          next_cursor: "",
        },
      ]),
    /COUNT/,
  );
  const changed = structuredClone(second);
  changed.items[0].issue.id = id.toUpperCase();
  assert.throws(() => collectPortfolioRows([first, changed]), /ROW/);
});
