"use client";
import { useState, useSyncExternalStore } from "react";
import Link from "next/link";
import { useQueryClient } from "@tanstack/react-query";
import { ApiError } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { useAuth } from "@/providers/auth-provider";
import { useLocale } from "@/providers/locale-provider";
import { Section } from "@/features/issues/issue-ui";
import { useWorkspace, workspaceKeys } from "@/features/workspaces/queries";
import { workspaceService } from "@/features/workspaces/workspace.service";
import {
  commentService,
  executeCommentCommand,
  type Comment,
  type CommentCommand,
} from "./comment.service";
import { commentDrafts, commentDraftScope } from "./draft-state";
import { commentKeys } from "./queries";
import { commentOperations as store } from "./comment-store";
import type {
  CommentOperation,
  CommentOperationScope,
  CommentAttempt,
} from "./operation-state";
import type { OperationSubmission } from "@/features/workspaces/operation-submission";
export function useCommentOperations() {
  return useSyncExternalStore(
    store.subscribe,
    store.snapshot,
    store.serverSnapshot,
  );
}
const same = (a?: string, b?: string) => a?.toLowerCase() === b?.toLowerCase();
function scoped(
  entry: CommentOperation<CommentCommand>,
  scope: CommentOperationScope,
) {
  return (
    same(entry.actorID, scope.actorID) &&
    same(entry.workspaceID, scope.workspaceID) &&
    same(entry.issueID, scope.issueID) &&
    same(entry.commentID, scope.commentID)
  );
}
function errorText(error: unknown) {
  return error instanceof Error ? error.message : String(error);
}
function visibleError(message: string, t: (key: string) => string) {
  return message.startsWith("COMMENT_")
    ? t("comments.reviewRequired")
    : message;
}
async function send(
  id: string,
  submission: OperationSubmission<CommentCommand>,
  ticket: CommentAttempt,
  cache: ReturnType<typeof useQueryClient>,
  onSaved?: (comment: Comment) => void,
) {
  const entry = store.get(id);
  if (!entry) return;
  try {
    const result = await executeCommentCommand(
      submission.input,
      submission.key,
      ticket.signal,
    );
    if (!store.success(id, ticket)) return;
    const command = submission.input;
    const draftKey = commentDraftScope(
      entry.actorID,
      entry.workspaceID,
      entry.issueID,
      command.action === "comment.create" ? undefined : entry.commentID,
    );
    if (command.action === "comment.create")
      commentDrafts.reset(draftKey, null);
    else if (
      (command.action === "comment.replace" ||
        command.action === "comment.patch") &&
      commentDrafts.get(draftKey)
    )
      commentDrafts.reset(draftKey, {
        revision: result.comment.revision,
        content: result.comment.content,
      });
    onSaved?.(result.comment);
    await Promise.all([
      cache.invalidateQueries({
        queryKey: commentKeys.all(submission.input.workspaceID, entry.actorID),
      }),
      cache.invalidateQueries({
        queryKey: workspaceKeys.activity(
          submission.input.workspaceID,
          entry.actorID,
        ),
      }),
    ]);
  } catch (error) {
    store.failed(
      id,
      ticket,
      errorText(error),
      error instanceof ApiError ? error.status : undefined,
    );
  }
}
export function useCommentOperation(
  workspaceID: string,
  issueID: string,
  commentID?: string,
  onSaved?: (comment: Comment) => void,
) {
  const { user } = useAuth(),
    cache = useQueryClient(),
    entries = useCommentOperations();
  const [error, setError] = useState<string | null>(null);
  const scope: CommentOperationScope = {
    actorID: user?.uuid ?? "",
    workspaceID,
    issueID,
    ...(commentID ? { commentID } : {}),
  };
  const entry = [...entries.values()].find((entry) => scoped(entry, scope));
  return {
    entry,
    locked: !!entry,
    error,
    async submit(command: CommentCommand, label: string) {
      setError(null);
      try {
        if (
          !user?.uuid ||
          !same(command.workspaceID, workspaceID) ||
          !same(command.issueID, issueID) ||
          !same(
            "commentID" in command ? command.commentID : undefined,
            commentID,
          )
        )
          throw new Error("COMMENT_CONTEXT_CHANGED");
        const next = store.begin(scope, command.action, label, command);
        await send(
          next.id,
          next.submission,
          store.ticket(next.id),
          cache,
          onSaved,
        );
      } catch (cause) {
        setError(errorText(cause));
      }
    },
  };
}
async function reviewCurrent(entry: CommentOperation<CommentCommand>) {
  const command = entry.submission.input;
  await workspaceService.get(entry.workspaceID);
  if (command.action === "comment.create") {
    const list = await commentService.list(
      entry.workspaceID,
      entry.issueID,
      true,
    );
    return list.capabilities.manage && list.capabilities.create;
  }
  const { comment } = await commentService.get(
    entry.workspaceID,
    command.commentID,
  );
  if (!same(comment.issue_id, entry.issueID) || !comment.capabilities.manage)
    return false;
  if (command.action === "comment.delete") return true;
  const list = await commentService.list(
    entry.workspaceID,
    entry.issueID,
    true,
  );
  if (!list.capabilities.manage || !list.capabilities.create) return false;
  if (command.action === "comment.restore") {
    const source = await commentService.revision(
      entry.workspaceID,
      command.commentID,
      command.body.revision_id,
    );
    return !source.revision.deleted && source.revision.capabilities.restore;
  }
  return true;
}
export function CommentOperationNotice({
  entry,
  allowed,
  canWrite = allowed,
}: {
  entry: CommentOperation<CommentCommand>;
  allowed: boolean;
  canWrite?: boolean;
}) {
  const { t } = useLocale(),
    cache = useQueryClient();
  const [error, setError] = useState<string | null>(null);
  return (
    <div className="space-y-2 rounded border p-3" role="status">
      <p>
        {t(entry.label)} ·{" "}
        {t(
          entry.pending
            ? "comments.sending"
            : entry.outcome === "rejected"
              ? "comments.rejected"
              : "comments.uncertain",
        )}
      </p>
      <p className="font-mono text-xs break-all">{entry.submission.key}</p>
      <details>
        <summary>{t("comments.request")}</summary>
        <pre className="max-h-52 overflow-auto text-xs whitespace-pre-wrap">
          {JSON.stringify(entry.submission.input, null, 2)}
        </pre>
      </details>
      {!entry.pending && (
        <div className="flex flex-wrap gap-2">
          <Button
            type="button"
            disabled={!allowed || entry.reviewing}
            onClick={() => void store.review(entry.id, reviewCurrent)}
          >
            {t("comments.review")}
          </Button>
          <Button
            type="button"
            disabled={
              !allowed ||
              !canWrite ||
              entry.reviewing ||
              !entry.reviewed ||
              !entry.canRetry ||
              !entry.retryable
            }
            onClick={() => {
              try {
                const submission = store.retry(entry.id);
                void send(entry.id, submission, store.ticket(entry.id), cache);
              } catch (cause) {
                setError(errorText(cause));
              }
            }}
          >
            {t("comments.retry")}
          </Button>
          <Button
            type="button"
            variant="outline"
            disabled={!allowed || entry.reviewing || !entry.reviewed}
            onClick={() => {
              try {
                store.discard(entry.id);
              } catch (cause) {
                setError(errorText(cause));
              }
            }}
          >
            {t("comments.discardRequest")}
          </Button>
        </div>
      )}
      {entry.reviewError && <p role="alert">{t("comments.reviewFailed")}</p>}
      {entry.reviewed && !entry.canRetry && (
        <p role="alert">{t("comments.retryBlocked")}</p>
      )}
      {(error ?? entry.error) && (
        <p role="alert">{visibleError(error ?? entry.error ?? "", t)}</p>
      )}
    </div>
  );
}
export function CommentPendingOperations({
  workspaceID,
}: {
  workspaceID: string;
}) {
  const { user } = useAuth(),
    { t } = useLocale(),
    entries = useCommentOperations(),
    workspace = useWorkspace(workspaceID);
  const scoped = [...entries.values()].filter(
    (entry) =>
      same(entry.actorID, user?.uuid) && same(entry.workspaceID, workspaceID),
  );
  if (!scoped.length) return null;
  const allowed =
    workspace.isSuccess && !workspace.isError && !workspace.isFetching;
  return (
    <Section title={t("comments.pending")}>
      <div hidden={!allowed}>
        {scoped.map((entry) => (
          <div key={entry.id} className="space-y-2 py-3">
            <Link
              className="underline"
              href={`/w/${workspaceID}/issues/${entry.issueID}${entry.commentID ? "#comment-" + entry.commentID : ""}`}
            >
              {t("comments.openIssue")}
            </Link>
            <CommentOperationNotice
              entry={entry}
              allowed={allowed}
              canWrite={!!workspace.data?.capabilities.update}
            />
          </div>
        ))}
      </div>
    </Section>
  );
}
