import type { components } from "@/generated/api";
import { viewTransport, updateViewTransport } from "./view-transport";
import { withinDomainBudget } from "./query-import";
import { apiClient, unwrap, parseApiError } from "@/lib/api";
import {
  validateQuery,
  validateColumns,
  validText,
  type Query,
  type Column,
} from "./query";
import type { SearchResource } from "./capture-state";
type S = components["schemas"];
export type SearchConfig = S["ProductSearchQueriesConfigOutput"];
export type IssuePage = S["ProductIssueQueriesCreateOutput"];
export type ProjectPage = S["ProductProjectQueriesCreateOutput"];
export type TeamPage = S["ProductTeamQueriesCreateOutput"];
export type SearchPage = IssuePage | ProjectPage | TeamPage;
export type SavedView = S["ProductSavedViewsGetOutput"]["view"];
export type ColumnPreferences = S["ProductColumnPreferencesGetOutput"];
export type CaptureBody = {
  request_id: string;
  query: Query;
  ttl_seconds: number | null;
  limit: number;
};
export type SearchReadCommand = {
  workspaceID: string;
  resource: SearchResource;
  path: string;
  body: CaptureBody;
};
export type ViewDefinition = {
  resource: SearchResource;
  issue: Query | null;
  project: Query | null;
  team: Query | null;
};
export type ViewValues = {
  name: string;
  definition: ViewDefinition;
  columns: Column[];
};
export type ViewCommand = { workspaceID: string; path: string } & (
  | { action: "view.create"; body: ViewValues }
  | {
      action: "view.update";
      viewID: string;
      body: ViewValues & { expected_revision: string };
    }
  | {
      action: "view.delete" | "view.restore";
      viewID: string;
      body: { expected_revision: string };
    }
  | {
      action: "columns.set";
      body: {
        resource: SearchResource;
        expected_revision: string | null;
        columns: Column[];
      };
    }
);
export function definition(
  resource: SearchResource,
  query: Query,
): ViewDefinition {
  return {
    resource,
    issue: resource === "issue" ? query : null,
    project: resource === "project" ? query : null,
    team: resource === "team" ? query : null,
  };
}
export function definitionQuery(value: ViewDefinition): Query {
  const query = value[value.resource];
  if (
    !query ||
    !validateQuery(value.resource, query) ||
    (["issue", "project", "team"] as const).some(
      (r) => r !== value.resource && value[r] !== null,
    )
  )
    throw new Error("SEARCH_INVALID_DEFINITION");
  return query;
}
function issueBody(
  body: CaptureBody,
): body is S["ProductIssueQueriesCreateInput"] {
  return validateQuery("issue", body.query);
}
function projectBody(
  body: CaptureBody,
): body is S["ProductProjectQueriesCreateInput"] {
  return validateQuery("project", body.query);
}
function teamBody(
  body: CaptureBody,
): body is S["ProductTeamQueriesCreateInput"] {
  return validateQuery("team", body.query);
}
function viewValues(
  value: ViewValues,
): value is S["ProductSavedViewsCreateInput"] {
  try {
    return (
      value.name.length > 0 &&
      validText(value.name, 120) &&
      validateColumns(value.definition.resource, value.columns) &&
      validateQuery(
        value.definition.resource,
        definitionQuery(value.definition),
      )
    );
  } catch {
    return false;
  }
}
function columnValues(value: {
  resource: SearchResource;
  expected_revision: string | null;
  columns: Column[];
}): value is S["ProductColumnPreferencesSetInput"] {
  return validateColumns(value.resource, value.columns);
}
function updateValues(
  value: ViewValues & { expected_revision: string },
): value is S["ProductSavedViewsUpdateInput"] {
  return viewValues(value) && typeof value.expected_revision === "string";
}
function assertDomainBudget(value: unknown) {
  if (!withinDomainBudget(value))
    throw parseApiError(400, {
      success: false,
      error: { code: "VALIDATION_ERROR", message: "" },
    });
}
export const searchService = {
  async config(workspaceID: string, signal?: AbortSignal) {
    return unwrap<SearchConfig>(
      await apiClient.GET("/v1/workspaces/{workspaceID}/search/config", {
        params: { path: { workspaceID } },
        signal,
      }),
    );
  },
  async capture(
    command: SearchReadCommand,
    signal?: AbortSignal,
  ): Promise<SearchPage> {
    const { workspaceID, body, resource } = command;
    assertDomainBudget(body);
    if (
      command.path !==
      `/v1/workspaces/${workspaceID}/search/${resource === "issue" ? "issues" : resource === "project" ? "projects" : "teams"}/sessions`
    )
      throw new Error("SEARCH_COMMAND_PATH");
    if (resource === "issue" && issueBody(body))
      return unwrap<IssuePage>(
        await apiClient.POST(
          "/v1/workspaces/{workspaceID}/search/issues/sessions",
          { params: { path: { workspaceID } }, body, signal },
        ),
      );
    if (resource === "project" && projectBody(body))
      return unwrap<ProjectPage>(
        await apiClient.POST(
          "/v1/workspaces/{workspaceID}/search/projects/sessions",
          { params: { path: { workspaceID } }, body, signal },
        ),
      );
    if (resource === "team" && teamBody(body))
      return unwrap<TeamPage>(
        await apiClient.POST(
          "/v1/workspaces/{workspaceID}/search/teams/sessions",
          { params: { path: { workspaceID } }, body, signal },
        ),
      );
    throw new Error("SEARCH_INVALID_QUERY");
  },
  async page(
    workspaceID: string,
    resource: SearchResource,
    sessionID: string,
    after: string,
    signal?: AbortSignal,
  ): Promise<SearchPage> {
    const params = {
      path: { workspaceID, sessionID },
      query: { limit: 100, after },
    };
    if (resource === "issue")
      return unwrap<IssuePage>(
        await apiClient.GET(
          "/v1/workspaces/{workspaceID}/search/issues/sessions/{sessionID}",
          { params, signal },
        ),
      );
    if (resource === "project")
      return unwrap<ProjectPage>(
        await apiClient.GET(
          "/v1/workspaces/{workspaceID}/search/projects/sessions/{sessionID}",
          { params, signal },
        ),
      );
    return unwrap<TeamPage>(
      await apiClient.GET(
        "/v1/workspaces/{workspaceID}/search/teams/sessions/{sessionID}",
        { params, signal },
      ),
    );
  },
  async views(
    workspaceID: string,
    resource: SearchResource,
    includeDeleted: boolean,
    after?: string,
    signal?: AbortSignal,
  ) {
    return unwrap<S["ProductSavedViewsListOutput"]>(
      await apiClient.GET("/v1/workspaces/{workspaceID}/saved-views", {
        params: {
          path: { workspaceID },
          query: {
            resource,
            include_deleted: includeDeleted,
            limit: 100,
            after,
          },
        },
        signal,
      }),
    );
  },
  async view(workspaceID: string, viewID: string, signal?: AbortSignal) {
    return unwrap<S["ProductSavedViewsGetOutput"]>(
      await apiClient.GET("/v1/workspaces/{workspaceID}/saved-views/{viewID}", {
        params: { path: { workspaceID, viewID } },
        signal,
      }),
    );
  },
  async columns(
    workspaceID: string,
    resource: SearchResource,
    signal?: AbortSignal,
  ) {
    return unwrap<ColumnPreferences>(
      await apiClient.GET("/v1/workspaces/{workspaceID}/column-preferences", {
        params: { path: { workspaceID }, query: { resource } },
        signal,
      }),
    );
  },
};
export async function executeViewCommand(
  command: ViewCommand,
  key: string,
  signal?: AbortSignal,
): Promise<{ view: SavedView } | ColumnPreferences> {
  const workspaceID = command.workspaceID,
    header = { "Idempotency-Key": key };
  const base = `/v1/workspaces/${workspaceID}`;
  if (command.action === "view.create") {
    assertDomainBudget(command.body);
    if (command.path !== base + "/saved-views" || !viewValues(command.body))
      throw new Error("SEARCH_INVALID_COMMAND");
    return unwrap<S["ProductSavedViewsCreateOutput"]>(
      await apiClient.POST("/v1/workspaces/{workspaceID}/saved-views", {
        params: { path: { workspaceID }, header },
        ...viewTransport(command.body),
        signal,
      }),
    );
  }
  if (command.action === "view.update") {
    assertDomainBudget({ ...command.body, view_id: command.viewID });
    if (
      command.path !== base + `/saved-views/${command.viewID}/update` ||
      !updateValues(command.body)
    )
      throw new Error("SEARCH_INVALID_COMMAND");
    return unwrap<S["ProductSavedViewsUpdateOutput"]>(
      await apiClient.POST(
        "/v1/workspaces/{workspaceID}/saved-views/{viewID}/update",
        {
          params: { path: { workspaceID, viewID: command.viewID }, header },
          ...updateViewTransport(command.body),
          signal,
        },
      ),
    );
  }
  if (command.action === "view.delete" || command.action === "view.restore") {
    const params = { path: { workspaceID, viewID: command.viewID }, header };
    if (
      command.path !==
      base +
        `/saved-views/${command.viewID}/${command.action === "view.delete" ? "delete" : "restore"}`
    )
      throw new Error("SEARCH_COMMAND_PATH");
    if (command.action === "view.delete")
      return unwrap<S["ProductSavedViewsDeleteOutput"]>(
        await apiClient.POST(
          "/v1/workspaces/{workspaceID}/saved-views/{viewID}/delete",
          { params, body: command.body, signal },
        ),
      );
    return unwrap<S["ProductSavedViewsRestoreOutput"]>(
      await apiClient.POST(
        "/v1/workspaces/{workspaceID}/saved-views/{viewID}/restore",
        { params, body: command.body, signal },
      ),
    );
  }
  if (command.action === "columns.set") {
    if (
      command.path !== base + "/column-preferences" ||
      !columnValues(command.body)
    )
      throw new Error("SEARCH_INVALID_COMMAND");
    return unwrap<S["ProductColumnPreferencesSetOutput"]>(
      await apiClient.POST("/v1/workspaces/{workspaceID}/column-preferences", {
        params: { path: { workspaceID }, header },
        body: command.body,
        signal,
      }),
    );
  }
  throw new Error("SEARCH_INVALID_COMMAND");
}
