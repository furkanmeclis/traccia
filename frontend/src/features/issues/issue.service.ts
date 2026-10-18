import type { components } from "@/generated/api";
import { apiClient, unwrap } from "@/lib/api";
import { projectService } from "@/features/projects/project.service";
import { teamService } from "@/features/teams/team.service";
import { collectIssues } from "./snapshot";
export type Schemas = components["schemas"];
export type Issue = Schemas["ProductIssuesGetOutput"]["issue"];
export type IssueFilters = {
  archived: boolean;
  q: string;
  team_id: string;
  project_id: string;
};
export const issueService = {
  async list(workspaceID: string, filters: IssueFilters, after?: string) {
    return unwrap<Schemas["ProductIssuesListOutput"]>(
      await apiClient.GET("/v1/workspaces/{workspaceID}/issues", {
        params: {
          path: { workspaceID },
          query: { ...filters, limit: 50, after },
        },
      }),
    );
  },
  async get(workspaceID: string, issueID: string) {
    return unwrap<Schemas["ProductIssuesGetOutput"]>(
      await apiClient.GET("/v1/workspaces/{workspaceID}/issues/{issueID}", {
        params: { path: { workspaceID, issueID } },
      }),
    );
  },
  async resolve(workspaceID: string, identifier: string) {
    return unwrap<Schemas["ProductIssuesResolveOutput"]>(
      await apiClient.GET(
        "/v1/workspaces/{workspaceID}/issue-identifiers/{identifier}",
        { params: { path: { workspaceID, identifier } } },
      ),
    );
  },
  async history(workspaceID: string, issueID: string, after?: string) {
    return unwrap<Schemas["ProductIssuesHistoryOutput"]>(
      await apiClient.GET(
        "/v1/workspaces/{workspaceID}/issues/{issueID}/history",
        {
          params: {
            path: { workspaceID, issueID },
            query: { limit: 50, after },
          },
        },
      ),
    );
  },
  async references(workspaceID: string) {
    const [teams, archivedTeams, projects, archivedProjects, members] =
      await Promise.all([
        collectIssues((after) => teamService.list(workspaceID, false, after)),
        collectIssues((after) => teamService.list(workspaceID, true, after)),
        collectIssues((after) =>
          projectService.projects(workspaceID, false, after),
        ),
        collectIssues((after) =>
          projectService.projects(workspaceID, true, after),
        ),
        projectService.members(workspaceID),
      ]);
    return {
      teams: [...teams.items, ...archivedTeams.items],
      projects: [...projects.items, ...archivedProjects.items],
      members: members.items,
    };
  },
  async selection(workspaceID: string, teamID: string, projectID: string) {
    const [
      statuses,
      labels,
      archivedLabels,
      sharedLabels,
      archivedSharedLabels,
      project,
    ] = await Promise.all([
      this.statuses(workspaceID, teamID),
      collectIssues((after) =>
        teamService.labels(workspaceID, teamID, false, after),
      ),
      collectIssues((after) =>
        teamService.labels(workspaceID, teamID, true, after),
      ),
      collectIssues((after) =>
        teamService.labels(workspaceID, "", false, after),
      ),
      collectIssues((after) =>
        teamService.labels(workspaceID, "", true, after),
      ),
      projectID
        ? this.projectSelection(workspaceID, projectID)
        : Promise.resolve(null),
    ]);
    const byID = new Map(
      [
        ...labels.items,
        ...archivedLabels.items,
        ...sharedLabels.items,
        ...archivedSharedLabels.items,
      ].map((label) => [label.id, label]),
    );
    return {
      statuses: statuses.items,
      labels: [...byID.values()].filter(
        (label) => !label.team_id || label.team_id === teamID,
      ),
      project,
    };
  },
  async statuses(workspaceID: string, teamID: string) {
    const load = async (archived: boolean) =>
      collectIssues(async (after) =>
        unwrap<Schemas["ProductWorkflowStatusesListOutput"]>(
          await apiClient.GET(
            "/v1/workspaces/{workspaceID}/teams/{teamID}/workflow-statuses",
            {
              params: {
                path: { workspaceID, teamID },
                query: { limit: 100, after, archived },
              },
            },
          ),
        ),
      );
    const [active, archived] = await Promise.all([load(false), load(true)]);
    if (active.workflow_version !== archived.workflow_version)
      throw new Error("ISSUE_SELECTION_CHANGED");
    return { ...active, items: [...active.items, ...archived.items] };
  },
  async projectSelection(workspaceID: string, projectID: string) {
    const [project, teams, milestones, archivedMilestones] = await Promise.all([
      projectService.get(workspaceID, projectID),
      projectService.contributors(workspaceID, projectID),
      projectService.allMilestones(workspaceID, projectID, false),
      projectService.allMilestones(workspaceID, projectID, true),
    ]);
    if (
      [
        teams.project_version,
        milestones.project_version,
        archivedMilestones.project_version,
      ].some((version) => version !== project.project.version)
    )
      throw new Error("ISSUE_SELECTION_CHANGED");
    return {
      project: project.project,
      teams: teams.items,
      milestones: [...milestones.items, ...archivedMilestones.items],
    };
  },
};
export type IssueCommand =
  | {
      action: "issue.create";
      path: { workspaceID: string };
      body: Schemas["ProductIssuesCreateInput"];
    }
  | {
      action: "issue.update";
      path: { workspaceID: string; issueID: string };
      body: Schemas["ProductIssuesUpdateInput"];
    }
  | {
      action: "issue.move";
      path: { workspaceID: string; issueID: string };
      body: Schemas["ProductIssuesMoveInput"];
    }
  | {
      action: "issue.archive";
      path: { workspaceID: string; issueID: string };
      body: Schemas["ProductIssuesArchiveInput"];
    }
  | {
      action: "issue.restore";
      path: { workspaceID: string; issueID: string };
      body: Schemas["ProductIssuesRestoreInput"];
    };
export async function executeIssueCommand(command: IssueCommand, key: string) {
  const params = { path: command.path, header: { "Idempotency-Key": key } };
  switch (command.action) {
    case "issue.create":
      return unwrap<Schemas["ProductIssuesCreateOutput"]>(
        await apiClient.POST("/v1/workspaces/{workspaceID}/issues", {
          params: { ...params, path: command.path },
          body: command.body,
        }),
      );
    case "issue.update":
      return unwrap<Schemas["ProductIssuesUpdateOutput"]>(
        await apiClient.PATCH("/v1/workspaces/{workspaceID}/issues/{issueID}", {
          params: { ...params, path: command.path },
          body: command.body,
        }),
      );
    case "issue.move":
      return unwrap<Schemas["ProductIssuesMoveOutput"]>(
        await apiClient.POST(
          "/v1/workspaces/{workspaceID}/issues/{issueID}/move",
          { params: { ...params, path: command.path }, body: command.body },
        ),
      );
    case "issue.archive":
      return unwrap<Schemas["ProductIssuesArchiveOutput"]>(
        await apiClient.POST(
          "/v1/workspaces/{workspaceID}/issues/{issueID}/archive",
          { params: { ...params, path: command.path }, body: command.body },
        ),
      );
    case "issue.restore":
      return unwrap<Schemas["ProductIssuesRestoreOutput"]>(
        await apiClient.POST(
          "/v1/workspaces/{workspaceID}/issues/{issueID}/restore",
          { params: { ...params, path: command.path }, body: command.body },
        ),
      );
  }
}
