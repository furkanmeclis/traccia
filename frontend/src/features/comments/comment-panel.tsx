"use client";
import { useEffect, useState, useSyncExternalStore } from "react";
import { useDrawerDraft } from "@/features/issue-drawer/draft-context";
import { Button } from "@/components/ui/button";
import { useAuth } from "@/providers/auth-provider";
import { useLocale } from "@/providers/locale-provider";
import { useWorkspace } from "@/features/workspaces/queries";
import { Section, LoadNotice } from "@/features/issues/issue-ui";
import { DocumentRenderer } from "@/features/documents/shared/renderer";
import type { MentionChoice } from "@/features/documents/shared/editor";
import type { Comment } from "./comment.service";
import { useComment, useComments, useCommentMentions } from "./queries";
import { commentDraftScope, commentDrafts, draftChanged } from "./draft-state";
import { CommentEditor } from "./comment-editor";
import {
  CommentProvenance,
  CommentHistory,
  CommentExport,
  CommentBacklinks,
} from "./comment-history";
import {
  CommentOperationNotice,
  CommentPendingOperations,
  useCommentOperation,
} from "./comment-operation";
function CommentCard({
  workspaceID,
  comment,
  ready,
  mentions,
  mentionsReady,
  refresh,
}: {
  workspaceID: string;
  comment: Comment;
  ready: boolean;
  mentions: MentionChoice[];
  mentionsReady: boolean;
  refresh: () => Promise<unknown>;
}) {
  const { user } = useAuth(),
    { t } = useLocale(),
    [editing, setEditing] = useDrawerDraft(
      `comments.${comment.id}.editing`,
      false,
    ),
    [history, setHistory] = useState(false),
    [exporting, setExporting] = useState(false);
  const draftKey = commentDraftScope(
    user?.uuid ?? "",
    workspaceID,
    comment.issue_id,
    comment.id,
  );
  const drafts = useSyncExternalStore(
    commentDrafts.subscribe,
    commentDrafts.snapshot,
    commentDrafts.serverSnapshot,
  );
  const retained = drafts.has(draftKey);
  const operation = useCommentOperation(
    workspaceID,
    comment.issue_id,
    comment.id,
  );
  const base = {
    workspaceID,
    issueID: comment.issue_id,
    commentID: comment.id,
  };
  return (
    <article
      id={`comment-${comment.id}`}
      className="space-y-3 rounded border p-3"
    >
      <CommentProvenance comment={comment} />
      <p className="font-mono text-xs break-all">
        {t("comments.revision")}: {comment.revision}
      </p>
      {comment.deleted ? (
        <p>{t("comments.tombstone")}</p>
      ) : (
        <DocumentRenderer content={comment.content} workspaceId={workspaceID} />
      )}
      <div className="flex flex-wrap gap-2">
        {(comment.capabilities.replace || retained) && (
          <Button
            type="button"
            variant="outline"
            onClick={() => setEditing((v) => !v)}
          >
            {t(editing ? "comments.closeEditor" : "comments.edit")}
          </Button>
        )}
        <Button
          type="button"
          variant="outline"
          onClick={() => setHistory((v) => !v)}
        >
          {t("comments.history")}
        </Button>
        <Button
          type="button"
          variant="outline"
          onClick={() => setExporting((v) => !v)}
        >
          {t("comments.export")}
        </Button>
        <Button
          type="button"
          variant="destructive"
          disabled={!ready || !comment.capabilities.delete || operation.locked}
          onClick={() =>
            void operation.submit(
              {
                ...base,
                action: "comment.delete",
                body: { expected_revision: comment.revision },
              },
              "comments.delete",
            )
          }
        >
          {t("comments.delete")}
        </Button>
      </div>
      <div hidden={!editing}>
        {(editing || retained) && (
          <CommentEditor
            draftKey={draftKey}
            baseline={{ revision: comment.revision, content: comment.content }}
            currentRevision={comment.revision}
            ready={ready && comment.capabilities.replace}
            locked={operation.locked}
            mentions={mentions}
            mentionsReady={mentionsReady}
            refresh={refresh}
            onSave={(value) =>
              void operation.submit(
                {
                  ...base,
                  action: "comment.replace",
                  body: { ...value, expected_revision: comment.revision },
                },
                "comments.save",
              )
            }
            patchReady={comment.capabilities.patch}
            onPatch={(patches) =>
              void operation.submit(
                {
                  ...base,
                  action: "comment.patch",
                  body: {
                    format: "plate",
                    schema_version: 1,
                    expected_revision: comment.revision,
                    patches,
                  },
                },
                "comments.patch",
              )
            }
          />
        )}
      </div>
      {history && (
        <CommentHistory
          workspaceID={workspaceID}
          comment={comment}
          enabled={ready}
          ready={ready}
          locked={operation.locked}
          onRestore={(revisionID) =>
            void operation.submit(
              {
                ...base,
                action: "comment.restore",
                body: {
                  expected_revision: comment.revision,
                  revision_id: revisionID,
                },
              },
              "comments.restore",
            )
          }
        />
      )}
      {exporting && (
        <CommentExport
          workspaceID={workspaceID}
          commentID={comment.id}
          enabled={ready}
        />
      )}
      {operation.entry && (
        <CommentOperationNotice
          entry={operation.entry}
          allowed={ready}
          canWrite={
            comment.capabilities.manage &&
            (operation.entry.submission.input.action === "comment.delete" ||
              comment.capabilities.replace ||
              comment.capabilities.restore)
          }
        />
      )}
      {operation.error && <p role="alert">{operation.error}</p>}
    </article>
  );
}
function RetainedComment({
  workspaceID,
  commentID,
  issueID,
  ready,
  mentions,
  mentionsReady,
}: {
  workspaceID: string;
  commentID: string;
  issueID: string;
  ready: boolean;
  mentions: MentionChoice[];
  mentionsReady: boolean;
}) {
  const read = useComment(workspaceID, commentID, ready);
  return (
    <div>
      <LoadNotice
        pending={read.isPending}
        error={read.isError}
        retry={() => read.refetch()}
      />
      <div hidden={!ready || read.isError}>
        {read.data && read.data.comment.issue_id === issueID && (
          <CommentCard
            workspaceID={workspaceID}
            comment={read.data.comment}
            ready={ready && !read.isFetching && !read.isError}
            mentions={mentions}
            mentionsReady={mentionsReady}
            refresh={() => read.refetch()}
          />
        )}
      </div>
    </div>
  );
}
export function CommentPanel({
  workspaceID,
  issueID,
  metadataVersion,
}: {
  workspaceID: string;
  issueID: string;
  metadataVersion: number;
}) {
  const { user } = useAuth(),
    { t } = useLocale(),
    workspace = useWorkspace(workspaceID);
  const [includeDeleted, setIncludeDeleted] = useState(true),
    [focused, setFocused] = useState("");
  const allowed = workspace.isSuccess && !workspace.isError;
  const list = useComments(
      workspaceID,
      issueID,
      includeDeleted,
      allowed,
      metadataVersion,
    ),
    mentions = useCommentMentions(workspaceID, allowed);
  const ready =
    allowed &&
    !workspace.isFetching &&
    list.isSuccess &&
    !list.isError &&
    !list.isFetching;
  const mentionsReady =
    allowed && mentions.isSuccess && !mentions.isError && !mentions.isFetching;
  const operation = useCommentOperation(workspaceID, issueID);
  const drafts = useSyncExternalStore(
    commentDrafts.subscribe,
    commentDrafts.snapshot,
    commentDrafts.serverSnapshot,
  );
  const draftKey = commentDraftScope(user?.uuid ?? "", workspaceID, issueID);
  const rows = list.data?.pages.flatMap((p) => p.items) ?? [];
  const retained = [...drafts.entries()].flatMap(([key, draft]) => {
    const scope: unknown = JSON.parse(key);
    return Array.isArray(scope) &&
      scope[0] === user?.uuid.toLowerCase() &&
      scope[1] === workspaceID.toLowerCase() &&
      scope[2] === issueID.toLowerCase() &&
      scope[3] !== "create" &&
      (draftChanged(draft) || !!draft.raw) &&
      typeof scope[3] === "string"
      ? [scope[3]]
      : [];
  });
  useEffect(() => {
    function select() {
      const id =
        window.location.hash.match(/^#comment-([0-9a-f-]{36})$/i)?.[1] ?? "";
      setFocused(id);
    }
    select();
    window.addEventListener("hashchange", select);
    return () => window.removeEventListener("hashchange", select);
  }, []);
  const extra = [
    ...new Set([...retained, ...(focused ? [focused] : [])]),
  ].filter(
    (id) => !rows.some((row) => row.id.toLowerCase() === id.toLowerCase()),
  );
  return (
    <Section title={t("comments.title")}>
      <CommentPendingOperations workspaceID={workspaceID} />
      <LoadNotice
        pending={list.isPending}
        error={workspace.isError || list.isError}
        retry={() => Promise.all([workspace.refetch(), list.refetch()])}
      />
      <div hidden={!allowed || list.isError} className="space-y-4">
        <label className="flex gap-2">
          <input
            type="checkbox"
            checked={includeDeleted}
            onChange={(e) => setIncludeDeleted(e.target.checked)}
          />
          {t("comments.includeDeleted")}
        </label>
        {(list.data?.pages[0]?.capabilities.create || drafts.has(draftKey)) && (
          <CommentEditor
            draftKey={draftKey}
            baseline={null}
            currentRevision={null}
            ready={ready && !!list.data?.pages[0]?.capabilities.create}
            locked={operation.locked}
            mentions={mentions.data ?? []}
            mentionsReady={mentionsReady}
            refresh={() => list.refetch()}
            onSave={(value) =>
              void operation.submit(
                { workspaceID, issueID, action: "comment.create", body: value },
                "comments.create",
              )
            }
          />
        )}
        {!list.data?.pages[0]?.capabilities.manage && (
          <p>{t("comments.readOnly")}</p>
        )}
        {operation.entry && (
          <CommentOperationNotice
            entry={operation.entry}
            allowed={ready}
            canWrite={
              !!list.data?.pages[0]?.capabilities.manage &&
              !!list.data?.pages[0]?.capabilities.create
            }
          />
        )}
        {operation.error && <p role="alert">{operation.error}</p>}
        {rows.map((comment) => (
          <CommentCard
            key={comment.id}
            workspaceID={workspaceID}
            comment={comment}
            ready={ready}
            mentions={mentions.data ?? []}
            mentionsReady={mentionsReady}
            refresh={() => list.refetch()}
          />
        ))}
        {list.hasNextPage && (
          <Button
            type="button"
            variant="outline"
            disabled={list.isFetching}
            onClick={() => void list.fetchNextPage()}
          >
            {t("comments.more")}
          </Button>
        )}
        {extra.length > 0 && (
          <div className="space-y-3">
            <h3>{t("comments.retainedDrafts")}</h3>
            {extra.map((id) => (
              <RetainedComment
                key={id}
                workspaceID={workspaceID}
                commentID={id}
                issueID={issueID}
                ready={ready}
                mentions={mentions.data ?? []}
                mentionsReady={mentionsReady}
              />
            ))}
          </div>
        )}
        <CommentBacklinks
          workspaceID={workspaceID}
          issueID={issueID}
          enabled={allowed}
        />
      </div>
    </Section>
  );
}
