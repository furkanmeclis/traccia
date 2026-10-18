# Issue creation and detail editing

T15 is under implementation and verification. It uses the existing issue, document, comment, relation and attachment operations without adding API or MCP contracts.

Quick creation reuses the complete issue field pickers and shared Plate editor. A plain issue uses the existing create operation. An issue with a description uses the existing atomic rich-create operation. Files are added after creation and bound separately to the committed issue UUID; creation does not promise an atomic file submission.

Local quick drafts are keyed by actor and workspace, with a distinct draft UUID. Closing the dialog keeps the draft. A captured save locks editing until its outcome is resolved. Recovery retains the exact command and idempotency key, requires a current access review, and retries that captured request. An unknown outcome cannot be discarded as though it were an ordinary rejected draft. A response from an old actor, workspace or dialog generation cannot open another issue or clear another draft.

The drawer keeps the selected issue UUID independent of list order. Active editing preserves its original metadata version rather than rebasing onto a live update. Feature-local drafts or explicit keep/discard boundaries allow inactive panels to unmount without retaining hidden editor trees. Access failures hide server-derived content while preserving local unsaved drafts.

Keyboard shortcuts respect text editing, composition and nested dialogs. Focus returns to a valid scoped opener unless another navigation or dialog has taken focus. A created issue is opened only after the server returns its stable UUID.
