"use client";
import { useEffect, useState, useSyncExternalStore } from "react";
import Link from "next/link";
import { Button } from "@/components/ui/button";
import { useLocale } from "@/providers/locale-provider";
import { useWorkspace } from "@/features/workspaces/queries";
import { QueryEditor } from "@/features/searchviews/query-editor";
import {
  definition,
  definitionQuery,
} from "@/features/searchviews/search.service";
import { validateQuery } from "@/features/searchviews/query";
import { queryEqual } from "@/features/searchviews/draft-state";
import { importQueryDraft } from "@/features/searchviews/query-import";
import { useProductSwitchers } from "./switchers";
import { homeDrafts, homeDraftKey } from "./draft-state";
import { portfolioCaptures } from "./personal-store";
import { portfolioScope, type PersonalResource } from "./portfolio-state";
import {
  usePersonalAccess,
  usePersonalConfig,
  usePortfolioPages,
} from "./queries";
import {
  beginPortfolio,
  retryPortfolio,
  portfolioFailure,
} from "./portfolio-capture";
import type { PortfolioPage } from "./personal.service";
import { collectPortfolioRows } from "./page-state";
import {
  PortfolioAccessGate,
  capturedScopesAuthorized,
} from "./portfolio-access";
import { FavoritesPanel } from "./favorites-panel";
export function PersonalHomeLanding() {
  const { t } = useLocale(),
    switchers = useProductSwitchers(null);
  return (
    <section className="space-y-4">
      <h1 className="text-2xl font-semibold">{t("personalwork.home")}</h1>
      <p>{t("personalwork.homeHelp")}</p>
      {!switchers.workspacesReadable ? (
        <p role={switchers.workspaces.isError ? "alert" : "status"}>
          {t(
            switchers.workspaces.isError
              ? "personalwork.readError"
              : "personalwork.loading",
          )}
        </p>
      ) : (
        <ul className="grid gap-3 sm:grid-cols-2">
          {switchers.workspaces.data?.map((workspace) => (
            <li key={workspace.id}>
              <Link
                href={`/w/${workspace.id}/home`}
                className="block rounded border p-4 font-medium underline"
              >
                {workspace.name}
              </Link>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
function PortfolioResults({
  workspaceID,
  first,
  readable,
}: {
  workspaceID: string;
  first: PortfolioPage;
  readable: boolean;
}) {
  const { t, locale } = useLocale(),
    { actorID } = usePersonalAccess(workspaceID),
    pages = usePortfolioPages(workspaceID, first, readable),
    id = portfolioScope({
      actorID: actorID ?? "",
      workspaceID,
      resource: first.session.resource,
    });
  useEffect(() => {
    if (!pages.error) return;
    const kind = portfolioFailure(pages.error);
    if (kind === "scope_changed" || kind === "forbidden")
      portfolioCaptures.block(id, first.session.id, kind);
    else if (kind === "expired") portfolioCaptures.expire(id, first.session.id);
  }, [pages.error, id, first.session.id]);
  useEffect(() => {
    const remaining = Date.parse(first.session.expires_at) - Date.now();
    if (remaining <= 0) {
      portfolioCaptures.expire(id, first.session.id);
      return;
    }
    const timer = setTimeout(
      () => portfolioCaptures.expire(id, first.session.id),
      remaining,
    );
    return () => clearTimeout(timer);
  }, [id, first.session.id, first.session.expires_at]);
  let rows: PortfolioPage["items"] = [],
    invalid = false;
  try {
    rows = collectPortfolioRows(pages.data?.pages ?? [first]);
  } catch {
    invalid = true;
  }
  if (!readable || pages.isError || invalid)
    return <p role="alert">{t("personalwork.readError")}</p>;
  const resource = first.session.resource,
    query = definitionQuery(first.definition);
  const date = (value: string) =>
    new Intl.DateTimeFormat(locale === "tr" ? "tr-TR" : "en-GB", {
      dateStyle: "medium",
      timeStyle: "short",
    }).format(new Date(value));
  return (
    <section
      className="min-w-0 space-y-3"
      aria-label={t("personalwork.applied")}
    >
      <div className="flex flex-wrap items-center gap-x-6 gap-y-2">
        <h2 className="text-lg font-semibold">
          {t(
            first.assigned_to_me
              ? "personalwork.assignedToMe"
              : "personalwork.results",
          )}
        </h2>
        <p>
          {t("personalwork.count")}:{" "}
          <strong data-testid="portfolio-total-count">
            {first.session.total_count}
          </strong>
        </p>
        <p className="text-sm">
          {t("personalwork.asOf")}:{" "}
          <time dateTime={first.session.as_of}>
            {date(first.session.as_of)}
          </time>
        </p>
      </div>
      <p className="text-muted-foreground text-sm">
        {t("personalwork.snapshotHelp")}
      </p>
      <ul className="flex flex-wrap gap-2 text-sm">
        {first.session.workspaces.map((scope) => (
          <li key={scope.id} className="rounded border px-2 py-1">
            {scope.name} · {scope.total_count}
          </li>
        ))}
      </ul>
      {query.search && (
        <p className="whitespace-pre-wrap">
          {t("personalwork.appliedSearch")}: {query.search}
        </p>
      )}
      <div className="max-w-full overflow-x-auto">
        <table className="w-full border-collapse text-sm">
          <caption className="sr-only">{t("personalwork.applied")}</caption>
          <thead>
            <tr>
              <th scope="col" className="border p-2 text-left">
                {t("workspaces.title")}
              </th>
              <th scope="col" className="border p-2 text-left">
                {t(
                  resource === "issue"
                    ? "issues.title"
                    : resource === "project"
                      ? "projects.title"
                      : "teams.title",
                )}
              </th>
              <th scope="col" className="border p-2">
                {t("personalwork.openLive")}
              </th>
            </tr>
          </thead>
          <tbody>
            {rows.map((row) => {
              const issue = row.resource === "issue" ? row.issue : null,
                project = row.resource === "project" ? row.project : null,
                team = row.resource === "team" ? row.team : null,
                current = issue ?? project ?? team;
              if (!current) return null;
              const label = issue
                ? issue.identifier + " · " + issue.title
                : project
                  ? project.name
                  : team?.code + " · " + team?.name;
              return (
                <tr
                  key={row.workspace_id + ":" + current.id}
                  data-portfolio-row={current.id}
                  data-workspace-id={row.workspace_id}
                >
                  <td className="border p-2">
                    {
                      first.session.workspaces.find(
                        (scope) => scope.id === row.workspace_id,
                      )?.name
                    }
                  </td>
                  <td className="border p-2 align-top whitespace-pre-wrap">
                    {label}
                  </td>
                  <td className="border p-2">
                    <Link
                      className="underline"
                      href={`/w/${row.workspace_id}/${resource === "issue" ? "issues" : resource === "project" ? "projects" : "teams"}/${current.id}`}
                    >
                      {t("personalwork.openLive")}
                    </Link>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
      {!rows.length && <p>{t("personalwork.noResults")}</p>}
      {pages.hasNextPage && (
        <Button
          type="button"
          variant="outline"
          disabled={pages.isFetching || !readable}
          onClick={() => void pages.fetchNextPage()}
        >
          {t("personalwork.loadMore")}
        </Button>
      )}
    </section>
  );
}
export function PersonalHome({ workspaceID }: { workspaceID: string }) {
  const { t } = useLocale(),
    access = usePersonalAccess(workspaceID),
    workspace = useWorkspace(workspaceID),
    switchers = useProductSwitchers(workspaceID),
    config = usePersonalConfig(workspaceID),
    [resource, setResource] = useState<PersonalResource>("issue"),
    drafts = useSyncExternalStore(
      homeDrafts.subscribe,
      homeDrafts.snapshot,
      homeDrafts.serverSnapshot,
    ),
    captures = useSyncExternalStore(
      portfolioCaptures.subscribe,
      portfolioCaptures.snapshot,
      portfolioCaptures.serverSnapshot,
    );
  const actorID = access.actorID ?? "",
    key = homeDraftKey(actorID, workspaceID, resource),
    draft = drafts.get(key),
    scope = { actorID, workspaceID, resource },
    capture = captures.get(portfolioScope(scope));
  const currentWorkspaceIDs = (
      switchers.workspacesReadable ? (switchers.workspaces.data ?? []) : []
    ).map((row) => row.id),
    anchorAuthorized =
      switchers.workspacesReadable &&
      currentWorkspaceIDs.some(
        (id) => id.toLowerCase() === workspaceID.toLowerCase(),
      );
  const readable =
    access.enabled &&
    anchorAuthorized &&
    workspace.isSuccess &&
    !workspace.isError &&
    !workspace.isFetching &&
    switchers.workspacesReadable &&
    config.isSuccess &&
    !config.isError &&
    !config.isFetching;
  useEffect(() => {
    if (access.enabled) homeDrafts.ensure(key, workspaceID, resource);
  }, [access.enabled, key, workspaceID, resource]);
  const ids = new Set(
      (switchers.workspacesReadable
        ? (switchers.workspaces.data ?? [])
        : []
      ).map((row) => row.id.toLowerCase()),
    ),
    selected = draft?.workspaceIDs ?? [],
    limit = config.data?.limits.max_workspaces ?? 32,
    scopesValid =
      selected.length > 0 &&
      selected.length <= limit &&
      selected.every((id) => ids.has(id.toLowerCase()));
  const queryValid = !!draft && validateQuery(resource, draft.query),
    ttl = draft?.ttl === "" ? null : Number(draft?.ttl),
    ttlValid =
      ttl === null ||
      (!!draft &&
        /^\d+$/.test(draft.ttl) &&
        Number.isInteger(ttl) &&
        ttl >= 60 &&
        ttl <= 86400),
    busy = capture?.status === "pending";
  function body() {
    if (!draft) throw new Error("PERSONAL_DRAFT_MISSING");
    return {
      workspace_ids: [...draft.workspaceIDs],
      definition: definition(resource, draft.query),
      assigned_to_me: resource === "issue" && draft.assignedToMe,
      ttl_seconds: ttl,
      limit: 100,
    };
  }
  useEffect(() => {
    const currentScope = { actorID, workspaceID, resource };
    if (
      readable &&
      draft &&
      scopesValid &&
      queryValid &&
      ttlValid &&
      !portfolioCaptures.get(portfolioScope(currentScope))
    )
      void beginPortfolio(currentScope, {
        workspace_ids: [...draft.workspaceIDs],
        definition: definition(resource, draft.query),
        assigned_to_me: resource === "issue" && draft.assignedToMe,
        ttl_seconds: ttl,
        limit: 100,
      });
  }, [
    readable,
    draft,
    scopesValid,
    queryValid,
    ttlValid,
    actorID,
    workspaceID,
    resource,
    ttl,
  ]);

  const appliedScopesAuthorized =
    !!capture?.applied &&
    capturedScopesAuthorized(
      workspaceID,
      capture.applied.command.body.workspace_ids,
      currentWorkspaceIDs,
      switchers.workspacesReadable,
    );
  const commandScopesAuthorized =
    !!capture &&
    capturedScopesAuthorized(
      workspaceID,
      capture.command.body.workspace_ids,
      currentWorkspaceIDs,
      switchers.workspacesReadable,
    );
  useEffect(() => {
    if (
      switchers.workspacesReadable &&
      capture &&
      capture.status !== "scope_changed" &&
      !commandScopesAuthorized
    )
      portfolioCaptures.denyScopes(
        portfolioScope({ actorID, workspaceID, resource }),
        capture.command.body.request_id,
      );
  }, [
    switchers.workspacesReadable,
    capture,
    commandScopesAuthorized,
    actorID,
    workspaceID,
    resource,
  ]);
  if (!draft) return <p role="status">{t("personalwork.loading")}</p>;
  const applied = capture?.applied,
    dirty =
      !!applied &&
      (!queryEqual(
        applied.command.body.definition,
        definition(resource, draft.query),
      ) ||
        !queryEqual(applied.command.body.workspace_ids, draft.workspaceIDs) ||
        applied.command.body.assigned_to_me !== draft.assignedToMe);
  return (
    <div className="space-y-8">
      <section className="space-y-4">
        <h1 className="text-2xl font-semibold">{t("personalwork.home")}</h1>
        <p>{t("personalwork.homeHelp")}</p>
        <form
          className="space-y-4 rounded border p-4"
          onSubmit={(event) => {
            event.preventDefault();
            if (readable && scopesValid && queryValid && ttlValid && !busy)
              void beginPortfolio(scope, body());
          }}
        >
          <div className="grid gap-3 sm:grid-cols-2">
            <label className="grid gap-1">
              <span>{t("personalwork.resource")}</span>
              <select
                className="rounded border p-2"
                value={resource}
                onChange={(event) => {
                  const next = event.target.value;
                  if (next === "issue" || next === "project" || next === "team")
                    setResource(next);
                }}
              >
                {(["issue", "project", "team"] as const).map((value) => (
                  <option key={value} value={value}>
                    {t(
                      value === "issue"
                        ? "issues.title"
                        : value === "project"
                          ? "projects.title"
                          : "teams.title",
                    )}
                  </option>
                ))}
              </select>
            </label>
            <label className="grid gap-1">
              <span>{t("personalwork.search")}</span>
              <input
                className="rounded border p-2"
                value={draft.query.search}
                onChange={(event) =>
                  homeDrafts.patch(key, {
                    query: { ...draft.query, search: event.target.value },
                  })
                }
              />
            </label>
          </div>
          {resource === "issue" && (
            <label className="flex items-center gap-2">
              <input
                type="checkbox"
                checked={draft.assignedToMe}
                onChange={(event) =>
                  homeDrafts.patch(key, { assignedToMe: event.target.checked })
                }
              />
              {t("personalwork.assignedToMe")}
            </label>
          )}
          <fieldset className="space-y-2">
            <legend className="font-medium">
              {t("personalwork.selectedWorkspaces")}
            </legend>
            <p className="text-muted-foreground text-sm">
              {t("personalwork.selectionHelp", { max: limit })}
            </p>
            {switchers.workspacesReadable ? (
              <div className="grid max-h-64 gap-2 overflow-y-auto sm:grid-cols-2">
                {switchers.workspaces.data?.map((row) => (
                  <label key={row.id} className="flex items-center gap-2">
                    <input
                      type="checkbox"
                      checked={selected.some(
                        (id) => id.toLowerCase() === row.id.toLowerCase(),
                      )}
                      disabled={
                        !selected.some(
                          (id) => id.toLowerCase() === row.id.toLowerCase(),
                        ) && selected.length >= limit
                      }
                      onChange={(event) =>
                        homeDrafts.patch(key, {
                          workspaceIDs: event.target.checked
                            ? [...selected, row.id]
                            : selected.filter(
                                (id) =>
                                  id.toLowerCase() !== row.id.toLowerCase(),
                              ),
                        })
                      }
                    />
                    {row.name}
                  </label>
                ))}
                {selected
                  .filter((id) => !ids.has(id.toLowerCase()))
                  .map((id) => (
                    <label key={id} className="flex items-center gap-2">
                      <input
                        type="checkbox"
                        checked
                        onChange={() =>
                          homeDrafts.patch(key, {
                            workspaceIDs: selected.filter(
                              (value) => value !== id,
                            ),
                          })
                        }
                      />
                      {t("personalwork.unavailableWorkspace")}
                    </label>
                  ))}
              </div>
            ) : (
              <p role="alert">{t("personalwork.readError")}</p>
            )}
            {selected.length >= limit && (
              <p role="status">
                {t("personalwork.scopeLimit", { max: limit })}
              </p>
            )}
          </fieldset>
          <details>
            <summary>{t("personalwork.advanced")}</summary>
            <div className="space-y-4 pt-3">
              <QueryEditor
                key={resource + ":" + draft.generation}
                resource={resource}
                value={draft.query}
                onChange={(query) => homeDrafts.patch(key, { query })}
                numericInputs={draft.numericInputs}
                onNumericInput={(path, raw) =>
                  homeDrafts.patch(key, {
                    numericInputs: { ...draft.numericInputs, [path]: raw },
                  })
                }
              />
              <label className="grid gap-1">
                <span>{t("personalwork.ttl")}</span>
                <input
                  className="rounded border p-2"
                  inputMode="numeric"
                  value={draft.ttl}
                  aria-invalid={!ttlValid}
                  onChange={(event) =>
                    homeDrafts.patch(key, { ttl: event.target.value })
                  }
                />
              </label>
              <details>
                <summary>{t("searchviews.rawQuery")}</summary>
                <textarea
                  className="min-h-32 w-full rounded border p-2 font-mono text-xs"
                  aria-label={t("searchviews.rawQuery")}
                  value={draft.raw}
                  onChange={(event) =>
                    homeDrafts.patch(key, {
                      raw: event.target.value,
                      error: null,
                    })
                  }
                />
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => {
                    const imported = importQueryDraft(resource, draft.raw);
                    if (imported.error)
                      homeDrafts.patch(key, { error: t(imported.error) });
                    else homeDrafts.loadQuery(key, imported.query);
                  }}
                >
                  {t("searchviews.importQuery")}
                </Button>
                {draft.error && <p role="alert">{draft.error}</p>}
              </details>
            </div>
          </details>
          {(!scopesValid || !queryValid || !ttlValid) && (
            <p role="alert">{t("personalwork.invalid")}</p>
          )}
          <Button
            type="submit"
            disabled={
              !readable || !scopesValid || !queryValid || !ttlValid || busy
            }
          >
            {t("personalwork.apply")}
          </Button>
        </form>
        {!readable && <p role="alert">{t("personalwork.readError")}</p>}
        {busy && <p role="status">{t("personalwork.loading")}</p>}
        {capture?.status === "uncertain" && (
          <div role="status">
            <p>{t("personalwork.captureUncertain")}</p>
            <Button
              type="button"
              disabled={!readable || busy || !commandScopesAuthorized}
              onClick={() => void retryPortfolio(scope)}
            >
              {t("personalwork.retryCapture")}
            </Button>
          </div>
        )}
        {capture?.status === "expired" && (
          <p role="alert">{t("personalwork.expired")}</p>
        )}
        {(capture?.status === "scope_changed" ||
          capture?.status === "forbidden") && (
          <p role="alert">{t("personalwork.scopeChanged")}</p>
        )}
        {capture?.status === "rejected" && (
          <p role="alert">{t("personalwork.invalid")}</p>
        )}
        {dirty && <p role="status">{t("personalwork.unapplied")}</p>}
        {capture && (
          <Button
            type="button"
            variant="outline"
            disabled={
              !readable ||
              busy ||
              capture.command.body.workspace_ids.some(
                (id) => !ids.has(id.toLowerCase()),
              )
            }
            onClick={() => {
              void beginPortfolio(scope, capture.command.body);
            }}
          >
            {t("personalwork.refresh")}
          </Button>
        )}
        {applied && capture?.status === "ready" && (
          <PortfolioAccessGate
            anchorID={workspaceID}
            capturedWorkspaceIDs={applied.command.body.workspace_ids}
            currentWorkspaceIDs={currentWorkspaceIDs}
            complete={switchers.workspacesReadable}
            readable={readable}
          >
            <PortfolioResults
              workspaceID={workspaceID}
              first={applied.page}
              readable={readable && appliedScopesAuthorized}
            />
          </PortfolioAccessGate>
        )}
      </section>
      <FavoritesPanel workspaceID={workspaceID} />
    </div>
  );
}
