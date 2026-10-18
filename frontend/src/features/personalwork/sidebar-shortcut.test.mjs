import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import vm from "node:vm";
import ts from "typescript";
const source = ts.createSourceFile(
  "sidebar.tsx",
  readFileSync(
    new URL("../../components/ui/sidebar.tsx", import.meta.url),
    "utf8",
  ),
  ts.ScriptTarget.Latest,
  true,
  ts.ScriptKind.TSX,
);
let actual;
function walk(node) {
  if (
    ts.isVariableDeclaration(node) &&
    node.name.getText(source) === "handleKeyDown"
  )
    actual = node.initializer;
  ts.forEachChild(node, walk);
}
walk(source);
assert(actual && ts.isArrowFunction(actual));
let toggles = 0,
  prevented = 0;
class EditingTarget {
  constructor(editing = false) {
    this.editing = editing;
  }
  closest(selector) {
    for (const required of [
      "input",
      "textarea",
      "select",
      "contenteditable",
      "textbox",
    ])
      assert(selector.includes(required));
    return this.editing ? this : null;
  }
}
const sandbox = {
  SIDEBAR_KEYBOARD_SHORTCUT: "b",
  Element: EditingTarget,
  toggleSidebar: () => toggles++,
  run: null,
};
vm.runInNewContext(
  ts.transpileModule("run=" + actual.getText(source), {
    compilerOptions: { target: ts.ScriptTarget.ES2022 },
  }).outputText,
  sandbox,
);
test("actual installed SidebarProvider handler preserves Plate bold/input/contenteditable, composition and handled events; unhandled nonediting shortcut still toggles", () => {
  const event = () => ({
    key: "b",
    ctrlKey: true,
    metaKey: false,
    defaultPrevented: false,
    isComposing: false,
    target: new EditingTarget(),
    preventDefault() {
      prevented++;
    },
  });
  for (const fields of [
    { defaultPrevented: true },
    { isComposing: true },
    ...[
      "input",
      "textarea",
      "select",
      "nested contenteditable",
      "role textbox",
    ].map((kind) => ({ target: new EditingTarget(kind) })),
  ])
    sandbox.run({ ...event(), ...fields });
  assert.equal(toggles, 0);
  assert.equal(prevented, 0);
  sandbox.run(event());
  sandbox.run({ ...event(), ctrlKey: false, metaKey: true });
  assert.equal(toggles, 2);
  assert.equal(prevented, 2);
  sandbox.run({ ...event(), ctrlKey: false, metaKey: false });
  sandbox.run({ ...event(), key: "x" });
  assert.equal(toggles, 2);
  assert.equal(prevented, 2);
});
