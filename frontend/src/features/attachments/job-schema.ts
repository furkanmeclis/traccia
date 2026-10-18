import { sameID } from "./upload-identity.ts";
import { z } from "zod";
const id = z.string().uuid(),
  version = z.number().int().positive().safe(),
  count = z
    .number()
    .int()
    .min(0)
    .max(256 * 1024 * 1024),
  hash = z.string().regex(/^[a-f0-9]{64}$/);
const bodyVersion = { draft_id: id, expected_version: version };
export const commandSchema = z.discriminatedUnion("action", [
  z
    .object({
      workspaceID: id,
      action: z.literal("upload.create"),
      body: z
        .object({
          draft_id: id,
          filename: z.string(),
          content_type: z.string(),
          size_bytes: count,
          sha256: hash,
        })
        .strict(),
    })
    .strict(),
  z
    .object({
      workspaceID: id,
      action: z.literal("upload.chunk"),
      uploadID: id,
      body: z
        .object({
          ...bodyVersion,
          offset: count,
          total_bytes: count,
          data_base64: z.string().max(Math.ceil((256 * 1024) / 3) * 4),
          chunk_sha256: hash,
        })
        .strict(),
    })
    .strict(),
  z
    .object({
      workspaceID: id,
      action: z.literal("upload.finalize"),
      uploadID: id,
      body: z.object(bodyVersion).strict(),
    })
    .strict(),
  z
    .object({
      workspaceID: id,
      action: z.literal("upload.abort"),
      uploadID: id,
      body: z.object(bodyVersion).strict(),
    })
    .strict(),
  z
    .object({
      workspaceID: id,
      action: z.literal("attachment.bind"),
      attachmentID: id,
      body: z.object({ ...bodyVersion, issue_id: id }).strict(),
    })
    .strict(),
  z
    .object({
      workspaceID: id,
      action: z.literal("attachment.remove"),
      attachmentID: id,
      body: z.object({ expected_version: version }).strict(),
    })
    .strict(),
]);
export const jobSchema = z
  .object({
    id,
    actorID: id,
    workspaceID: id,
    draftID: id,
    issueID: id.nullable(),
    filename: z.string(),
    contentType: z.string(),
    size: count,
    lastModified: z.number().finite().min(0),
    sha256: hash.nullable(),
    uploadID: id.nullable(),
    uploadVersion: version.nullable(),
    offset: count,
    serverState: z.string(),
    expiresAt: z.string().nullable(),
    attachmentID: id.nullable(),
    attachmentVersion: version.nullable(),
    fileRevision: id.nullable(),
    boundIssueID: id.nullable(),
    phase: z.enum([
      "queued",
      "hashing",
      "ready",
      "running",
      "paused",
      "uncertain",
      "error",
      "finalized",
      "bound",
      "cancelled",
    ]),
    hashedBytes: count,
    error: z.string().nullable(),
    pending: z
      .object({
        key: id,
        command: commandSchema,
        reviewed: z.boolean(),
        outcome: z.enum(["uncertain", "rejected"]),
        retryable: z.boolean(),
        canRetry: z.boolean(),
        reviewError: z.boolean(),
      })
      .strict()
      .nullable(),
  })
  .strict();
export type UploadJob = z.infer<typeof jobSchema>;
export function validateJobIdentity(job: UploadJob): UploadJob {
  if (job.pending) {
    if (!sameID(job.pending.command.workspaceID, job.workspaceID))
      throw new Error("UPLOAD_OWNER_MISMATCH");
    const body = job.pending.command.body;
    if ("draft_id" in body && !sameID(body.draft_id, job.draftID))
      throw new Error("UPLOAD_OWNER_MISMATCH");
    const command = job.pending.command;
    if ("uploadID" in command && !sameID(command.uploadID, job.uploadID))
      throw new Error("UPLOAD_IDENTITY_MISMATCH");
    if (
      "attachmentID" in command &&
      !sameID(command.attachmentID, job.attachmentID)
    )
      throw new Error("ATTACHMENT_IDENTITY_MISMATCH");
    if (
      command.action === "upload.create" &&
      (command.body.filename !== job.filename ||
        command.body.content_type !== job.contentType ||
        command.body.size_bytes !== job.size ||
        command.body.sha256 !== job.sha256)
    )
      throw new Error("FILE_IDENTITY_MISMATCH");
    if (
      command.action === "upload.chunk" &&
      command.body.total_bytes !== job.size
    )
      throw new Error("FILE_IDENTITY_MISMATCH");
  }
  return job;
}
export function parseStoredJob(raw: unknown): UploadJob {
  const job = validateJobIdentity(jobSchema.parse(raw));
  if (job.pending) {
    job.pending.reviewed = false;
    job.pending.canRetry = false;
    job.pending.reviewError = false;
    job.phase = "uncertain";
  } else if (["hashing", "running"].includes(job.phase)) job.phase = "paused";
  return job;
}
