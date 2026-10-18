"use client";
import { useState } from "react";
import Link from "next/link";
import { useQuery } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { useAuth } from "@/providers/auth-provider";
import { useLocale } from "@/providers/locale-provider";
import { LoadNotice } from "@/features/issues/issue-ui";
import { DocumentRenderer } from "@/features/documents/shared/renderer";
import {
  commentService,
  type Comment,
  type CommentRevision,
} from "./comment.service";
import {
  commentKeys,
  useCommentRevisions,
  useCommentRevision,
  useCommentBacklinks,
} from "./queries";
const channels: Record<string, string> = {
  panel: "comments.channelPanel",
  api: "comments.channelApi",
  mcp: "comments.channelMcp",
  unknown: "comments.channelUnknown",
};
const kinds: Record<string, string> = {
  create: "comments.created",
  replace: "comments.replaced",
  patch: "comments.patched",
  delete: "comments.deleted",
  restore: "comments.restored",
};
export function Channel({ value }: { value: string }) {
  const { t } = useLocale();
  return <span>{t(channels[value] ?? "comments.channelUnknown")}</span>;
}
export function CommentProvenance({ comment }: { comment: Comment }) {
  const { t, locale } = useLocale();
  return (
    <p className="text-muted-foreground text-sm break-all">
      {t("comments.author")}:{" "}
      <span title={comment.author_id}>{comment.author_id}</span> ·{" "}
      <Channel value={comment.created_channel} /> ·{" "}
      <time dateTime={comment.created_at}>
        {new Intl.DateTimeFormat(locale, {
          dateStyle: "medium",
          timeStyle: "short",
        }).format(new Date(comment.created_at))}
      </time>
    </p>
  );
}
export function RevisionProvenance({
  revision,
}: {
  revision: CommentRevision;
}) {
  const { t, locale } = useLocale();
  return (
    <p className="text-muted-foreground text-sm break-all">
      {t(kinds[revision.kind] ?? "comments.changed")} · {t("comments.actor")}:{" "}
      {revision.actor_id} · <Channel value={revision.channel} /> ·{" "}
      <time dateTime={revision.occurred_at}>
        {new Intl.DateTimeFormat(locale, {
          dateStyle: "medium",
          timeStyle: "short",
        }).format(new Date(revision.occurred_at))}
      </time>
    </p>
  );
}
export function CommentRevisionContent({
  workspaceID,
  commentID,
  revisionID,
  enabled,
}: {
  workspaceID: string;
  commentID: string;
  revisionID: string;
  enabled: boolean;
}) {
  const { t } = useLocale(),
    read = useCommentRevision(workspaceID, commentID, revisionID, enabled);
  return (
    <div className="space-y-2">
      <LoadNotice
        pending={enabled && read.isPending}
        error={read.isError}
        retry={() => read.refetch()}
      />
      <div hidden={!enabled || read.isError}>
        {read.data && (
          <>
            <CommentProvenance comment={read.data.comment} />
            <RevisionProvenance revision={read.data.revision} />
            {read.data.comment.deleted && <p>{t("comments.tombstone")}</p>}
            <DocumentRenderer
              content={read.data.comment.content}
              workspaceId={workspaceID}
            />
            <details>
              <summary>{t("comments.fullJson")}</summary>
              <pre className="max-h-64 overflow-auto text-xs whitespace-pre-wrap">
                {JSON.stringify(
                  {
                    format: read.data.comment.format,
                    schema_version: read.data.comment.schema_version,
                    content: read.data.comment.content,
                  },
                  null,
                  2,
                )}
              </pre>
            </details>
          </>
        )}
      </div>
    </div>
  );
}
export function CommentHistory({
  workspaceID,
  comment,
  enabled,
  ready,
  locked,
  onRestore,
}: {
  workspaceID: string;
  comment: Comment;
  enabled: boolean;
  ready: boolean;
  locked: boolean;
  onRestore: (revisionID: string) => void;
}) {
  const { t } = useLocale(),
    revisions = useCommentRevisions(workspaceID, comment.id, enabled);
  const [selected, setSelected] = useState("");
  const old = useCommentRevision(
    workspaceID,
    comment.id,
    selected,
    enabled && !!selected,
  );
  return (
    <div className="space-y-3">
      <LoadNotice
        pending={enabled && revisions.isPending}
        error={revisions.isError}
        retry={() => revisions.refetch()}
      />
      <ol hidden={!enabled || revisions.isError} className="divide-y">
        {revisions.data?.pages
          .flatMap((page) => page.items)
          .map((revision) => (
            <li key={revision.id} className="space-y-1 py-2">
              <RevisionProvenance revision={revision} />
              <p className="font-mono text-xs break-all">
                {revision.id}
                {revision.source_revision && (
                  <>
                    {" "}
                    · {t("comments.sourceRevision")}: {revision.source_revision}
                  </>
                )}
              </p>
              <Button
                type="button"
                variant="outline"
                aria-pressed={selected === revision.id}
                onClick={() => setSelected(revision.id)}
              >
                {t("comments.viewRevision")}
              </Button>
            </li>
          ))}
      </ol>
      {revisions.hasNextPage && (
        <Button
          type="button"
          disabled={
            revisions.isFetchingNextPage || revisions.isError || !enabled
          }
          onClick={() => void revisions.fetchNextPage()}
        >
          {t("comments.loadMore")}
        </Button>
      )}
      {selected && (
        <div className="space-y-3 rounded border p-3">
          <LoadNotice
            pending={enabled && old.isPending}
            error={old.isError}
            retry={() => old.refetch()}
          />
          <div hidden={!enabled || old.isError}>
            {old.data && (
              <>
                <div className="grid gap-4 lg:grid-cols-2">
                  <section className="min-w-0 space-y-2">
                    <h4 className="font-medium">
                      {t("comments.selectedRevision")}
                    </h4>
                    <CommentProvenance comment={old.data.comment} />
                    <RevisionProvenance revision={old.data.revision} />
                    {old.data.revision.deleted && (
                      <p>{t("comments.tombstone")}</p>
                    )}
                    <DocumentRenderer
                      content={old.data.comment.content}
                      workspaceId={workspaceID}
                    />
                  </section>
                  <section className="min-w-0 space-y-2">
                    <h4 className="font-medium">
                      {t("comments.currentContent")}
                    </h4>
                    {comment.deleted && <p>{t("comments.tombstone")}</p>}
                    <DocumentRenderer
                      content={comment.content}
                      workspaceId={workspaceID}
                    />
                  </section>
                </div>
                <p className="text-sm">{t("comments.restoreHelp")}</p>
                <Button
                  type="button"
                  disabled={
                    !ready ||
                    locked ||
                    old.isFetching ||
                    old.isError ||
                    !comment.capabilities.restore ||
                    !old.data.revision.capabilities.restore ||
                    old.data.revision.deleted
                  }
                  onClick={() => onRestore(old.data!.revision.id)}
                >
                  {t("comments.restoreRevision")}
                </Button>
                <details>
                  <summary>{t("comments.fullJson")}</summary>
                  <pre className="max-h-64 overflow-auto text-xs whitespace-pre-wrap">
                    {JSON.stringify(
                      {
                        format: old.data.comment.format,
                        schema_version: old.data.comment.schema_version,
                        content: old.data.comment.content,
                      },
                      null,
                      2,
                    )}
                  </pre>
                </details>
              </>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
export function CommentExport({
  workspaceID,
  commentID,
  enabled,
}: {
  workspaceID: string;
  commentID: string;
  enabled: boolean;
}) {
  const { user } = useAuth(),
    { t } = useLocale();
  const [format, setFormat] = useState<"json" | "html" | "markdown">("json"),
    [requested, setRequested] = useState(false),
    [error, setError] = useState<string | null>(null);
  const output = useQuery({
    queryKey: [
      ...commentKeys.all(workspaceID, user?.uuid),
      "export",
      commentID,
      format,
    ],
    enabled: enabled && requested,
    queryFn: () => commentService.export(workspaceID, commentID, format),
  });
  return (
    <div className="space-y-2">
      <label className="block">
        {t("comments.exportFormat")}
        <select
          className="ms-2 rounded border p-2"
          value={format}
          onChange={(event) => {
            const value = event.target.value;
            if (value === "json" || value === "html" || value === "markdown")
              setFormat(value);
          }}
        >
          <option value="json">JSON</option>
          <option value="html">HTML</option>
          <option value="markdown">Markdown</option>
        </select>
      </label>
      <Button
        type="button"
        disabled={!enabled || output.isFetching}
        onClick={() => {
          if (requested) void output.refetch();
          else setRequested(true);
        }}
      >
        {t("comments.readExport")}
      </Button>
      <LoadNotice
        pending={requested && output.isPending}
        error={output.isError}
        retry={() => output.refetch()}
      />
      <div hidden={!enabled || output.isError}>
        {output.data && (
          <>
            <p className="font-mono text-xs break-all">
              {t("comments.revision")}: {output.data.revision} ·{" "}
              {t("comments.rendererVersion")}: {output.data.renderer_version}
            </p>
            {format === "markdown" && <p>{t("comments.markdownLossy")}</p>}
            <ul>
              {(output.data.losses ?? []).map((loss) => (
                <li key={loss}>{loss}</li>
              ))}
            </ul>
            <label className="block">
              {t("comments.exportContent")}
              <textarea
                readOnly
                className="min-h-40 w-full rounded border p-2 font-mono text-xs"
                value={output.data.content}
              />
            </label>
            <Button
              type="button"
              onClick={() => {
                try {
                  setError(null);
                  const result = output.data;
                  if (!result) return;
                  const blob = new Blob([result.content], {
                      type: "text/plain;charset=utf-8",
                    }),
                    url = URL.createObjectURL(blob),
                    anchor = document.createElement("a");
                  anchor.href = url;
                  anchor.download = `comment-${commentID}.${format === "markdown" ? "md" : format}`;
                  anchor.click();
                  setTimeout(() => URL.revokeObjectURL(url), 1000);
                } catch (cause) {
                  setError(String(cause));
                }
              }}
            >
              {t("comments.downloadExport")}
            </Button>
          </>
        )}
      </div>
      {error && <p role="alert">{error}</p>}
    </div>
  );
}
export function CommentBacklinks({
  workspaceID,
  issueID,
  enabled,
}: {
  workspaceID: string;
  issueID: string;
  enabled: boolean;
}) {
  const { t } = useLocale(),
    list = useCommentBacklinks(workspaceID, issueID, enabled);
  return (
    <details className="rounded border p-3">
      <summary>{t("comments.backlinks")}</summary>
      <LoadNotice
        pending={enabled && list.isPending}
        error={list.isError}
        retry={() => list.refetch()}
      />
      <ol hidden={!enabled || list.isError}>
        {list.data?.pages
          .flatMap((page) => page.items)
          .map((row) => (
            <li key={row.id} className="py-2">
              <Link
                className="underline"
                href={`/w/${workspaceID}/issues/${row.issue_id}#comment-${row.comment_id}`}
              >
                {row.value || t("comments.openComment")}
              </Link>
              <p className="font-mono text-xs break-all">
                {t("comments.revision")}: {row.revision} · {t("comments.node")}:{" "}
                {row.node_id}
              </p>
            </li>
          ))}
      </ol>
      {list.hasNextPage && (
        <Button
          type="button"
          disabled={!enabled || list.isError || list.isFetchingNextPage}
          onClick={() => void list.fetchNextPage()}
        >
          {t("comments.loadMore")}
        </Button>
      )}
    </details>
  );
}
