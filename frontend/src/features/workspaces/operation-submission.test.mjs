import assert from "node:assert/strict";
import { test } from "node:test";
import { prepareSubmission } from "./operation-submission.ts";

test("credential retry retains its key, absolute expiry and scope snapshot", () => {
  const scopes = ["workspace.read"];
  const intent = { name: "Editor", days: 30, scopes };
  let builds = 0;
  const build = () => ({
    name: intent.name,
    scopes,
    expires_at: new Date(1_800_000_000_000 + builds++ * 1000).toISOString(),
  });
  const first = prepareSubmission(null, intent, build);
  const retry = prepareSubmission(first, intent, build);
  assert.equal(retry, first);
  assert.equal(builds, 1);
  scopes.push("workspace.write");
  assert.deepEqual(first.input.scopes, ["workspace.read"]);
  const changed = prepareSubmission(first, intent, build);
  assert.notEqual(changed.key, first.key);
  assert.equal(builds, 2);
});

test("edit retry retains the submitted version until explicit reconciliation", () => {
  const baseline = { name: "Original", version: 2 };
  const draft = { name: "My draft", expected_version: baseline.version };
  const first = prepareSubmission(null, draft, () => draft);
  const backgroundResult = { name: "Another edit", version: 3 };
  const retry = prepareSubmission(first, draft, () => draft);
  assert.equal(retry.key, first.key);
  assert.equal(retry.input.expected_version, 2);
  const reconciled = { ...draft, expected_version: backgroundResult.version };
  const next = prepareSubmission(first, reconciled, () => reconciled);
  assert.notEqual(next.key, first.key);
  assert.equal(next.input.expected_version, 3);
  assert.equal(next.input.name, "My draft");
});
