"use client";
import { useRef, useState, type FormEvent } from "react";
import Link from "next/link";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, Settings2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  CardDescription,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ApiError } from "@/lib/api";
import {
  prepareSubmission,
  type OperationSubmission,
} from "./operation-submission";
import { useLocale } from "@/providers/locale-provider";
import { useWorkspace, workspaceKeys } from "./queries";
import { workspaceService, type Workspace } from "./workspace.service";
import { WorkspaceActivity } from "./workspace-activity";
import { WorkspaceCredentials } from "./workspace-credentials";
function WorkspaceSettings({ workspace }: { workspace: Workspace }) {
  const { t } = useLocale();
  const cache = useQueryClient();
  const [name, setName] = useState(workspace.name);
  const [baseline, setBaseline] = useState(workspace);
  const [conflict, setConflict] = useState(false);
  const [latest, setLatest] = useState<Workspace | null>(null);
  const input = { name: name.trim(), expected_version: baseline.version };
  const submission = useRef<OperationSubmission<typeof input> | null>(null);
  const review = useMutation({
    mutationFn: () => workspaceService.get(workspace.id),
    onSuccess: (result) => setLatest(result.workspace),
  });
  const update = useMutation({
    mutationFn: (operation: OperationSubmission<typeof input>) =>
      workspaceService.update(workspace.id, operation.input, operation.key),
    onError: (error) => {
      if (error instanceof ApiError && error.code === "VERSION_CONFLICT")
        setConflict(true);
    },
    onSuccess: async (result) => {
      submission.current = null;
      setBaseline(result.workspace);
      setConflict(false);
      setLatest(null);
      await cache.invalidateQueries({ queryKey: workspaceKeys.all });
    },
  });
  function submit(e: FormEvent) {
    e.preventDefault();
    if (conflict || update.isPending) return;
    submission.current = prepareSubmission(
      submission.current,
      input,
      () => input,
    );
    update.mutate(submission.current);
  }
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Settings2 className="size-4" />
          {t("workspaces.settings")}
        </CardTitle>
        <CardDescription>{t("workspaces.scope_description")}</CardDescription>
      </CardHeader>
      <CardContent>
        <form onSubmit={submit} className="space-y-4">
          <div className="max-w-md space-y-2">
            <Label htmlFor="workspace-settings-name">
              {t("workspaces.name")}
            </Label>
            <Input
              id="workspace-settings-name"
              disabled={update.isPending}
              required
              maxLength={100}
              value={name}
              onChange={(e) => {
                setName(e.target.value);
                if (!conflict) update.reset();
              }}
            />
          </div>
          {conflict && (
            <div
              role="alert"
              className="space-y-3 rounded-lg border p-4 text-sm"
            >
              <p>{t("workspaces.version_conflict")}</p>
              {!latest ? (
                <Button
                  type="button"
                  variant="outline"
                  disabled={review.isPending}
                  onClick={() => review.mutate()}
                >
                  {t("workspaces.review_latest")}
                </Button>
              ) : (
                <>
                  <p>{t("workspaces.latest_name", { name: latest.name })}</p>
                  <p>{t("workspaces.reconcile_description")}</p>
                  <Button
                    type="button"
                    variant="outline"
                    onClick={() => {
                      setBaseline(latest);
                      setConflict(false);
                      setLatest(null);
                      submission.current = null;
                      update.reset();
                    }}
                  >
                    {t("workspaces.keep_draft")}
                  </Button>
                </>
              )}
              {review.isError && (
                <p className="text-destructive">{review.error.message}</p>
              )}
            </div>
          )}
          <div className="flex flex-wrap items-center gap-3">
            <Button
              type="submit"
              disabled={
                conflict ||
                update.isPending ||
                !name.trim() ||
                name.trim() === baseline.name
              }
            >
              {update.isPending ? t("workspaces.saving") : t("workspaces.save")}
            </Button>
            {update.isSuccess && (
              <p role="status" className="text-muted-foreground text-sm">
                {t("workspaces.saved")}
              </p>
            )}
            {update.isError && !conflict && (
              <p role="alert" className="text-destructive text-sm">
                {update.error.message}
              </p>
            )}
          </div>
        </form>
      </CardContent>
    </Card>
  );
}
export function WorkspaceDetail({ workspaceId }: { workspaceId: string }) {
  const { t } = useLocale();
  const result = useWorkspace(workspaceId);
  return (
    <div className="space-y-8">
      <Button variant="ghost" asChild className="-ml-3">
        <Link href="/workspaces">
          <ArrowLeft />
          {t("workspaces.switch")}
        </Link>
      </Button>
      {result.isPending && <p role="status">{t("common.loading")}</p>}
      {result.isError && (
        <div role="alert" className="space-y-3">
          <h1 className="text-xl font-semibold">
            {t("workspaces.unavailable")}
          </h1>
          <p className="text-muted-foreground">
            {t("workspaces.unavailable_description")}
          </p>
          <Button variant="outline" onClick={() => result.refetch()}>
            {t("workspaces.retry")}
          </Button>
        </div>
      )}
      {result.data && !result.isError && (
        <>
          <div>
            <p className="text-primary mb-2 text-sm font-medium">
              {t("workspaces.workspace")}
            </p>
            <h1 className="text-3xl font-semibold tracking-tight break-words">
              {result.data.workspace.name}
            </h1>
            <p className="text-muted-foreground mt-2 font-mono text-xs">
              {result.data.workspace.id}
            </p>
          </div>
          {result.data.capabilities.update && (
            <WorkspaceSettings
              key={`settings:${result.data.workspace.id}`}
              workspace={result.data.workspace}
            />
          )}
          <WorkspaceActivity
            key={`activity:${workspaceId}`}
            workspaceId={workspaceId}
          />
          {(result.data.capabilities.read_credentials ||
            result.data.capabilities.manage_credentials) && (
            <WorkspaceCredentials
              key={`credentials:${workspaceId}`}
              workspaceId={workspaceId}
              canRead={result.data.capabilities.read_credentials}
              canManage={result.data.capabilities.manage_credentials}
            />
          )}
        </>
      )}
    </div>
  );
}
