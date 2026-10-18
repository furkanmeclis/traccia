"use client";
import { useState, useSyncExternalStore } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { ApiError } from "@/lib/api";
import { useAuth } from "@/providers/auth-provider";
import { useLocale } from "@/providers/locale-provider";
import { workspaceService } from "@/features/workspaces/workspace.service";
import { useWorkspace, workspaceKeys } from "@/features/workspaces/queries";
import type { OperationSubmission } from "@/features/workspaces/operation-submission";
import { personalService, type FavoriteCommand } from "./personal.service";
const executeFavoriteCommand = personalService.setFavorite;
import { favoriteOperations as store } from "./personal-store";
import type {
  ViewAttempt,
  ViewOperation,
} from "../searchviews/operation-state";
import { personalKeys, usePersonalConfig } from "./queries";
export function useFavoriteOperations() {
  return useSyncExternalStore(
    store.subscribe,
    store.snapshot,
    store.serverSnapshot,
  );
}
const same = (a: string, b: string) => a.toLowerCase() === b.toLowerCase();
async function send(
  id: string,
  submission: OperationSubmission<FavoriteCommand>,
  ticket: ViewAttempt,
  cache: ReturnType<typeof useQueryClient>,
  onSaved?: (
    value: Awaited<ReturnType<typeof personalService.setFavorite>>,
  ) => void,
) {
  const entry = store.get(id);
  if (!entry) return;
  try {
    const result = await executeFavoriteCommand(
      submission.input,
      submission.key,
      ticket.signal,
    );
    if (!store.success(id, ticket)) return;
    onSaved?.(result);
    await Promise.all([
      cache.invalidateQueries({
        queryKey: personalKeys.all(entry.actorID, entry.workspaceID),
      }),
      cache.invalidateQueries({
        queryKey: workspaceKeys.activity(entry.workspaceID, entry.actorID),
      }),
    ]);
  } catch (error) {
    store.failed(
      id,
      ticket,
      error instanceof Error ? error.message : String(error),
      error instanceof ApiError ? error.status : undefined,
    );
  }
}
export function useFavoriteOperation(
  workspaceID: string,
  target: string,
  ready: boolean,
  onSaved?: (
    value: Awaited<ReturnType<typeof personalService.setFavorite>>,
  ) => void,
) {
  const { user } = useAuth(),
    cache = useQueryClient(),
    entries = useFavoriteOperations(),
    [error, setError] = useState<string | null>(null);
  const scope = { actorID: user?.uuid ?? "", workspaceID, target },
    entry = [...entries.values()].find(
      (e) =>
        same(e.actorID, scope.actorID) &&
        same(e.workspaceID, workspaceID) &&
        same(e.target, target),
    );
  return {
    entry,
    locked: !!entry,
    error,
    async submit(command: FavoriteCommand, label: string) {
      if (!ready || !user?.uuid || entry) return;
      try {
        setError(null);
        const created = store.begin(scope, command.action, label, command);
        await send(
          created.id,
          structuredClone(created.submission),
          store.ticket(created.id),
          cache,
          onSaved,
        );
      } catch (error) {
        setError(error instanceof Error ? error.message : String(error));
      }
    },
  };
}
async function review(entry: ViewOperation<FavoriteCommand>) {
  const workspace = await workspaceService.get(entry.workspaceID),
    config = await personalService.config(entry.workspaceID);
  if (!workspace.capabilities.update || !config.capabilities.manage)
    return false;
  const current = await personalService.favorites(entry.workspaceID);
  return (
    current.capabilities.manage &&
    current.items.every((row) => same(row.actor_id, entry.actorID))
  );
}
function PendingCommand({
  entry,
  ready,
  canWrite,
}: {
  entry: ViewOperation<FavoriteCommand>;
  ready: boolean;
  canWrite: boolean;
}) {
  const { t } = useLocale(),
    cache = useQueryClient();
  return (
    <article className="space-y-2 rounded border p-3">
      <h3 className="font-medium">{entry.label}</h3>
      <p className="text-sm">
        {t(
          entry.outcome === "rejected"
            ? "personalwork.commandRejected"
            : "personalwork.reviewRequired",
        )}
      </p>
      <details>
        <summary>{t("personalwork.recovery")}</summary>
        {entry.error && (
          <p role="alert" className="text-sm break-all">
            {entry.error}
          </p>
        )}
        <pre className="max-h-64 overflow-auto text-xs">
          {JSON.stringify(entry.submission.input, null, 2)}
        </pre>
        <p className="font-mono text-xs break-all">{entry.submission.key}</p>
      </details>
      <div className="flex flex-wrap gap-2">
        <Button
          type="button"
          variant="outline"
          disabled={!ready || entry.pending || entry.reviewing}
          onClick={() => void store.review(entry.id, review)}
        >
          {t("personalwork.review")}
        </Button>
        <Button
          type="button"
          disabled={
            !ready ||
            !canWrite ||
            entry.pending ||
            entry.reviewing ||
            !entry.reviewed ||
            !entry.canRetry ||
            entry.reviewError
          }
          onClick={() => {
            try {
              const replay = store.retry(entry.id);
              void send(entry.id, replay, store.ticket(entry.id), cache);
            } catch {
              /* review/context gate remains visible */
            }
          }}
        >
          {t("personalwork.retry")}
        </Button>
        <Button
          type="button"
          variant="outline"
          disabled={
            !ready || entry.pending || entry.reviewing || !entry.reviewed
          }
          onClick={() => {
            try {
              store.discard(entry.id);
            } catch {
              /* context changed */
            }
          }}
        >
          {t("personalwork.discard")}
        </Button>
      </div>
    </article>
  );
}
export function FavoritePendingOperations({
  workspaceID,
}: {
  workspaceID: string;
}) {
  const { user } = useAuth(),
    { t } = useLocale(),
    entries = useFavoriteOperations(),
    workspace = useWorkspace(workspaceID),
    config = usePersonalConfig(workspaceID),
    ready =
      workspace.isSuccess &&
      !workspace.isError &&
      !workspace.isFetching &&
      config.isSuccess &&
      !config.isError &&
      !config.isFetching;
  const own = [...entries.values()].filter(
    (e) =>
      same(e.workspaceID, workspaceID) && same(e.actorID, user?.uuid ?? ""),
  );
  if (!own.length) return null;
  return (
    <section className="space-y-3" aria-label={t("personalwork.recovery")}>
      <h2 className="text-lg font-semibold">{t("personalwork.recovery")}</h2>
      {!ready && <p role="alert">{t("personalwork.readError")}</p>}
      {own.map((entry) => (
        <PendingCommand
          key={entry.id}
          entry={entry}
          ready={ready}
          canWrite={
            ready &&
            !!workspace.data?.capabilities.update &&
            !!config.data?.capabilities.manage
          }
        />
      ))}
    </section>
  );
}
