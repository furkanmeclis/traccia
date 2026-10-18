"use client";
import { useState } from "react";
import Link from "next/link";
import { Button } from "@/components/ui/button";
import { useLocale } from "@/providers/locale-provider";
import { useWorkspace } from "@/features/workspaces/queries";
import {
  useProject,
  useProjectChildren,
  useProjectReferences,
} from "./queries";
import { projectService, type Project, type Schemas } from "./project.service";
import { ProjectForm } from "./project-form";
import { ContributorSettings } from "./contributor-settings";
import { MilestoneSettings } from "./milestone-settings";
import { ResourceSettings } from "./resource-settings";
import {
  PendingOperations,
  Section,
  LoadNotice,
  MutationNotice,
  useProjectMutation,
} from "./project-ui";
function ProjectSettings({
  workspaceId,
  project,
  manage,
  restore,
  reviewLatest,
}: {
  workspaceId: string;
  project: Project;
  manage: boolean;
  restore: boolean;
  reviewLatest: () => Promise<unknown>;
}) {
  const { t } = useLocale();
  const [editing, setEditing] = useState(false);
  const lifecycle = useProjectMutation({
    workspaceId,
    projectId: project.id,
    operationId: "project-lifecycle",
    operationLabel: t(
      project.archived
        ? "projects.restore_project"
        : "projects.archive_project",
    ),
    reviewLatest,
    run: (
      input: {
        action: "archive" | "restore";
        body: Schemas["ProductProjectsArchiveInput"];
      },
      key,
    ) =>
      input.action === "restore"
        ? projectService.restoreProject(
            workspaceId,
            project.id,
            input.body,
            key,
          )
        : projectService.archiveProject(
            workspaceId,
            project.id,
            input.body,
            key,
          ),
  });
  return (
    <Section title={t("projects.settings")}>
      {editing ? (
        <ProjectForm
          workspaceId={workspaceId}
          project={project}
          canManage={manage}
          reviewLatest={reviewLatest}
          onDone={() => setEditing(false)}
        />
      ) : (
        <div className="flex flex-wrap gap-3">
          {manage && (
            <Button
              type="button"
              variant="outline"
              disabled={lifecycle.locked}
              onClick={() => setEditing(true)}
            >
              {t("projects.edit_project")}
            </Button>
          )}
          {((manage && !project.archived) || (restore && project.archived)) && (
            <Button
              type="button"
              variant="outline"
              disabled={lifecycle.locked}
              onClick={() =>
                lifecycle.submit({
                  action: project.archived ? "restore" : "archive",
                  body: { expected_version: project.version },
                })
              }
            >
              {t(
                project.archived
                  ? "projects.restore_project"
                  : "projects.archive_project",
              )}
            </Button>
          )}
        </div>
      )}
      <MutationNotice
        canRetry={manage || restore}
        mutation={lifecycle}
        onReconcile={() => setEditing(false)}
      />
    </Section>
  );
}
export function ProjectDetail({
  workspaceId,
  projectId,
}: {
  workspaceId: string;
  projectId: string;
}) {
  const { t } = useLocale();
  const workspace = useWorkspace(workspaceId);
  const allowed = workspace.isSuccess && !workspace.isError;
  const project = useProject(workspaceId, projectId, allowed);
  const ready = allowed && project.isSuccess && !project.isError;
  const references = useProjectReferences(workspaceId, ready);
  const lead =
    references.isSuccess && !references.isError
      ? references.data.allMembers.find(
          (member) => member.id === project.data?.project.lead_user_id,
        )
      : undefined;
  const [archived, setArchived] = useState(false);
  const children = useProjectChildren(workspaceId, projectId, archived, ready);
  const childrenReady =
    ready &&
    children.isSuccess &&
    !children.isError &&
    children.data.projectVersion === project.data.project.version;
  async function reviewLatest() {
    const [latestProject, latestChildren] = await Promise.all([
      project.refetch({ throwOnError: true }),
      children.refetch({ throwOnError: true }),
      workspace.refetch({ throwOnError: true }),
    ]);
    if (
      !latestProject.data ||
      !latestChildren.data ||
      latestProject.data.project.version !== latestChildren.data.projectVersion
    )
      throw new Error("PROJECT_SNAPSHOT_CHANGED");
  }
  const current = project.data?.project;
  return (
    <div className="space-y-8">
      <Button type="button" variant="ghost" asChild>
        <Link href={`/w/${workspaceId}/projects`}>
          {t("projects.back_projects")}
        </Link>
      </Button>
      <LoadNotice
        pending={workspace.isPending}
        error={workspace.isError}
        retry={() => workspace.refetch()}
      />
      <PendingOperations
        workspaceId={workspaceId}
        projectId={projectId}
        enabled={
          childrenReady &&
          (project.data.capabilities.manage ||
            project.data.capabilities.restore)
        }
      />
      {allowed && (
        <LoadNotice
          pending={project.isPending}
          error={project.isError}
          retry={() => project.refetch()}
        />
      )}
      {current && project.data && (
        <div className="space-y-8" hidden={!ready}>
          <header className="space-y-3">
            <p className="text-primary text-sm font-medium">
              {t("projects.project")}
            </p>
            <h1 className="text-3xl font-semibold break-words">
              {current.name}
            </h1>
            <p className="text-muted-foreground break-words whitespace-pre-wrap">
              {current.summary}
            </p>
            {current.archived && (
              <p className="text-muted-foreground">{t("projects.archived")}</p>
            )}
            <dl className="grid gap-4 text-sm sm:grid-cols-2">
              <div>
                <dt className="text-muted-foreground">{t("projects.lead")}</dt>
                <dd>
                  {!current.lead_user_id
                    ? t("projects.no_lead")
                    : lead
                      ? `${lead.name} ${lead.surname}`.trim() ||
                        t("projects.member")
                      : t("projects.unavailable_lead")}
                </dd>
              </div>
              {(["start_date", "target_date"] as const).map((field) => (
                <div key={field}>
                  <dt className="text-muted-foreground">
                    {t(
                      field === "start_date"
                        ? "projects.start_date"
                        : "projects.target_date",
                    )}
                  </dt>
                  <dd>
                    {current[field] ? (
                      <time dateTime={current[field]}>{current[field]}</time>
                    ) : (
                      t("projects.no_date")
                    )}
                  </dd>
                </div>
              ))}
            </dl>
          </header>
          {(project.data.capabilities.manage ||
            project.data.capabilities.restore) && (
            <ProjectSettings
              workspaceId={workspaceId}
              project={current}
              manage={
                ready && project.data.capabilities.manage && !current.archived
              }
              restore={ready && project.data.capabilities.restore}
              reviewLatest={reviewLatest}
            />
          )}
          <div className="flex flex-wrap gap-3">
            <Button
              type="button"
              variant={archived ? "outline" : "default"}
              aria-pressed={!archived}
              onClick={() => setArchived(false)}
            >
              {t("projects.active_children")}
            </Button>
            <Button
              type="button"
              variant={archived ? "default" : "outline"}
              aria-pressed={archived}
              onClick={() => setArchived(true)}
            >
              {t("projects.archived_children")}
            </Button>
          </div>
          <LoadNotice
            pending={children.isPending}
            error={children.isError}
            retry={() => {
              void reviewLatest().catch(() => {});
            }}
          />
          {children.isSuccess && !children.isError && !childrenReady && (
            <div role="alert" className="space-y-3">
              <p>{t("projects.snapshot_changed")}</p>
              <Button
                type="button"
                variant="outline"
                onClick={() => {
                  void reviewLatest().catch(() => {});
                }}
              >
                {t("projects.retry")}
              </Button>
            </div>
          )}
          {children.data && (
            <div className="space-y-8" hidden={children.isError}>
              <ContributorSettings
                workspaceId={workspaceId}
                projectId={projectId}
                teams={children.data.contributors.items}
                version={current.version}
                canManage={
                  childrenReady &&
                  project.data.capabilities.manage &&
                  children.data.contributors.capabilities.manage &&
                  !current.archived
                }
                reviewLatest={reviewLatest}
              />
              <MilestoneSettings
                workspaceId={workspaceId}
                projectId={projectId}
                version={current.version}
                items={children.data.milestones.items}
                showCreate={
                  project.data.capabilities.manage &&
                  children.data.milestones.capabilities.manage &&
                  !current.archived
                }
                canManage={
                  childrenReady &&
                  project.data.capabilities.manage &&
                  children.data.milestones.capabilities.manage &&
                  !current.archived
                }
                canRestore={
                  childrenReady &&
                  children.data.milestones.capabilities.restore &&
                  !current.archived
                }
                reviewLatest={reviewLatest}
              />
              <ResourceSettings
                workspaceId={workspaceId}
                projectId={projectId}
                version={current.version}
                items={children.data.resources.items}
                showCreate={
                  project.data.capabilities.manage &&
                  children.data.resources.capabilities.manage &&
                  !current.archived
                }
                canManage={
                  childrenReady &&
                  project.data.capabilities.manage &&
                  children.data.resources.capabilities.manage &&
                  !current.archived
                }
                canRestore={
                  childrenReady &&
                  children.data.resources.capabilities.restore &&
                  !current.archived
                }
                reviewLatest={reviewLatest}
              />
            </div>
          )}
        </div>
      )}
    </div>
  );
}
