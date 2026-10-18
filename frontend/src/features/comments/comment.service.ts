import type { components } from "@/generated/api";
import { apiClient, unwrap } from "@/lib/api";
import { issueService } from "@/features/issues/issue.service";
import { collectIssues } from "@/features/issues/snapshot";
import { validateContent } from "@/features/documents/shared/contract";
import type { MentionChoice } from "@/features/documents/shared/editor";
type S = components["schemas"];
export type Comment = S["ProductCommentsGetOutput"]["comment"];
export type CommentRevision = S["ProductCommentsGetRevisionOutput"]["revision"];
export type CommentHistory = S["ProductCommentsGetRevisionOutput"];
export type TimelineItem = S["ProductIssueTimelineListOutput"]["items"][number];
export type CommentCommand = { workspaceID: string; issueID: string } & (
  | { action: "comment.create"; body: S["ProductCommentsCreateInput"] }
  | {
      action: "comment.replace";
      commentID: string;
      body: S["ProductCommentsReplaceInput"];
    }
  | {
      action: "comment.patch";
      commentID: string;
      body: S["ProductCommentsPatchInput"];
    }
  | {
      action: "comment.delete";
      commentID: string;
      body: S["ProductCommentsDeleteInput"];
    }
  | {
      action: "comment.restore";
      commentID: string;
      body: S["ProductCommentsRestoreInput"];
    }
);
function validateComment(comment: Comment) {
  if (comment.format !== "plate" || comment.schema_version !== 1)
    throw new Error("COMMENT_CONTENT_VERSION");
  validateContent(comment.content);
}
async function unwrapComment<T extends { comment: Comment }>(
  result: Parameters<typeof unwrap>[0],
): Promise<T> {
  const value = await unwrap<T>(result);
  validateComment(value.comment);
  return value;
}
async function unwrapCommentList<T extends { items: Comment[] }>(
  result: Parameters<typeof unwrap>[0],
): Promise<T> {
  const value = await unwrap<T>(result);
  if (!Array.isArray(value.items)) throw new Error("COMMENT_LIST_INVALID");
  value.items.forEach(validateComment);
  return value;
}
export const commentService = {
  async get(workspaceID: string, commentID: string, signal?: AbortSignal) {
    return unwrapComment<S["ProductCommentsGetOutput"]>(
      await apiClient.GET("/v1/workspaces/{workspaceID}/comments/{commentID}", {
        params: { path: { workspaceID, commentID } },
        signal,
      }),
    );
  },
  async list(
    workspaceID: string,
    issueID: string,
    includeDeleted: boolean,
    after?: string,
  ) {
    return unwrapCommentList<S["ProductCommentsListOutput"]>(
      await apiClient.GET(
        "/v1/workspaces/{workspaceID}/issues/{issueID}/comments",
        {
          params: {
            path: { workspaceID, issueID },
            query: { include_deleted: includeDeleted, limit: 50, after },
          },
        },
      ),
    );
  },
  async revisions(workspaceID: string, commentID: string, after?: string) {
    return unwrap<S["ProductCommentsRevisionsOutput"]>(
      await apiClient.GET(
        "/v1/workspaces/{workspaceID}/comments/{commentID}/revisions",
        {
          params: {
            path: { workspaceID, commentID },
            query: { limit: 50, after },
          },
        },
      ),
    );
  },
  async revision(workspaceID: string, commentID: string, revisionID: string) {
    return unwrapComment<S["ProductCommentsGetRevisionOutput"]>(
      await apiClient.GET(
        "/v1/workspaces/{workspaceID}/comments/{commentID}/revisions/{revisionID}",
        { params: { path: { workspaceID, commentID, revisionID } } },
      ),
    );
  },
  async export(
    workspaceID: string,
    commentID: string,
    format: "json" | "html" | "markdown",
  ) {
    return unwrap<S["ProductCommentsExportOutput"]>(
      await apiClient.GET(
        "/v1/workspaces/{workspaceID}/comments/{commentID}/export",
        { params: { path: { workspaceID, commentID }, query: { format } } },
      ),
    );
  },
  async backlinks(workspaceID: string, targetID: string, after?: string) {
    return unwrap<S["ProductCommentsBacklinksOutput"]>(
      await apiClient.GET("/v1/workspaces/{workspaceID}/comment-backlinks", {
        params: {
          path: { workspaceID },
          query: {
            target_kind: "issue",
            target_id: targetID,
            limit: 50,
            after,
          },
        },
      }),
    );
  },
  async timeline(workspaceID: string, issueID: string, after?: string) {
    return unwrap<S["ProductIssueTimelineListOutput"]>(
      await apiClient.GET(
        "/v1/workspaces/{workspaceID}/issues/{issueID}/timeline",
        {
          params: {
            path: { workspaceID, issueID },
            query: { limit: 50, after },
          },
        },
      ),
    );
  },
  async mentions(workspaceID: string): Promise<MentionChoice[]> {
    const [references, active, archived] = await Promise.all([
      issueService.references(workspaceID),
      collectIssues((after) =>
        issueService.list(
          workspaceID,
          { archived: false, q: "", team_id: "", project_id: "" },
          after,
        ),
      ),
      collectIssues((after) =>
        issueService.list(
          workspaceID,
          { archived: true, q: "", team_id: "", project_id: "" },
          after,
        ),
      ),
    ]);
    return [
      ...references.members.map((member) => ({
        kind: "user" as const,
        id: member.id,
        label: member.name,
      })),
      ...references.projects.map((project) => ({
        kind: "project" as const,
        id: project.id,
        label: project.name,
      })),
      ...[
        ...new Map(
          [...active.items, ...archived.items].map((issue) => [
            issue.id.toLowerCase(),
            issue,
          ]),
        ).values(),
      ].map((issue) => ({
        kind: "issue" as const,
        id: issue.id,
        label: issue.identifier + " · " + issue.title,
      })),
    ];
  },
};
export async function executeCommentCommand(
  command: CommentCommand,
  key: string,
  signal?: AbortSignal,
) {
  const headers = { "Idempotency-Key": key },
    workspaceID = command.workspaceID;
  switch (command.action) {
    case "comment.create":
      return unwrapComment<S["ProductCommentsCreateOutput"]>(
        await apiClient.POST(
          "/v1/workspaces/{workspaceID}/issues/{issueID}/comments",
          {
            params: {
              path: { workspaceID, issueID: command.issueID },
              header: headers,
            },
            body: command.body,
            signal,
          },
        ),
      );
    case "comment.replace":
      return unwrapComment<S["ProductCommentsReplaceOutput"]>(
        await apiClient.POST(
          "/v1/workspaces/{workspaceID}/comments/{commentID}/replace",
          {
            params: {
              path: { workspaceID, commentID: command.commentID },
              header: headers,
            },
            body: command.body,
            signal,
          },
        ),
      );
    case "comment.patch":
      return unwrapComment<S["ProductCommentsPatchOutput"]>(
        await apiClient.POST(
          "/v1/workspaces/{workspaceID}/comments/{commentID}/patch",
          {
            params: {
              path: { workspaceID, commentID: command.commentID },
              header: headers,
            },
            body: command.body,
            signal,
          },
        ),
      );
    case "comment.delete":
      return unwrapComment<S["ProductCommentsDeleteOutput"]>(
        await apiClient.POST(
          "/v1/workspaces/{workspaceID}/comments/{commentID}/delete",
          {
            params: {
              path: { workspaceID, commentID: command.commentID },
              header: headers,
            },
            body: command.body,
            signal,
          },
        ),
      );
    case "comment.restore":
      return unwrapComment<S["ProductCommentsRestoreOutput"]>(
        await apiClient.POST(
          "/v1/workspaces/{workspaceID}/comments/{commentID}/restore",
          {
            params: {
              path: { workspaceID, commentID: command.commentID },
              header: headers,
            },
            body: command.body,
            signal,
          },
        ),
      );
  }
}
