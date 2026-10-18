import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import vm from "node:vm";
import ts from "typescript";
import React from "react";
import { renderToStaticMarkup } from "react-dom/server";
import {
  createOperationStore,
  operationScope,
} from "../workspaces/operation-state.mjs";
import * as snapshot from "../issues/snapshot.ts";
import * as copies from "../issues/issue-copy.ts";
import { initialMetadata } from "./draft-state.ts";
import * as quick from "./quick-store.ts";
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
const actorID = "10000000-0000-4000-8000-000000000001",
  workspaceID = "20000000-0000-4000-8000-000000000001",
  teamID = "30000000-0000-4000-8000-000000000001",
  statusID = "40000000-0000-4000-8000-000000000001",
  issueID = "50000000-0000-4000-8000-000000000001";
const legacy = createOperationStore(),
  scope = operationScope(actorID, workspaceID),
  operationKey = JSON.stringify([scope, "issue-create"]);
const ast = ts.createSourceFile(
  "issue-ui.tsx",
  source("../issues/issue-ui.tsx"),
  ts.ScriptTarget.Latest,
  true,
  ts.ScriptKind.TSX,
);
const hook = ast.statements.find(
  (n) => ts.isFunctionDeclaration(n) && n.name?.text === "useIssueMutation",
);
assert(hook);
let legacySends = 0,
  form,
  mutationOptions,
  externalSends;
const actualHook = compile(
  hook.getText(ast),
  {},
  {
    store: legacy,
    issueOperationScope: operationScope,
    useAuth: () => ({ user: { uuid: actorID } }),
    useQueryClient: () => ({ invalidateQueries: async () => {} }),
    useSyncExternalStore: () => legacy.snapshot(),
    useMutation: () => ({
      mutate() {
        legacySends++;
      },
      reset() {},
    }),
  },
).useIssueMutation;
const jsx = require("react/jsx-runtime");
const bindings = {
  "react/jsx-runtime": Object.fromEntries(
    Object.entries(jsx).map(([key, value]) => [
      key,
      key === "jsx" || key === "jsxs"
        ? (type, props, id) => {
            if (type === "form") form = props;
            return value(type, props, id);
          }
        : value,
    ]),
  ),
  "./issue-copy": copies,
  "./snapshot": snapshot,
  "@/providers/locale-provider": { useLocale: () => ({ t: (key) => key }) },
  "./queries": {
    useIssueReferences: () => ({
      isSuccess: true,
      data: {
        teams: [{ id: teamID, name: "Team", archived: false }],
        members: [],
      },
    }),
    useIssueSelection: () => ({
      isSuccess: true,
      data: {
        statuses: [{ id: statusID, name: "Ready", archived: false }],
        labels: [],
      },
    }),
  },
  "./issue-ui": {
    LoadNotice: () => null,
    MutationNotice: () => React.createElement("p", null, "LEGACY_RECOVERY"),
    useIssueMutation(options) {
      mutationOptions = options;
      return actualHook(options);
    },
  },
  "@/components/ui/button": {
    Button: (props) => {
      const clean = { ...props };
      delete clean.variant;
      return React.createElement("button", clean);
    },
  },
  "@/components/ui/label": {
    Label: (props) => React.createElement("label", props),
  },
  "@/components/ui/text-limit-input": { TextLimitInput: () => null },
  "@/components/ui/date-picker": { DatePicker: () => null },
};
const { IssueForm } = compile(source("../issues/issue-form.tsx"), bindings);
function renderForm(external = true, locked = false) {
  externalSends = [];
  const metadata = initialMetadata(teamID);
  metadata.values.title = "Retained İ e\u0301 😀";
  metadata.values.status_id = statusID;
  return renderToStaticMarkup(
    React.createElement(IssueForm, {
      workspaceId: workspaceID,
      canManage: true,
      reviewLatest: async () => {},
      controlledDraft: { snapshot: metadata, onChange() {} },
      ...(external
        ? {
            createOverride: {
              locked,
              ready: true,
              submit: (body) => externalSends.push(body),
            },
          }
        : {}),
    }),
  );
}
test("actual IssueForm external creator ignores pending/uncertain legacy recovery while default caller remains blocked", () => {
  for (const pending of [true, false]) {
    legacy.set(operationKey, {
      scope,
      label: "legacy",
      submission: {
        key: "legacy-key",
        input: {
          action: "issue.create",
          path: { workspaceID },
          body: { title: "old" },
        },
      },
      pending,
      uncertain: !pending,
      conflict: false,
      reviewed: false,
      reviewing: false,
      reviewError: false,
      validationRejected: false,
      retry() {},
      review() {},
    });
    const original = legacy.get(operationKey),
      html = renderForm();
    assert(!html.includes("LEGACY_RECOVERY"));
    assert(!/<button[^>]*type="submit"[^>]*disabled/.test(html));
    assert.equal(mutationOptions.onSaved, undefined);
    form.onSubmit({ preventDefault() {} });
    assert.equal(externalSends.length, 1);
    assert.equal(externalSends[0].title, "Retained İ e\u0301 😀");
    assert.equal(legacy.get(operationKey), original);
    assert.equal(legacySends, 0);
    const defaultHTML = renderForm(false);
    assert(defaultHTML.includes("LEGACY_RECOVERY"));
    assert(/<button[^>]*type="submit"[^>]*disabled/.test(defaultHTML));
    assert.equal(typeof mutationOptions.onSaved, "function");
    renderForm(true, true);
    form.onSubmit({ preventDefault() {} });
    assert.equal(externalSends.length, 0);
  }
});
let resolveSend;
const sent = [];
class ApiError extends Error {}
const operation = compile(
  source("./operation.tsx") + "\nexport { send, submittedRevisions };",
  {
    "@/lib/api": { ApiError },
    "@/components/ui/button": bindings["@/components/ui/button"],
    "@/providers/locale-provider": bindings["@/providers/locale-provider"],
    "@/features/workspaces/workspace.service": { workspaceService: {} },
    "@/features/workspaces/queries": {
      workspaceKeys: { activity: (...keys) => keys },
    },
    "@/features/issues/issue.service": { issueService: {} },
    "@/features/issues/queries": { issueKeys: { all: (id) => [id] } },
    "./quick-store": quick,
    "./commands": {
      executeQuickCommand(input, key) {
        sent.push({ input, key });
        return new Promise((resolve) => {
          resolveSend = resolve;
        });
      },
    },
  },
);
const cache = { invalidateQueries: async () => {} };
function start() {
  const context = { actorID, workspaceID },
    draft = quick.quickDrafts.ensure(context, initialMetadata(teamID), []);
  quick.activateQuickContext(actorID, workspaceID, draft.id);
  const entry = quick.quickOperations.begin(
    { ...context, target: draft.id },
    "issue.create",
    "",
    {
      action: "issue.create",
      path: { workspaceID },
      body: { title: "captured" },
    },
  );
  operation.submittedRevisions.set(entry.id, draft.revision);
  const callbacks = [],
    promise = operation.send(
      entry,
      quick.quickOperations.ticket(entry.id),
      quick.quickContextTicket(),
      cache,
      (result) => callbacks.push(result),
    );
  return { context, draft, entry, promise, callbacks };
}
test("production completion locks post-freeze edits and replays exactly without clearing the legacy shelf", async () => {
  const run = start(),
    frozen = structuredClone(run.entry.submission);
  assert.notEqual(run.draft.id, operationKey);
  assert.equal(
    quick.quickDrafts.patch(run.context, run.draft.id, {
      metadata: initialMetadata(),
    }),
    false,
  );
  assert(
    quick.quickOperations.failed(
      run.entry.id,
      quick.quickOperations.ticket(run.entry.id),
      "lost response",
    ),
  );
  quick.quickDrafts.markUncertain(run.draft.id);
  resolveSend(issueID);
  await run.promise;
  assert.equal(run.callbacks.length, 0);
  assert.deepEqual(quick.quickOperations.get(run.entry.id).submission, frozen);
  assert.equal(
    quick.quickDrafts.patch(run.context, run.draft.id, { content: [] }),
    false,
  );
  assert(await quick.quickOperations.review(run.entry.id, async () => true));
  quick.quickOperations.retry(run.entry.id);
  const retried = quick.quickOperations.get(run.entry.id),
    replay = operation.send(
      retried,
      quick.quickOperations.ticket(run.entry.id),
      quick.quickContextTicket(),
      cache,
      (value) => run.callbacks.push(value),
    );
  resolveSend(issueID);
  await replay;
  assert.equal(run.callbacks.length, 1);
  assert.equal(run.callbacks[0].draftID, run.draft.id);
  assert.equal(run.callbacks[0].issueID, issueID);
  assert.deepEqual(sent.at(-1), sent.at(-2));
  assert.equal(quick.quickDrafts.get(run.draft.id), undefined);
  assert(legacy.get(operationKey));
});
test("production late completion after actor A→B→A preserves the frozen original without publishing or clearing it", async () => {
  const run = start(),
    frozen = structuredClone(run.entry.submission);
  quick.activateQuickContext("other-actor", workspaceID, null);
  quick.activateQuickContext(actorID, workspaceID, run.draft.id);
  resolveSend(issueID);
  await run.promise;
  assert.equal(run.callbacks.length, 0);
  assert.equal(quick.quickDrafts.get(run.draft.id).id, run.draft.id);
  assert.deepEqual(quick.quickOperations.get(run.entry.id).submission, frozen);
  assert.equal(quick.quickDrafts.get(run.draft.id).uncertain, true);
});
