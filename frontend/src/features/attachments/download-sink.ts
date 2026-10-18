"use client";
import { assertFileSize } from "./transfer-core";
import type { DownloadSink } from "./download-core";
type PickerWindow = Window & {
  showSaveFilePicker: (options: {
    suggestedName: string;
  }) => Promise<FileSystemFileHandle>;
};
export function canStreamToDisk(value: Window): value is PickerWindow {
  return (
    "showSaveFilePicker" in value &&
    typeof value.showSaveFilePicker === "function"
  );
}
export async function openDownloadSink(
  filename: string,
  size: number,
  mode: "disk" | "memory",
): Promise<DownloadSink> {
  assertFileSize(size);
  if (!filename || /[\\/\u0000-\u001f\u007f]/u.test(filename))
    throw new Error("INVALID_DOWNLOAD_FILENAME");
  if (mode === "disk") {
    if (!canStreamToDisk(window)) throw new Error("DISK_STREAM_UNAVAILABLE");
    const handle = await window.showSaveFilePicker({ suggestedName: filename });
    const writer = await handle.createWritable();
    return {
      async write(bytes) {
        await writer.write(new Uint8Array(bytes));
      },
      async close() {
        await writer.close();
      },
      async abort() {
        await writer.abort();
      },
    };
  }
  // Explicit fallback: at most the declared file size (hard maximum256MiB).
  // The UI explains this allocation before the user chooses this mode.
  let chunks: ArrayBuffer[] = [],
    count = 0;
  return {
    async write(bytes) {
      if (count + bytes.length > size)
        throw new Error("DOWNLOAD_SIZE_MISMATCH");
      chunks.push(new Uint8Array(bytes).buffer);
      count += bytes.length;
    },
    async close() {
      if (count !== size) throw new Error("DOWNLOAD_SIZE_MISMATCH");
      const blob = new Blob(chunks, { type: "application/octet-stream" });
      chunks = [];
      const url = URL.createObjectURL(blob);
      try {
        const link = document.createElement("a");
        link.href = url;
        link.download = filename;
        document.body.append(link);
        link.click();
        link.remove();
      } finally {
        window.setTimeout(() => URL.revokeObjectURL(url), 1000);
      }
    },
    async abort() {
      chunks = [];
      count = 0;
    },
  };
}
