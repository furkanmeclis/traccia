"use client";
import Link from "next/link";
import { useLocale } from "@/providers/locale-provider";
import type { SearchResource, CaptureSession } from "./capture-state";
import type { Column, Query } from "./query";
export type SnapshotRow = {
  id: string;
  workspace_id: string;
  [key: string]: string | number | boolean | string[] | null;
};
export function SnapshotTable({
  resource,
  workspaceID,
  session,
  query,
  rows,
  columns,
  readable,
}: {
  resource: SearchResource;
  workspaceID: string;
  session: CaptureSession;
  query: Query;
  rows: SnapshotRow[];
  columns: Column[];
  readable: boolean;
}) {
  const { t } = useLocale();
  if (!readable) return <p role="alert">{t("searchviews.readForbidden")}</p>;
  function cell(row: SnapshotRow, key: string) {
    const value = row[key];
    if (value === null || value === undefined) return "—";
    if (typeof value === "boolean")
      return t(value ? "searchviews.yes" : "searchviews.no");
    if (Array.isArray(value)) return value.join(", ");
    if (key === "priority" && typeof value === "string") {
      const labels: Record<string, string> = {
        none: "issues.priority_none",
        urgent: "issues.priority_urgent",
        high: "issues.priority_high",
        normal: "issues.priority_normal",
        low: "issues.priority_low",
      };
      return labels[value] ? t(labels[value]) : value;
    }
    return String(value);
  }
  return (
    <section
      className="min-w-0 space-y-3"
      aria-label={t("searchviews.applied")}
    >
      <dl className="grid gap-2 text-sm sm:grid-cols-3">
        <div>
          <dt>{t("searchviews.asOf")}</dt>
          <dd>
            <time dateTime={session.as_of}>{session.as_of}</time>
          </dd>
        </div>
        <div>
          <dt>{t("searchviews.expires")}</dt>
          <dd>
            <time dateTime={session.expires_at}>{session.expires_at}</time>
          </dd>
        </div>
        <div>
          <dt>{t("searchviews.count")}</dt>
          <dd className="font-mono" data-testid="snapshot-total-count">
            {session.total_count}
          </dd>
        </div>
      </dl>
      <p className="text-muted-foreground text-sm">
        {t("searchviews.snapshotHelp")}
      </p>
      <details>
        <summary>{t("searchviews.applied")}</summary>
        <pre className="max-h-64 overflow-auto rounded border p-2 text-xs">
          {JSON.stringify(query, null, 2)}
        </pre>
      </details>
      {!readable ? (
        <p role="alert">{t("searchviews.readForbidden")}</p>
      ) : (
        <div className="max-w-full overflow-x-auto">
          <table className="w-full border-collapse text-sm">
            <caption className="sr-only">
              {t("searchviews.applied")} — {session.as_of}
            </caption>
            <thead>
              <tr>
                {columns.map((column) => (
                  <th
                    key={column.key}
                    scope="col"
                    className="border p-2 text-left"
                    style={{ minWidth: column.width, width: column.width }}
                  >
                    {t("searchviews.fields." + column.key)}
                  </th>
                ))}
                <th scope="col" className="border p-2">
                  {t("searchviews.openLive")}
                </th>
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => (
                <tr key={row.id} data-snapshot-row={row.id}>
                  {columns.map((column) => (
                    <td
                      key={column.key}
                      className="border p-2 align-top break-words whitespace-pre-wrap"
                      style={{ minWidth: column.width, width: column.width }}
                    >
                      {cell(row, column.key)}
                    </td>
                  ))}
                  <td className="border p-2">
                    <Link
                      className="underline"
                      href={`/w/${workspaceID}/${resource === "issue" ? "issues" : resource === "project" ? "projects" : "teams"}/${row.id}`}
                    >
                      {t("searchviews.openLive")}
                    </Link>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {rows.length === 0 && (
            <p className="p-3">{t("searchviews.noResults")}</p>
          )}
        </div>
      )}
    </section>
  );
}
