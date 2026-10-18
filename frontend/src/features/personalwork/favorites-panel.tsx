"use client";
import Link from "next/link";
import { Star } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useLocale } from "@/providers/locale-provider";
import { useWorkspace } from "@/features/workspaces/queries";
import { useFavorites, usePersonalConfig } from "./queries";
import {
  useFavoriteOperation,
  FavoritePendingOperations,
} from "./favorite-operation";
import type { Favorite, FavoriteBody } from "./personal.service";
import type { ProductScope } from "./scope";
const same = (a: string, b: string) => a.toLowerCase() === b.toLowerCase();
export function favoriteHref(
  workspaceID: string,
  target: FavoriteBody["target"],
) {
  return target.kind === "workspace"
    ? `/w/${workspaceID}/home`
    : `/w/${workspaceID}/${target.kind === "issue" ? "issues" : target.kind === "project" ? "projects" : "teams"}/${target.id}`;
}
function FavoriteToggle({
  workspaceID,
  target,
  favorites,
  ready,
  manage,
}: {
  workspaceID: string;
  target: FavoriteBody["target"];
  favorites: Favorite[];
  ready: boolean;
  manage: boolean;
}) {
  const { t } = useLocale(),
    current = favorites.find(
      (row) =>
        row.target.kind === target.kind && same(row.target.id, target.id),
    ),
    operation = useFavoriteOperation(
      workspaceID,
      `${target.kind}:${target.id}`,
      ready && manage,
    ),
    active = current?.active ?? false;
  return (
    <div>
      <Button
        type="button"
        variant="outline"
        size="sm"
        aria-pressed={active}
        disabled={!ready || !manage || operation.locked}
        onClick={() =>
          void operation.submit(
            {
              action: "favorite.set",
              workspaceID,
              path: `/v1/workspaces/${workspaceID}/favorites`,
              body: {
                target,
                active: !active,
                expected_revision: current?.revision ?? null,
              },
            },
            t("personalwork.favoriteCommand"),
          )
        }
      >
        <Star className={active ? "fill-current" : ""} />
        {t(active ? "personalwork.removeFavorite" : "personalwork.addFavorite")}
      </Button>
      {operation.entry && (
        <p role="status" className="text-sm">
          <Link className="underline" href={`/w/${workspaceID}/home#favorites`}>
            {t("personalwork.pendingFavorite")}
          </Link>
        </p>
      )}
      {operation.error && <p role="alert">{t("personalwork.invalid")}</p>}
    </div>
  );
}
export function CurrentTargetFavorite({ scope }: { scope: ProductScope }) {
  if (!scope.workspaceID) return null;
  return (
    <CurrentTargetInner
      key={scope.workspaceID}
      scope={scope}
      workspaceID={scope.workspaceID}
    />
  );
}
function CurrentTargetInner({
  scope,
  workspaceID,
}: {
  scope: ProductScope;
  workspaceID: string;
}) {
  const workspace = useWorkspace(workspaceID),
    config = usePersonalConfig(workspaceID),
    favorites = useFavorites(workspaceID),
    ready =
      workspace.isSuccess &&
      !workspace.isError &&
      !workspace.isFetching &&
      config.isSuccess &&
      !config.isError &&
      !config.isFetching &&
      favorites.isSuccess &&
      !favorites.isError &&
      !favorites.isFetching;
  const target: FavoriteBody["target"] = scope.issueID
    ? { kind: "issue", id: scope.issueID }
    : scope.projectID
      ? { kind: "project", id: scope.projectID }
      : scope.teamID
        ? { kind: "team", id: scope.teamID }
        : { kind: "workspace", id: workspaceID };
  return (
    <FavoriteToggle
      workspaceID={workspaceID}
      target={target}
      favorites={ready ? favorites.data!.items : []}
      ready={ready}
      manage={
        ready &&
        !!workspace.data?.capabilities.update &&
        !!config.data?.capabilities.manage &&
        !!favorites.data?.capabilities.manage
      }
    />
  );
}
export function FavoritesPanel({ workspaceID }: { workspaceID: string }) {
  const { t } = useLocale(),
    workspace = useWorkspace(workspaceID),
    config = usePersonalConfig(workspaceID),
    favorites = useFavorites(workspaceID),
    ready =
      workspace.isSuccess &&
      !workspace.isError &&
      !workspace.isFetching &&
      config.isSuccess &&
      !config.isError &&
      !config.isFetching &&
      favorites.isSuccess &&
      !favorites.isError &&
      !favorites.isFetching;
  return (
    <section
      id="favorites"
      tabIndex={-1}
      className="space-y-3 rounded border p-4 outline-none"
    >
      <h2 className="text-lg font-semibold">{t("personalwork.favorites")}</h2>
      {!ready ? (
        <p
          role={
            favorites.isError || config.isError || workspace.isError
              ? "alert"
              : "status"
          }
        >
          {t(
            favorites.isError || config.isError || workspace.isError
              ? "personalwork.readError"
              : "personalwork.loading",
          )}
        </p>
      ) : (
        <>
          {!favorites.data?.items.length && (
            <p>{t("personalwork.noFavorites")}</p>
          )}
          <ul className="space-y-2">
            {favorites.data?.items.map((favorite) => (
              <li
                key={favorite.id}
                className="flex flex-wrap items-center justify-between gap-2 rounded border p-3"
              >
                <div>
                  <Link
                    className="underline"
                    href={favoriteHref(workspaceID, favorite.target)}
                  >
                    {favorite.metadata.identifier
                      ? favorite.metadata.identifier + " · "
                      : ""}
                    {favorite.metadata.label}
                  </Link>
                  {favorite.metadata.archived && (
                    <span className="ml-2 text-sm">
                      {t("personalwork.archived")}
                    </span>
                  )}
                  {!favorite.active && (
                    <span className="ml-2 text-sm">
                      {t("personalwork.inactiveFavorite")}
                    </span>
                  )}
                </div>
                <FavoriteToggle
                  workspaceID={workspaceID}
                  target={favorite.target}
                  favorites={favorites.data!.items}
                  ready={ready}
                  manage={
                    !!workspace.data?.capabilities.update &&
                    !!config.data?.capabilities.manage &&
                    !!favorites.data?.capabilities.manage
                  }
                />
              </li>
            ))}
          </ul>
        </>
      )}
      <FavoritePendingOperations workspaceID={workspaceID} />
    </section>
  );
}
