"use client";
import { useId, useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Label } from "@/components/ui/label";
import { DatePicker } from "@/components/ui/date-picker";
import { useLocale } from "@/providers/locale-provider";
import {
  projectService,
  type Milestone,
  type Schemas,
} from "./project.service";
import { Section, MutationNotice, useProjectMutation } from "./project-ui";
type Context = {
  workspaceId: string;
  projectId: string;
  version: number;
  canManage: boolean;
  showCreate: boolean;
  canRestore: boolean;
  reviewLatest: () => Promise<unknown>;
};
function MilestoneForm({
  context,
  item,
  onDone,
}: {
  context: Context;
  item?: Milestone;
  onDone?: () => void;
}) {
  const { t } = useLocale();
  const id = useId();
  const [draft, setDraft] = useState({
    name: item?.name ?? "",
    description: item?.description ?? "",
    target_date: item?.target_date ?? "",
  });
  const [baseline, setBaseline] = useState({
    project: context.version,
    row: item?.version,
  });
  const reset = () => {
    setDraft({ name: "", description: "", target_date: "" });
    setBaseline({ project: context.version, row: item?.version });
    onDone?.();
  };
  const mutation = useProjectMutation({
    workspaceId: context.workspaceId,
    projectId: context.projectId,
    operationId: `milestone-edit:${item?.id ?? "create"}`,
    operationLabel: t(
      item ? "projects.edit_milestone" : "projects.create_milestone",
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
        | Schemas["ProductProjectMilestonesCreateInput"]
        | Schemas["ProductProjectMilestonesUpdateInput"],
      key,
    ) =>
      item
        ? projectService.updateMilestone(
            context.workspaceId,
            context.projectId,
            item.id,
            input as Schemas["ProductProjectMilestonesUpdateInput"],
            key,
          )
        : projectService.createMilestone(
            context.workspaceId,
            context.projectId,
            input as Schemas["ProductProjectMilestonesCreateInput"],
            key,
          ),
  });
  return (
    <form
      className="space-y-4"
      onSubmit={(event) => {
        event.preventDefault();
        if (!context.canManage || !draft.name.trim()) return;
        const body = {
          name: draft.name.trim(),
          description: draft.description.trim(),
          target_date: draft.target_date || null,
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
          <Label htmlFor={`${id}-name`}>{t("projects.milestone_name")}</Label>
          <Input
            id={`${id}-name`}
            required
            maxLength={100}
            value={draft.name}
            onChange={(event) =>
              setDraft({ ...draft, name: event.target.value })
            }
          />
        </div>
        <div className="space-y-2">
          <Label htmlFor={`${id}-description`}>
            {t("projects.description")}
          </Label>
          <Textarea
            id={`${id}-description`}
            maxLength={2000}
            value={draft.description}
            onChange={(event) =>
              setDraft({ ...draft, description: event.target.value })
            }
          />
        </div>
        <div className="max-w-md space-y-2">
          <Label htmlFor={`${id}-target`}>{t("projects.target_date")}</Label>
          <DatePicker
            id={`${id}-target`}
            disabled={mutation.locked || !context.canManage}
            value={draft.target_date}
            onChange={(value) => setDraft({ ...draft, target_date: value })}
          />
          <Button
            type="button"
            variant="ghost"
            size="sm"
            onClick={() => setDraft({ ...draft, target_date: "" })}
          >
            {t("projects.clear_date")}
          </Button>
        </div>
      </fieldset>
      <div className="flex flex-wrap gap-3">
        <Button
          type="submit"
          disabled={mutation.locked || !context.canManage || !draft.name.trim()}
        >
          {t(item ? "projects.save" : "projects.create_milestone")}
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
function MilestoneRow({
  item,
  context,
}: {
  item: Milestone;
  context: Context;
}) {
  const { t } = useLocale();
  const [editing, setEditing] = useState(false);
  const lifecycle = useProjectMutation({
    workspaceId: context.workspaceId,
    projectId: context.projectId,
    operationId: `milestone-lifecycle:${item.id}`,
    operationLabel: `${t(item.archived ? "projects.restore" : "projects.archive")}: ${item.name}`,
    reviewLatest: context.reviewLatest,
    run: (
      input: {
        action: "archive" | "restore";
        body: Schemas["ProductProjectMilestonesArchiveInput"];
      },
      key,
    ) =>
      input.action === "restore"
        ? projectService.restoreMilestone(
            context.workspaceId,
            context.projectId,
            item.id,
            input.body,
            key,
          )
        : projectService.archiveMilestone(
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
        <h3 className="font-medium break-words">{item.name}</h3>
        <p className="text-muted-foreground mt-1 text-sm break-words whitespace-pre-wrap">
          {item.description}
        </p>
        {item.target_date && (
          <p className="mt-2 text-sm">
            {t("projects.target_date")}:{" "}
            <time dateTime={item.target_date}>{item.target_date}</time>
          </p>
        )}
        {item.archived && (
          <p className="text-muted-foreground text-sm">
            {t("projects.archived")}
          </p>
        )}
      </div>
      {editing ? (
        <MilestoneForm
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
export function MilestoneSettings({
  items,
  ...context
}: Context & { items: Milestone[] }) {
  const { t } = useLocale();
  return (
    <Section title={t("projects.milestones")}>
      {items.length === 0 && (
        <p className="text-muted-foreground text-sm">
          {t("projects.no_milestones")}
        </p>
      )}
      {items.map((item) => (
        <MilestoneRow key={item.id} item={item} context={context} />
      ))}
      {context.showCreate && (
        <div className="border-t pt-4">
          <h3 className="mb-4 font-medium">{t("projects.create_milestone")}</h3>
          <MilestoneForm context={context} />
        </div>
      )}
    </Section>
  );
}
