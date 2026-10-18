import type { components } from "@/generated/api";
import { apiClient, unwrap, parseApiError } from "@/lib/api";
import { validateQuery } from "@/features/searchviews/query";
import {
  definitionQuery,
  type ViewDefinition,
} from "@/features/searchviews/search.service";
import { withinDomainBudget } from "@/features/searchviews/query-import";
import { portfolioTransport } from "./portfolio-transport";
import { collectSelector, validID } from "./scope";
type S = components["schemas"];
export type PersonalConfig = S["ProductPersonalWorkConfigOutput"];
export type Favorite = S["ProductFavoritesListOutput"]["items"][number];
export type FavoriteBody = S["ProductFavoritesSetInput"];
export type FavoriteCommand = {
  action: "favorite.set";
  workspaceID: string;
  path: string;
  body: FavoriteBody;
};
export type PortfolioBody = S["ProductPersonalPortfoliosCreateInput"];
export type PortfolioPage = S["ProductPersonalPortfoliosCreateOutput"];
export type PortfolioValues = Omit<PortfolioBody, "definition"> & {
  definition: ViewDefinition;
};
export type PortfolioCommand = {
  workspaceID: string;
  path: string;
  body: PortfolioValues;
};
export function portfolioBody(value: PortfolioValues): value is PortfolioBody {
  try {
    return (
      validID(value.request_id) &&
      value.workspace_ids.length > 0 &&
      value.workspace_ids.length <= 32 &&
      value.workspace_ids.every(validID) &&
      new Set(value.workspace_ids.map((id) => id.toLowerCase())).size ===
        value.workspace_ids.length &&
      validateQuery(
        value.definition.resource,
        definitionQuery(value.definition),
      ) &&
      (!value.assigned_to_me || value.definition.resource === "issue") &&
      Number.isInteger(value.limit) &&
      value.limit >= 1 &&
      value.limit <= 100 &&
      (value.ttl_seconds === null ||
        (Number.isInteger(value.ttl_seconds) &&
          value.ttl_seconds >= 60 &&
          value.ttl_seconds <= 86400))
    );
  } catch {
    return false;
  }
}
function validation() {
  return parseApiError(400, {
    success: false,
    error: { code: "VALIDATION_ERROR", message: "" },
  });
}
export const personalService = {
  async config(workspaceID: string, signal?: AbortSignal) {
    return unwrap<PersonalConfig>(
      await apiClient.GET("/v1/workspaces/{workspaceID}/personal-work/config", {
        params: { path: { workspaceID } },
        signal,
      }),
    );
  },
  async favorites(workspaceID: string, signal?: AbortSignal) {
    let manage = true;
    const items = await collectSelector(
      async (after) => {
        const page = await unwrap<S["ProductFavoritesListOutput"]>(
          await apiClient.GET("/v1/workspaces/{workspaceID}/favorites", {
            params: {
              path: { workspaceID },
              query: { include_inactive: true, limit: 100, after },
            },
            signal,
          }),
        );
        manage &&= page.capabilities.manage;
        return page;
      },
      (row) => row.workspace_id.toLowerCase() === workspaceID.toLowerCase(),
    );
    return { items, capabilities: { manage } };
  },
  async setFavorite(
    command: FavoriteCommand,
    key: string,
    signal?: AbortSignal,
  ) {
    const expected = `/v1/workspaces/${command.workspaceID}/favorites`;
    if (command.path !== expected || !withinDomainBudget(command.body))
      throw validation();
    return unwrap<S["ProductFavoritesSetOutput"]>(
      await apiClient.POST("/v1/workspaces/{workspaceID}/favorites", {
        params: {
          path: { workspaceID: command.workspaceID },
          header: { "Idempotency-Key": key },
        },
        body: command.body,
        signal,
      }),
    );
  },
  async capture(command: PortfolioCommand, signal?: AbortSignal) {
    const body = structuredClone(command.body);
    if (
      command.path !==
        `/v1/workspaces/${command.workspaceID}/personal-work/sessions` ||
      !portfolioBody(body) ||
      !withinDomainBudget(body)
    )
      throw validation();
    const transport = portfolioTransport(body);
    return unwrap<PortfolioPage>(
      await apiClient.POST(
        "/v1/workspaces/{workspaceID}/personal-work/sessions",
        {
          params: { path: { workspaceID: command.workspaceID } },
          ...transport,
          signal,
        },
      ),
    );
  },
  async page(
    workspaceID: string,
    sessionID: string,
    after?: string,
    signal?: AbortSignal,
  ) {
    return unwrap<S["ProductPersonalPortfoliosGetOutput"]>(
      await apiClient.GET(
        "/v1/workspaces/{workspaceID}/personal-work/sessions/{sessionID}",
        {
          params: {
            path: { workspaceID, sessionID },
            query: { limit: 100, after },
          },
          signal,
        },
      ),
    );
  },
};
