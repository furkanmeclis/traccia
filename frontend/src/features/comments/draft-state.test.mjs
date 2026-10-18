import test from "node:test";
import assert from "node:assert/strict";
import {
  createCommentDraftStore,
  commentDraftScope,
  draftChanged,
} from "./draft-state.ts";
const node = (id, text) => ({ id, type: "p", children: [{ text }] });
const one = "12345678-1234-4234-8234-123456789012",
  two = "12345678-1234-4234-8234-123456789013";
test("comment edit draft survives unrelated fresh head reads and row remount without rebasing its CAS or node IDs", () => {
  const store = createCommentDraftStore(),
    scope = commentDraftScope("actor", "workspace", "issue", "comment");
  const baseline = {
    revision: "revision1",
    content: [node(one, "original İ e\u0301 👩🏽‍💻\r\n")],
  };
  store.ensure(scope, baseline);
  baseline.content[0].children[0].text = "external mutation";
  const changed = [node(one, "dirty edit\r\n")];
  store.patch(scope, { content: changed, raw: "preserved invalid import" });
  changed[0].children[0].text = "external draft mutation";
  const retained = store.ensure(scope, {
    revision: "revision2",
    content: [node(two, "competing edit")],
  });
  assert.equal(retained.revision, "revision1");
  assert.equal(retained.content[0].id, one);
  assert.equal(retained.content[0].children[0].text, "dirty edit\r\n");
  assert.equal(
    retained.baselineContent[0].children[0].text,
    "original İ e\u0301 👩🏽‍💻\r\n",
  );
  assert.equal(retained.raw, "preserved invalid import");
  assert(draftChanged(retained));
  const reset = store.reset(scope, {
    revision: "revision2",
    content: [node(two, "competing edit")],
  });
  assert.equal(reset.revision, "revision2");
  assert.equal(reset.content[0].id, two);
  assert.equal(draftChanged(reset), false);
});
test("separate actor/workspace/issue/comment create and edit scopes keep drafts isolated; explicit canonical import preserves IDs and baseline", () => {
  const store = createCommentDraftStore(),
    scope = commentDraftScope("A", "W", "I"),
    initial = store.ensure(scope, null);
  assert.equal(commentDraftScope("a", "w", "i"), scope);
  for (const other of [
    commentDraftScope("B", "W", "I"),
    commentDraftScope("A", "W2", "I"),
    commentDraftScope("A", "W", "I2"),
    commentDraftScope("A", "W", "I", "C"),
  ]) {
    assert.notEqual(scope, other);
    assert.notEqual(
      store.ensure(other, null).content[0].id,
      initial.content[0].id,
    );
  }
  store.patch(scope, { raw: "{unknown:true}", error: "invalid" });
  assert.throws(() =>
    store.load(scope, [{ ...node(one, "keep"), unknown: true }]),
  );
  assert.equal(store.get(scope).raw, "{unknown:true}");
  assert.equal(store.get(scope).error, "invalid");
  const loaded = store.load(scope, [node(one, "imported")]);
  assert.equal(loaded.content[0].id, one);
  assert.equal(loaded.revision, null);
  assert.equal(loaded.baselineContent[0].id, initial.content[0].id);
  assert(draftChanged(loaded));
});
