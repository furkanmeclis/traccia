"use client";
import {
  useIssueDrawer,
  useDrawerRows,
} from "@/features/issue-drawer/issue-drawer";
import { plainIssueClick } from "@/features/issue-drawer/navigation";
import { CommentPendingOperations } from "@/features/comments/comment-operation";
import { AttachmentShelf } from "@/features/attachments/attachment-panel";
import { issueFieldKeys, issuePriorityKeys } from "./issue-copy";
import { useId, useState } from "react";
import Link from "next/link";
import { Button } from "@/components/ui/button";
import { TextLimitInput } from "@/components/ui/text-limit-input";
import { Label } from "@/components/ui/label";
import { useLocale } from "@/providers/locale-provider";
import { useWorkspace } from "@/features/workspaces/queries";
import { useIssues, useIssueReferences } from "./queries";
import { RelationPendingOperations } from "@/features/issue-relations/relation-operation";
import { DocumentPendingOperations } from "@/features/documents/document-operation";
import { LoadNotice, PendingOperations } from "./issue-ui";
export function IssueList({
  workspaceId,
  initialTeam = "",
  initialProject = "",
}: {
  workspaceId: string;
  initialTeam?: string;
  initialProject?: string;
}) {
  const { t } = useLocale();
  const id = useId();
  const drawer = useIssueDrawer();
  const workspace = useWorkspace(workspaceId);
  const allowed = workspace.isSuccess && !workspace.isError;
  const [filters, setFilters] = useState({
    archived: false,
    q: "",
    team_id: initialTeam,
    project_id: initialProject,
  });
  const list = useIssues(workspaceId, filters, allowed);
  const references = useIssueReferences(workspaceId, allowed);
  const ready =
    allowed &&
    !workspace.isFetching &&
    list.isSuccess &&
    !list.isError &&
    !list.isFetching;
  useDrawerRows(
    allowed && list.isSuccess && !list.isError
      ? list.data.pages.flatMap((page) =>
          (page.items ?? []).map((issue) => issue.id),
        )
      : [],
  );
  const manage =
    ready && list.data.pages.every((page) => page.capabilities.manage);
  const showCreate =
    list.data?.pages.some((page) => page.capabilities.manage) ?? false;
  return (
    <div className="space-y-8">
      <Button type="button" variant="ghost" asChild>
        <Link href={`/w/${workspaceId}`}>{t("issues.back_workspace")}</Link>
      </Button>
      <h1
        id="issue-list-heading"
        tabIndex={-1}
        className="text-3xl font-semibold outline-none"
      >
        {t("issues.title")}
      </h1>
      <LoadNotice
        pending={workspace.isPending}
        error={workspace.isError}
        retry={() => workspace.refetch()}
      />
      <PendingOperations workspaceId={workspaceId} canRun={() => !!manage} />
      <RelationPendingOperations workspaceId={workspaceId} />
      <DocumentPendingOperations workspaceId={workspaceId} />
      <CommentPendingOperations workspaceID={workspaceId} />
      <AttachmentShelf workspaceID={workspaceId} />
      <div hidden={!allowed} className="space-y-8">
        <div className="flex flex-wrap gap-3">
          {[false, true].map((archived) => (
            <Button
              key={String(archived)}
              type="button"
              variant={filters.archived === archived ? "default" : "outline"}
              aria-pressed={filters.archived === archived}
              onClick={() => setFilters({ ...filters, archived })}
            >
              {t(archived ? "issues.archived" : "issues.active")}
            </Button>
          ))}
        </div>
        <div className="grid gap-4 sm:grid-cols-3">
          <div className="space-y-2">
            <Label htmlFor={`${id}-search`}>{t("issues.search")}</Label>
            <TextLimitInput
              id={`${id}-search`}
              value={filters.q}
              maxCodePoints={255}
              onChange={(event) =>
                setFilters({ ...filters, q: event.target.value })
              }
            />
          </div>
          {(["team_id", "project_id"] as const).map((field) => (
            <div className="space-y-2" key={field}>
              <Label htmlFor={`${id}-${field}`}>
                {t(issueFieldKeys[field])}
              </Label>
              <select
                id={`${id}-${field}`}
                className="border-input bg-background h-10 w-full rounded-md border px-3 text-sm"
                value={filters[field]}
                disabled={
                  !references.isSuccess ||
                  references.isError ||
                  references.isFetching
                }
                onChange={(event) =>
                  setFilters({ ...filters, [field]: event.target.value })
                }
              >
                <option value="">{t("issues.all")}</option>
                {(field === "team_id"
                  ? references.data?.teams
                  : references.data?.projects
                )?.map((item) => (
                  <option key={item.id} value={item.id}>
                    {item.name}
                    {item.archived ? ` · ${t("issues.archived")}` : ""}
                  </option>
                ))}
              </select>
            </div>
          ))}
        </div>
        <LoadNotice
          pending={references.isPending}
          error={references.isError}
          retry={() => references.refetch()}
        />
        <LoadNotice
          pending={list.isPending}
          error={list.isError}
          retry={() => list.refetch()}
        />
        {list.isSuccess && !list.isError && (
          <>
            <div className="grid gap-4 sm:grid-cols-2">
              {list.data.pages
                .flatMap((page) => page.items ?? [])
                .map((issue) => (
                  <Link
                    key={issue.id}
                    data-issue-row={issue.id}
                    onClick={(event) => {
                      if (drawer && plainIssueClick(event)) {
                        event.preventDefault();
                        drawer.openIssue(issue.id, event.currentTarget);
                      }
                    }}
                    href={`/w/${workspaceId}/issues/${issue.id}`}
                    className="hover:border-primary focus-visible:ring-ring space-y-2 rounded-lg border p-5 focus-visible:ring-2"
                  >
                    <p className="text-muted-foreground font-mono text-sm">
                      {issue.identifier}
                    </p>
                    <h2 className="font-medium break-words">{issue.title}</h2>
                    <p className="text-muted-foreground text-sm">
                      {t(issuePriorityKeys[issue.priority])}
                    </p>
                  </Link>
                ))}
            </div>
            {list.data.pages.every((page) => !page.items?.length) && (
              <p>{t("issues.empty")}</p>
            )}
            {list.hasNextPage && (
              <Button
                type="button"
                variant="outline"
                disabled={list.isFetchingNextPage}
                onClick={() => list.fetchNextPage()}
              >
                {t("issues.load_more")}
              </Button>
            )}
          </>
        )}
        <div hidden={!showCreate}>
          <Button
            type="button"
            disabled={!manage || !drawer?.canCreate}
            onClick={(event) =>
              drawer?.openCreate(
                event.currentTarget,
                initialTeam || undefined,
                initialProject || undefined,
              )
            }
          >
            {t("issues.create")}
          </Button>
        </div>
      </div>
    </div>
  );
}
