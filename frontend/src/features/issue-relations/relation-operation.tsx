"use client";
import { useSyncExternalStore } from "react";
import Link from "next/link";
import { useMutation, useQueryClient } from "@tanstack/react-query";
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
import { Section } from "@/features/issues/issue-ui";
import {
  executeRelationCommand,
  relationService,
  isUnlink,
  type RelationCommand,
} from "./relation.service";
import { relationKeys, useRelationState } from "./queries";
import {
  relationOperationStore as store,
  relationOperationScope,
  operationFailure,
  type PendingRelationOperation,
} from "./operation-state";
export function useRelationOperation(
  workspaceID: string,
  issueID: string,
  operationID: string,
  label: string,
  onSaved?: () => void,
) {
  const { user } = useAuth();
  const cache = useQueryClient();
  const scope = relationOperationScope(user?.uuid, workspaceID, issueID);
  const operationKey = JSON.stringify([scope, operationID]);
  const entries = useSyncExternalStore(
    store.subscribe,
    store.snapshot,
    store.serverSnapshot,
  );
  const entry = entries.get(operationKey);
  const mutation = useMutation({
    mutationFn: (submission: OperationSubmission<RelationCommand>) =>
      executeRelationCommand(submission.input, submission.key),
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
      if (error instanceof ApiError && [401, 403, 404].includes(error.status)) {
        void cache.invalidateQueries({
          queryKey: relationKeys.all(workspaceID),
        });
        void cache.invalidateQueries({
          queryKey: workspaceKeys.detail(workspaceID, user?.uuid),
        });
      }
    },
    onSuccess: async () => {
      await cache.invalidateQueries({
        queryKey: relationKeys.all(workspaceID),
      });
      await cache.invalidateQueries({
        queryKey: workspaceKeys.activity(workspaceID, user?.uuid),
      });
      store.clear(operationKey);
      onSaved?.();
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
    const command = prior.submission.input as RelationCommand;
    const ids = new Set([
      command.path.issueID,
      ...(command.body.expected_versions ?? []).map((item) => item.issue_id),
    ]);
    void Promise.all([
      workspaceService.get(command.path.workspaceID),
      ...[...ids].map(async (id) => {
        const state = await relationService.state(command.path.workspaceID, id);
        cache.setQueryData(
          relationKeys.state(command.path.workspaceID, user?.uuid, id),
          state,
        );
      }),
    ])
      .then(() =>
        cache.invalidateQueries({ queryKey: relationKeys.all(workspaceID) }),
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
    submit(command: RelationCommand) {
      if (!user?.uuid || store.get(operationKey)) return;
      const submission = prepareSubmission<RelationCommand>(
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
          current.submission as OperationSubmission<RelationCommand>,
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
export function RelationOperationNotice({
  operationKey,
  entry,
}: {
  operationKey: string;
  entry: PendingRelationOperation;
}) {
  const { t } = useLocale();
  const command = entry.submission.input as RelationCommand;
  const workspace = useWorkspace(command.path.workspaceID);
  const allowed =
    workspace.isSuccess && !workspace.isError && !workspace.isFetching;
  const current = useRelationState(
    command.path.workspaceID,
    command.path.issueID,
    allowed,
  );
  const needsActiveTarget =
    !isUnlink(command) && !!command.body.target_issue_id;
  const target = useRelationState(
    command.path.workspaceID,
    command.body.target_issue_id ?? "",
    allowed && needsActiveTarget,
  );
  const targetReady =
    !needsActiveTarget ||
    (target.isSuccess &&
      !target.isError &&
      !target.isFetching &&
      target.data.issue.active &&
      target.data.capabilities.link);
  const canRun =
    targetReady &&
    allowed &&
    current.isSuccess &&
    !current.isError &&
    !current.isFetching &&
    (isUnlink(command)
      ? current.data.capabilities.unlink
      : current.data.capabilities.link);
  return (
    <div className="space-y-3 rounded-lg border p-4">
      <p className="font-medium">{entry.label}</p>
      <Link
        className="text-primary text-sm underline"
        href={`/w/${command.path.workspaceID}/issues/${command.path.issueID}`}
      >
        {t("relations.view_issue")}
      </Link>
      {entry.pending ? (
        <p role="status">{t("common.loading")}</p>
      ) : (
        <>
          <p role="alert">
            {t(
              entry.validationRejected
                ? "relations.validation_error"
                : entry.conflict
                  ? "relations.conflict"
                  : "relations.save_error",
            )}
          </p>
          <div className="flex flex-wrap gap-3">
            {!entry.conflict && !entry.validationRejected && (
              <Button
                type="button"
                variant="outline"
                disabled={
                  !canRun ||
                  entry.reviewing ||
                  (entry.uncertain && !entry.reviewed)
                }
                onClick={entry.retry}
              >
                {t("relations.retry")}
              </Button>
            )}
            {entry.validationRejected ? (
              <Button
                type="button"
                variant="outline"
                onClick={() => store.clear(operationKey)}
              >
                {t("relations.edit_request")}
              </Button>
            ) : entry.reviewed ? (
              <Button
                type="button"
                variant="outline"
                onClick={() => store.clear(operationKey)}
              >
                {t("relations.discard")}
              </Button>
            ) : (
              <Button
                type="button"
                variant="outline"
                disabled={entry.reviewing}
                onClick={entry.review}
              >
                {t("relations.review")}
              </Button>
            )}
          </div>
          {(entry.reviewError ||
            current.isError ||
            (needsActiveTarget && target.isError)) && (
            <p role="alert">{t("issues.load_error")}</p>
          )}
        </>
      )}
    </div>
  );
}
export function RelationPendingOperations({
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
    const command = entry.submission.input as RelationCommand;
    return (
      command.path.workspaceID === workspaceId &&
      entry.scope ===
        relationOperationScope(user?.uuid, workspaceId, command.path.issueID)
    );
  });
  if (!pending.length) return null;
  return (
    <Section title={t("relations.pending")}>
      <p className="text-muted-foreground text-sm">
        {t("relations.pending_help")}
      </p>
      {pending.map(([key, entry]) => (
        <RelationOperationNotice key={key} operationKey={key} entry={entry} />
      ))}
    </Section>
  );
}
