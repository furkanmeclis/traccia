"use client";
import { useId, useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Label } from "@/components/ui/label";
import { useLocale } from "@/providers/locale-provider";
import type { Product } from "./product.service";
import { MutationNotice, useProductMutation } from "./product-ui";
import { safeCatalogUrl, withinLimit } from "./snapshot";
export function ProductForm({
  workspaceId,
  product,
  canManage,
  reviewLatest,
  onDone,
}: {
  workspaceId: string;
  product?: Product;
  canManage: boolean;
  reviewLatest: () => Promise<unknown>;
  onDone?: () => void;
}) {
  const { t } = useLocale();
  const id = useId();
  const empty = {
    name: "",
    summary: "",
    repository_url: "",
    repository_provider: "",
    repository_reference: "",
  };
  const [draft, setDraft] = useState(
    product
      ? {
          name: product.name,
          summary: product.summary,
          repository_url: product.repository_url,
          repository_provider: product.repository_provider,
          repository_reference: product.repository_reference,
        }
      : empty,
  );
  const [baseline] = useState(product?.version);
  const reset = () => {
    setDraft(empty);
    onDone?.();
  };
  const mutation = useProductMutation({
    workspaceId,
    productId: product?.id,
    operationId: product ? "product-edit" : "product-create",
    operationLabel: t(product ? "products.edit_product" : "products.create"),
    reviewLatest,
    onSaved: reset,
  });
  const fields = [
    { key: "name" as const, label: t("products.name"), limit: 100 },
    { key: "summary" as const, label: t("products.summary"), limit: 2000 },
    {
      key: "repository_url" as const,
      label: t("products.repository_url"),
      limit: 2048,
    },
    {
      key: "repository_provider" as const,
      label: t("products.repository_provider"),
      limit: 100,
    },
    {
      key: "repository_reference" as const,
      label: t("products.repository_reference"),
      limit: 200,
    },
  ];
  const stale = !!product && product.version !== baseline;
  const ready = canManage && !stale;
  const valid =
    !!draft.name.trim() &&
    fields.every(({ key, limit }) => withinLimit(draft[key].trim(), limit)) &&
    (!draft.repository_url.trim() ||
      !!safeCatalogUrl(draft.repository_url.trim()));
  return (
    <form
      className="space-y-4"
      onSubmit={(event) => {
        event.preventDefault();
        if (!valid || !ready) return;
        const body = {
          name: draft.name.trim(),
          summary: draft.summary.trim(),
          repository_url: draft.repository_url.trim(),
          repository_provider: draft.repository_provider.trim(),
          repository_reference: draft.repository_reference.trim(),
        };
        mutation.submit(
          product
            ? {
                action: "product.update",
                path: { workspaceID: workspaceId, productID: product.id },
                body: { ...body, expected_version: baseline! },
              }
            : {
                action: "product.create",
                path: { workspaceID: workspaceId },
                body,
              },
        );
      }}
    >
      <fieldset disabled={!ready || mutation.locked} className="space-y-4">
        {fields.map(({ key, label, limit }) => (
          <div key={key} className="space-y-2">
            <Label htmlFor={`${id}-${key}`}>{label}</Label>
            {key === "summary" ? (
              <Textarea
                id={`${id}-${key}`}
                value={draft[key]}
                onChange={(event) =>
                  setDraft({ ...draft, [key]: event.target.value })
                }
              />
            ) : (
              <Input
                id={`${id}-${key}`}
                required={key === "name"}
                value={draft[key]}
                onChange={(event) =>
                  setDraft({ ...draft, [key]: event.target.value })
                }
              />
            )}
            {!withinLimit(draft[key].trim(), limit) && (
              <p role="alert" className="text-destructive text-sm">
                {t("products.too_long", { limit })}
              </p>
            )}
          </div>
        ))}
        <p className="text-muted-foreground text-sm">
          {t("products.repository_help")}
        </p>
        {draft.repository_url.trim() &&
          !safeCatalogUrl(draft.repository_url.trim()) && (
            <p role="alert" className="text-destructive text-sm">
              {t("products.url_error")}
            </p>
          )}
      </fieldset>
      {stale && (
        <p role="alert" className="text-sm">
          {t("products.stale_draft")}
        </p>
      )}
      <div className="flex flex-wrap gap-3">
        <Button type="submit" disabled={!valid || !ready || mutation.locked}>
          {t(product ? "products.save" : "products.create")}
        </Button>
        {product && (
          <Button
            type="button"
            variant="outline"
            disabled={mutation.locked}
            onClick={onDone}
          >
            {t(stale ? "products.discard_draft" : "products.cancel")}
          </Button>
        )}
      </div>
      <MutationNotice
        canRetry={canManage}
        mutation={mutation}
        onReconcile={reset}
      />
    </form>
  );
}
