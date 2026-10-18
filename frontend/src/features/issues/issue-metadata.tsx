"use client";
import { issueFieldKeys, issuePriorityKeys } from "./issue-copy";
import { useLocale } from "@/providers/locale-provider";
import { useIssueReferences, useIssueSelection } from "./queries";
import { LoadNotice, Section } from "./issue-ui";
import type { Issue } from "./issue.service";
export function IssueMetadata({
  workspaceId,
  issue,
  enabled = true,
}: {
  workspaceId: string;
  issue: Issue;
  enabled?: boolean;
}) {
  const { t } = useLocale();
  const references = useIssueReferences(workspaceId, enabled);
  const selection = useIssueSelection(
    workspaceId,
    issue.team_id,
    issue.project_id ?? "",
    enabled,
  );
  const member = references.data?.members.find(
    (member) => member.id === issue.assignee_id,
  );
  const name = (value?: string | null) => value || t("issues.unassigned");
  return (
    <Section title={t("issues.metadata")}>
      <LoadNotice
        pending={references.isPending || selection.isPending}
        error={references.isError || selection.isError}
        retry={() => {
          void references.refetch();
          void selection.refetch();
        }}
      />
      <dl
        hidden={
          !references.isSuccess ||
          references.isError ||
          !selection.isSuccess ||
          selection.isError
        }
        className="grid gap-4 text-sm sm:grid-cols-2"
      >
        {[
          [
            "team_id",
            name(
              references.data?.teams.find((team) => team.id === issue.team_id)
                ?.name ?? t("issues.unavailable_reference"),
            ),
          ],
          [
            "status_id",
            name(
              selection.data?.statuses.find(
                (status) => status.id === issue.status_id,
              )?.name ?? t("issues.unavailable_reference"),
            ),
          ],
          ["priority", t(issuePriorityKeys[issue.priority])],
          [
            "lifecycle",
            t(issue.archived ? "issues.archived" : "issues.active"),
          ],
          [
            "assignee_id",
            issue.assignee_id
              ? member
                ? `${member.name} ${member.surname}`.trim()
                : t("issues.unavailable_reference")
              : t("issues.unassigned"),
          ],
          [
            "project_id",
            issue.project_id
              ? (references.data?.projects.find(
                  (project) => project.id === issue.project_id,
                )?.name ?? t("issues.unavailable_reference"))
              : t("issues.unassigned"),
          ],
          [
            "milestone_id",
            issue.milestone_id
              ? (selection.data?.project?.milestones.find(
                  (milestone) => milestone.id === issue.milestone_id,
                )?.name ?? t("issues.unavailable_reference"))
              : t("issues.unassigned"),
          ],
          ["start_date", name(issue.start_date)],
          ["target_date", name(issue.target_date)],
          [
            "effort",
            issue.effort == null
              ? t("issues.unassigned")
              : String(issue.effort),
          ],
          [
            "labels",
            (issue.label_ids ?? [])
              .map(
                (id) =>
                  selection.data?.labels.find((label) => label.id === id)
                    ?.name ?? t("issues.unavailable_reference"),
              )
              .join(", ") || t("issues.unassigned"),
          ],
        ].map(([field, value]) => (
          <div key={field}>
            <dt className="text-muted-foreground">
              {t(issueFieldKeys[field])}
            </dt>
            <dd className="break-words">{value}</dd>
          </div>
        ))}
      </dl>
    </Section>
  );
}
