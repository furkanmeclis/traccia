"use client";
import { Button } from "@/components/ui/button";
import { useLocale } from "@/providers/locale-provider";
import { Section, LoadNotice } from "@/features/issues/issue-ui";
import { useRelationHistory } from "./queries";
import { relationEventKeys } from "./copy";
import { IssueReferenceLink } from "./reference";
export function RelationHistory({
  workspaceId,
  issueId,
  enabled,
}: {
  workspaceId: string;
  issueId: string;
  enabled: boolean;
}) {
  const { t, locale } = useLocale();
  const history = useRelationHistory(workspaceId, issueId, enabled);
  return (
    <Section title={t("relations.history")}>
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
                <li key={item.id} className="space-y-3 py-4">
                  <p className="font-medium">
                    {t(
                      relationEventKeys[item.event_name] ?? "relations.changed",
                    )}{" "}
                    · {t("relations.revision", { version: item.version })}
                  </p>
                  <IssueReferenceLink reference={item.snapshot.state.issue} />
                  <p className="text-muted-foreground text-sm">
                    <time dateTime={item.occurred_at}>
                      {new Intl.DateTimeFormat(locale, {
                        dateStyle: "medium",
                        timeStyle: "short",
                      }).format(new Date(item.occurred_at))}
                    </time>
                  </p>
                  <details>
                    <summary className="cursor-pointer text-sm underline">
                      {t("relations.view_snapshot")}
                    </summary>
                    <div className="mt-3 space-y-3">
                      {(
                        [
                          ["parent", item.snapshot.state.parent],
                          ["duplicate_of", item.snapshot.state.duplicate_of],
                          [
                            "canonical",
                            item.snapshot.state.canonical_duplicate,
                          ],
                        ] as const
                      ).map(([label, reference]) => (
                        <p className="break-words" key={label}>
                          <span>{t("relations." + label)}: </span>
                          {reference ? (
                            <IssueReferenceLink reference={reference} />
                          ) : (
                            t("relations.none")
                          )}
                        </p>
                      ))}
                      <ul className="space-y-3">
                        {item.snapshot.changed_relations?.map((relation) => (
                          <li
                            className="space-y-1 rounded-md border p-3"
                            key={relation.id}
                          >
                            <p>
                              {t(
                                relation.removed
                                  ? "relations.snapshot_before"
                                  : "relations.snapshot_after",
                              )}{" "}
                              ·{" "}
                              {t(
                                "relations." +
                                  (relation.kind === "duplicate"
                                    ? "duplicate_of"
                                    : relation.kind),
                              )}
                            </p>
                            <IssueReferenceLink reference={relation.source} />
                            <p>→</p>
                            <IssueReferenceLink reference={relation.target} />
                          </li>
                        ))}
                      </ul>
                      <ul className="space-y-2">
                        {item.snapshot.affected_issues?.map((reference) => (
                          <li className="break-words" key={reference.id}>
                            <IssueReferenceLink reference={reference} /> ·{" "}
                            {t("relations.revision", {
                              version: reference.relation_version,
                            })}
                          </li>
                        ))}
                      </ul>
                    </div>
                  </details>
                </li>
              ))}
          </ol>
          {history.data.pages.every((page) => !page.items?.length) && (
            <p>{t("relations.no_history")}</p>
          )}
          {history.hasNextPage && (
            <Button
              type="button"
              variant="outline"
              disabled={history.isFetchingNextPage}
              onClick={() => history.fetchNextPage()}
            >
              {t("relations.history_more")}
            </Button>
          )}
        </>
      )}
    </Section>
  );
}
