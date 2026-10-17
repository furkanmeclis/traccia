"use client";
import { useState } from "react";
import Link from "next/link";
import { useQueryClient } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useLocale } from "@/providers/locale-provider";
import { useWorkspace } from "@/features/workspaces/queries";
import { useTeams, teamKeys } from "./queries";
import { teamService } from "./team.service";
import {
  SettingsPage,
  SettingsSection,
  QueryNotice,
  MutationNotice,
  useSettingsMutation,
} from "./settings-ui";
export function TeamList({ workspaceId }: { workspaceId: string }) {
  const { t } = useLocale();
  const workspace = useWorkspace(workspaceId);
  const allowed = workspace.isSuccess && !workspace.isError;
  const [archived, setArchived] = useState(false);
  const list = useTeams(workspaceId, archived, allowed);
  const cache = useQueryClient();
  const [name, setName] = useState("");
  const [code, setCode] = useState("");
  const create = useSettingsMutation({
    reviewLatest: () => list.refetch({ throwOnError: true }),
    onSaved: () => {
      setName("");
      setCode("");
    },
    run: (input: { name: string; code: string }, key) =>
      teamService.create(workspaceId, input, key),
    refresh: () =>
      cache.invalidateQueries({ queryKey: teamKeys.all(workspaceId) }),
  });
  const canManage =
    allowed &&
    list.isSuccess &&
    !list.isError &&
    list.data.pages.every((page) => page.capabilities.manage);
  return (
    <SettingsPage workspaceId={workspaceId} title={t("teams.title")}>
      <QueryNotice
        pending={workspace.isPending}
        error={workspace.isError}
        retry={() => workspace.refetch()}
      />
      {allowed && (
        <>
          <div className="flex flex-wrap gap-3">
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
            <Button variant="outline" asChild>
              <Link href={`/w/${workspaceId}/labels`}>
                {t("teams.shared_labels")}
              </Link>
            </Button>
          </div>
          <QueryNotice
            pending={list.isPending}
            error={list.isError}
            retry={() => list.refetch()}
          />
          {list.isSuccess && !list.isError && (
            <>
              <div className="grid gap-3 sm:grid-cols-2">
                {list.data.pages
                  .flatMap((page) => page.items ?? [])
                  .map((team) => (
                    <Link
                      key={team.id}
                      href={`/w/${workspaceId}/teams/${team.id}`}
                      className="hover:border-primary focus-visible:ring-ring rounded-lg border p-5 focus-visible:ring-2"
                    >
                      <span className="text-muted-foreground font-mono text-sm">
                        {team.code}
                      </span>
                      <h2 className="mt-2 font-medium break-words">
                        {team.name}
                      </h2>
                      {team.archived && (
                        <p className="text-muted-foreground text-sm">
                          {t("teams.archived")}
                        </p>
                      )}
                    </Link>
                  ))}
              </div>
              {list.data.pages[0]?.items?.length === 0 && (
                <p>{t("teams.empty")}</p>
              )}
              {list.hasNextPage && (
                <Button
                  disabled={list.isFetchingNextPage}
                  variant="outline"
                  onClick={() => list.fetchNextPage()}
                >
                  {t("teams.load_more")}
                </Button>
              )}
            </>
          )}
          {canManage && (
            <SettingsSection title={t("teams.create")}>
              <form
                className="max-w-lg space-y-4"
                onSubmit={(event) => {
                  event.preventDefault();
                  if (
                    canManage &&
                    name.trim() &&
                    /^[A-Z][A-Z0-9]{1,9}$/.test(code)
                  )
                    create.submit({ name: name.trim(), code });
                }}
              >
                <div className="space-y-2">
                  <Label htmlFor="team-name">{t("teams.name")}</Label>
                  <Input
                    id="team-name"
                    value={name}
                    onChange={(event) => setName(event.target.value)}
                    required
                    maxLength={100}
                    disabled={create.isPending}
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="team-code">{t("teams.code")}</Label>
                  <Input
                    id="team-code"
                    value={code}
                    onChange={(event) =>
                      setCode(event.target.value.toUpperCase())
                    }
                    required
                    minLength={2}
                    maxLength={10}
                    pattern="[A-Z][A-Z0-9]{1,9}"
                    aria-describedby="team-code-help"
                    disabled={create.isPending}
                  />
                  <p
                    id="team-code-help"
                    className="text-muted-foreground text-sm"
                  >
                    {t("teams.code_help")}
                  </p>
                </div>
                <Button
                  type="submit"
                  disabled={
                    create.isPending ||
                    create.conflict ||
                    !name.trim() ||
                    !/^[A-Z][A-Z0-9]{1,9}$/.test(code)
                  }
                >
                  {t("teams.create")}
                </Button>
                <MutationNotice
                  mutation={create}
                  onReconcile={() => {
                    setName("");
                    setCode("");
                  }}
                />
              </form>
            </SettingsSection>
          )}
        </>
      )}
    </SettingsPage>
  );
}
