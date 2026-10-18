"use client";
import { CommentPanel } from "@/features/comments/comment-panel";
import { IssueTimeline } from "@/features/comments/issue-timeline";
import { AttachmentPanel } from "@/features/attachments/attachment-panel";
import { useAuth } from "@/providers/auth-provider";
import { useDrawerDraft } from "@/features/issue-drawer/draft-context";
import Link from "next/link";
import { Button } from "@/components/ui/button";
import { useWorkspace } from "@/features/workspaces/queries";
import { useLocale } from "@/providers/locale-provider";
import { useIssue } from "./queries";
import { IssueMetadata } from "./issue-metadata";
import { IssueForm, type IssueFormSnapshot } from "./issue-form";
import { DocumentPanel } from "@/features/documents/document-panel";
import { IssueRelations } from "@/features/issue-relations/relation-panel";
import { IssueHistory } from "./issue-history";
import {
  LoadNotice,
  Section,
  PendingOperations,
  MutationNotice,
  useIssueMutation,
} from "./issue-ui";
import type { Issue } from "./issue.service";
function RetainedIssueForm({
  generation,
  ...props
}: React.ComponentProps<typeof IssueForm> & { generation: number }) {
  const issue = props.issue;
  const [snapshot, onChange] = useDrawerDraft<IssueFormSnapshot>(
    `metadata.${generation}.${props.move ? "move" : "edit"}`,
    () => ({
      baselineVersion: issue?.version,
      values: {
        title: issue?.title ?? "",
        priority: issue?.priority ?? "none",
        team_id: issue?.team_id ?? "",
        status_id: issue?.status_id ?? "",
        assignee_id: issue?.assignee_id ?? "",
        project_id: issue?.project_id ?? "",
        milestone_id: issue?.milestone_id ?? "",
        start_date: issue?.start_date ?? "",
        target_date: issue?.target_date ?? "",
        effort: issue?.effort == null ? "" : String(issue.effort),
        label_ids: [...(issue?.label_ids ?? [])],
      },
    }),
  );
  return <IssueForm {...props} controlledDraft={{ snapshot, onChange }} />;
}
function IssueSettings({
  workspaceId,
  issue,
  manage,
  canMove,
  restore,
  reviewLatest,
}: {
  workspaceId: string;
  issue: Issue;
  manage: boolean;
  canMove: boolean;
  restore: boolean;
  reviewLatest: () => Promise<unknown>;
}) {
  const { t } = useLocale();
  const [editing, setEditing] = useDrawerDraft<"edit" | "move" | null>(
    "metadata.editing",
    null,
  );
  const [generation, setGeneration] = useDrawerDraft("metadata.generation", 0);
  function done() {
    setEditing(null);
    setGeneration((n) => n + 1);
  }
  const lifecycle = useIssueMutation({
    workspaceId,
    issueId: issue.id,
    operationId: "issue-lifecycle",
    operationLabel: t(issue.archived ? "issues.restore" : "issues.archive"),
    reviewLatest,
  });
  return (
    <Section title={t("issues.settings")}>
      {/* An active draft survives reference refreshes and sibling mutations. */}
      {editing ? (
        <RetainedIssueForm
          key={generation}
          generation={generation}
          workspaceId={workspaceId}
          issue={issue}
          move={editing === "move"}
          canManage={editing === "move" ? canMove : manage}
          reviewLatest={reviewLatest}
          onDone={done}
        />
      ) : (
        <div className="flex flex-wrap gap-3">
          {manage && !issue.archived && (
            <>
              <Button
                type="button"
                variant="outline"
                data-issue-edit
                disabled={lifecycle.locked}
                onClick={() => setEditing("edit")}
              >
                {t("issues.edit")}
              </Button>
            </>
          )}
          {canMove && !issue.archived && (
            <Button
              type="button"
              variant="outline"
              disabled={lifecycle.locked}
              onClick={() => setEditing("move")}
            >
              {t("issues.move")}
            </Button>
          )}
          {((manage && !issue.archived) || (restore && issue.archived)) && (
            <Button
              type="button"
              variant="outline"
              disabled={lifecycle.locked}
              onClick={() =>
                lifecycle.submit({
                  action: issue.archived ? "issue.restore" : "issue.archive",
                  path: { workspaceID: workspaceId, issueID: issue.id },
                  body: { expected_version: issue.version },
                })
              }
            >
              {t(issue.archived ? "issues.restore" : "issues.archive")}
            </Button>
          )}
        </div>
      )}
      <MutationNotice
        canRetry={
          lifecycle.command?.action === "issue.restore" ? restore : manage
        }
        mutation={lifecycle}
        onReconcile={done}
      />
    </Section>
  );
}
export function IssueDetail({
  workspaceId,
  issueId,
  drawer = false,
}: {
  workspaceId: string;
  issueId: string;
  drawer?: boolean;
}) {
  const { t } = useLocale();
  const { user } = useAuth();
  const workspace = useWorkspace(workspaceId);
  const allowed = workspace.isSuccess && !workspace.isError;
  const query = useIssue(workspaceId, issueId, allowed);
  const ready = allowed && query.isSuccess && !query.isError;
  const current = query.data?.issue;
  async function reviewLatest() {
    await Promise.all([
      workspace.refetch({ throwOnError: true }),
      query.refetch({ throwOnError: true }),
    ]);
  }
  const writable = ready && !workspace.isFetching && !query.isFetching;
  return (
    <div className="space-y-8">
      {!drawer && (
        <Button type="button" variant="ghost" asChild>
          <Link href={`/w/${workspaceId}/issues`}>
            {t("issues.back_issues")}
          </Link>
        </Button>
      )}
      <LoadNotice
        pending={workspace.isPending}
        error={workspace.isError}
        retry={() => workspace.refetch()}
      />
      <PendingOperations
        workspaceId={workspaceId}
        canRun={() =>
          writable &&
          !!(
            query.data?.capabilities.manage ||
            query.data?.capabilities.restore ||
            query.data?.capabilities.move
          )
        }
      />
      {allowed && (
        <LoadNotice
          pending={query.isPending}
          error={query.isError}
          retry={() => query.refetch()}
        />
      )}
      {current && (
        <div hidden={!ready} className="space-y-8">
          <header className="space-y-3">
            <Link
              className="text-primary font-mono underline"
              href={`/w/${workspaceId}/issues/by-identifier/${encodeURIComponent(current.identifier)}`}
            >
              {current.identifier}
            </Link>
            <h1 className="text-3xl font-semibold break-words">
              {current.title}
            </h1>
            {current.archived && <p>{t("issues.archived")}</p>}
          </header>
          <IssueMetadata
            workspaceId={workspaceId}
            issue={current}
            enabled={ready}
          />
          <IssueSettings
            key={JSON.stringify([workspaceId, user?.uuid, issueId])}
            workspaceId={workspaceId}
            issue={current}
            manage={
              writable && !!query.data?.capabilities.manage && !current.archived
            }
            canMove={writable && !!query.data?.capabilities.move}
            restore={writable && !!query.data?.capabilities.restore}
            reviewLatest={reviewLatest}
          />
          <DocumentPanel
            workspaceId={workspaceId}
            owner={{ kind: "issue", id: issueId }}
            metadataVersion={current.version}
          />
          <IssueRelations
            workspaceId={workspaceId}
            issueId={issueId}
            metadataVersion={current.version}
          />
          <AttachmentPanel
            workspaceID={workspaceId}
            issueID={issueId}
            metadataVersion={current.version}
          />
          <CommentPanel
            workspaceID={workspaceId}
            issueID={issueId}
            metadataVersion={current.version}
          />
          <IssueTimeline workspaceID={workspaceId} issueID={issueId} />
          <IssueHistory
            workspaceId={workspaceId}
            issueId={issueId}
            enabled={ready}
          />
        </div>
      )}
    </div>
  );
}
