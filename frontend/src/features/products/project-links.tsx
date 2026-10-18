"use client";
import { useState } from "react";
import Link from "next/link";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import type { Project } from "@/features/projects/project.service";
import { useLocale } from "@/providers/locale-provider";
import { useProductChoices } from "./queries";
import {
  Section,
  LoadNotice,
  MutationNotice,
  useProductMutation,
} from "./product-ui";
export function ProjectLinks({
  workspaceId,
  productId,
  projects,
  historicalProjects,
  version,
  canManage,
  reviewLatest,
}: {
  workspaceId: string;
  productId: string;
  projects: Project[];
  historicalProjects: Project[];
  version: number;
  canManage: boolean;
  reviewLatest: () => Promise<unknown>;
}) {
  const { t } = useLocale();
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState<string[]>([]);
  const [baseline, setBaseline] = useState(version);
  const choices = useProductChoices(workspaceId, editing && canManage);
  const mutation = useProductMutation({
    workspaceId,
    productId,
    operationId: "projects-set",
    operationLabel: t("products.edit_projects"),
    reviewLatest,
    onSaved: () => setEditing(false),
  });
  const available = [
    ...(choices.data?.items ?? []),
    ...projects.filter(
      (project) => !choices.data?.items.some((item) => item.id === project.id),
    ),
  ];
  const stale = editing && baseline !== version;
  const ready =
    !stale &&
    canManage &&
    choices.isSuccess &&
    !choices.isError &&
    !choices.isFetching;
  return (
    <Section title={t("products.projects")}>
      <ul className="space-y-2">
        {projects.map((project) => (
          <li key={project.id} className="break-words">
            <Link
              href={`/w/${workspaceId}/projects/${project.id}`}
              className="text-primary underline underline-offset-4"
            >
              {project.name}
            </Link>
            {project.archived && (
              <span className="text-muted-foreground text-sm">
                {" "}
                · {t("products.archived")}
              </span>
            )}
          </li>
        ))}
      </ul>
      {historicalProjects.length > 0 && (
        <div className="space-y-2">
          <h3 className="text-sm font-medium">
            {t("products.historical_projects")}
          </h3>
          <ul className="space-y-2">
            {historicalProjects.map((project) => (
              <li
                key={project.id}
                className="text-muted-foreground text-sm break-words"
              >
                <Link
                  href={`/w/${workspaceId}/projects/${project.id}`}
                  className="underline underline-offset-4"
                >
                  {project.name}
                </Link>
                {project.archived && ` · ${t("products.archived")}`}
              </li>
            ))}
          </ul>
        </div>
      )}
      {!projects.length && (
        <p className="text-muted-foreground text-sm">
          {t("products.no_projects")}
        </p>
      )}
      {!editing && canManage && (
        <Button
          type="button"
          variant="outline"
          onClick={() => {
            setDraft(projects.map((project) => project.id));
            setBaseline(version);
            setEditing(true);
          }}
        >
          {t("products.edit_projects")}
        </Button>
      )}
      {editing && (
        <form
          className="space-y-4"
          onSubmit={(event) => {
            event.preventDefault();
            if (ready)
              mutation.submit({
                action: "projects.set",
                path: { workspaceID: workspaceId, productID: productId },
                body: {
                  project_ids: draft,
                  expected_product_version: baseline,
                },
              });
          }}
        >
          <LoadNotice
            pending={choices.isPending}
            error={choices.isError}
            retry={() => choices.refetch()}
          />
          <fieldset disabled={!ready || mutation.locked} className="space-y-2">
            <legend className="mb-2 text-sm">
              {t("products.projects_help")}
            </legend>
            {available.map((project) => (
              <label
                key={project.id}
                className="flex items-center gap-3 rounded-md border p-3"
              >
                <Checkbox
                  type="button"
                  disabled={project.archived && !draft.includes(project.id)}
                  checked={draft.includes(project.id)}
                  onCheckedChange={(checked) =>
                    setDraft(
                      checked
                        ? [...draft, project.id]
                        : draft.filter((id) => id !== project.id),
                    )
                  }
                />
                <span className="min-w-0 break-words">
                  {project.name}
                  {project.archived && (
                    <span className="text-muted-foreground">
                      {" "}
                      · {t("products.retained_project")}
                    </span>
                  )}
                </span>
              </label>
            ))}
          </fieldset>
          {stale && (
            <p role="alert" className="text-sm">
              {t("products.stale_draft")}
            </p>
          )}
          <div className="flex flex-wrap gap-3">
            <Button type="submit" disabled={!ready || mutation.locked}>
              {t("products.save")}
            </Button>
            <Button
              type="button"
              variant="outline"
              disabled={mutation.locked}
              onClick={() => setEditing(false)}
            >
              {t(stale ? "products.discard_draft" : "products.cancel")}
            </Button>
          </div>
          <MutationNotice
            canRetry={ready}
            mutation={mutation}
            onReconcile={() => setEditing(false)}
          />
        </form>
      )}
    </Section>
  );
}
