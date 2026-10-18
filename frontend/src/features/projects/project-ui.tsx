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
import { projectKeys } from "./queries";
import type { Project } from "./project.service";
import {
  projectOperationStore as store,
  projectOperationScope,
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
        <p>{t("projects.load_error")}</p>
        <Button type="button" variant="outline" onClick={retry}>
          {t("projects.retry")}
        </Button>
      </div>
    );
  if (pending) return <p role="status">{t("common.loading")}</p>;
  return null;
}
export function useProjectMutation<
  T,
  R extends { project?: Project; project_version?: number },
>({
  workspaceId,
  projectId,
  operationId,
  operationLabel,
  run,
  reviewLatest,
  onSaved,
}: {
  workspaceId: string;
  projectId?: string;
  operationId: string;
  operationLabel: string;
  run: (input: T, key: string) => Promise<R>;
  reviewLatest: () => Promise<unknown>;
  onSaved?: (result: R) => void;
}) {
  const cache = useQueryClient();
  const { user } = useAuth();
  const scope = projectOperationScope(user?.uuid, workspaceId, projectId);
  const operationKey = JSON.stringify([scope, operationId]);
  const entries = useSyncExternalStore(
    store.subscribe,
    store.snapshot,
    store.serverSnapshot,
  );
  const entry = entries.get(operationKey);
  const mutation = useMutation({
    mutationFn: (submission: OperationSubmission<T>) =>
      run(submission.input, submission.key),
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
          queryKey: projectKeys.all(workspaceId),
        });
    },
    onSuccess: async (result) => {
      if (projectId)
        cache.setQueryData(
          [...projectKeys.all(workspaceId), user?.uuid, projectId, "detail"],
          (current: { project: Project } | undefined) =>
            current
              ? {
                  ...current,
                  project: result.project ?? {
                    ...current.project,
                    version: result.project_version ?? current.project.version,
                  },
                }
              : current,
        );
      await cache.invalidateQueries({ queryKey: projectKeys.all(workspaceId) });
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
    void reviewLatest().then(
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
    locked: !!entry,
    review: {
      isSuccess: entry?.reviewed ?? false,
      isPending: entry?.reviewing ?? false,
      isError: entry?.reviewError ?? false,
      mutate: review,
    },
    submit(input: T) {
      if (store.get(operationKey) || !user?.uuid) return;
      const submission = prepareSubmission<T>(null, input, () => input);
      const retry = () => {
        const current = store.get(operationKey);
        if (
          !current ||
          current.pending ||
          current.conflict ||
          current.reviewing
        )
          return;
        store.patch(operationKey, {
          pending: true,
          reviewed: false,
          reviewError: false,
        });
        mutation.mutate(current.submission as OperationSubmission<T>);
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
  projectId,
  enabled,
}: {
  workspaceId: string;
  projectId?: string;
  enabled: boolean;
}) {
  const { user } = useAuth();
  const { t } = useLocale();
  const entries = useSyncExternalStore(
    store.subscribe,
    store.snapshot,
    store.serverSnapshot,
  );
  const scope = projectOperationScope(user?.uuid, workspaceId, projectId);
  const pending = [...entries].filter(([, entry]) => entry.scope === scope);
  if (!pending.length) return null;
  return (
    <Section title={t("projects.pending_operations")}>
      <p className="text-muted-foreground text-sm">
        {t("projects.pending_help")}
      </p>
      {pending.map(([key, entry]) => (
        <div key={key} className="space-y-3 rounded-lg border p-3">
          <p className="text-sm font-medium">{entry.label}</p>
          {entry.pending ? (
            <p role="status">{t("common.loading")}</p>
          ) : (
            <>
              <p role="alert" className="text-sm">
                {t(
                  entry.validationRejected
                    ? "projects.validation_error"
                    : entry.conflict
                      ? "projects.version_conflict"
                      : "projects.save_error",
                )}
              </p>
              <div className="flex flex-wrap gap-3">
                {!entry.conflict && !entry.validationRejected && (
                  <Button
                    type="button"
                    variant="outline"
                    disabled={!enabled || entry.reviewing}
                    onClick={entry.retry}
                  >
                    {t("projects.retry_submission")}
                  </Button>
                )}
                {entry.validationRejected ? (
                  <Button
                    type="button"
                    variant="outline"
                    onClick={() => store.clear(key)}
                  >
                    {t("projects.edit_request")}
                  </Button>
                ) : entry.reviewed ? (
                  <Button
                    type="button"
                    variant="outline"
                    onClick={() => store.clear(key)}
                  >
                    {t("projects.discard_reviewed_operation")}
                  </Button>
                ) : (
                  <Button
                    type="button"
                    variant="outline"
                    disabled={entry.reviewing}
                    onClick={entry.review}
                  >
                    {t("projects.review_latest")}
                  </Button>
                )}
              </div>
              {entry.reviewError && (
                <p role="alert" className="text-sm">
                  {t("projects.load_error")}
                </p>
              )}
            </>
          )}
        </div>
      ))}
    </Section>
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
        <p>{t("projects.version_conflict")}</p>
        {!mutation.review.isSuccess ? (
          <Button
            type="button"
            variant="outline"
            disabled={mutation.review.isPending}
            onClick={() => mutation.review.mutate()}
          >
            {t("projects.review_latest")}
          </Button>
        ) : (
          <>
            <p>{t("projects.reconcile_help")}</p>
            <Button
              type="button"
              variant="outline"
              onClick={() => {
                onReconcile();
                mutation.reconcile();
              }}
            >
              {t("projects.discard_draft")}
            </Button>
          </>
        )}
        {mutation.review.isError && <p>{t("projects.load_error")}</p>}
      </div>
    );
  if (mutation.validationRejected)
    return (
      <div role="alert" className="space-y-3">
        <p>{t("projects.validation_error")}</p>
        <Button type="button" variant="outline" onClick={mutation.editRejected}>
          {t("projects.edit_request")}
        </Button>
      </div>
    );
  if (mutation.isError)
    return (
      <div role="alert" className="space-y-3">
        <p>{t("projects.save_error")}</p>
        <Button
          type="button"
          variant="outline"
          disabled={!canRetry || mutation.review.isPending}
          onClick={mutation.retrySubmission}
        >
          {t("projects.retry_submission")}
        </Button>
      </div>
    );
  return mutation.isSuccess ? (
    <p role="status" className="text-muted-foreground text-sm">
      {t("projects.saved")}
    </p>
  ) : null;
}
export function safeResourceUrl(value: string): string | null {
  try {
    const url = new URL(value);
    return (url.protocol === "http:" || url.protocol === "https:") &&
      !url.username &&
      !url.password
      ? url.href
      : null;
  } catch {
    return null;
  }
}
export function validDates(start: string, target: string) {
  return !start || !target || start <= target;
}
