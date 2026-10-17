"use client";
import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useWorkspace } from "@/features/workspaces/queries";
import { useLocale } from "@/providers/locale-provider";
import { teamKeys, useTeam } from "./queries";
import { teamService, type Team } from "./team.service";
import {
  SettingsPage,
  SettingsSection,
  QueryNotice,
  MutationNotice,
  useSettingsMutation,
} from "./settings-ui";
import { WorkflowSettings } from "./workflow-settings";
import { LabelSettings } from "./label-settings";
function TeamSettings({
  workspaceId,
  team,
  reviewLatest,
}: {
  workspaceId: string;
  team: Team;
  reviewLatest: () => Promise<unknown>;
}) {
  const { t } = useLocale();
  const cache = useQueryClient();
  const [draft, setDraft] = useState({
    name: team.name,
    version: team.version,
  });
  const refresh = () =>
    cache.invalidateQueries({ queryKey: teamKeys.all(workspaceId) });
  const update = useSettingsMutation({
    run: (input: { name: string; expected_version: number }, key) =>
      teamService.update(workspaceId, team.id, input, key).then((result) => {
        setDraft({ name: result.team.name, version: result.team.version });
        return result;
      }),
    refresh,
    reviewLatest,
  });
  const archive = useSettingsMutation({
    run: (input: { expected_version: number }, key) =>
      teamService.archive(workspaceId, team.id, input, key),
    refresh,
    reviewLatest,
  });
  return (
    <SettingsSection title={t("teams.settings")}>
      <form
        className="max-w-lg space-y-4"
        onSubmit={(e) => {
          e.preventDefault();
          update.submit({
            name: draft.name.trim(),
            expected_version: draft.version,
          });
        }}
      >
        <Label htmlFor="edit-team-name">{t("teams.name")}</Label>
        <Input
          id="edit-team-name"
          required
          maxLength={100}
          value={draft.name}
          disabled={update.isPending || archive.isPending}
          onChange={(e) => setDraft({ ...draft, name: e.target.value })}
        />
        <Button
          type="submit"
          disabled={
            update.isPending ||
            archive.isPending ||
            update.conflict ||
            !draft.name.trim() ||
            draft.name.trim() === team.name
          }
        >
          {t("teams.save")}
        </Button>
        <MutationNotice
          mutation={update}
          onReconcile={() =>
            setDraft({ name: team.name, version: team.version })
          }
        />
      </form>
      <div className="space-y-3 border-t pt-4">
        <p className="text-muted-foreground text-sm">
          {t("teams.archive_help")}
        </p>
        <Button
          variant="outline"
          disabled={archive.isPending || update.isPending || archive.conflict}
          onClick={() => archive.submit({ expected_version: team.version })}
        >
          {t("teams.archive_team")}
        </Button>
        <MutationNotice
          mutation={archive}
          onReconcile={() =>
            setDraft({ name: team.name, version: team.version })
          }
        />
      </div>
    </SettingsSection>
  );
}
export function TeamDetail({
  workspaceId,
  teamId,
}: {
  workspaceId: string;
  teamId: string;
}) {
  const { t } = useLocale();
  const workspace = useWorkspace(workspaceId);
  const allowed = workspace.isSuccess && !workspace.isError;
  const query = useTeam(workspaceId, teamId, allowed);
  return (
    <SettingsPage workspaceId={workspaceId} title={t("teams.team")}>
      <QueryNotice
        pending={workspace.isPending}
        error={workspace.isError}
        retry={() => workspace.refetch()}
      />
      {allowed && (
        <>
          <QueryNotice
            pending={query.isPending}
            error={query.isError}
            retry={() => query.refetch()}
          />
          {query.isSuccess && !query.isError && (
            <>
              <div>
                <h2 className="text-2xl font-semibold break-words">
                  {query.data.team.name}
                </h2>
                <p className="text-muted-foreground font-mono">
                  {query.data.team.code}
                </p>
                {query.data.team.archived && <p>{t("teams.archived")}</p>}
              </div>
              {query.data.capabilities.manage && !query.data.team.archived && (
                <TeamSettings
                  reviewLatest={() => query.refetch({ throwOnError: true })}
                  workspaceId={workspaceId}
                  team={query.data.team}
                />
              )}
              <WorkflowSettings
                workspaceId={workspaceId}
                teamId={teamId}
                canManage={
                  query.data.capabilities.manage && !query.data.team.archived
                }
              />
              <LabelSettings
                workspaceId={workspaceId}
                teamId={teamId}
                canManage={
                  query.data.capabilities.manage && !query.data.team.archived
                }
              />
            </>
          )}
        </>
      )}
    </SettingsPage>
  );
}
