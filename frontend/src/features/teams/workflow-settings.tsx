"use client";
import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { ArrowDown, ArrowUp } from "lucide-react";
import { Button } from "@/components/ui/button";
import { TextLimitInput as Input } from "@/components/ui/text-limit-input";
import { Label } from "@/components/ui/label";
import { useLocale } from "@/providers/locale-provider";
import { teamKeys, useStatuses } from "./queries";
import {
  teamService,
  statusCategories,
  type WorkflowStatus,
  type StatusCategory,
} from "./team.service";
import {
  SettingsSection,
  QueryNotice,
  MutationNotice,
  useSettingsMutation,
} from "./settings-ui";
const categoryKeys: Record<string, string> = {
  backlog: "teams.category_backlog",
  unstarted: "teams.category_unstarted",
  started: "teams.category_started",
  completed: "teams.category_completed",
  canceled: "teams.category_canceled",
};

export function WorkflowSettings({
  workspaceId,
  teamId,
  canManage,
}: {
  workspaceId: string;
  teamId: string;
  canManage: boolean;
}) {
  const { t } = useLocale();
  const cache = useQueryClient();
  const query = useStatuses(workspaceId, teamId, true);
  const [editing, setEditing] = useState<WorkflowStatus | null>(null);
  const [name, setName] = useState("");
  const [color, setColor] = useState("#64748B");
  const [category, setCategory] = useState<StatusCategory>("backlog");
  const [baseline, setBaseline] = useState<number | null>(null);
  const refresh = () =>
    cache.invalidateQueries({ queryKey: teamKeys.all(workspaceId) });
  type SaveInput = {
    name: string;
    color: string;
    category: StatusCategory;
    expected_workflow_version: number;
    target: WorkflowStatus | null;
  };
  const save = useSettingsMutation({
    run: (input: SaveInput, key) => {
      const body = {
        name: input.name,
        color: input.color,
        category: input.category,
        expected_workflow_version: input.expected_workflow_version,
      };
      return input.target
        ? teamService.updateStatus(
            workspaceId,
            teamId,
            input.target.id,
            { ...body, expected_version: input.target.version },
            key,
          )
        : teamService.createStatus(workspaceId, teamId, body, key);
    },
    onSaved: () => reset(),
    refresh,
    reviewLatest: () => query.refetch({ throwOnError: true }),
  });
  const archive = useSettingsMutation({
    run: (
      input: {
        id: string;
        expected_version: number;
        expected_workflow_version: number;
      },
      key,
    ) =>
      teamService.archiveStatus(
        workspaceId,
        teamId,
        input.id,
        {
          expected_version: input.expected_version,
          expected_workflow_version: input.expected_workflow_version,
        },
        key,
      ),
    refresh,
    reviewLatest: () => query.refetch({ throwOnError: true }),
  });
  const reorder = useSettingsMutation({
    run: (
      input: { status_ids: string[]; expected_workflow_version: number },
      key,
    ) => teamService.reorder(workspaceId, teamId, input, key),
    refresh,
    reviewLatest: () => query.refetch({ throwOnError: true }),
  });
  const reset = () => {
    setEditing(null);
    setName("");
    setColor("#64748B");
    setCategory("backlog");
    setBaseline(null);
  };
  const busy =
    save.isPending ||
    archive.isPending ||
    reorder.isPending ||
    save.conflict ||
    archive.conflict ||
    reorder.conflict;
  const writable =
    canManage &&
    query.isSuccess &&
    !query.isError &&
    query.data.capabilities.manage;
  const items =
    query.isSuccess && !query.isError
      ? [...query.data.items]
          .filter((item) => !item.archived)
          .sort((a, b) => a.position - b.position)
      : [];
  function move(index: number, delta: number) {
    if (!query.data || busy) return;
    const ids = items.map((item) => item.id);
    [ids[index], ids[index + delta]] = [ids[index + delta], ids[index]];
    reorder.submit({
      status_ids: ids,
      expected_workflow_version: query.data.workflow_version,
    });
  }
  return (
    <SettingsSection id="workflow" title={t("teams.workflow")}>
      <p className="text-muted-foreground text-sm">
        {t("teams.workflow_help")}
      </p>
      <QueryNotice
        pending={query.isPending}
        error={query.isError}
        retry={() => query.refetch()}
      />
      {query.isSuccess && !query.isError && (
        <ol className="space-y-3">
          {items.map((status, index) => (
            <li
              key={status.id}
              className="flex flex-wrap items-center gap-3 rounded-lg border p-3"
            >
              <span
                aria-hidden="true"
                className="size-4 shrink-0 rounded-full border"
                style={{ backgroundColor: status.color }}
              />
              <div className="min-w-0 flex-1">
                <p className="break-words">{status.name}</p>
                <p className="text-muted-foreground text-sm">
                  {t(categoryKeys[status.category] ?? "teams.category")}
                </p>
              </div>
              {writable && (
                <div className="flex flex-wrap gap-2">
                  <Button
                    variant="outline"
                    size="icon"
                    aria-label={t("teams.move_up", { name: status.name })}
                    disabled={busy || index === 0}
                    onClick={() => move(index, -1)}
                  >
                    <ArrowUp />
                  </Button>
                  <Button
                    variant="outline"
                    size="icon"
                    aria-label={t("teams.move_down", { name: status.name })}
                    disabled={busy || index === items.length - 1}
                    onClick={() => move(index, 1)}
                  >
                    <ArrowDown />
                  </Button>
                  <Button
                    variant="outline"
                    disabled={busy}
                    onClick={() => {
                      setEditing(status);
                      setName(status.name);
                      setColor(status.color);
                      setCategory(status.category);
                      setBaseline(query.data.workflow_version);
                    }}
                  >
                    {t("teams.edit")}
                  </Button>
                  <Button
                    variant="outline"
                    disabled={busy}
                    onClick={() =>
                      archive.submit({
                        id: status.id,
                        expected_version: status.version,
                        expected_workflow_version: query.data.workflow_version,
                      })
                    }
                  >
                    {t("teams.archive")}
                  </Button>
                </div>
              )}
            </li>
          ))}
        </ol>
      )}
      {writable && (
        <>
          <form
            className="max-w-lg space-y-4 border-t pt-4"
            onSubmit={(e) => {
              e.preventDefault();
              if (query.data)
                save.submit({
                  name: name.trim(),
                  color,
                  category,
                  target: editing,
                  expected_workflow_version:
                    baseline ?? query.data.workflow_version,
                });
            }}
          >
            <h3 className="font-medium">
              {t(editing ? "teams.edit_status" : "teams.create_status")}
            </h3>
            <div className="space-y-2">
              <Label htmlFor="status-name">{t("teams.name")}</Label>
              <Input
                id="status-name"
                required
                maxCodePoints={100}
                value={name}
                disabled={busy}
                onChange={(e) => {
                  setName(e.target.value);
                  setBaseline((value) => value ?? query.data.workflow_version);
                }}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="status-category">{t("teams.category")}</Label>
              <select
                id="status-category"
                className="bg-background h-10 w-full rounded-md border px-3 text-sm"
                value={category}
                disabled={busy}
                onChange={(e) => {
                  setCategory(e.target.value as StatusCategory);
                  setBaseline((value) => value ?? query.data.workflow_version);
                }}
              >
                {statusCategories.map((value) => (
                  <option key={value} value={value}>
                    {t(categoryKeys[value])}
                  </option>
                ))}
              </select>
            </div>
            <div className="space-y-2">
              <Label htmlFor="status-color">{t("teams.color")}</Label>
              <Input
                id="status-color"
                type="color"
                className="h-10 w-24"
                value={color}
                disabled={busy}
                onChange={(e) => {
                  setColor(e.target.value);
                  setBaseline((value) => value ?? query.data.workflow_version);
                }}
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
                {t("teams.new_status")}
              </Button>
            </div>
          </form>
          <MutationNotice mutation={save} onReconcile={reset} />
          <MutationNotice mutation={archive} onReconcile={reset} />
          <MutationNotice mutation={reorder} onReconcile={reset} />
        </>
      )}
    </SettingsSection>
  );
}
