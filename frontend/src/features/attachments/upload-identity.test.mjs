import test from "node:test";
import assert from "node:assert/strict";
import {
  assertUploadOwner,
  uploadScope,
  verifyReselectedFile,
} from "./upload-identity.ts";
import { digestSource } from "./transfer-core.ts";
test("upload identity includes actor, workspace and client draft; sibling drafts never share recovery", () => {
  const owner = { actorID: "a", workspaceID: "w", draftID: "d" };
  assert.doesNotThrow(() => assertUploadOwner(owner, { ...owner }));
  for (const field of Object.keys(owner))
    assert.throws(
      () => assertUploadOwner(owner, { ...owner, [field]: "other" }),
      /UPLOAD_OWNER_MISMATCH/,
    );
  assert.equal(
    uploadScope({ actorID: "AB", workspaceID: "CD", draftID: "EF" }),
    uploadScope({ actorID: "ab", workspaceID: "cd", draftID: "ef" }),
  );
  assert.notEqual(
    uploadScope(owner),
    uploadScope({ ...owner, draftID: "other" }),
  );
});
test("reload reselect verifies complete bytes rather than trusting name, size or last modified", async () => {
  const file = new File(["same"], "file.bin", { lastModified: 1 }),
    sha256 = await digestSource(file);
  assert.equal(
    await verifyReselectedFile(file, { size: 4, sha256 }, digestSource),
    file,
  );
  await assert.rejects(
    verifyReselectedFile(
      new File(["fake"], "file.bin", { lastModified: 1 }),
      { size: 4, sha256 },
      digestSource,
    ),
    /FILE_BYTES_CHANGED/,
  );
  await assert.rejects(
    verifyReselectedFile(
      new File(["longer"], "file.bin"),
      { size: 4, sha256 },
      () => {
        throw new Error("must not hash");
      },
    ),
    /FILE_BYTES_CHANGED/,
  );
});
