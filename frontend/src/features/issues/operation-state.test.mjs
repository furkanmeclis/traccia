import assert from "node:assert/strict";
import { test } from "node:test";
import { prepareSubmission } from "../workspaces/operation-submission.ts";
import {
  issueOperationStore as store,
  issueOperationScope,
  operationFailure,
} from "./operation-state.ts";
function pending(scope, command) {
  return {
    scope,
    label: "Issue",
    submission: prepareSubmission(null, command, () => command),
    pending: true,
    uncertain: false,
    conflict: false,
    validationRejected: false,
    reviewed: false,
    reviewing: false,
    reviewError: false,
    retry() {},
    review() {},
  };
}
test("uncertain command keeps original action, complete path, body and key after filters/remount", () => {
  const scope = issueOperationScope("owner", "workspace", "issue");
  const command = {
    action: "issue.archive",
    path: {
      workspaceID: "workspace",
      issueID: "issue",
    },
    body: { expected_version: 4 },
  };
  const entry = pending(scope, command);
  store.set("env", entry);
  store.patch("env", operationFailure(entry));
  command.action = "issue.restore";
  command.path.issueID = "other";
  command.body.expected_version = 5;
  const recovered = store.get("env");
  assert.equal(recovered.submission.input.action, "issue.archive");
  assert.deepEqual(recovered.submission.input.path, {
    workspaceID: "workspace",
    issueID: "issue",
  });
  assert.deepEqual(recovered.submission.input.body, {
    expected_version: 4,
  });
  assert.equal(recovered.submission.key, entry.submission.key);
  assert.equal(recovered.uncertain, true);
  store.clear("env");
});
test("scopes isolate actors, workspaces and issues and stores isolate domains", async () => {
  const a = issueOperationScope("actor", "w", "p");
  for (const b of [
    issueOperationScope("other", "w", "p"),
    issueOperationScope("actor", "other", "p"),
    issueOperationScope("actor", "w", "other"),
    issueOperationScope("actor", "w"),
  ])
    assert.notEqual(a, b);
  const { projectOperationStore } =
    await import("../projects/operation-state.ts");
  store.set("same", pending(a, {}));
  assert.equal(projectOperationStore.get("same"), undefined);
  store.clear("same");
});
test("review never survives a later uncertain attempt; only definitive validation allows correction", () => {
  const first = pending("s", {});
  assert.equal(
    operationFailure(first, "VALIDATION_ERROR", 400).validationRejected,
    true,
  );
  const uncertain = { ...first, ...operationFailure(first), reviewed: true };
  const retryFailure = operationFailure(uncertain, "VALIDATION_ERROR", 400);
  assert.equal(retryFailure.reviewed, false);
  assert.equal(retryFailure.validationRejected, false);
  assert.equal(retryFailure.uncertain, true);
});
test("settling one child keeps another submitted dirty draft unchanged", () => {
  const a = pending("s", {
    action: "issue.update",
    body: { name: "Unsaved", expected_version: 4 },
  });
  store.set("edit", a);
  store.set("create", pending("s", {}));
  store.clear("create");
  assert.equal(store.get("edit"), a);
  assert.equal(store.get("edit").submission.input.body.expected_version, 4);
  store.clear("edit");
});
