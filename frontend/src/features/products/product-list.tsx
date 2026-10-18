"use client";
import { useId, useState } from "react";
import Link from "next/link";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useWorkspace } from "@/features/workspaces/queries";
import { useLocale } from "@/providers/locale-provider";
import { useProducts, useProductChoices } from "./queries";
import { ProductForm } from "./product-form";
import { Section, LoadNotice, PendingOperations } from "./product-ui";
export function ProductList({ workspaceId }: { workspaceId: string }) {
  const { t } = useLocale();
  const id = useId();
  const workspace = useWorkspace(workspaceId);
  const allowed = workspace.isSuccess && !workspace.isError;
  const [filters, setFilters] = useState({
    archived: false,
    q: "",
    project_id: "",
  });
  const list = useProducts(workspaceId, filters, allowed);
  const choices = useProductChoices(workspaceId, allowed, true);
  const ready =
    allowed &&
    !workspace.isFetching &&
    list.isSuccess &&
    !list.isError &&
    !list.isFetching;
  const manage =
    ready && list.data.pages.every((page) => page.capabilities.manage);
  const showCreate =
    list.data?.pages.some((page) => page.capabilities.manage) || false;
  async function reviewLatest() {
    await Promise.all([
      workspace.refetch({ throwOnError: true }),
      list.refetch({ throwOnError: true }),
    ]);
  }
  return (
    <div className="space-y-8">
      <Button type="button" variant="ghost" asChild>
        <Link href={`/w/${workspaceId}`}>{t("products.back_workspace")}</Link>
      </Button>
      <h1 className="text-3xl font-semibold">{t("products.title")}</h1>
      <LoadNotice
        pending={workspace.isPending}
        error={workspace.isError}
        retry={() => workspace.refetch()}
      />
      <PendingOperations workspaceId={workspaceId} canRun={() => !!manage} />
      <div hidden={!allowed} className="space-y-8">
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
              {t("products.search_products")}
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
            <Label htmlFor={`${id}-project`}>
              {t("products.filter_project")}
            </Label>
            <select
              id={`${id}-project`}
              disabled={
                !choices.isSuccess || choices.isError || choices.isFetching
              }
              className="border-input bg-background h-10 w-full rounded-md border px-3 text-sm"
              value={filters.project_id}
              onChange={(event) =>
                setFilters({ ...filters, project_id: event.target.value })
              }
            >
              <option value="">{t("products.all_projects")}</option>
              {choices.data?.items.map((project) => (
                <option key={project.id} value={project.id}>
                  {project.name}
                  {project.archived ? ` · ${t("products.archived")}` : ""}
                </option>
              ))}
            </select>
          </div>
        </div>
        <LoadNotice
          pending={choices.isPending}
          error={choices.isError}
          retry={() => choices.refetch()}
        />
        <LoadNotice
          pending={list.isPending}
          error={list.isError}
          retry={() => list.refetch()}
        />
        {list.isSuccess && !list.isError && (
          <>
            <div className="grid gap-4 sm:grid-cols-2">
              {list.data.pages
                .flatMap((page) => page.items ?? [])
                .map((product) => (
                  <Link
                    key={product.id}
                    href={`/w/${workspaceId}/products/${product.id}`}
                    className="hover:border-primary focus-visible:ring-ring space-y-2 rounded-lg border p-5 focus-visible:ring-2"
                  >
                    <h2 className="font-medium break-words">{product.name}</h2>
                    <p className="text-muted-foreground text-sm break-words whitespace-pre-wrap">
                      {product.summary}
                    </p>
                    {product.archived && (
                      <p className="text-muted-foreground text-sm">
                        {t("products.archived")}
                      </p>
                    )}
                  </Link>
                ))}
            </div>
            {list.data.pages.every((page) => !page.items?.length) && (
              <p>{t("products.empty")}</p>
            )}
            {list.hasNextPage && (
              <Button
                type="button"
                variant="outline"
                disabled={list.isFetchingNextPage}
                onClick={() => list.fetchNextPage()}
              >
                {t("products.load_more")}
              </Button>
            )}
          </>
        )}
        {/* Keep the editor mounted while filters load or capabilities refresh. */}
        <div hidden={!showCreate}>
          <Section title={t("products.create")}>
            <ProductForm
              workspaceId={workspaceId}
              canManage={!!manage}
              reviewLatest={reviewLatest}
            />
          </Section>
        </div>
      </div>
    </div>
  );
}
