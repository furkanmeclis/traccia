import type { components } from "@/generated/api";
import { apiClient, unwrap } from "@/lib/api";
import { issueService } from "@/features/issues/issue.service";
import { teamService } from "@/features/teams/team.service";
import { collectRelationPages } from "./snapshot";
type Schemas = components["schemas"];
export type RelationList = Schemas["ProductIssueRelationsListOutput"];
export type Relation = NonNullable<RelationList["items"]>[number];
export type RelationState = RelationList["state"];
export type IssueReference = RelationState["issue"];
export type RelationMutation = Schemas["ProductIssueRelationsSetParentOutput"];
type SetInput = Schemas["ProductIssueRelationsSetParentInput"];
type LinkInput = Schemas["ProductIssueRelationsLinkBlockInput"];
export type RelationAction =
  | "parent"
  | "duplicate"
  | "blocks-link"
  | "blocks-unlink"
  | "related-link"
  | "related-unlink";
export type RelationCommand = {
  path: { workspaceID: string; issueID: string };
} & (
  | { action: "parent" | "duplicate"; body: SetInput }
  | { action: Exclude<RelationAction, "parent" | "duplicate">; body: LinkInput }
);
export const relationService = {
  async page(workspaceID: string, issueID: string, after?: string) {
    return unwrap<RelationList>(
      await apiClient.GET(
        "/v1/workspaces/{workspaceID}/issues/{issueID}/relations",
        {
          params: {
            path: { workspaceID, issueID },
            query: { limit: 100, after, direction: "all", removed: false },
          },
        },
      ),
    );
  },
  async state(workspaceID: string, issueID: string) {
    return (await this.page(workspaceID, issueID)).state;
  },
  async all(workspaceID: string, issueID: string) {
    return collectRelationPages(
      (after) => this.page(workspaceID, issueID, after),
      (page) => page.state.issue.relation_version,
      (page) => page.state.capabilities,
    );
  },
  async candidates(workspaceID: string) {
    const [issues, teams] = await Promise.all([
      collectRelationPages(
        (after) =>
          issueService.list(
            workspaceID,
            { archived: false, q: "", team_id: "", project_id: "" },
            after,
          ),
        () => undefined,
      ),
      collectRelationPages(
        (after) => teamService.list(workspaceID, false, after),
        () => undefined,
      ),
    ]);
    const activeTeams = new Set(teams.items.map((team) => team.id));
    return issues.items.filter(
      (issue) => !issue.archived && activeTeams.has(issue.team_id),
    );
  },
  async history(workspaceID: string, issueID: string, after?: string) {
    return unwrap<Schemas["ProductIssueRelationsHistoryOutput"]>(
      await apiClient.GET(
        "/v1/workspaces/{workspaceID}/issues/{issueID}/relations/history",
        {
          params: {
            path: { workspaceID, issueID },
            query: { limit: 50, after },
          },
        },
      ),
    );
  },
};
export async function executeRelationCommand(
  command: RelationCommand,
  key: string,
) {
  const params = { path: command.path, header: { "Idempotency-Key": key } };
  switch (command.action) {
    case "parent":
      return unwrap<RelationMutation>(
        await apiClient.POST(
          "/v1/workspaces/{workspaceID}/issues/{issueID}/relations/parent",
          { params, body: command.body },
        ),
      );
    case "duplicate":
      return unwrap<RelationMutation>(
        await apiClient.POST(
          "/v1/workspaces/{workspaceID}/issues/{issueID}/relations/duplicate",
          { params, body: command.body },
        ),
      );
    case "blocks-link":
      return unwrap<RelationMutation>(
        await apiClient.POST(
          "/v1/workspaces/{workspaceID}/issues/{issueID}/relations/blocks",
          { params, body: command.body },
        ),
      );
    case "blocks-unlink":
      return unwrap<RelationMutation>(
        await apiClient.POST(
          "/v1/workspaces/{workspaceID}/issues/{issueID}/relations/blocks/unlink",
          { params, body: command.body },
        ),
      );
    case "related-link":
      return unwrap<RelationMutation>(
        await apiClient.POST(
          "/v1/workspaces/{workspaceID}/issues/{issueID}/relations/related",
          { params, body: command.body },
        ),
      );
    case "related-unlink":
      return unwrap<RelationMutation>(
        await apiClient.POST(
          "/v1/workspaces/{workspaceID}/issues/{issueID}/relations/related/unlink",
          { params, body: command.body },
        ),
      );
  }
}
export function isUnlink(command: RelationCommand) {
  return (
    command.action.endsWith("-unlink") || command.body.target_issue_id === null
  );
}
