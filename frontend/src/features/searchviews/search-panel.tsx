"use client";
import {
  useEffect,
  useState,
  useSyncExternalStore,
  type FormEvent,
} from "react";
import Link from "next/link";
import { Button } from "@/components/ui/button";
import { useAuth } from "@/providers/auth-provider";
import { useLocale } from "@/providers/locale-provider";
import { useWorkspace } from "@/features/workspaces/queries";
import { importQueryDraft } from "./query-import";
import { QueryEditor } from "./query-editor";
import { ColumnEditor } from "./column-editor";
import { SnapshotTable, type SnapshotRow } from "./result-table";
import { viewDrafts, draftScope, queryEqual } from "./draft-state";
import { captures } from "./search-store";
import { captureScope, type SearchResource } from "./capture-state";
import {
  defaultColumns,
  validateQuery,
  validateColumns,
  validText,
  type Query,
} from "./query";
import {
  useSearchConfig,
  useColumns,
  useSavedViews,
  useSavedView,
  useSnapshotPages,
} from "./queries";
import {
  definition,
  definitionQuery,
  type SearchPage,
  type SavedView,
} from "./search.service";
import { beginCapture, retryCapture, isExpiredCapture } from "./search-capture";
import { collectSnapshotRows } from "./page-state";
import { useViewOperation, ViewPendingOperations } from "./view-operation";
function SnapshotResults({
  workspaceID,
  resource,
  first,
  columns,
  readable,
  expired,
}: {
  workspaceID: string;
  resource: SearchResource;
  first: SearchPage;
  columns: Parameters<typeof SnapshotTable>[0]["columns"];
  readable: boolean;
  expired: boolean;
}) {
  const { user } = useAuth(),
    { t } = useLocale(),
    pages = useSnapshotPages(
      workspaceID,
      resource,
      first,
      readable && !expired,
    ),
    id = captureScope({ actorID: user?.uuid ?? "", workspaceID, resource });
  useEffect(() => {
    if (isExpiredCapture(pages.error)) captures.expire(id, first.session.id);
  }, [pages.error, id, first.session.id]);
  useEffect(() => {
    const remaining = Date.parse(first.session.expires_at) - Date.now();
    if (!Number.isFinite(remaining)) return;
    if (remaining <= 0) {
      captures.expire(id, first.session.id);
      return;
    }
    const timer = setTimeout(
      () => captures.expire(id, first.session.id),
      remaining,
    );
    return () => clearTimeout(timer);
  }, [id, first.session.id, first.session.expires_at]);
  let rows: SnapshotRow[] = [],
    validation: string | null = null;
  try {
    rows = collectSnapshotRows<SnapshotRow>(pages.data?.pages ?? [first]);
    if (
      rows.some(
        (row) => row.workspace_id.toLowerCase() !== workspaceID.toLowerCase(),
      ) ||
      !validateQuery(resource, first.query)
    )
      throw new Error("SEARCH_RESPONSE_SCOPE");
  } catch (error) {
    validation = error instanceof Error ? error.message : String(error);
  }
  const query = first.query;
  if (!validateQuery(resource, query))
    return <p role="alert">{t("searchviews.readError")}</p>;
  return (
    <div className="space-y-3">
      {(pages.isError || validation) && (
        <p role="alert">
          {isExpiredCapture(pages.error)
            ? t("searchviews.expired")
            : t("searchviews.readError")}
          {validation && <span className="block text-xs">{validation}</span>}
        </p>
      )}
      <SnapshotTable
        resource={resource}
        workspaceID={workspaceID}
        session={first.session}
        query={query}
        rows={rows}
        columns={columns}
        readable={readable && !validation && !pages.isError}
      />
      {pages.hasNextPage && (
        <Button
          type="button"
          variant="outline"
          disabled={
            !readable ||
            expired ||
            pages.isFetching ||
            pages.isError ||
            !!validation
          }
          onClick={() => void pages.fetchNextPage()}
        >
          {t("searchviews.loadMore")}
        </Button>
      )}
      {pages.isError && !isExpiredCapture(pages.error) && (
        <Button
          type="button"
          variant="outline"
          disabled={!readable || pages.isFetching}
          onClick={() => void pages.refetch()}
        >
          {t("common.retry")}
        </Button>
      )}
    </div>
  );
}
function SavedViewEditor({
  workspaceID,
  summary,
  ready,
  onLoad,
}: {
  workspaceID: string;
  summary: SavedView;
  ready: boolean;
  onLoad: (
    resource: SearchResource,
    query: Query,
    columns: SavedView["columns"],
  ) => void;
}) {
  const { user } = useAuth(),
    { t } = useLocale(),
    current = useSavedView(workspaceID, summary.id, ready),
    view = current.data?.view ?? summary,
    currentResource = view.definition.resource;
  const key = draftScope(
      user?.uuid ?? "",
      workspaceID,
      currentResource,
      "view:" + view.id,
    ),
    drafts = useSyncExternalStore(
      viewDrafts.subscribe,
      viewDrafts.snapshot,
      viewDrafts.serverSnapshot,
    ),
    draft = drafts.get(key),
    resource = draft?.resource ?? currentResource;
  useEffect(() => {
    if (!viewDrafts.get(key))
      viewDrafts.ensure(key, currentResource, {
        name: view.name,
        query: definitionQuery(view.definition),
        columns: view.columns,
        revision: view.revision,
      });
  }, [key, currentResource, view]);
  const readReady =
      ready && current.isSuccess && !current.isError && !current.isFetching,
    operation = useViewOperation(
      workspaceID,
      "view:" + view.id,
      readReady && view.capabilities.manage,
      (result) => {
        if ("view" in result)
          viewDrafts.reset(key, result.view.definition.resource, {
            name: result.view.name,
            query: definitionQuery(result.view.definition),
            columns: result.view.columns,
            revision: result.view.revision,
          });
      },
    );
  if (!draft) return null;
  const stale =
      draft.revision !== view.revision || draft.resource !== currentResource,
    valid =
      validateQuery(resource, draft.query) &&
      validateColumns(resource, draft.columns) &&
      draft.name.length > 0 &&
      validText(draft.name, 120),
    base = `/v1/workspaces/${workspaceID}/saved-views/${view.id}`;
  return (
    <>
      {!readReady && <p role="alert">{t("searchviews.readError")}</p>}
      <article hidden={!readReady} className="space-y-3 rounded border p-3">
        <h3 className="font-medium">
          {view.name}
          {view.deleted ? " — " + t("searchviews.deleted") : ""}
        </h3>
        <p className="font-mono text-xs break-all">
          {t("searchviews.revision")}: {view.revision}
        </p>
        {!readReady && <p role="alert">{t("searchviews.readError")}</p>}
        {stale && <p role="alert">{t("searchviews.stale")}</p>}
        <Button
          type="button"
          variant="outline"
          disabled={!readReady}
          onClick={() =>
            onLoad(
              view.definition.resource,
              definitionQuery(view.definition),
              view.columns,
            )
          }
        >
          {t("searchviews.loadView")}
        </Button>
        <details>
          <summary>{t("searchviews.saveView")}</summary>
          <form
            className="space-y-3 pt-3"
            onSubmit={(e: FormEvent) => {
              e.preventDefault();
              if (
                !readReady ||
                !view.capabilities.update ||
                operation.locked ||
                stale ||
                !valid ||
                !draft.revision
              )
                return;
              void operation.submit(
                {
                  action: "view.update",
                  workspaceID,
                  viewID: view.id,
                  path: base + "/update",
                  body: {
                    expected_revision: draft.revision,
                    name: draft.name,
                    definition: definition(resource, draft.query),
                    columns: draft.columns,
                  },
                },
                t("searchviews.saveView"),
              );
            }}
          >
            <fieldset className="space-y-3" disabled={operation.locked}>
              <label className="grid gap-1">
                <span>{t("searchviews.viewName")}</span>
                <input
                  className="rounded border p-2"
                  value={draft.name}
                  onChange={(e) =>
                    viewDrafts.patch(key, { name: e.target.value })
                  }
                />
              </label>
              <QueryEditor
                key={draft.generation}
                resource={resource}
                value={draft.query}
                numericInputs={draft.numericInputs}
                onNumericInput={(path, raw) =>
                  viewDrafts.patch(key, {
                    numericInputs: {
                      ...viewDrafts.get(key)!.numericInputs,
                      [path]: raw,
                    },
                  })
                }
                onChange={(query) => viewDrafts.patch(key, { query })}
              />
              <ColumnEditor
                resource={resource}
                value={draft.columns}
                onChange={(columns) => viewDrafts.patch(key, { columns })}
              />
              {!valid && <p role="alert">{t("searchviews.invalid")}</p>}
              <Button
                type="submit"
                disabled={
                  !readReady ||
                  !view.capabilities.update ||
                  operation.locked ||
                  stale ||
                  !valid
                }
              >
                {t("searchviews.saveView")}
              </Button>
            </fieldset>
          </form>
        </details>
        <div className="flex flex-wrap gap-2">
          <Button
            type="button"
            variant="outline"
            disabled={!readReady || operation.locked}
            onClick={() =>
              viewDrafts.reset(key, currentResource, {
                name: view.name,
                query: definitionQuery(view.definition),
                columns: view.columns,
                revision: view.revision,
              })
            }
          >
            {t("searchviews.resetDraft")}
          </Button>
          <Button
            type="button"
            variant="destructive"
            disabled={
              !readReady ||
              !view.capabilities.delete ||
              operation.locked ||
              stale
            }
            onClick={() =>
              void operation.submit(
                {
                  action: "view.delete",
                  workspaceID,
                  viewID: view.id,
                  path: base + "/delete",
                  body: { expected_revision: view.revision },
                },
                t("searchviews.deleteView"),
              )
            }
          >
            {t("searchviews.deleteView")}
          </Button>
          <Button
            type="button"
            variant="outline"
            disabled={
              !readReady ||
              !view.capabilities.restore ||
              operation.locked ||
              stale
            }
            onClick={() =>
              void operation.submit(
                {
                  action: "view.restore",
                  workspaceID,
                  viewID: view.id,
                  path: base + "/restore",
                  body: { expected_revision: view.revision },
                },
                t("searchviews.restoreView"),
              )
            }
          >
            {t("searchviews.restoreView")}
          </Button>
        </div>
        {operation.error && <p role="alert">{operation.error}</p>}
      </article>
    </>
  );
}
function ResourceSearch({
  workspaceID,
  resource,
  onResource,
}: {
  workspaceID: string;
  resource: SearchResource;
  onResource: (resource: SearchResource) => void;
}) {
  const { user } = useAuth(),
    { t } = useLocale(),
    workspace = useWorkspace(workspaceID),
    config = useSearchConfig(
      workspaceID,
      workspace.isSuccess && !workspace.isError,
    ),
    columns = useColumns(
      workspaceID,
      resource,
      workspace.isSuccess && !workspace.isError,
    ),
    [includeDeleted, setIncludeDeleted] = useState(false),
    views = useSavedViews(
      workspaceID,
      resource,
      includeDeleted,
      workspace.isSuccess && !workspace.isError,
    ),
    [error, setError] = useState<string | null>(null);
  const key = draftScope(user?.uuid ?? "", workspaceID, resource),
    columnKey = draftScope(user?.uuid ?? "", workspaceID, resource, "columns"),
    drafts = useSyncExternalStore(
      viewDrafts.subscribe,
      viewDrafts.snapshot,
      viewDrafts.serverSnapshot,
    ),
    draft = drafts.get(key),
    columnDraft = drafts.get(columnKey),
    allCaptures = useSyncExternalStore(
      captures.subscribe,
      captures.snapshot,
      captures.serverSnapshot,
    ),
    scope = { actorID: user?.uuid ?? "", workspaceID, resource },
    capture = allCaptures.get(captureScope(scope));
  useEffect(() => {
    if (!user?.uuid) return;
    if (columns.data) {
      viewDrafts.ensure(key, resource, {
        name: "",
        query: {
          version: 1,
          archived: "active",
          search: "",
          filter: null,
          sort: [],
        },
        columns: columns.data.columns.length
          ? columns.data.columns
          : defaultColumns(resource),
        revision: null,
      });
      viewDrafts.ensure(columnKey, resource, {
        name: "",
        query: {
          version: 1,
          archived: "active",
          search: "",
          filter: null,
          sort: [],
        },
        columns: columns.data.columns.length
          ? columns.data.columns
          : defaultColumns(resource),
        revision: columns.data.revision,
      });
    }
  }, [key, columnKey, resource, user?.uuid, columns.data]);
  const readable =
      workspace.isSuccess &&
      !workspace.isError &&
      !workspace.isFetching &&
      config.isSuccess &&
      !config.isError &&
      !config.isFetching,
    manageable =
      readable &&
      !!workspace.data?.capabilities.update &&
      !!config.data?.capabilities.manage;
  const newView = useViewOperation(
      workspaceID,
      "create:" + resource,
      manageable,
    ),
    columnOp = useViewOperation(
      workspaceID,
      "columns:" + resource,
      manageable &&
        columns.isSuccess &&
        !columns.isError &&
        !columns.isFetching,
      (result) => {
        if (!("view" in result))
          viewDrafts.reset(columnKey, resource, {
            name: "",
            query: {
              version: 1,
              archived: "active",
              search: "",
              filter: null,
              sort: [],
            },
            columns: result.columns,
            revision: result.revision,
          });
      },
    );
  if (!draft || !columnDraft)
    return (
      <div className="space-y-2">
        <p>{t("searchviews.readError")}</p>
        <Button
          type="button"
          variant="outline"
          onClick={() => void columns.refetch()}
        >
          {t("common.retry")}
        </Button>
      </div>
    );
  const ttl = draft.ttl;
  const valid =
      validateQuery(resource, draft.query) &&
      validateColumns(resource, draft.columns),
    ttlValue = ttl === "" ? null : Number(ttl),
    ttlValid =
      ttlValue === null ||
      (/^\d+$/.test(ttl) &&
        Number.isInteger(ttlValue) &&
        ttlValue >= (config.data?.limits.min_ttl_seconds ?? 60) &&
        ttlValue <= (config.data?.limits.max_ttl_seconds ?? 86400)),
    busy = capture?.status === "pending",
    columnStale =
      !!columns.data && columnDraft.revision !== columns.data.revision;
  const captureQuery = async (query: Query, seconds: number | null) => {
    if (!readable || busy) return;
    try {
      setError(null);
      await beginCapture(scope, query, seconds);
    } catch (error) {
      setError(error instanceof Error ? error.message : String(error));
    }
  };
  return (
    <div className="space-y-6">
      {!readable && <p role="alert">{t("searchviews.readError")}</p>}
      {readable && !manageable && <p>{t("searchviews.noManage")}</p>}
      <p className="text-muted-foreground text-sm">
        {t("searchviews.matchingHelp")}
      </p>
      <form
        className="space-y-3"
        onSubmit={(e) => {
          e.preventDefault();
          if (valid && ttlValid) void captureQuery(draft.query, ttlValue);
        }}
      >
        <h2 className="text-lg font-semibold">{t("searchviews.draft")}</h2>
        {!queryEqual(
          draft.query,
          capture?.applied?.command.body.query ?? draft.baseline.query,
        ) && <p>{t("searchviews.dirty")}</p>}
        <QueryEditor
          key={draft.generation}
          resource={resource}
          value={draft.query}
          numericInputs={draft.numericInputs}
          onNumericInput={(path, raw) =>
            viewDrafts.patch(key, {
              numericInputs: {
                ...viewDrafts.get(key)!.numericInputs,
                [path]: raw,
              },
            })
          }
          onChange={(query) => viewDrafts.patch(key, { query })}
        />
        <label className="grid gap-1">
          <span>{t("searchviews.ttl")}</span>
          <input
            className="rounded border p-2"
            inputMode="numeric"
            value={ttl}
            placeholder={t("searchviews.defaultTTL")}
            aria-invalid={!ttlValid}
            onChange={(e) => viewDrafts.patch(key, { ttl: e.target.value })}
          />
        </label>
        <ColumnEditor
          resource={resource}
          value={draft.columns}
          onChange={(next) => viewDrafts.patch(key, { columns: next })}
        />
        {(!valid || !ttlValid) && (
          <p role="alert">{t("searchviews.invalid")}</p>
        )}
        <Button
          type="submit"
          disabled={!readable || busy || !valid || !ttlValid}
        >
          {t("searchviews.apply")}
        </Button>
      </form>
      <details>
        <summary>{t("searchviews.rawQuery")}</summary>
        <div className="space-y-2 pt-2">
          <textarea
            className="min-h-32 w-full rounded border p-2 font-mono text-xs"
            aria-label={t("searchviews.rawQuery")}
            value={draft.raw}
            onChange={(e) =>
              viewDrafts.patch(key, { raw: e.target.value, error: null })
            }
          />
          <Button
            type="button"
            variant="outline"
            onClick={() => {
              const imported = importQueryDraft(resource, draft.raw);
              if (imported.error) {
                viewDrafts.patch(key, { error: t(imported.error) });
              } else {
                viewDrafts.loadQuery(key, imported.query);
              }
            }}
          >
            {t("searchviews.importQuery")}
          </Button>
          <Button
            type="button"
            variant="outline"
            onClick={() =>
              viewDrafts.patch(key, {
                raw: JSON.stringify(draft.query, null, 2),
              })
            }
          >
            {t("searchviews.exportQuery")}
          </Button>
          {draft.error && <p role="alert">{draft.error}</p>}
        </div>
      </details>
      <section className="space-y-3">
        <h2 className="text-lg font-semibold">{t("searchviews.applied")}</h2>
        {busy && <p role="status">{t("searchviews.capturePending")}</p>}
        {capture?.status === "uncertain" && (
          <>
            <p role="alert">{t("searchviews.uncertain")}</p>
            <Button
              type="button"
              disabled={!readable || busy}
              onClick={() =>
                void retryCapture(scope).catch((e) => setError(String(e)))
              }
            >
              {t("searchviews.retryRead")}
            </Button>
          </>
        )}
        {capture?.status === "expired" && (
          <p role="alert">{t("searchviews.expired")}</p>
        )}
        {(capture?.status === "forbidden" ||
          capture?.status === "rejected") && (
          <p role="alert">{capture.error ?? t("searchviews.readForbidden")}</p>
        )}
        {capture && (
          <p className="font-mono text-xs break-all">
            {t("searchviews.captureID")}: {capture.command.body.request_id}
          </p>
        )}
        {capture?.applied && (
          <>
            <Button
              type="button"
              variant="outline"
              disabled={!readable || busy}
              onClick={() =>
                void captureQuery(
                  capture.applied!.command.body.query,
                  capture.applied!.command.body.ttl_seconds,
                )
              }
            >
              {t("searchviews.refresh")}
            </Button>
            <SnapshotResults
              key={capture.applied.page.session.id}
              workspaceID={workspaceID}
              resource={resource}
              first={capture.applied.page}
              expired={capture.status === "expired"}
              columns={
                validateColumns(resource, draft.columns)
                  ? draft.columns
                  : draft.baseline.columns
              }
              readable={readable && capture.status !== "forbidden"}
            />
          </>
        )}
        {error && <p role="alert">{error}</p>}
      </section>
      <section className="space-y-3">
        <h2 className="text-lg font-semibold">{t("searchviews.columns")}</h2>
        {columnStale && <p role="alert">{t("searchviews.stale")}</p>}
        <p className="font-mono text-xs break-all">
          {t("searchviews.revision")}:{" "}
          {columnDraft.revision ?? t("searchviews.unset")}
        </p>
        <div className="flex flex-wrap gap-2">
          <Button
            type="button"
            disabled={
              !manageable ||
              !columns.isSuccess ||
              columns.isError ||
              columns.isFetching ||
              columnOp.locked ||
              columnStale ||
              !validateColumns(resource, draft.columns)
            }
            onClick={() =>
              void columnOp.submit(
                {
                  action: "columns.set",
                  workspaceID,
                  path: `/v1/workspaces/${workspaceID}/column-preferences`,
                  body: {
                    resource,
                    expected_revision: columnDraft.revision,
                    columns: draft.columns,
                  },
                },
                t("searchviews.saveColumns"),
              )
            }
          >
            {t("searchviews.saveColumns")}
          </Button>
          <Button
            type="button"
            variant="outline"
            disabled={
              !readable ||
              !columns.isSuccess ||
              columns.isError ||
              columns.isFetching ||
              columnOp.locked
            }
            onClick={() => {
              viewDrafts.reset(columnKey, resource, {
                name: "",
                query: draft.query,
                columns: columns.data!.columns.length
                  ? columns.data!.columns
                  : defaultColumns(resource),
                revision: columns.data!.revision,
              });
              viewDrafts.patch(key, {
                columns: columns.data!.columns.length
                  ? columns.data!.columns
                  : defaultColumns(resource),
              });
            }}
          >
            {t("searchviews.resetDraft")}
          </Button>
        </div>
      </section>
      <section className="space-y-3">
        <h2 className="text-lg font-semibold">{t("searchviews.views")}</h2>
        <form
          className="flex flex-wrap items-end gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            if (
              manageable &&
              valid &&
              draft.name.length > 0 &&
              validText(draft.name, 120)
            )
              void newView.submit(
                {
                  action: "view.create",
                  workspaceID,
                  path: `/v1/workspaces/${workspaceID}/saved-views`,
                  body: {
                    name: draft.name,
                    definition: definition(resource, draft.query),
                    columns: draft.columns,
                  },
                },
                t("searchviews.saveNew"),
              );
          }}
        >
          <label className="grid gap-1">
            <span>{t("searchviews.viewName")}</span>
            <input
              className="rounded border p-2"
              value={draft.name}
              onChange={(e) => viewDrafts.patch(key, { name: e.target.value })}
            />
          </label>
          <Button
            type="submit"
            disabled={
              !manageable ||
              !valid ||
              !validText(draft.name, 120) ||
              draft.name.length === 0 ||
              newView.locked
            }
          >
            {t("searchviews.saveNew")}
          </Button>
        </form>
        <label className="flex gap-2">
          <input
            type="checkbox"
            checked={includeDeleted}
            onChange={(e) => setIncludeDeleted(e.target.checked)}
          />
          {t("searchviews.includeDeleted")}
        </label>
        {views.isError && <p role="alert">{t("searchviews.readError")}</p>}
        {views.data?.pages
          .flatMap((p) => p.items)
          .map((view) => (
            <SavedViewEditor
              key={view.id}
              workspaceID={workspaceID}
              summary={view}
              ready={readable && !views.isError && !views.isFetching}
              onLoad={(nextResource, query, nextColumns) => {
                const nextKey = draftScope(
                  user?.uuid ?? "",
                  workspaceID,
                  nextResource,
                );
                viewDrafts.ensure(nextKey, nextResource);
                viewDrafts.loadQuery(nextKey, query);
                viewDrafts.patch(nextKey, { columns: nextColumns });
                onResource(nextResource);
              }}
            />
          ))}
        {views.hasNextPage && (
          <Button
            type="button"
            variant="outline"
            disabled={!readable || views.isFetching || views.isError}
            onClick={() => void views.fetchNextPage()}
          >
            {t("searchviews.loadMore")}
          </Button>
        )}
        {newView.error && <p role="alert">{newView.error}</p>}
      </section>
      <ViewPendingOperations workspaceID={workspaceID} />
    </div>
  );
}
export function SearchPanel({ workspaceID }: { workspaceID: string }) {
  const { t } = useLocale(),
    [resource, setResource] = useState<SearchResource>("issue");
  return (
    <div className="min-w-0 space-y-6">
      <Link href={`/w/${workspaceID}`} className="text-sm underline">
        {t("workspaces.title")}
      </Link>
      <h1 className="text-2xl font-semibold">{t("searchviews.title")}</h1>
      <label className="grid gap-1">
        <span>{t("searchviews.resource")}</span>
        <select
          className="rounded border p-2"
          value={resource}
          onChange={(e) => {
            const next = e.target.value;
            if (next === "issue" || next === "project" || next === "team")
              setResource(next);
          }}
        >
          {(["issue", "project", "team"] as const).map((r) => (
            <option key={r} value={r}>
              {t("searchviews.resources." + r)}
            </option>
          ))}
        </select>
      </label>
      <ResourceSearch
        key={workspaceID + resource}
        workspaceID={workspaceID}
        resource={resource}
        onResource={setResource}
      />
    </div>
  );
}
