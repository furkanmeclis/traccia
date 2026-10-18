import assert from "node:assert/strict";
import { test } from "node:test";
import { prepareSubmission } from "../workspaces/operation-submission.ts";
import {
  documentOperationStore as store,
  documentOperationScope,
  operationFailure,
} from "./operation-state.ts";
function pending(command) {
  return {
    scope: documentOperationScope("actor", "workspace", "document"),
    label: "Document",
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
test("document node patch retries freeze exact action/path/revision/ordered patch/body/key across two uncertain attempts", () => {
  const command = {
    action: "document.patch",
    path: { workspaceID: "workspace", documentID: "document" },
    body: {
      expected_revision: "9007199254740993",
      patches: [
        {
          op: "replace_node",
          node_id: "node",
          node: {
            id: "node",
            type: "p",
            children: [{ text: "İstanbul 👩🏽‍💻 é\r\n\r\n" }],
          },
        },
      ],
    },
  };
  const first = pending(command);
  store.set("patch", first);
  store.patch("patch", operationFailure(first));
  command.action = "document.restore";
  command.body.expected_revision = "9007199254740994";
  command.body.patches[0].node.children[0].text = "changed";
  const current = store.get("patch");
  assert.equal(current.submission.input.action, "document.patch");
  assert.equal(
    current.submission.input.body.expected_revision,
    "9007199254740993",
  );
  assert.equal(
    current.submission.input.body.patches[0].node.children[0].text,
    "İstanbul 👩🏽‍💻 é\r\n\r\n",
  );
  assert.equal(current.submission.key, first.submission.key);
  const later = operationFailure(
    { ...current, reviewed: true },
    "VALIDATION_ERROR",
    400,
  );
  assert.equal(later.reviewed, false);
  assert.equal(later.uncertain, true);
  assert.equal(later.validationRejected, false);
  store.clear("patch");
});
test("document recovery cannot change metadata or relation stores, actor or workspace scope", async () => {
  const { issueOperationStore } = await import("../issues/operation-state.ts");
  const { relationOperationStore } =
    await import("../issue-relations/operation-state.ts");
  const old = pending({
    body: { title: "Retained metadata", expected_version: 8 },
  });
  issueOperationStore.set("same", old);
  relationOperationStore.set("same", old);
  store.set("same", pending({ action: "document.restore" }));
  store.clear("same");
  assert.equal(issueOperationStore.get("same"), old);
  assert.equal(relationOperationStore.get("same"), old);
  for (const scope of [
    documentOperationScope("other", "workspace", "document"),
    documentOperationScope("actor", "other", "document"),
    documentOperationScope("actor", "workspace", "other"),
  ])
    assert.notEqual(scope, old.scope);
  issueOperationStore.clear("same");
  relationOperationStore.clear("same");
});
