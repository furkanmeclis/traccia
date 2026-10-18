import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import vm from "node:vm";
import ts from "typescript";
import React from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { createCommentDraftStore } from "./draft-state.ts";
import * as contract from "../documents/shared/contract.ts";
import * as patch from "../documents/shared/patch.ts";
const require = createRequire(import.meta.url),
  store = createCommentDraftStore();
let observed;
const sandboxModule = { exports: {} };
const code = ts.transpileModule(
  readFileSync(new URL("./comment-editor.tsx", import.meta.url), "utf8"),
  {
    compilerOptions: {
      module: ts.ModuleKind.CommonJS,
      jsx: ts.JsxEmit.ReactJSX,
    },
  },
).outputText;
vm.runInNewContext(code, {
  module: sandboxModule,
  exports: sandboxModule.exports,
  structuredClone,
  require(name) {
    if (name === "react")
      return {
        ...React,
        useEffect() {},
        useSyncExternalStore: () => store.snapshot(),
      };
    if (name === "@/components/ui/button")
      return {
        Button: (props) => {
          const clean = { ...props };
          delete clean.variant;
          return React.createElement("button", clean);
        },
      };
    if (name === "@/providers/locale-provider")
      return { useLocale: () => ({ t: (k) => k }) };
    if (name === "@/features/documents/shared/editor")
      return {
        DocumentEditor: (props) => {
          observed = props;
          return React.createElement("div", { "data-editor": true });
        },
      };
    if (name === "@/features/documents/shared/contract") return contract;
    if (name === "@/features/documents/shared/patch") return patch;
    if (name === "./draft-state")
      return {
        commentDrafts: store,
        draftChanged: (d) =>
          JSON.stringify(d.content) !== JSON.stringify(d.baselineContent),
      };
    return require(name);
  },
});
const { CommentEditor } = sandboxModule.exports;
const id = "12345678-1234-4234-8234-123456789012",
  node = (text) => ({ id, type: "p", children: [{ text }] }),
  baseline = { revision: "old", content: [node("original")] };
const props = {
  draftKey: "scope",
  baseline,
  currentRevision: "old",
  ready: true,
  locked: false,
  mentions: [],
  mentionsReady: true,
  onSave() {},
  onPatch() {},
  refresh: async () => {},
};
function render(update = {}) {
  return renderToStaticMarkup(
    React.createElement(CommentEditor, { ...props, ...update }),
  );
}
function saveDisabled(html) {
  return /<button[^>]*type="submit"[^>]*disabled/.test(html);
}
test("actual comment editor remount reads dirty canonical content rather than its initial server baseline; stale read preserves draft and blocks replace/patch", () => {
  store.ensure("scope", baseline);
  store.patch("scope", {
    content: [node("dirty İ e\u0301 👩🏽‍💻\r\n")],
    raw: "{ invalid import",
  });
  let html = render();
  assert.equal(saveDisabled(html), false);
  assert.equal(
    observed.initialValue[0].children[0].text,
    "dirty İ e\u0301 👩🏽‍💻\r\n",
  );
  assert.equal(observed.initialValue[0].id, id);
  html = render({
    baseline: { revision: "new", content: [node("competitor")] },
    currentRevision: "new",
  });
  assert.equal(saveDisabled(html), true);
  assert(html.includes("comments.stale"));
  assert.equal(
    observed.initialValue[0].children[0].text,
    "dirty İ e\u0301 👩🏽‍💻\r\n",
  );
  assert.equal(store.get("scope").revision, "old");
  assert.equal(store.get("scope").raw, "{ invalid import");
  assert.match(html, /<button[^>]*disabled[^>]*>comments.patchNode/);
});
test("actual comment form requires current read/capability, no unresolved operation, and valid content before submit; editor remains mounted", () => {
  for (const update of [{ ready: false }, { locked: true }]) {
    const html = render(update);
    assert.equal(saveDisabled(html), true);
    assert.equal(observed.disabled, true);
    assert(html.includes("data-editor"));
  }
  store.patch("scope", { error: "visible invalid content" });
  const html = render();
  assert.equal(saveDisabled(html), true);
  assert(html.includes("visible invalid content"));
  assert.equal(store.get("scope").raw, "{ invalid import");
});
