import { commandSchema } from "./job-schema";
import type { components } from "@/generated/api";
import { apiClient, unwrap } from "@/lib/api";
type S = components["schemas"];
export type Upload = S["ProductAttachmentUploadsGetOutput"]["upload"];
export type Attachment = S["ProductAttachmentsGetOutput"]["attachment"];
export type Limits = S["ProductAttachmentUploadsConfigOutput"]["limits"];
export type AttachmentCommand = { workspaceID: string } & (
  | { action: "upload.create"; body: S["ProductAttachmentUploadsCreateInput"] }
  | {
      action: "upload.chunk";
      uploadID: string;
      body: S["ProductAttachmentUploadsChunkInput"];
    }
  | {
      action: "upload.finalize";
      uploadID: string;
      body: S["ProductAttachmentUploadsFinalizeInput"];
    }
  | {
      action: "upload.abort";
      uploadID: string;
      body: S["ProductAttachmentUploadsAbortInput"];
    }
  | {
      action: "attachment.bind";
      attachmentID: string;
      body: S["ProductAttachmentsBindInput"];
    }
  | {
      action: "attachment.remove";
      attachmentID: string;
      body: S["ProductAttachmentsRemoveInput"];
    }
);
function completeRows<T>(
  items: readonly (T | null)[] | null,
): NonNullable<T>[] {
  if (!Array.isArray(items)) throw new Error("INVALID_LIST_RESPONSE");
  return items.map((row) => {
    if (row === null || row === undefined)
      throw new Error("INVALID_LIST_RESPONSE");
    return row;
  });
}
export const attachmentService = {
  async config(workspaceID: string) {
    return unwrap<S["ProductAttachmentUploadsConfigOutput"]>(
      await apiClient.GET(
        "/v1/workspaces/{workspaceID}/attachment-uploads/config",
        { params: { path: { workspaceID } } },
      ),
    );
  },
  async upload(workspaceID: string, uploadID: string, draftID: string) {
    return unwrap<S["ProductAttachmentUploadsGetOutput"]>(
      await apiClient.GET(
        "/v1/workspaces/{workspaceID}/attachment-uploads/{uploadID}",
        {
          params: {
            path: { workspaceID, uploadID },
            query: { draft_id: draftID },
          },
        },
      ),
    );
  },
  async uploads(workspaceID: string, after?: string, draftID = "") {
    const result = await unwrap<S["ProductAttachmentUploadsListOutput"]>(
      await apiClient.GET("/v1/workspaces/{workspaceID}/attachment-uploads", {
        params: {
          path: { workspaceID },
          query: { draft_id: draftID, state: "any", limit: 50, after },
        },
      }),
    );
    return { ...result, items: completeRows(result.items) };
  },
  async uploadHistory(
    workspaceID: string,
    uploadID: string,
    draftID: string,
    after?: string,
  ) {
    const result = await unwrap<S["ProductAttachmentUploadsHistoryOutput"]>(
      await apiClient.GET(
        "/v1/workspaces/{workspaceID}/attachment-uploads/{uploadID}/history",
        {
          params: {
            path: { workspaceID, uploadID },
            query: { draft_id: draftID, limit: 50, after },
          },
        },
      ),
    );
    return { ...result, items: completeRows(result.items) };
  },
  async get(workspaceID: string, attachmentID: string, draftID = "") {
    return unwrap<S["ProductAttachmentsGetOutput"]>(
      await apiClient.GET(
        "/v1/workspaces/{workspaceID}/attachments/{attachmentID}",
        {
          params: {
            path: { workspaceID, attachmentID },
            query: { draft_id: draftID },
          },
        },
      ),
    );
  },
  async list(
    workspaceID: string,
    issueID: string,
    removed: boolean,
    search: string,
    after?: string,
  ) {
    const result = await unwrap<S["ProductAttachmentsListOutput"]>(
      await apiClient.GET(
        "/v1/workspaces/{workspaceID}/issues/{issueID}/attachments",
        {
          params: {
            path: { workspaceID, issueID },
            query: { removed, search, limit: 50, after },
          },
        },
      ),
    );
    return { ...result, items: completeRows(result.items) };
  },
  async history(
    workspaceID: string,
    attachmentID: string,
    draftID: string,
    after?: string,
  ) {
    const result = await unwrap<S["ProductAttachmentsHistoryOutput"]>(
      await apiClient.GET(
        "/v1/workspaces/{workspaceID}/attachments/{attachmentID}/history",
        {
          params: {
            path: { workspaceID, attachmentID },
            query: { draft_id: draftID, limit: 50, after },
          },
        },
      ),
    );
    return { ...result, items: completeRows(result.items) };
  },
  async download(
    workspaceID: string,
    file: Attachment,
    draftID: string,
    offset: number,
    limit: number,
    signal: AbortSignal,
  ) {
    if (!Number.isSafeInteger(limit) || limit < 1 || limit > 256 * 1024)
      throw new Error("INVALID_CHUNK_LIMIT");
    const result = await unwrap<S["ProductAttachmentsDownloadChunkOutput"]>(
      await apiClient.GET(
        "/v1/workspaces/{workspaceID}/attachments/{attachmentID}/download-chunk",
        {
          params: {
            path: { workspaceID, attachmentID: file.id },
            query: {
              draft_id: draftID,
              file_revision: file.file_revision,
              offset,
              limit,
            },
          },
          signal,
        },
      ),
    );
    if (
      result.attachment_id !== file.id ||
      result.file_revision !== file.file_revision ||
      result.filename !== file.filename ||
      result.download_filename !== file.download_filename ||
      result.content_type !== "application/octet-stream" ||
      result.next_offset - result.offset > limit
    )
      throw new Error("INVALID_DOWNLOAD_IDENTITY");
    return result;
  },
};
export async function executeAttachmentCommand(
  input: AttachmentCommand,
  key: string,
  signal?: AbortSignal,
) {
  const command = commandSchema.parse(input);
  const workspaceID = command.workspaceID,
    headers = { "Idempotency-Key": key };
  switch (command.action) {
    case "upload.create":
      return unwrap<S["ProductAttachmentUploadsCreateOutput"]>(
        await apiClient.POST(
          "/v1/workspaces/{workspaceID}/attachment-uploads",
          {
            params: { path: { workspaceID }, header: headers },
            body: command.body,
            signal,
          },
        ),
      );
    case "upload.chunk":
      return unwrap<S["ProductAttachmentUploadsChunkOutput"]>(
        await apiClient.POST(
          "/v1/workspaces/{workspaceID}/attachment-uploads/{uploadID}/chunks",
          {
            params: {
              path: { workspaceID, uploadID: command.uploadID },
              header: headers,
            },
            body: command.body,
            signal,
          },
        ),
      );
    case "upload.finalize":
      return unwrap<S["ProductAttachmentUploadsFinalizeOutput"]>(
        await apiClient.POST(
          "/v1/workspaces/{workspaceID}/attachment-uploads/{uploadID}/finalize",
          {
            params: {
              path: { workspaceID, uploadID: command.uploadID },
              header: headers,
            },
            body: command.body,
            signal,
          },
        ),
      );
    case "upload.abort":
      return unwrap<S["ProductAttachmentUploadsAbortOutput"]>(
        await apiClient.POST(
          "/v1/workspaces/{workspaceID}/attachment-uploads/{uploadID}/abort",
          {
            params: {
              path: { workspaceID, uploadID: command.uploadID },
              header: headers,
            },
            body: command.body,
            signal,
          },
        ),
      );
    case "attachment.bind":
      return unwrap<S["ProductAttachmentsBindOutput"]>(
        await apiClient.POST(
          "/v1/workspaces/{workspaceID}/attachments/{attachmentID}/bind",
          {
            params: {
              path: { workspaceID, attachmentID: command.attachmentID },
              header: headers,
            },
            body: command.body,
            signal,
          },
        ),
      );
    case "attachment.remove":
      return unwrap<S["ProductAttachmentsRemoveOutput"]>(
        await apiClient.POST(
          "/v1/workspaces/{workspaceID}/attachments/{attachmentID}/remove",
          {
            params: {
              path: { workspaceID, attachmentID: command.attachmentID },
              header: headers,
            },
            body: command.body,
            signal,
          },
        ),
      );
  }
}
