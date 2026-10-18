"use client";
import { useEffect, useId, useState, useSyncExternalStore } from "react";
import Link from "next/link";
import {
  useInfiniteQuery,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { useAuth } from "@/providers/auth-provider";
import { useLocale } from "@/providers/locale-provider";
import { Section, LoadNotice } from "@/features/issues/issue-ui";
import { useWorkspace } from "@/features/workspaces/queries";
import { workspaceKeys } from "@/features/workspaces/queries";
import { workspaceService } from "@/features/workspaces/workspace.service";
import { relationService } from "@/features/issue-relations/relation.service";
import {
  attachmentService,
  type Attachment,
  type AttachmentCommand,
} from "./attachment.service";
import {
  attachmentKeys,
  useAttachmentConfig,
  useAttachmentList,
  useOwnUploads,
} from "./queries";
import { attachmentError, attachmentActivityKeys } from "./copy";
import { sameID } from "./upload-identity";
import { uploadJobs } from "./job-store";
import type { UploadJob } from "./job-schema";
import {
  useAttachmentRemove,
  AttachmentPendingRemovals,
} from "./remove-operation";
import { decodeChunk, MAX_FILE_BYTES } from "./transfer-core";
import { receiveDownload } from "./download-core";
import { canStreamToDisk, openDownloadSink } from "./download-sink";
const phaseCopy = {
  queued: "attachments.phase_queued",
  hashing: "attachments.phase_hashing",
  ready: "attachments.phase_ready",
  running: "attachments.phase_running",
  paused: "attachments.phase_paused",
  uncertain: "attachments.phase_uncertain",
  error: "attachments.phase_error",
  finalized: "attachments.phase_finalized",
  bound: "attachments.phase_bound",
  cancelled: "attachments.phase_cancelled",
} as const;
const stateCopy: Record<string, string> = {
  local: "attachments.state_local",
  uploading: "attachments.state_uploading",
  finalized: "attachments.state_finalized",
  bound: "attachments.state_bound",
  removed: "attachments.state_removed",
  aborted: "attachments.state_aborted",
  expired: "attachments.state_expired",
};
const cleanupCopy: Record<string, string> = {
  none: "attachments.cleanup_none",
  pending: "attachments.cleanup_pending",
  retrying: "attachments.cleanup_retrying",
  reconciled: "attachments.cleanup_reconciled",
};
function useJobs(workspaceID: string) {
  const { user } = useAuth();
  const jobs = useSyncExternalStore(
    uploadJobs.subscribe,
    uploadJobs.snapshot,
    uploadJobs.serverSnapshot,
  );
  useEffect(() => {
    if (user?.uuid) void uploadJobs.hydrate(user.uuid, workspaceID);
    else uploadJobs.deactivate();
  }, [user?.uuid, workspaceID]);
  return { jobs, actor: user?.uuid };
}
type DownloadConfig = Pick<
  ReturnType<typeof useAttachmentConfig>,
  "data" | "isError" | "isFetching"
>;
function Download({
  config,
  workspaceID,
  file,
  draftID,
  ready,
}: {
  config: DownloadConfig;
  workspaceID: string;
  file: Attachment;
  draftID: string;
  ready: boolean;
}) {
  const { t } = useLocale();
  const [choice, setMode] = useState<"disk" | "memory" | null>(null),
    [progress, setProgress] = useState(0),
    [error, setError] = useState<string | null>(null),
    [verified, setVerified] = useState(false),
    [control, setControl] = useState<AbortController | null>(null);
  const disk = useSyncExternalStore(
    () => () => {},
    () => canStreamToDisk(window),
    () => false,
  );
  const mode = choice ?? (disk ? "disk" : "memory");
  useEffect(() => () => control?.abort(), [control]);
  async function download() {
    setError(null);
    setVerified(false);
    setProgress(0);
    const controller = new AbortController();
    setControl(controller);
    let opened = false;
    try {
      if (
        !ready ||
        !file.capabilities.download ||
        !config.data ||
        config.isError ||
        config.isFetching
      )
        throw new Error("READ_CURRENT_STATE_REQUIRED");
      const chunkBytes = config.data.limits.chunk_bytes;
      if (
        mode === "memory" &&
        file.size_bytes > config.data.limits.blob_fallback_max_bytes
      )
        throw new Error("DOWNLOAD_MEMORY_LIMIT");
      const sink = await openDownloadSink(
        file.download_filename,
        file.size_bytes,
        mode,
      );
      opened = true;
      await receiveDownload({
        size: file.size_bytes,
        sha256: file.sha256,
        fileRevision: file.file_revision,
        sink,
        signal: controller.signal,
        progress: setProgress,
        read: async (offset) => {
          const part = await attachmentService.download(
            workspaceID,
            file,
            draftID,
            offset,
            chunkBytes,
            controller.signal,
          );
          return {
            offset: part.offset,
            nextOffset: part.next_offset,
            eof: part.eof,
            size: part.size_bytes,
            sha256: part.sha256,
            fileRevision: part.file_revision,
            chunkSha256: part.chunk_sha256,
            bytes: decodeChunk(part.data_base64),
          };
        },
      });
      setVerified(true);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "DOWNLOAD_FAILED");
    } finally {
      setControl(null);
      if (!opened) setProgress(0);
    }
  }
  return (
    <div className="space-y-2">
      <label className="block text-sm">
        {t("attachments.downloadMode")}
        <select
          className="ms-2 rounded border p-2"
          value={mode}
          disabled={!!control}
          onChange={(e) =>
            setMode(e.target.value === "disk" ? "disk" : "memory")
          }
        >
          <option value="disk" disabled={!disk}>
            {t("attachments.disk")}
          </option>
          <option value="memory">{t("attachments.memory")}</option>
        </select>
      </label>
      {mode === "memory" && (
        <p className="text-muted-foreground text-sm">
          {t("attachments.memoryHelp", {
            bytes: file.size_bytes,
            max: Math.min(
              config.data?.limits.blob_fallback_max_bytes ?? MAX_FILE_BYTES,
              MAX_FILE_BYTES,
            ),
          })}
        </p>
      )}
      <Button
        type="button"
        disabled={
          !ready ||
          !file.capabilities.download ||
          !!control ||
          config.isFetching ||
          config.isError
        }
        onClick={() => void download()}
      >
        {t("attachments.download")}
      </Button>
      {control && (
        <Button type="button" variant="outline" onClick={() => control.abort()}>
          {t("attachments.cancelDownload")}
        </Button>
      )}
      {control && (
        <progress
          aria-label={t("attachments.downloadProgress")}
          max={Math.max(1, file.size_bytes)}
          value={progress}
        />
      )}
      <p className="font-mono text-xs break-all">SHA-256: {file.sha256}</p>
      {verified && (
        <p role="status">{t("attachments.verified", { bytes: progress })}</p>
      )}
      {error && (
        <p role="alert">
          {t("attachments.failed")}: {attachmentError(error, t)}
        </p>
      )}
    </div>
  );
}
function FileHistory({
  workspaceID,
  file,
  draftID,
  enabled,
}: {
  workspaceID: string;
  file: Attachment;
  draftID: string;
  enabled: boolean;
}) {
  const { t } = useLocale(),
    { user } = useAuth();
  const query = useInfiniteQuery({
    queryKey: [
      ...attachmentKeys.all(workspaceID, user?.uuid),
      "history",
      file.id,
      draftID,
    ],
    enabled: enabled && file.capabilities.history,
    queryFn: ({ pageParam }) =>
      attachmentService.history(workspaceID, file.id, draftID, pageParam),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (p) => (p.has_more ? p.next_cursor : undefined),
  });
  if (!file.capabilities.history) return null;
  return (
    <details>
      <summary>{t("attachments.fileHistory")}</summary>
      <LoadNotice
        pending={query.isPending}
        error={query.isError}
        retry={() => void query.refetch()}
      />
      <ol hidden={!enabled || query.isError}>
        {query.data?.pages
          .flatMap((p) => p.items)
          .map((row) => (
            <li key={row.id} className="py-2">
              <p>
                {t(
                  attachmentActivityKeys[row.event_name] ??
                    "attachments.eventUnknown",
                )}{" "}
                · {row.actor_id} · {row.occurred_at}
              </p>
              <details>
                <summary>{t("attachments.snapshot")}</summary>
                <pre className="max-h-64 overflow-auto text-xs">
                  {JSON.stringify(row.snapshot, null, 2)}
                </pre>
              </details>
            </li>
          ))}
      </ol>
      {query.hasNextPage && (
        <Button
          type="button"
          disabled={query.isFetchingNextPage}
          onClick={() => void query.fetchNextPage()}
        >
          {t("attachments.loadMore")}
        </Button>
      )}
    </details>
  );
}
async function reviewPermission(job: UploadJob) {
  await workspaceService.get(job.workspaceID);
  const config = await attachmentService.config(job.workspaceID);
  if (!config.capabilities.manage) return false;
  const command = job.pending?.command;
  if (!command) return false;
  if (command.action === "upload.create") {
    let after: string | undefined;
    const matches = [];
    do {
      const page = await attachmentService.uploads(
        job.workspaceID,
        after,
        job.draftID,
      );
      matches.push(
        ...page.items.filter(
          (upload) =>
            sameID(upload.draft_id, job.draftID) &&
            sameID(upload.actor_id, job.actorID) &&
            upload.filename === job.filename &&
            upload.size_bytes === job.size &&
            upload.sha256 === job.sha256,
        ),
      );
      after = page.has_more ? page.next_cursor : undefined;
    } while (after);
    if (matches.length > 1) throw new Error("AMBIGUOUS_UPLOAD_STATE");
    if (matches[0]) await uploadJobs.synchronize(job.id, matches[0]);
    if (!job.issueID) return false;
    const list = await attachmentService.list(
      job.workspaceID,
      job.issueID,
      false,
      "",
    );
    return list.capabilities.bind;
  }
  if (
    command.action === "upload.chunk" ||
    command.action === "upload.finalize" ||
    command.action === "upload.abort"
  ) {
    if (!job.uploadID) return false;
    const upload = (
      await attachmentService.upload(job.workspaceID, job.uploadID, job.draftID)
    ).upload;
    await uploadJobs.synchronize(job.id, upload);
    if (
      !sameID(upload.actor_id, job.actorID) ||
      !sameID(upload.draft_id, job.draftID)
    )
      return false;
    if (command.action === "upload.chunk")
      return (
        upload.capabilities.upload_chunk ||
        (upload.state === "uploading" &&
          upload.capabilities.finalize &&
          upload.next_offset >=
            command.body.offset + decodeChunk(command.body.data_base64).length)
      );
    if (command.action === "upload.finalize")
      return (
        upload.capabilities.finalize ||
        (upload.state === "finalized" && upload.capabilities.bind)
      );
    return upload.capabilities.abort || upload.state === "aborted";
  }
  if (!job.attachmentID) return false;
  const file = (
    await attachmentService.get(job.workspaceID, job.attachmentID, job.draftID)
  ).attachment;
  await uploadJobs.synchronizeFile(job.id, file);
  if (command.action === "attachment.bind") {
    const list = await attachmentService.list(
      job.workspaceID,
      command.body.issue_id,
      false,
      "",
    );
    return (
      list.capabilities.bind &&
      (file.capabilities.bind || sameID(file.issue_id, command.body.issue_id))
    );
  }
  if (!file.issue_id) return false;
  const list = await attachmentService.list(
    job.workspaceID,
    file.issue_id,
    true,
    "",
  );
  return list.capabilities.remove && (file.capabilities.remove || file.removed);
}
function UploadJobCard({ job, allowed }: { job: UploadJob; allowed: boolean }) {
  const { t } = useLocale(),
    { user } = useAuth(),
    cache = useQueryClient(),
    id = useId();
  const [issueID, setIssueID] = useState(job.issueID ?? ""),
    [error, setError] = useState<string | null>(null),
    [reviewing, setReviewing] = useState(false);
  const owner = sameID(job.actorID, user?.uuid),
    enabled = allowed && owner;
  const permission = useAttachmentConfig(job.workspaceID, enabled);
  const upload = useQuery({
    queryKey: [
      ...attachmentKeys.all(job.workspaceID, user?.uuid),
      "upload",
      job.uploadID,
      job.draftID,
    ],
    enabled: enabled && !!job.uploadID,
    staleTime: 0,
    queryFn: () =>
      attachmentService.upload(job.workspaceID, job.uploadID!, job.draftID),
  });
  const file = useQuery({
    queryKey: [
      ...attachmentKeys.all(job.workspaceID, user?.uuid),
      "file",
      job.attachmentID,
      job.draftID,
    ],
    enabled: enabled && !!job.attachmentID,
    staleTime: 0,
    queryFn: () =>
      attachmentService.get(job.workspaceID, job.attachmentID!, job.draftID),
  });
  const choices = useQuery({
    queryKey: [...attachmentKeys.all(job.workspaceID, user?.uuid), "targets"],
    enabled:
      enabled &&
      !!job.attachmentID &&
      !job.boundIssueID &&
      file.data?.attachment.issue_id === null,
    queryFn: () => relationService.candidates(job.workspaceID),
  });
  const target = useQuery({
    queryKey: [
      ...attachmentKeys.all(job.workspaceID, user?.uuid),
      "target",
      issueID,
    ],
    enabled: enabled && !!issueID,
    staleTime: 0,
    queryFn: () => attachmentService.list(job.workspaceID, issueID, false, ""),
  });
  const busy = uploadJobs.isRunning(job.id) || reviewing,
    readReady =
      enabled &&
      !upload.isError &&
      !upload.isFetching &&
      !file.isError &&
      !file.isFetching,
    mutable =
      readReady &&
      !busy &&
      !job.pending &&
      permission.isSuccess &&
      !permission.isError &&
      !permission.isFetching &&
      permission.data.capabilities.manage;
  async function action(run: () => Promise<unknown>) {
    setError(null);
    try {
      await run();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "TRANSFER_FAILED");
    } finally {
      await Promise.all([
        cache.invalidateQueries({
          queryKey: attachmentKeys.all(job.workspaceID, user?.uuid),
        }),
        cache.invalidateQueries({
          queryKey: workspaceKeys.activity(job.workspaceID, user?.uuid),
        }),
      ]);
    }
  }
  function command(input: AttachmentCommand) {
    void action(() => uploadJobs.run(job.id, input));
  }
  const up = upload.data?.upload,
    attachment = file.data?.attachment;
  return (
    <article className="space-y-3 rounded border p-3">
      <p className="font-medium break-all">{job.filename}</p>
      <p className="text-sm">
        {job.size} {t("attachments.bytes")} · {t(phaseCopy[job.phase])} ·{" "}
        {t(
          stateCopy[up?.state ?? job.serverState] ??
            "attachments.state_unknown",
        )}
      </p>
      <p className="font-mono text-xs break-all">
        {t("attachments.draft")}: {job.draftID}
        {job.uploadID && (
          <>
            {" "}
            · {t("attachments.uploadId")}: {job.uploadID}
          </>
        )}
      </p>
      {(up?.expires_at ?? job.expiresAt) && (
        <p className="text-sm">
          {t("attachments.expires")}: {up?.expires_at ?? job.expiresAt}
        </p>
      )}
      {up && (
        <p className="text-sm">
          {t("attachments.cleanup")}:{" "}
          {t(cleanupCopy[up.cleanup.state] ?? "attachments.state_unknown")}
          {up.cleanup.next_check_at && <> · {up.cleanup.next_check_at}</>}
        </p>
      )}
      {job.phase === "hashing" ? (
        <progress
          aria-label={t("attachments.hashProgress")}
          max={Math.max(1, job.size)}
          value={job.hashedBytes}
        />
      ) : (
        <progress
          aria-label={t("attachments.uploadProgress")}
          max={Math.max(1, job.size)}
          value={job.offset}
        />
      )}
      <p className="text-sm">
        {job.offset}/{job.size} {t("attachments.bytesConfirmed")}
      </p>
      {job.sha256 && (
        <p className="font-mono text-xs break-all">SHA-256: {job.sha256}</p>
      )}
      <LoadNotice
        pending={false}
        error={upload.isError || file.isError}
        retry={() => {
          void upload.refetch();
          void file.refetch();
        }}
      />
      {job.pending ? (
        <div className="space-y-2 rounded border p-3">
          <p role="alert">
            {t(
              job.pending.outcome === "rejected"
                ? "attachments.rejected"
                : "attachments.uncertain",
            )}
          </p>
          <details>
            <summary>{t("attachments.requestMetadata")}</summary>
            <pre className="max-h-64 overflow-auto text-xs">
              {JSON.stringify(
                job.pending.command,
                (key, value) =>
                  key === "data_base64" && typeof value === "string"
                    ? t("attachments.binaryRetained", { chars: value.length })
                    : value,
                2,
              )}
            </pre>
          </details>
          <p className="font-mono text-xs">
            {job.pending.command.action} · {job.pending.key}
          </p>
          <Button
            type="button"
            disabled={!enabled || busy}
            onClick={() => {
              setReviewing(true);
              void action(() =>
                uploadJobs.review(job.id, reviewPermission),
              ).finally(() => setReviewing(false));
            }}
          >
            {t("attachments.review")}
          </Button>
          <Button
            type="button"
            disabled={
              !readReady ||
              busy ||
              !job.pending.reviewed ||
              !job.pending.canRetry ||
              !job.pending.retryable ||
              !permission.data?.capabilities.manage ||
              permission.isError ||
              permission.isFetching
            }
            onClick={() => void action(() => uploadJobs.retry(job.id))}
          >
            {t("attachments.retrySame")}
          </Button>
          <Button
            type="button"
            variant="outline"
            disabled={!enabled || busy || !job.pending.reviewed}
            onClick={() => void action(() => uploadJobs.discard(job.id))}
          >
            {t("attachments.discard")}
          </Button>
          {job.pending.reviewError && (
            <p role="alert">{t("attachments.reviewFailed")}</p>
          )}
          {job.pending.reviewed && !job.pending.canRetry && (
            <p role="status">{t("attachments.blocked")}</p>
          )}
        </div>
      ) : (
        <div className="flex flex-wrap gap-2">
          {!job.attachmentID && (
            <>
              <label htmlFor={id} className="block text-sm">
                {t("attachments.reselect")}
                <input
                  id={id}
                  type="file"
                  disabled={!enabled || busy}
                  onChange={(event) => {
                    const selected = event.target.files?.[0];
                    if (selected)
                      void action(() => uploadJobs.reselect(job.id, selected));
                    event.target.value = "";
                  }}
                />
              </label>
              <Button
                type="button"
                disabled={
                  !mutable ||
                  !uploadJobs.hasFile(job.id) ||
                  (!up &&
                    (!target.isSuccess ||
                      target.isFetching ||
                      target.isError ||
                      !target.data.capabilities.bind)) ||
                  (up &&
                    !up.capabilities.upload_chunk &&
                    up.next_offset < up.size_bytes) ||
                  (job.offset === job.size && !!job.uploadID)
                }
                onClick={() => void action(() => uploadJobs.upload(job.id))}
              >
                {t("attachments.uploadResume")}
              </Button>
              {up && (
                <Button
                  type="button"
                  disabled={!mutable || !up.capabilities.finalize}
                  onClick={() =>
                    command({
                      workspaceID: job.workspaceID,
                      action: "upload.finalize",
                      uploadID: up.id,
                      body: {
                        draft_id: job.draftID,
                        expected_version: up.version,
                      },
                    })
                  }
                >
                  {t("attachments.finalize")}
                </Button>
              )}
            </>
          )}
          {busy && (
            <Button
              type="button"
              variant="outline"
              onClick={() => uploadJobs.pause(job.id)}
            >
              {t("attachments.pause")}
            </Button>
          )}
          {up?.capabilities.abort && (
            <Button
              type="button"
              variant="outline"
              disabled={!mutable}
              onClick={() =>
                command({
                  workspaceID: job.workspaceID,
                  action: "upload.abort",
                  uploadID: up.id,
                  body: { draft_id: job.draftID, expected_version: up.version },
                })
              }
            >
              {t("attachments.abort")}
            </Button>
          )}
          {attachment && !attachment.issue_id && (
            <div className="space-y-2">
              <label className="block text-sm">
                {t("attachments.bindTarget")}
                <select
                  className="ms-2 rounded border p-2"
                  value={issueID}
                  disabled={!mutable || choices.isFetching || choices.isError}
                  onChange={(event) => setIssueID(event.target.value)}
                >
                  <option value="">{t("attachments.chooseIssue")}</option>
                  {choices.data?.map((issue) => (
                    <option key={issue.id} value={issue.id}>
                      {issue.identifier} · {issue.title}
                    </option>
                  ))}
                </select>
              </label>
              <LoadNotice
                pending={choices.isPending}
                error={choices.isError}
                retry={() => void choices.refetch()}
              />
              <Button
                type="button"
                disabled={
                  !mutable ||
                  !attachment.capabilities.bind ||
                  !issueID ||
                  target.isFetching ||
                  target.isError ||
                  !target.data?.capabilities.bind
                }
                onClick={() =>
                  command({
                    workspaceID: job.workspaceID,
                    action: "attachment.bind",
                    attachmentID: attachment.id,
                    body: {
                      draft_id: job.draftID,
                      issue_id: issueID,
                      expected_version: attachment.version,
                    },
                  })
                }
              >
                {t("attachments.bind")}
              </Button>
              <p className="text-muted-foreground text-sm">
                {t("attachments.bindHelp")}
              </p>
            </div>
          )}
          {!busy && (
            <Button
              type="button"
              variant="outline"
              onClick={() => void action(() => uploadJobs.forget(job.id))}
            >
              {t("attachments.forget")}
            </Button>
          )}
        </div>
      )}
      {(error ?? job.error) && (
        <p role="alert">
          {t("attachments.failed")}:{" "}
          {attachmentError(error ?? job.error ?? "", t)}
        </p>
      )}
      {attachment && (
        <>
          <Download
            workspaceID={job.workspaceID}
            file={attachment}
            config={permission}
            draftID={job.draftID}
            ready={readReady}
          />
          {attachment.issue_id && (
            <Link
              className="underline"
              href={`/w/${job.workspaceID}/issues/${attachment.issue_id}`}
            >
              {t("attachments.viewIssue")}
            </Link>
          )}
          <FileHistory
            workspaceID={job.workspaceID}
            file={attachment}
            draftID={job.draftID}
            enabled={readReady}
          />
        </>
      )}
      {up && (
        <UploadHistory
          workspaceID={job.workspaceID}
          uploadID={up.id}
          draftID={job.draftID}
          enabled={readReady}
        />
      )}
    </article>
  );
}
function UploadHistory({
  workspaceID,
  uploadID,
  draftID,
  enabled,
}: {
  workspaceID: string;
  uploadID: string;
  draftID: string;
  enabled: boolean;
}) {
  const { t } = useLocale(),
    { user } = useAuth();
  const history = useInfiniteQuery({
    queryKey: [
      ...attachmentKeys.all(workspaceID, user?.uuid),
      "upload-history",
      uploadID,
      draftID,
    ],
    enabled,
    queryFn: ({ pageParam }) =>
      attachmentService.uploadHistory(
        workspaceID,
        uploadID,
        draftID,
        pageParam,
      ),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (p) => (p.has_more ? p.next_cursor : undefined),
  });
  return (
    <details>
      <summary>{t("attachments.uploadHistory")}</summary>
      <LoadNotice
        pending={history.isPending}
        error={history.isError}
        retry={() => void history.refetch()}
      />
      <ol hidden={!enabled || history.isError}>
        {history.data?.pages
          .flatMap((p) => p.items)
          .map((row) => (
            <li key={row.id} className="py-2">
              <p>
                {t(
                  attachmentActivityKeys[row.event_name] ??
                    "attachments.eventUnknown",
                )}{" "}
                · {row.actor_id} · {row.occurred_at}
              </p>
              <details>
                <summary>{t("attachments.snapshot")}</summary>
                <pre className="max-h-64 overflow-auto text-xs">
                  {JSON.stringify(row.snapshot, null, 2)}
                </pre>
              </details>
            </li>
          ))}
      </ol>
      {history.hasNextPage && (
        <Button
          type="button"
          disabled={history.isFetchingNextPage}
          onClick={() => void history.fetchNextPage()}
        >
          {t("attachments.loadMore")}
        </Button>
      )}
    </details>
  );
}
export function AttachmentShelf({ workspaceID }: { workspaceID: string }) {
  const { t } = useLocale(),
    { jobs, actor } = useJobs(workspaceID),
    workspace = useWorkspace(workspaceID);
  const allowed = workspace.isSuccess && !workspace.isError,
    uploads = useOwnUploads(workspaceID, allowed);
  const [error, setError] = useState<string | null>(null);
  const scoped = [...jobs.values()].filter(
    (job) => sameID(job.actorID, actor) && sameID(job.workspaceID, workspaceID),
  );
  return (
    <Section title={t("attachments.queue")}>
      <AttachmentPendingRemovals
        workspaceID={workspaceID}
        allowed={allowed && !workspace.isFetching}
      />
      <LoadNotice
        pending={workspace.isPending}
        error={workspace.isError}
        retry={() => void workspace.refetch()}
      />
      {uploadJobs.persistenceError() && (
        <div role="alert">
          <p>{t("attachments.persistenceFailed")}</p>
          <Button
            type="button"
            disabled={!actor}
            onClick={() => {
              if (actor) void uploadJobs.hydrate(actor, workspaceID);
            }}
          >
            {t("attachments.retryStorage")}
          </Button>
        </div>
      )}
      <div hidden={!allowed}>
        {scoped.map((job) => (
          <UploadJobCard
            key={job.id}
            job={job}
            allowed={allowed && !workspace.isFetching}
          />
        ))}
      </div>
      <details>
        <summary>{t("attachments.serverUploads")}</summary>
        <LoadNotice
          pending={uploads.isPending}
          error={uploads.isError}
          retry={() => void uploads.refetch()}
        />
        <ul hidden={!allowed || uploads.isError}>
          {uploads.data?.pages
            .flatMap((p) => p.items)
            .map((upload) => (
              <li key={upload.id} className="space-y-1 py-2">
                <p className="break-all">
                  {upload.filename} ·{" "}
                  {t(stateCopy[upload.state] ?? "attachments.state_unknown")} ·{" "}
                  {upload.next_offset}/{upload.size_bytes}
                </p>
                <p className="font-mono text-xs">
                  {upload.draft_id} · {upload.id}
                </p>
                <Button
                  type="button"
                  disabled={
                    !allowed ||
                    workspace.isFetching ||
                    scoped.some((job) => sameID(job.uploadID, upload.id))
                  }
                  onClick={() => {
                    void uploadJobs
                      .adopt(upload)
                      .catch((cause) =>
                        setError(
                          cause instanceof Error
                            ? cause.message
                            : "TRANSFER_FAILED",
                        ),
                      );
                  }}
                >
                  {t("attachments.resumeServer")}
                </Button>
              </li>
            ))}
        </ul>
        {uploads.hasNextPage && (
          <Button
            type="button"
            disabled={uploads.isFetchingNextPage}
            onClick={() => void uploads.fetchNextPage()}
          >
            {t("attachments.loadMore")}
          </Button>
        )}
      </details>
      {error && <p role="alert">{attachmentError(error, t)}</p>}
    </Section>
  );
}
function BoundAttachment({
  config,
  workspaceID,
  file,
  ready,
  remove,
}: {
  config: DownloadConfig;
  workspaceID: string;
  file: Attachment;
  ready: boolean;
  remove: boolean;
}) {
  const { t } = useLocale();
  const [error, setError] = useState<string | null>(null);
  const operation = useAttachmentRemove(workspaceID, file);
  return (
    <article className="space-y-3 rounded border p-3">
      <p className="font-medium break-all">{file.filename}</p>
      <p>
        {file.size_bytes} {t("attachments.bytes")} · {file.content_type}
      </p>
      <p className="font-mono text-xs break-all">
        {file.id} · {file.file_revision}
      </p>
      {file.removed && <p>{t("attachments.removed")}</p>}
      <Download
        workspaceID={workspaceID}
        file={file}
        config={config}
        draftID=""
        ready={ready}
      />
      {remove && (
        <Button
          type="button"
          variant="outline"
          disabled={!ready || !file.capabilities.remove || operation.locked}
          onClick={() =>
            void operation.submit().catch((cause) => setError(String(cause)))
          }
        >
          {t("attachments.remove")}
        </Button>
      )}
      {error && <p role="alert">{attachmentError(error, t)}</p>}
      <FileHistory
        workspaceID={workspaceID}
        file={file}
        draftID=""
        enabled={ready}
      />
    </article>
  );
}
export function AttachmentPanel({
  workspaceID,
  issueID,
  metadataVersion,
}: {
  workspaceID: string;
  issueID: string;
  metadataVersion: number;
}) {
  const { t } = useLocale(),
    { user } = useAuth(),
    workspace = useWorkspace(workspaceID),
    id = useId();
  useJobs(workspaceID);
  const [removed, setRemoved] = useState(false),
    [search, setSearch] = useState(""),
    [error, setError] = useState<string | null>(null);
  const allowed = workspace.isSuccess && !workspace.isError,
    list = useAttachmentList(
      workspaceID,
      issueID,
      removed,
      search,
      allowed,
      metadataVersion,
    ),
    config = useAttachmentConfig(workspaceID, allowed);
  const ready =
      allowed &&
      !workspace.isFetching &&
      list.isSuccess &&
      !list.isError &&
      !list.isFetching &&
      config.isSuccess &&
      !config.isError &&
      !config.isFetching,
    bind =
      ready &&
      !!config.data.capabilities.manage &&
      list.data.pages.every((page) => page.capabilities.bind),
    remove = ready && list.data.pages.every((page) => page.capabilities.remove);
  async function add(files: File[]) {
    setError(null);
    try {
      if (!bind || !user?.uuid || !config.data)
        throw new Error("READ_CURRENT_STATE_REQUIRED");
      for (const file of files)
        if (file.size > config.data.limits.max_file_bytes)
          throw new Error("FILE_SIZE_LIMIT");
      await uploadJobs.add(user.uuid, workspaceID, issueID, files);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "TRANSFER_FAILED");
    }
  }
  return (
    <>
      <Section title={t("attachments.title")}>
        <LoadNotice
          pending={workspace.isPending || list.isPending || config.isPending}
          error={workspace.isError || list.isError || config.isError}
          retry={() => {
            void workspace.refetch();
            void list.refetch();
            void config.refetch();
          }}
        />
        <p className="text-muted-foreground text-sm">
          {t("attachments.uploadHelp", {
            bytes: config.data?.limits.max_file_bytes ?? MAX_FILE_BYTES,
            chunk: config.data?.limits.chunk_bytes ?? 256 * 1024,
          })}
        </p>
        <div
          className="rounded border border-dashed p-3"
          onDragOver={(event) => event.preventDefault()}
          onDrop={(event) => {
            event.preventDefault();
            if (bind) void add(Array.from(event.dataTransfer.files));
          }}
        >
          <label htmlFor={id}>{t("attachments.pick")}</label>
          <input
            id={id}
            type="file"
            multiple
            disabled={!bind}
            onChange={(event) => {
              void add(Array.from(event.target.files ?? []));
              event.target.value = "";
            }}
          />
          <p className="text-sm">{t("attachments.drop")}</p>
        </div>
        {error && (
          <p role="alert">
            {t("attachments.failed")}: {attachmentError(error, t)}
          </p>
        )}
        <label className="block text-sm">
          {t("attachments.search")}
          <input
            className="ms-2 rounded border p-2"
            type="search"
            value={search}
            onChange={(event) => setSearch(event.target.value)}
          />
        </label>
        <label className="flex items-center gap-2">
          <input
            type="checkbox"
            checked={removed}
            onChange={(event) => setRemoved(event.target.checked)}
          />
          {t("attachments.showRemoved")}
        </label>
        <div hidden={!allowed || list.isError}>
          {list.data?.pages
            .flatMap((page) => page.items)
            .map((file) => (
              <BoundAttachment
                key={file.id}
                workspaceID={workspaceID}
                file={file}
                config={config}
                ready={ready}
                remove={remove}
              />
            ))}
        </div>
        {list.hasNextPage && (
          <Button
            type="button"
            disabled={list.isFetchingNextPage || !allowed || list.isError}
            onClick={() => void list.fetchNextPage()}
          >
            {t("attachments.loadMore")}
          </Button>
        )}
      </Section>
      <AttachmentShelf workspaceID={workspaceID} />
    </>
  );
}
