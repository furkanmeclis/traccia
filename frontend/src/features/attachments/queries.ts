"use client";
import { useEffect } from "react";
import {
  useInfiniteQuery,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { useAuth } from "@/providers/auth-provider";
import { attachmentService } from "./attachment.service";
export const attachmentKeys = {
  all: (w: string, actor?: string) =>
    ["product-attachments", w, actor] as const,
};
export function useAttachmentList(
  workspaceID: string,
  issueID: string,
  removed: boolean,
  search: string,
  enabled: boolean,
  metadataVersion: number,
) {
  const { user } = useAuth(),
    cache = useQueryClient(),
    key = [
      ...attachmentKeys.all(workspaceID, user?.uuid),
      "issue",
      issueID,
      removed,
      search,
    ];
  useEffect(() => {
    void cache.invalidateQueries({
      queryKey: attachmentKeys.all(workspaceID, user?.uuid),
    });
  }, [cache, workspaceID, user?.uuid, metadataVersion]);
  return useInfiniteQuery({
    queryKey: key,
    enabled: enabled && !!user?.uuid,
    staleTime: 0,
    queryFn: ({ pageParam }) =>
      attachmentService.list(workspaceID, issueID, removed, search, pageParam),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (p) => (p.has_more ? p.next_cursor : undefined),
  });
}
export function useAttachmentConfig(workspaceID: string, enabled: boolean) {
  const { user } = useAuth();
  return useQuery({
    queryKey: [...attachmentKeys.all(workspaceID, user?.uuid), "config"],
    enabled: enabled && !!user?.uuid,
    queryFn: () => attachmentService.config(workspaceID),
    staleTime: 0,
  });
}
export function useOwnUploads(workspaceID: string, enabled: boolean) {
  const { user } = useAuth();
  return useInfiniteQuery({
    queryKey: [...attachmentKeys.all(workspaceID, user?.uuid), "uploads"],
    enabled: enabled && !!user?.uuid,
    queryFn: ({ pageParam }) =>
      attachmentService.uploads(workspaceID, pageParam),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (p) => (p.has_more ? p.next_cursor : undefined),
    staleTime: 0,
  });
}
