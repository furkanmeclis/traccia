import type { components } from "@/generated/api";
import { apiClient, unwrap } from "@/lib/api";
type Schemas = components["schemas"];
export type Team = Schemas["ProductTeamsGetOutput"]["team"];
export type WorkflowStatus = NonNullable<
  Schemas["ProductWorkflowStatusesListOutput"]["items"]
>[number];
export type TeamLabel = NonNullable<
  Schemas["ProductLabelsListOutput"]["items"]
>[number];
export type StatusCategory = WorkflowStatus["category"];
export const statusCategories = [
  "backlog",
  "unstarted",
  "started",
  "completed",
  "canceled",
] as const;
export const teamService = {
  async list(workspaceID: string, archived: boolean, after?: string) {
    return unwrap<Schemas["ProductTeamsListOutput"]>(
      await apiClient.GET("/v1/workspaces/{workspaceID}/teams", {
        params: {
          path: { workspaceID },
          query: { limit: 20, after, archived },
        },
      }),
    );
  },
  async get(workspaceID: string, teamID: string) {
    return unwrap<Schemas["ProductTeamsGetOutput"]>(
      await apiClient.GET("/v1/workspaces/{workspaceID}/teams/{teamID}", {
        params: { path: { workspaceID, teamID } },
      }),
    );
  },
  async create(
    workspaceID: string,
    body: { name: string; code: string },
    key: string,
  ) {
    return unwrap<Schemas["ProductTeamsCreateOutput"]>(
      await apiClient.POST("/v1/workspaces/{workspaceID}/teams", {
        params: { path: { workspaceID }, header: { "Idempotency-Key": key } },
        body,
      }),
    );
  },
  async update(
    workspaceID: string,
    teamID: string,
    body: { name: string; expected_version: number },
    key: string,
  ) {
    return unwrap<Schemas["ProductTeamsUpdateOutput"]>(
      await apiClient.PATCH("/v1/workspaces/{workspaceID}/teams/{teamID}", {
        params: {
          path: { workspaceID, teamID },
          header: { "Idempotency-Key": key },
        },
        body,
      }),
    );
  },
  async archive(
    workspaceID: string,
    teamID: string,
    body: { expected_version: number },
    key: string,
  ) {
    return unwrap<Schemas["ProductTeamsArchiveOutput"]>(
      await apiClient.POST(
        "/v1/workspaces/{workspaceID}/teams/{teamID}/archive",
        {
          params: {
            path: { workspaceID, teamID },
            header: { "Idempotency-Key": key },
          },
          body,
        },
      ),
    );
  },
  async statuses(workspaceID: string, teamID: string) {
    const items: WorkflowStatus[] = [];
    let after: string | undefined;
    const cursors = new Set<string>();
    let first: Schemas["ProductWorkflowStatusesListOutput"] | undefined;
    do {
      const page = unwrap<Schemas["ProductWorkflowStatusesListOutput"]>(
        await apiClient.GET(
          "/v1/workspaces/{workspaceID}/teams/{teamID}/workflow-statuses",
          {
            params: {
              path: { workspaceID, teamID },
              query: { limit: 100, after },
            },
          },
        ),
      );
      const result = await page;
      if (first && first.workflow_version !== result.workflow_version)
        throw new Error(
          "Workflow changed during pagination. Reload the workflow.",
        );
      if (first)
        first.capabilities.manage =
          first.capabilities.manage && result.capabilities.manage;
      first ??= result;
      items.push(...(result.items ?? []));
      if (
        result.has_more &&
        (!result.next_cursor || cursors.has(result.next_cursor))
      )
        throw new Error("Incomplete workflow pagination");
      if (result.next_cursor) cursors.add(result.next_cursor);
      after = result.has_more ? result.next_cursor : undefined;
    } while (after);
    return { ...first!, items };
  },
  async createStatus(
    workspaceID: string,
    teamID: string,
    body: {
      name: string;
      category: StatusCategory;
      color: string;
      expected_workflow_version: number;
    },
    key: string,
  ) {
    return unwrap<Schemas["ProductWorkflowStatusesCreateOutput"]>(
      await apiClient.POST(
        "/v1/workspaces/{workspaceID}/teams/{teamID}/workflow-statuses",
        {
          params: {
            path: { workspaceID, teamID },
            header: { "Idempotency-Key": key },
          },
          body,
        },
      ),
    );
  },
  async updateStatus(
    workspaceID: string,
    teamID: string,
    statusID: string,
    body: {
      name: string;
      category: StatusCategory;
      color: string;
      expected_version: number;
      expected_workflow_version: number;
    },
    key: string,
  ) {
    return unwrap<Schemas["ProductWorkflowStatusesUpdateOutput"]>(
      await apiClient.PATCH(
        "/v1/workspaces/{workspaceID}/teams/{teamID}/workflow-statuses/{statusID}",
        {
          params: {
            path: { workspaceID, teamID, statusID },
            header: { "Idempotency-Key": key },
          },
          body,
        },
      ),
    );
  },
  async archiveStatus(
    workspaceID: string,
    teamID: string,
    statusID: string,
    body: { expected_version: number; expected_workflow_version: number },
    key: string,
  ) {
    return unwrap<Schemas["ProductWorkflowStatusesArchiveOutput"]>(
      await apiClient.POST(
        "/v1/workspaces/{workspaceID}/teams/{teamID}/workflow-statuses/{statusID}/archive",
        {
          params: {
            path: { workspaceID, teamID, statusID },
            header: { "Idempotency-Key": key },
          },
          body,
        },
      ),
    );
  },
  async reorder(
    workspaceID: string,
    teamID: string,
    body: { status_ids: string[]; expected_workflow_version: number },
    key: string,
  ) {
    return unwrap<Schemas["ProductWorkflowStatusesReorderOutput"]>(
      await apiClient.POST(
        "/v1/workspaces/{workspaceID}/teams/{teamID}/workflow-statuses/reorder",
        {
          params: {
            path: { workspaceID, teamID },
            header: { "Idempotency-Key": key },
          },
          body,
        },
      ),
    );
  },
  async labels(
    workspaceID: string,
    teamID: string,
    archived: boolean,
    after?: string,
  ) {
    return unwrap<Schemas["ProductLabelsListOutput"]>(
      await apiClient.GET("/v1/workspaces/{workspaceID}/labels", {
        params: {
          path: { workspaceID },
          query: { limit: 20, team_id: teamID, archived, after },
        },
      }),
    );
  },
  async createLabel(
    workspaceID: string,
    body: { name: string; color: string; team_id: string },
    key: string,
  ) {
    return unwrap<Schemas["ProductLabelsCreateOutput"]>(
      await apiClient.POST("/v1/workspaces/{workspaceID}/labels", {
        params: { path: { workspaceID }, header: { "Idempotency-Key": key } },
        body,
      }),
    );
  },
  async updateLabel(
    workspaceID: string,
    labelID: string,
    body: { name: string; color: string; expected_version: number },
    key: string,
  ) {
    return unwrap<Schemas["ProductLabelsUpdateOutput"]>(
      await apiClient.PATCH("/v1/workspaces/{workspaceID}/labels/{labelID}", {
        params: {
          path: { workspaceID, labelID },
          header: { "Idempotency-Key": key },
        },
        body,
      }),
    );
  },
  async archiveLabel(
    workspaceID: string,
    labelID: string,
    body: { expected_version: number },
    key: string,
  ) {
    return unwrap<Schemas["ProductLabelsArchiveOutput"]>(
      await apiClient.POST(
        "/v1/workspaces/{workspaceID}/labels/{labelID}/archive",
        {
          params: {
            path: { workspaceID, labelID },
            header: { "Idempotency-Key": key },
          },
          body,
        },
      ),
    );
  },
};
