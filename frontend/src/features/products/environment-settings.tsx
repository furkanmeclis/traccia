"use client";
import { useId, useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Label } from "@/components/ui/label";
import { useLocale } from "@/providers/locale-provider";
import type { Environment } from "./product.service";
import {
  Section,
  MutationNotice,
  PublicLink,
  useProductMutation,
} from "./product-ui";
import { safeCatalogUrl, withinLimit } from "./snapshot";
type Context = {
  workspaceId: string;
  productId: string;
  version: number;
  canManage: boolean;
  canRestore: boolean;
  showCreate: boolean;
  reviewLatest: () => Promise<unknown>;
};
function useKinds() {
  const { t } = useLocale();
  return [
    { value: "production" as const, label: t("products.kind_production") },
    { value: "staging" as const, label: t("products.kind_staging") },
    { value: "local" as const, label: t("products.kind_local") },
    { value: "custom" as const, label: t("products.kind_custom") },
  ];
}
function EnvironmentForm({
  context,
  item,
  onDone,
}: {
  context: Context;
  item?: Environment;
  onDone?: () => void;
}) {
  const { t } = useLocale();
  const id = useId();
  const kinds = useKinds();
  const empty = {
    name: "",
    kind: "production" as Environment["kind"],
    url: "",
    deploy_url: "",
    deploy_provider: "",
    deploy_reference: "",
    summary: "",
  };
  const [draft, setDraft] = useState(
    item
      ? {
          name: item.name,
          kind: item.kind,
          url: item.url,
          deploy_url: item.deploy_url,
          deploy_provider: item.deploy_provider,
          deploy_reference: item.deploy_reference,
          summary: item.summary,
        }
      : empty,
  );
  const [baseline] = useState({ product: context.version, row: item?.version });
  const stale =
    !!item &&
    (baseline.product !== context.version || baseline.row !== item.version);
  const ready = context.canManage && !stale;
  const reset = () => {
    setDraft(empty);
    onDone?.();
  };
  const mutation = useProductMutation({
    workspaceId: context.workspaceId,
    productId: context.productId,
    operationId: `environment-edit:${item?.id ?? "create"}`,
    operationLabel: t(
      item ? "products.edit_environment" : "products.create_environment",
    ),
    reviewLatest: context.reviewLatest,
    onSaved: reset,
  });
  const fields = [
    { key: "name" as const, label: t("products.environment_name"), limit: 100 },
    { key: "url" as const, label: t("products.environment_url"), limit: 2048 },
    { key: "summary" as const, label: t("products.summary"), limit: 2000 },
    {
      key: "deploy_url" as const,
      label: t("products.deploy_url"),
      limit: 2048,
    },
    {
      key: "deploy_provider" as const,
      label: t("products.deploy_provider"),
      limit: 100,
    },
    {
      key: "deploy_reference" as const,
      label: t("products.deploy_reference"),
      limit: 200,
    },
  ];
  const valid =
    !!draft.name.trim() &&
    !!safeCatalogUrl(draft.url.trim()) &&
    (!draft.deploy_url.trim() || !!safeCatalogUrl(draft.deploy_url.trim())) &&
    fields.every(({ key, limit }) => withinLimit(draft[key].trim(), limit));
  return (
    <form
      className="space-y-4"
      onSubmit={(event) => {
        event.preventDefault();
        if (!ready || !valid) return;
        const body = {
          name: draft.name.trim(),
          kind: draft.kind,
          url: draft.url.trim(),
          summary: draft.summary.trim(),
          deploy_url: draft.deploy_url.trim(),
          deploy_provider: draft.deploy_provider.trim(),
          deploy_reference: draft.deploy_reference.trim(),
          expected_product_version: item ? baseline.product : context.version,
        };
        mutation.submit(
          item
            ? {
                action: "environment.update",
                path: {
                  workspaceID: context.workspaceId,
                  productID: context.productId,
                  environmentID: item.id,
                },
                body: { ...body, expected_version: baseline.row! },
              }
            : {
                action: "environment.create",
                path: {
                  workspaceID: context.workspaceId,
                  productID: context.productId,
                },
                body,
              },
        );
      }}
    >
      <fieldset disabled={!ready || mutation.locked} className="space-y-4">
        <div className="space-y-2">
          <Label htmlFor={`${id}-kind`}>{t("products.environment_kind")}</Label>
          <select
            id={`${id}-kind`}
            className="border-input bg-background h-10 w-full rounded-md border px-3 text-sm"
            value={draft.kind}
            onChange={(event) =>
              setDraft({
                ...draft,
                kind: event.target.value as Environment["kind"],
              })
            }
          >
            {kinds.map((kind) => (
              <option key={kind.value} value={kind.value}>
                {kind.label}
              </option>
            ))}
          </select>
        </div>
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
                required={key === "name" || key === "url"}
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
          {t("products.deploy_help")}
        </p>
        {((draft.url.trim() && !safeCatalogUrl(draft.url.trim())) ||
          (draft.deploy_url.trim() &&
            !safeCatalogUrl(draft.deploy_url.trim()))) && (
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
        <Button type="submit" disabled={!ready || !valid || mutation.locked}>
          {t(item ? "products.save" : "products.create_environment")}
        </Button>
        {item && (
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
        canRetry={context.canManage}
        mutation={mutation}
        onReconcile={reset}
      />
    </form>
  );
}
function EnvironmentRow({
  item,
  context,
  visible,
}: {
  item: Environment;
  context: Context;
  visible: boolean;
}) {
  const { t } = useLocale();
  const kinds = useKinds();
  const [editing, setEditing] = useState(false);
  const lifecycle = useProductMutation({
    workspaceId: context.workspaceId,
    productId: context.productId,
    operationId: `environment-lifecycle:${item.id}`,
    operationLabel: t(
      item.archived
        ? "products.restore_environment"
        : "products.archive_environment",
    ),
    reviewLatest: context.reviewLatest,
  });
  return (
    <article hidden={!visible} className="space-y-4 rounded-lg border p-4">
      <div className="space-y-2">
        <p className="text-muted-foreground text-xs">
          {kinds.find((kind) => kind.value === item.kind)?.label}
        </p>
        <h3 className="font-medium break-words">{item.name}</h3>
        <PublicLink value={item.url} />
        <p className="text-muted-foreground text-sm break-words whitespace-pre-wrap">
          {item.summary}
        </p>
        {item.deploy_url && (
          <div>
            <p className="text-sm font-medium">{t("products.deploy_url")}</p>
            <PublicLink value={item.deploy_url} />
          </div>
        )}
        {(item.deploy_provider || item.deploy_reference) && (
          <p className="text-muted-foreground text-sm break-words">
            {item.deploy_provider}
            {item.deploy_provider && item.deploy_reference ? " · " : ""}
            {item.deploy_reference}
          </p>
        )}
        {item.archived && (
          <p className="text-muted-foreground text-sm">
            {t("products.archived")}
          </p>
        )}
      </div>
      {editing ? (
        <EnvironmentForm
          context={{
            ...context,
            canManage: context.canManage && !item.archived,
          }}
          item={item}
          onDone={() => setEditing(false)}
        />
      ) : (
        <div className="flex flex-wrap gap-3">
          {context.canManage && !item.archived && (
            <Button
              type="button"
              variant="outline"
              disabled={lifecycle.locked}
              onClick={() => setEditing(true)}
            >
              {t("products.edit")}
            </Button>
          )}
          {((context.canManage && !item.archived) ||
            (context.canRestore && item.archived)) && (
            <Button
              type="button"
              variant="outline"
              disabled={lifecycle.locked}
              onClick={() =>
                lifecycle.submit({
                  action: item.archived
                    ? "environment.restore"
                    : "environment.archive",
                  path: {
                    workspaceID: context.workspaceId,
                    productID: context.productId,
                    environmentID: item.id,
                  },
                  body: {
                    expected_version: item.version,
                    expected_product_version: context.version,
                  },
                })
              }
            >
              {t(item.archived ? "products.restore" : "products.archive")}
            </Button>
          )}
        </div>
      )}
      <MutationNotice
        canRetry={item.archived ? context.canRestore : context.canManage}
        mutation={lifecycle}
        onReconcile={() => setEditing(false)}
      />
    </article>
  );
}
export function EnvironmentSettings({
  items,
  ...context
}: Context & { items: Environment[] }) {
  const { t } = useLocale();
  const id = useId();
  const kinds = useKinds();
  const [filters, setFilters] = useState({ archived: false, q: "", kind: "" });
  const matches = (item: Environment) =>
    item.archived === filters.archived &&
    (!filters.kind || item.kind === filters.kind) &&
    (!filters.q.trim() ||
      [item.name, item.summary, item.url].some((value) =>
        value
          .toLocaleLowerCase()
          .includes(filters.q.trim().toLocaleLowerCase()),
      ));
  return (
    <Section title={t("products.environments")}>
      <p className="text-muted-foreground text-sm">
        {t("products.environments_help")}
      </p>
      <div className="flex flex-wrap gap-3">
        <Button
          type="button"
          variant={filters.archived ? "outline" : "default"}
          aria-pressed={!filters.archived}
          onClick={() => setFilters({ ...filters, archived: false })}
        >
          {t("products.active")}
        </Button>
        <Button
          type="button"
          variant={filters.archived ? "default" : "outline"}
          aria-pressed={filters.archived}
          onClick={() => setFilters({ ...filters, archived: true })}
        >
          {t("products.archived")}
        </Button>
      </div>
      <div className="grid gap-4 sm:grid-cols-2">
        <div className="space-y-2">
          <Label htmlFor={`${id}-search`}>
            {t("products.search_environments")}
          </Label>
          <Input
            id={`${id}-search`}
            value={filters.q}
            onChange={(event) =>
              setFilters({ ...filters, q: event.target.value })
            }
          />
        </div>
        <div className="space-y-2">
          <Label htmlFor={`${id}-kind-filter`}>
            {t("products.environment_kind")}
          </Label>
          <select
            id={`${id}-kind-filter`}
            className="border-input bg-background h-10 w-full rounded-md border px-3 text-sm"
            value={filters.kind}
            onChange={(event) =>
              setFilters({ ...filters, kind: event.target.value })
            }
          >
            <option value="">{t("products.all_kinds")}</option>
            {kinds.map((kind) => (
              <option key={kind.value} value={kind.value}>
                {kind.label}
              </option>
            ))}
          </select>
        </div>
      </div>
      {!items.some(matches) && (
        <p className="text-muted-foreground text-sm">
          {t("products.no_environments")}
        </p>
      )}
      {items.map((item) => (
        <EnvironmentRow
          key={item.id}
          item={item}
          context={context}
          visible={matches(item)}
        />
      ))}
      <div hidden={!context.showCreate} className="border-t pt-4">
        <h3 className="mb-4 font-medium">{t("products.create_environment")}</h3>
        <EnvironmentForm context={context} />
      </div>
    </Section>
  );
}
