import test from "node:test";
import assert from "node:assert/strict";
import { ReviewContext } from "./review-context.ts";
test("late actor A review cannot reopen permission after A to B to A", async () => {
  const context = new ReviewContext();
  context.change("actor-a", "workspace");
  let release;
  const read = new Promise((resolve) => (release = resolve)),
    token = context.token();
  const pending = read.then(() => context.current(token));
  context.change("actor-b", "workspace");
  context.change("actor-a", "workspace");
  release(true);
  assert.equal(await pending, false);
  assert.equal(context.current(context.token()), true);
});
test("workspace switch expires pending review, identical scope does not, immutable request survives", async () => {
  const context = new ReviewContext(),
    command = { key: "preserved", body: { expected_version: 7 } };
  context.change("actor", "workspace-a");
  const token = context.token(),
    snapshot = JSON.stringify(command);
  assert.equal(context.change("ACTOR", "WORKSPACE-A"), false);
  assert.equal(context.current(token), true);
  context.change("actor", "workspace-b");
  await Promise.resolve();
  context.change("actor", "workspace-a");
  assert.equal(context.current(token), false);
  assert.equal(JSON.stringify(command), snapshot);
});
