import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import vm from "node:vm";
import ts from "typescript";
import React from "react";
import { renderToStaticMarkup } from "react-dom/server";
import * as drafts from "./drafts.ts";
import * as contract from "../documents/shared/contract.ts";
const require = createRequire(import.meta.url);
const source = (path) => readFileSync(new URL(path, import.meta.url), "utf8");
function compile(text, bindings = {}, globals = {}) {
  const sandboxModule = { exports: {} };
  vm.runInNewContext(
    ts.transpileModule(text, {
      compilerOptions: {
        module: ts.ModuleKind.CommonJS,
        jsx: ts.JsxEmit.ReactJSX,
      },
    }).outputText,
    {
      module: sandboxModule,
      exports: sandboxModule.exports,
      structuredClone,
      ...globals,
      require: (name) => (name in bindings ? bindings[name] : require(name)),
    },
  );
  return sandboxModule.exports;
}
function ast(path) {
  return ts.createSourceFile(
    path,
    source(path),
    ts.ScriptTarget.Latest,
    true,
    ts.ScriptKind.TSX,
  );
}
const hook = compile(source("./draft-context.tsx"), { "./drafts": drafts });
const docAST = ast("../documents/document-panel.tsx");
const settings = docAST.statements.find(
  (n) => ts.isFunctionDeclaration(n) && n.name?.text === "DocumentSettings",
);
assert(settings);
let editor, saved;
const { DocumentSettings } = compile(
  "export " + settings.getText(docAST),
  {},
  {
    useDrawerDraft: hook.useDrawerDraft,
    useLocale: () => ({ t: (key) => key }),
    paragraph: contract.paragraph,
    canonical: contract.canonical,
    useDocumentMentions: () => ({ isSuccess: true, data: [] }),
    useDocumentOperation(_workspace, _owner, _slot, _label, callback) {
      saved = callback;
      return { locked: false };
    },
    LoadNotice: () => null,
    WidgetEditorHarness: () => null,
    DocumentEditor: (props) => {
      editor = props;
      return React.createElement("div", { "data-editor": true });
    },
    Button: (props) => {
      const clean = { ...props };
      delete clean.variant;
      return React.createElement("button", clean);
    },
  },
);
const nodeID = "10000000-0000-4000-8000-000000000001";
const content = (text) => [{ id: nodeID, type: "p", children: [{ text }] }];
function renderDocument(scope, revision = "old", text = "original") {
  return renderToStaticMarkup(
    React.createElement(
      hook.DrawerDraftContext.Provider,
      { value: scope },
      React.createElement(DocumentSettings, {
        workspaceId: "workspace",
        owner: { kind: "issue", id: "issue-a" },
        document: { id: "doc", revision, content: content(text) },
        ready: true,
        review: async () => {},
      }),
    ),
  );
}
test("actual document consumer and drawer hook restore dirty canonical nodes with original revision across unmount and fresh server head", () => {
  const scope = drafts.drawerScope("actor", "workspace", "issue-a");
  renderDocument(scope);
  editor.onChange(content("unsent İ e\u0301 👩🏽‍💻\r\n"), null);
  const restored = renderDocument(scope, "fresh", "competitor");
  assert.equal(editor.initialValue[0].id, nodeID);
  assert.equal(
    editor.initialValue[0].children[0].text,
    "unsent İ e\u0301 👩🏽‍💻\r\n",
  );
  assert.equal(
    drafts.drawerDrafts.read(scope, "document.baseline", () => "wrong"),
    "old",
  );
  assert(restored.includes("documents.stale"));
  assert.match(restored, /<button[^>]*type="submit"[^>]*disabled/);
  renderDocument(drafts.drawerScope("actor", "workspace", "issue-b"));
  assert.equal(editor.initialValue[0].children[0].text, "original");
  renderDocument(drafts.drawerScope("actor-b", "workspace", "issue-a"));
  assert.equal(editor.initialValue[0].children[0].text, "original");
  renderDocument(scope);
  assert.equal(
    editor.initialValue[0].children[0].text,
    "unsent İ e\u0301 👩🏽‍💻\r\n",
  );
  saved({ revision: "saved", content: content("committed") });
  renderDocument(scope, "saved", "committed");
  assert.equal(editor.initialValue[0].children[0].text, "committed");
  assert.equal(
    drafts.drawerDrafts.read(scope, "document.baseline", () => "wrong"),
    "saved",
  );
});
test("outside the drawer the same document consumer remains local and does not inherit retained issue state", () => {
  renderDocument(null);
  editor.onChange(content("local-only"), null);
  renderDocument(null, "fresh", "new page baseline");
  assert.equal(editor.initialValue[0].children[0].text, "new page baseline");
});
const drawerAST = ast("./issue-drawer.tsx");
const handlers = new Map();
function walk(node) {
  if (
    ts.isJsxAttribute(node) &&
    ["onKeyDown", "onCloseAutoFocus"].includes(node.name.getText(drawerAST))
  )
    handlers.set(
      node.name.getText(drawerAST),
      node.initializer.expression.getText(drawerAST),
    );
  ts.forEachChild(node, walk);
}
walk(drawerAST);
assert(handlers.has("onKeyDown"));
assert(handlers.has("onCloseAutoFocus"));
class Element {
  constructor(options = {}) {
    Object.assign(this, { isConnected: true, visible: true }, options);
  }
  closest(selector) {
    if (selector.includes("contenteditable") || selector.includes("input"))
      return this.editable ? this : null;
    if (selector === '[role="dialog"]') return this.dialog ?? null;
    return this.hidden ? this : null;
  }
  matches() {
    return !!this.disabled;
  }
  getClientRects() {
    return this.visible ? [{}] : [];
  }
  focus() {
    this.focuses = (this.focuses ?? 0) + 1;
  }
}
function event(target, overrides = {}) {
  return {
    target,
    nativeEvent: { isComposing: false },
    preventDefault() {
      this.prevented = true;
    },
    ...overrides,
  };
}
function handlerFixture() {
  const original = new Element(),
    replacement = new Element({ dataset: { issueRow: "a" } }),
    fallback = new Element();
  const contentNode = new Element(),
    edit = {
      disabled: false,
      clicks: 0,
      click() {
        this.clicks++;
      },
    };
  contentNode.querySelector = () => edit;
  contentNode.contains = (node) => node.dialog === contentNode;
  const contentRef = { current: contentNode },
    lifecycle = {
      current: { alive: true, open: false, generation: 1, closing: 1 },
    };
  const document = {
    body: {},
    activeElement: null,
    querySelectorAll: () => [replacement],
    getElementById: () => fallback,
  };
  document.activeElement = document.body;
  const selected = [];
  const nav = compile(source("./navigation.ts"), {}, { Element });
  const globals = {
    HTMLElement: Element,
    document,
    window: { location: { pathname: "/issues" } },
    content: contentRef,
    lifecycle,
    openedPath: { current: "/issues" },
    origin: { current: original },
    originID: { current: "a" },
    visibleFocusTarget: nav.visibleFocusTarget,
    editingTarget: nav.editingTarget,
    next: "b",
    previous: "z",
    step: (id) => selected.push(id),
  };
  return {
    original,
    replacement,
    fallback,
    contentNode,
    edit,
    lifecycle,
    document,
    selected,
    globals,
    key: compile("export const run = " + handlers.get("onKeyDown"), {}, globals)
      .run,
    close: compile(
      "export const run = " + handlers.get("onCloseAutoFocus"),
      {},
      globals,
    ).run,
  };
}
test("actual drawer shortcuts respect editable, composition, handled and nested-dialog boundaries while live UUID navigation works", () => {
  const f = handlerFixture(),
    target = new Element({ dialog: f.contentNode });
  for (const overrides of [
    { defaultPrevented: true },
    { nativeEvent: { isComposing: true } },
    { metaKey: true },
    { shiftKey: true },
    { ctrlKey: true },
    { altKey: true },
  ])
    f.key(event(target, { key: "j", ...overrides }));
  f.key(
    event(new Element({ editable: true, dialog: f.contentNode }), { key: "j" }),
  );
  f.key(event(new Element({ dialog: new Element() }), { key: "j" }));
  assert.deepEqual(f.selected, []);
  f.key(event(target, { key: "j" }));
  f.key(event(target, { key: "k" }));
  f.key(event(target, { key: "e" }));
  assert.deepEqual(f.selected, ["b", "z"]);
  assert.equal(f.edit.clicks, 1);
  f.edit.disabled = true;
  f.key(event(target, { key: "e" }));
  assert.equal(f.edit.clicks, 1);
});
test("actual drawer close restores disappearing-row fallback but never steals focus after a new cycle, navigation, or unmount", () => {
  let f = handlerFixture();
  f.close(event(null));
  assert.equal(f.original.focuses, 1);
  f = handlerFixture();
  f.original.isConnected = false;
  f.close(event(null));
  assert.equal(f.replacement.focuses, 1);
  f = handlerFixture();
  f.original.isConnected = false;
  f.replacement.visible = false;
  f.close(event(null));
  assert.equal(f.fallback.focuses, 1);
  for (const update of [{ open: true }, { generation: 2 }, { alive: false }]) {
    f = handlerFixture();
    Object.assign(f.lifecycle.current, update);
    f.close(event(null));
    assert.equal(f.original.focuses, undefined);
  }
  f = handlerFixture();
  f.globals.window.location.pathname = "/other";
  f.close(event(null));
  assert.equal(f.original.focuses, undefined);
  f = handlerFixture();
  f.document.activeElement = new Element();
  f.close(event(null));
  assert.equal(f.original.focuses, undefined);
});
const detailAST = ast("../issues/issue-detail.tsx");
const retained = detailAST.statements.find(
  (n) => ts.isFunctionDeclaration(n) && n.name?.text === "RetainedIssueForm",
);
assert(retained);
let metadataProps;
const { RetainedIssueForm } = compile(
  "export " + retained.getText(detailAST),
  {},
  {
    useDrawerDraft: hook.useDrawerDraft,
    IssueForm: (props) => {
      metadataProps = props;
      return React.createElement("div");
    },
  },
);
test("actual retained metadata consumer keeps dirty fields and captured numeric CAS after drawer remount/current-head drift", () => {
  const scope = drafts.drawerScope("metadata-actor", "workspace", "issue-a");
  const original = {
    id: "issue-a",
    version: 7,
    title: "baseline",
    priority: "normal",
    team_id: "team",
    status_id: "status",
    label_ids: ["label"],
  };
  const render = (issue) =>
    renderToStaticMarkup(
      React.createElement(
        hook.DrawerDraftContext.Provider,
        { value: scope },
        React.createElement(RetainedIssueForm, {
          generation: 0,
          issue,
          workspaceId: "workspace",
          canManage: true,
          reviewLatest: async () => {},
        }),
      ),
    );
  render(original);
  const dirty = structuredClone(metadataProps.controlledDraft.snapshot);
  dirty.values.title = "unsent İ 😀";
  dirty.values.label_ids.push("second-label");
  metadataProps.controlledDraft.onChange(dirty);
  render({ ...original, version: 8, title: "competitor", label_ids: [] });
  assert.deepEqual(metadataProps.controlledDraft.snapshot, dirty);
  assert.equal(metadataProps.controlledDraft.snapshot.baselineVersion, 7);
  assert.equal(metadataProps.issue.version, 8);
});
const reopen = drawerAST.statements.find(
  (n) => ts.isFunctionDeclaration(n) && n.name?.text === "QuickIssueButton",
);
assert(reopen);
let reopenSelect;
const jsx = require("react/jsx-runtime"),
  opened = [],
  recovery = {
    canCreate: false,
    visited: [
      { id: "dirty-a-no-longer-in-list" },
      { id: "visited-b" },
      ...Array.from({ length: 110 }, (_, n) => ({ id: `other-${n}` })),
    ],
    openIssue: (...args) => opened.push(args),
    openCreate() {},
  };
const { QuickIssueButton } = compile(
  "export " + reopen.getText(drawerAST),
  {
    "react/jsx-runtime": Object.fromEntries(
      Object.entries(jsx).map(([key, value]) => [
        key,
        key === "jsx" || key === "jsxs"
          ? (type, props, id) => {
              if (type === "select") reopenSelect = props;
              return value(type, props, id);
            }
          : value,
      ]),
    ),
  },
  {
    useIssueDrawer: () => recovery,
    useLocale: () => ({ t: (key) => key }),
    Button: (props) => React.createElement("button", props),
  },
);
test("actual scoped reopen consumer exposes every retained UUID after A disappears and B opens, independent of latest selection or list rows", () => {
  const html = renderToStaticMarkup(React.createElement(QuickIssueButton));
  assert(html.includes('value="dirty-a-no-longer-in-list"'));
  assert(html.includes('value="visited-b"'));
  assert(html.includes('value="other-109"'));
  assert.equal((html.match(/<option/g) ?? []).length, 113);
  const opener = {};
  reopenSelect.onChange({
    target: { value: "dirty-a-no-longer-in-list" },
    currentTarget: opener,
  });
  assert.deepEqual(opened, [["dirty-a-no-longer-in-list", opener]]);
});
