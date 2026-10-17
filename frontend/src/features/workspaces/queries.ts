"use client";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { ApiError } from "@/lib/api";
import { useAuth } from "@/providers/auth-provider";
import { canAutomaticallyRefreshActivity } from "./activity-refresh";
import { workspaceService } from "./workspace.service";
export const workspaceKeys = {
  all: ["product-workspaces"] as const,
  detail: (id: string, actor?: string) =>
    ["product-workspaces", id, "detail", actor] as const,
  activity: (id: string, actor?: string) =>
    ["product-workspaces", id, "activity", actor] as const,
  credentials: (id: string, actor?: string) =>
    actor
      ? (["product-workspaces", id, "credentials", actor] as const)
      : (["product-workspaces", id, "credentials"] as const),
};
export function useWorkspaces() {
  const { user } = useAuth();
  return useInfiniteQuery({
    queryKey: [...workspaceKeys.all, "list", user?.uuid],
    enabled: Boolean(user?.uuid),
    staleTime: 0,
    refetchOnWindowFocus: true,
    queryFn: ({ pageParam }) => workspaceService.list(pageParam),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (page) => (page.has_more ? page.next_cursor : undefined),
  });
}
export function useWorkspace(id: string) {
  const { user } = useAuth();
  return useQuery({
    queryKey: workspaceKeys.detail(id, user?.uuid),
    enabled: Boolean(user?.uuid),
    staleTime: 0,
    refetchOnWindowFocus: true,
    queryFn: () => workspaceService.get(id),
  });
}
export function useCredentials(id: string, canRead = true) {
  const { user } = useAuth();
  return useInfiniteQuery({
    queryKey: workspaceKeys.credentials(id, user?.uuid),
    enabled: Boolean(user?.uuid) && canRead,
    staleTime: 0,
    refetchOnWindowFocus: true,
    queryFn: ({ pageParam }) => workspaceService.credentials(id, pageParam),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (page) => (page.has_more ? page.next_cursor : undefined),
  });
}

export function useWorkspaceActivity(id: string) {
  const { user } = useAuth();
  const automaticRefresh = (query: {
    state: { data?: { pages: readonly unknown[] }; error: unknown };
  }) =>
    canAutomaticallyRefreshActivity(
      query.state.data?.pages.length ?? 0,
      query.state.error instanceof ApiError
        ? query.state.error.status
        : undefined,
    );
  return useInfiniteQuery({
    queryKey: workspaceKeys.activity(id, user?.uuid),
    enabled: Boolean(user?.uuid),
    staleTime: 0,
    queryFn: ({ pageParam }) => workspaceService.activity(id, pageParam),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (page) => (page.has_more ? page.next_cursor : undefined),
    refetchOnMount: automaticRefresh,
    refetchOnWindowFocus: automaticRefresh,
    refetchOnReconnect: automaticRefresh,
    refetchInterval: (query) => (automaticRefresh(query) ? 30_000 : false),
    refetchIntervalInBackground: false,
    retry: (count, error) =>
      !(error instanceof ApiError && [401, 403, 404].includes(error.status)) &&
      count < 2,
  });
}
