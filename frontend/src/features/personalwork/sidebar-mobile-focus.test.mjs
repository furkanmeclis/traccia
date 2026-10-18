import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import vm from "node:vm";
import ts from "typescript";
import React from "react";
const require = createRequire(import.meta.url);
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
const functions = new Map();
let toggle;
function walk(node) {
  if (ts.isFunctionDeclaration(node)) functions.set(node.name?.text, node);
  if (
    ts.isVariableDeclaration(node) &&
    node.name.getText(source) === "toggleSidebar"
  )
    toggle = node.initializer.arguments[0];
  ts.forEachChild(node, walk);
}
walk(source);
for (const name of [
  "createSidebarMobileFocus",
  "Sidebar",
  "SidebarTrigger",
  "SidebarRail",
])
  assert(functions.has(name));
assert(toggle);
const sheet = ts.createSourceFile(
  "sheet.tsx",
  readFileSync(
    new URL("../../components/ui/sheet.tsx", import.meta.url),
    "utf8",
  ),
  ts.ScriptTarget.Latest,
  true,
  ts.ScriptKind.TSX,
);
let sheetRender;
function findSheet(node) {
  if (
    ts.isVariableDeclaration(node) &&
    node.name.getText(sheet) === "SheetContent"
  )
    sheetRender = node.initializer.arguments[0];
  ts.forEachChild(node, findSheet);
}
findSheet(sheet);
assert(sheetRender);
const dialogRequire = createRequire(require.resolve("@radix-ui/react-dialog"));
const radix = ts.createSourceFile(
  "dialog.mjs",
  readFileSync(require.resolve("@radix-ui/react-dialog"), "utf8"),
  ts.ScriptTarget.Latest,
  true,
  ts.ScriptKind.JS,
);
let installedClose;
function findClose(node) {
  if (
    !installedClose &&
    ts.isPropertyAssignment(node) &&
    node.name.getText(radix) === "onCloseAutoFocus" &&
    ts.isCallExpression(node.initializer)
  )
    installedClose = node.initializer;
  ts.forEachChild(node, findClose);
}
findClose(radix);
assert(installedClose);
function fixture() {
  const body = {},
    content = { contains: (node) => node?.inside === true };
  const document = {
    body,
    activeElement: body,
    defaultView: {
      location: { href: "http://local/w/one/home" },
      getComputedStyle: () => ({ visibility: "visible" }),
    },
  };
  let focuses = 0,
    desktop = 0;
  const opener = {
    isConnected: true,
    disabled: false,
    ownerDocument: document,
    getClientRects: () => [{}],
    focus(options) {
      assert.equal(options.preventScroll, true);
      focuses++;
      document.activeElement = this;
    },
  };
  const sandbox = {
    React: { ...React, useRef: () => ({ current: content }) },
    require,
    Sheet: "Sheet",
    SheetContent: "SheetContent",
    SheetHeader: "SheetHeader",
    SheetTitle: "SheetTitle",
    SheetDescription: "SheetDescription",
    Button: "Button",
    PanelLeftIcon: "PanelLeftIcon",
    SheetPortal: "SheetPortal",
    SheetOverlay: "SheetOverlay",
    DialogPrimitive: { Content: "DialogContent", Close: "DialogClose" },
    X: "X",
    cn: (...values) => values.join(" "),
    sheetVariants: () => "",
    SIDEBAR_WIDTH_MOBILE: "18rem",
    exports: {},
    isMobile: true,
    openMobile: false,
    setOpenMobile(fn) {
      sandbox.openMobile =
        typeof fn === "function" ? fn(sandbox.openMobile) : fn;
    },
    setOpen() {
      desktop++;
    },
    composeEventHandlers: dialogRequire("@radix-ui/primitive")
      .composeEventHandlers,
    import_primitive: dialogRequire("@radix-ui/primitive"),
    renderSheet: null,
    context: { triggerRef: { current: null } },
  };
  sandbox.useSidebar = () => ({
    isMobile: sandbox.isMobile,
    state: "expanded",
    openMobile: sandbox.openMobile,
    setOpenMobile: sandbox.setOpenMobile,
    mobileFocus: sandbox.mobileFocus,
    toggleSidebar: sandbox.toggleSidebar,
  });
  const code =
    [...functions.values()]
      .filter((n) =>
        [
          "createSidebarMobileFocus",
          "Sidebar",
          "SidebarTrigger",
          "SidebarRail",
        ].includes(n.name.text),
      )
      .map((n) => n.getText(source))
      .join("\n") +
    "\nexports={createSidebarMobileFocus,Sidebar,SidebarTrigger,SidebarRail};\n";
  vm.runInNewContext(
    ts.transpileModule(code, {
      compilerOptions: {
        target: ts.ScriptTarget.ES2022,
        module: ts.ModuleKind.CommonJS,
        jsx: ts.JsxEmit.ReactJSX,
      },
    }).outputText,
    sandbox,
  );
  sandbox.mobileFocus = sandbox.exports.createSidebarMobileFocus();
  vm.runInNewContext(
    ts.transpileModule("toggleSidebar=" + toggle.getText(source), {
      compilerOptions: { target: ts.ScriptTarget.ES2022 },
    }).outputText,
    sandbox,
  );
  const opening = () => {
    document.activeElement = opener;
    const trigger = sandbox.exports.SidebarTrigger({});
    trigger.props.onClick({ currentTarget: opener });
    const root = sandbox.exports.Sidebar({});
    assert.equal(root.type, "Sheet");
    assert.equal(root.props.open, true);
    return root.props.children;
  };
  const close = (element) => {
    element.props.onEscapeKeyDown();
    document.activeElement = body;
    sandbox.setOpenMobile(false);
    sandbox.props = element.props;
    vm.runInNewContext("close=" + installedClose.getText(radix), sandbox);
    const event = {
      defaultPrevented: false,
      preventDefault() {
        this.defaultPrevented = true;
      },
    };
    sandbox.close(event);
    return event;
  };
  const forward = (element) => {
    sandbox.props = element.props;
    const text = "renderSheet=" + sheetRender.getText(sheet);
    vm.runInNewContext(
      "(() => {" +
        ts.transpileModule(text, {
          compilerOptions: {
            jsx: ts.JsxEmit.ReactJSX,
            module: ts.ModuleKind.CommonJS,
          },
        }).outputText +
        "})()",
      sandbox,
    );
    return sandbox
      .renderSheet(element.props, null)
      .props.children.find((child) => child.type === "DialogContent");
  };
  return {
    sandbox,
    document,
    content,
    opener,
    opening,
    close,
    forward,
    focuses: () => focuses,
    desktop: () => desktop,
  };
}
test("actual SidebarTrigger → Sidebar Sheet → forwarded SheetContent → installed Radix modal close restores mobile Escape opener; original null-trigger fallback does not", () => {
  const f = fixture(),
    element = f.opening(),
    forwarded = f.forward(element);
  assert.equal(
    forwarded.props.onCloseAutoFocus,
    element.props.onCloseAutoFocus,
  );
  f.close(forwarded);
  assert.equal(f.focuses(), 1);
  assert.equal(f.document.activeElement, f.opener);
  const original = fixture(),
    opened = original.opening();
  original.sandbox.props = { ...opened.props, onCloseAutoFocus: undefined };
  vm.runInNewContext(
    "oldClose=" + installedClose.getText(radix),
    original.sandbox,
  );
  original.document.activeElement = original.document.body;
  original.sandbox.oldClose({
    defaultPrevented: false,
    preventDefault() {
      this.defaultPrevented = true;
    },
  });
  assert.equal(original.focuses(), 0);
});
test("actual close callback never steals newer destination focus/navigation, hidden/disconnected opener or reopened cycle", () => {
  for (const change of [
    (f) => (f.opener.isConnected = false),
    (f) => (f.opener.disabled = true),
    (f) => (f.opener.getClientRects = () => []),
    (f) =>
      (f.document.defaultView.getComputedStyle = () => ({
        visibility: "hidden",
      })),
    (f) => (f.document.defaultView.location.href += "#destination"),
    (f) => (f.document.activeElement = { destination: true }),
  ]) {
    const f = fixture(),
      element = f.opening();
    element.props.onEscapeKeyDown();
    f.sandbox.setOpenMobile(false);
    f.document.activeElement = f.document.body;
    change(f);
    element.props.onCloseAutoFocus({ preventDefault() {} });
    assert.equal(f.focuses(), 0);
  }
  const f = fixture(),
    old = f.opening();
  old.props.onEscapeKeyDown();
  f.sandbox.setOpenMobile(false);
  const current = f.opening();
  old.props.onCloseAutoFocus({ preventDefault() {} });
  assert.equal(f.focuses(), 0);
  f.close(current);
  assert.equal(f.focuses(), 1);
});
test("desktop SidebarTrigger retains original toggle and no mobile focus capture", () => {
  const f = fixture();
  f.sandbox.isMobile = false;
  f.sandbox.exports
    .SidebarTrigger({})
    .props.onClick({ currentTarget: f.opener });
  assert.equal(f.desktop(), 1);
  assert.equal(f.sandbox.mobileFocus.cycle(), 0);
});

test("actual SidebarRail passes currentTarget rather than MouseEvent, preserving mobile Escape and default desktop toggle", () => {
  const f = fixture();
  f.document.activeElement = f.opener;
  f.sandbox.exports.SidebarRail({}).props.onClick({ currentTarget: f.opener });
  const sheet = f.sandbox.exports.Sidebar({}).props.children;
  f.close(sheet);
  assert.equal(f.focuses(), 1);
  const desktop = fixture();
  desktop.sandbox.isMobile = false;
  desktop.sandbox.exports
    .SidebarRail({})
    .props.onClick({ currentTarget: desktop.opener });
  assert.equal(desktop.desktop(), 1);
});
