type ClipboardEvent = {
  clipboardData: DataTransfer;
  preventDefault(): void;
};
/** Plate's composed handlers require an explicit handled result. */
export function documentClipboardHandlers({
  copy,
  cut,
  paste,
}: {
  copy: (data: DataTransfer) => boolean;
  cut: () => void;
  paste: (data: DataTransfer) => void;
}) {
  return {
    onCopy(event: ClipboardEvent) {
      event.preventDefault();
      copy(event.clipboardData);
      return true;
    },
    onCut(event: ClipboardEvent) {
      // An unsuccessful copy must never fall through to native deletion.
      event.preventDefault();
      if (copy(event.clipboardData)) cut();
      return true;
    },
    onPaste(event: ClipboardEvent) {
      event.preventDefault();
      paste(event.clipboardData);
      return true;
    },
  };
}
