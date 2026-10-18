"use client";
import { useEffect, useState, useSyncExternalStore } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { ReviewContext } from "./review-context";
import { ApiError } from "@/lib/api";
import {
  entrySchema,
  removalFailure,
  mayRetryRemoval,
  type RemovalEntry as Entry,
} from "./remove-schema";
import { Button } from "@/components/ui/button";
import { useAuth } from "@/providers/auth-provider";
import { useLocale } from "@/providers/locale-provider";
import { workspaceKeys } from "@/features/workspaces/queries";
import { workspaceService } from "@/features/workspaces/workspace.service";
import { Section } from "@/features/issues/issue-ui";
import {
  attachmentService,
  executeAttachmentCommand,
  type Attachment,
} from "./attachment.service";
import { attachmentKeys } from "./queries";
import { attachmentRemovalPersistence } from "./persistence";
import { attachmentError } from "./copy";
import { sameID } from "./upload-identity";
const listeners = new Set<() => void>();
const creating = new Set<string>(),
  sending = new Set<string>(),
  controls = new Map<string, AbortController>();
let activeActor: string | undefined;
const reviewContext = new ReviewContext();
function activateActor(next: string | undefined, workspaceID: string) {
  if (reviewContext.change(next, workspaceID)) {
    controls.forEach((c) => c.abort());
    activeActor = next;
    for (const [k, entry] of entries)
      set(k, { ...entry, reviewed: false, reviewing: false, canRetry: false });
  }
}
let entries: ReadonlyMap<string, Entry> = new Map();
const empty: ReadonlyMap<string, Entry> = new Map();
const subscribe = (fn: () => void) => {
  listeners.add(fn);
  return () => {
    listeners.delete(fn);
  };
};
const key = (actorID: string, workspaceID: string, attachmentID: string) =>
  JSON.stringify([
    actorID.toLowerCase(),
    workspaceID.toLowerCase(),
    attachmentID.toLowerCase(),
  ]);
function set(k: string, value: Entry | null) {
  const next = new Map(entries);
  if (value) next.set(k, value);
  else next.delete(k);
  entries = next;
  listeners.forEach((fn) => fn());
}
async function commit(k: string, value: Entry | null) {
  const previous = entries.get(k);
  const token = reviewContext.token();
  try {
    if (value) {
      const checked = entrySchema.parse(value);
      if (
        checked.id !==
        key(
          checked.actorID,
          checked.command.workspaceID,
          checked.command.attachmentID,
        )
      )
        throw new Error("ATTACHMENT_IDENTITY_MISMATCH");
      await attachmentRemovalPersistence.put(checked);
    } else await attachmentRemovalPersistence.remove(k);
    set(
      k,
      value && !reviewContext.current(token)
        ? { ...value, reviewed: false, reviewing: false, canRetry: false }
        : value,
    );
  } catch (error) {
    const retained = previous ?? value;
    if (retained)
      set(k, {
        ...retained,
        pending: false,
        reviewed: false,
        reviewing: false,
        canRetry: false,
        error: "LOCAL_PERSISTENCE_FAILED",
      });
    throw error;
  }
}
async function hydrate(actorID: string) {
  for (const raw of await attachmentRemovalPersistence.read()) {
    if (
      typeof raw !== "object" ||
      !raw ||
      !("actorID" in raw) ||
      typeof raw.actorID !== "string" ||
      !sameID(raw.actorID, actorID)
    )
      continue;
    const parsed = entrySchema.parse(raw);
    if (
      parsed.id !==
      key(
        parsed.actorID,
        parsed.command.workspaceID,
        parsed.command.attachmentID,
      )
    )
      throw new Error("ATTACHMENT_IDENTITY_MISMATCH");
    if (!entries.has(parsed.id))
      set(parsed.id, {
        ...parsed,
        pending: false,
        reviewed: false,
        reviewing: false,
        canRetry: false,
      });
  }
}
async function send(k: string) {
  const entry = entries.get(k);
  if (!entry || entry.pending || sending.has(k)) return;
  if (!entry.retryable || !sameID(entry.actorID, activeActor))
    throw new Error("UPLOAD_OWNER_MISMATCH");
  sending.add(k);
  const controller = new AbortController();
  controls.set(k, controller);
  try {
    await commit(k, {
      ...entry,
      pending: true,
      reviewed: false,
      canRetry: false,
    });
    if (!sameID(entry.actorID, activeActor))
      throw new Error("UPLOAD_OWNER_MISMATCH");
    await executeAttachmentCommand(entry.command, entry.key, controller.signal);
    await commit(k, null);
  } catch (error) {
    await commit(k, {
      ...entry,
      pending: false,
      reviewed: false,
      canRetry: false,
      ...removalFailure(error instanceof ApiError ? error.status : undefined),
      error: error instanceof Error ? error.message : "TRANSFER_FAILED",
    });
  } finally {
    sending.delete(k);
    controls.delete(k);
  }
}
async function invalidate(
  cache: ReturnType<typeof useQueryClient>,
  workspaceID: string,
  actorID?: string,
) {
  await Promise.all([
    cache.invalidateQueries({
      queryKey: attachmentKeys.all(workspaceID, actorID),
    }),
    cache.invalidateQueries({
      queryKey: workspaceKeys.activity(workspaceID, actorID),
    }),
  ]);
}
export function useAttachmentRemove(workspaceID: string, file: Attachment) {
  const { user } = useAuth(),
    cache = useQueryClient();
  useEffect(
    () => activateActor(user?.uuid, workspaceID),
    [user?.uuid, workspaceID],
  );
  const all = useSyncExternalStore(
      subscribe,
      () => entries,
      () => empty,
    ),
    operationKey = key(user?.uuid ?? "", workspaceID, file.id);
  return {
    locked: all.has(operationKey),
    async submit() {
      if (
        !user?.uuid ||
        entries.has(operationKey) ||
        creating.has(operationKey)
      )
        return;
      creating.add(operationKey);
      try {
        await commit(operationKey, {
          id: operationKey,
          actorID: user.uuid,
          command: {
            workspaceID,
            action: "attachment.remove",
            attachmentID: file.id,
            body: { expected_version: file.version },
          },
          key: crypto.randomUUID(),
          outcome: "uncertain",
          retryable: true,
          pending: false,
          reviewed: false,
          reviewing: false,
          canRetry: false,
          error: null,
        });
        await send(operationKey);
        await invalidate(cache, workspaceID, user.uuid);
      } finally {
        creating.delete(operationKey);
      }
    },
  };
}
export function AttachmentPendingRemovals({
  workspaceID,
  allowed,
}: {
  workspaceID: string;
  allowed: boolean;
}) {
  const { user } = useAuth(),
    { t } = useLocale(),
    cache = useQueryClient();
  const all = useSyncExternalStore(
      subscribe,
      () => entries,
      () => empty,
    ),
    [error, setError] = useState<string | null>(null);
  useEffect(() => {
    activateActor(user?.uuid, workspaceID);
    if (user?.uuid)
      void hydrate(user.uuid).catch((cause) => setError(String(cause)));
  }, [user?.uuid, workspaceID]);
  const scoped = [...all].filter(
    ([, entry]) =>
      sameID(entry.actorID, user?.uuid) &&
      sameID(entry.command.workspaceID, workspaceID),
  );
  async function refresh(k: string, entry: Entry) {
    const epoch = reviewContext.token();
    if (!sameID(entry.actorID, activeActor)) return;
    try {
      await commit(k, {
        ...entry,
        reviewed: false,
        reviewing: true,
        canRetry: false,
      });
      if (!reviewContext.current(epoch) || !sameID(entry.actorID, activeActor))
        return;
      await workspaceService.get(workspaceID);
      const config = await attachmentService.config(workspaceID);
      const file = (
        await attachmentService.get(workspaceID, entry.command.attachmentID, "")
      ).attachment;
      const list = file.issue_id
        ? await attachmentService.list(workspaceID, file.issue_id, true, "")
        : null;
      if (!reviewContext.current(epoch) || !sameID(entry.actorID, activeActor))
        return;
      await commit(k, {
        ...entry,
        reviewed: true,
        reviewing: false,
        canRetry: mayRetryRemoval(
          entry,
          config.capabilities.manage,
          !!list?.capabilities.remove,
          file.removed,
          file.capabilities.remove,
        ),
        error: null,
      });
    } catch (cause) {
      if (!reviewContext.current(epoch) || !sameID(entry.actorID, activeActor))
        return;
      await commit(k, {
        ...entry,
        reviewed: false,
        reviewing: false,
        canRetry: false,
        error: cause instanceof Error ? cause.message : "TRANSFER_FAILED",
      }).catch((failure) => setError(String(failure)));
    }
  }
  if (!scoped.length && !error) return null;
  return (
    <Section title={t("attachments.pendingRemovals")}>
      <div hidden={!allowed}>
        {scoped.map(([k, entry]) => (
          <div key={k} className="space-y-2 rounded border p-3">
            <p className="font-mono text-xs break-all">
              {entry.command.attachmentID} · {entry.key}
            </p>
            <p role="alert">
              {t(
                entry.outcome === "rejected"
                  ? "attachments.rejected"
                  : "attachments.uncertain",
              )}
            </p>
            <details>
              <summary>{t("attachments.requestMetadata")}</summary>
              <pre className="max-h-48 overflow-auto text-xs">
                {JSON.stringify(entry.command, null, 2)}
              </pre>
            </details>
            <Button
              type="button"
              disabled={!allowed || entry.pending || entry.reviewing}
              onClick={() => void refresh(k, entry)}
            >
              {t("attachments.review")}
            </Button>
            <Button
              type="button"
              disabled={
                !allowed ||
                entry.pending ||
                entry.reviewing ||
                !entry.reviewed ||
                !entry.canRetry
              }
              onClick={() => {
                void send(k)
                  .then(() => invalidate(cache, workspaceID, user?.uuid))
                  .catch((cause) => setError(String(cause)));
              }}
            >
              {t("attachments.retrySame")}
            </Button>
            <Button
              type="button"
              disabled={
                !allowed || entry.pending || entry.reviewing || !entry.reviewed
              }
              onClick={() =>
                void commit(k, null).catch((cause) => setError(String(cause)))
              }
            >
              {t("attachments.discard")}
            </Button>
            {entry.error && (
              <p role="alert">{attachmentError(entry.error, t)}</p>
            )}
          </div>
        ))}
      </div>
      {error && <p role="alert">{attachmentError(error, t)}</p>}
    </Section>
  );
}
