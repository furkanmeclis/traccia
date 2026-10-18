import type { components } from "@/generated/api";
import { apiClient, unwrap } from "@/lib/api";
import { teamService, type Team } from "@/features/teams/team.service";
export type Schemas = components["schemas"];
export type Project = Schemas["ProductProjectsGetOutput"]["project"];
export type Milestone =
  Schemas["ProductProjectMilestonesGetOutput"]["milestone"];
export type Resource = Schemas["ProductProjectResourcesGetOutput"]["resource"];
// Exhaust selectors and project children; a partial snapshot is never writable.
async function collect<
  T extends { id: string },
  P extends {
    items?: T[] | null;
    has_more: boolean;
    next_cursor?: string | null;
    project_version?: number;
    capabilities?: { manage: boolean; restore?: boolean };
  },
>(load: (after?: string) => Promise<P>) {
  const items: T[] = [];
  const ids = new Set<string>();
  const cursors = new Set<string>();
  let first: P | undefined;
  let after: string | undefined;
  do {
    const page = await load(after);
    if (first && first.project_version !== page.project_version)
      throw new Error("PROJECT_SNAPSHOT_CHANGED");
    if (first?.capabilities && page.capabilities) {
      first.capabilities.manage &&= page.capabilities.manage;
      first.capabilities.restore = Boolean(
        first.capabilities.restore && page.capabilities.restore,
      );
    }
    first ??= page;
    for (const item of page.items ?? []) {
      if (!ids.has(item.id)) {
        ids.add(item.id);
        items.push(item);
      }
    }
    if (page.has_more && (!page.next_cursor || cursors.has(page.next_cursor)))
      throw new Error("INCOMPLETE_PAGINATION");
    if (page.next_cursor) cursors.add(page.next_cursor);
    after = page.has_more ? page.next_cursor! : undefined;
  } while (after);
  return { ...first!, items };
}
export const projectService = {
  async projects(workspaceID: string, archived: boolean, after?: string) {
    return unwrap<Schemas["ProductProjectsListOutput"]>(
      await apiClient.GET("/v1/workspaces/{workspaceID}/projects", {
        params: {
          path: { workspaceID },
          query: { limit: 50, after, archived },
        },
      }),
    );
  },
  async get(workspaceID: string, projectID: string) {
    return unwrap<Schemas["ProductProjectsGetOutput"]>(
      await apiClient.GET("/v1/workspaces/{workspaceID}/projects/{projectID}", {
        params: { path: { workspaceID, projectID } },
      }),
    );
  },
  async createProject(
    workspaceID: string,
    body: Schemas["ProductProjectsCreateInput"],
    key: string,
  ) {
    return unwrap<Schemas["ProductProjectsCreateOutput"]>(
      await apiClient.POST("/v1/workspaces/{workspaceID}/projects", {
        params: { path: { workspaceID }, header: { "Idempotency-Key": key } },
        body,
      }),
    );
  },
  async updateProject(
    workspaceID: string,
    projectID: string,
    body: Schemas["ProductProjectsUpdateInput"],
    key: string,
  ) {
    return unwrap<Schemas["ProductProjectsUpdateOutput"]>(
      await apiClient.PATCH(
        "/v1/workspaces/{workspaceID}/projects/{projectID}",
        {
          params: {
            path: { workspaceID, projectID },
            header: { "Idempotency-Key": key },
          },
          body,
        },
      ),
    );
  },
  async archiveProject(
    workspaceID: string,
    projectID: string,
    body: Schemas["ProductProjectsArchiveInput"],
    key: string,
  ) {
    return unwrap<Schemas["ProductProjectsArchiveOutput"]>(
      await apiClient.POST(
        "/v1/workspaces/{workspaceID}/projects/{projectID}/archive",
        {
          params: {
            path: { workspaceID, projectID },
            header: { "Idempotency-Key": key },
          },
          body,
        },
      ),
    );
  },
  async restoreProject(
    workspaceID: string,
    projectID: string,
    body: Schemas["ProductProjectsRestoreInput"],
    key: string,
  ) {
    return unwrap<Schemas["ProductProjectsRestoreOutput"]>(
      await apiClient.POST(
        "/v1/workspaces/{workspaceID}/projects/{projectID}/restore",
        {
          params: {
            path: { workspaceID, projectID },
            header: { "Idempotency-Key": key },
          },
          body,
        },
      ),
    );
  },
  async milestones(
    workspaceID: string,
    projectID: string,
    archived: boolean,
    after?: string,
  ) {
    return unwrap<Schemas["ProductProjectMilestonesListOutput"]>(
      await apiClient.GET(
        "/v1/workspaces/{workspaceID}/projects/{projectID}/milestones",
        {
          params: {
            path: { workspaceID, projectID },
            query: { limit: 50, after, archived },
          },
        },
      ),
    );
  },
  async getMilestone(
    workspaceID: string,
    projectID: string,
    milestoneID: string,
  ) {
    return unwrap<Schemas["ProductProjectMilestonesGetOutput"]>(
      await apiClient.GET(
        "/v1/workspaces/{workspaceID}/projects/{projectID}/milestones/{milestoneID}",
        { params: { path: { workspaceID, projectID, milestoneID } } },
      ),
    );
  },
  async createMilestone(
    workspaceID: string,
    projectID: string,
    body: Schemas["ProductProjectMilestonesCreateInput"],
    key: string,
  ) {
    return unwrap<Schemas["ProductProjectMilestonesCreateOutput"]>(
      await apiClient.POST(
        "/v1/workspaces/{workspaceID}/projects/{projectID}/milestones",
        {
          params: {
            path: { workspaceID, projectID },
            header: { "Idempotency-Key": key },
          },
          body,
        },
      ),
    );
  },
  async updateMilestone(
    workspaceID: string,
    projectID: string,
    milestoneID: string,
    body: Schemas["ProductProjectMilestonesUpdateInput"],
    key: string,
  ) {
    return unwrap<Schemas["ProductProjectMilestonesUpdateOutput"]>(
      await apiClient.PATCH(
        "/v1/workspaces/{workspaceID}/projects/{projectID}/milestones/{milestoneID}",
        {
          params: {
            path: { workspaceID, projectID, milestoneID },
            header: { "Idempotency-Key": key },
          },
          body,
        },
      ),
    );
  },
  async archiveMilestone(
    workspaceID: string,
    projectID: string,
    milestoneID: string,
    body: Schemas["ProductProjectMilestonesArchiveInput"],
    key: string,
  ) {
    return unwrap<Schemas["ProductProjectMilestonesArchiveOutput"]>(
      await apiClient.POST(
        "/v1/workspaces/{workspaceID}/projects/{projectID}/milestones/{milestoneID}/archive",
        {
          params: {
            path: { workspaceID, projectID, milestoneID },
            header: { "Idempotency-Key": key },
          },
          body,
        },
      ),
    );
  },
  async restoreMilestone(
    workspaceID: string,
    projectID: string,
    milestoneID: string,
    body: Schemas["ProductProjectMilestonesRestoreInput"],
    key: string,
  ) {
    return unwrap<Schemas["ProductProjectMilestonesRestoreOutput"]>(
      await apiClient.POST(
        "/v1/workspaces/{workspaceID}/projects/{projectID}/milestones/{milestoneID}/restore",
        {
          params: {
            path: { workspaceID, projectID, milestoneID },
            header: { "Idempotency-Key": key },
          },
          body,
        },
      ),
    );
  },
  async resources(
    workspaceID: string,
    projectID: string,
    archived: boolean,
    after?: string,
  ) {
    return unwrap<Schemas["ProductProjectResourcesListOutput"]>(
      await apiClient.GET(
        "/v1/workspaces/{workspaceID}/projects/{projectID}/resources",
        {
          params: {
            path: { workspaceID, projectID },
            query: { limit: 50, after, archived },
          },
        },
      ),
    );
  },
  async getResource(
    workspaceID: string,
    projectID: string,
    resourceID: string,
  ) {
    return unwrap<Schemas["ProductProjectResourcesGetOutput"]>(
      await apiClient.GET(
        "/v1/workspaces/{workspaceID}/projects/{projectID}/resources/{resourceID}",
        { params: { path: { workspaceID, projectID, resourceID } } },
      ),
    );
  },
  async createResource(
    workspaceID: string,
    projectID: string,
    body: Schemas["ProductProjectResourcesCreateInput"],
    key: string,
  ) {
    return unwrap<Schemas["ProductProjectResourcesCreateOutput"]>(
      await apiClient.POST(
        "/v1/workspaces/{workspaceID}/projects/{projectID}/resources",
        {
          params: {
            path: { workspaceID, projectID },
            header: { "Idempotency-Key": key },
          },
          body,
        },
      ),
    );
  },
  async updateResource(
    workspaceID: string,
    projectID: string,
    resourceID: string,
    body: Schemas["ProductProjectResourcesUpdateInput"],
    key: string,
  ) {
    return unwrap<Schemas["ProductProjectResourcesUpdateOutput"]>(
      await apiClient.PATCH(
        "/v1/workspaces/{workspaceID}/projects/{projectID}/resources/{resourceID}",
        {
          params: {
            path: { workspaceID, projectID, resourceID },
            header: { "Idempotency-Key": key },
          },
          body,
        },
      ),
    );
  },
  async archiveResource(
    workspaceID: string,
    projectID: string,
    resourceID: string,
    body: Schemas["ProductProjectResourcesArchiveInput"],
    key: string,
  ) {
    return unwrap<Schemas["ProductProjectResourcesArchiveOutput"]>(
      await apiClient.POST(
        "/v1/workspaces/{workspaceID}/projects/{projectID}/resources/{resourceID}/archive",
        {
          params: {
            path: { workspaceID, projectID, resourceID },
            header: { "Idempotency-Key": key },
          },
          body,
        },
      ),
    );
  },
  async restoreResource(
    workspaceID: string,
    projectID: string,
    resourceID: string,
    body: Schemas["ProductProjectResourcesRestoreInput"],
    key: string,
  ) {
    return unwrap<Schemas["ProductProjectResourcesRestoreOutput"]>(
      await apiClient.POST(
        "/v1/workspaces/{workspaceID}/projects/{projectID}/resources/{resourceID}/restore",
        {
          params: {
            path: { workspaceID, projectID, resourceID },
            header: { "Idempotency-Key": key },
          },
          body,
        },
      ),
    );
  },
  async teams(workspaceID: string) {
    return collect<Team, Awaited<ReturnType<typeof teamService.list>>>(
      (after) => teamService.list(workspaceID, false, after),
    );
  },
  async members(workspaceID: string) {
    const load = async (after?: string) =>
      unwrap<Schemas["ProductWorkspaceMembersListOutput"]>(
        await apiClient.GET("/v1/workspaces/{workspaceID}/members", {
          params: { path: { workspaceID }, query: { limit: 100, after } },
        }),
      );
    return collect<
      NonNullable<
        Schemas["ProductWorkspaceMembersListOutput"]["items"]
      >[number],
      Awaited<ReturnType<typeof load>>
    >(load);
  },
  async contributors(workspaceID: string, projectID: string) {
    const load = async (after?: string) =>
      unwrap<Schemas["ProductProjectTeamsListOutput"]>(
        await apiClient.GET(
          "/v1/workspaces/{workspaceID}/projects/{projectID}/teams",
          {
            params: {
              path: { workspaceID, projectID },
              query: { limit: 100, after, archived: false },
            },
          },
        ),
      );
    return collect<Team, Awaited<ReturnType<typeof load>>>(load);
  },
  async setTeams(
    workspaceID: string,
    projectID: string,
    body: Schemas["ProductProjectTeamsSetInput"],
    key: string,
  ) {
    return unwrap<Schemas["ProductProjectTeamsSetOutput"]>(
      await apiClient.PUT(
        "/v1/workspaces/{workspaceID}/projects/{projectID}/teams",
        {
          params: {
            path: { workspaceID, projectID },
            header: { "Idempotency-Key": key },
          },
          body,
        },
      ),
    );
  },
  async allMilestones(
    workspaceID: string,
    projectID: string,
    archived: boolean,
  ) {
    return collect<Milestone, Schemas["ProductProjectMilestonesListOutput"]>(
      (after) => this.milestones(workspaceID, projectID, archived, after),
    );
  },
  async allResources(
    workspaceID: string,
    projectID: string,
    archived: boolean,
  ) {
    return collect<Resource, Schemas["ProductProjectResourcesListOutput"]>(
      (after) => this.resources(workspaceID, projectID, archived, after),
    );
  },
};
