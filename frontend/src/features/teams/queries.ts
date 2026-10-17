"use client";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { useAuth } from "@/providers/auth-provider";
import { teamService } from "./team.service";
export const teamKeys = {
  all: (workspace: string) => ["product-teams", workspace] as const,
};
export function useTeams(
  workspace: string,
  archived: boolean,
  enabled: boolean,
) {
  const { user } = useAuth();
  return useInfiniteQuery({
    queryKey: [...teamKeys.all(workspace), user?.uuid, "list", archived],
    enabled: enabled && Boolean(user?.uuid),
    staleTime: 0,
    refetchOnWindowFocus: true,
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      teamService.list(workspace, archived, pageParam),
    getNextPageParam: (page) => (page.has_more ? page.next_cursor : undefined),
  });
}
export function useTeam(workspace: string, team: string, enabled: boolean) {
  const { user } = useAuth();
  return useQuery({
    queryKey: [...teamKeys.all(workspace), user?.uuid, team, "detail"],
    enabled: enabled && Boolean(user?.uuid),
    staleTime: 0,
    refetchOnWindowFocus: true,
    queryFn: () => teamService.get(workspace, team),
  });
}
export function useStatuses(workspace: string, team: string, enabled: boolean) {
  const { user } = useAuth();
  return useQuery({
    queryKey: [...teamKeys.all(workspace), user?.uuid, team, "workflow"],
    enabled: enabled && Boolean(user?.uuid),
    staleTime: 0,
    refetchOnWindowFocus: true,
    queryFn: () => teamService.statuses(workspace, team),
  });
}
export function useLabels(
  workspace: string,
  team: string,
  archived: boolean,
  enabled: boolean,
) {
  const { user } = useAuth();
  return useInfiniteQuery({
    queryKey: [
      ...teamKeys.all(workspace),
      user?.uuid,
      team,
      "labels",
      archived,
    ],
    enabled: enabled && Boolean(user?.uuid),
    staleTime: 0,
    refetchOnWindowFocus: true,
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      teamService.labels(workspace, team, archived, pageParam),
    getNextPageParam: (page) => (page.has_more ? page.next_cursor : undefined),
  });
}
