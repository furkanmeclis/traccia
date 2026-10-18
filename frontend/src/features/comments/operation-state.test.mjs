import test from "node:test";
import assert from "node:assert/strict";
import { createCommentOperationStore } from "./operation-state.ts";
const scope = { actorID: "A", workspaceID: "W", issueID: "I", commentID: "C" };
const command = () => ({
  action: "comment.replace",
  workspaceID: "W",
  issueID: "I",
  commentID: "C",
  body: {
    expected_revision: "old-revision",
    format: "plate",
    schema_version: 1,
    content: [
      { id: "node", type: "p", children: [{ text: "İ 👩🏽‍💻 e\u0301\r\n" }] },
    ],
  },
});
test("two lost comment responses require fresh review and preserve exact immutable command, body, revision and key", async () => {
  const store = createCommentOperationStore();
  store.activate("A", "W");
  const input = command(),
    entry = store.begin(scope, "edit", "edit", input),
    frozen = structuredClone(entry.submission);
  input.body.expected_revision = "changed";
  for (let i = 0; i < 2; i++) {
    store.failed(entry.id, store.ticket(entry.id), "network");
    assert.throws(() => store.retry(entry.id));
    await store.review(entry.id, async () => true);
    assert.deepEqual(store.retry(entry.id), frozen);
    assert.throws(() => store.retry(entry.id));
  }
  store.success(entry.id, store.ticket(entry.id));
  assert.equal(store.snapshot().size, 0);
});
test("definitive CAS rejection stays nonretryable after current manage review but exact descriptor remains reviewable/discardable", async () => {
  const store = createCommentOperationStore();
  store.activate("A", "W");
  const entry = store.begin(scope, "delete", "delete", command());
  store.failed(entry.id, store.ticket(entry.id), "VERSION_CONFLICT", 409);
  await store.review(entry.id, async () => true);
  assert.equal(store.get(entry.id).outcome, "rejected");
  assert.equal(store.get(entry.id).canRetry, false);
  assert.throws(() => store.retry(entry.id));
  assert.deepEqual(store.get(entry.id).submission, entry.submission);
  store.discard(entry.id);
  assert.equal(store.snapshot().size, 0);
});
test("late review from actor or workspace A to B to A cannot restore a stale grant or erase the pending command", async () => {
  for (const switchContext of [
    (store) => {
      store.activate("B", "W");
      store.activate("A", "W");
    },
    (store) => {
      store.activate("A", "W2");
      store.activate("A", "W");
    },
  ]) {
    const store = createCommentOperationStore();
    store.activate("A", "W");
    const entry = store.begin(scope, "restore", "restore", command());
    store.failed(entry.id, store.ticket(entry.id), "network");
    let release;
    const pending = store.review(
      entry.id,
      () => new Promise((resolve) => (release = resolve)),
    );
    switchContext(store);
    release(true);
    assert.equal(await pending, false);
    assert.equal(store.get(entry.id).reviewed, false);
    assert.deepEqual(store.get(entry.id).submission, entry.submission);
    assert.throws(() => store.retry(entry.id));
    await store.review(entry.id, async () => false);
    assert.equal(store.get(entry.id).reviewed, true);
    assert.equal(store.get(entry.id).canRetry, false);
    store.discard(entry.id);
  }
});

test("actor/workspace transition aborts in-flight transport and retains its frozen uncertain receipt for later scoped review", async () => {
  const store = createCommentOperationStore();
  store.activate("A", "W");
  const entry = store.begin(scope, "edit", "edit", command()),
    signal = store.signal(entry.id);
  assert(signal && !signal.aborted);
  store.activate("A", "W2");
  assert(signal.aborted);
  assert.equal(store.get(entry.id).pending, false);
  assert.deepEqual(store.get(entry.id).submission, entry.submission);
  assert.throws(() => store.retry(entry.id));
  store.activate("A", "W");
  await store.review(entry.id, async () => true);
  const replay = store.retry(entry.id);
  assert.deepEqual(replay, entry.submission);
  assert.equal(store.signal(entry.id).aborted, false);
});

test("late prior attempt success or failure after context switch cannot clear a current retry/controller or its exact receipt", async () => {
  for (const completion of ["success", "failure"]) {
    const store = createCommentOperationStore();
    store.activate("A", "W");
    const entry = store.begin(scope, "edit", "edit", command()),
      old = store.ticket(entry.id);
    store.activate("B", "W");
    store.activate("A", "W");
    await store.review(entry.id, async () => true);
    const replay = store.retry(entry.id),
      current = store.ticket(entry.id);
    assert.notEqual(current.attempt, old.attempt);
    assert.equal(
      completion === "success"
        ? store.success(entry.id, old)
        : store.failed(entry.id, old, "late failure"),
      false,
    );
    assert.equal(store.get(entry.id).pending, true);
    assert.equal(store.signal(entry.id), current.signal);
    assert.deepEqual(replay, entry.submission);
    assert.equal(store.success(entry.id, current), true);
  }
});
test("old receipt completion cannot remove a newly created command sharing the same UI operation scope", async () => {
  const store = createCommentOperationStore();
  store.activate("A", "W");
  const oldEntry = store.begin(scope, "edit", "edit", command()),
    old = store.ticket(oldEntry.id);
  store.failed(oldEntry.id, old, "network");
  await store.review(oldEntry.id, async () => true);
  store.discard(oldEntry.id);
  const next = store.begin(scope, "edit", "edit", {
    ...command(),
    body: { ...command().body, expected_revision: "fresh-head" },
  });
  assert.notEqual(next.submission.key, old.key);
  assert.equal(store.success(next.id, old), false);
  assert.equal(store.failed(next.id, old, "old rejected", 409), false);
  assert.equal(store.get(next.id).submission.key, next.submission.key);
  assert.equal(store.get(next.id).pending, true);
});

test("one unresolved comment command blocks a different write action on the same comment while independent comments remain usable", () => {
  const store = createCommentOperationStore();
  store.activate("A", "W");
  store.begin(scope, "edit", "edit", command());
  assert.throws(() => store.begin(scope, "delete", "delete", command()));
  const other = store.begin(
    { ...scope, commentID: "other" },
    "edit",
    "edit",
    command(),
  );
  assert(other);
  assert.equal(store.snapshot().size, 2);
});
