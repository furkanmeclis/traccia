"use client";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { useAuth } from "@/providers/auth-provider";
import { issueService, type IssueFilters } from "./issue.service";
export const issueKeys = {
  all: (workspace: string) => ["product-issues", workspace] as const,
  detail: (workspace: string, actor: string | undefined, issue: string) =>
    ["product-issues", workspace, actor, "detail", issue] as const,
};
export function useIssues(
  workspace: string,
  filters: IssueFilters,
  enabled: boolean,
) {
  const { user } = useAuth();
  return useInfiniteQuery({
    queryKey: [...issueKeys.all(workspace), user?.uuid, "list", filters],
    enabled: enabled && !!user?.uuid,
    staleTime: 0,
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      issueService.list(workspace, filters, pageParam),
    getNextPageParam: (page) => (page.has_more ? page.next_cursor : undefined),
  });
}
export function useIssue(workspace: string, issue: string, enabled: boolean) {
  const { user } = useAuth();
  return useQuery({
    queryKey: issueKeys.detail(workspace, user?.uuid, issue),
    enabled: enabled && !!user?.uuid,
    staleTime: 0,
    queryFn: () => issueService.get(workspace, issue),
  });
}
export function useIssueReferences(workspace: string, enabled: boolean) {
  const { user } = useAuth();
  return useQuery({
    queryKey: [...issueKeys.all(workspace), user?.uuid, "references"],
    enabled: enabled && !!user?.uuid,
    staleTime: 0,
    queryFn: () => issueService.references(workspace),
  });
}
export function useIssueSelection(
  workspace: string,
  team: string,
  project: string,
  enabled: boolean,
) {
  const { user } = useAuth();
  return useQuery({
    queryKey: [
      ...issueKeys.all(workspace),
      user?.uuid,
      "selection",
      team,
      project,
    ],
    enabled: enabled && !!user?.uuid && !!team,
    staleTime: 0,
    queryFn: () => issueService.selection(workspace, team, project),
  });
}
export function useIssueHistory(
  workspace: string,
  issue: string,
  enabled: boolean,
) {
  const { user } = useAuth();
  return useInfiniteQuery({
    queryKey: [...issueKeys.all(workspace), user?.uuid, "history", issue],
    enabled: enabled && !!user?.uuid,
    staleTime: 0,
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      issueService.history(workspace, issue, pageParam),
    getNextPageParam: (page) => (page.has_more ? page.next_cursor : undefined),
  });
}
