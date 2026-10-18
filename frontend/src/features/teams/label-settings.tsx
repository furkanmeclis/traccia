"use client";
import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { TextLimitInput as Input } from "@/components/ui/text-limit-input";
import { Label } from "@/components/ui/label";
import { useLocale } from "@/providers/locale-provider";
import { useWorkspace } from "@/features/workspaces/queries";
import { useLabels, teamKeys } from "./queries";
import { teamService, type TeamLabel } from "./team.service";
import {
  SettingsPage,
  SettingsSection,
  QueryNotice,
  MutationNotice,
  useSettingsMutation,
} from "./settings-ui";
export function LabelSettings({
  workspaceId,
  teamId = "",
  canManage,
}: {
  workspaceId: string;
  teamId?: string;
  canManage: boolean;
}) {
  const { t } = useLocale();
  const cache = useQueryClient();
  const [archived, setArchived] = useState(false);
  const query = useLabels(workspaceId, teamId, archived, true);
  const [editing, setEditing] = useState<TeamLabel | null>(null);
  const [name, setName] = useState("");
  const [color, setColor] = useState("#64748B");
  const refresh = () =>
    cache.invalidateQueries({ queryKey: teamKeys.all(workspaceId) });
  type LabelInput = { name: string; color: string; target: TeamLabel | null };
  const save = useSettingsMutation({
    run: (input: LabelInput, key) =>
      input.target
        ? teamService.updateLabel(
            workspaceId,
            input.target.id,
            {
              name: input.name,
              color: input.color,
              expected_version: input.target.version,
            },
            key,
          )
        : teamService.createLabel(
            workspaceId,
            { name: input.name, color: input.color, team_id: teamId },
            key,
          ),
    onSaved: () => reset(),
    refresh,
    reviewLatest: () => query.refetch({ throwOnError: true }),
  });
  const archive = useSettingsMutation({
    run: (input: { id: string; expected_version: number }, key) =>
      teamService.archiveLabel(
        workspaceId,
        input.id,
        { expected_version: input.expected_version },
        key,
      ),
    refresh,
    reviewLatest: () => query.refetch({ throwOnError: true }),
  });
  const writable =
    canManage &&
    query.isSuccess &&
    !query.isError &&
    query.data.pages.every((page) => page.capabilities.manage);
  const reset = () => {
    setEditing(null);
    setName("");
    setColor("#64748B");
  };
  const busy =
    save.isPending || archive.isPending || save.conflict || archive.conflict;
  return (
    <SettingsSection
      id={teamId ? "team-labels" : "shared-labels"}
      title={t(teamId ? "teams.team_labels" : "teams.shared_labels")}
    >
      <div className="flex gap-3">
        <Button
          variant={archived ? "outline" : "default"}
          onClick={() => setArchived(false)}
        >
          {t("teams.active")}
        </Button>
        <Button
          variant={archived ? "default" : "outline"}
          onClick={() => setArchived(true)}
        >
          {t("teams.archived")}
        </Button>
      </div>
      <QueryNotice
        pending={query.isPending}
        error={query.isError}
        retry={() => query.refetch()}
      />
      {query.isSuccess && !query.isError && (
        <>
          <ul className="space-y-3">
            {query.data.pages
              .flatMap((page) => page.items ?? [])
              .map((label) => (
                <li
                  key={label.id}
                  className="flex flex-wrap items-center gap-3 rounded-lg border p-3"
                >
                  <span
                    aria-hidden="true"
                    className="size-4 shrink-0 rounded-full border"
                    style={{ backgroundColor: label.color }}
                  />
                  <span className="min-w-0 flex-1 break-words">
                    {label.name}
                  </span>
                  {writable && !label.archived && (
                    <>
                      <Button
                        variant="outline"
                        disabled={busy}
                        onClick={() => {
                          setEditing(label);
                          setName(label.name);
                          setColor(label.color);
                        }}
                      >
                        {t("teams.edit")}
                      </Button>
                      <Button
                        variant="outline"
                        disabled={busy}
                        onClick={() =>
                          archive.submit({
                            id: label.id,
                            expected_version: label.version,
                          })
                        }
                      >
                        {t("teams.archive")}
                      </Button>
                    </>
                  )}
                </li>
              ))}
          </ul>
          {query.data.pages[0]?.items?.length === 0 && (
            <p>{t("teams.no_labels")}</p>
          )}
          {query.hasNextPage && (
            <Button
              variant="outline"
              disabled={query.isFetchingNextPage}
              onClick={() => query.fetchNextPage()}
            >
              {t("teams.load_more")}
            </Button>
          )}
        </>
      )}
      {writable && (
        <>
          <form
            className="max-w-lg space-y-4 border-t pt-4"
            onSubmit={(event) => {
              event.preventDefault();
              save.submit({ name: name.trim(), color, target: editing });
            }}
          >
            <h3 className="font-medium">
              {t(editing ? "teams.edit_label" : "teams.create_label")}
            </h3>
            <div className="space-y-2">
              <Label htmlFor={`${teamId || "shared"}-label-name`}>
                {t("teams.name")}
              </Label>
              <Input
                id={`${teamId || "shared"}-label-name`}
                required
                maxCodePoints={100}
                value={name}
                disabled={busy}
                onChange={(e) => setName(e.target.value)}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor={`${teamId || "shared"}-label-color`}>
                {t("teams.color")}
              </Label>
              <Input
                id={`${teamId || "shared"}-label-color`}
                type="color"
                className="h-10 w-24"
                value={color}
                disabled={busy}
                onChange={(e) => setColor(e.target.value)}
              />
            </div>
            <div className="flex gap-3">
              <Button type="submit" disabled={busy || !name.trim()}>
                {t("teams.save")}
              </Button>
              <Button
                type="button"
                variant="outline"
                disabled={busy}
                onClick={reset}
              >
                {t("teams.new_label")}
              </Button>
            </div>
          </form>
          <MutationNotice mutation={save} onReconcile={reset} />
          <MutationNotice mutation={archive} onReconcile={reset} />
        </>
      )}
    </SettingsSection>
  );
}
export function SharedLabels({ workspaceId }: { workspaceId: string }) {
  const { t } = useLocale();
  const workspace = useWorkspace(workspaceId);
  return (
    <SettingsPage workspaceId={workspaceId} title={t("teams.shared_labels")}>
      <QueryNotice
        pending={workspace.isPending}
        error={workspace.isError}
        retry={() => workspace.refetch()}
      />
      {workspace.isSuccess && !workspace.isError && (
        <LabelSettings
          workspaceId={workspaceId}
          canManage={workspace.data.capabilities.update}
        />
      )}
    </SettingsPage>
  );
}
