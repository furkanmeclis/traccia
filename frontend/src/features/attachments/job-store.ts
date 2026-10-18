"use client";
import { ReviewContext } from "./review-context";
import { ApiError } from "@/lib/api";
import { attachmentPersistence } from "./persistence";
import {
  commandSchema,
  jobSchema,
  parseStoredJob,
  validateJobIdentity,
  type UploadJob,
} from "./job-schema";
import {
  attachmentService,
  executeAttachmentCommand,
  type Attachment,
  type AttachmentCommand,
  type Upload,
} from "./attachment.service";
import { hashFile } from "./hash-file";
import {
  assertFileSize,
  checkAbort,
  digestBytes,
  encodeChunk,
  MAX_CHUNK_BYTES,
} from "./transfer-core";
import { sameID, verifyReselectedFile } from "./upload-identity";
const listeners = new Set<() => void>(),
  files = new Map<string, File>(),
  controllers = new Map<string, AbortController>(),
  running = new Set<string>();
let jobs: ReadonlyMap<string, UploadJob> = new Map(),
  actor: string | undefined,
  persistError: string | null = null;
const reviewContext = new ReviewContext();
const empty: ReadonlyMap<string, UploadJob> = new Map();
let saves: Promise<unknown> = Promise.resolve();
function publish(job: UploadJob) {
  jobs = new Map(jobs).set(job.id, job);
  listeners.forEach((fn) => fn());
}
async function save(job: UploadJob) {
  const next = validateJobIdentity(jobSchema.parse(structuredClone(job)));
  const previous = jobs.get(job.id);
  const token = reviewContext.token();
  const write = saves
    .catch(() => {})
    .then(() => attachmentPersistence.put(next));
  saves = write;
  try {
    await write;
    persistError = null;
    publish(
      !reviewContext.current(token) && next.pending
        ? {
            ...next,
            pending: { ...next.pending, reviewed: false, canRetry: false },
          }
        : next,
    );
  } catch {
    persistError = "LOCAL_PERSISTENCE_FAILED";
    const retained = previous ?? next;
    publish({
      ...retained,
      phase: retained.pending ? "uncertain" : "error",
      error: persistError,
      pending: retained.pending
        ? {
            ...retained.pending,
            reviewed: false,
            canRetry: false,
            reviewError: false,
          }
        : null,
    });
    throw new Error(persistError);
  }
}
async function patch(id: string, update: Partial<UploadJob>) {
  const job = jobs.get(id);
  if (!job) throw new Error("UPLOAD_JOB_MISSING");
  await save({ ...job, ...update });
}
function errorText(error: unknown) {
  return error instanceof Error
    ? error.message.slice(0, 2000)
    : "TRANSFER_FAILED";
}
function owned(id: string) {
  const job = jobs.get(id);
  if (!job || !sameID(job.actorID, actor))
    throw new Error("UPLOAD_OWNER_MISMATCH");
  return job;
}
function uploadPatch(job: UploadJob, upload: Upload): Partial<UploadJob> {
  if (
    !sameID(upload.workspace_id, job.workspaceID) ||
    !sameID(upload.actor_id, job.actorID) ||
    !sameID(upload.draft_id, job.draftID) ||
    upload.filename !== job.filename ||
    upload.size_bytes !== job.size ||
    upload.sha256 !== job.sha256 ||
    upload.content_type !== job.contentType ||
    (job.uploadID !== null && !sameID(upload.id, job.uploadID)) ||
    upload.next_offset < 0 ||
    upload.next_offset > job.size ||
    !Number.isSafeInteger(upload.chunk_bytes) ||
    upload.chunk_bytes < 1 ||
    upload.chunk_bytes > MAX_CHUNK_BYTES ||
    !Number.isSafeInteger(upload.version) ||
    !Number.isSafeInteger(upload.next_offset)
  )
    throw new Error("INVALID_UPLOAD_SNAPSHOT");
  return {
    ...(["aborted", "expired"].includes(upload.state)
      ? { phase: "cancelled" as const }
      : {}),
    uploadID: upload.id,
    uploadVersion: upload.version,
    offset: upload.next_offset,
    serverState: upload.state,
    expiresAt: upload.expires_at,
    attachmentID: upload.attachment_id,
  };
}
function filePatch(job: UploadJob, file: Attachment): Partial<UploadJob> {
  if (
    !sameID(file.workspace_id, job.workspaceID) ||
    !sameID(file.actor_id, job.actorID) ||
    !sameID(file.draft_id, job.draftID) ||
    file.size_bytes !== job.size ||
    file.sha256 !== job.sha256 ||
    file.filename !== job.filename ||
    file.content_type !== job.contentType ||
    (job.uploadID !== null && !sameID(file.upload_id, job.uploadID)) ||
    (job.fileRevision !== null &&
      !sameID(file.file_revision, job.fileRevision)) ||
    !Number.isSafeInteger(file.version)
  )
    throw new Error("INVALID_ATTACHMENT_SNAPSHOT");
  return {
    attachmentID: file.id,
    attachmentVersion: file.version,
    fileRevision: file.file_revision,
    boundIssueID: file.issue_id,
    expiresAt: file.expires_at,
    serverState: file.state,
    phase:
      file.state === "finalized"
        ? "finalized"
        : file.issue_id
          ? "bound"
          : "cancelled",
  };
}
async function execute(
  id: string,
  command: AttachmentCommand,
  signal: AbortSignal,
) {
  const job = owned(id);
  if (job.pending) throw new Error("REVIEW_REQUIRED");
  const checked = commandSchema.parse(command);
  await patch(id, {
    pending: {
      key: crypto.randomUUID(),
      outcome: "uncertain",
      retryable: true,
      command: checked,
      reviewed: false,
      canRetry: false,
      reviewError: false,
    },
    phase: "running",
    error: null,
  });
  return replay(id, signal, false);
}
async function replay(id: string, signal: AbortSignal, reviewRequired = true) {
  const job = owned(id),
    pending = job.pending;
  if (
    !pending ||
    (reviewRequired &&
      (!pending.reviewed || !pending.canRetry || !pending.retryable))
  )
    throw new Error("REVIEW_REQUIRED");
  try {
    checkAbort(signal);
    const result = await executeAttachmentCommand(
      pending.command,
      pending.key,
      signal,
    );
    const latest = owned(id);
    let update: Partial<UploadJob> = {
      pending: null,
      error: null,
      phase: "ready",
    };
    if ("upload" in result)
      update = { ...update, ...uploadPatch(latest, result.upload) };
    if ("attachment" in result)
      update = { ...update, ...filePatch(latest, result.attachment) };
    await patch(id, update);
  } catch (error) {
    const latest = owned(id);
    if (latest.pending)
      await patch(id, {
        phase: "uncertain",
        error: errorText(error),
        pending: {
          ...latest.pending,
          outcome:
            error instanceof ApiError &&
            error.status < 500 &&
            error.status !== 408
              ? "rejected"
              : "uncertain",
          retryable: !(
            error instanceof ApiError && [400, 409].includes(error.status)
          ),
          reviewed: false,
          canRetry: false,
          reviewError: false,
        },
      });
    throw error;
  }
}
async function locked(id: string, run: (signal: AbortSignal) => Promise<void>) {
  if (running.has(id)) return;
  owned(id);
  running.add(id);
  const controller = new AbortController();
  controllers.set(id, controller);
  try {
    await run(controller.signal);
  } catch (error) {
    const job = jobs.get(id);
    if (job && sameID(job.actorID, actor) && !job.pending)
      await patch(id, {
        phase: controller.signal.aborted ? "paused" : "error",
        error: controller.signal.aborted ? null : errorText(error),
      }).catch(() => {});
  } finally {
    running.delete(id);
    controllers.delete(id);
    jobs = new Map(jobs);
    listeners.forEach((fn) => fn());
  }
}
export const uploadJobs = {
  subscribe(fn: () => void) {
    listeners.add(fn);
    return () => {
      listeners.delete(fn);
    };
  },
  snapshot: () => jobs,
  serverSnapshot: () => empty,
  persistenceError: () => persistError,
  isRunning: (id: string) => running.has(id),
  hasFile: (id: string) => files.has(id),
  async hydrate(actorID: string, workspaceID: string) {
    if (reviewContext.change(actorID, workspaceID)) {
      for (const job of jobs.values())
        if (job.pending)
          publish({
            ...job,
            pending: { ...job.pending, reviewed: false, canRetry: false },
          });
    }
    if (!sameID(actor, actorID)) {
      controllers.forEach((c) => c.abort());
      files.clear();
      actor = actorID;
      jobs = new Map();
      listeners.forEach((fn) => fn());
    }
    try {
      const stored = await attachmentPersistence.read();
      if (!sameID(actor, actorID)) return;
      for (const raw of stored) {
        if (
          typeof raw !== "object" ||
          !raw ||
          !("actorID" in raw) ||
          typeof raw.actorID !== "string" ||
          !sameID(raw.actorID, actorID)
        )
          continue;
        const parsed = parseStoredJob(raw);
        if (!jobs.has(parsed.id)) publish(parsed);
      }
      persistError = null;
      jobs = new Map(jobs);
      listeners.forEach((fn) => fn());
    } catch {
      persistError = "LOCAL_PERSISTENCE_FAILED";
      jobs = new Map(jobs);
      listeners.forEach((fn) => fn());
    }
  },
  deactivate() {
    reviewContext.change();
    controllers.forEach((c) => c.abort());
    files.clear();
    actor = undefined;
    jobs = new Map();
    listeners.forEach((fn) => fn());
  },
  async add(
    actorID: string,
    workspaceID: string,
    issueID: string | null,
    selected: File[],
  ) {
    if (!sameID(actor, actorID)) throw new Error("UPLOAD_OWNER_MISMATCH");
    for (const file of selected) {
      assertFileSize(file.size);
      const job: UploadJob = {
        id: crypto.randomUUID(),
        actorID,
        workspaceID,
        draftID: crypto.randomUUID(),
        issueID,
        filename: file.name,
        contentType: file.type || "application/octet-stream",
        size: file.size,
        lastModified: file.lastModified,
        sha256: null,
        uploadID: null,
        uploadVersion: null,
        offset: 0,
        serverState: "local",
        expiresAt: null,
        attachmentID: null,
        attachmentVersion: null,
        fileRevision: null,
        boundIssueID: null,
        phase: "queued",
        hashedBytes: 0,
        error: null,
        pending: null,
      };
      files.set(job.id, file);
      await save(job);
    }
  },
  async reselect(id: string, file: File) {
    await locked(id, async (signal) => {
      const job = owned(id);
      if (job.sha256)
        await verifyReselectedFile(
          file,
          { size: job.size, sha256: job.sha256 },
          (f) =>
            hashFile(
              f,
              (bytes) => publish({ ...owned(id), hashedBytes: bytes }),
              signal,
            ),
        );
      else if (file.size !== job.size) throw new Error("FILE_BYTES_CHANGED");
      files.set(id, file);
      await patch(id, {
        phase: job.attachmentID ? "finalized" : "ready",
        error: null,
      });
    });
  },
  pause(id: string) {
    owned(id);
    controllers.get(id)?.abort();
  },
  async upload(id: string) {
    await locked(id, async (signal) => {
      let job = owned(id);
      if (job.pending) throw new Error("REVIEW_REQUIRED");
      const file = files.get(id);
      if (!file) throw new Error("RESELECT_FILE_REQUIRED");
      await patch(id, { phase: "hashing", error: null });
      const sha256 = await hashFile(
        file,
        (bytes) => publish({ ...owned(id), hashedBytes: bytes }),
        signal,
      );
      job = owned(id);
      if (job.sha256 && job.sha256 !== sha256)
        throw new Error("FILE_BYTES_CHANGED");
      await patch(id, { sha256, phase: "ready" });
      job = owned(id);
      if (!job.uploadID)
        await execute(
          id,
          {
            workspaceID: job.workspaceID,
            action: "upload.create",
            body: {
              draft_id: job.draftID,
              filename: job.filename,
              content_type: job.contentType,
              size_bytes: job.size,
              sha256,
            },
          },
          signal,
        );
      job = owned(id);
      if (!job.uploadID) throw new Error("UPLOAD_JOB_MISSING");
      const refreshed = (
        await attachmentService.upload(
          job.workspaceID,
          job.uploadID,
          job.draftID,
        )
      ).upload;
      await patch(id, uploadPatch(job, refreshed));
      while ((job = owned(id)).offset < job.size) {
        checkAbort(signal);
        if (job.pending || !job.uploadID || job.uploadVersion === null)
          throw new Error("REVIEW_REQUIRED");
        const chunkSize = refreshed.chunk_bytes;
        if (
          !Number.isSafeInteger(chunkSize) ||
          chunkSize < 1 ||
          chunkSize > MAX_CHUNK_BYTES
        )
          throw new Error("INVALID_CHUNK_LIMIT");
        const bytes = new Uint8Array(
          await file
            .slice(job.offset, Math.min(job.size, job.offset + chunkSize))
            .arrayBuffer(),
        );
        if (bytes.length !== Math.min(chunkSize, job.size - job.offset))
          throw new Error("FILE_BYTES_CHANGED");
        await execute(
          id,
          {
            workspaceID: job.workspaceID,
            action: "upload.chunk",
            uploadID: job.uploadID,
            body: {
              draft_id: job.draftID,
              expected_version: job.uploadVersion,
              offset: job.offset,
              total_bytes: job.size,
              data_base64: encodeChunk(bytes),
              chunk_sha256: await digestBytes(bytes),
            },
          },
          signal,
        );
      }
      await patch(id, { phase: "ready" });
    });
  },
  async run(id: string, command: AttachmentCommand) {
    await locked(id, (signal) => execute(id, command, signal));
  },
  async retry(id: string) {
    await locked(id, (signal) => replay(id, signal));
  },
  async review(id: string, canRetry: (job: UploadJob) => Promise<boolean>) {
    const job = owned(id);
    const token = reviewContext.token();
    if (!job.pending || running.has(id)) return false;
    await patch(id, {
      pending: {
        ...job.pending,
        reviewed: false,
        canRetry: false,
        reviewError: false,
      },
    });
    try {
      if (!reviewContext.current(token)) return false;
      const allowed = await canRetry(owned(id));
      if (!reviewContext.current(token)) return false;
      const current = owned(id);
      if (current.pending)
        await patch(id, {
          pending: {
            ...current.pending,
            reviewed: true,
            canRetry: allowed,
            reviewError: false,
          },
        });
      return allowed;
    } catch (error) {
      if (!reviewContext.current(token)) return false;
      const current = owned(id);
      if (current.pending)
        await patch(id, {
          error: errorText(error),
          pending: {
            ...current.pending,
            reviewed: false,
            canRetry: false,
            reviewError: true,
          },
        });
      return false;
    }
  },
  async synchronize(id: string, upload: Upload) {
    const job = owned(id);
    await patch(id, uploadPatch(job, upload));
  },
  async synchronizeFile(id: string, file: Attachment) {
    const job = owned(id);
    await patch(id, filePatch(job, file));
  },
  async discard(id: string) {
    const job = owned(id);
    if (!job.pending?.reviewed) throw new Error("REVIEW_REQUIRED");
    await patch(id, { pending: null, phase: "paused", error: null });
  },
  async forget(id: string) {
    const job = owned(id);
    if (job.pending || running.has(id)) throw new Error("REVIEW_REQUIRED");
    await attachmentPersistence.remove(id);
    files.delete(id);
    const next = new Map(jobs);
    next.delete(id);
    jobs = next;
    listeners.forEach((fn) => fn());
  },
  async adopt(upload: Upload) {
    if (!sameID(upload.actor_id, actor))
      throw new Error("UPLOAD_OWNER_MISMATCH");
    if ([...jobs.values()].some((j) => sameID(j.uploadID, upload.id))) return;
    await save({
      id: crypto.randomUUID(),
      actorID: upload.actor_id,
      workspaceID: upload.workspace_id,
      draftID: upload.draft_id,
      issueID: null,
      filename: upload.filename,
      contentType: upload.content_type,
      size: upload.size_bytes,
      lastModified: 0,
      sha256: upload.sha256,
      uploadID: upload.id,
      uploadVersion: upload.version,
      offset: upload.next_offset,
      serverState: upload.state,
      expiresAt: upload.expires_at,
      attachmentID: upload.attachment_id,
      attachmentVersion: null,
      fileRevision: null,
      boundIssueID: null,
      phase: upload.attachment_id ? "finalized" : "paused",
      hashedBytes: 0,
      error: null,
      pending: null,
    });
  },
};
