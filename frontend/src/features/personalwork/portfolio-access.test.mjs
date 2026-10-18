import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import vm from "node:vm";
import ts from "typescript";
import React from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { QueryClient, QueryObserver } from "@tanstack/react-query";
import { createRequire } from "node:module";
const require = createRequire(import.meta.url),
  sandboxModule = { exports: {} };
vm.runInNewContext(
  ts.transpileModule(
    readFileSync(new URL("./portfolio-access.tsx", import.meta.url), "utf8"),
    {
      compilerOptions: {
        module: ts.ModuleKind.CommonJS,
        jsx: ts.JsxEmit.ReactJSX,
      },
    },
  ).outputText,
  { module: sandboxModule, exports: sandboxModule.exports, require, Set },
);
const { PortfolioAccessGate, capturedScopesAuthorized } = sandboxModule.exports;
test("mounted real workspace QueryObserver omission hides entire cached portfolio before later page403, using original scope set rather than dirty draft", async () => {
  const anchor = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
    other = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
    client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  let resolveList, rejectPage;
  const list = new QueryObserver(client, {
      queryKey: ["current-workspaces"],
      initialData: [anchor, other],
      staleTime: Infinity,
      queryFn: () => new Promise((resolve) => (resolveList = resolve)),
    }),
    snapshot = {
      name: "PRIVATE_REMOVED_WORKSPACE",
      count: "9007199254740993",
      rows: ["PRIVATE_ISSUE"],
    },
    page = new QueryObserver(client, {
      queryKey: ["cached-session"],
      initialData: snapshot,
      staleTime: Infinity,
      queryFn: () => new Promise((resolve, reject) => (rejectPage = reject)),
    });
  const unsubList = list.subscribe(() => {}),
    unsubPage = page.subscribe(() => {});
  const original = [anchor, other],
    dirtyDraft = [anchor];
  function render(scopes = original) {
    const current = list.getCurrentResult();
    return renderToStaticMarkup(
      React.createElement(
        PortfolioAccessGate,
        {
          anchorID: anchor,
          capturedWorkspaceIDs: scopes,
          currentWorkspaceIDs: current.data,
          complete: current.isSuccess && !current.isFetching,
          readable: true,
        },
        React.createElement(
          "section",
          null,
          snapshot.name + " " + snapshot.count + " " + snapshot.rows[0],
        ),
      ),
    );
  }
  try {
    assert(render().includes(snapshot.name));
    const pageRequest = page.refetch();
    await Promise.resolve();
    assert(page.getCurrentResult().isFetching);
    const listRequest = list.refetch();
    await Promise.resolve();
    assert.equal(render(), "");
    resolveList([anchor]);
    await listRequest;
    assert(list.getCurrentResult().isSuccess);
    assert(!list.getCurrentResult().isFetching);
    assert.equal(page.getCurrentResult().data, snapshot);
    assert(page.getCurrentResult().isFetching);
    assert.equal(render(), "");
    assert(
      render(dirtyDraft).includes(snapshot.name),
      "negative control: using only current draft scopes would reveal cached private payload",
    );
    rejectPage(
      Object.assign(new Error("scope changed"), {
        status: 403,
        details: [{ field: "session_id", code: "scope_changed" }],
      }),
    );
    await pageRequest;
    assert(page.getCurrentResult().isError);
    assert.equal(page.getCurrentResult().data, snapshot);
    assert.equal(render(), "");
    assert(
      !capturedScopesAuthorized(anchor, [other], [other], true),
      "anchor omission denies even when anchor is not selected",
    );
    assert(
      capturedScopesAuthorized(anchor, [other], [anchor, other], true),
      "authorized unselected anchor is not auto-inserted",
    );
    const source = readFileSync(
      new URL("./personal-home.tsx", import.meta.url),
      "utf8",
    );
    assert(
      source.includes(
        "capturedWorkspaceIDs={applied.command.body.workspace_ids}",
      ),
    );
  } finally {
    unsubList();
    unsubPage();
    list.destroy();
    page.destroy();
    client.clear();
  }
});
