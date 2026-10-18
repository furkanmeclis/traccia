export const attachmentActivityKeys: Record<string, string> = {
  "attachment_uploads.created": "attachments.eventUploadCreated",
  "attachment_uploads.chunk_uploaded": "attachments.eventChunkUploaded",
  "attachment_uploads.finalized": "attachments.eventUploadFinalized",
  "attachment_uploads.aborted": "attachments.eventUploadAborted",
  "attachment_uploads.bound": "attachments.eventUploadBound",
  "attachment_uploads.removed": "attachments.eventUploadRemoved",
  "attachment_uploads.expired": "attachments.eventUploadExpired",
  "attachments.finalized": "attachments.eventFileFinalized",
  "attachments.bound": "attachments.eventFileBound",
  "attachments.removed": "attachments.eventFileRemoved",
};

const errorKeys: Record<string, string> = {
  FILE_SIZE_LIMIT: "size",
  CHUNK_SIZE_LIMIT: "chunk",
  INVALID_CHUNK_LIMIT: "chunk",
  FILE_BYTES_CHANGED: "fileChanged",
  RESELECT_FILE_REQUIRED: "reselect",
  INVALID_CHUNK_BASE64: "integrity",
  INVALID_DOWNLOAD_METADATA: "integrity",
  INVALID_DOWNLOAD_CHUNK: "integrity",
  DOWNLOAD_HASH_MISMATCH: "integrity",
  DOWNLOAD_SIZE_MISMATCH: "integrity",
  INVALID_DOWNLOAD_IDENTITY: "integrity",
  DISK_STREAM_UNAVAILABLE: "disk",
  INVALID_DOWNLOAD_FILENAME: "filename",
  DOWNLOAD_MEMORY_LIMIT: "memory",
  HASH_FAILED: "hash",
  REVIEW_REQUIRED: "review",
  READ_CURRENT_STATE_REQUIRED: "review",
  AMBIGUOUS_UPLOAD_STATE: "review",
  LOCAL_PERSISTENCE_FAILED: "storage",
  UPLOAD_OWNER_MISMATCH: "identity",
  UPLOAD_IDENTITY_MISMATCH: "identity",
  ATTACHMENT_IDENTITY_MISMATCH: "identity",
  FILE_IDENTITY_MISMATCH: "identity",
  INVALID_UPLOAD_SNAPSHOT: "identity",
  INVALID_ATTACHMENT_SNAPSHOT: "identity",
  UPLOAD_JOB_MISSING: "identity",
  INVALID_LIST_RESPONSE: "response",
  TRANSFER_FAILED: "transfer",
  DOWNLOAD_FAILED: "transfer",
};
export function attachmentError(
  message: string,
  translate: (key: string) => string,
): string {
  const key = errorKeys[message.replace(/^Error:\s*/, "")];
  return key ? translate(`attachments.error_${key}`) : message;
}
