"use client";
import { useRef, useState, type FormEvent } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useMutation } from "@tanstack/react-query";
import { ArrowRight, Layers3, Plus } from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@/components/ui/dialog";
import { useLocale } from "@/providers/locale-provider";
import { useWorkspaces, workspaceKeys } from "./queries";
import {
  prepareSubmission,
  type OperationSubmission,
} from "./operation-submission";
import { workspaceService } from "./workspace.service";
export function WorkspaceList() {
  const { t } = useLocale();
  const router = useRouter();
  const cache = useQueryClient();
  const list = useWorkspaces();
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const submission = useRef<OperationSubmission<{ name: string }> | null>(null);
  const create = useMutation({
    mutationFn: (operation: OperationSubmission<{ name: string }>) =>
      workspaceService.create(operation.input.name, operation.key),
    onSuccess: async (result) => {
      await cache.invalidateQueries({ queryKey: workspaceKeys.all });
      submission.current = null;
      setOpen(false);
      setName("");
      router.push(`/w/${result.workspace.id}`);
    },
  });
  function submit(e: FormEvent) {
    e.preventDefault();
    if (!canCreate || create.isPending) return;
    submission.current = prepareSubmission(
      submission.current,
      { name: name.trim() },
      () => ({ name: name.trim() }),
    );
    create.mutate(submission.current);
  }
  const items = list.isError
    ? []
    : (list.data?.pages.flatMap((page) => page.items ?? []) ?? []);
  const canCreate =
    list.isSuccess && !list.isError && list.data.pages[0]?.can_create === true;
  return (
    <div className="space-y-8">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <p className="text-primary mb-2 text-sm font-medium">Traccia</p>
          <h1 className="text-3xl font-semibold tracking-tight">
            {t("workspaces.title")}
          </h1>
          <p className="text-muted-foreground mt-2 max-w-xl text-sm">
            {t("workspaces.description")}
          </p>
        </div>
        {canCreate && (
          <Button
            onClick={() => {
              create.reset();
              setOpen(true);
            }}
          >
            <Plus />
            {t("workspaces.create")}
          </Button>
        )}
      </div>
      {list.isPending && (
        <p role="status" className="text-muted-foreground py-12">
          {t("common.loading")}
        </p>
      )}
      {list.isError && (
        <div role="alert" className="space-y-3">
          <p>{t("workspaces.load_error")}</p>
          <Button variant="outline" onClick={() => list.refetch()}>
            {t("workspaces.retry")}
          </Button>
        </div>
      )}
      {!list.isPending && !list.isError && items.length === 0 && (
        <Card>
          <CardContent className="flex flex-col items-center gap-3 py-16 text-center">
            <Layers3 className="text-muted-foreground mb-2 size-10" />
            <h2 className="text-lg font-medium">
              {canCreate
                ? t("workspaces.empty_title")
                : t("workspaces.empty_readonly_title")}
            </h2>
            <p className="text-muted-foreground max-w-md text-sm">
              {canCreate
                ? t("workspaces.empty_description")
                : t("workspaces.empty_readonly_description")}
            </p>
            {canCreate && (
              <Button className="mt-3" onClick={() => setOpen(true)}>
                <Plus />
                {t("workspaces.create")}
              </Button>
            )}
          </CardContent>
        </Card>
      )}
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {items.map((workspace) => (
          <Link
            key={workspace.id}
            href={`/w/${workspace.id}`}
            className="focus-visible:ring-ring rounded-xl outline-none focus-visible:ring-2"
          >
            <Card className="hover:border-primary/50 h-full transition-colors">
              <CardContent className="flex items-center gap-4 p-6">
                <span className="bg-primary/10 text-primary flex size-11 shrink-0 items-center justify-center rounded-lg text-lg font-semibold">
                  {workspace.name.slice(0, 1).toLocaleUpperCase()}
                </span>
                <div className="min-w-0 flex-1">
                  <h2 className="truncate font-medium">{workspace.name}</h2>
                  <p className="text-muted-foreground mt-1 text-xs">
                    {t("workspaces.open")}
                  </p>
                </div>
                <ArrowRight className="text-muted-foreground size-4" />
              </CardContent>
            </Card>
          </Link>
        ))}
      </div>
      {list.hasNextPage && (
        <Button
          variant="outline"
          disabled={list.isFetchingNextPage}
          onClick={() => list.fetchNextPage()}
        >
          {t("workspaces.load_more")}
        </Button>
      )}
      <Dialog
        open={open && canCreate}
        onOpenChange={(value) => {
          if (!create.isPending) {
            setOpen(value);
            if (!value) submission.current = null;
          }
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("workspaces.create")}</DialogTitle>
            <DialogDescription>
              {t("workspaces.create_description")}
            </DialogDescription>
          </DialogHeader>
          <form onSubmit={submit} className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="workspace-name">{t("workspaces.name")}</Label>
              <Input
                id="workspace-name"
                autoFocus
                disabled={create.isPending}
                required
                maxLength={100}
                value={name}
                onChange={(e) => setName(e.target.value)}
              />
            </div>
            {create.isError && (
              <p role="alert" className="text-destructive text-sm">
                {create.error.message}
              </p>
            )}
            <Button
              type="submit"
              disabled={!canCreate || create.isPending || !name.trim()}
              className="w-full"
            >
              {create.isPending
                ? t("workspaces.saving")
                : t("workspaces.create")}
            </Button>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  );
}
