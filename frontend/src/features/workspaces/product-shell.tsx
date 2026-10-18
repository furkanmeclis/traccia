"use client";
import { useEffect, type ReactNode } from "react";
import {
  IssueDrawerProvider,
  QuickIssueButton,
} from "@/features/issue-drawer/issue-drawer";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { Bell, Layers3 } from "lucide-react";
import { ImpersonationBanner } from "@/features/auth/components/impersonation-banner";
import { RouteGuard } from "@/components/common/route-guard";
import { LocaleSwitch } from "@/components/layout/locale-switch";
import { ThemeSwitch } from "@/components/layout/theme-switch";
import { UserMenu } from "@/components/layout/user-menu";
import { Button } from "@/components/ui/button";
import {
  Sidebar,
  SidebarProvider,
  SidebarInset,
  SidebarHeader,
  SidebarTrigger,
} from "@/components/ui/sidebar";
import { NavEngine } from "@/features/nav-engine";
import { useLocale } from "@/providers/locale-provider";
import { usePermission } from "@/providers/permission-provider";
import { permissions } from "@/config/permissions";
import { routes } from "@/config/routes";
import { navigationFocus } from "@/features/personalwork/navigation-focus";
import { productScope } from "@/features/personalwork/scope";
import { productCatalog } from "@/features/personalwork/navigation";
import { PersonalContextBoundary } from "@/features/personalwork/context-boundary";
import { useProductSwitchers } from "@/features/personalwork/switchers";
import { CurrentTargetFavorite } from "@/features/personalwork/favorites-panel";
import { ProductCommandMenu } from "@/features/personalwork/command-menu";
function ShellContent({ children }: { children: ReactNode }) {
  const { t } = useLocale(),
    { can } = usePermission(),
    pathname = usePathname(),
    router = useRouter(),
    scope = productScope(pathname),
    switchers = useProductSwitchers(scope.workspaceID);
  useEffect(() => {
    navigationFocus.consume(pathname, document);
  }, [pathname]);
  const catalog = productCatalog(
      switchers.actorID,
      scope.workspaceID,
      switchers.resourcesReadable,
    ),
    resources = switchers.resourcesReadable ? switchers.resources.data : null;
  return (
    <SidebarProvider>
      <a
        href="#product-main"
        className="bg-background focus:ring-ring sr-only fixed top-2 left-2 z-50 rounded p-3 focus:not-sr-only focus:ring-2"
      >
        {t("personalwork.skipContent")}
      </a>
      <Sidebar
        mobileTitle={t("personalwork.navigation")}
        mobileDescription={t("personalwork.navigationHelp")}
      >
        <SidebarHeader>
          <Link
            href="/home"
            className="flex items-center gap-2 px-2 py-3 text-lg font-semibold"
          >
            <Layers3 className="size-5" />
            Traccia
          </Link>
        </SidebarHeader>
        <NavEngine catalog={catalog} homeHref="/home" />
      </Sidebar>
      <SidebarInset className="min-w-0">
        <header className="bg-background/95 sticky top-0 z-30 border-b backdrop-blur">
          <div className="flex min-h-16 flex-wrap items-center gap-2 px-3 py-2 sm:px-6">
            <SidebarTrigger
              type="button"
              aria-label={t("personalwork.toggleNavigation")}
            />
            <label className="min-w-0 flex-1 sm:max-w-64">
              <span className="sr-only">
                {t("personalwork.switchWorkspace")}
              </span>
              <select
                className="bg-background w-full min-w-0 rounded border p-2 text-sm"
                value={
                  switchers.workspacesReadable ? (scope.workspaceID ?? "") : ""
                }
                disabled={!switchers.workspacesReadable}
                onChange={(event) => {
                  if (event.target.value)
                    router.push(`/w/${event.target.value}/home`);
                }}
              >
                <option value="">
                  {t(
                    switchers.workspaces.isError
                      ? "personalwork.readError"
                      : "personalwork.chooseWorkspace",
                  )}
                </option>
                {switchers.workspacesReadable &&
                  switchers.workspaces.data?.map((workspace) => (
                    <option key={workspace.id} value={workspace.id}>
                      {workspace.name}
                    </option>
                  ))}
              </select>
            </label>
            <div className="ml-auto flex items-center gap-1">
              <QuickIssueButton />
              <ProductCommandMenu
                key={JSON.stringify([switchers.actorID, scope.workspaceID])}
                catalog={catalog}
                switchers={switchers}
                workspaceID={scope.workspaceID}
              />
              {can(permissions.notifications.platformRead) && (
                <Button type="button" asChild variant="ghost" size="icon">
                  <Link
                    href={routes.platform.notifications.root}
                    aria-label={t("personalwork.notifications")}
                    title={t("personalwork.notifications")}
                  >
                    <Bell className="size-4" />
                  </Link>
                </Button>
              )}
              <LocaleSwitch />
              <ThemeSwitch />
              <UserMenu />
            </div>
          </div>
          {scope.workspaceID && (
            <div className="flex flex-wrap gap-2 border-t px-3 py-2 sm:px-6">
              <label className="min-w-0 flex-1 sm:max-w-64">
                <span className="sr-only">{t("personalwork.switchTeam")}</span>
                <select
                  className="bg-background w-full rounded border p-2 text-sm"
                  disabled={!resources}
                  value={resources ? (scope.teamID ?? "") : ""}
                  onChange={(event) => {
                    if (event.target.value)
                      router.push(
                        `/w/${scope.workspaceID}/teams/${event.target.value}`,
                      );
                  }}
                >
                  <option value="">{t("personalwork.chooseTeam")}</option>
                  {resources?.teams.map((team) => (
                    <option key={team.id} value={team.id}>
                      {team.code} · {team.name}
                    </option>
                  ))}
                </select>
              </label>
              <label className="min-w-0 flex-1 sm:max-w-64">
                <span className="sr-only">
                  {t("personalwork.switchProject")}
                </span>
                <select
                  className="bg-background w-full rounded border p-2 text-sm"
                  disabled={!resources}
                  value={resources ? (scope.projectID ?? "") : ""}
                  onChange={(event) => {
                    if (event.target.value)
                      router.push(
                        `/w/${scope.workspaceID}/projects/${event.target.value}`,
                      );
                  }}
                >
                  <option value="">{t("personalwork.chooseProject")}</option>
                  {resources?.projects.map((project) => (
                    <option key={project.id} value={project.id}>
                      {project.name}
                    </option>
                  ))}
                </select>
              </label>
              {switchers.resources.isError && (
                <p role="alert" className="text-sm">
                  {t("personalwork.readError")}
                </p>
              )}
            </div>
          )}
          {scope.workspaceID && (
            <div className="border-t px-3 py-2 sm:px-6">
              <CurrentTargetFavorite scope={scope} />
            </div>
          )}
        </header>
        <div className="px-4 sm:px-8">
          <ImpersonationBanner />
        </div>
        <main
          id="product-main"
          tabIndex={-1}
          className="mx-auto w-full max-w-6xl min-w-0 px-4 py-8 outline-none sm:px-8 sm:py-10"
        >
          {children}
        </main>
      </SidebarInset>
    </SidebarProvider>
  );
}
export function ProductShell({ children }: { children: ReactNode }) {
  return (
    <RouteGuard mode="product">
      <PersonalContextBoundary />
      <IssueDrawerProvider>
        <ShellContent>{children}</ShellContent>
      </IssueDrawerProvider>
    </RouteGuard>
  );
}
