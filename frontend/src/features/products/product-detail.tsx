"use client";
import { useState } from "react";
import Link from "next/link";
import { Button } from "@/components/ui/button";
import { useWorkspace } from "@/features/workspaces/queries";
import { useLocale } from "@/providers/locale-provider";
import { useProduct, useProductChildren } from "./queries";
import type { Product, ProductCommand } from "./product.service";
import { ProductForm } from "./product-form";
import { ProjectLinks } from "./project-links";
import { EnvironmentSettings } from "./environment-settings";
import {
  Section,
  LoadNotice,
  PendingOperations,
  MutationNotice,
  PublicLink,
  useProductMutation,
} from "./product-ui";
function ProductSettings({
  workspaceId,
  product,
  manage,
  restore,
  reviewLatest,
}: {
  workspaceId: string;
  product: Product;
  manage: boolean;
  restore: boolean;
  reviewLatest: () => Promise<unknown>;
}) {
  const { t } = useLocale();
  const [editing, setEditing] = useState(false);
  const lifecycle = useProductMutation({
    workspaceId,
    productId: product.id,
    operationId: "product-lifecycle",
    operationLabel: t(
      product.archived
        ? "products.restore_product"
        : "products.archive_product",
    ),
    reviewLatest,
  });
  return (
    <Section title={t("products.settings")}>
      {editing ? (
        <ProductForm
          workspaceId={workspaceId}
          product={product}
          canManage={manage}
          reviewLatest={reviewLatest}
          onDone={() => setEditing(false)}
        />
      ) : (
        <div className="flex flex-wrap gap-3">
          {manage && (
            <Button
              type="button"
              variant="outline"
              disabled={lifecycle.locked}
              onClick={() => setEditing(true)}
            >
              {t("products.edit_product")}
            </Button>
          )}
          {((manage && !product.archived) || (restore && product.archived)) && (
            <Button
              type="button"
              variant="outline"
              disabled={lifecycle.locked}
              onClick={() =>
                lifecycle.submit({
                  action: product.archived
                    ? "product.restore"
                    : "product.archive",
                  path: { workspaceID: workspaceId, productID: product.id },
                  body: { expected_version: product.version },
                })
              }
            >
              {t(
                product.archived
                  ? "products.restore_product"
                  : "products.archive_product",
              )}
            </Button>
          )}
        </div>
      )}
      {product.archived && (
        <p className="text-muted-foreground text-sm">
          {t("products.restore_help")}
        </p>
      )}
      <MutationNotice
        canRetry={manage || restore}
        mutation={lifecycle}
        onReconcile={() => setEditing(false)}
      />
    </Section>
  );
}
export function ProductDetail({
  workspaceId,
  productId,
}: {
  workspaceId: string;
  productId: string;
}) {
  const { t } = useLocale();
  const workspace = useWorkspace(workspaceId);
  const allowed = workspace.isSuccess && !workspace.isError;
  const product = useProduct(workspaceId, productId, allowed);
  const ready =
    allowed &&
    !workspace.isFetching &&
    product.isSuccess &&
    !product.isError &&
    !product.isFetching;
  const children = useProductChildren(workspaceId, productId, ready);
  const childrenReady =
    ready &&
    children.isSuccess &&
    !children.isError &&
    !children.isFetching &&
    children.data.productVersion === product.data.product.version;
  async function reviewLatest() {
    const [latestProduct, latestChildren] = await Promise.all([
      product.refetch({ throwOnError: true }),
      children.refetch({ throwOnError: true }),
      workspace.refetch({ throwOnError: true }),
    ]);
    if (
      !latestProduct.data ||
      !latestChildren.data ||
      latestProduct.data.product.version !== latestChildren.data.productVersion
    )
      throw new Error("PRODUCT_SNAPSHOT_CHANGED");
  }
  const current = product.data?.product;
  const manage = !!(
    childrenReady &&
    product.data.capabilities.manage &&
    !current?.archived
  );
  const restore = !!(childrenReady && product.data.capabilities.restore);
  const environmentManage = !!(
    manage && children.data?.environments.capabilities.manage
  );
  const environmentRestore = !!(
    childrenReady &&
    !current?.archived &&
    children.data?.environments.archivedCapabilities.restore
  );
  function canRun(command: ProductCommand) {
    if (command.action === "product.restore") return restore;
    if (command.action.startsWith("product.")) return manage;
    if (command.action === "environment.restore") return environmentRestore;
    if (command.action.startsWith("environment.")) return environmentManage;
    return manage && !!children.data?.projectsManage;
  }
  return (
    <div className="space-y-8">
      <Button type="button" variant="ghost" asChild>
        <Link href={`/w/${workspaceId}/products`}>
          {t("products.back_products")}
        </Link>
      </Button>
      <LoadNotice
        pending={workspace.isPending}
        error={workspace.isError}
        retry={() => workspace.refetch()}
      />
      <PendingOperations
        workspaceId={workspaceId}
        productId={productId}
        canRun={canRun}
      />
      <LoadNotice
        pending={product.isPending}
        error={product.isError}
        retry={() => product.refetch()}
      />
      {current && product.data && (
        <div hidden={!allowed || product.isError} className="space-y-8">
          <header className="space-y-3">
            <p className="text-primary text-sm font-medium">
              {t("products.product")}
            </p>
            <h1 className="text-3xl font-semibold tracking-tight break-words">
              {current.name}
            </h1>
            <p className="text-muted-foreground break-words whitespace-pre-wrap">
              {current.summary}
            </p>
            {current.archived && <p>{t("products.archived")}</p>}
            {current.repository_url && (
              <div>
                <p className="text-sm font-medium">
                  {t("products.repository_url")}
                </p>
                <PublicLink value={current.repository_url} />
              </div>
            )}
            {(current.repository_provider || current.repository_reference) && (
              <p className="text-muted-foreground text-sm break-words">
                {current.repository_provider}
                {current.repository_provider && current.repository_reference
                  ? " · "
                  : ""}
                {current.repository_reference}
              </p>
            )}
          </header>
          <ProductSettings
            workspaceId={workspaceId}
            product={current}
            manage={manage}
            restore={restore}
            reviewLatest={reviewLatest}
          />
          <LoadNotice
            pending={children.isPending}
            error={children.isError}
            retry={() => {
              void reviewLatest().catch(() => {});
            }}
          />
          {children.data && !childrenReady && !children.isError && (
            <div role="status" className="space-y-3">
              <p>{t("products.snapshot_changed")}</p>
              <Button
                type="button"
                variant="outline"
                disabled={product.isFetching || children.isFetching}
                onClick={() => {
                  void reviewLatest().catch(() => {});
                }}
              >
                {t("products.retry")}
              </Button>
            </div>
          )}
          {children.data && (
            <div hidden={children.isError} className="space-y-8">
              <ProjectLinks
                workspaceId={workspaceId}
                productId={productId}
                projects={children.data.projects}
                historicalProjects={children.data.historicalProjects}
                version={current.version}
                canManage={manage && children.data.projectsManage}
                reviewLatest={reviewLatest}
              />
              <div id="environments">
                <EnvironmentSettings
                  workspaceId={workspaceId}
                  productId={productId}
                  version={current.version}
                  items={children.data.environments.items}
                  canManage={environmentManage}
                  canRestore={environmentRestore}
                  showCreate={
                    !!children.data.environments.capabilities.manage &&
                    !current.archived
                  }
                  reviewLatest={reviewLatest}
                />
              </div>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
