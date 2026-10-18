import { Home, Layers3, Search, Users, Folder, Tag, Star } from "lucide-react";
import { defineNav, type NavCatalog } from "@/features/nav-engine";
export function productCatalog(
  actorID: string | undefined,
  workspaceID: string | null,
  readable: boolean,
): NavCatalog {
  const home = {
    id: "home",
    titleKey: "personalwork.home",
    href: "/home",
    icon: Home,
  };
  return defineNav({
    id: JSON.stringify([
      "product-personal",
      actorID?.toLowerCase() ?? null,
      workspaceID?.toLowerCase() ?? null,
    ]),
    groups: [
      {
        id: "personal",
        labelKey: "personalwork.navigation",
        collapsible: false,
        items: [
          home,
          {
            id: "workspaces",
            titleKey: "workspaces.title",
            href: "/workspaces",
            icon: Layers3,
          },
          ...(workspaceID && readable
            ? [
                {
                  id: "workspace-home",
                  titleKey: "personalwork.workspaceHome",
                  href: `/w/${workspaceID}/home`,
                  icon: Home,
                },
                {
                  id: "favorites",
                  titleKey: "personalwork.favorites",
                  href: `/w/${workspaceID}/home#favorites`,
                  icon: Star,
                },
                {
                  id: "search",
                  titleKey: "searchviews.title",
                  href: `/w/${workspaceID}/search`,
                  icon: Search,
                },
                {
                  id: "issues",
                  titleKey: "issues.title",
                  href: `/w/${workspaceID}/issues`,
                  icon: Layers3,
                },
                {
                  id: "teams",
                  titleKey: "teams.title",
                  href: `/w/${workspaceID}/teams`,
                  icon: Users,
                },
                {
                  id: "projects",
                  titleKey: "projects.title",
                  href: `/w/${workspaceID}/projects`,
                  icon: Folder,
                },
                {
                  id: "labels",
                  titleKey: "teams.shared_labels",
                  href: `/w/${workspaceID}/labels`,
                  icon: Tag,
                },
              ]
            : []),
        ],
      },
    ],
  });
}
