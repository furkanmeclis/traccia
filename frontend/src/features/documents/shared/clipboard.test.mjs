import test from "node:test";
import assert from "node:assert/strict";
import { pipeHandler } from "platejs/react";
import { createDocumentEditor } from "./plate.ts";
import { documentClipboardHandlers } from "./clipboard.ts";
import { paragraph } from "./contract.ts";
test("pinned Plate composition stops native fallback for canonical clipboard handlers", () => {
  const editor = createDocumentEditor([paragraph("clipboard")]);
  let copies = 0,
    cuts = 0,
    pastes = 0,
    success = true;
  const handlers = documentClipboardHandlers({
    copy: () => {
      copies++;
      return success;
    },
    cut: () => {
      cuts++;
    },
    paste: () => {
      pastes++;
    },
  });
  for (const key of ["onCopy", "onCut", "onPaste"]) {
    const event = {
      clipboardData: {},
      preventDefault() {
        this.prevented = true;
      },
      isPropagationStopped: () => false,
    };
    const composed = pipeHandler(editor, {
      editableProps: handlers,
      handlerKey: key,
    });
    assert.equal(composed(event), true);
    assert.equal(event.prevented, true);
  }
  assert.deepEqual([copies, cuts, pastes], [2, 1, 1]);
  success = false;
  const event = {
    clipboardData: {},
    preventDefault() {},
    isPropagationStopped: () => false,
  };
  assert.equal(
    pipeHandler(editor, { editableProps: handlers, handlerKey: "onCopy" })(
      event,
    ),
    true,
  );
  assert.equal(
    pipeHandler(editor, { editableProps: handlers, handlerKey: "onCut" })(
      event,
    ),
    true,
  );
  assert.equal(cuts, 1, "failed canonical copy never deletes the selection");
});

import { documentFixture } from "./fixtures.ts";
import { parseClipboard } from "./contract.ts";
test("canonical range extraction preserves the single merged cell that pinned table getFragment flattens", () => {
  const before = documentFixture();
  const editor = createDocumentEditor(before);
  editor.tf.select(editor.api.range([]));
  assert.equal(
    editor.api.getFragment().some((node) => node.type === "table"),
    false,
  );
  const fragment = editor.api.fragment(editor.selection);
  assert.deepEqual(fragment, before);
  assert.deepEqual(
    parseClipboard(
      JSON.stringify({ format: "plate", schema_version: 1, fragment }),
    ),
    before,
  );
});
