import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import vm from "node:vm";
import ts from "typescript";
import createClient from "openapi-fetch";
import { portfolioTransport } from "./portfolio-transport.ts";
import { emptyQuery } from "../searchviews/query.ts";
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

function body(resource) {
  return {
    request_id: crypto.randomUUID(),
    workspace_ids: [crypto.randomUUID(), crypto.randomUUID()],
    definition: {
      resource,
      issue: resource === "issue" ? emptyQuery() : null,
      project: resource === "project" ? emptyQuery() : null,
      team: resource === "team" ? emptyQuery() : null,
    },
    assigned_to_me: resource === "issue",
    ttl_seconds: null,
    limit: 100,
  };
}
test("actual installed apiClient preserves all portfolio null branches, explicit scopes and request identity with no receipt key despite helper omissions", async () => {
  const captures = [],
    api = client(async (request) => {
      captures.push({
        url: request.url,
        body: await request.clone().text(),
        key: request.headers.get("Idempotency-Key"),
      });
      return Response.json({ success: true, data: {} });
    });
  for (const resource of ["issue", "project", "team"]) {
    const canonical = body(resource),
      expected = JSON.stringify(canonical),
      prepared = portfolioTransport(canonical);
    canonical.workspace_ids.pop();
    canonical.definition[resource].search = "mutated after preparation";
    await api.POST("/v1/workspaces/{workspaceID}/personal-work/sessions", {
      params: { path: { workspaceID: "anchor" } },
      ...prepared,
    });
    const wire = captures.at(-1);
    assert.equal(wire.body, expected);
    assert.equal(wire.key, null);
    const actual = JSON.parse(wire.body);
    for (const inactive of ["issue", "project", "team"].filter(
      (value) => value !== resource,
    ))
      assert.equal(actual.definition[inactive], null);
    assert.equal(actual.workspace_ids.length, 2);
    assert(!actual.workspace_ids.includes("anchor"));
  }
});
test("actual shared401 refresh clone replays identical portfolio read bytes/request UUID without adding receipt semantics", async () => {
  const captures = [];
  let first = true;
  const api = client(async (request, options) => {
      if (typeof request === "string") {
        assert(request.endsWith("/v1/auth/refresh"));
        assert.equal(options.method, "POST");
        return Response.json({ success: true, data: { authenticated: true } });
      }
      captures.push({
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
    }),
    canonical = body("issue");
  await api.POST("/v1/workspaces/{workspaceID}/personal-work/sessions", {
    params: { path: { workspaceID: "anchor" } },
    ...portfolioTransport(canonical),
  });
  assert.equal(captures.length, 2);
  assert.deepEqual(captures[0], captures[1]);
  assert.equal(captures[0].body, JSON.stringify(canonical));
  assert.equal(captures[0].key, null);
});
