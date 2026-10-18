"use client";
import { useEffect } from "react";
import {
  useInfiniteQuery,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { useAuth } from "@/providers/auth-provider";
import { relationService } from "./relation.service";
export const relationKeys = {
  all: (workspace: string) => ["issue-relations", workspace] as const,
  state: (workspace: string, actor: string | undefined, issue: string) =>
    ["issue-relations", workspace, actor, "state", issue] as const,
};
export function useRelationState(
  workspace: string,
  issue: string,
  enabled: boolean,
) {
  const { user } = useAuth();
  return useQuery({
    queryKey: relationKeys.state(workspace, user?.uuid, issue),
    enabled: enabled && !!user?.uuid && !!issue,
    staleTime: 0,
    queryFn: () => relationService.state(workspace, issue),
  });
}
export function useRelations(
  workspace: string,
  issue: string,
  enabled: boolean,
  metadataVersion: number,
) {
  const { user } = useAuth();
  const cache = useQueryClient();
  useEffect(() => {
    void cache.invalidateQueries({
      queryKey: [...relationKeys.all(workspace), user?.uuid, "complete", issue],
    });
  }, [cache, workspace, user?.uuid, issue, metadataVersion]);
  return useQuery({
    queryKey: [...relationKeys.all(workspace), user?.uuid, "complete", issue],
    enabled: enabled && !!user?.uuid,
    staleTime: 0,
    queryFn: () => relationService.all(workspace, issue),
  });
}
export function useRelationCandidates(workspace: string, enabled: boolean) {
  const { user } = useAuth();
  return useQuery({
    queryKey: [...relationKeys.all(workspace), user?.uuid, "candidates"],
    enabled: enabled && !!user?.uuid,
    staleTime: 0,
    queryFn: () => relationService.candidates(workspace),
  });
}
export function useRelationHistory(
  workspace: string,
  issue: string,
  enabled: boolean,
) {
  const { user } = useAuth();
  return useInfiniteQuery({
    queryKey: [...relationKeys.all(workspace), user?.uuid, "history", issue],
    enabled: enabled && !!user?.uuid,
    staleTime: 0,
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      relationService.history(workspace, issue, pageParam),
    getNextPageParam: (page) => (page.has_more ? page.next_cursor : undefined),
  });
}
