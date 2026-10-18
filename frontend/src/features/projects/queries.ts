"use client";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { useAuth } from "@/providers/auth-provider";
import { projectService } from "./project.service";
export const projectKeys = {
  all: (workspace: string) => ["product-projects", workspace] as const,
};
export function useProjects(
  workspace: string,
  archived: boolean,
  enabled: boolean,
) {
  const { user } = useAuth();
  return useInfiniteQuery({
    queryKey: [...projectKeys.all(workspace), user?.uuid, "list", archived],
    enabled: enabled && !!user?.uuid,
    staleTime: 0,
    refetchOnWindowFocus: true,
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      projectService.projects(workspace, archived, pageParam),
    getNextPageParam: (page) => (page.has_more ? page.next_cursor : undefined),
  });
}
export function useProject(
  workspace: string,
  project: string,
  enabled: boolean,
) {
  const { user } = useAuth();
  return useQuery({
    queryKey: [...projectKeys.all(workspace), user?.uuid, project, "detail"],
    enabled: enabled && !!user?.uuid,
    staleTime: 0,
    refetchOnWindowFocus: true,
    queryFn: () => projectService.get(workspace, project),
  });
}
export function useProjectReferences(workspace: string, enabled: boolean) {
  const { user } = useAuth();
  return useQuery({
    queryKey: [...projectKeys.all(workspace), user?.uuid, "references"],
    enabled: enabled && !!user?.uuid,
    staleTime: 0,
    refetchOnWindowFocus: true,
    queryFn: async () => {
      const [teams, members] = await Promise.all([
        projectService.teams(workspace),
        projectService.members(workspace),
      ]);
      return {
        teams: teams.items.filter((team) => !team.archived),
        allMembers: members.items,
        members: members.items.filter(
          (member) => member.active && member.assignable,
        ),
      };
    },
  });
}
export function useProjectChildren(
  workspace: string,
  project: string,
  archived: boolean,
  enabled: boolean,
) {
  const { user } = useAuth();
  return useQuery({
    queryKey: [
      ...projectKeys.all(workspace),
      user?.uuid,
      project,
      "children",
      archived,
    ],
    enabled: enabled && !!user?.uuid,
    staleTime: 0,
    refetchOnWindowFocus: true,
    queryFn: async () => {
      const [contributors, milestones, resources] = await Promise.all([
        projectService.contributors(workspace, project),
        projectService.allMilestones(workspace, project, archived),
        projectService.allResources(workspace, project, archived),
      ]);
      if (
        contributors.project_version !== milestones.project_version ||
        milestones.project_version !== resources.project_version
      )
        throw new Error("PROJECT_SNAPSHOT_CHANGED");
      return {
        contributors,
        milestones,
        resources,
        projectVersion: contributors.project_version,
      };
    },
  });
}
