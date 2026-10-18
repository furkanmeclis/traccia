"use client";
import { issueFieldKeys, issuePriorityKeys } from "./issue-copy";
import { useId, useState } from "react";
import { Button } from "@/components/ui/button";
import { TextLimitInput } from "@/components/ui/text-limit-input";
import { Label } from "@/components/ui/label";
import { DatePicker } from "@/components/ui/date-picker";
import { useLocale } from "@/providers/locale-provider";
import { useIssueReferences, useIssueSelection } from "./queries";
import type { Issue } from "./issue.service";
import { LoadNotice, MutationNotice, useIssueMutation } from "./issue-ui";
import { validIssueTitle, validIssueDates, validIssueEffort } from "./snapshot";
export const priorities = ["none", "urgent", "high", "normal", "low"] as const;
type Draft = {
  title: string;
  priority: Issue["priority"];
  team_id: string;
  status_id: string;
  assignee_id: string;
  project_id: string;
  milestone_id: string;
  start_date: string;
  target_date: string;
  effort: string;
  label_ids: string[];
};
const selectClass =
  "border-input bg-background h-10 w-full rounded-md border px-3 text-sm";
export function IssueForm({
  workspaceId,
  issue,
  move = false,
  initialTeam = "",
  initialProject = "",
  canManage,
  reviewLatest,
  onDone,
}: {
  workspaceId: string;
  issue?: Issue;
  move?: boolean;
  initialTeam?: string;
  initialProject?: string;
  canManage: boolean;
  reviewLatest: () => Promise<unknown>;
  onDone?: () => void;
}) {
  const { t } = useLocale();
  const id = useId();
  const initial: Draft = {
    title: issue?.title ?? "",
    priority: issue?.priority ?? "none",
    team_id: issue?.team_id ?? initialTeam,
    status_id: issue?.status_id ?? "",
    assignee_id: issue?.assignee_id ?? "",
    project_id: issue?.project_id ?? initialProject,
    milestone_id: issue?.milestone_id ?? "",
    start_date: issue?.start_date ?? "",
    target_date: issue?.target_date ?? "",
    effort: issue?.effort == null ? "" : String(issue.effort),
    label_ids: [...(issue?.label_ids ?? [])],
  };
  const [draft, setDraft] = useState(initial);
  const [baseline] = useState(issue?.version);
  const references = useIssueReferences(workspaceId, true);
  const selection = useIssueSelection(
    workspaceId,
    draft.team_id,
    draft.project_id,
    true,
  );
  const reset = () => {
    setDraft({ ...initial, title: "", label_ids: [] });
    onDone?.();
  };
  const mutation = useIssueMutation({
    workspaceId,
    issueId: issue?.id,
    operationId: move ? "issue-move" : issue ? "issue-edit" : "issue-create",
    operationLabel: t(
      move ? "issues.move" : issue ? "issues.edit" : "issues.create",
    ),
    reviewLatest,
    onSaved: reset,
  });
  const stale = !!issue && baseline !== issue.version;
  const loaded =
    references.isSuccess &&
    !references.isError &&
    !references.isFetching &&
    selection.isSuccess &&
    !selection.isError &&
    !selection.isFetching;
  const ready = canManage && loaded && !stale;
  const retained = !move && !!issue;
  const project = selection.data?.project;
  const validProject =
    !draft.project_id ||
    (retained && draft.project_id === issue?.project_id) ||
    (!!project &&
      !project.project.archived &&
      project.teams.some(
        (team) => team.id === draft.team_id && !team.archived,
      ));
  const validMilestone =
    !draft.milestone_id ||
    (retained &&
      draft.project_id === issue?.project_id &&
      draft.milestone_id === issue?.milestone_id) ||
    !!project?.milestones.some(
      (milestone) => milestone.id === draft.milestone_id && !milestone.archived,
    );
  const validStatus = selection.data?.statuses.some(
    (status) =>
      status.id === draft.status_id &&
      (!status.archived || (retained && status.id === issue?.status_id)),
  );
  const validLabels = draft.label_ids.every((labelID) =>
    selection.data?.labels.some(
      (label) =>
        label.id === labelID &&
        (!label.archived ||
          (issue?.label_ids?.includes(labelID) &&
            (!move || !label.team_id || label.team_id === draft.team_id))),
    ),
  );
  const validTeam = references.data?.teams.some(
    (team) =>
      team.id === draft.team_id &&
      (!team.archived || (retained && team.id === issue?.team_id)),
  );
  const validMember =
    !draft.assignee_id ||
    (retained && draft.assignee_id === issue?.assignee_id) ||
    references.data?.members.some(
      (member) =>
        member.id === draft.assignee_id && member.active && member.assignable,
    );
  const valid =
    validTeam &&
    validStatus &&
    validProject &&
    validMilestone &&
    validLabels &&
    (move ||
      (validIssueTitle(draft.title) &&
        validMember &&
        validIssueDates(draft.start_date, draft.target_date) &&
        validIssueEffort(draft.effort))) &&
    (!move || draft.team_id !== issue?.team_id);
  function choice(
    field:
      | "team_id"
      | "status_id"
      | "assignee_id"
      | "project_id"
      | "milestone_id"
      | "priority",
    choices: { id: string; name: string; disabled?: boolean }[],
    required = false,
  ) {
    const selected = draft[field];
    return (
      <div className="space-y-2">
        <Label htmlFor={`${id}-${field}`}>{t(issueFieldKeys[field])}</Label>
        <select
          id={`${id}-${field}`}
          className={selectClass}
          value={selected}
          required={required}
          onChange={(event) =>
            setDraft({ ...draft, [field]: event.target.value })
          }
        >
          <option value="">
            {t(required ? "issues.choose" : "issues.unassigned")}
          </option>
          {!!selected && !choices.some((item) => item.id === selected) && (
            <option value={selected} disabled>
              {t("issues.unavailable_reference")}
            </option>
          )}
          {choices.map((item) => (
            <option key={item.id} value={item.id} disabled={item.disabled}>
              {item.name}
              {item.disabled ? ` · ${t("issues.inactive")}` : ""}
            </option>
          ))}
        </select>
      </div>
    );
  }
  return (
    <form
      className="space-y-4"
      onSubmit={(event) => {
        event.preventDefault();
        if (!ready || !valid || mutation.locked) return;
        const path = { workspaceID: workspaceId, issueID: issue?.id ?? "" };
        if (move && issue) {
          mutation.submit({
            action: "issue.move",
            path,
            body: {
              expected_version: baseline!,
              team_id: draft.team_id,
              status_id: draft.status_id,
              project_id: draft.project_id || null,
              milestone_id: draft.milestone_id || null,
              label_ids: [...draft.label_ids],
            },
          });
          return;
        }
        const metadata = {
          title: draft.title.trim(),
          priority: draft.priority,
          status_id: draft.status_id,
          assignee_id: draft.assignee_id || null,
          project_id: draft.project_id || null,
          milestone_id: draft.milestone_id || null,
          start_date: draft.start_date || null,
          target_date: draft.target_date || null,
          effort: draft.effort === "" ? null : Number(draft.effort),
          label_ids: [...draft.label_ids],
        };
        mutation.submit(
          issue
            ? {
                action: "issue.update",
                path,
                body: {
                  ...metadata,
                  expected_version: baseline!,
                },
              }
            : {
                action: "issue.create",
                path: { workspaceID: workspaceId },
                body: { ...metadata, team_id: draft.team_id },
              },
        );
      }}
    >
      <LoadNotice
        pending={references.isPending}
        error={references.isError}
        retry={() => references.refetch()}
      />
      {draft.team_id && (
        <LoadNotice
          pending={selection.isPending}
          error={selection.isError}
          retry={() => selection.refetch()}
        />
      )}
      <fieldset
        disabled={
          !canManage ||
          mutation.locked ||
          stale ||
          !references.isSuccess ||
          references.isError
        }
        className="space-y-4"
      >
        {!move && (
          <div className="space-y-2">
            <Label htmlFor={`${id}-title`}>{t("issues.issue_title")}</Label>
            <TextLimitInput
              id={`${id}-title`}
              value={draft.title}
              required
              maxCodePoints={255}
              onChange={(event) =>
                setDraft({ ...draft, title: event.target.value })
              }
            />
            {draft.title && !validIssueTitle(draft.title) && (
              <p role="alert">{t("issues.title_invalid")}</p>
            )}
          </div>
        )}
        <div className="grid gap-4 sm:grid-cols-2">
          {(!issue || move) &&
            choice(
              "team_id",
              (references.data?.teams ?? [])
                .filter((team) => !team.archived)
                .map((team) => ({
                  id: team.id,
                  name: `${team.name} (${team.code})`,
                })),
              true,
            )}
          {choice(
            "status_id",
            (selection.data?.statuses ?? []).map((status) => ({
              id: status.id,
              name:
                status.name +
                (status.archived ? ` · ${t("issues.inactive")}` : ""),
              disabled:
                status.archived &&
                !(retained && status.id === issue?.status_id),
            })),
            true,
          )}
          {!move &&
            choice(
              "priority",
              priorities.map((priority) => ({
                id: priority,
                name: t(issuePriorityKeys[priority]),
              })),
              true,
            )}
          {!move &&
            choice(
              "assignee_id",
              (references.data?.members ?? []).map((member) => ({
                id: member.id,
                name:
                  (`${member.name} ${member.surname}`.trim() ||
                    t("issues.member")) +
                  (!member.active || !member.assignable
                    ? ` · ${t("issues.inactive")}`
                    : ""),
                disabled:
                  (!member.active || !member.assignable) &&
                  !(retained && member.id === issue?.assignee_id),
              })),
            )}
          {choice(
            "project_id",
            (references.data?.projects ?? []).map((project) => ({
              id: project.id,
              name:
                project.name +
                (project.archived ? ` · ${t("issues.inactive")}` : ""),
              disabled:
                project.archived &&
                !(retained && project.id === issue?.project_id),
            })),
          )}
          {choice(
            "milestone_id",
            (project?.milestones ?? []).map((milestone) => ({
              id: milestone.id,
              name:
                milestone.name +
                (milestone.archived ? ` · ${t("issues.inactive")}` : ""),
              disabled:
                milestone.archived &&
                !(
                  retained &&
                  milestone.id === issue?.milestone_id &&
                  draft.project_id === issue?.project_id
                ),
            })),
          )}
        </div>
        {move && (
          <p className="text-muted-foreground text-sm">
            {t("issues.move_help")}
          </p>
        )}
        {loaded &&
          (!validProject ||
            !validMilestone ||
            !validStatus ||
            !validLabels) && (
            <p role="alert">{t("issues.resolve_selections")}</p>
          )}
        <fieldset className="space-y-2">
          <legend className="mb-2 text-sm font-medium">
            {t("issues.labels")}
          </legend>
          {[
            ...(selection.data?.labels ?? []).map((label) => ({
              id: label.id,
              name:
                label.name +
                (label.archived ? ` · ${t("issues.inactive")}` : ""),
              disabled:
                label.archived &&
                !(
                  issue?.label_ids?.includes(label.id) &&
                  (!move || !label.team_id || label.team_id === draft.team_id)
                ),
            })),
            ...draft.label_ids
              .filter(
                (labelID) =>
                  !selection.data?.labels.some((label) => label.id === labelID),
              )
              .map((labelID) => ({
                id: labelID,
                name: t("issues.unavailable_reference"),
                disabled: false,
              })),
          ].map((label) => (
            <label
              key={label.id}
              className="flex items-center gap-3 rounded-md border p-3"
            >
              <input
                type="checkbox"
                checked={draft.label_ids.includes(label.id)}
                disabled={label.disabled && !draft.label_ids.includes(label.id)}
                onChange={(event) =>
                  setDraft({
                    ...draft,
                    label_ids: event.target.checked
                      ? [...draft.label_ids, label.id]
                      : draft.label_ids.filter((value) => value !== label.id),
                  })
                }
              />
              <span className="min-w-0 break-words">
                {label.name}
                {label.disabled ? ` · ${t("issues.inactive")}` : ""}
              </span>
            </label>
          ))}
        </fieldset>
        {!move && (
          <>
            <div className="grid gap-4 sm:grid-cols-2">
              {(["start_date", "target_date"] as const).map((field) => (
                <div key={field} className="space-y-2">
                  <Label htmlFor={`${id}-${field}`}>
                    {t(issueFieldKeys[field])}
                  </Label>
                  <DatePicker
                    id={`${id}-${field}`}
                    value={draft[field]}
                    disabled={
                      !canManage ||
                      mutation.locked ||
                      stale ||
                      !references.isSuccess ||
                      references.isError
                    }
                    onChange={(value) => setDraft({ ...draft, [field]: value })}
                  />
                  <Button
                    type="button"
                    variant="ghost"
                    onClick={() => setDraft({ ...draft, [field]: "" })}
                  >
                    {t("issues.clear_date")}
                  </Button>
                </div>
              ))}
            </div>
            {!validIssueDates(draft.start_date, draft.target_date) && (
              <p role="alert">{t("issues.date_order")}</p>
            )}
            <div className="space-y-2">
              <Label htmlFor={`${id}-effort`}>{t("issues.effort")}</Label>
              <TextLimitInput
                id={`${id}-effort`}
                inputMode="numeric"
                value={draft.effort}
                onChange={(event) =>
                  setDraft({ ...draft, effort: event.target.value })
                }
              />
              {!validIssueEffort(draft.effort) && (
                <p role="alert">{t("issues.effort_invalid")}</p>
              )}
            </div>
          </>
        )}
      </fieldset>
      {stale && <p role="alert">{t("issues.stale_draft")}</p>}
      <div className="flex flex-wrap gap-3">
        <Button type="submit" disabled={!ready || !valid || mutation.locked}>
          {t(move ? "issues.move" : issue ? "issues.save" : "issues.create")}
        </Button>
        {issue && (
          <Button
            type="button"
            variant="outline"
            disabled={mutation.locked}
            onClick={onDone}
          >
            {t(stale ? "issues.discard_draft" : "issues.cancel")}
          </Button>
        )}
      </div>
      <MutationNotice
        canRetry={canManage}
        mutation={mutation}
        onReconcile={reset}
      />
    </form>
  );
}
