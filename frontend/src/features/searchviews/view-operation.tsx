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
import {
  executeViewCommand,
  searchService,
  type ViewCommand,
} from "./search.service";
import { viewOperations as store } from "./search-store";
import type { ViewAttempt, ViewOperation } from "./operation-state";
import { searchKeys, useSearchConfig } from "./queries";
export function useViewOperations() {
  return useSyncExternalStore(
    store.subscribe,
    store.snapshot,
    store.serverSnapshot,
  );
}
const same = (a: string, b: string) => a.toLowerCase() === b.toLowerCase();
async function send(
  id: string,
  submission: OperationSubmission<ViewCommand>,
  ticket: ViewAttempt,
  cache: ReturnType<typeof useQueryClient>,
  onSaved?: (value: Awaited<ReturnType<typeof executeViewCommand>>) => void,
) {
  const entry = store.get(id);
  if (!entry) return;
  try {
    const result = await executeViewCommand(
      submission.input,
      submission.key,
      ticket.signal,
    );
    if (!store.success(id, ticket)) return;
    onSaved?.(result);
    await Promise.all([
      cache.invalidateQueries({
        queryKey: searchKeys.all(entry.workspaceID, entry.actorID),
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
export function useViewOperation(
  workspaceID: string,
  target: string,
  ready: boolean,
  onSaved?: (value: Awaited<ReturnType<typeof executeViewCommand>>) => void,
) {
  const { user } = useAuth(),
    cache = useQueryClient(),
    entries = useViewOperations(),
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
    async submit(command: ViewCommand, label: string) {
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
async function review(entry: ViewOperation<ViewCommand>) {
  const workspace = await workspaceService.get(entry.workspaceID),
    config = await searchService.config(entry.workspaceID);
  if (!workspace.capabilities.update || !config.capabilities.manage)
    return false;
  const command = entry.submission.input;
  if (command.action === "view.create") return true;
  if (command.action === "columns.set") {
    const current = await searchService.columns(
      entry.workspaceID,
      command.body.resource,
    );
    return current.capabilities.manage;
  }
  const current = await searchService.view(entry.workspaceID, command.viewID);
  return current.view.capabilities.manage;
}
function PendingCommand({
  entry,
  ready,
  canWrite,
}: {
  entry: ViewOperation<ViewCommand>;
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
            ? "searchviews.commandRejected"
            : "searchviews.reviewRequired",
        )}
      </p>
      {entry.error && (
        <p role="alert" className="text-sm break-all">
          {entry.error}
        </p>
      )}
      <details>
        <summary>{t("searchviews.recovery")}</summary>
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
          {t("searchviews.review")}
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
          {t("searchviews.retry")}
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
          {t("searchviews.discard")}
        </Button>
      </div>
    </article>
  );
}
export function ViewPendingOperations({
  workspaceID,
}: {
  workspaceID: string;
}) {
  const { user } = useAuth(),
    { t } = useLocale(),
    entries = useViewOperations(),
    workspace = useWorkspace(workspaceID),
    config = useSearchConfig(
      workspaceID,
      workspace.isSuccess && !workspace.isError,
    ),
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
    <section className="space-y-3" aria-label={t("searchviews.recovery")}>
      <h2 className="text-lg font-semibold">{t("searchviews.recovery")}</h2>
      {!ready && <p role="alert">{t("searchviews.readError")}</p>}
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
