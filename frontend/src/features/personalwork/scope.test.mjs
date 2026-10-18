import test from "node:test";
import assert from "node:assert/strict";
import { productScope, personalScope, collectSelector } from "./scope.ts";
const actor = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
  workspace = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
  target = "cccccccc-cccc-cccc-cccc-cccccccccccc";
test("product route scope uses stable workspace/resource UUIDs and isolates actor/workspace identity", () => {
  assert.deepEqual(productScope(`/w/${workspace}/issues/by-identifier/ABC-1`), {
    workspaceID: workspace,
    teamID: null,
    projectID: null,
    issueID: null,
  });
  assert.equal(productScope(`/w/${workspace}/teams/${target}`).teamID, target);
  assert.equal(
    productScope(`/w/${workspace}/projects/${target}`).projectID,
    target,
  );
  assert.equal(
    productScope(`/w/${workspace}/issues/${target}`).issueID,
    target,
  );
  assert.equal(productScope("/w/not-uuid/teams/" + target).workspaceID, null);
  assert.notEqual(
    personalScope(actor, workspace),
    personalScope(target, workspace),
  );
  assert.notEqual(
    personalScope(actor, workspace),
    personalScope(actor, target),
  );
  assert.equal(
    personalScope(actor.toUpperCase(), workspace.toUpperCase()),
    personalScope(actor, workspace),
  );
});
test("switcher exhausts105 cursor rows and rejects incomplete, duplicate or foreign scoped data", async () => {
  const rows = Array.from({ length: 105 }, (_, i) => ({
    id: `dddddddd-dddd-dddd-dddd-${i.toString(16).padStart(12, "0")}`,
    workspace_id: workspace,
  }));
  const seen = [];
  const all = await collectSelector(
    async (after) => {
      seen.push(after);
      return after
        ? { items: rows.slice(100), has_more: false, next_cursor: null }
        : {
            items: rows.slice(0, 100),
            has_more: true,
            next_cursor: "opaque page two",
          };
    },
    (row) => row.workspace_id === workspace,
  );
  assert.deepEqual(all, rows);
  assert.deepEqual(seen, [undefined, "opaque page two"]);
  await assert.rejects(
    () =>
      collectSelector(async () => ({
        items: [],
        has_more: true,
        next_cursor: null,
      })),
    /INCOMPLETE/,
  );
  await assert.rejects(
    () =>
      collectSelector(async () => ({
        items: [rows[0], { ...rows[0], id: rows[0].id.toUpperCase() }],
        has_more: false,
      })),
    /CHANGED/,
  );
  await assert.rejects(
    () =>
      collectSelector(
        async () => ({ items: [rows[0]], has_more: false }),
        (row) => row.workspace_id === target,
      ),
    /CHANGED/,
  );
  await assert.rejects(
    () =>
      collectSelector(async () => ({
        items: [],
        has_more: true,
        next_cursor: "same",
      })),
    /INCOMPLETE/,
  );
});

import { createPersonalContext } from "./context-state.ts";
test("actor/workspace generation fences late read publication through A-B-A and same-actor scope changes", () => {
  const context = createPersonalContext();
  context.activate(actor, workspace);
  const old = context.snapshot();
  context.activate(target, workspace);
  context.activate(actor, workspace);
  assert(!context.matches(old));
  const latest = context.snapshot();
  assert(context.matches(latest));
  context.activate(actor, target);
  assert(!context.matches(latest));
  const moved = context.snapshot();
  context.activate(actor.toUpperCase(), target.toUpperCase());
  assert(context.matches(moved));
});
