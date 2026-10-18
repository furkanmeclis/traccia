import assert from "node:assert/strict";
import { test } from "node:test";
import { prepareSubmission } from "../workspaces/operation-submission.ts";
import {
  productOperationStore as store,
  productOperationScope,
  operationFailure,
} from "./operation-state.ts";
function pending(scope, command) {
  return {
    scope,
    label: "Environment",
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
  const scope = productOperationScope("owner", "workspace", "product");
  const command = {
    action: "environment.archive",
    path: {
      workspaceID: "workspace",
      productID: "product",
      environmentID: "env",
    },
    body: { expected_version: 2, expected_product_version: 4 },
  };
  const entry = pending(scope, command);
  store.set("env", entry);
  store.patch("env", operationFailure(entry));
  command.action = "environment.restore";
  command.path.environmentID = "other";
  command.path.productID = "other";
  command.body.expected_product_version = 5;
  const recovered = store.get("env");
  assert.equal(recovered.submission.input.action, "environment.archive");
  assert.deepEqual(recovered.submission.input.path, {
    workspaceID: "workspace",
    productID: "product",
    environmentID: "env",
  });
  assert.deepEqual(recovered.submission.input.body, {
    expected_version: 2,
    expected_product_version: 4,
  });
  assert.equal(recovered.submission.key, entry.submission.key);
  assert.equal(recovered.uncertain, true);
  store.clear("env");
});
test("scopes isolate actors, workspaces and products and stores isolate domains", async () => {
  const a = productOperationScope("actor", "w", "p");
  for (const b of [
    productOperationScope("other", "w", "p"),
    productOperationScope("actor", "other", "p"),
    productOperationScope("actor", "w", "other"),
    productOperationScope("actor", "w"),
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
    action: "environment.update",
    body: { name: "Unsaved", expected_product_version: 4 },
  });
  store.set("edit", a);
  store.set("create", pending("s", {}));
  store.clear("create");
  assert.equal(store.get("edit"), a);
  assert.equal(
    store.get("edit").submission.input.body.expected_product_version,
    4,
  );
  store.clear("edit");
});
