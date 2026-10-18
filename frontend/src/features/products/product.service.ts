import type { components } from "@/generated/api";
import { apiClient, unwrap } from "@/lib/api";
import {
  projectService,
  type Project,
} from "@/features/projects/project.service";
import { collectCatalog } from "./snapshot";
export type Schemas = components["schemas"];
export type Product = Schemas["ProductProductsGetOutput"]["product"];
export type Environment =
  Schemas["ProductProductEnvironmentsGetOutput"]["environment"];
export type ProductFilters = {
  archived: boolean;
  q: string;
  project_id: string;
};
export type EnvironmentFilters = {
  archived: boolean;
  q: string;
  kind: Environment["kind"] | "";
};
export const productService = {
  async list(workspaceID: string, filters: ProductFilters, after?: string) {
    return unwrap<Schemas["ProductProductsListOutput"]>(
      await apiClient.GET("/v1/workspaces/{workspaceID}/products", {
        params: {
          path: { workspaceID },
          query: { limit: 50, after, ...filters },
        },
      }),
    );
  },
  async get(workspaceID: string, productID: string) {
    return unwrap<Schemas["ProductProductsGetOutput"]>(
      await apiClient.GET("/v1/workspaces/{workspaceID}/products/{productID}", {
        params: { path: { workspaceID, productID } },
      }),
    );
  },
  async environments(
    workspaceID: string,
    productID: string,
    filters: EnvironmentFilters,
    after?: string,
  ) {
    return unwrap<Schemas["ProductProductEnvironmentsListOutput"]>(
      await apiClient.GET(
        "/v1/workspaces/{workspaceID}/products/{productID}/environments",
        {
          params: {
            path: { workspaceID, productID },
            query: { limit: 50, after, ...filters },
          },
        },
      ),
    );
  },
  async allEnvironments(
    workspaceID: string,
    productID: string,
    filters: EnvironmentFilters,
  ) {
    return collectCatalog<
      Environment,
      Schemas["ProductProductEnvironmentsListOutput"]
    >((after) => this.environments(workspaceID, productID, filters, after));
  },
  async projects(
    workspaceID: string,
    productID: string,
    archived: boolean,
    after?: string,
  ) {
    return unwrap<Schemas["ProductProductProjectsListOutput"]>(
      await apiClient.GET(
        "/v1/workspaces/{workspaceID}/products/{productID}/projects",
        {
          params: {
            path: { workspaceID, productID },
            query: { limit: 100, after, archived },
          },
        },
      ),
    );
  },
  async allProjects(workspaceID: string, productID: string, archived: boolean) {
    return collectCatalog<Project, Schemas["ProductProductProjectsListOutput"]>(
      (after) => this.projects(workspaceID, productID, archived, after),
    );
  },
  async projectChoices(workspaceID: string, includeArchived = false) {
    const active = await collectCatalog<
      Project,
      Awaited<ReturnType<typeof projectService.projects>>
    >((after) => projectService.projects(workspaceID, false, after));
    if (!includeArchived) return active;
    const archived = await collectCatalog<
      Project,
      Awaited<ReturnType<typeof projectService.projects>>
    >((after) => projectService.projects(workspaceID, true, after));
    return { ...active, items: [...active.items, ...archived.items] };
  },
};
export type ProductCommand =
  | {
      action: "product.create";
      path: { workspaceID: string };
      body: Schemas["ProductProductsCreateInput"];
    }
  | {
      action: "product.update";
      path: { workspaceID: string; productID: string };
      body: Schemas["ProductProductsUpdateInput"];
    }
  | {
      action: "product.archive";
      path: { workspaceID: string; productID: string };
      body: Schemas["ProductProductsArchiveInput"];
    }
  | {
      action: "product.restore";
      path: { workspaceID: string; productID: string };
      body: Schemas["ProductProductsRestoreInput"];
    }
  | {
      action: "environment.create";
      path: { workspaceID: string; productID: string };
      body: Schemas["ProductProductEnvironmentsCreateInput"];
    }
  | {
      action: "environment.update";
      path: { workspaceID: string; productID: string; environmentID: string };
      body: Schemas["ProductProductEnvironmentsUpdateInput"];
    }
  | {
      action: "environment.archive";
      path: { workspaceID: string; productID: string; environmentID: string };
      body: Schemas["ProductProductEnvironmentsArchiveInput"];
    }
  | {
      action: "environment.restore";
      path: { workspaceID: string; productID: string; environmentID: string };
      body: Schemas["ProductProductEnvironmentsRestoreInput"];
    }
  | {
      action: "projects.set";
      path: { workspaceID: string; productID: string };
      body: Schemas["ProductProductProjectsSetInput"];
    };
export async function executeProductCommand(
  command: ProductCommand,
  key: string,
) {
  switch (command.action) {
    case "product.create":
      return unwrap<Schemas["ProductProductsCreateOutput"]>(
        await apiClient.POST("/v1/workspaces/{workspaceID}/products", {
          params: { path: command.path, header: { "Idempotency-Key": key } },
          body: command.body,
        }),
      );
    case "product.update":
      return unwrap<Schemas["ProductProductsUpdateOutput"]>(
        await apiClient.PATCH(
          "/v1/workspaces/{workspaceID}/products/{productID}",
          {
            params: { path: command.path, header: { "Idempotency-Key": key } },
            body: command.body,
          },
        ),
      );
    case "product.archive":
      return unwrap<Schemas["ProductProductsArchiveOutput"]>(
        await apiClient.POST(
          "/v1/workspaces/{workspaceID}/products/{productID}/archive",
          {
            params: { path: command.path, header: { "Idempotency-Key": key } },
            body: command.body,
          },
        ),
      );
    case "product.restore":
      return unwrap<Schemas["ProductProductsRestoreOutput"]>(
        await apiClient.POST(
          "/v1/workspaces/{workspaceID}/products/{productID}/restore",
          {
            params: { path: command.path, header: { "Idempotency-Key": key } },
            body: command.body,
          },
        ),
      );
    case "environment.create":
      return unwrap<Schemas["ProductProductEnvironmentsCreateOutput"]>(
        await apiClient.POST(
          "/v1/workspaces/{workspaceID}/products/{productID}/environments",
          {
            params: { path: command.path, header: { "Idempotency-Key": key } },
            body: command.body,
          },
        ),
      );
    case "environment.update":
      return unwrap<Schemas["ProductProductEnvironmentsUpdateOutput"]>(
        await apiClient.PATCH(
          "/v1/workspaces/{workspaceID}/products/{productID}/environments/{environmentID}",
          {
            params: { path: command.path, header: { "Idempotency-Key": key } },
            body: command.body,
          },
        ),
      );
    case "environment.archive":
      return unwrap<Schemas["ProductProductEnvironmentsArchiveOutput"]>(
        await apiClient.POST(
          "/v1/workspaces/{workspaceID}/products/{productID}/environments/{environmentID}/archive",
          {
            params: { path: command.path, header: { "Idempotency-Key": key } },
            body: command.body,
          },
        ),
      );
    case "environment.restore":
      return unwrap<Schemas["ProductProductEnvironmentsRestoreOutput"]>(
        await apiClient.POST(
          "/v1/workspaces/{workspaceID}/products/{productID}/environments/{environmentID}/restore",
          {
            params: { path: command.path, header: { "Idempotency-Key": key } },
            body: command.body,
          },
        ),
      );
    case "projects.set":
      return unwrap<Schemas["ProductProductProjectsSetOutput"]>(
        await apiClient.PUT(
          "/v1/workspaces/{workspaceID}/products/{productID}/projects",
          {
            params: { path: command.path, header: { "Idempotency-Key": key } },
            body: command.body,
          },
        ),
      );
  }
}
