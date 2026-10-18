import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import vm from "node:vm";
import ts from "typescript";
import * as quick from "./quick-store.ts";
import { initialMetadata } from "./draft-state.ts";
const require = createRequire(import.meta.url);
const scope = {
  actorID: "10000000-0000-4000-8000-000000000015",
  workspaceID: "20000000-0000-4000-8000-000000000015",
};
function mountedDialog(extra = {}) {
  const effects = [];
  let focused = 0;
  class Element {
    isConnected = true;
    getClientRects() {
      return [1];
    }
    closest() {
      return null;
    }
    focus() {
      focused++;
    }
  }
  const opener = new Element(),
    body = new Element();
  const document = { activeElement: opener, body };
  const draft = quick.quickDrafts.ensure(scope, initialMetadata(), []);
  const bindings = {
    react: {
      useEffect: (setup) => effects.push(setup),
      useRef: (value) => ({ current: value }),
      useState: (value) => [
        typeof value === "function" ? value() : value,
        () => {},
      ],
    },
    "@/components/ui/button": { Button: "button" },
    "@/components/ui/dialog": Object.fromEntries(
      [
        "Dialog",
        "DialogContent",
        "DialogHeader",
        "DialogTitle",
        "DialogDescription",
      ].map((name) => [name, name]),
    ),
    "@/features/issues/issue-form": { IssueForm: "IssueForm" },
    "@/features/workspaces/queries": {
      useWorkspace: () => ({ isSuccess: true }),
    },
    "@/features/documents/queries": { useDocumentMentions: () => ({}) },
    "@/features/documents/shared/editor": { DocumentEditor: "DocumentEditor" },
    "@/features/documents/shared/contract": {
      paragraph: () => ({}),
      validateContent() {},
    },
    "@/providers/auth-provider": {
      useAuth: () => ({
        bootstrapped: true,
        isAuthenticated: true,
        user: { uuid: scope.actorID },
      }),
    },
    "@/providers/locale-provider": { useLocale: () => ({ t: (key) => key }) },
    "./draft-state": { initialMetadata },
    "./quick-store": quick,
    "./operation": {
      useQuickDrafts() {},
      useQuickOperation: () => ({}),
      QuickOperationNotice: "Notice",
    },
  };
  const sandboxModule = { exports: {} };
  vm.runInNewContext(
    ts.transpileModule(
      readFileSync(
        new URL("./quick-issue-dialog.tsx", import.meta.url),
        "utf8",
      ),
      {
        compilerOptions: {
          module: ts.ModuleKind.CommonJS,
          jsx: ts.JsxEmit.ReactJSX,
        },
      },
    ).outputText,
    {
      module: sandboxModule,
      exports: sandboxModule.exports,
      document,
      HTMLElement: Element,
      queueMicrotask: (run) => run(),
      JSON,
      require: (name) => (name in bindings ? bindings[name] : require(name)),
    },
  );
  const outer = sandboxModule.exports.QuickIssueDialog({
    ...scope,
    open: true,
    onOpenChange() {},
    onCreated() {},
    reviewLatest: async () => {},
    canCreate: true,
    ...extra,
  });
  const dialog = outer.type(outer.props);
  // React's passive mount effects run from child to parent. Exercise the actual
  // exported consumer on its first render already open, rather than a closed shell.
  effects.toReversed().forEach((setup) => setup());
  return { dialog, draft, document, body, focused: () => focused };
}
test("first-open quick dialog retains its active draft after child-before-parent effects", () => {
  const { draft } = mountedDialog();
  assert.equal(
    quick.quickContextMatches(scope, draft.id, quick.quickContextTicket()),
    true,
  );
});
test("supplied null focus destination suppresses the opener fallback", () => {
  const { dialog, document, body, focused } = mountedDialog({
    returnFocus: () => null,
  });
  document.activeElement = body;
  dialog.props.children.props.onCloseAutoFocus({ preventDefault() {} });
  assert.equal(focused(), 0);
});
test("quick dialog close never steals a newer connected focus destination", () => {
  const { dialog, document, focused } = mountedDialog();
  document.activeElement = new document.activeElement.constructor();
  dialog.props.children.props.onCloseAutoFocus({ preventDefault() {} });
  assert.equal(focused(), 0);
});
