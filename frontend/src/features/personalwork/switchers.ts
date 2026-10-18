"use client";
import { useSyncExternalStore } from "react";
import { personalContext } from "./context-state";
import { useQuery } from "@tanstack/react-query";
import { useAuth } from "@/providers/auth-provider";
import { workspaceService } from "@/features/workspaces/workspace.service";
import { teamService } from "@/features/teams/team.service";
import { projectService } from "@/features/projects/project.service";
import { collectSelector } from "./scope";
export function useProductSwitchers(workspaceID: string | null) {
  const { user, isAuthenticated, bootstrapped } = useAuth();
  const context = useSyncExternalStore(
    personalContext.subscribe,
    personalContext.snapshot,
    personalContext.serverSnapshot,
  );
  const actorID = user?.uuid,
    enabled =
      bootstrapped &&
      isAuthenticated &&
      !!actorID &&
      context.actorID === actorID.toLowerCase() &&
      context.workspaceID === (workspaceID?.toLowerCase() ?? null);
  const workspaces = useQuery({
    queryKey: [
      "personalwork",
      "switcher-workspaces",
      actorID,
      context.generation,
    ],
    enabled,
    staleTime: 0,
    refetchOnWindowFocus: true,
    queryFn: async () => {
      const rows = await collectSelector((after) =>
        workspaceService.list(after),
      );
      if (!personalContext.matches(context))
        throw new Error("PERSONAL_CONTEXT_CHANGED");
      return rows;
    },
  });
  const resources = useQuery({
    queryKey: [
      "personalwork",
      "switcher-resources",
      actorID,
      workspaceID,
      context.generation,
    ],
    enabled: enabled && !!workspaceID,
    staleTime: 0,
    refetchOnWindowFocus: true,
    async queryFn() {
      if (!workspaceID) throw new Error("PERSONAL_SCOPE_INVALID");
      const workspace = await workspaceService.get(workspaceID);
      const [teams, projects] = await Promise.all([
        collectSelector(
          (after) => teamService.list(workspaceID, false, after),
          (row) => row.workspace_id.toLowerCase() === workspaceID.toLowerCase(),
        ),
        collectSelector(
          (after) => projectService.projects(workspaceID, false, after),
          (row) => row.workspace_id.toLowerCase() === workspaceID.toLowerCase(),
        ),
      ]);
      if (!personalContext.matches(context))
        throw new Error("PERSONAL_CONTEXT_CHANGED");
      return { workspace, teams, projects };
    },
  });
  return {
    actorID,
    workspaces,
    resources,
    workspacesReadable:
      enabled &&
      workspaces.isSuccess &&
      !workspaces.isError &&
      !workspaces.isFetching,
    resourcesReadable:
      enabled &&
      !!workspaceID &&
      resources.isSuccess &&
      !resources.isError &&
      !resources.isFetching &&
      workspaces.isSuccess &&
      !workspaces.isError &&
      !workspaces.isFetching,
  };
}
