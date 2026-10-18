import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import vm from "node:vm";
import ts from "typescript";
import { routes } from "../../config/routes.ts";
const source = ts.createSourceFile(
    "auth.ts",
    readFileSync(new URL("../../auth.ts", import.meta.url), "utf8"),
    ts.ScriptTarget.Latest,
    true,
    ts.ScriptKind.TS,
  ),
  protectedFunction = source.statements.find(
    (node) =>
      ts.isFunctionDeclaration(node) && node.name?.text === "isProtectedPath",
  );
assert(protectedFunction);
const sandbox = {
  routes,
  isGuestAuthPath: (path) => path.startsWith(routes.guest.login),
  result: null,
};
vm.runInNewContext(
  ts.transpileModule(
    protectedFunction.getText(source) + ";result=isProtectedPath;",
    { compilerOptions: { target: ts.ScriptTarget.ES2022 } },
  ).outputText,
  sandbox,
);
test("actual shared authentication path classifier and proxy matcher protect personal home without changing existing product/platform/public boundaries", () => {
  for (const path of [
    "/home",
    "/home/child",
    "/workspaces",
    "/w/abc/home",
    routes.platform.notifications.root,
  ])
    assert(sandbox.result(path));
  for (const path of ["/home-public", "/some-public-page", routes.guest.login])
    assert(!sandbox.result(path));
  const proxy = readFileSync(
    new URL("../../proxy.ts", import.meta.url),
    "utf8",
  );
  assert(proxy.includes('"/home/:path*"'));
  assert.equal(routes.product.home, "/home");
});
