"use client";
import { useState } from "react";
import Link from "next/link";
import { Button } from "@/components/ui/button";
import { useLocale } from "@/providers/locale-provider";
import { useWorkspace } from "@/features/workspaces/queries";
import { useProjects } from "./queries";
import { ProjectForm } from "./project-form";
import { Section, LoadNotice, PendingOperations } from "./project-ui";
export function ProjectList({ workspaceId }: { workspaceId: string }) {
  const { t } = useLocale();
  const workspace = useWorkspace(workspaceId);
  const allowed = workspace.isSuccess && !workspace.isError;
  const [archived, setArchived] = useState(false);
  const list = useProjects(workspaceId, archived, allowed);
  const ready = allowed && list.isSuccess && !list.isError;
  const manage =
    ready && list.data.pages.every((page) => page.capabilities.manage);
  return (
    <div className="space-y-8">
      <Button type="button" variant="ghost" asChild>
        <Link href={`/w/${workspaceId}`}>{t("projects.back_workspace")}</Link>
      </Button>
      <h1 className="text-3xl font-semibold">{t("projects.title")}</h1>
      <LoadNotice
        pending={workspace.isPending}
        error={workspace.isError}
        retry={() => workspace.refetch()}
      />
      <PendingOperations workspaceId={workspaceId} enabled={Boolean(manage)} />
      {allowed && (
        <>
          <div className="flex flex-wrap gap-3">
            <Button
              type="button"
              variant={archived ? "outline" : "default"}
              aria-pressed={!archived}
              onClick={() => setArchived(false)}
            >
              {t("projects.active")}
            </Button>
            <Button
              type="button"
              variant={archived ? "default" : "outline"}
              aria-pressed={archived}
              onClick={() => setArchived(true)}
            >
              {t("projects.archived")}
            </Button>
          </div>
          <LoadNotice
            pending={list.isPending}
            error={list.isError}
            retry={() => list.refetch()}
          />
          {ready && (
            <>
              <div className="grid gap-4 sm:grid-cols-2">
                {list.data.pages
                  .flatMap((page) => page.items ?? [])
                  .map((project) => (
                    <Link
                      key={project.id}
                      href={`/w/${workspaceId}/projects/${project.id}`}
                      className="hover:border-primary focus-visible:ring-ring space-y-2 rounded-lg border p-5 focus-visible:ring-2"
                    >
                      <h2 className="font-medium break-words">
                        {project.name}
                      </h2>
                      <p className="text-muted-foreground text-sm break-words whitespace-pre-wrap">
                        {project.summary}
                      </p>
                      {project.target_date && (
                        <p className="text-sm">
                          {t("projects.target_date")}:{" "}
                          <time dateTime={project.target_date}>
                            {project.target_date}
                          </time>
                        </p>
                      )}
                      {project.archived && (
                        <p className="text-muted-foreground text-sm">
                          {t("projects.archived")}
                        </p>
                      )}
                    </Link>
                  ))}
              </div>
              {list.data.pages.every((page) => !page.items?.length) && (
                <p>{t("projects.empty")}</p>
              )}
              {list.hasNextPage && (
                <Button
                  type="button"
                  variant="outline"
                  disabled={list.isFetchingNextPage}
                  onClick={() => list.fetchNextPage()}
                >
                  {t("projects.load_more")}
                </Button>
              )}
            </>
          )}
          {manage && (
            <Section title={t("projects.create")}>
              <ProjectForm
                workspaceId={workspaceId}
                reviewLatest={() => list.refetch({ throwOnError: true })}
              />
            </Section>
          )}
        </>
      )}
    </div>
  );
}
