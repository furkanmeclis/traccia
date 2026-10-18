"use client";
import { useSyncExternalStore, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { ApiError } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { useLocale } from "@/providers/locale-provider";
import { workspaceService } from "@/features/workspaces/workspace.service";
import { workspaceKeys } from "@/features/workspaces/queries";
import { issueService } from "@/features/issues/issue.service";
import { issueKeys } from "@/features/issues/queries";
import type { components } from "@/generated/api";
import type {
  ViewAttempt,
  ViewOperation,
} from "../searchviews/operation-state";
import {
  quickDrafts,
  quickOperations,
  quickContextTicket,
  quickContextMatches,
} from "./quick-store";
import {
  captureQuickCommand,
  executeQuickCommand,
  type QuickCommand,
} from "./commands";
import type { QuickIssueDraft, QuickIssueCreated } from "./draft-state";

const submittedRevisions = new Map<string, number>();
export function useQuickEntries() {
  return useSyncExternalStore(
    quickOperations.subscribe,
    quickOperations.snapshot,
    quickOperations.serverSnapshot,
  );
}
export function useQuickDrafts() {
  return useSyncExternalStore(
    quickDrafts.subscribe,
    quickDrafts.snapshot,
    quickDrafts.serverSnapshot,
  );
}
const validID = (v: string) =>
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(v) &&
  v !== "00000000-0000-0000-0000-000000000000";
async function send(
  entry: ViewOperation<QuickCommand>,
  ticket: ViewAttempt,
  publication: number,
  cache: ReturnType<typeof useQueryClient>,
  onCreated: (created: QuickIssueCreated) => void,
) {
  try {
    const issueID = await executeQuickCommand(
      structuredClone(entry.submission.input),
      entry.submission.key,
    );
    if (!validID(issueID)) throw new Error("QUICK_RESULT_ID");
    if (!quickOperations.success(entry.id, ticket)) return;
    const revision = submittedRevisions.get(entry.id);
    submittedRevisions.delete(entry.id);
    const active = quickContextMatches(entry, entry.target, publication);
    quickDrafts.rememberCreated(entry, entry.target, issueID);
    if (
      active &&
      revision !== undefined &&
      quickDrafts.get(entry.target)?.revision === revision
    ) {
      onCreated({
        actorID: entry.actorID,
        workspaceID: entry.workspaceID,
        draftID: entry.target,
        issueID,
      });
      quickDrafts.complete(entry, entry.target, revision);
    }
    await Promise.all([
      cache.invalidateQueries({ queryKey: issueKeys.all(entry.workspaceID) }),
      cache.invalidateQueries({
        queryKey: workspaceKeys.activity(entry.workspaceID, entry.actorID),
      }),
    ]);
  } catch (error) {
    const status = error instanceof ApiError ? error.status : undefined;
    if (
      quickOperations.failed(
        entry.id,
        ticket,
        error instanceof Error ? error.message : String(error),
        status,
      )
    ) {
      if (status === undefined || status >= 500 || status === 408)
        quickDrafts.markUncertain(entry.target);
      if (status !== undefined && [401, 403, 404].includes(status))
        void cache.invalidateQueries({
          queryKey: workspaceKeys.detail(entry.workspaceID, entry.actorID),
        });
    }
  }
}
async function review(entry: ViewOperation<QuickCommand>) {
  const [workspace, list] = await Promise.all([
    workspaceService.get(entry.workspaceID),
    issueService.list(entry.workspaceID, {
      archived: false,
      q: "",
      team_id: "",
      project_id: "",
    }),
  ]);
  return workspace.capabilities.update && list.capabilities.manage;
}
export function useQuickOperation(
  draft: QuickIssueDraft | undefined,
  ready: boolean,
  onCreated: (created: QuickIssueCreated) => void,
) {
  const entries = useQuickEntries(),
    cache = useQueryClient(),
    [error, setError] = useState<string | null>(null);
  const entry =
    draft &&
    [...entries.values()].find(
      (e) =>
        e.target === draft.id &&
        e.actorID.toLowerCase() === draft.actorID.toLowerCase() &&
        e.workspaceID.toLowerCase() === draft.workspaceID.toLowerCase(),
    );
  return {
    entry,
    error,
    async submit(metadata: components["schemas"]["ProductIssuesCreateInput"]) {
      if (!draft || !ready || entry || draft.createdIssueID) return;
      try {
        const current = quickDrafts.get(draft.id);
        if (
          !current ||
          current.metadata.values.title !== current.metadata.values.title.trim()
        )
          throw new Error("QUICK_TITLE_BOUNDARY");
        const command = captureQuickCommand(current, metadata);
        const created = quickOperations.begin(
          {
            actorID: draft.actorID,
            workspaceID: draft.workspaceID,
            target: draft.id,
          },
          command.action,
          "",
          command,
        );
        submittedRevisions.set(created.id, current.revision);
        setError(null);
        await send(
          created,
          quickOperations.ticket(created.id),
          quickContextTicket(),
          cache,
          onCreated,
        );
      } catch (error) {
        setError(error instanceof Error ? error.message : String(error));
      }
    },
  };
}
export function QuickOperationNotice({
  entry,
  ready,
  onCreated,
}: {
  entry: ViewOperation<QuickCommand>;
  ready: boolean;
  onCreated: (created: QuickIssueCreated) => void;
}) {
  const { t } = useLocale(),
    cache = useQueryClient();
  useQuickDrafts();
  useQuickEntries();
  const draft = quickDrafts.get(entry.target),
    uncertain = draft?.uncertain || entry.outcome === "uncertain";
  return (
    <section
      className="space-y-3 rounded border p-3"
      aria-label={t("quickissue.recovery")}
    >
      <p role="status">
        {t(
          entry.pending
            ? "quickissue.pending"
            : uncertain
              ? "quickissue.uncertain"
              : "quickissue.rejected",
        )}
      </p>
      {entry.error && <p role="alert">{t("quickissue.saveError")}</p>}
      <details>
        <summary>{t("quickissue.request")}</summary>
        <pre className="max-h-48 overflow-auto text-xs break-all whitespace-pre-wrap">
          {JSON.stringify(entry.submission.input, null, 2)}
        </pre>
        <p className="font-mono text-xs break-all">{entry.submission.key}</p>
      </details>
      <div className="flex flex-wrap gap-2">
        <Button
          type="button"
          variant="outline"
          disabled={!ready || entry.pending || entry.reviewing}
          onClick={() => void quickOperations.review(entry.id, review)}
        >
          {t("quickissue.review")}
        </Button>
        <Button
          type="button"
          disabled={
            !ready ||
            entry.pending ||
            entry.reviewing ||
            !entry.reviewed ||
            !entry.canRetry ||
            entry.reviewError
          }
          onClick={() => {
            try {
              quickOperations.retry(entry.id);
              const current = quickOperations.get(entry.id);
              if (current)
                void send(
                  current,
                  quickOperations.ticket(entry.id),
                  quickContextTicket(),
                  cache,
                  onCreated,
                );
            } catch {
              /* Visible review gate remains. */
            }
          }}
        >
          {t("quickissue.retry")}
        </Button>
        {!uncertain && (
          <Button
            type="button"
            variant="outline"
            disabled={
              !ready || entry.pending || entry.reviewing || !entry.reviewed
            }
            onClick={() => {
              try {
                quickOperations.discard(entry.id);
                submittedRevisions.delete(entry.id);
              } catch {
                /* Keep the captured request. */
              }
            }}
          >
            {t("quickissue.editRejected")}
          </Button>
        )}
      </div>
      {entry.reviewError && <p role="alert">{t("quickissue.reviewError")}</p>}
    </section>
  );
}
