"use client";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { useAuth } from "@/providers/auth-provider";
import { searchService, type SearchPage } from "./search.service";
import type { SearchResource } from "./capture-state";
export const searchKeys = {
  all: (workspaceID: string, actorID?: string) =>
    ["searchviews", workspaceID, actorID] as const,
};
export function useSearchConfig(workspaceID: string, enabled: boolean) {
  const { user } = useAuth();
  return useQuery({
    queryKey: [...searchKeys.all(workspaceID, user?.uuid), "config"],
    enabled: enabled && !!user?.uuid,
    staleTime: 0,
    queryFn: ({ signal }) => searchService.config(workspaceID, signal),
  });
}
export function useSavedViews(
  workspaceID: string,
  resource: SearchResource,
  includeDeleted: boolean,
  enabled: boolean,
) {
  const { user } = useAuth();
  return useInfiniteQuery({
    queryKey: [
      ...searchKeys.all(workspaceID, user?.uuid),
      "views",
      resource,
      includeDeleted,
    ],
    enabled: enabled && !!user?.uuid,
    queryFn: ({ pageParam, signal }) =>
      searchService.views(
        workspaceID,
        resource,
        includeDeleted,
        pageParam,
        signal,
      ),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (page) => (page.has_more ? page.next_cursor : undefined),
  });
}
export function useSavedView(
  workspaceID: string,
  viewID: string,
  enabled: boolean,
) {
  const { user } = useAuth();
  return useQuery({
    queryKey: [...searchKeys.all(workspaceID, user?.uuid), "view", viewID],
    enabled: enabled && !!user?.uuid && !!viewID,
    staleTime: 0,
    queryFn: ({ signal }) => searchService.view(workspaceID, viewID, signal),
  });
}
export function useColumns(
  workspaceID: string,
  resource: SearchResource,
  enabled: boolean,
) {
  const { user } = useAuth();
  return useQuery({
    queryKey: [...searchKeys.all(workspaceID, user?.uuid), "columns", resource],
    enabled: enabled && !!user?.uuid,
    staleTime: 0,
    queryFn: ({ signal }) =>
      searchService.columns(workspaceID, resource, signal),
  });
}
export function useSnapshotPages(
  workspaceID: string,
  resource: SearchResource,
  first: SearchPage,
  enabled: boolean,
) {
  const { user } = useAuth();
  return useInfiniteQuery({
    queryKey: [
      ...searchKeys.all(workspaceID, user?.uuid),
      "snapshot",
      resource,
      first.session.id,
    ],
    enabled: enabled && !!user?.uuid,
    staleTime: Infinity,
    retry: false,
    queryFn: ({ pageParam, signal }) =>
      searchService.page(
        workspaceID,
        resource,
        first.session.id,
        pageParam,
        signal,
      ),
    initialPageParam: "",
    initialData: { pages: [first], pageParams: [""] },
    getNextPageParam: (page) => (page.has_more ? page.next_cursor : undefined),
  });
}
