import test from "node:test";
import assert from "node:assert/strict";
import {
  entrySchema,
  removalFailure,
  mayRetryRemoval,
} from "./remove-schema.ts";
const id = "12345678-1234-4234-8234-123456789012";
const seed = () => ({
  id: "scope",
  actorID: id,
  command: {
    workspaceID: id,
    action: "attachment.remove",
    attachmentID: id,
    body: { expected_version: 7 },
  },
  key: id,
  pending: false,
  reviewed: false,
  reviewing: false,
  canRetry: false,
  error: null,
});
test("competing manager removal rejection survives reload and fresh permission cannot revive stale CAS", () => {
  for (const status of [400, 409]) {
    const original = seed(),
      rejected = entrySchema.parse({ ...original, ...removalFailure(status) });
    const reloaded = entrySchema.parse(JSON.parse(JSON.stringify(rejected)));
    assert.equal(reloaded.outcome, "rejected");
    assert.equal(mayRetryRemoval(reloaded, true, true, true, false), false);
    assert.deepEqual(reloaded.command, original.command);
    assert.equal(reloaded.key, original.key);
  }
});
test("lost removal response retains exact request and permits receipt retry only with current manage/remove access", () => {
  const original = seed(),
    uncertain = entrySchema.parse({ ...original, ...removalFailure() });
  const reloaded = entrySchema.parse(JSON.parse(JSON.stringify(uncertain)));
  assert.equal(reloaded.outcome, "uncertain");
  assert.equal(reloaded.reviewed, false);
  assert.equal(mayRetryRemoval(reloaded, true, true, true, false), true);
  assert.equal(mayRetryRemoval(reloaded, false, true, true, false), false);
  assert.equal(mayRetryRemoval(reloaded, true, false, true, false), false);
  assert.deepEqual(reloaded.command, original.command);
  assert.equal(reloaded.key, original.key);
  assert.equal(removalFailure(408).outcome, "uncertain");
  assert.equal(removalFailure(503).retryable, true);
});
