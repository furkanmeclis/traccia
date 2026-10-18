import test from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, relative } from "node:path";
import { discoverProductTests } from "./run-product-tests.mjs";

test("discovery includes nested new product regressions without a workflow allowlist", () => {
  const root=mkdtempSync(join(tmpdir(),"traccia-test-discovery-"));
  try {
    for (const path of ["src/features/new-feature/deep","src/lib/text","src/providers/deep"]) mkdirSync(join(root,path),{recursive:true});
    for (const path of ["src/features/new-feature/deep/z.test.mjs","src/features/new-feature/deep/a.test.mjs","src/lib/text/limits.test.mjs","src/lib/text/helper.mjs","src/providers/deep/auth.test.mjs"]) writeFileSync(join(root,path),"");
    assert.deepEqual(discoverProductTests(root).map(path=>relative(root,path)),["src/features/new-feature/deep/a.test.mjs","src/features/new-feature/deep/z.test.mjs","src/lib/text/limits.test.mjs","src/providers/deep/auth.test.mjs"]);
  } finally {rmSync(root,{recursive:true,force:true});}
});
test("released relation and rich clipboard suites remain in the actual product inventory",()=>{
  const files=discoverProductTests();
  for(const suffix of ["features/issue-relations/operation-state.test.mjs","features/documents/shared/clipboard.test.mjs","features/documents/shared/plate.test.mjs","features/workspaces/operation-submission.test.mjs"]) {
    assert.ok(files.some(path=>path.endsWith(suffix)),suffix);
  }
  assert.equal(new Set(files).size,files.length);
});
