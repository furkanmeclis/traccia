import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import vm from "node:vm";
import ts from "typescript";
const require = createRequire(import.meta.url),
  sandboxModule = { exports: {} },
  effects = [],
  invalidations = [];
const code = ts.transpileModule(
  readFileSync(new URL("./queries.ts", import.meta.url), "utf8"),
  { compilerOptions: { module: ts.ModuleKind.CommonJS } },
).outputText;
let actor = "actor-a";
vm.runInNewContext(code, {
  module: sandboxModule,
  exports: sandboxModule.exports,
  require(name) {
    if (name === "react") return { useEffect: (fn) => effects.push(fn) };
    if (name === "@tanstack/react-query")
      return {
        useInfiniteQuery: (opts) => opts,
        useQuery: (opts) => opts,
        useQueryClient: () => ({
          invalidateQueries: (opts) => invalidations.push(opts),
        }),
      };
    if (name === "@/providers/auth-provider")
      return { useAuth: () => ({ user: { uuid: actor } }) };
    if (name === "./comment.service") return { commentService: {} };
    return require(name);
  },
});
const { useComments, useIssueTimeline } = sandboxModule.exports;
const plain = (value) => JSON.parse(JSON.stringify(value));
test("actual comment query identity survives metadata changes and read failure gates while invalidating same scoped cache; actor/issue/tombstone filters stay isolated", () => {
  const first = useComments("w", "i", true, true, 1),
    second = useComments("w", "i", true, false, 2);
  assert.deepEqual(plain(first.queryKey), plain(second.queryKey));
  assert.equal(second.enabled, false);
  effects.forEach((fn) => fn());
  assert.deepEqual(plain(invalidations), [
    { queryKey: ["comments", "w", "actor-a"] },
    { queryKey: ["comments", "w", "actor-a"] },
  ]);
  assert.notDeepEqual(
    plain(first.queryKey),
    plain(useComments("w", "other", true, true, 1).queryKey),
  );
  assert.notDeepEqual(
    plain(first.queryKey),
    plain(useComments("w", "i", false, true, 1).queryKey),
  );
  actor = "actor-b";
  assert.notDeepEqual(
    plain(first.queryKey),
    plain(useComments("w", "i", true, true, 1).queryKey),
  );
});
test("actual server comments and unified timeline cursor callbacks preserve complete next cursor without client length inference", () => {
  for (const query of [
    useComments("w", "i", true, true, 1),
    useIssueTimeline("w", "i", true),
  ]) {
    assert.equal(
      query.getNextPageParam({
        items: [],
        has_more: true,
        next_cursor: "opaque:cursor",
      }),
      "opaque:cursor",
    );
    assert.equal(
      query.getNextPageParam({
        items: Array(100).fill({}),
        has_more: false,
        next_cursor: "",
      }),
      undefined,
    );
  }
});
