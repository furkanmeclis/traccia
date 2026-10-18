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
import { productKeys } from "./queries";
import { safeCatalogUrl } from "./snapshot";
import { executeProductCommand, type ProductCommand } from "./product.service";
import {
  productOperationStore as store,
  productOperationScope,
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
        <p>{t("products.load_error")}</p>
        <Button type="button" variant="outline" onClick={retry}>
          {t("products.retry")}
        </Button>
      </div>
    );
  if (pending) return <p role="status">{t("common.loading")}</p>;
  return null;
}
export function useProductMutation({
  workspaceId,
  productId,
  operationId,
  operationLabel,
  reviewLatest,
  onSaved,
}: {
  workspaceId: string;
  productId?: string;
  operationId: string;
  operationLabel: string;
  reviewLatest: () => Promise<unknown>;
  onSaved?: (result: Awaited<ReturnType<typeof executeProductCommand>>) => void;
}) {
  const cache = useQueryClient();
  const { user } = useAuth();
  const scope = productOperationScope(user?.uuid, workspaceId, productId);
  const operationKey = JSON.stringify([scope, operationId]);
  const entries = useSyncExternalStore(
    store.subscribe,
    store.snapshot,
    store.serverSnapshot,
  );
  const entry = entries.get(operationKey);
  const mutation = useMutation({
    mutationFn: (submission: OperationSubmission<ProductCommand>) =>
      executeProductCommand(submission.input, submission.key),
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
          queryKey: productKeys.all(workspaceId),
        });
    },
    onSuccess: async (result) => {
      if (productId)
        cache.setQueryData(
          productKeys.detail(workspaceId, user?.uuid, productId),
          (current: { product: { version: number } } | undefined) =>
            current
              ? {
                  ...current,
                  product:
                    "product" in result
                      ? result.product
                      : { ...current.product, version: result.product_version },
                }
              : current,
        );
      await cache.invalidateQueries({ queryKey: productKeys.all(workspaceId) });
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
    submit(input: ProductCommand) {
      if (store.get(operationKey) || !user?.uuid) return;
      const submission = prepareSubmission<ProductCommand>(
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
          current.reviewing
        )
          return;
        store.patch(operationKey, {
          pending: true,
          reviewed: false,
          reviewError: false,
        });
        mutation.mutate(
          current.submission as OperationSubmission<ProductCommand>,
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
  productId,
  canRun,
}: {
  workspaceId: string;
  productId?: string;
  canRun: (command: ProductCommand) => boolean;
}) {
  const { user } = useAuth();
  const { t } = useLocale();
  const entries = useSyncExternalStore(
    store.subscribe,
    store.snapshot,
    store.serverSnapshot,
  );
  const scope = productOperationScope(user?.uuid, workspaceId, productId);
  const pending = [...entries].filter(([, entry]) => entry.scope === scope);
  if (!pending.length) return null;
  return (
    <Section title={t("products.pending_operations")}>
      <p className="text-muted-foreground text-sm">
        {t("products.pending_help")}
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
                    ? "products.validation_error"
                    : entry.conflict
                      ? "products.version_conflict"
                      : "products.save_error",
                )}
              </p>
              <div className="flex flex-wrap gap-3">
                {!entry.conflict && !entry.validationRejected && (
                  <Button
                    type="button"
                    variant="outline"
                    disabled={
                      !canRun(entry.submission.input as ProductCommand) ||
                      entry.reviewing
                    }
                    onClick={entry.retry}
                  >
                    {t("products.retry_submission")}
                  </Button>
                )}
                {entry.validationRejected ? (
                  <Button
                    type="button"
                    variant="outline"
                    onClick={() => store.clear(key)}
                  >
                    {t("products.edit_request")}
                  </Button>
                ) : entry.reviewed ? (
                  <Button
                    type="button"
                    variant="outline"
                    onClick={() => store.clear(key)}
                  >
                    {t("products.discard_reviewed_operation")}
                  </Button>
                ) : (
                  <Button
                    type="button"
                    variant="outline"
                    disabled={entry.reviewing}
                    onClick={entry.review}
                  >
                    {t("products.review_latest")}
                  </Button>
                )}
              </div>
              {entry.reviewError && (
                <p role="alert" className="text-sm">
                  {t("products.load_error")}
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
        <p>{t("products.version_conflict")}</p>
        {!mutation.review.isSuccess ? (
          <Button
            type="button"
            variant="outline"
            disabled={mutation.review.isPending}
            onClick={() => mutation.review.mutate()}
          >
            {t("products.review_latest")}
          </Button>
        ) : (
          <>
            <p>{t("products.reconcile_help")}</p>
            <Button
              type="button"
              variant="outline"
              onClick={() => {
                onReconcile();
                mutation.reconcile();
              }}
            >
              {t("products.discard_draft")}
            </Button>
          </>
        )}
        {mutation.review.isError && <p>{t("products.load_error")}</p>}
      </div>
    );
  if (mutation.validationRejected)
    return (
      <div role="alert" className="space-y-3">
        <p>{t("products.validation_error")}</p>
        <Button type="button" variant="outline" onClick={mutation.editRejected}>
          {t("products.edit_request")}
        </Button>
      </div>
    );
  if (mutation.isError)
    return (
      <div role="alert" className="space-y-3">
        <p>{t("products.save_error")}</p>
        <Button
          type="button"
          variant="outline"
          disabled={!canRetry || mutation.review.isPending}
          onClick={mutation.retrySubmission}
        >
          {t("products.retry_submission")}
        </Button>
      </div>
    );
  return mutation.isSuccess ? (
    <p role="status" className="text-muted-foreground text-sm">
      {t("products.saved")}
    </p>
  ) : null;
}
export { safeCatalogUrl } from "./snapshot";
export function PublicLink({ value }: { value: string }) {
  const { t } = useLocale();
  const href = safeCatalogUrl(value);
  return href ? (
    <a
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      className="text-primary block text-sm break-all underline underline-offset-4"
    >
      {value}
      <span className="sr-only"> {t("products.opens_new_tab")}</span>
    </a>
  ) : null;
}
