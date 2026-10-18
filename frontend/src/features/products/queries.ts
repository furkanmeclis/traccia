"use client";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { useAuth } from "@/providers/auth-provider";
import { productService, type ProductFilters } from "./product.service";
export const productKeys = {
  all: (workspace: string) => ["product-catalog", workspace] as const,
  detail: (workspace: string, actor: string | undefined, product: string) =>
    ["product-catalog", workspace, actor, product, "detail"] as const,
};
export function useProducts(
  workspace: string,
  filters: ProductFilters,
  enabled: boolean,
) {
  const { user } = useAuth();
  return useInfiniteQuery({
    queryKey: [...productKeys.all(workspace), user?.uuid, "list", filters],
    enabled: enabled && !!user?.uuid,
    staleTime: 0,
    refetchOnWindowFocus: true,
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      productService.list(workspace, filters, pageParam),
    getNextPageParam: (page) => (page.has_more ? page.next_cursor : undefined),
  });
}
export function useProduct(
  workspace: string,
  product: string,
  enabled: boolean,
) {
  const { user } = useAuth();
  return useQuery({
    queryKey: productKeys.detail(workspace, user?.uuid, product),
    enabled: enabled && !!user?.uuid,
    staleTime: 0,
    refetchOnWindowFocus: true,
    queryFn: () => productService.get(workspace, product),
  });
}
export function useProductChoices(
  workspace: string,
  enabled: boolean,
  includeArchived = false,
) {
  const { user } = useAuth();
  return useQuery({
    queryKey: [
      ...productKeys.all(workspace),
      user?.uuid,
      "project-choices",
      includeArchived,
    ],
    enabled: enabled && !!user?.uuid,
    staleTime: 0,
    refetchOnWindowFocus: true,
    queryFn: () => productService.projectChoices(workspace, includeArchived),
  });
}
export function useProductChildren(
  workspace: string,
  product: string,
  enabled: boolean,
) {
  const { user } = useAuth();
  return useQuery({
    queryKey: [...productKeys.all(workspace), user?.uuid, product, "children"],
    enabled: enabled && !!user?.uuid,
    staleTime: 0,
    refetchOnWindowFocus: true,
    queryFn: async () => {
      const [
        environments,
        archivedEnvironments,
        activeProjects,
        archivedProjects,
      ] = await Promise.all([
        productService.allEnvironments(workspace, product, {
          archived: false,
          q: "",
          kind: "",
        }),
        productService.allEnvironments(workspace, product, {
          archived: true,
          q: "",
          kind: "",
        }),
        productService.allProjects(workspace, product, false),
        productService.allProjects(workspace, product, true),
      ]);
      if (
        environments.product_version !== archivedEnvironments.product_version ||
        environments.product_version !== activeProjects.product_version ||
        activeProjects.product_version !== archivedProjects.product_version
      )
        throw new Error("PRODUCT_SNAPSHOT_CHANGED");
      return {
        environments: {
          ...environments,
          items: [...environments.items, ...archivedEnvironments.items],
          capabilities: environments.capabilities,
          archivedCapabilities: archivedEnvironments.capabilities,
        },
        projects: activeProjects.items,
        historicalProjects: archivedProjects.items,
        projectsManage: activeProjects.capabilities.manage,
        productVersion: environments.product_version,
      };
    },
  });
}
