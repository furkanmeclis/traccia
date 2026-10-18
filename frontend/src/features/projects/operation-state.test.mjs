import assert from "node:assert/strict";
import { test } from "node:test";
import { prepareSubmission } from "../workspaces/operation-submission.ts";
import {
  projectOperationStore as store,
  projectOperationScope,
  operationFailure,
} from "./operation-state.ts";
function operation(scope, input) {
  const submission = prepareSubmission(null, input, () => input);
  return {
    scope,
    label: "Archive",
    submission,
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
test("lost archive reply preserves operation, body and key after filtering and remount", () => {
  const scope = projectOperationScope("owner", "workspace", "project");
  const key = scope + "milestone-1";
  const input = {
    action: "archive",
    body: { expected_version: 2, expected_project_version: 4 },
  };
  const pending = operation(scope, input);
  store.set(key, pending);
  store.patch(key, operationFailure(pending, undefined, undefined));
  // The active row disappears; the archived view now offers Restore. Its new
  // props must not replace the submitted archive request held above the row.
  input.action = "restore";
  input.body.expected_project_version = 5;
  const recovered = store.get(key);
  assert.equal(recovered.submission.input.action, "archive");
  assert.deepEqual(recovered.submission.input.body, {
    expected_version: 2,
    expected_project_version: 4,
  });
  assert.equal(recovered.submission.key, pending.submission.key);
  assert.equal(recovered.uncertain, true);
  assert.notEqual(
    scope,
    projectOperationScope("other-owner", "workspace", "project"),
  );
  assert.notEqual(
    scope,
    projectOperationScope("owner", "other-workspace", "project"),
  );
  store.clear(key);
  assert.equal(store.get(key), undefined);
});
test("only initial definitive validation permits correcting fields; uncertain retries keep their identity", () => {
  const first = operation("scope", { name: "Invalid" });
  assert.equal(
    operationFailure(first, "VALIDATION_ERROR", 400).validationRejected,
    true,
  );
  const uncertain = {
    ...first,
    ...operationFailure(first, undefined, undefined),
  };
  assert.equal(
    operationFailure(uncertain, "VALIDATION_ERROR", 400).validationRejected,
    false,
  );
  assert.equal(operationFailure(uncertain, "FORBIDDEN", 403).uncertain, true);
  assert.equal(operationFailure(first, "VERSION_CONFLICT", 409).conflict, true);
  assert.equal(
    operationFailure(first, "IDEMPOTENCY_CONFLICT", 409).conflict,
    true,
  );
});
test("one child request changing aggregate version leaves unrelated pending operations intact", () => {
  const scope = projectOperationScope("actor", "workspace", "project");
  store.set(
    "milestone-edit",
    operation(scope, {
      name: "Unsaved milestone",
      expected_version: 2,
      expected_project_version: 4,
    }),
  );
  store.set(
    "resource-create",
    operation(scope, { title: "Resource", expected_project_version: 4 }),
  );
  const before = store.get("milestone-edit");
  store.clear("resource-create");
  assert.equal(store.get("milestone-edit"), before);
  assert.equal(
    store.get("milestone-edit").submission.input.name,
    "Unsaved milestone",
  );
  assert.equal(
    store.get("milestone-edit").submission.input.expected_project_version,
    4,
  );
  store.clear("milestone-edit");
});

test("a failed retry invalidates any review of the preceding attempt", () => {
  const prior = {
    ...operation("review-scope", { action: "archive" }),
    reviewed: true,
    reviewError: true,
  };
  const failure = operationFailure(prior, undefined, undefined);
  assert.equal(failure.reviewed, false);
  assert.equal(failure.reviewError, false);
  assert.equal(failure.uncertain, true);
});
