import test from "node:test";
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { receiveDownload } from "./download-core.ts";
import { MAX_CHUNK_BYTES } from "./transfer-core.ts";
const sha = (b) => createHash("sha256").update(b).digest("hex");
function fixture(size, chunkBytes = MAX_CHUNK_BYTES) {
  const bytes = Uint8Array.from({ length: size }, (_, i) => i % 256),
    writes = [];
  let closed = 0,
    aborted = 0;
  return {
    bytes,
    writes,
    get closed() {
      return closed;
    },
    get aborted() {
      return aborted;
    },
    args: {
      size,
      sha256: sha(bytes),
      fileRevision: "11111111-1111-4111-8111-111111111111",
      signal: new AbortController().signal,
      progress() {},
      async read(offset) {
        const chunk = bytes.slice(offset, offset + chunkBytes);
        return {
          offset,
          nextOffset: offset + chunk.length,
          eof: offset + chunk.length === size,
          size,
          sha256: sha(bytes),
          chunkSha256: sha(chunk),
          fileRevision: "11111111-1111-4111-8111-111111111111",
          bytes: chunk,
        };
      },
      sink: {
        async write(b) {
          writes.push(b);
        },
        async close() {
          closed++;
        },
        async abort() {
          aborted++;
        },
      },
    },
  };
}
test("complete zero/binary/multichunk download verifies size/hash before committing a bounded sink", async () => {
  for (const size of [0, 256, MAX_CHUNK_BYTES, MAX_CHUNK_BYTES * 2 + 1]) {
    const f = fixture(size),
      r = await receiveDownload(f.args);
    assert.equal(r.verified, true);
    assert.equal(f.closed, 1);
    assert.equal(f.aborted, 0);
    assert.deepEqual(Buffer.concat(f.writes), Buffer.from(f.bytes));
    assert(f.writes.every((b) => b.length <= MAX_CHUNK_BYTES));
  }
});
test("gap, wrong hash, changed immutable metadata and false EOF abort without reporting verified", async () => {
  for (const patch of [
    { offset: 1 },
    { sha256: "a".repeat(64) },
    { chunkSha256: "b".repeat(64) },
    { nextOffset: 255 },
    { eof: false },
    { fileRevision: "22222222-2222-4222-8222-222222222222" },
  ]) {
    const f = fixture(256),
      read = f.args.read;
    f.args.read = async (o) => ({ ...(await read(o)), ...patch });
    await assert.rejects(receiveDownload(f.args));
    assert.equal(f.closed, 0);
    assert.equal(f.aborted, 1);
  }
});
test("a correct chunk digest cannot hide a wrong whole-file digest, and cancellation aborts staged writes", async () => {
  const f = fixture(256),
    read = f.args.read;
  f.args.sha256 = "a".repeat(64);
  f.args.read = async (o) => ({ ...(await read(o)), sha256: f.args.sha256 });
  await assert.rejects(receiveDownload(f.args), /DOWNLOAD_HASH_MISMATCH/);
  assert.equal(f.closed, 0);
  assert.equal(f.aborted, 1);
  const g = fixture(256),
    c = new AbortController();
  g.args.signal = c.signal;
  c.abort();
  await assert.rejects(receiveDownload(g.args), { name: "AbortError" });
  assert.equal(g.closed, 0);
  assert.equal(g.aborted, 1);
});

test("invalid immutable download metadata aborts an already opened sink", async () => {
  const f = fixture(1);
  f.args.sha256 = "invalid";
  await assert.rejects(receiveDownload(f.args), /INVALID_DOWNLOAD_METADATA/);
  assert.equal(f.aborted, 1);
  assert.equal(f.closed, 0);
});

test("smaller configured chunks still reconstruct and verify the complete file", async () => {
  const f = fixture(8193, 1024);
  await receiveDownload(f.args);
  assert.equal(f.closed, 1);
  assert.equal(f.writes.length, 9);
  assert(f.writes.every((b) => b.length <= 1024));
  assert.deepEqual(Buffer.concat(f.writes), Buffer.from(f.bytes));
});
