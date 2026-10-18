import type { components } from "@/generated/api";
import { apiClient, unwrap } from "@/lib/api";
import { issueService } from "@/features/issues/issue.service";
import { relationService } from "@/features/issue-relations/relation.service";
import type { MentionChoice } from "./shared/editor";
type S = components["schemas"];
export type Document = S["ProductDocumentsGetOutput"]["document"];
export type Owner = { kind: "issue" | "project"; id: string };
export type DocumentCommand = { workspaceID: string; owner: Owner } & (
  | { action: "document.create"; body: S["ProductDocumentsCreateInput"] }
  | {
      action: "document.replace";
      documentID: string;
      body: S["ProductDocumentsReplaceInput"];
    }
  | {
      action: "document.patch";
      documentID: string;
      body: S["ProductDocumentsPatchInput"];
    }
  | {
      action: "document.restore";
      documentID: string;
      body: S["ProductDocumentsRestoreInput"];
    }
  | { action: "issue.create-rich"; body: S["ProductIssuesCreateRichInput"] }
);
export const documentService = {
  async resolve(workspaceID: string, owner: Owner) {
    return unwrap<S["ProductDocumentsResolveOutput"]>(
      await apiClient.GET("/v1/workspaces/{workspaceID}/documents/by-owner", {
        params: {
          path: { workspaceID },
          query: { owner_kind: owner.kind, owner_id: owner.id },
        },
      }),
    );
  },
  async get(workspaceID: string, documentID: string) {
    return unwrap<S["ProductDocumentsGetOutput"]>(
      await apiClient.GET(
        "/v1/workspaces/{workspaceID}/documents/{documentID}",
        { params: { path: { workspaceID, documentID } } },
      ),
    );
  },
  async revisions(workspaceID: string, documentID: string, after?: string) {
    return unwrap<S["ProductDocumentsRevisionsOutput"]>(
      await apiClient.GET(
        "/v1/workspaces/{workspaceID}/documents/{documentID}/revisions",
        {
          params: {
            path: { workspaceID, documentID },
            query: { limit: 50, after },
          },
        },
      ),
    );
  },
  async revision(workspaceID: string, documentID: string, revisionID: string) {
    return unwrap<S["ProductDocumentsGetRevisionOutput"]>(
      await apiClient.GET(
        "/v1/workspaces/{workspaceID}/documents/{documentID}/revisions/{revisionID}",
        { params: { path: { workspaceID, documentID, revisionID } } },
      ),
    );
  },
  async export(
    workspaceID: string,
    documentID: string,
    format: "json" | "html" | "markdown",
  ) {
    return unwrap<S["ProductDocumentsExportOutput"]>(
      await apiClient.GET(
        "/v1/workspaces/{workspaceID}/documents/{documentID}/export",
        { params: { path: { workspaceID, documentID }, query: { format } } },
      ),
    );
  },
  async backlinks(workspaceID: string, owner: Owner, after?: string) {
    return unwrap<S["ProductDocumentsBacklinksOutput"]>(
      await apiClient.GET("/v1/workspaces/{workspaceID}/documents/backlinks", {
        params: {
          path: { workspaceID },
          query: {
            target_kind: owner.kind,
            target_id: owner.id,
            limit: 50,
            after,
          },
        },
      }),
    );
  },
  async mentions(workspaceID: string): Promise<MentionChoice[]> {
    const [references, issues] = await Promise.all([
      issueService.references(workspaceID),
      relationService.candidates(workspaceID),
    ]);
    return [
      ...references.members
        .filter((x) => x.active)
        .map((x) => ({ kind: "user" as const, id: x.id, label: x.name })),
      ...references.projects
        .filter((x) => !x.archived)
        .map((x) => ({ kind: "project" as const, id: x.id, label: x.name })),
      ...issues.map((x) => ({
        kind: "issue" as const,
        id: x.id,
        label: x.identifier + " · " + x.title,
        value: x.title,
      })),
    ];
  },
};
export async function executeDocumentCommand(
  command: DocumentCommand,
  key: string,
) {
  const headers = { "Idempotency-Key": key };
  const workspaceID = command.workspaceID;
  switch (command.action) {
    case "document.create":
      return unwrap<S["ProductDocumentsCreateOutput"]>(
        await apiClient.POST("/v1/workspaces/{workspaceID}/documents", {
          params: { path: { workspaceID }, header: headers },
          body: command.body,
        }),
      );
    case "document.replace":
      return unwrap<S["ProductDocumentsReplaceOutput"]>(
        await apiClient.PUT(
          "/v1/workspaces/{workspaceID}/documents/{documentID}",
          {
            params: {
              path: { workspaceID, documentID: command.documentID },
              header: headers,
            },
            body: command.body,
          },
        ),
      );
    case "document.patch":
      return unwrap<S["ProductDocumentsPatchOutput"]>(
        await apiClient.POST(
          "/v1/workspaces/{workspaceID}/documents/{documentID}/patch",
          {
            params: {
              path: { workspaceID, documentID: command.documentID },
              header: headers,
            },
            body: command.body,
          },
        ),
      );
    case "document.restore":
      return unwrap<S["ProductDocumentsRestoreOutput"]>(
        await apiClient.POST(
          "/v1/workspaces/{workspaceID}/documents/{documentID}/restore",
          {
            params: {
              path: { workspaceID, documentID: command.documentID },
              header: headers,
            },
            body: command.body,
          },
        ),
      );
    case "issue.create-rich":
      return unwrap<S["ProductIssuesCreateRichOutput"]>(
        await apiClient.POST("/v1/workspaces/{workspaceID}/issues/rich", {
          params: { path: { workspaceID }, header: headers },
          body: command.body,
        }),
      );
  }
}
