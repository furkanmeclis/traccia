"use client";
import { useId, useState } from "react";
import { Button } from "@/components/ui/button";
import { TextLimitInput as Input } from "@/components/ui/text-limit-input";
import { TextLimitTextarea as Textarea } from "@/components/ui/text-limit-input";
import { Label } from "@/components/ui/label";
import { useLocale } from "@/providers/locale-provider";
import { projectService, type Resource, type Schemas } from "./project.service";
import {
  Section,
  MutationNotice,
  safeResourceUrl,
  useProjectMutation,
} from "./project-ui";
type Context = {
  workspaceId: string;
  projectId: string;
  version: number;
  canManage: boolean;
  showCreate: boolean;
  canRestore: boolean;
  reviewLatest: () => Promise<unknown>;
};
function ResourceForm({
  context,
  item,
  onDone,
}: {
  context: Context;
  item?: Resource;
  onDone?: () => void;
}) {
  const { t } = useLocale();
  const id = useId();
  const empty = {
    kind: "link" as Resource["kind"],
    title: "",
    url: "",
    summary: "",
    provider: "",
    reference: "",
  };
  const [draft, setDraft] = useState(
    item
      ? {
          kind: item.kind,
          title: item.title,
          url: item.url,
          summary: item.summary,
          provider: item.provider,
          reference: item.reference,
        }
      : empty,
  );
  const [baseline, setBaseline] = useState({
    project: context.version,
    row: item?.version,
  });
  const reset = () => {
    setDraft(empty);
    setBaseline({ project: context.version, row: item?.version });
    onDone?.();
  };
  const mutation = useProjectMutation({
    workspaceId: context.workspaceId,
    projectId: context.projectId,
    operationId: `resource-edit:${item?.id ?? "create"}`,
    operationLabel: t(
      item ? "projects.edit_resource" : "projects.create_resource",
    ),
    reviewLatest: context.reviewLatest,
    onSaved: (result) => {
      reset();
      setBaseline({
        project: result.project_version ?? context.version,
        row: item?.version,
      });
    },
    run: (
      input:
        | Schemas["ProductProjectResourcesCreateInput"]
        | Schemas["ProductProjectResourcesUpdateInput"],
      key,
    ) =>
      item
        ? projectService.updateResource(
            context.workspaceId,
            context.projectId,
            item.id,
            input as Schemas["ProductProjectResourcesUpdateInput"],
            key,
          )
        : projectService.createResource(
            context.workspaceId,
            context.projectId,
            input as Schemas["ProductProjectResourcesCreateInput"],
            key,
          ),
  });
  const valid = !!draft.title.trim() && !!safeResourceUrl(draft.url.trim());
  return (
    <form
      className="space-y-4"
      onSubmit={(event) => {
        event.preventDefault();
        if (!valid || !context.canManage) return;
        const body = {
          ...draft,
          title: draft.title.trim(),
          url: draft.url.trim(),
          summary: draft.summary.trim(),
          provider: draft.provider.trim(),
          reference: draft.reference.trim(),
          expected_project_version: item ? baseline.project : context.version,
        };
        mutation.submit(
          item ? { ...body, expected_version: baseline.row! } : body,
        );
      }}
    >
      <fieldset
        disabled={mutation.locked || !context.canManage}
        className="space-y-4"
      >
        <div className="space-y-2">
          <Label htmlFor={`${id}-kind`}>{t("projects.resource_kind")}</Label>
          <select
            id={`${id}-kind`}
            className="border-input bg-background h-10 w-full rounded-md border px-3 text-sm"
            value={draft.kind}
            onChange={(event) =>
              setDraft({
                ...draft,
                kind: event.target.value as Resource["kind"],
              })
            }
          >
            {(["document", "repository", "link"] as const).map((kind) => (
              <option key={kind} value={kind}>
                {t(
                  kind === "document"
                    ? "projects.kind_document"
                    : kind === "repository"
                      ? "projects.kind_repository"
                      : "projects.kind_link",
                )}
              </option>
            ))}
          </select>
        </div>
        {(["title", "url", "provider", "reference"] as const).map((field) => (
          <div className="space-y-2" key={field}>
            <Label htmlFor={`${id}-${field}`}>
              {t(
                field === "title"
                  ? "projects.resource_title"
                  : field === "url"
                    ? "projects.resource_url"
                    : field === "provider"
                      ? "projects.resource_provider"
                      : "projects.resource_reference",
              )}
            </Label>
            <Input
              id={`${id}-${field}`}
              type={field === "url" ? "url" : "text"}
              required={field === "title" || field === "url"}
              maxBytes={field === "url" ? 2048 : undefined}
              maxCodePoints={
                field === "url" ? undefined : field === "reference" ? 200 : 100
              }
              value={draft[field]}
              onChange={(event) =>
                setDraft({ ...draft, [field]: event.target.value })
              }
            />
          </div>
        ))}
        <div className="space-y-2">
          <Label htmlFor={`${id}-summary`}>{t("projects.summary")}</Label>
          <Textarea
            id={`${id}-summary`}
            maxCodePoints={2000}
            value={draft.summary}
            onChange={(event) =>
              setDraft({ ...draft, summary: event.target.value })
            }
          />
        </div>
        {draft.url && !safeResourceUrl(draft.url.trim()) && (
          <p role="alert" className="text-destructive text-sm">
            {t("projects.url_error")}
          </p>
        )}
      </fieldset>
      <div className="flex flex-wrap gap-3">
        <Button
          type="submit"
          disabled={mutation.locked || !context.canManage || !valid}
        >
          {t(item ? "projects.save" : "projects.create_resource")}
        </Button>
        {item && (
          <Button
            type="button"
            variant="outline"
            disabled={mutation.locked}
            onClick={onDone}
          >
            {t("projects.cancel")}
          </Button>
        )}
      </div>
      <MutationNotice
        canRetry={context.canManage}
        mutation={mutation}
        onReconcile={reset}
      />
    </form>
  );
}
function ResourceRow({ item, context }: { item: Resource; context: Context }) {
  const { t } = useLocale();
  const [editing, setEditing] = useState(false);
  const href = safeResourceUrl(item.url);
  const lifecycle = useProjectMutation({
    workspaceId: context.workspaceId,
    projectId: context.projectId,
    operationId: `resource-lifecycle:${item.id}`,
    operationLabel: `${t(item.archived ? "projects.restore" : "projects.archive")}: ${item.title}`,
    reviewLatest: context.reviewLatest,
    run: (
      input: {
        action: "archive" | "restore";
        body: Schemas["ProductProjectResourcesArchiveInput"];
      },
      key,
    ) =>
      input.action === "restore"
        ? projectService.restoreResource(
            context.workspaceId,
            context.projectId,
            item.id,
            input.body,
            key,
          )
        : projectService.archiveResource(
            context.workspaceId,
            context.projectId,
            item.id,
            input.body,
            key,
          ),
  });
  return (
    <article className="space-y-4 rounded-lg border p-4">
      <div>
        <p className="text-muted-foreground text-xs">
          {t(
            item.kind === "document"
              ? "projects.kind_document"
              : item.kind === "repository"
                ? "projects.kind_repository"
                : "projects.kind_link",
          )}
        </p>
        <h3 className="mt-1 font-medium break-words">{item.title}</h3>
        {href && (
          <a
            href={href}
            target="_blank"
            rel="noopener noreferrer"
            className="text-primary mt-2 block text-sm break-all underline underline-offset-4"
          >
            {item.url}
            <span className="sr-only"> {t("projects.opens_new_tab")}</span>
          </a>
        )}
        <p className="text-muted-foreground mt-2 text-sm break-words whitespace-pre-wrap">
          {item.summary}
        </p>
        {(item.provider || item.reference) && (
          <p className="text-muted-foreground mt-2 text-sm break-words">
            {item.provider}
            {item.provider && item.reference ? " · " : ""}
            {item.reference}
          </p>
        )}
        {item.archived && (
          <p className="text-muted-foreground text-sm">
            {t("projects.archived")}
          </p>
        )}
      </div>
      {editing ? (
        <ResourceForm
          context={context}
          item={item}
          onDone={() => setEditing(false)}
        />
      ) : (
        <div className="flex flex-wrap gap-3">
          {context.canManage && !item.archived && (
            <Button
              type="button"
              variant="outline"
              disabled={lifecycle.locked}
              onClick={() => setEditing(true)}
            >
              {t("projects.edit")}
            </Button>
          )}
          {((context.canManage && !item.archived) ||
            (context.canRestore && item.archived)) && (
            <Button
              type="button"
              variant="outline"
              disabled={lifecycle.locked}
              onClick={() =>
                lifecycle.submit({
                  action: item.archived ? "restore" : "archive",
                  body: {
                    expected_version: item.version,
                    expected_project_version: context.version,
                  },
                })
              }
            >
              {t(item.archived ? "projects.restore" : "projects.archive")}
            </Button>
          )}
        </div>
      )}
      <MutationNotice
        canRetry={context.canManage || context.canRestore}
        mutation={lifecycle}
        onReconcile={() => setEditing(false)}
      />
    </article>
  );
}
export function ResourceSettings({
  items,
  ...context
}: Context & { items: Resource[] }) {
  const { t } = useLocale();
  return (
    <Section title={t("projects.resources")}>
      <p className="text-muted-foreground text-sm">
        {t("projects.resources_help")}
      </p>
      {items.length === 0 && (
        <p className="text-muted-foreground text-sm">
          {t("projects.no_resources")}
        </p>
      )}
      {items.map((item) => (
        <ResourceRow key={item.id} item={item} context={context} />
      ))}
      {context.showCreate && (
        <div className="border-t pt-4">
          <h3 className="mb-4 font-medium">{t("projects.create_resource")}</h3>
          <ResourceForm context={context} />
        </div>
      )}
    </Section>
  );
}
