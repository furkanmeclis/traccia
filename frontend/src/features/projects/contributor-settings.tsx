"use client";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { useLocale } from "@/providers/locale-provider";
import type { Team } from "@/features/teams/team.service";
import { useProjectReferences } from "./queries";
import { projectService, type Schemas } from "./project.service";
import {
  Section,
  LoadNotice,
  MutationNotice,
  useProjectMutation,
} from "./project-ui";
export function ContributorSettings({
  workspaceId,
  projectId,
  teams,
  version,
  canManage,
  reviewLatest,
}: {
  workspaceId: string;
  projectId: string;
  teams: Team[];
  version: number;
  canManage: boolean;
  reviewLatest: () => Promise<unknown>;
}) {
  const { t } = useLocale();
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState<string[]>([]);
  const [baseline, setBaseline] = useState(version);
  const references = useProjectReferences(workspaceId, editing && canManage);
  const mutation = useProjectMutation({
    workspaceId,
    projectId,
    operationId: "contributors-set",
    operationLabel: t("projects.edit_teams"),
    reviewLatest,
    run: (input: Schemas["ProductProjectTeamsSetInput"], key) =>
      projectService.setTeams(workspaceId, projectId, input, key),
    onSaved: () => setEditing(false),
  });
  const available = [
    ...(references.data?.teams ?? []),
    ...teams.filter(
      (team) => !references.data?.teams.some((item) => item.id === team.id),
    ),
  ];
  const ready = references.isSuccess && !references.isError && canManage;
  return (
    <Section title={t("projects.teams")}>
      <ul className="space-y-2">
        {teams.map((team) => (
          <li key={team.id} className="break-words">
            {team.name}{" "}
            <span className="text-muted-foreground text-sm">
              {team.code}
              {team.archived && ` · ${t("projects.archived")}`}
            </span>
          </li>
        ))}
      </ul>
      {canManage && !editing && (
        <Button
          type="button"
          variant="outline"
          onClick={() => {
            setDraft(teams.map((team) => team.id));
            setBaseline(version);
            setEditing(true);
          }}
        >
          {t("projects.edit_teams")}
        </Button>
      )}
      {editing && (
        <form
          className="space-y-4"
          onSubmit={(event) => {
            event.preventDefault();
            if (ready && draft.length)
              mutation.submit({
                team_ids: draft,
                expected_project_version: baseline,
              });
          }}
        >
          <LoadNotice
            pending={references.isPending}
            error={references.isError}
            retry={() => references.refetch()}
          />
          <fieldset disabled={!ready || mutation.locked} className="space-y-2">
            <legend className="mb-2 text-sm">{t("projects.teams_help")}</legend>
            {available.map((team) => (
              <label
                key={team.id}
                className="flex items-center gap-3 rounded-md border p-3"
              >
                <Checkbox
                  type="button"
                  checked={draft.includes(team.id)}
                  onCheckedChange={(checked) =>
                    setDraft(
                      checked
                        ? [...draft, team.id]
                        : draft.filter((id) => id !== team.id),
                    )
                  }
                />
                <span className="min-w-0 break-words">
                  {team.name}
                  {team.archived && (
                    <span className="text-muted-foreground">
                      {" "}
                      · {t("projects.retained_archived_team")}
                    </span>
                  )}
                </span>
              </label>
            ))}
          </fieldset>
          <div className="flex flex-wrap gap-3">
            <Button
              type="submit"
              disabled={!ready || !draft.length || mutation.locked}
            >
              {t("projects.save")}
            </Button>
            <Button
              type="button"
              variant="outline"
              disabled={mutation.locked}
              onClick={() => setEditing(false)}
            >
              {t("projects.cancel")}
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
