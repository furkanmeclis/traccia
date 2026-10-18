import test from "node:test";
import assert from "node:assert/strict";
import {
  createDocumentEditor,
  convertDocumentBlock,
  splitDocumentCell,
} from "./plate.ts";
import { documentFixture } from "./fixtures.ts";
import {
  paragraph,
  reseedSubtree,
  validateContent,
  listNumbers,
} from "./contract.ts";
test("pinned Plate force-normalization preserves canonical CRLF, empty code lines, merged cells, checked false and UUIDs", () => {
  const before = documentFixture();
  const editor = createDocumentEditor(before);
  editor.tf.normalize({ force: true });
  assert.deepEqual(editor.children, before);
  validateContent(editor.children);
});
test("real Plate undo/redo and moves preserve IDs; explicit cross-document paste changes every subtree ID", () => {
  const before = documentFixture();
  const editor = createDocumentEditor(before);
  const added = paragraph("new");
  editor.tf.insertNodes(added, { at: [before.length] });
  assert.equal(editor.children.at(-1).id, added.id);
  editor.tf.undo();
  assert.deepEqual(editor.children, before);
  editor.tf.redo();
  assert.equal(editor.children.at(-1).id, added.id);
  editor.tf.moveNodes({ at: [0], to: [2] });
  assert.equal(editor.children[2].id, before[0].id);
  const pasted = reseedSubtree([before[2]]);
  editor.tf.insertNodes(pasted, { at: [editor.children.length] });
  assert.equal(editor.children.at(-1).id, pasted[0].id);
  assert.notEqual(pasted[0].id, before[2].id);
  validateContent(editor.children);
});
test("covered empty rows retain their exact canonical structure under real Plate normalization", () => {
  const e = (type, children, props = {}) => ({
    id: crypto.randomUUID(),
    type,
    children,
    ...props,
  });
  const v = [
    e(
      "table",
      [e("tr", [e("td", [paragraph("merged")], { rowSpan: 2 })]), e("tr", [])],
      { colSizes: [200] },
    ),
  ];
  validateContent(v);
  const editor = createDocumentEditor(v);
  editor.tf.normalize({ force: true });
  assert.deepEqual(editor.children, v);
  validateContent(editor.children);
});

test("real Plate explicit block conversions remove incompatible list properties", () => {
  const node = paragraph("draft");
  const editor = createDocumentEditor([node]);
  editor.tf.select({ path: [0, 0], offset: 0 });
  for (const [type, list] of [
    ["p", "decimal"],
    ["p", "todo"],
    ["p", "disc"],
    ["p"],
    ["h1"],
    ["p", "todo"],
    ["h2"],
    ["blockquote"],
    ["p", "decimal"],
  ]) {
    convertDocumentBlock(editor, type, list);
    validateContent(editor.children);
    assert.equal(editor.children[0].id, node.id);
    assert.equal(editor.children[0].type, type);
    assert.equal(editor.children[0].listStyleType, list);
    assert.equal("checked" in editor.children[0], list === "todo");
    assert.equal("listStart" in editor.children[0], list === "decimal");
  }
});
test("explicit numbering and inline blockquote remain canonical through forced normalization and sibling edits", () => {
  const a = {
      ...paragraph("one"),
      listStyleType: "decimal",
      indent: 1,
      listStart: 1,
      listRestart: 1,
    },
    b = {
      ...paragraph("five"),
      listStyleType: "decimal",
      indent: 1,
      listStart: 5,
    },
    quote = { ...paragraph("quote"), type: "blockquote" };
  const before = [a, b, quote],
    editor = createDocumentEditor(before);
  editor.tf.normalize({ force: true });
  assert.deepEqual(editor.children, before);
  editor.tf.insertText("!", { at: { path: [2, 0], offset: 5 } });
  assert.deepEqual(editor.children.slice(0, 2), before.slice(0, 2));
  assert.equal(editor.children[2].children[0].text, "quote!");
  validateContent(editor.children);
});
test("real Plate table merge and split keep fully covered rows canonical", async () => {
  const { TablePlugin } = await import("@platejs/table/react");
  const e = (type, children, props = {}) => ({
    id: crypto.randomUUID(),
    type,
    children,
    ...props,
  });
  const v = [
    e(
      "table",
      [
        e("tr", [e("td", [paragraph("a")])]),
        e("tr", [e("td", [paragraph("b")])]),
      ],
      { colSizes: [160] },
    ),
  ];
  const editor = createDocumentEditor(v);
  editor.tf.select({
    anchor: { path: [0, 0, 0, 0, 0], offset: 0 },
    focus: { path: [0, 1, 0, 0, 0], offset: 1 },
  });
  editor.getTransforms(TablePlugin).table.merge();
  validateContent(editor.children);
  assert.equal(editor.children[0].children[0].children[0].rowSpan, 2);
  assert.deepEqual(editor.children[0].children[1].children, []);
  splitDocumentCell(editor);
  validateContent(editor.children);
  assert.equal(editor.children[0].children[1].children.length, 1);
});

test("mixed-span non-first-column split preserves neighboring identities and exact undo/redo", () => {
  const e = (type, children, props = {}) => ({
    id: crypto.randomUUID(),
    type,
    children,
    ...props,
  });
  const left = e("td", [paragraph("left")], { rowSpan: 2 }),
    merged = e("th", [paragraph("keep exact\r\n👩🏽‍💻")], {
      rowSpan: 2,
      colSpan: 2,
      background: "#AbC",
      borders: { left: { color: "#123", style: "solid", size: 1 } },
    }),
    table = e("table", [e("tr", [left, merged]), e("tr", [])], {
      colSizes: [100, 120, 120],
      marginLeft: 8,
    }),
    before = [table],
    editor = createDocumentEditor(before);
  editor.tf.select({ path: [0, 0, 1, 0, 0], offset: 0 });
  splitDocumentCell(editor);
  validateContent(editor.children);
  const after = structuredClone(editor.children);
  assert.equal(after[0].id, table.id);
  assert.equal(after[0].children[0].id, table.children[0].id);
  assert.deepEqual(after[0].children[0].children[0], left);
  assert.equal(after[0].children[0].children[1].id, merged.id);
  assert.deepEqual(after[0].children[0].children[1].children, merged.children);
  assert.equal(after[0].children[1].children.length, 2);
  editor.tf.undo();
  assert.deepEqual(editor.children, before);
  editor.tf.redo();
  assert.deepEqual(editor.children, after);
});
test("real Plate retains inline reference UUIDs, typed targets, marks and voids", () => {
  const e = (type, children, props = {}) => ({
    id: crypto.randomUUID(),
    type,
    children,
    ...props,
  });
  const target = crypto.randomUUID(),
    link = e("a", [{ text: "link", bold: true }], {
      url: "https://example.com/a?x=1&y=2",
      target: "_blank",
    }),
    mention = e("mention", [{ text: "" }], {
      key: "issue:" + target,
      value: "İş 👩🏽‍💻",
      target_kind: "issue",
      target_id: target,
    });
  const content = [
    e("p", [
      { text: "" },
      link,
      { text: " / ", underline: false },
      mention,
      { text: " end", italic: true, strikethrough: true, code: false },
    ]),
    e("hr", [{ text: "" }]),
  ];
  validateContent(content);
  const editor = createDocumentEditor(content);
  editor.tf.normalize({ force: true });
  assert.deepEqual(editor.children, content);
});
test("real Enter continues an ordered list while existing explicit numbering remains unchanged", () => {
  const p = {
    ...paragraph("first"),
    listStyleType: "decimal",
    indent: 1,
    listStart: 7,
  };
  const editor = createDocumentEditor([p]);
  editor.tf.select({ path: [0, 0], offset: 5 });
  editor.tf.insertBreak();
  validateContent(editor.children);
  assert.equal(editor.children[0].id, p.id);
  assert.equal(editor.children[0].listStart, 7);
  assert.notEqual(editor.children[1].id, p.id);
  assert.equal(editor.children[1].listStart, undefined);
  const numbers = listNumbers(editor.children);
  assert.deepEqual(
    editor.children.map((n) => numbers.get(n.id)),
    [7, 8],
  );
});
test("undo of a list merge restores explicit numbering and UUIDs without conversion cleanup", () => {
  const content = [
      { ...paragraph("a"), listStyleType: "decimal", indent: 1, listStart: 7 },
      {
        ...paragraph("b"),
        listStyleType: "decimal",
        indent: 1,
        listStart: 9,
        listRestart: 9,
      },
    ],
    editor = createDocumentEditor(content);
  editor.tf.withNewBatch(() => editor.tf.mergeNodes({ at: [1] }));
  validateContent(editor.children);
  editor.tf.undo();
  assert.deepEqual(editor.children, content);
  editor.tf.redo();
  validateContent(editor.children);
});
