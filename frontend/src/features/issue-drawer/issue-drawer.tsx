"use client";
import {
  createContext,
  useContext,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { usePathname } from "next/navigation";
import Link from "next/link";
import {
  Dialog,
  DialogContent,
  DialogTitle,
  DialogDescription,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { useAuth } from "@/providers/auth-provider";
import { useLocale } from "@/providers/locale-provider";
import { productScope } from "@/features/personalwork/scope";
import { QuickIssueDialog } from "@/features/issue-quick-create/quick-issue-dialog";
import { useWorkspace } from "@/features/workspaces/queries";
import { IssueDetail } from "@/features/issues/issue-detail";
import { DrawerDraftContext, useDrawerDraft } from "./draft-context";
import { drawerScope } from "./drafts";
import { adjacentIssue, editingTarget, visibleFocusTarget } from "./navigation";
type DrawerAPI = {
  openIssue: (id: string, opener?: HTMLElement) => void;
  setRows: (ids: string[]) => void;
  openCreate: (
    opener?: HTMLElement,
    teamID?: string,
    projectID?: string,
  ) => void;
  canCreate: boolean;
  visited: { id: string }[];
  selected: string | null;
};
const DrawerContext = createContext<DrawerAPI | null>(null);
export function useIssueDrawer() {
  return useContext(DrawerContext);
}
function ScopedDrawer({
  children,
  actorID,
  workspaceID,
}: {
  children: ReactNode;
  actorID: string;
  workspaceID: string;
}) {
  const { t } = useLocale(),
    pathname = usePathname();
  const [visited, setVisited] = useDrawerDraft<{ id: string }[]>(
    "visited",
    [],
    drawerScope(actorID, workspaceID, "drawer-navigation"),
  );
  const [selected, setSelected] = useState<string | null>(null),
    [open, setOpen] = useState(false),
    [rows, setRows] = useState<string[]>([]);
  const workspace = useWorkspace(workspaceID);
  const canCreate =
    workspace.isSuccess &&
    !workspace.isError &&
    !workspace.isFetching &&
    !!workspace.data.capabilities.update;
  const [quick, setQuick] = useState<{
    open: boolean;
    teamID?: string;
    projectID?: string;
  }>({ open: false });
  const quickOpener = useRef<HTMLElement | null>(null);
  function openCreate(
    opener?: HTMLElement,
    teamID?: string,
    projectID?: string,
  ) {
    if (!canCreate) return;
    quickOpener.current = opener ?? null;
    setQuick({ open: true, teamID, projectID });
  }
  useEffect(() => {
    function key(event: KeyboardEvent) {
      if (
        event.defaultPrevented ||
        event.isComposing ||
        event.repeat ||
        editingTarget(event.target) ||
        event.ctrlKey ||
        event.metaKey ||
        event.altKey ||
        event.shiftKey ||
        document.querySelector('[role="dialog"]')
      )
        return;
      if (event.key === "c" && canCreate) {
        event.preventDefault();
        openCreate(
          document.activeElement instanceof HTMLElement
            ? document.activeElement
            : undefined,
        );
      }
    }
    window.addEventListener("keydown", key);
    return () => window.removeEventListener("keydown", key);
  });
  const origin = useRef<HTMLElement | null>(null),
    originID = useRef<string | null>(null),
    openedPath = useRef(pathname),
    content = useRef<HTMLDivElement>(null);
  const lifecycle = useRef({
    generation: 0,
    closing: -1,
    open: false,
    alive: true,
  });
  useEffect(() => {
    const current = lifecycle.current;
    current.alive = true;
    return () => {
      current.alive = false;
      current.generation++;
    };
  }, []);
  function changeOpen(value: boolean) {
    lifecycle.current.open = value;
    if (!value) lifecycle.current.closing = lifecycle.current.generation;
    setOpen(value);
  }
  function openIssue(id: string, opener?: HTMLElement) {
    lifecycle.current.generation++;
    lifecycle.current.open = true;
    if (opener) {
      origin.current = opener;
      originID.current = id;
    }
    openedPath.current = pathname;
    setVisited((prior) =>
      prior.some((item) => item.id === id) ? prior : [...prior, { id }],
    );
    setSelected(id);
    setOpen(true);
  }
  const previous = selected ? adjacentIssue(rows, selected, -1) : null,
    next = selected ? adjacentIssue(rows, selected, 1) : null;
  function step(id: string | null) {
    if (id) {
      setVisited((prior) =>
        prior.some((item) => item.id === id) ? prior : [...prior, { id }],
      );
      setSelected(id);
    }
  }
  return (
    <DrawerContext.Provider
      value={{ openIssue, setRows, openCreate, canCreate, visited, selected }}
    >
      {children}
      <QuickIssueDialog
        actorID={actorID}
        workspaceID={workspaceID}
        initialTeamID={quick.teamID}
        initialProjectID={quick.projectID}
        open={quick.open}
        onOpenChange={(value) =>
          setQuick((prior) => ({ ...prior, open: value }))
        }
        canCreate={canCreate}
        reviewLatest={() => workspace.refetch({ throwOnError: true })}
        returnFocus={() => {
          if (lifecycle.current.open) return null;
          return visibleFocusTarget(quickOpener.current)
            ? quickOpener.current
            : document.getElementById("product-main");
        }}
        onCreated={(created) => {
          if (
            created.actorID.toLowerCase() !== actorID.toLowerCase() ||
            created.workspaceID.toLowerCase() !== workspaceID.toLowerCase() ||
            !lifecycle.current.alive
          )
            return;
          setQuick((prior) => ({ ...prior, open: false }));
          openIssue(
            created.issueID,
            visibleFocusTarget(quickOpener.current)
              ? quickOpener.current
              : undefined,
          );
        }}
      />
      <Dialog open={open} onOpenChange={changeOpen}>
        <DialogContent
          ref={content}
          closeLabel={t("drawer.close")}
          className="inset-y-0 right-0 left-auto flex h-dvh w-full max-w-4xl translate-x-0 translate-y-0 flex-col rounded-none p-0 sm:rounded-none"
          onCloseAutoFocus={(event) => {
            event.preventDefault();
            if (
              !lifecycle.current.alive ||
              lifecycle.current.open ||
              lifecycle.current.closing !== lifecycle.current.generation
            )
              return;
            if (openedPath.current !== window.location.pathname) return;
            const active = document.activeElement;
            if (
              active instanceof HTMLElement &&
              active !== document.body &&
              !content.current?.contains(active)
            )
              return;
            const replacement =
              Array.from(
                document.querySelectorAll<HTMLElement>("[data-issue-row]"),
              ).find((node) => node.dataset.issueRow === originID.current) ??
              null;
            const target = visibleFocusTarget(origin.current)
              ? origin.current
              : visibleFocusTarget(replacement)
                ? replacement
                : (document.getElementById("issue-list-heading") ??
                  document.getElementById("product-main"));
            if (visibleFocusTarget(target)) target.focus();
          }}
          onKeyDown={(event) => {
            if (
              event.defaultPrevented ||
              event.nativeEvent.isComposing ||
              editingTarget(event.target) ||
              event.metaKey ||
              event.ctrlKey ||
              event.altKey ||
              event.shiftKey ||
              (event.target as HTMLElement).closest('[role="dialog"]') !==
                content.current
            )
              return;
            if (event.key === "j" && next) {
              event.preventDefault();
              step(next);
            }
            if (event.key === "k" && previous) {
              event.preventDefault();
              step(previous);
            }
            if (event.key === "e") {
              const edit =
                content.current?.querySelector<HTMLButtonElement>(
                  "[data-issue-edit]",
                );
              if (edit && !edit.disabled) {
                event.preventDefault();
                edit.click();
              }
            }
          }}
        >
          <header className="space-y-3 border-b p-4 pr-12">
            <DialogTitle>{t("drawer.title")}</DialogTitle>
            <DialogDescription>{t("drawer.help")}</DialogDescription>
            <div className="flex flex-wrap gap-2">
              <Button
                type="button"
                variant="outline"
                disabled={!previous}
                onClick={() => step(previous)}
              >
                {t("drawer.previous")}
              </Button>
              <Button
                type="button"
                variant="outline"
                disabled={!next}
                onClick={() => step(next)}
              >
                {t("drawer.next")}
              </Button>
              {selected && (
                <Button type="button" variant="outline" asChild>
                  <Link href={`/w/${workspaceID}/issues/${selected}`}>
                    {t("drawer.fullPage")}
                  </Link>
                </Button>
              )}
            </div>
          </header>
          <div className="min-h-0 flex-1 overflow-y-auto p-4 sm:p-6">
            {selected && (
              <DrawerDraftContext.Provider
                key={selected}
                value={drawerScope(actorID, workspaceID, selected)}
              >
                <IssueDetail
                  workspaceId={workspaceID}
                  issueId={selected}
                  drawer
                />
              </DrawerDraftContext.Provider>
            )}
          </div>
        </DialogContent>
      </Dialog>
    </DrawerContext.Provider>
  );
}
export function IssueDrawerProvider({ children }: { children: ReactNode }) {
  const { user } = useAuth(),
    pathname = usePathname(),
    scope = productScope(pathname);
  return user?.uuid && scope.workspaceID ? (
    <ScopedDrawer
      key={JSON.stringify([user.uuid, scope.workspaceID, pathname])}
      actorID={user.uuid}
      workspaceID={scope.workspaceID}
    >
      {children}
    </ScopedDrawer>
  ) : (
    children
  );
}
export function useDrawerRows(ids: string[]) {
  const api = useIssueDrawer(),
    setRows = api?.setRows,
    signature = JSON.stringify(ids);
  useEffect(() => {
    setRows?.(JSON.parse(signature));
    return () => setRows?.([]);
  }, [setRows, signature]);
}

export function QuickIssueButton() {
  const api = useIssueDrawer(),
    { t } = useLocale();
  return api ? (
    <div className="flex flex-wrap items-center gap-2">
      <Button
        type="button"
        disabled={!api.canCreate}
        onClick={(event) => api.openCreate(event.currentTarget)}
      >
        {t("issues.create")}
      </Button>
      {!!api.visited.length && (
        <label className="text-sm">
          <span className="sr-only">{t("drawer.reopen")}</span>
          <select
            className="bg-background max-w-44 rounded border p-2"
            value=""
            onChange={(event) => {
              if (event.target.value)
                api.openIssue(event.target.value, event.currentTarget);
            }}
          >
            <option value="">{t("drawer.reopen")}</option>
            {api.visited.map((item, index) => (
              <option key={item.id} value={item.id}>
                {t("drawer.issue", { number: index + 1 })}
              </option>
            ))}
          </select>
        </label>
      )}
    </div>
  ) : null;
}
