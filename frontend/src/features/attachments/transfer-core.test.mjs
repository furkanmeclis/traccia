import test from "node:test";
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import {
  digestSource,
  digestBytes,
  encodeChunk,
  decodeChunk,
  MAX_CHUNK_BYTES,
  MAX_FILE_BYTES,
} from "./transfer-core.ts";
const nativeHash = (bytes) => createHash("sha256").update(bytes).digest("hex");
test("incremental SHA256 hashes zero bytes, exact boundaries and complete binary data with bounded reads", async () => {
  for (const size of [
    0,
    1,
    MAX_CHUNK_BYTES,
    MAX_CHUNK_BYTES + 1,
    MAX_CHUNK_BYTES * 3 + 19,
  ]) {
    const bytes = Uint8Array.from({ length: size }, (_, i) => i % 256),
      blob = new Blob([bytes]);
    const reads = [],
      progress = [];
    const source = {
      size,
      slice(start, end) {
        reads.push(end - start);
        return blob.slice(start, end);
      },
    };
    assert.equal(
      await digestSource(source, (n) => progress.push(n)),
      nativeHash(bytes),
    );
    assert(reads.every((n) => n <= MAX_CHUNK_BYTES));
    assert.equal(progress.at(-1), size);
    if (size <= MAX_CHUNK_BYTES)
      assert.equal(await digestBytes(bytes), nativeHash(bytes));
  }
});
test("chunk base64 preserves every byte and rejects noncanonical, excessive or truncated representations", () => {
  const bytes = Uint8Array.from({ length: MAX_CHUNK_BYTES }, (_, i) => i % 256);
  assert.deepEqual(decodeChunk(encodeChunk(bytes)), bytes);
  for (const bad of ["AA=", "A===", "AB==", "AA==\n", "-___"])
    assert.throws(() => decodeChunk(bad));
  assert.throws(() => encodeChunk(new Uint8Array(MAX_CHUNK_BYTES + 1)));
});
test("file size and cancellation checks occur before reading or exposing a complete digest", async () => {
  const unread = () => {
    throw new Error("must not read");
  };
  await assert.rejects(
    digestSource({ size: MAX_FILE_BYTES + 1, slice: unread }),
    /FILE_SIZE_LIMIT/,
  );
  const c = new AbortController();
  c.abort();
  await assert.rejects(
    digestSource({ size: 10, slice: unread }, undefined, c.signal),
    { name: "AbortError" },
  );
  await assert.rejects(
    digestSource({ size: 3, slice: () => new Blob(["x"]) }),
    /FILE_BYTES_CHANGED/,
  );
});
