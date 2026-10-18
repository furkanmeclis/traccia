import assert from "node:assert/strict";
import { test } from "node:test";
import { prepareSubmission } from "../workspaces/operation-submission.ts";
import {
  relationOperationStore as store,
  relationOperationScope,
  operationFailure,
} from "./operation-state.ts";
function pending(command) {
  return {
    scope: relationOperationScope("actor", "workspace", "source"),
    label: "Relation",
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
test("relation replay freezes original action, both UUID endpoints and complete revision set", () => {
  const command = {
    action: "relation.unlink",
    path: { workspaceID: "w", issueID: "source" },
    body: {
      target_issue_id: "target",
      expected_versions: [
        { issue_id: "source", expected_version: 3 },
        { issue_id: "target", expected_version: 8 },
      ],
    },
  };
  const entry = pending(command);
  store.set("unlink", entry);
  store.patch("unlink", operationFailure(entry));
  command.action = "relation.link";
  command.body.target_issue_id = "replacement";
  command.body.expected_versions[0].expected_version = 9;
  const recovered = store.get("unlink");
  assert.equal(recovered.submission.input.action, "relation.unlink");
  assert.equal(recovered.submission.input.body.target_issue_id, "target");
  assert.deepEqual(
    recovered.submission.input.body.expected_versions.map(
      (x) => x.expected_version,
    ),
    [3, 8],
  );
  assert.equal(recovered.submission.key, entry.submission.key);
  const secondLoss = operationFailure({ ...recovered, reviewed: true });
  assert.equal(secondLoss.reviewed, false);
  assert.equal(secondLoss.uncertain, true);
  assert.equal(
    operationFailure({ ...recovered, reviewed: true }, "VALIDATION_ERROR", 400)
      .validationRejected,
    false,
  );
  store.clear("unlink");
});
test("relation operations isolate metadata, actors, workspaces and source drafts", async () => {
  const { issueOperationStore } = await import("../issues/operation-state.ts");
  const entry = pending({});
  store.set("same", entry);
  assert.equal(issueOperationStore.get("same"), undefined);
  for (const scope of [
    relationOperationScope("other", "workspace", "source"),
    relationOperationScope("actor", "other", "source"),
    relationOperationScope("actor", "workspace", "other"),
  ])
    assert.notEqual(scope, entry.scope);
  store.set("sibling", pending({ title: "Dirty draft" }));
  store.clear("same");
  assert.equal(store.get("sibling").submission.input.title, "Dirty draft");
  store.clear("sibling");
});
test("graph cycle and inactive-endpoint conflicts require review instead of an identical rejected retry", () => {
  const failure = operationFailure(pending({}), "CONFLICT", 409);
  assert.equal(failure.conflict, true);
  assert.equal(failure.reviewed, false);
  assert.equal(failure.validationRejected, false);
});
test("T08 metadata draft and frozen command survive relation conflict review and discard unchanged", async () => {
  const { issueOperationStore } = await import("../issues/operation-state.ts");
  const metadata = pending({
    action: "issue.update",
    path: { workspaceID: "workspace", issueID: "source" },
    body: { title: "Unsaved metadata", expected_version: 12 },
  });
  issueOperationStore.set("metadata-draft", metadata);
  const graph = pending({
    action: "parent",
    body: {
      target_issue_id: "target",
      expected_versions: [{ issue_id: "source", expected_version: 3 }],
    },
  });
  store.set("metadata-draft", graph);
  store.patch(
    "metadata-draft",
    operationFailure(graph, "VERSION_CONFLICT", 409),
  );
  store.patch("metadata-draft", { reviewed: true });
  store.clear("metadata-draft");
  assert.equal(issueOperationStore.get("metadata-draft"), metadata);
  assert.deepEqual(
    issueOperationStore.get("metadata-draft").submission.input.body,
    { title: "Unsaved metadata", expected_version: 12 },
  );
  assert.equal(
    issueOperationStore.get("metadata-draft").submission.key,
    metadata.submission.key,
  );
  issueOperationStore.clear("metadata-draft");
});
