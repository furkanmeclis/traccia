"use client";
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { useAuth } from "@/providers/auth-provider";
import { useLocale } from "@/providers/locale-provider";
import { useWorkspace } from "@/features/workspaces/queries";
import { Section, LoadNotice } from "@/features/issues/issue-ui";
import { IssueMetadata } from "@/features/issues/issue-metadata";
import { documentService } from "@/features/documents/document.service";
import { DocumentRenderer } from "@/features/documents/shared/renderer";
import { Channel, CommentRevisionContent } from "./comment-history";
import { commentKeys, useIssueTimeline } from "./queries";
import type { TimelineItem } from "./comment.service";
const timelineLabels = {
  issue: "comments.timeline_issue",
  document: "comments.timeline_document",
  comment: "comments.timeline_comment",
  relation: "comments.timeline_relation",
  attachment: "comments.timeline_attachment",
  legacy: "comments.timeline_legacy",
};
const relationLabels = {
  parent: "comments.relation_parent",
  blocks: "comments.relation_blocks",
  duplicate: "comments.relation_duplicate",
  related: "comments.relation_related",
};
function DocumentRevision({
  workspaceID,
  documentID,
  revisionID,
  enabled,
}: {
  workspaceID: string;
  documentID: string;
  revisionID: string;
  enabled: boolean;
}) {
  const { user } = useAuth();
  const read = useQuery({
    queryKey: [
      ...commentKeys.all(workspaceID, user?.uuid),
      "timelineDocument",
      documentID,
      revisionID,
    ],
    enabled,
    queryFn: () =>
      documentService.revision(workspaceID, documentID, revisionID),
  });
  return (
    <div>
      <LoadNotice
        pending={read.isPending}
        error={read.isError}
        retry={() => read.refetch()}
      />
      <div hidden={!enabled || read.isError}>
        {read.data && (
          <DocumentRenderer
            workspaceId={workspaceID}
            content={read.data.document.content}
          />
        )}
      </div>
    </div>
  );
}
function TimelineDetail({
  workspaceID,
  item,
  enabled,
}: {
  workspaceID: string;
  item: TimelineItem;
  enabled: boolean;
}) {
  const { t } = useLocale(),
    detail = item.detail;
  switch (detail.kind) {
    case "issue":
      return detail.issue ? (
        <div>
          <p>
            {detail.issue.snapshot.identifier} · {detail.issue.snapshot.title}
          </p>
          <IssueMetadata
            workspaceId={workspaceID}
            issue={detail.issue.snapshot}
            enabled={enabled}
          />
        </div>
      ) : null;
    case "document":
      return detail.document ? (
        <DocumentRevision
          workspaceID={workspaceID}
          documentID={detail.document.document_id}
          revisionID={detail.document.revision}
          enabled={enabled}
        />
      ) : null;
    case "comment":
      return detail.comment ? (
        <CommentRevisionContent
          workspaceID={workspaceID}
          commentID={detail.comment.comment_id}
          revisionID={detail.comment.revision}
          enabled={enabled}
        />
      ) : null;
    case "relation":
      return detail.relation ? (
        <div className="space-y-2">
          <p>
            {detail.relation.snapshot.state.issue.identifier} ·{" "}
            {detail.relation.snapshot.state.issue.title}
          </p>
          <ul>
            {detail.relation.snapshot.changed_relations.map((edge) => (
              <li key={edge.id}>
                {t(relationLabels[edge.kind])}: {edge.source.identifier} →{" "}
                {edge.target.identifier} ·{" "}
                {t(edge.removed ? "comments.removed" : "comments.linked")}
              </li>
            ))}
          </ul>
        </div>
      ) : null;
    case "attachment":
      return detail.attachment ? (
        <dl className="space-y-1 break-all">
          <dt>{t("comments.filename")}</dt>
          <dd>{detail.attachment.filename}</dd>
          <dt>{t("comments.bytes")}</dt>
          <dd>{detail.attachment.size}</dd>
          <dt>SHA-256</dt>
          <dd className="font-mono text-xs">{detail.attachment.sha256}</dd>
          <dt>{t("comments.fileRevision")}</dt>
          <dd>{detail.attachment.file_revision}</dd>
          <dd>
            {t(
              detail.attachment.removed
                ? "comments.removed"
                : "comments.linked",
            )}
          </dd>
        </dl>
      ) : null;
    case "legacy":
      return detail.legacy ? (
        <p>
          {t("comments.legacy")}: {detail.legacy.resource_type} ·{" "}
          {detail.legacy.resource_id ?? t("comments.unavailable")}
        </p>
      ) : null;
  }
}
function TimelineRow({
  workspaceID,
  item,
  enabled,
}: {
  workspaceID: string;
  item: TimelineItem;
  enabled: boolean;
}) {
  const { t, locale } = useLocale(),
    [expanded, setExpanded] = useState(false);
  return (
    <li className="space-y-2 rounded border p-3">
      <p>
        {t(timelineLabels[item.detail.kind])} · <Channel value={item.channel} />
      </p>
      <p className="text-sm break-all">
        {t("comments.actor")}: {item.actor_id} ·{" "}
        <time dateTime={item.occurred_at}>
          {new Intl.DateTimeFormat(locale, {
            dateStyle: "medium",
            timeStyle: "short",
          }).format(new Date(item.occurred_at))}
        </time>
      </p>
      <p className="font-mono text-xs break-all">
        {item.event_name} · {item.id}
      </p>
      <Button
        type="button"
        variant="outline"
        onClick={() => setExpanded((v) => !v)}
      >
        {t(expanded ? "comments.hideSnapshot" : "comments.showSnapshot")}
      </Button>
      {expanded && (
        <TimelineDetail
          workspaceID={workspaceID}
          item={item}
          enabled={enabled}
        />
      )}
    </li>
  );
}
export function IssueTimeline({
  workspaceID,
  issueID,
}: {
  workspaceID: string;
  issueID: string;
}) {
  const { t } = useLocale(),
    workspace = useWorkspace(workspaceID),
    allowed = workspace.isSuccess && !workspace.isError;
  const timeline = useIssueTimeline(workspaceID, issueID, allowed),
    ready =
      allowed &&
      !workspace.isFetching &&
      !timeline.isError &&
      !timeline.isFetching;
  return (
    <Section title={t("comments.timeline")}>
      <Button
        type="button"
        variant="outline"
        disabled={timeline.isFetching}
        onClick={() =>
          void Promise.all([workspace.refetch(), timeline.refetch()])
        }
      >
        {t("comments.refresh")}
      </Button>
      <LoadNotice
        pending={timeline.isPending}
        error={workspace.isError || timeline.isError}
        retry={() => Promise.all([workspace.refetch(), timeline.refetch()])}
      />
      <div hidden={!allowed || timeline.isError}>
        <ol className="space-y-3">
          {timeline.data?.pages
            .flatMap((p) => p.items)
            .map((item) => (
              <TimelineRow
                key={item.id}
                workspaceID={workspaceID}
                item={item}
                enabled={ready}
              />
            ))}
        </ol>
        {timeline.hasNextPage && (
          <Button
            type="button"
            variant="outline"
            disabled={timeline.isFetching}
            onClick={() => void timeline.fetchNextPage()}
          >
            {t("comments.more")}
          </Button>
        )}
      </div>
    </Section>
  );
}
