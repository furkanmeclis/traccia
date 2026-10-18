import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import vm from "node:vm";
import ts from "typescript";
import React from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { QueryClient, QueryObserver } from "@tanstack/react-query";
import { createRequire } from "node:module";
const require = createRequire(import.meta.url);
const source = ts.createSourceFile(
  "panel.tsx",
  readFileSync(new URL("./attachment-panel.tsx", import.meta.url), "utf8"),
  ts.ScriptTarget.Latest,
  true,
  ts.ScriptKind.TSX,
);
const component = source.statements.find(
  (n) => ts.isFunctionDeclaration(n) && n.name?.text === "Download",
);
assert(component);
const sandboxModule = { exports: {} };
const code = ts.transpileModule(
  `import {useState,useEffect,useSyncExternalStore} from 'react'; const Button='button'; const useLocale=()=>({t:(key)=>key}); const MAX_FILE_BYTES=268435456; export ${component.getText(source)}`,
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
  require,
});
const { Download } = sandboxModule.exports;
const file = {
  size_bytes: 0,
  sha256: "0".repeat(64),
  download_filename: "zero.bin",
  capabilities: { download: true },
};
function disabled(config, ready = true, currentFile = file) {
  return /<button[^>]*disabled/.test(
    renderToStaticMarkup(
      React.createElement(Download, {
        config,
        ready,
        file: currentFile,
        workspaceID: "w",
        draftID: "",
      }),
    ),
  );
}
test("actual Download consumes one parent observer: finite config reads, current error/fetch/capability gates retained", async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  let calls = 0,
    fail = false;
  const observer = new QueryObserver(client, {
    queryKey: ["attachments", "w", "actor", "config"],
    staleTime: 0,
    enabled: true,
    queryFn: async () => {
      calls++;
      if (fail) throw Error("read failed");
      return {
        limits: { chunk_bytes: 1024, blob_fallback_max_bytes: 268435456 },
      };
    },
  });
  const observed = [];
  const unsubscribe = observer.subscribe((result) =>
    observed.push(disabled(result)),
  );
  await observer.refetch();
  assert.equal(calls, 1);
  assert.equal(disabled(observer.getCurrentResult()), false);
  await new Promise((resolve) => setTimeout(resolve, 25));
  assert.equal(
    calls,
    1,
    "rendering Download must not create a readiness-dependent config observer",
  );
  assert.equal(disabled(observer.getCurrentResult(), false), true);
  assert.equal(
    disabled(observer.getCurrentResult(), true, {
      ...file,
      capabilities: { download: false },
    }),
    true,
  );
  fail = true;
  await observer.refetch();
  assert.equal(disabled(observer.getCurrentResult()), true);
  fail = false;
  await observer.refetch();
  assert.equal(calls, 3);
  assert.equal(disabled(observer.getCurrentResult()), false);
  assert(observed.includes(true));
  unsubscribe();
  client.clear();
});
