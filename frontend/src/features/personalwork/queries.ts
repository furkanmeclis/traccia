"use client";
import { useSyncExternalStore } from "react";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { useAuth } from "@/providers/auth-provider";
import { personalContext } from "./context-state";
import { personalService, type PortfolioPage } from "./personal.service";
export function usePersonalAccess(workspaceID: string) {
  const { user, isAuthenticated, bootstrapped } = useAuth(),
    context = useSyncExternalStore(
      personalContext.subscribe,
      personalContext.snapshot,
      personalContext.serverSnapshot,
    );
  return {
    context,
    actorID: user?.uuid,
    enabled:
      bootstrapped &&
      isAuthenticated &&
      !!user?.uuid &&
      context.actorID === user.uuid.toLowerCase() &&
      context.workspaceID === workspaceID.toLowerCase(),
  };
}
export const personalKeys = {
  all: (actorID: string | undefined, workspaceID: string) =>
    ["personalwork", actorID, workspaceID] as const,
};
export function usePersonalConfig(workspaceID: string) {
  const { actorID, context, enabled } = usePersonalAccess(workspaceID);
  return useQuery({
    queryKey: [
      ...personalKeys.all(actorID, workspaceID),
      context.generation,
      "config",
    ],
    enabled,
    staleTime: 0,
    refetchOnWindowFocus: true,
    async queryFn({ signal }) {
      const result = await personalService.config(workspaceID, signal);
      if (!personalContext.matches(context))
        throw new Error("PERSONAL_CONTEXT_CHANGED");
      return result;
    },
  });
}
export function useFavorites(workspaceID: string) {
  const { actorID, context, enabled } = usePersonalAccess(workspaceID);
  return useQuery({
    queryKey: [
      ...personalKeys.all(actorID, workspaceID),
      context.generation,
      "favorites",
    ],
    enabled,
    staleTime: 0,
    refetchOnWindowFocus: true,
    async queryFn({ signal }) {
      const result = await personalService.favorites(workspaceID, signal);
      if (
        !personalContext.matches(context) ||
        result.items.some(
          (row) => row.actor_id.toLowerCase() !== actorID?.toLowerCase(),
        )
      )
        throw new Error("PERSONAL_CONTEXT_CHANGED");
      return result;
    },
  });
}
export function usePortfolioPages(
  workspaceID: string,
  first: PortfolioPage,
  readable: boolean,
) {
  const { actorID, context, enabled } = usePersonalAccess(workspaceID);
  return useInfiniteQuery({
    queryKey: [
      ...personalKeys.all(actorID, workspaceID),
      context.generation,
      "portfolio",
      first.session.id,
    ],
    enabled: enabled && readable,
    initialPageParam: undefined as string | undefined,
    initialData: { pages: [first], pageParams: [undefined] },
    async queryFn({ pageParam, signal }) {
      const page = await personalService.page(
        workspaceID,
        first.session.id,
        pageParam,
        signal,
      );
      if (!personalContext.matches(context))
        throw new Error("PERSONAL_CONTEXT_CHANGED");
      return page;
    },
    getNextPageParam: (page) => (page.has_more ? page.next_cursor : undefined),
  });
}
