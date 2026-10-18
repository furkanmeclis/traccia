import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import vm from "node:vm";
import ts from "typescript";
import * as queries from "./query.ts";
import * as transport from "./view-transport.ts";
import * as queryImport from "./query-import.ts";
const calls = [],
  sandboxModule = { exports: {} };
let result = {};
const apiClient = {
  GET: async (path, options) => {
    calls.push({ method: "GET", path, options });
    return result;
  },
  POST: async (path, options) => {
    calls.push({ method: "POST", path, options });
    return result;
  },
};
vm.runInNewContext(
  ts.transpileModule(
    readFileSync(new URL("./search.service.ts", import.meta.url), "utf8"),
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
    require(name) {
      if (name === "@/lib/api")
        return {
          apiClient,
          unwrap: async (value) => value,
          parseApiError: (status, body) =>
            Object.assign(new Error(body.error.code), {
              status,
              code: body.error.code,
            }),
        };
      if (name === "./query") return queries;
      if (name === "./view-transport") return transport;
      if (name === "./query-import") return queryImport;
      throw new Error(name);
    },
  },
);
const { searchService, executeViewCommand, definition } = sandboxModule.exports,
  plain = (v) => JSON.parse(JSON.stringify(v)),
  workspaceID = "workspace",
  viewID = "view",
  signal = new AbortController().signal;
test("actual logical-read POST binding preserves body request UUID/query/TTL/page limit and signal across all three resources without any receipt header", async () => {
  for (const [resource, plural] of [
    ["issue", "issues"],
    ["project", "projects"],
    ["team", "teams"],
  ]) {
    const body = {
      request_id: crypto.randomUUID(),
      query: { ...queries.emptyQuery(), search: "  İ 👩🏽‍💻 e\u0301\r\n " },
      ttl_seconds: null,
      limit: 100,
    };
    const command = {
      workspaceID,
      resource,
      path: `/v1/workspaces/${workspaceID}/search/${plural}/sessions`,
      body,
    };
    await searchService.capture(command, signal);
    const call = calls.at(-1);
    assert.equal(
      call.path,
      `/v1/workspaces/{workspaceID}/search/${plural}/sessions`,
    );
    assert.deepEqual(plain(call.options.body), body);
    assert.equal(call.options.signal, signal);
    assert(!("header" in call.options.params));
    await searchService.page(
      workspaceID,
      resource,
      "session",
      "opaque+/==",
      signal,
    );
    assert.equal(calls.at(-1).options.params.query.after, "opaque+/==");
    assert.equal(calls.at(-1).options.signal, signal);
  }
});
test("all five actual personal write bindings preserve closed body, original UUID/null CAS, canonical path, signal and stable receipt key", async () => {
  const body = {
    name: "  Private İ ",
    definition: definition("issue", queries.emptyQuery()),
    columns: queries.defaultColumns("issue"),
  };
  for (const [action, path, payload] of [
    ["view.create", "/saved-views", body],
    [
      "view.update",
      "/saved-views/view/update",
      { ...body, expected_revision: "old-revision" },
    ],
    [
      "view.delete",
      "/saved-views/view/delete",
      { expected_revision: "old-revision" },
    ],
    [
      "view.restore",
      "/saved-views/view/restore",
      { expected_revision: "old-revision" },
    ],
    [
      "columns.set",
      "/column-preferences",
      { resource: "issue", expected_revision: null, columns: body.columns },
    ],
  ]) {
    const command = {
      action,
      workspaceID,
      ...(action.startsWith("view.") && action !== "view.create"
        ? { viewID }
        : {}),
      path: "/v1/workspaces/workspace" + path,
      body: payload,
    };
    await executeViewCommand(command, "frozen-key", signal);
    const call = calls.at(-1);
    assert.deepEqual(
      call.options.bodySerializer
        ? JSON.parse(call.options.bodySerializer(call.options.body))
        : plain(call.options.body),
      plain(payload),
    );
    assert.deepEqual(plain(call.options.params.header), {
      "Idempotency-Key": "frozen-key",
    });
    assert.equal(call.options.signal, signal);
    assert(!("view_id" in call.options.body));
    assert.equal(
      call.path,
      "/v1/workspaces/{workspaceID}" + path.replace("/view/", "/{viewID}/"),
    );
  }
});
test("service refuses mismatched frozen paths and invalid resource AST or columns before transport", async () => {
  const count = calls.length;
  await assert.rejects(() =>
    searchService.capture({
      workspaceID,
      resource: "team",
      path: "/wrong",
      body: {
        request_id: "id",
        query: queries.emptyQuery(),
        ttl_seconds: null,
        limit: 100,
      },
    }),
  );
  await assert.rejects(() =>
    executeViewCommand(
      {
        action: "columns.set",
        workspaceID,
        path: "/v1/workspaces/workspace/column-preferences",
        body: {
          resource: "team",
          expected_revision: null,
          columns: [{ key: "title", width: 100 }],
        },
      },
      "key",
    ),
  );
  assert.equal(calls.length, count);
});

test("final submit validates complete canonical capture/update domain overhead as definitive400 before transport, with path-only view UUID excluded from wire", async () => {
  const id = "abcdef12-1234-1234-1234-123456789abc",
    ids = Array.from(
      { length: 100 },
      (_, i) => id.slice(0, -8) + i.toString(16).padStart(8, "0"),
    ),
    leaf = (n) => ({
      kind: "uuid",
      field: "id",
      op: "in",
      ids: ids.slice(0, n),
    }),
    near = {
      ...queries.emptyQuery(),
      filter: {
        kind: "all",
        children: [...Array.from({ length: 33 }, () => leaf(100)), leaf(17)],
      },
    };
  assert(queries.validateQuery("issue", near));
  assert(queryImport.withinDomainBudget(near));
  const count = calls.length;
  const captureBody = {
    request_id: id,
    query: near,
    ttl_seconds: null,
    limit: 100,
  };
  assert(!queryImport.withinDomainBudget(captureBody));
  await assert.rejects(
    () =>
      searchService.capture({
        workspaceID,
        resource: "issue",
        path: "/v1/workspaces/workspace/search/issues/sessions",
        body: captureBody,
      }),
    (error) => error.status === 400 && error.code === "VALIDATION_ERROR",
  );
  assert.equal(calls.length, count);
  const query = {
      ...queries.emptyQuery(),
      filter: {
        kind: "all",
        children: [...Array.from({ length: 33 }, () => leaf(100)), leaf(5)],
      },
    },
    body = {
      name: "view",
      expected_revision: id,
      definition: definition("issue", query),
      columns: queries.defaultColumns("issue"),
    };
  const fill = 131060 - JSON.stringify(body).length;
  assert(fill >= 0 && fill <= 256);
  query.search = "x".repeat(fill);
  assert(queries.validateQuery("issue", query));
  assert(queryImport.withinDomainBudget(body));
  assert(!queryImport.withinDomainBudget({ ...body, view_id: id }));
  await assert.rejects(
    () =>
      executeViewCommand(
        {
          action: "view.update",
          workspaceID,
          viewID: id,
          path: `/v1/workspaces/workspace/saved-views/${id}/update`,
          body,
        },
        "same-key",
      ),
    (error) => error.status === 400 && error.code === "VALIDATION_ERROR",
  );
  assert.equal(calls.length, count);
  assert(!("view_id" in body));
  assert.equal(body.definition.issue.search, "x".repeat(fill));
});
