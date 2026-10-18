"use client";
import { useSyncExternalStore } from "react";
import Link from "next/link";
import { useMutation, useQueryClient, useQuery } from "@tanstack/react-query";
import { useAuth } from "@/providers/auth-provider";
import { useLocale } from "@/providers/locale-provider";
import { Button } from "@/components/ui/button";
import { ApiError } from "@/lib/api";
import {
  prepareSubmission,
  type OperationSubmission,
} from "@/features/workspaces/operation-submission";
import { workspaceService } from "@/features/workspaces/workspace.service";
import { useWorkspace, workspaceKeys } from "@/features/workspaces/queries";
import { teamService } from "@/features/teams/team.service";
import { issueService } from "@/features/issues/issue.service";
import { issueKeys, useIssueSelection } from "@/features/issues/queries";
import { Section } from "@/features/issues/issue-ui";
import {
  executeDocumentCommand,
  documentService,
  type DocumentCommand,
  type Document,
  type Owner,
} from "./document.service";
import { documentKeys, useDocument } from "./queries";
import {
  documentOperationStore as store,
  documentOperationScope,
  operationFailure,
  type PendingDocumentOperation,
} from "./operation-state";
export function useDocumentOperation(
  workspaceID: string,
  owner: Owner,
  operationID: string,
  label: string,
  onSaved?: (document: Document) => void,
) {
  const { user } = useAuth();
  const cache = useQueryClient();
  const scope = documentOperationScope(
      user?.uuid,
      workspaceID,
      owner.kind + ":" + owner.id,
    ),
    operationKey = JSON.stringify([scope, operationID]);
  const entries = useSyncExternalStore(
      store.subscribe,
      store.snapshot,
      store.serverSnapshot,
    ),
    entry = entries.get(operationKey);
  const mutation = useMutation({
    mutationFn: (submission: OperationSubmission<DocumentCommand>) =>
      executeDocumentCommand(submission.input, submission.key),
    onError: (error) => {
      const prior = store.get(operationKey);
      if (prior)
        store.patch(
          operationKey,
          operationFailure(
            prior,
            error instanceof ApiError ? error.code : undefined,
            error instanceof ApiError ? error.status : undefined,
          ),
        );
      void cache.invalidateQueries({ queryKey: documentKeys.all(workspaceID) });
      if (error instanceof ApiError && [401, 403, 404].includes(error.status))
        void cache.invalidateQueries({
          queryKey: workspaceKeys.detail(workspaceID, user?.uuid),
        });
    },
    onSuccess: async (result) => {
      await Promise.all([
        cache.invalidateQueries({ queryKey: documentKeys.all(workspaceID) }),
        cache.invalidateQueries({ queryKey: issueKeys.all(workspaceID) }),
        cache.invalidateQueries({
          queryKey: workspaceKeys.activity(workspaceID, user?.uuid),
        }),
      ]);
      store.clear(operationKey);
      onSaved?.(result.document);
    },
  });
  function review() {
    const prior = store.get(operationKey);
    if (!prior || prior.pending || prior.reviewing) return;
    store.patch(operationKey, {
      reviewed: false,
      reviewing: true,
      reviewError: false,
    });
    const command = prior.submission.input as DocumentCommand;
    void Promise.all([
      workspaceService.get(command.workspaceID),
      command.action === "issue.create-rich"
        ? Promise.all([
            issueService.references(command.workspaceID),
            issueService.selection(
              command.workspaceID,
              command.body.issue.team_id,
              command.body.issue.project_id ?? "",
            ),
          ])
        : documentService.resolve(command.workspaceID, command.owner),
    ])
      .then(() =>
        cache.invalidateQueries({ queryKey: documentKeys.all(workspaceID) }),
      )
      .then(
        () => store.patch(operationKey, { reviewed: true, reviewing: false }),
        () =>
          store.patch(operationKey, {
            reviewed: false,
            reviewing: false,
            reviewError: true,
          }),
      );
  }
  return {
    entry,
    locked: !!entry,
    operationKey,
    submit(command: DocumentCommand) {
      if (!user?.uuid || store.get(operationKey)) return;
      const submission = prepareSubmission<DocumentCommand>(
        null,
        command,
        () => command,
      );
      const retry = () => {
        const current = store.get(operationKey);
        if (
          !current ||
          current.pending ||
          current.conflict ||
          current.validationRejected ||
          current.reviewing ||
          (current.uncertain && !current.reviewed)
        )
          return;
        store.patch(operationKey, {
          pending: true,
          reviewed: false,
          reviewError: false,
        });
        mutation.mutate(
          current.submission as OperationSubmission<DocumentCommand>,
        );
      };
      store.set(operationKey, {
        scope,
        label,
        submission,
        pending: true,
        uncertain: false,
        conflict: false,
        validationRejected: false,
        reviewed: false,
        reviewing: false,
        reviewError: false,
        retry,
        review,
      });
      mutation.mutate(submission);
    },
  };
}
export function DocumentOperationNotice({
  operationKey,
  entry,
}: {
  operationKey: string;
  entry: PendingDocumentOperation;
}) {
  const { t } = useLocale();
  const { user } = useAuth();
  const command = entry.submission.input as DocumentCommand;
  const workspace = useWorkspace(command.workspaceID);
  const allowed =
    workspace.isSuccess && !workspace.isError && !workspace.isFetching;
  const current = useDocument(
    command.workspaceID,
    command.owner,
    allowed && command.action !== "issue.create-rich",
  );
  const rich = command.action === "issue.create-rich" ? command : null;
  const selection = useIssueSelection(
    command.workspaceID,
    rich?.body.issue.team_id ?? "",
    rich?.body.issue.project_id ?? "",
    allowed && !!rich,
  );
  const richPermission = useQuery({
    queryKey: [
      ...documentKeys.all(command.workspaceID),
      user?.uuid,
      "rich-permission",
      rich?.body.issue.team_id,
    ],
    enabled: allowed && !!rich,
    staleTime: 0,
    queryFn: async () => {
      if (!rich) return false;
      const [team, list] = await Promise.all([
        teamService.get(command.workspaceID, rich.body.issue.team_id),
        issueService.list(command.workspaceID, {
          archived: false,
          q: "",
          team_id: rich.body.issue.team_id,
          project_id: "",
        }),
      ]);
      return !team.team.archived && list.capabilities.manage;
    },
  });
  const canRun =
    allowed &&
    (rich
      ? richPermission.isSuccess &&
        !richPermission.isError &&
        !richPermission.isFetching &&
        selection.isSuccess &&
        !selection.isError &&
        !selection.isFetching &&
        richPermission.data === true
      : current.isSuccess &&
        !current.isError &&
        !current.isFetching &&
        current.data.capabilities.manage);
  return (
    <div className="space-y-3 rounded border p-3">
      <p className="font-medium">{entry.label}</p>
      {!rich && (
        <Link
          className="underline"
          href={`/w/${command.workspaceID}/${command.owner.kind === "issue" ? "issues" : "projects"}/${command.owner.id}`}
        >
          {t("documents.viewOwner")}
        </Link>
      )}
      {entry.pending ? (
        <p role="status">{t("common.loading")}</p>
      ) : (
        <>
          <p role="alert">
            {t(
              entry.validationRejected
                ? "documents.validation"
                : entry.conflict
                  ? "documents.conflict"
                  : "documents.saveError",
            )}
          </p>
          <div className="flex flex-wrap gap-2">
            {!entry.conflict && !entry.validationRejected && (
              <Button
                type="button"
                disabled={
                  !canRun ||
                  entry.reviewing ||
                  (entry.uncertain && !entry.reviewed)
                }
                onClick={entry.retry}
              >
                {t("documents.retry")}
              </Button>
            )}
            {entry.validationRejected ? (
              <Button
                type="button"
                variant="outline"
                onClick={() => store.clear(operationKey)}
              >
                {t("documents.editRequest")}
              </Button>
            ) : entry.reviewed ? (
              <Button
                type="button"
                variant="outline"
                onClick={() => store.clear(operationKey)}
              >
                {t("documents.discard")}
              </Button>
            ) : (
              <Button
                type="button"
                variant="outline"
                disabled={entry.reviewing}
                onClick={entry.review}
              >
                {t("documents.review")}
              </Button>
            )}
          </div>
          {(entry.reviewError || current.isError || selection.isError) && (
            <p role="alert">{t("issues.load_error")}</p>
          )}
        </>
      )}
    </div>
  );
}
export function DocumentPendingOperations({
  workspaceId,
}: {
  workspaceId: string;
}) {
  const { user } = useAuth();
  const { t } = useLocale();
  const entries = useSyncExternalStore(
    store.subscribe,
    store.snapshot,
    store.serverSnapshot,
  );
  const pending = [...entries].filter(([, entry]) => {
    const c = entry.submission.input as DocumentCommand;
    return (
      c.workspaceID === workspaceId &&
      entry.scope ===
        documentOperationScope(
          user?.uuid,
          workspaceId,
          c.owner.kind + ":" + c.owner.id,
        )
    );
  });
  if (!pending.length) return null;
  return (
    <Section title={t("documents.pending")}>
      {pending.map(([key, entry]) => (
        <DocumentOperationNotice key={key} operationKey={key} entry={entry} />
      ))}
    </Section>
  );
}
