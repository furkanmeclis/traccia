import { createSHA256 } from "hash-wasm";
export const MAX_FILE_BYTES = 256 * 1024 * 1024;
export const MAX_CHUNK_BYTES = 256 * 1024;
export type ByteSource = Pick<Blob, "size" | "slice">;
export function assertFileSize(size: number) {
  if (!Number.isSafeInteger(size) || size < 0 || size > MAX_FILE_BYTES)
    throw new Error("FILE_SIZE_LIMIT");
}
export function checkAbort(signal?: AbortSignal) {
  if (signal?.aborted) throw new DOMException("Aborted", "AbortError");
}
/** No read or hash buffer exceeds one transfer chunk, including whole-file hashing. */
export async function digestSource(
  source: ByteSource,
  progress?: (bytes: number) => void,
  signal?: AbortSignal,
) {
  assertFileSize(source.size);
  checkAbort(signal);
  const hash = await createSHA256();
  hash.init();
  for (let offset = 0; offset < source.size; offset += MAX_CHUNK_BYTES) {
    checkAbort(signal);
    const end = Math.min(source.size, offset + MAX_CHUNK_BYTES);
    const bytes = new Uint8Array(await source.slice(offset, end).arrayBuffer());
    if (bytes.length !== end - offset) throw new Error("FILE_BYTES_CHANGED");
    checkAbort(signal);
    hash.update(bytes);
    progress?.(end);
  }
  checkAbort(signal);
  progress?.(source.size);
  return hash.digest("hex");
}
export async function digestBytes(bytes: Uint8Array) {
  if (bytes.length > MAX_CHUNK_BYTES) throw new Error("CHUNK_SIZE_LIMIT");
  const hash = await createSHA256();
  hash.init();
  hash.update(bytes);
  return hash.digest("hex");
}
export function encodeChunk(bytes: Uint8Array) {
  if (bytes.length > MAX_CHUNK_BYTES) throw new Error("CHUNK_SIZE_LIMIT");
  let binary = "";
  for (let i = 0; i < bytes.length; i += 8192)
    binary += String.fromCharCode(...bytes.subarray(i, i + 8192));
  return btoa(binary);
}
export function decodeChunk(raw: string) {
  if (
    raw.length > Math.ceil(MAX_CHUNK_BYTES / 3) * 4 ||
    !/^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/.test(
      raw,
    )
  )
    throw new Error("INVALID_CHUNK_BASE64");
  const bytes = Uint8Array.from(atob(raw), (c) => c.charCodeAt(0));
  if (bytes.length > MAX_CHUNK_BYTES || encodeChunk(bytes) !== raw)
    throw new Error("INVALID_CHUNK_BASE64");
  return bytes;
}
