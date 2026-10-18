import { createSHA256 } from "hash-wasm";
import {
  assertFileSize,
  checkAbort,
  digestBytes,
  MAX_CHUNK_BYTES,
} from "./transfer-core.ts";
export type DownloadFrame = {
  offset: number;
  nextOffset: number;
  eof: boolean;
  size: number;
  sha256: string;
  chunkSha256: string;
  fileRevision: string;
  bytes: Uint8Array;
};
export type DownloadSink = {
  write: (bytes: Uint8Array) => Promise<void>;
  close: () => Promise<void>;
  abort: () => Promise<void>;
};
/** The sink is committed only after all immutable metadata, bytes and digest agree. */
export async function receiveDownload({
  size,
  sha256,
  fileRevision,
  read,
  sink,
  signal,
  progress,
}: {
  size: number;
  sha256: string;
  fileRevision: string;
  read: (offset: number) => Promise<DownloadFrame>;
  sink: DownloadSink;
  signal: AbortSignal;
  progress: (bytes: number) => void;
}) {
  try {
    assertFileSize(size);
    if (
      !/^[a-f0-9]{64}$/.test(sha256) ||
      !/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(
        fileRevision,
      )
    )
      throw new Error("INVALID_DOWNLOAD_METADATA");
    const hash = await createSHA256();
    hash.init();
    let offset = 0;
    do {
      checkAbort(signal);
      const frame = await read(offset);
      checkAbort(signal);
      if (
        frame.offset !== offset ||
        frame.size !== size ||
        frame.sha256 !== sha256 ||
        frame.fileRevision !== fileRevision ||
        frame.bytes.length > MAX_CHUNK_BYTES ||
        frame.nextOffset !== offset + frame.bytes.length ||
        frame.nextOffset > size ||
        frame.eof !== (frame.nextOffset === size) ||
        (!frame.eof && frame.bytes.length === 0) ||
        (await digestBytes(frame.bytes)) !== frame.chunkSha256
      )
        throw new Error("INVALID_DOWNLOAD_CHUNK");
      hash.update(frame.bytes);
      await sink.write(frame.bytes);
      offset = frame.nextOffset;
      progress(offset);
      if (frame.eof) break;
    } while (offset < size);
    if (offset !== size || hash.digest("hex") !== sha256)
      throw new Error("DOWNLOAD_HASH_MISMATCH");
    checkAbort(signal);
    await sink.close();
    return { size: offset, sha256, verified: true as const };
  } catch (error) {
    await sink.abort();
    throw error;
  }
}
