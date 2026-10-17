"use client";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { useAuth } from "@/providers/auth-provider";
import { workspaceService } from "./workspace.service";
export const workspaceKeys = {
  all: ["product-workspaces"] as const,
  detail: (id: string, actor?: string) =>
    ["product-workspaces", id, "detail", actor] as const,
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
