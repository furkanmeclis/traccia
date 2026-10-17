"use client";

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
