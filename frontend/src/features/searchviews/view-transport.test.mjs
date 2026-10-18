import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import vm from "node:vm";
import ts from "typescript";
import createClient from "openapi-fetch";
import { viewTransport, updateViewTransport } from "./view-transport.ts";
import { emptyQuery, defaultColumns } from "./query.ts";
function client(fetch) {
  const sandboxModule = { exports: {} };
  vm.runInNewContext(
    ts.transpileModule(
      readFileSync(new URL("../../lib/api/client.ts", import.meta.url), "utf8"),
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
      Request,
      Response,
      Headers,
      URL,
      WeakMap,
      fetch,
      require(name) {
        if (name === "openapi-fetch")
          return { default: (options) => createClient({ ...options, fetch }) };
        if (name === "@/config/api")
          return { apiConfig: { baseUrl: "https://same-origin.test/api" } };
        if (name === "./errors")
          return {
            ApiError: class extends Error {},
            emitApiError() {},
            parseApiError() {
              throw new Error("unexpected error");
            },
          };
        if (name.includes("step-up"))
          return { runStepUpEnsure: async () => false };
        throw new Error(name);
      },
    },
  );
  return sandboxModule.exports.apiClient;
}
function canonical(resource) {
  return {
    name: "  İ e\u0301 👩🏽‍💻\r\n ",
    definition: {
      resource,
      issue: resource === "issue" ? emptyQuery() : null,
      project: resource === "project" ? emptyQuery() : null,
      team: resource === "team" ? emptyQuery() : null,
    },
    columns: defaultColumns(resource),
  };
}
test("actual installed apiClient Request emits canonical create/update JSON with both required null slots for every resource, despite helper-only argument omission", async () => {
  const captured = [],
    api = client(async (request) => {
      captured.push({
        body: await request.clone().text(),
        key: request.headers.get("Idempotency-Key"),
        url: request.url,
      });
      return Response.json({ success: true, data: {} });
    });
  for (const resource of ["issue", "project", "team"])
    for (const update of [false, true]) {
      const original = canonical(resource),
        body = update
          ? { ...original, expected_revision: "frozen-revision" }
          : original,
        expected = JSON.stringify(body),
        prepared = update ? updateViewTransport(body) : viewTransport(body);
      body.name = "mutated after preparation";
      body.definition[resource].search = "mutated draft";
      assert(!("issue" in prepared.body.definition) || resource === "issue");
      const path = update
        ? "/v1/workspaces/{workspaceID}/saved-views/{viewID}/update"
        : "/v1/workspaces/{workspaceID}/saved-views";
      await api.POST(path, {
        params: {
          path: {
            workspaceID: "workspace",
            ...(update ? { viewID: "view" } : {}),
          },
          header: { "Idempotency-Key": "fixed-key" },
        },
        ...prepared,
      });
      const wire = captured.at(-1);
      assert.equal(wire.body, expected);
      assert.equal(wire.key, "fixed-key");
      const json = JSON.parse(wire.body);
      for (const other of ["issue", "project", "team"].filter(
        (v) => v !== resource,
      ))
        assert.equal(json.definition[other], null);
      assert(!("view_id" in json));
    }
});
test("actual shared apiClient 401 request-clone refresh retry preserves serializer bytes and receipt key without losing explicit nulls", async () => {
  const captured = [];
  let first = true;
  const api = client(async (request, options) => {
    if (typeof request === "string") {
      assert(request.endsWith("/v1/auth/refresh"));
      assert.equal(options.method, "POST");
      return Response.json({ success: true, data: { authenticated: true } });
    }
    captured.push({
      body: await request.clone().text(),
      key: request.headers.get("Idempotency-Key"),
    });
    if (first) {
      first = false;
      return Response.json(
        { success: false, error: { code: "UNAUTHORIZED" } },
        { status: 401 },
      );
    }
    return Response.json({ success: true, data: {} });
  });
  const body = canonical("project");
  await api.POST("/v1/workspaces/{workspaceID}/saved-views", {
    params: {
      path: { workspaceID: "workspace" },
      header: { "Idempotency-Key": "same-receipt" },
    },
    ...viewTransport(body),
  });
  assert.equal(captured.length, 2);
  assert.equal(captured[0].body, JSON.stringify(body));
  assert.deepEqual(captured[1], captured[0]);
});
