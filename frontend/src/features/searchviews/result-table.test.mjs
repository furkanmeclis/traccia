import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import vm from "node:vm";
import ts from "typescript";
import React from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { createRequire } from "node:module";
const require = createRequire(import.meta.url),
  sandboxModule = { exports: {} };
vm.runInNewContext(
  ts.transpileModule(
    readFileSync(new URL("./result-table.tsx", import.meta.url), "utf8"),
    {
      compilerOptions: {
        module: ts.ModuleKind.CommonJS,
        jsx: ts.JsxEmit.ReactJSX,
        target: ts.ScriptTarget.ES2022,
      },
    },
  ).outputText,
  {
    module: sandboxModule,
    exports: sandboxModule.exports,
    require(name) {
      if (name === "next/link")
        return {
          default: ({ children, ...props }) =>
            React.createElement("a", props, children),
        };
      if (name === "@/providers/locale-provider")
        return { useLocale: () => ({ t: (key) => key }) };
      return require(name);
    },
  },
);
const props = {
  resource: "issue",
  workspaceID: "workspace",
  session: {
    id: "capture",
    request_id: "request",
    workspace_id: "workspace",
    resource: "issue",
    as_of: "2026-10-18T00:00:00Z",
    expires_at: "2026-10-18T00:30:00Z",
    total_count: "9007199254740993",
  },
  query: {
    version: 1,
    archived: "active",
    search: "private-applied-query",
    filter: null,
    sort: [],
  },
  rows: [
    {
      id: "stable-uuid",
      workspace_id: "workspace",
      identifier: "T-9007199254740993",
      title: "<script>private-row</script>",
      version: "9007199254740993",
    },
  ],
  columns: [
    { key: "identifier", width: 180 },
    { key: "title", width: 320 },
  ],
};
test("actual snapshot renderer pairs exact decimal count/as-of/applied query, escapes metadata and links stable UUID; failed read hides all cached server metadata", () => {
  const { SnapshotTable } = sandboxModule.exports,
    html = renderToStaticMarkup(
      React.createElement(SnapshotTable, { ...props, readable: true }),
    );
  assert(html.includes("9007199254740993"));
  assert(!html.includes("9007199254740992"));
  assert(html.includes("private-applied-query"));
  assert(html.includes(props.session.as_of));
  assert(html.includes('href="/w/workspace/issues/stable-uuid"'));
  assert(html.includes("&lt;script&gt;private-row&lt;/script&gt;"));
  assert(!html.includes("<script>"));
  const blocked = renderToStaticMarkup(
    React.createElement(SnapshotTable, { ...props, readable: false }),
  );
  for (const secret of [
    "9007199254740993",
    "private-applied-query",
    props.session.as_of,
    "private-row",
    "stable-uuid",
  ])
    assert(!blocked.includes(secret));
  assert(blocked.includes('role="alert"'));
});
