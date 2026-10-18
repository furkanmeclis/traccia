"use client";
import { useEffect, useRef, useState, type FormEvent } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Check, Copy, KeyRound, Plus, ShieldCheck } from "lucide-react";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
} from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { TextLimitInput as Input } from "@/components/ui/text-limit-input";
import { Label } from "@/components/ui/label";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@/components/ui/dialog";
import {
  prepareSubmission,
  type OperationSubmission,
} from "./operation-submission";
import { useLocale } from "@/providers/locale-provider";
import { useCredentials, workspaceKeys } from "./queries";
import {
  credentialScopes,
  workspaceService,
  type CreateCredential,
  type CredentialScope,
  type WorkspaceCredential,
} from "./workspace.service";

export function WorkspaceCredentials({
  workspaceId,
  canRead,
  canManage,
}: {
  workspaceId: string;
  canRead: boolean;
  canManage: boolean;
}) {
  const { t, locale } = useLocale();
  const scopeLabels: Record<CredentialScope, string> = {
    "workspace.read": t("workspaces.scope_workspace_read"),
    "workspace.write": t("workspaces.scope_workspace_write"),
    "credentials.read": t("workspaces.scope_credentials_read"),
    "credentials.manage": t("workspaces.scope_credentials_manage"),
  };
  const cache = useQueryClient();
  const list = useCredentials(workspaceId, canRead);
  const canManageNow = canManage && !list.isError;
  const [open, setOpen] = useState(false);
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 30_000);
    return () => window.clearInterval(timer);
  }, []);
  const [name, setName] = useState("");
  const [audience, setAudience] = useState<"api" | "mcp">("api");
  const [days, setDays] = useState(30);
  const [scopes, setScopes] = useState<CredentialScope[]>(["workspace.read"]);
  const [token, setToken] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
  const [revokeTarget, setRevokeTarget] = useState<WorkspaceCredential | null>(
    null,
  );
  const submission = useRef<OperationSubmission<CreateCredential> | null>(null);
  const revokeSubmission = useRef<OperationSubmission<{ id: string }> | null>(
    null,
  );
  const create = useMutation({
    mutationFn: async (operation: OperationSubmission<CreateCredential>) => {
      const result = await workspaceService.createCredential(
        workspaceId,
        operation.input,
        operation.key,
      );
      setToken(result.token);
      return result.credential;
    },
    onSuccess: async () => {
      await cache.invalidateQueries({
        queryKey: workspaceKeys.credentials(workspaceId),
      });
    },
    gcTime: 0,
  });
  const revoke = useMutation({
    mutationFn: (operation: OperationSubmission<{ id: string }>) =>
      workspaceService.revokeCredential(
        workspaceId,
        operation.input.id,
        operation.key,
      ),
    onSuccess: async () => {
      revokeSubmission.current = null;
      setRevokeTarget(null);
      await cache.invalidateQueries({
        queryKey: workspaceKeys.credentials(workspaceId),
      });
      toast.success(t("workspaces.revoked_success"));
    },
  });
  function close() {
    if (create.isPending) return;
    submission.current = null;
    setOpen(false);
    setToken(null);
    setCopied(false);
    create.reset();
    setName("");
    setScopes(["workspace.read"]);
    setAudience("api");
    setDays(30);
  }
  async function copy() {
    if (!token) return;
    try {
      await navigator.clipboard.writeText(token);
      setCopied(true);
    } catch {
      toast.error(t("workspaces.copy_error"));
    }
  }
  function submit(e: FormEvent) {
    e.preventDefault();
    if (!canManageNow || create.isPending) return;
    const intent = { name: name.trim(), audience, scopes, days };
    submission.current = prepareSubmission(submission.current, intent, () => ({
      name: intent.name,
      audience,
      scopes,
      expires_at: new Date(Date.now() + days * 86400000).toISOString(),
    }));
    create.mutate(submission.current);
  }
  const items =
    !canRead || list.isError
      ? []
      : (list.data?.pages.flatMap((page) => page.items ?? []) ?? []);
  const formatDate = (value: string) =>
    new Intl.DateTimeFormat(locale, {
      dateStyle: "medium",
      timeStyle: "short",
    }).format(new Date(value));
  return (
    <Card>
      <CardHeader className="flex flex-wrap items-start justify-between gap-4 sm:flex-row">
        <div className="space-y-2">
          <CardTitle className="flex items-center gap-2">
            <KeyRound className="size-4" />
            {t("workspaces.credentials")}
          </CardTitle>
          <CardDescription className="max-w-xl">
            {t("workspaces.credentials_description")}
          </CardDescription>
        </div>
        {canManageNow && (
          <Button variant="outline" onClick={() => setOpen(true)}>
            <Plus />
            {t("workspaces.credential_create")}
          </Button>
        )}
      </CardHeader>
      <CardContent className="space-y-4">
        {canRead && list.isPending && (
          <p role="status" className="text-muted-foreground text-sm">
            {t("common.loading")}
          </p>
        )}
        {canRead && list.isError && (
          <div role="alert" className="space-y-2">
            <p>{t("workspaces.credentials_load_error")}</p>
            <Button variant="outline" onClick={() => list.refetch()}>
              {t("workspaces.retry")}
            </Button>
          </div>
        )}
        {canRead && !list.isPending && !list.isError && items.length === 0 && (
          <div className="rounded-lg border border-dashed px-6 py-10 text-center">
            <ShieldCheck className="text-muted-foreground mx-auto mb-3 size-7" />
            <p className="font-medium">{t("workspaces.credentials_empty")}</p>
            <p className="text-muted-foreground mt-2 text-sm">
              {t("workspaces.credentials_empty_description")}
            </p>
          </div>
        )}
        <div className="divide-y">
          {items.map((item) => {
            const expired = new Date(item.expires_at).getTime() <= now;
            return (
              <div
                key={item.id}
                className="flex flex-col gap-3 py-5 first:pt-0 sm:flex-row sm:items-start"
              >
                <div className="min-w-0 flex-1 space-y-2">
                  <div className="flex flex-wrap items-center gap-2">
                    <h3 className="font-medium break-words">{item.name}</h3>
                    <Badge variant="secondary">
                      {item.audience.toUpperCase()}
                    </Badge>
                    <Badge
                      variant={
                        item.revoked_at || expired ? "outline" : "secondary"
                      }
                    >
                      {item.revoked_at
                        ? t("workspaces.revoked")
                        : expired
                          ? t("workspaces.expired")
                          : t("workspaces.active")}
                    </Badge>
                  </div>
                  <div className="flex flex-wrap gap-1">
                    {(item.scopes ?? []).map((scope) => (
                      <span
                        key={scope}
                        className="bg-muted rounded px-2 py-1 font-mono text-xs"
                      >
                        {scope}
                      </span>
                    ))}
                  </div>
                  <p className="text-muted-foreground text-xs">
                    {t("workspaces.expires", {
                      date: formatDate(item.expires_at),
                    })}
                  </p>
                </div>
                {canManageNow && !item.revoked_at && (
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => {
                      revokeSubmission.current = null;
                      revoke.reset();
                      setRevokeTarget(item);
                    }}
                  >
                    {t("workspaces.revoke")}
                  </Button>
                )}
              </div>
            );
          })}
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
          open={open && (Boolean(token) || canManageNow)}
          onOpenChange={(value) => {
            if (!value) close();
          }}
        >
          <DialogContent className="max-h-[90svh] overflow-y-auto">
            <DialogHeader>
              <DialogTitle>
                {token
                  ? t("workspaces.token_title")
                  : t("workspaces.credential_create")}
              </DialogTitle>
              <DialogDescription>
                {token
                  ? t("workspaces.token_description")
                  : t("workspaces.credential_description")}
              </DialogDescription>
            </DialogHeader>
            {token ? (
              <div className="space-y-4">
                <div className="bg-muted rounded-lg border p-4">
                  <p className="mb-2 text-xs font-medium">
                    {t("workspaces.token_label")}
                  </p>
                  <code className="block text-sm break-all select-all">
                    {token}
                  </code>
                </div>
                <Button variant="outline" className="w-full" onClick={copy}>
                  {copied ? <Check /> : <Copy />}
                  {copied ? t("workspaces.copied") : t("workspaces.copy")}
                </Button>
                <Button className="w-full" onClick={close}>
                  {t("workspaces.token_done")}
                </Button>
              </div>
            ) : (
              <form onSubmit={submit} className="space-y-5">
                <fieldset disabled={create.isPending} className="space-y-5">
                  <div className="space-y-2">
                    <Label htmlFor="credential-name">
                      {t("workspaces.credential_name")}
                    </Label>
                    <Input
                      id="credential-name"
                      autoFocus
                      required
                      maxCodePoints={100}
                      autoComplete="off"
                      value={name}
                      onChange={(e) => setName(e.target.value)}
                    />
                  </div>
                  <div className="grid grid-cols-2 gap-4">
                    <div className="space-y-2">
                      <Label htmlFor="credential-audience">
                        {t("workspaces.audience")}
                      </Label>
                      <select
                        id="credential-audience"
                        className="border-input bg-background h-9 w-full rounded-md border px-3 text-sm"
                        value={audience}
                        onChange={(e) =>
                          setAudience(e.target.value as "api" | "mcp")
                        }
                      >
                        <option value="api">API</option>
                        <option value="mcp">MCP</option>
                      </select>
                    </div>
                    <div className="space-y-2">
                      <Label htmlFor="credential-lifetime">
                        {t("workspaces.lifetime")}
                      </Label>
                      <select
                        id="credential-lifetime"
                        className="border-input bg-background h-9 w-full rounded-md border px-3 text-sm"
                        value={days}
                        onChange={(e) => setDays(Number(e.target.value))}
                      >
                        {[1, 7, 30, 90, 365].map((day) => (
                          <option key={day} value={day}>
                            {t("workspaces.days", { count: day })}
                          </option>
                        ))}
                      </select>
                    </div>
                  </div>
                  <fieldset className="space-y-3">
                    <legend className="mb-3 text-sm font-medium">
                      {t("workspaces.scopes")}
                    </legend>
                    {credentialScopes.map((scope) => (
                      <label
                        key={scope}
                        className="flex cursor-pointer items-start gap-3"
                      >
                        <Checkbox
                          checked={scopes.includes(scope)}
                          onCheckedChange={(checked) =>
                            setScopes((current) =>
                              checked
                                ? [...current, scope]
                                : current.filter((value) => value !== scope),
                            )
                          }
                        />
                        <span className="space-y-1 text-sm">
                          <span className="block font-medium">
                            {scopeLabels[scope]}
                          </span>
                          <span className="text-muted-foreground block font-mono text-xs">
                            {scope}
                          </span>
                        </span>
                      </label>
                    ))}
                  </fieldset>
                  {create.isError && (
                    <p role="alert" className="text-destructive text-sm">
                      {create.error.message}
                    </p>
                  )}
                  <Button
                    type="submit"
                    className="w-full"
                    disabled={
                      !canManageNow ||
                      create.isPending ||
                      !name.trim() ||
                      !scopes.length
                    }
                  >
                    {create.isPending
                      ? t("workspaces.saving")
                      : t("workspaces.credential_create")}
                  </Button>
                </fieldset>
              </form>
            )}
          </DialogContent>
        </Dialog>
        <Dialog
          open={Boolean(revokeTarget) && canManageNow}
          onOpenChange={(value) => {
            if (!value && !revoke.isPending) setRevokeTarget(null);
          }}
        >
          <DialogContent>
            <DialogHeader>
              <DialogTitle>{t("workspaces.revoke_title")}</DialogTitle>
              <DialogDescription>
                {t("workspaces.revoke_description", {
                  name: revokeTarget?.name ?? "",
                })}
              </DialogDescription>
            </DialogHeader>
            {revoke.isError && (
              <p role="alert" className="text-destructive text-sm">
                {revoke.error.message}
              </p>
            )}
            <div className="flex flex-wrap justify-end gap-2">
              <Button
                variant="outline"
                disabled={revoke.isPending}
                onClick={() => setRevokeTarget(null)}
              >
                {t("workspaces.cancel")}
              </Button>
              <Button
                variant="destructive"
                disabled={revoke.isPending}
                onClick={() => {
                  if (!canManageNow || !revokeTarget || revoke.isPending)
                    return;
                  const input = { id: revokeTarget.id };
                  revokeSubmission.current = prepareSubmission(
                    revokeSubmission.current,
                    input,
                    () => input,
                  );
                  revoke.mutate(revokeSubmission.current);
                }}
              >
                {t("workspaces.revoke")}
              </Button>
            </div>
          </DialogContent>
        </Dialog>
      </CardContent>
    </Card>
  );
}
