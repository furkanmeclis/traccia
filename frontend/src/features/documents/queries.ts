"use client";
import { useEffect } from "react";
import {
  useQuery,
  useInfiniteQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { useAuth } from "@/providers/auth-provider";
import { documentService, type Owner } from "./document.service";
export const documentKeys = {
  all: (workspace: string) => ["documents", workspace] as const,
  owner: (workspace: string, actor: string | undefined, owner: Owner) =>
    ["documents", workspace, actor, "owner", owner.kind, owner.id] as const,
};
export function useDocument(
  workspace: string,
  owner: Owner,
  enabled: boolean,
  metadataVersion?: number,
) {
  const { user } = useAuth();
  const cache = useQueryClient();
  useEffect(() => {
    void cache.invalidateQueries({
      queryKey: documentKeys.owner(workspace, user?.uuid, {
        kind: owner.kind,
        id: owner.id,
      }),
    });
  }, [cache, workspace, user?.uuid, owner.kind, owner.id, metadataVersion]);
  return useQuery({
    queryKey: documentKeys.owner(workspace, user?.uuid, owner),
    enabled: enabled && !!user?.uuid,
    staleTime: 0,
    queryFn: async () => {
      const resolved = await documentService.resolve(workspace, owner);
      if (!resolved.document) return resolved;
      const current = await documentService.get(
        workspace,
        resolved.document.id,
      );
      return {
        ...resolved,
        document: current.document,
        capabilities: {
          manage:
            resolved.capabilities.manage &&
            current.document.capabilities.manage,
        },
      };
    },
  });
}
export function useDocumentMentions(workspace: string, enabled: boolean) {
  const { user } = useAuth();
  return useQuery({
    queryKey: [...documentKeys.all(workspace), user?.uuid, "mentions"],
    enabled: enabled && !!user?.uuid,
    staleTime: 0,
    queryFn: () => documentService.mentions(workspace),
  });
}
export function useDocumentRevisions(
  workspace: string,
  document: string,
  enabled: boolean,
) {
  const { user } = useAuth();
  return useInfiniteQuery({
    queryKey: [
      ...documentKeys.all(workspace),
      user?.uuid,
      document,
      "revisions",
    ],
    enabled: enabled && !!document && !!user?.uuid,
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      documentService.revisions(workspace, document, pageParam),
    getNextPageParam: (page) => (page.has_more ? page.next_cursor : undefined),
  });
}
export function useDocumentBacklinks(
  workspace: string,
  owner: Owner,
  enabled: boolean,
) {
  const { user } = useAuth();
  return useInfiniteQuery({
    queryKey: [
      ...documentKeys.all(workspace),
      user?.uuid,
      "backlinks",
      owner.kind,
      owner.id,
    ],
    enabled: enabled && !!user?.uuid,
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      documentService.backlinks(workspace, owner, pageParam),
    getNextPageParam: (page) => (page.has_more ? page.next_cursor : undefined),
  });
}
