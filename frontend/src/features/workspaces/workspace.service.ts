import type { components } from "@/generated/api";
import { apiClient, unwrap } from "@/lib/api";

type Schemas = components["schemas"];
export type WorkspaceDetailResponse = Schemas["ProductWorkspacesGetOutput"];
export type Workspace = WorkspaceDetailResponse["workspace"];
export type WorkspaceCredential =
  Schemas["ProductCredentialsCreateOutput"]["credential"];
export type CreateCredential = Schemas["ProductCredentialsCreateInput"];
export type CredentialScope = NonNullable<CreateCredential["scopes"]>[number];
export const credentialScopes = [
  "workspace.read",
  "workspace.write",
  "credentials.read",
  "credentials.manage",
] as const satisfies readonly CredentialScope[];
export const workspaceService = {
  async list(after?: string) {
    return unwrap<Schemas["ProductWorkspacesListOutput"]>(
      await apiClient.GET("/v1/workspaces", {
        params: { query: { limit: 20, after } },
      }),
    );
  },
  async create(name: string) {
    return unwrap<Schemas["ProductWorkspacesCreateOutput"]>(
      await apiClient.POST("/v1/workspaces", { body: { name } }),
    );
  },
  async get(id: string) {
    return unwrap<WorkspaceDetailResponse>(
      await apiClient.GET("/v1/workspaces/{workspaceID}", {
        params: { path: { workspaceID: id } },
      }),
    );
  },
  async update(id: string, name: string) {
    return unwrap<Schemas["ProductWorkspacesUpdateOutput"]>(
      await apiClient.PATCH("/v1/workspaces/{workspaceID}", {
        params: { path: { workspaceID: id } },
        body: { name },
      }),
    );
  },
  async credentials(id: string, after?: string) {
    return unwrap<Schemas["ProductCredentialsListOutput"]>(
      await apiClient.GET("/v1/workspaces/{workspaceID}/credentials", {
        params: { path: { workspaceID: id }, query: { limit: 20, after } },
      }),
    );
  },
  async createCredential(id: string, body: CreateCredential) {
    return unwrap<Schemas["ProductCredentialsCreateOutput"]>(
      await apiClient.POST("/v1/workspaces/{workspaceID}/credentials", {
        params: { path: { workspaceID: id } },
        body,
      }),
    );
  },
  async revokeCredential(id: string, credentialID: string) {
    return unwrap<Schemas["ProductCredentialsRevokeOutput"]>(
      await apiClient.DELETE(
        "/v1/workspaces/{workspaceID}/credentials/{credentialID}",
        { params: { path: { workspaceID: id, credentialID } } },
      ),
    );
  },
};
