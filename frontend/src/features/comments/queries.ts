"use client";
import { useEffect } from "react";
import {
  useInfiniteQuery,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { useAuth } from "@/providers/auth-provider";
import { commentService } from "./comment.service";
export const commentKeys = {
  all: (workspaceID: string, actorID?: string) =>
    ["comments", workspaceID, actorID] as const,
};
export function useComments(
  workspaceID: string,
  issueID: string,
  includeDeleted: boolean,
  enabled: boolean,
  metadataVersion: number,
) {
  const { user } = useAuth(),
    cache = useQueryClient();
  useEffect(() => {
    void cache.invalidateQueries({
      queryKey: commentKeys.all(workspaceID, user?.uuid),
    });
  }, [cache, workspaceID, user?.uuid, metadataVersion]);
  return useInfiniteQuery({
    queryKey: [
      ...commentKeys.all(workspaceID, user?.uuid),
      "list",
      issueID,
      includeDeleted,
    ],
    enabled: enabled && !!user?.uuid,
    staleTime: 0,
    queryFn: ({ pageParam }) =>
      commentService.list(workspaceID, issueID, includeDeleted, pageParam),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (page) => (page.has_more ? page.next_cursor : undefined),
  });
}
export function useComment(
  workspaceID: string,
  commentID: string,
  enabled: boolean,
) {
  const { user } = useAuth();
  return useQuery({
    queryKey: [...commentKeys.all(workspaceID, user?.uuid), "get", commentID],
    enabled: enabled && !!user?.uuid && !!commentID,
    staleTime: 0,
    queryFn: ({ signal }) => commentService.get(workspaceID, commentID, signal),
  });
}
export function useCommentRevisions(
  workspaceID: string,
  commentID: string,
  enabled: boolean,
) {
  const { user } = useAuth();
  return useInfiniteQuery({
    queryKey: [
      ...commentKeys.all(workspaceID, user?.uuid),
      "revisions",
      commentID,
    ],
    enabled: enabled && !!user?.uuid && !!commentID,
    queryFn: ({ pageParam }) =>
      commentService.revisions(workspaceID, commentID, pageParam),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (page) => (page.has_more ? page.next_cursor : undefined),
  });
}
export function useCommentRevision(
  workspaceID: string,
  commentID: string,
  revisionID: string,
  enabled: boolean,
) {
  const { user } = useAuth();
  return useQuery({
    queryKey: [
      ...commentKeys.all(workspaceID, user?.uuid),
      "revision",
      commentID,
      revisionID,
    ],
    enabled: enabled && !!user?.uuid && !!revisionID,
    queryFn: () => commentService.revision(workspaceID, commentID, revisionID),
  });
}
export function useCommentMentions(workspaceID: string, enabled: boolean) {
  const { user } = useAuth();
  return useQuery({
    queryKey: [...commentKeys.all(workspaceID, user?.uuid), "mentions"],
    enabled: enabled && !!user?.uuid,
    queryFn: () => commentService.mentions(workspaceID),
  });
}
export function useIssueTimeline(
  workspaceID: string,
  issueID: string,
  enabled: boolean,
) {
  const { user } = useAuth();
  return useInfiniteQuery({
    queryKey: [
      ...commentKeys.all(workspaceID, user?.uuid),
      "timeline",
      issueID,
    ],
    enabled: enabled && !!user?.uuid,
    queryFn: ({ pageParam }) =>
      commentService.timeline(workspaceID, issueID, pageParam),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (page) => (page.has_more ? page.next_cursor : undefined),
  });
}
export function useCommentBacklinks(
  workspaceID: string,
  issueID: string,
  enabled: boolean,
) {
  const { user } = useAuth();
  return useInfiniteQuery({
    queryKey: [
      ...commentKeys.all(workspaceID, user?.uuid),
      "backlinks",
      issueID,
    ],
    enabled: enabled && !!user?.uuid,
    queryFn: ({ pageParam }) =>
      commentService.backlinks(workspaceID, issueID, pageParam),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (page) => (page.has_more ? page.next_cursor : undefined),
  });
}
