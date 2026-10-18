import { digestSource } from "./transfer-core";
self.onmessage = async (event: MessageEvent<{ file: File }>) => {
  try {
    const sha256 = await digestSource(event.data.file, (bytes) =>
      self.postMessage({ type: "progress", bytes }),
    );
    self.postMessage({ type: "done", sha256 });
  } catch (error) {
    self.postMessage({
      type: "error",
      message: error instanceof Error ? error.message : "HASH_FAILED",
    });
  }
};
