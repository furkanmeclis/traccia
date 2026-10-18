import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import vm from "node:vm";
import ts from "typescript";
import { validateContent } from "../documents/shared/contract.ts";
import { collectIssues } from "../issues/snapshot.ts";
const require = createRequire(import.meta.url),
  sandboxModule = { exports: {} },
  calls = [];
const id = "12345678-1234-4234-8234-123456789012",
  content = [{ id, type: "p", children: [{ text: "İ 👩🏽‍💻 e\u0301\r\n" }] }];
let result = { comment: { format: "plate", schema_version: 1, content } };
const apiClient = {
  POST: async (path, options) => {
    calls.push({ path, options });
    return structuredClone(result);
  },
};
const listing = [];
const issueService = {
  references: async () => ({
    members: [{ id: "inactive-user", name: "Inactive user", active: false }],
    projects: [
      { id: "archived-project", name: "Archived project", archived: true },
    ],
  }),
  list: async (w, filter, after) => {
    listing.push({ w, filter, after });
    return {
      items: [
        {
          id: filter.archived ? "archived-issue" : "active-issue",
          identifier: "A-1",
          title: "Reference",
        },
      ],
      has_more: !after,
      next_cursor: after ? "" : "second",
    };
  },
};
vm.runInNewContext(
  ts.transpileModule(
    readFileSync(new URL("./comment.service.ts", import.meta.url), "utf8"),
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
    Map,
    require(name) {
      if (name === "@/lib/api")
        return { apiClient, unwrap: async (value) => value };
      if (name === "@/features/issues/issue.service") return { issueService };
      if (name === "@/features/issues/snapshot") return { collectIssues };
      if (name === "@/features/documents/shared/contract")
        return { validateContent };
      return require(name);
    },
  },
);
const { executeCommentCommand, commentService } = sandboxModule.exports,
  plain = (value) => JSON.parse(JSON.stringify(value));
test("actual comment write bindings send only exact closed body, UUID path, receipt key and captured signal; channel and author are never fabricated", async () => {
  const controller = new AbortController();
  const fixtures = [
    [
      "create",
      { format: "plate", schema_version: 1, content },
      "/issues/{issueID}/comments",
    ],
    [
      "replace",
      {
        format: "plate",
        schema_version: 1,
        content,
        expected_revision: "head",
      },
      "/comments/{commentID}/replace",
    ],
    [
      "patch",
      {
        format: "plate",
        schema_version: 1,
        patches: [{ op: "remove_node", node_id: id }],
        expected_revision: "head",
      },
      "/comments/{commentID}/patch",
    ],
    ["delete", { expected_revision: "head" }, "/comments/{commentID}/delete"],
    [
      "restore",
      { expected_revision: "head", revision_id: "historical" },
      "/comments/{commentID}/restore",
    ],
  ];
  for (const [kind, body, suffix] of fixtures) {
    await executeCommentCommand(
      {
        workspaceID: "workspace",
        issueID: "issue",
        ...(kind !== "create" ? { commentID: "comment" } : {}),
        action: `comment.${kind}`,
        body,
      },
      "same-key",
      controller.signal,
    );
    const call = calls.at(-1);
    assert.equal(call.path, "/v1/workspaces/{workspaceID}" + suffix);
    assert.deepEqual(plain(call.options.body), body);
    assert.deepEqual(plain(call.options.params.header), {
      "Idempotency-Key": "same-key",
    });
    assert.deepEqual(
      plain(call.options.params.path),
      kind === "create"
        ? { workspaceID: "workspace", issueID: "issue" }
        : { workspaceID: "workspace", commentID: "comment" },
    );
    assert.equal(call.options.signal, controller.signal);
  }
  result = { comment: { format: "plate", schema_version: 2, content } };
  await assert.rejects(
    executeCommentCommand(
      {
        workspaceID: "w",
        issueID: "i",
        action: "comment.create",
        body: fixtures[0][1],
      },
      "key",
    ),
    /COMMENT_CONTENT_VERSION/,
  );
});
test("actual comment mention adapter completes both active and archived issue cursors and retains existing workspace members/projects without invented active restriction", async () => {
  const choices = await commentService.mentions("workspace");
  assert.deepEqual(plain(choices.map((item) => item.id)), [
    "inactive-user",
    "archived-project",
    "active-issue",
    "archived-issue",
  ]);
  assert.equal(listing.length, 4);
  assert.deepEqual(
    listing.map((item) => item.after).sort(),
    ["second", "second", undefined, undefined].sort(),
  );
  assert(listing.every((item) => item.w === "workspace"));
});
