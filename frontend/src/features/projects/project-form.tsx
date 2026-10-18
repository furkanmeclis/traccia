"use client";
import { useId, useState } from "react";
import { Button } from "@/components/ui/button";
import { TextLimitInput as Input } from "@/components/ui/text-limit-input";
import { TextLimitTextarea as Textarea } from "@/components/ui/text-limit-input";
import { Label } from "@/components/ui/label";
import { DatePicker } from "@/components/ui/date-picker";
import { Checkbox } from "@/components/ui/checkbox";
import { useLocale } from "@/providers/locale-provider";
import { useProjectReferences } from "./queries";
import { projectService, type Project, type Schemas } from "./project.service";
import {
  LoadNotice,
  MutationNotice,
  useProjectMutation,
  validDates,
} from "./project-ui";

export function ProjectForm({
  workspaceId,
  project,
  canManage = true,
  reviewLatest,
  onDone,
}: {
  workspaceId: string;
  project?: Project;
  canManage?: boolean;
  reviewLatest: () => Promise<unknown>;
  onDone?: () => void;
}) {
  const { t } = useLocale();
  const id = useId();
  const references = useProjectReferences(workspaceId, true);
  const [draft, setDraft] = useState({
    name: project?.name ?? "",
    summary: project?.summary ?? "",
    lead_user_id: project?.lead_user_id ?? "",
    start_date: project?.start_date ?? "",
    target_date: project?.target_date ?? "",
    team_ids: [] as string[],
  });
  const [version] = useState(project?.version);
  const reset = () => {
    setDraft({
      name: "",
      summary: "",
      lead_user_id: "",
      start_date: "",
      target_date: "",
      team_ids: [],
    });
    onDone?.();
  };
  const mutation = useProjectMutation({
    workspaceId,
    projectId: project?.id,
    operationId: project ? "project-edit" : "project-create",
    operationLabel: t(project ? "projects.edit_project" : "projects.create"),
    reviewLatest,
    run: (
      input:
        | Schemas["ProductProjectsCreateInput"]
        | Schemas["ProductProjectsUpdateInput"],
      key,
    ) =>
      project
        ? projectService.updateProject(
            workspaceId,
            project.id,
            input as Schemas["ProductProjectsUpdateInput"],
            key,
          )
        : projectService.createProject(
            workspaceId,
            input as Schemas["ProductProjectsCreateInput"],
            key,
          ),
    onSaved: reset,
  });
  const ready = canManage && references.isSuccess && !references.isError;
  const valid =
    draft.name.trim().length > 0 &&
    (Boolean(project) || draft.team_ids.length > 0) &&
    validDates(draft.start_date, draft.target_date);
  return (
    <form
      className="space-y-4"
      onSubmit={(event) => {
        event.preventDefault();
        if (!ready || !valid) return;
        const base = {
          name: draft.name.trim(),
          summary: draft.summary.trim(),
          lead_user_id: draft.lead_user_id || null,
          start_date: draft.start_date || null,
          target_date: draft.target_date || null,
        };
        mutation.submit(
          project
            ? { ...base, expected_version: version! }
            : { ...base, team_ids: draft.team_ids },
        );
      }}
    >
      <LoadNotice
        pending={references.isPending}
        error={references.isError}
        retry={() => references.refetch()}
      />
      <fieldset disabled={mutation.locked || !ready} className="space-y-4">
        <div className="space-y-2">
          <Label htmlFor={`${id}-name`}>{t("projects.name")}</Label>
          <Input
            id={`${id}-name`}
            required
            maxCodePoints={100}
            value={draft.name}
            onChange={(event) =>
              setDraft({ ...draft, name: event.target.value })
            }
          />
        </div>
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
        <div className="space-y-2">
          <Label htmlFor={`${id}-lead`}>{t("projects.lead")}</Label>
          <select
            id={`${id}-lead`}
            className="border-input bg-background h-10 w-full rounded-md border px-3 text-sm"
            value={draft.lead_user_id}
            onChange={(event) =>
              setDraft({ ...draft, lead_user_id: event.target.value })
            }
          >
            <option value="">{t("projects.no_lead")}</option>
            {ready &&
              !references.data.members.some(
                (member) => member.id === draft.lead_user_id,
              ) &&
              draft.lead_user_id && (
                <option value={draft.lead_user_id} disabled>
                  {t("projects.unavailable_lead")}
                </option>
              )}
            {references.data?.members.map((member) => (
              <option key={member.id} value={member.id}>
                {`${member.name} ${member.surname}`.trim() ||
                  t("projects.member")}
              </option>
            ))}
          </select>
        </div>
        <div className="grid gap-4 sm:grid-cols-2">
          {(["start_date", "target_date"] as const).map((field) => (
            <div className="space-y-2" key={field}>
              <Label htmlFor={`${id}-${field}`}>
                {t(
                  field === "start_date"
                    ? "projects.start_date"
                    : "projects.target_date",
                )}
              </Label>
              <DatePicker
                id={`${id}-${field}`}
                value={draft[field]}
                disabled={mutation.locked || !ready}
                onChange={(value) => setDraft({ ...draft, [field]: value })}
              />
              <Button
                type="button"
                variant="ghost"
                size="sm"
                onClick={() => setDraft({ ...draft, [field]: "" })}
              >
                {t("projects.clear_date")}
              </Button>
            </div>
          ))}
        </div>
        {!validDates(draft.start_date, draft.target_date) && (
          <p role="alert" className="text-destructive text-sm">
            {t("projects.date_order")}
          </p>
        )}
        {!project && (
          <fieldset className="space-y-2">
            <legend className="mb-2 text-sm font-medium">
              {t("projects.teams")}
            </legend>
            <p className="text-muted-foreground text-sm">
              {t("projects.teams_help")}
            </p>
            {references.data?.teams.map((team) => (
              <label
                key={team.id}
                className="flex items-center gap-3 rounded-md border p-3"
              >
                <Checkbox
                  type="button"
                  checked={draft.team_ids.includes(team.id)}
                  onCheckedChange={(checked) =>
                    setDraft({
                      ...draft,
                      team_ids: checked
                        ? [...draft.team_ids, team.id]
                        : draft.team_ids.filter((id) => id !== team.id),
                    })
                  }
                />
                <span className="min-w-0 break-words">
                  {team.name}{" "}
                  <span className="text-muted-foreground">{team.code}</span>
                </span>
              </label>
            ))}
            {ready && references.data.teams.length === 0 && (
              <p className="text-muted-foreground text-sm">
                {t("projects.no_teams")}
              </p>
            )}
          </fieldset>
        )}
      </fieldset>
      <div className="flex flex-wrap gap-3">
        <Button type="submit" disabled={!ready || !valid || mutation.locked}>
          {t(project ? "projects.save" : "projects.create")}
        </Button>
        {project && (
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
        canRetry={ready}
        mutation={mutation}
        onReconcile={reset}
      />
    </form>
  );
}
