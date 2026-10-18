import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import vm from "node:vm";
import ts from "typescript";
import * as query from "../searchviews/query.ts";
import * as budget from "../searchviews/query-import.ts";
import * as transport from "./portfolio-transport.ts";
import * as scope from "./scope.ts";
const calls = [],
  sandboxModule = { exports: {} },
  w = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
  actor = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb";
let pages = [];
const searchSource = ts.createSourceFile(
    "service.ts",
    readFileSync(
      new URL("../searchviews/search.service.ts", import.meta.url),
      "utf8",
    ),
    ts.ScriptTarget.Latest,
    true,
    ts.ScriptKind.TS,
  ),
  functionSource = searchSource.statements.find(
    (node) =>
      ts.isFunctionDeclaration(node) && node.name?.text === "definitionQuery",
  );
const pure = { ...query };
vm.runInNewContext(
  ts.transpileModule(
    functionSource
      .getText(searchSource)
      .replace("export function", "function") +
      ";definitionQueryExport=definitionQuery;",
    { compilerOptions: { target: ts.ScriptTarget.ES2022 } },
  ).outputText,
  pure,
);
const apiClient = Object.fromEntries(
  ["GET", "POST"].map((method) => [
    method,
    async (path, options) => {
      calls.push({ method, path, options });
      return pages.shift() ?? {};
    },
  ]),
);
vm.runInNewContext(
  ts.transpileModule(
    readFileSync(new URL("./personal.service.ts", import.meta.url), "utf8"),
    {
      compilerOptions: {
        module: ts.ModuleKind.CommonJS,
        target: ts.ScriptTarget.ES2022,
      },
    },
  ).outputText,
  {
    module: sandboxModule,
    exports: sandboxModule.exports,
    structuredClone,
    Set,
    require(name) {
      if (name === "@/lib/api")
        return {
          apiClient,
          unwrap: async (value) => value,
          parseApiError: (status) =>
            Object.assign(new Error("VALIDATION_ERROR"), { status }),
        };
      if (name.endsWith("/query")) return query;
      if (name.endsWith("/search.service"))
        return { definitionQuery: pure.definitionQueryExport };
      if (name.endsWith("/query-import")) return budget;
      if (name === "./portfolio-transport") return transport;
      if (name === "./scope") return scope;
      throw new Error(name);
    },
  },
);
const { personalService, portfolioBody } = sandboxModule.exports;
const plain = (value) => JSON.parse(JSON.stringify(value));
function capture(resource = "issue") {
  return {
    workspaceID: w,
    path: `/v1/workspaces/${w}/personal-work/sessions`,
    body: {
      request_id: crypto.randomUUID(),
      workspace_ids: [w],
      definition: {
        resource,
        issue: resource === "issue" ? query.emptyQuery() : null,
        project: resource === "project" ? query.emptyQuery() : null,
        team: resource === "team" ? query.emptyQuery() : null,
      },
      assigned_to_me: resource === "issue",
      ttl_seconds: null,
      limit: 100,
    },
  };
}
test("actual five new frontend bindings preserve exact scope, read request identity, receipt/null CAS, opaque cursor and configured signal", async () => {
  const signal = new AbortController().signal;
  await personalService.config(w, signal);
  assert.equal(
    calls.at(-1).path,
    "/v1/workspaces/{workspaceID}/personal-work/config",
  );
  for (const resource of ["issue", "project", "team"]) {
    const command = capture(resource);
    assert(portfolioBody(command.body));
    await personalService.capture(command, signal);
    const call = calls.at(-1);
    assert.equal(call.method, "POST");
    assert.equal(
      call.path,
      "/v1/workspaces/{workspaceID}/personal-work/sessions",
    );
    assert.equal(call.options.signal, signal);
    assert(!("header" in call.options.params));
    assert.deepEqual(JSON.parse(call.options.bodySerializer()), command.body);
  }
  await personalService.page(w, "session", "opaque + / =", signal);
  assert.equal(calls.at(-1).options.params.query.after, "opaque + / =");
  const body = {
    target: { kind: "issue", id: actor },
    active: true,
    expected_revision: null,
  };
  await personalService.setFavorite(
    {
      action: "favorite.set",
      workspaceID: w,
      path: `/v1/workspaces/${w}/favorites`,
      body,
    },
    "frozen-key",
    signal,
  );
  assert.deepEqual(plain(calls.at(-1).options.body), body);
  assert.equal(
    calls.at(-1).options.params.header["Idempotency-Key"],
    "frozen-key",
  );
  assert.equal(calls.at(-1).options.signal, signal);
});
test("complete private favorites exhaust105 rows, retain inactive CAS rows, intersect current capabilities and reject foreign workspace pages", async () => {
  const rows = Array.from({ length: 105 }, (_, i) => ({
    id: `cccccccc-cccc-cccc-cccc-${i.toString(16).padStart(12, "0")}`,
    workspace_id: w,
    actor_id: actor,
    active: i % 2 === 0,
  }));
  pages = [
    {
      items: rows.slice(0, 100),
      has_more: true,
      next_cursor: "next opaque",
      capabilities: { manage: true },
    },
    {
      items: rows.slice(100),
      has_more: false,
      next_cursor: "",
      capabilities: { manage: false },
    },
  ];
  const all = await personalService.favorites(w);
  assert.deepEqual(all.items, rows);
  assert.equal(all.capabilities.manage, false);
  assert.equal(calls.at(-1).options.params.query.include_inactive, true);
  assert.equal(calls.at(-1).options.params.query.after, "next opaque");
  pages = [
    {
      items: [{ ...rows[0], workspace_id: actor }],
      has_more: false,
      next_cursor: "",
      capabilities: { manage: true },
    },
  ];
  await assert.rejects(() => personalService.favorites(w), /SELECTOR_CHANGED/);
});
test("full portfolio domain overhead and invalid assigned resource reject definitively before any network or uncertain outcome", async () => {
  const before = calls.length,
    invalid = capture("project");
  invalid.body.assigned_to_me = true;
  await assert.rejects(
    () => personalService.capture(invalid),
    (error) => error.status === 400,
  );
  const command = capture(),
    ids = Array.from(
      { length: 100 },
      (_, i) => `dddddddd-dddd-dddd-dddd-${i.toString(16).padStart(12, "0")}`,
    );
  command.body.definition.issue.filter = {
    kind: "all",
    children: Array.from({ length: 40 }, () => ({
      kind: "uuid",
      field: "id",
      op: "in",
      ids,
    })),
  };
  assert(
    new TextEncoder().encode(JSON.stringify(command.body)).length > 131072,
  );
  await assert.rejects(
    () => personalService.capture(command),
    (error) => error.status === 400,
  );
  assert.equal(calls.length, before);
});
