import { assertFileSize } from "./transfer-core.ts";
export type UploadOwner = {
  actorID: string;
  workspaceID: string;
  draftID: string;
};
export function sameID(
  a: string | null | undefined,
  b: string | null | undefined,
) {
  return a == null || b == null ? a === b : a.toLowerCase() === b.toLowerCase();
}
export function uploadScope(owner: UploadOwner) {
  return JSON.stringify([
    owner.actorID.toLowerCase(),
    owner.workspaceID.toLowerCase(),
    owner.draftID.toLowerCase(),
  ]);
}
export function assertUploadOwner(expected: UploadOwner, actual: UploadOwner) {
  if (uploadScope(expected) !== uploadScope(actual))
    throw new Error("UPLOAD_OWNER_MISMATCH");
}
export async function verifyReselectedFile(
  file: File,
  expected: { size: number; sha256: string },
  digest: (file: File) => Promise<string>,
) {
  assertFileSize(file.size);
  if (file.size !== expected.size) throw new Error("FILE_BYTES_CHANGED");
  if ((await digest(file)) !== expected.sha256)
    throw new Error("FILE_BYTES_CHANGED");
  return file;
}
