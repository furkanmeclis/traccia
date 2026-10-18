"use client";
import { useSyncExternalStore, type ReactNode } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { ApiError } from "@/lib/api";
import { useLocale } from "@/providers/locale-provider";
import { useAuth } from "@/providers/auth-provider";
import {
  prepareSubmission,
  type OperationSubmission,
} from "@/features/workspaces/operation-submission";
import { issueKeys, useIssue } from "./queries";
import { issueService } from "./issue.service";
import { workspaceService } from "@/features/workspaces/workspace.service";
import { executeIssueCommand, type IssueCommand } from "./issue.service";
import {
  issueOperationStore as store,
  issueOperationScope,
  operationFailure,
} from "./operation-state";

export function Section({
  title,
  children,
}: {
  title: string;
  children: ReactNode;
}) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>{title}</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">{children}</CardContent>
    </Card>
  );
}
export function LoadNotice({
  pending,
  error,
  retry,
}: {
  pending: boolean;
  error: boolean;
  retry: () => void;
}) {
  const { t } = useLocale();
  if (error)
    return (
      <div role="alert" className="space-y-3">
        <p>{t("issues.load_error")}</p>
        <Button type="button" variant="outline" onClick={retry}>
          {t("issues.retry")}
        </Button>
      </div>
    );
  if (pending) return <p role="status">{t("common.loading")}</p>;
  return null;
}
export function useIssueMutation({
  workspaceId,
  issueId,
  operationId,
  operationLabel,
  reviewLatest,
  onSaved,
}: {
  workspaceId: string;
  issueId?: string;
  operationId: string;
  operationLabel: string;
  reviewLatest: () => Promise<unknown>;
  onSaved?: (result: Awaited<ReturnType<typeof executeIssueCommand>>) => void;
}) {
  const cache = useQueryClient();
  const { user } = useAuth();
  const scope = issueOperationScope(user?.uuid, workspaceId, issueId);
  const operationKey = JSON.stringify([scope, operationId]);
  const entries = useSyncExternalStore(
    store.subscribe,
    store.snapshot,
    store.serverSnapshot,
  );
  const entry = entries.get(operationKey);
  const mutation = useMutation({
    mutationFn: (submission: OperationSubmission<IssueCommand>) =>
      executeIssueCommand(submission.input, submission.key),
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
      if (error instanceof ApiError && [401, 403, 404].includes(error.status))
        void cache.invalidateQueries({
          queryKey: issueKeys.all(workspaceId),
        });
    },
    onSuccess: async (result) => {
      if (issueId)
        cache.setQueryData(
          issueKeys.detail(workspaceId, user?.uuid, issueId),
          result,
        );
      await cache.invalidateQueries({ queryKey: issueKeys.all(workspaceId) });
      await cache.invalidateQueries({
        queryKey: ["product-workspaces", workspaceId, "activity", user?.uuid],
      });
      onSaved?.(result);
      store.clear(operationKey);
    },
  });
  function review() {
    const prior = store.get(operationKey);
    if (!prior || prior.reviewing || prior.pending) return;
    store.patch(operationKey, {
      reviewing: true,
      reviewed: false,
      reviewError: false,
    });
    const command = prior.submission.input as IssueCommand;
    const freshReview = async () => {
      await workspaceService.get(command.path.workspaceID);
      if ("issueID" in command.path) {
        try {
          const latest = await issueService.get(
            command.path.workspaceID,
            command.path.issueID,
          );
          cache.setQueryData(
            issueKeys.detail(
              command.path.workspaceID,
              user?.uuid,
              command.path.issueID,
            ),
            latest,
          );
        } catch (error) {
          if (!(error instanceof ApiError && error.status === 404)) throw error;
          return;
        }
      }
      await reviewLatest();
    };
    void freshReview().then(
      () => store.patch(operationKey, { reviewing: false, reviewed: true }),
      () =>
        store.patch(operationKey, {
          reviewing: false,
          reviewed: false,
          reviewError: true,
        }),
    );
  }
  return {
    ...mutation,
    isError: !!entry && !entry.pending,
    isPending: !!entry?.pending,
    conflict: entry?.conflict ?? false,
    validationRejected: entry?.validationRejected ?? false,
    command: entry?.submission.input as IssueCommand | undefined,
    locked: !!entry,
    uncertain: entry?.uncertain ?? false,
    review: {
      isSuccess: entry?.reviewed ?? false,
      isPending: entry?.reviewing ?? false,
      isError: entry?.reviewError ?? false,
      mutate: review,
    },
    submit(input: IssueCommand) {
      if (store.get(operationKey) || !user?.uuid) return;
      const submission = prepareSubmission<IssueCommand>(
        null,
        input,
        () => input,
      );
      const retry = () => {
        const current = store.get(operationKey);
        if (
          !current ||
          current.pending ||
          current.conflict ||
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
          current.submission as OperationSubmission<IssueCommand>,
        );
      };
      store.set(operationKey, {
        scope,
        label: operationLabel,
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
    retrySubmission() {
      store.get(operationKey)?.retry();
    },
    editRejected() {
      if (store.get(operationKey)?.validationRejected) {
        store.clear(operationKey);
        mutation.reset();
      }
    },
    reconcile() {
      if (store.get(operationKey)?.reviewed) {
        store.clear(operationKey);
        mutation.reset();
      }
    },
  };
}
export function PendingOperations({
  workspaceId,
  issueId,
  canRun,
}: {
  workspaceId: string;
  issueId?: string;
  canRun: (command: IssueCommand) => boolean;
}) {
  const { user } = useAuth();
  const { t } = useLocale();
  const entries = useSyncExternalStore(
    store.subscribe,
    store.snapshot,
    store.serverSnapshot,
  );
  const scope = issueOperationScope(user?.uuid, workspaceId, issueId);
  const pending = [...entries].filter(([, entry]) => {
    const command = entry.submission.input as IssueCommand;
    return (
      command.path.workspaceID === workspaceId &&
      entry.scope ===
        issueOperationScope(
          user?.uuid,
          workspaceId,
          "issueID" in command.path ? command.path.issueID : undefined,
        ) &&
      (!issueId || entry.scope === scope)
    );
  });
  if (!pending.length) return null;
  return (
    <Section title={t("issues.pending_operations")}>
      <p className="text-muted-foreground text-sm">
        {t("issues.pending_help")}
      </p>
      {pending.map(([key, entry]) => (
        <PendingIssueOperation
          key={key}
          operationKey={key}
          entry={entry}
          canCreate={canRun(entry.submission.input as IssueCommand)}
        />
      ))}
    </Section>
  );
}
function PendingIssueOperation({
  operationKey,
  entry,
  canCreate,
}: {
  operationKey: string;
  entry: import("./operation-state").PendingIssueOperation;
  canCreate: boolean;
}) {
  const { t } = useLocale();
  const command = entry.submission.input as IssueCommand;
  const issueID = "issueID" in command.path ? command.path.issueID : "";
  const query = useIssue(command.path.workspaceID, issueID, !!issueID);
  const canRun = issueID
    ? query.isSuccess &&
      !query.isError &&
      !query.isFetching &&
      (command.action === "issue.move"
        ? query.data.capabilities.move
        : command.action === "issue.restore"
          ? query.data.capabilities.restore
          : query.data.capabilities.manage)
    : canCreate;
  return (
    <div className="space-y-3 rounded-lg border p-3">
      <p className="text-sm font-medium">{entry.label}</p>
      {issueID && (
        <a
          className="text-primary text-sm underline"
          href={`/w/${command.path.workspaceID}/issues/${issueID}`}
        >
          {t("issues.view_issue")}
        </a>
      )}
      {entry.pending ? (
        <p role="status">{t("common.loading")}</p>
      ) : (
        <>
          <p role="alert">
            {t(
              entry.validationRejected
                ? "issues.validation_error"
                : entry.conflict
                  ? "issues.version_conflict"
                  : "issues.save_error",
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
                {t("issues.retry_submission")}
              </Button>
            )}
            {entry.validationRejected ? (
              <Button
                type="button"
                variant="outline"
                onClick={() => store.clear(operationKey)}
              >
                {t("issues.edit_request")}
              </Button>
            ) : entry.reviewed ? (
              <Button
                type="button"
                variant="outline"
                onClick={() => store.clear(operationKey)}
              >
                {t("issues.discard_reviewed_operation")}
              </Button>
            ) : (
              <Button
                type="button"
                variant="outline"
                disabled={entry.reviewing}
                onClick={entry.review}
              >
                {t("issues.review_latest")}
              </Button>
            )}
          </div>
          {entry.reviewError && <p role="alert">{t("issues.load_error")}</p>}
        </>
      )}
    </div>
  );
}
export function MutationNotice({
  canRetry,
  mutation,
  onReconcile,
}: {
  canRetry: boolean;
  mutation: {
    isError: boolean;
    isSuccess: boolean;
    conflict: boolean;
    uncertain: boolean;
    validationRejected: boolean;
    editRejected: () => void;
    retrySubmission: () => void;
    review: {
      isSuccess: boolean;
      isPending: boolean;
      isError: boolean;
      mutate: () => void;
    };
    reconcile: () => void;
  };
  onReconcile: () => void;
}) {
  const { t } = useLocale();
  if (mutation.conflict)
    return (
      <div role="alert" className="space-y-3 rounded-lg border p-4 text-sm">
        <p>{t("issues.version_conflict")}</p>
        {!mutation.review.isSuccess ? (
          <Button
            type="button"
            variant="outline"
            disabled={mutation.review.isPending}
            onClick={() => mutation.review.mutate()}
          >
            {t("issues.review_latest")}
          </Button>
        ) : (
          <>
            <p>{t("issues.reconcile_help")}</p>
            <Button
              type="button"
              variant="outline"
              onClick={() => {
                onReconcile();
                mutation.reconcile();
              }}
            >
              {t("issues.discard_draft")}
            </Button>
          </>
        )}
        {mutation.review.isError && <p>{t("issues.load_error")}</p>}
      </div>
    );
  if (mutation.validationRejected)
    return (
      <div role="alert" className="space-y-3">
        <p>{t("issues.validation_error")}</p>
        <Button type="button" variant="outline" onClick={mutation.editRejected}>
          {t("issues.edit_request")}
        </Button>
      </div>
    );
  if (mutation.isError)
    return (
      <div role="alert" className="space-y-3">
        <p>{t("issues.save_error")}</p>
        {mutation.uncertain && !mutation.review.isSuccess && (
          <Button
            type="button"
            variant="outline"
            disabled={mutation.review.isPending}
            onClick={mutation.review.mutate}
          >
            {t("issues.review_latest")}
          </Button>
        )}
        {mutation.review.isError && (
          <p role="alert">{t("issues.load_error")}</p>
        )}
        <Button
          type="button"
          variant="outline"
          disabled={
            !canRetry ||
            mutation.review.isPending ||
            (mutation.uncertain && !mutation.review.isSuccess)
          }
          onClick={mutation.retrySubmission}
        >
          {t("issues.retry_submission")}
        </Button>
      </div>
    );
  return mutation.isSuccess ? (
    <p role="status" className="text-muted-foreground text-sm">
      {t("issues.saved")}
    </p>
  ) : null;
}
