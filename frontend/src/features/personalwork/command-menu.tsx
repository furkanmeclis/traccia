"use client";
import { useEffect, useRef, useState } from "react";
import { navigationFocus } from "./navigation-focus";
import { usePathname, useRouter } from "next/navigation";
import { Search } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogTitle,
  DialogDescription,
} from "@/components/ui/dialog";
import {
  Command,
  CommandInput,
  CommandList,
  CommandEmpty,
  CommandGroup,
  CommandItem,
} from "@/components/ui/command";
import { useLocale } from "@/providers/locale-provider";
import type { NavCatalog } from "@/features/nav-engine";
import type { useProductSwitchers } from "./switchers";
export function ProductCommandMenu({
  catalog,
  switchers,
  workspaceID,
}: {
  catalog: NavCatalog;
  switchers: ReturnType<typeof useProductSwitchers>;
  workspaceID: string | null;
}) {
  const { t } = useLocale(),
    router = useRouter(),
    pathname = usePathname();
  const [open, setOpen] = useState(false),
    [search, setSearch] = useState("");
  const input = useRef<HTMLInputElement>(null),
    trigger = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    function onKey(event: KeyboardEvent) {
      if (
        (event.metaKey || event.ctrlKey) &&
        event.key.toLowerCase() === "k" &&
        !event.isComposing &&
        !event.defaultPrevented
      ) {
        event.preventDefault();
        setOpen((value) => !value);
      }
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);
  function go(href: string) {
    setOpen(false);
    setSearch("");
    navigationFocus.request(href);
    router.push(href);
  }
  return (
    <>
      <Button
        ref={trigger}
        type="button"
        variant="outline"
        size="icon"
        aria-label={t("personalwork.commands")}
        title={t("personalwork.commands")}
        onClick={() => setOpen(true)}
      >
        <Search className="size-4" />
      </Button>
      <Dialog
        open={open}
        onOpenChange={(value) => {
          setOpen(value);
          if (!value) setSearch("");
        }}
      >
        <DialogContent
          closeLabel={t("common.close")}
          className="max-w-[calc(100vw-2rem)] overflow-hidden p-0 sm:max-w-xl"
          onOpenAutoFocus={(event) => {
            event.preventDefault();
            input.current?.focus();
          }}
          onCloseAutoFocus={(event) => {
            event.preventDefault();
            if (!navigationFocus.peek()) trigger.current?.focus();
            else
              requestAnimationFrame(() =>
                navigationFocus.consume(pathname, document),
              );
          }}
        >
          <div className="sr-only">
            <DialogTitle>{t("personalwork.commands")}</DialogTitle>
            <DialogDescription>
              {t("personalwork.commandsHelp")}
            </DialogDescription>
          </div>
          <Command label={t("personalwork.commandSearch")}>
            <CommandInput
              ref={input}
              value={search}
              onValueChange={setSearch}
              placeholder={t("personalwork.commandSearch")}
              aria-label={t("personalwork.commandSearch")}
            />
            <CommandList label={t("personalwork.commands")}>
              <CommandEmpty>{t("personalwork.noResults")}</CommandEmpty>
              <CommandGroup heading={t("personalwork.navigation")}>
                {catalog.groups
                  .flatMap((group) => group.items)
                  .map((item) => (
                    <CommandItem
                      key={item.id}
                      value={item.id + " " + t(item.titleKey)}
                      onSelect={() => go(item.href)}
                    >
                      <item.icon />
                      <span>{t(item.titleKey)}</span>
                    </CommandItem>
                  ))}
              </CommandGroup>
              {switchers.workspacesReadable && (
                <CommandGroup heading={t("workspaces.title")}>
                  {switchers.workspaces.data?.map((workspace) => (
                    <CommandItem
                      key={workspace.id}
                      value={workspace.id + " " + workspace.name}
                      onSelect={() => go(`/w/${workspace.id}/home`)}
                    >
                      {workspace.name}
                    </CommandItem>
                  ))}
                </CommandGroup>
              )}
              {workspaceID && switchers.resourcesReadable && (
                <>
                  <CommandGroup heading={t("teams.title")}>
                    {switchers.resources.data?.teams.map((team) => (
                      <CommandItem
                        key={team.id}
                        value={team.id + " " + team.code + " " + team.name}
                        onSelect={() =>
                          go(`/w/${workspaceID}/teams/${team.id}`)
                        }
                      >
                        {team.code} · {team.name}
                      </CommandItem>
                    ))}
                  </CommandGroup>
                  <CommandGroup heading={t("projects.title")}>
                    {switchers.resources.data?.projects.map((project) => (
                      <CommandItem
                        key={project.id}
                        value={project.id + " " + project.name}
                        onSelect={() =>
                          go(`/w/${workspaceID}/projects/${project.id}`)
                        }
                      >
                        {project.name}
                      </CommandItem>
                    ))}
                  </CommandGroup>
                </>
              )}
            </CommandList>
          </Command>
        </DialogContent>
      </Dialog>
    </>
  );
}
