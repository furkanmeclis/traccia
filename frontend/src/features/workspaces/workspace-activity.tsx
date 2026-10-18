"use client";
import { searchActivityKeys } from "@/features/searchviews/activity-copy";
import { attachmentActivityKeys } from "@/features/attachments/copy";
import { relationActivityKeys } from "@/features/issue-relations/copy";

import Link from "next/link";
import { History, RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { ApiError } from "@/lib/api";
import { useAuth } from "@/providers/auth-provider";
import { useLocale } from "@/providers/locale-provider";
import { useWorkspaceActivity } from "./queries";

const activityActionKeys: Record<string, string> = {
  "comments.created": "comments.activityCreated",
  "comments.replaced": "comments.activityReplaced",
  "comments.patched": "comments.activityPatched",
  "comments.deleted": "comments.activityDeleted",
  "comments.restored": "comments.activityRestored",
  ...searchActivityKeys,
  ...relationActivityKeys,
  ...attachmentActivityKeys,
  "documents.created": "documents.activityCreated",
  "documents.replaced": "documents.activityReplaced",
  "documents.patched": "documents.activityPatched",
  "documents.restored": "documents.activityRestored",
  "issues.created": "issues.event_issues_created",
  "issues.updated": "issues.event_issues_updated",
  "issues.moved": "issues.event_issues_moved",
  "issues.archived": "issues.event_issues_archived",
  "issues.restored": "issues.event_issues_restored",
  "products.created": "products.activity_products_created",
  "products.updated": "products.activity_products_updated",
  "products.archived": "products.activity_products_archived",
  "products.restored": "products.activity_products_restored",
  "product_environments.created":
    "products.activity_product_environments_created",
  "product_environments.updated":
    "products.activity_product_environments_updated",
  "product_environments.archived":
    "products.activity_product_environments_archived",
  "product_environments.restored":
    "products.activity_product_environments_restored",
  "product_projects.updated": "products.activity_product_projects_updated",
  "projects.created": "projects.activity_projects_created",
  "projects.updated": "projects.activity_projects_updated",
  "projects.archived": "projects.activity_projects_archived",
  "projects.restored": "projects.activity_projects_restored",
  "project_milestones.created": "projects.activity_project_milestones_created",
  "project_milestones.updated": "projects.activity_project_milestones_updated",
  "project_milestones.archived":
    "projects.activity_project_milestones_archived",
  "project_milestones.restored":
    "projects.activity_project_milestones_restored",
  "project_resources.created": "projects.activity_project_resources_created",
  "project_resources.updated": "projects.activity_project_resources_updated",
  "project_resources.archived": "projects.activity_project_resources_archived",
  "project_resources.restored": "projects.activity_project_resources_restored",
  "project_teams.updated": "projects.activity_project_teams_updated",

  "workspaces.created": "workspaces.activity_created",
  "workspaces.updated": "workspaces.activity_updated",
  "teams.created": "teams.activity_team_created",
  "teams.updated": "teams.activity_team_updated",
  "teams.archived": "teams.activity_team_archived",
  "workflow_statuses.created": "teams.activity_status_created",
  "workflow_statuses.updated": "teams.activity_status_updated",
  "workflow_statuses.archived": "teams.activity_status_archived",
  "workflow_statuses.reordered": "teams.activity_status_reordered",
  "labels.created": "teams.activity_label_created",
  "labels.updated": "teams.activity_label_updated",
  "labels.archived": "teams.activity_label_archived",
};

export function WorkspaceActivity({ workspaceId }: { workspaceId: string }) {
  const { t, locale } = useLocale();
  const { user } = useAuth();
  const list = useWorkspaceActivity(workspaceId);
  const browsingHistory = (list.data?.pages.length ?? 0) > 1;
  const accessError =
    list.error instanceof ApiError &&
    [401, 403, 404].includes(list.error.status);
  const showItems =
    Boolean(user?.uuid) &&
    !accessError &&
    (!list.isError || list.isFetchNextPageError);
  const items = showItems
    ? (list.data?.pages.flatMap((page) => page.items ?? []) ?? [])
    : [];
  const formatDate = (value: string) => {
    const date = new Date(value);
    return Number.isNaN(date.getTime())
      ? t("workspaces.activity_time_unknown")
      : new Intl.DateTimeFormat(locale, {
          dateStyle: "medium",
          timeStyle: "short",
        }).format(date);
  };
  return (
    <Card>
      <CardHeader className="flex flex-wrap items-start justify-between gap-4 sm:flex-row">
        <div className="space-y-2">
          <CardTitle className="flex items-center gap-2">
            <History className="size-4" />
            {t("workspaces.activity")}
          </CardTitle>
          <CardDescription className="max-w-xl">
            {browsingHistory
              ? t("workspaces.activity_history_description")
              : t("workspaces.activity_description")}
          </CardDescription>
        </div>
        <Button
          variant="outline"
          disabled={list.isFetching}
          onClick={() => list.refetch()}
        >
          <RefreshCw />
          {t("workspaces.activity_refresh")}
        </Button>
      </CardHeader>
      <CardContent className="space-y-4">
        {list.isPending && (
          <p role="status" className="text-muted-foreground text-sm">
            {t("common.loading")}
          </p>
        )}
        {list.isError && (
          <div role="alert" className="space-y-2">
            <p className="text-sm">
              {list.isFetchNextPageError && !accessError
                ? t("workspaces.activity_more_error")
                : t("workspaces.activity_load_error")}
            </p>
            <Button
              variant="outline"
              disabled={list.isFetching}
              onClick={() =>
                list.isFetchNextPageError && !accessError
                  ? list.fetchNextPage()
                  : list.refetch()
              }
            >
              {t("workspaces.retry")}
            </Button>
          </div>
        )}
        {showItems &&
          !list.isPending &&
          !list.isError &&
          items.length === 0 && (
            <p className="text-muted-foreground rounded-lg border border-dashed p-6 text-sm">
              {t("workspaces.activity_empty")}
            </p>
          )}
        {items.length > 0 && (
          <ol className="divide-y">
            {items.map((item) => (
              <li key={item.id} className="space-y-2 py-4 first:pt-0">
                <p className="text-sm font-medium break-words">
                  {t(
                    activityActionKeys[item.event_name] ??
                      "workspaces.activity_changed",
                    { name: item.name },
                  )}
                </p>
                {item.product_id &&
                  [
                    "products",
                    "product_environments",
                    "product_projects",
                  ].includes(item.resource_type) && (
                    <Link
                      className="text-primary text-xs underline underline-offset-4"
                      href={`/w/${workspaceId}/products/${encodeURIComponent(item.product_id)}${item.environment_id ? "#environments" : ""}`}
                    >
                      {t(
                        item.environment_id
                          ? "products.view_environment"
                          : "products.view_product",
                      )}
                    </Link>
                  )}
                {item.project_id &&
                  [
                    "projects",
                    "project_teams",
                    "project_milestones",
                    "project_resources",
                  ].includes(item.resource_type) && (
                    <Link
                      className="text-primary text-xs underline underline-offset-4"
                      href={`/w/${workspaceId}/projects/${encodeURIComponent(item.project_id)}`}
                    >
                      {t("projects.view_project")}
                    </Link>
                  )}
                {item.resource_type === "issues" && item.issue_id && (
                  <Link
                    className="text-primary text-xs underline underline-offset-4"
                    href={`/w/${workspaceId}/issues/${encodeURIComponent(item.issue_id)}`}
                  >
                    {t("issues.view_issue")}
                  </Link>
                )}
                {item.resource_type === "teams" && item.resource_id && (
                  <Link
                    className="text-primary text-xs underline underline-offset-4"
                    href={`/w/${workspaceId}/teams/${encodeURIComponent(item.resource_id)}`}
                  >
                    {t("teams.view_team")}
                  </Link>
                )}
                {item.resource_type === "workflow_statuses" && item.team_id && (
                  <Link
                    className="text-primary text-xs underline underline-offset-4"
                    href={`/w/${workspaceId}/teams/${encodeURIComponent(item.team_id)}#workflow`}
                  >
                    {t("teams.view_workflow")}
                  </Link>
                )}
                {item.resource_type === "labels" && (
                  <Link
                    className="text-primary text-xs underline underline-offset-4"
                    href={
                      item.team_id
                        ? `/w/${workspaceId}/teams/${encodeURIComponent(item.team_id)}#team-labels`
                        : `/w/${workspaceId}/labels`
                    }
                  >
                    {t(
                      item.team_id
                        ? "teams.team_labels"
                        : "teams.shared_labels",
                    )}
                  </Link>
                )}
                <p className="text-muted-foreground flex flex-wrap gap-x-3 gap-y-1 text-xs">
                  <span>
                    {item.actor_id === user?.uuid
                      ? t("workspaces.activity_actor_you")
                      : item.actor_id
                        ? t("workspaces.activity_actor_user")
                        : t("workspaces.activity_actor_system")}
                  </span>
                  <time dateTime={item.occurred_at}>
                    {formatDate(item.occurred_at)}
                  </time>
                </p>
              </li>
            ))}
          </ol>
        )}
        {showItems && list.hasNextPage && !list.isFetchNextPageError && (
          <Button
            variant="outline"
            disabled={list.isFetching}
            onClick={() => list.fetchNextPage()}
          >
            {list.isFetchingNextPage
              ? t("common.loading")
              : t("workspaces.load_more")}
          </Button>
        )}
        {list.isFetching && !list.isPending && !list.isFetchingNextPage && (
          <p role="status" className="text-muted-foreground text-xs">
            {t("workspaces.activity_refreshing")}
          </p>
        )}
      </CardContent>
    </Card>
  );
}
