"use client";

import { useRef, useState, type ReactNode } from "react";
import { useMutation } from "@tanstack/react-query";
import Link from "next/link";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { ApiError } from "@/lib/api";
import { useLocale } from "@/providers/locale-provider";
import {
  prepareSubmission,
  type OperationSubmission,
} from "@/features/workspaces/operation-submission";

export function SettingsSection({
  title,
  children,
  id,
}: {
  id?: string;
  title: string;
  children: ReactNode;
}) {
  return (
    <Card id={id}>
      <CardHeader>
        <CardTitle>{title}</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">{children}</CardContent>
    </Card>
  );
}

export function SettingsPage({
  workspaceId,
  title,
  children,
}: {
  workspaceId: string;
  title: string;
  children: ReactNode;
}) {
  const { t } = useLocale();
  return (
    <div className="space-y-8">
      <Button variant="ghost" asChild>
        <Link href={`/w/${workspaceId}`}>{t("teams.back_workspace")}</Link>
      </Button>
      <h1 className="text-3xl font-semibold tracking-tight">{title}</h1>
      {children}
    </div>
  );
}

export function QueryNotice({
  pending,
  error,
  retry,
}: {
  pending: boolean;
  error: boolean;
  retry: () => void;
}) {
  const { t } = useLocale();
  if (pending) return <p role="status">{t("common.loading")}</p>;
  if (error)
    return (
      <div role="alert" className="space-y-3">
        <p>{t("teams.load_error")}</p>
        <Button variant="outline" onClick={retry}>
          {t("teams.retry")}
        </Button>
      </div>
    );
  return null;
}

// A retry keeps the original key and entire wire body. A conflict can only be
// resolved by reviewing fresh authoritative data and starting a new operation.
export function useSettingsMutation<T>({
  run,
  refresh,
  reviewLatest,
  onSaved,
}: {
  run: (input: T, key: string) => Promise<unknown>;
  refresh: () => Promise<unknown>;
  reviewLatest: () => Promise<unknown>;
  onSaved?: () => void;
}) {
  const operation = useRef<OperationSubmission<T> | null>(null);
  const [conflict, setConflict] = useState(false);
  const [reviewed, setReviewed] = useState(false);
  const review = useMutation({
    mutationFn: reviewLatest,
    onSuccess: () => setReviewed(true),
  });
  const mutation = useMutation({
    mutationFn: (submission: OperationSubmission<T>) =>
      run(submission.input, submission.key),
    onError: (error) => {
      if (error instanceof ApiError && error.code === "VERSION_CONFLICT")
        setConflict(true);
    },
    onSuccess: async () => {
      operation.current = null;
      await refresh();
      onSaved?.();
    },
  });
  return {
    ...mutation,
    conflict,
    reviewed,
    review,
    submit(input: T) {
      if (conflict || mutation.isPending) return;
      operation.current = prepareSubmission(
        operation.current,
        input,
        () => input,
      );
      mutation.mutate(operation.current);
    },
    retrySubmission() {
      if (!conflict && !mutation.isPending && operation.current)
        mutation.mutate(operation.current);
    },
    reconcile() {
      if (!reviewed) return;
      operation.current = null;
      setConflict(false);
      setReviewed(false);
      mutation.reset();
      review.reset();
    },
  };
}

export function MutationNotice({
  mutation,
  onReconcile,
}: {
  mutation: {
    retrySubmission: () => void;
    isError: boolean;
    error: Error | null;
    isSuccess: boolean;
    conflict: boolean;
    reviewed: boolean;
    review: { isPending: boolean; isError: boolean; mutate: () => void };
    reconcile: () => void;
  };
  onReconcile: () => void;
}) {
  const { t } = useLocale();
  if (mutation.conflict)
    return (
      <div role="alert" className="space-y-3 rounded-lg border p-4 text-sm">
        <p>{t("teams.version_conflict")}</p>
        {!mutation.reviewed ? (
          <Button
            type="button"
            variant="outline"
            disabled={mutation.review.isPending}
            onClick={() => mutation.review.mutate()}
          >
            {t("teams.review_latest")}
          </Button>
        ) : (
          <>
            <p>{t("teams.reconcile_description")}</p>
            <Button
              type="button"
              variant="outline"
              onClick={() => {
                onReconcile();
                mutation.reconcile();
              }}
            >
              {t("teams.discard_draft")}
            </Button>
          </>
        )}
        {mutation.review.isError && <p>{t("teams.load_error")}</p>}
      </div>
    );
  if (mutation.isError)
    return (
      <div role="alert" className="space-y-2">
        <p className="text-destructive text-sm">{mutation.error?.message}</p>
        <Button
          type="button"
          variant="outline"
          onClick={mutation.retrySubmission}
        >
          {t("teams.retry_submission")}
        </Button>
      </div>
    );
  if (mutation.isSuccess)
    return (
      <p role="status" className="text-muted-foreground text-sm">
        {t("teams.saved")}
      </p>
    );
  return null;
}
