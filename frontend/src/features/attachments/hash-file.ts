"use client";
import { assertFileSize, checkAbort } from "./transfer-core";
export function hashFile(
  file: File,
  progress: (bytes: number) => void,
  signal: AbortSignal,
): Promise<string> {
  assertFileSize(file.size);
  checkAbort(signal);
  return new Promise((resolve, reject) => {
    const worker = new Worker(new URL("./hash.worker.ts", import.meta.url));
    const finish = () => {
      signal.removeEventListener("abort", abort);
      worker.terminate();
    };
    const abort = () => {
      finish();
      reject(new DOMException("Aborted", "AbortError"));
    };
    signal.addEventListener("abort", abort, { once: true });
    worker.onerror = () => {
      finish();
      reject(new Error("HASH_FAILED"));
    };
    worker.onmessage = (event: MessageEvent) => {
      const value = event.data;
      if (value.type === "progress") progress(value.bytes);
      else if (value.type === "done") {
        finish();
        resolve(value.sha256);
      } else if (value.type === "error") {
        finish();
        reject(new Error(value.message));
      }
    };
    worker.postMessage({ file });
  });
}
