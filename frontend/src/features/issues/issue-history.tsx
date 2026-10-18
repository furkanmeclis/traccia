"use client";
import { issueEventKeys } from "./issue-copy";
import { useState } from "react";
import type { Issue } from "./issue.service";
import { IssueMetadata } from "./issue-metadata";
import { Button } from "@/components/ui/button";
import { useLocale } from "@/providers/locale-provider";
import { useIssueHistory } from "./queries";
import { LoadNotice, Section } from "./issue-ui";
export function IssueHistory({
  workspaceId,
  issueId,
  enabled,
}: {
  workspaceId: string;
  issueId: string;
  enabled: boolean;
}) {
  const { t, locale } = useLocale();
  const history = useIssueHistory(workspaceId, issueId, enabled);
  return (
    <Section title={t("issues.history")}>
      <LoadNotice
        pending={enabled && history.isPending}
        error={history.isError}
        retry={() => history.refetch()}
      />
      {enabled && history.isSuccess && !history.isError && (
        <>
          <ol className="divide-y">
            {history.data.pages
              .flatMap((page) => page.items ?? [])
              .map((item) => (
                <li key={item.id} className="space-y-2 py-4">
                  <p className="font-medium">
                    {t(
                      issueEventKeys[item.event_name] ?? "issues.event_changed",
                    )}
                  </p>
                  <p className="break-words">
                    {item.issue.identifier} · {item.issue.title}
                  </p>
                  <HistorySnapshot
                    workspaceId={workspaceId}
                    issue={item.issue}
                    enabled={enabled}
                  />
                  <p className="text-muted-foreground text-sm">
                    {t("issues.revision", { version: item.version })} ·{" "}
                    <time dateTime={item.occurred_at}>
                      {new Intl.DateTimeFormat(locale, {
                        dateStyle: "medium",
                        timeStyle: "short",
                      }).format(new Date(item.occurred_at))}
                    </time>
                  </p>
                </li>
              ))}
          </ol>
          {history.data.pages.every((page) => !page.items?.length) && (
            <p>{t("issues.no_history")}</p>
          )}
          {history.hasNextPage && (
            <Button
              type="button"
              variant="outline"
              disabled={history.isFetchingNextPage}
              onClick={() => history.fetchNextPage()}
            >
              {t("issues.load_more")}
            </Button>
          )}
        </>
      )}
    </Section>
  );
}

function HistorySnapshot({
  workspaceId,
  issue,
  enabled,
}: {
  workspaceId: string;
  issue: Issue;
  enabled: boolean;
}) {
  const { t } = useLocale();
  const [open, setOpen] = useState(false);
  return (
    <details onToggle={(event) => setOpen(event.currentTarget.open)}>
      <summary className="cursor-pointer text-sm underline">
        {t("issues.view_snapshot")}
      </summary>
      {open && (
        <div className="mt-3">
          <IssueMetadata
            workspaceId={workspaceId}
            issue={issue}
            enabled={enabled}
          />
        </div>
      )}
    </details>
  );
}
