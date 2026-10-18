"use client";
import { useDrawerDraft } from "@/features/issue-drawer/draft-context";
import { useAuth } from "@/providers/auth-provider";
import { useLocale } from "@/providers/locale-provider";
import { Button } from "@/components/ui/button";
import { Section, LoadNotice } from "@/features/issues/issue-ui";
import { useWorkspace } from "@/features/workspaces/queries";
import { useRelations } from "./queries";
import type { Relation, RelationState } from "./relation.service";
import { RelationForm } from "./relation-form";
import { expectedVersions } from "./snapshot";
import {
  RelationOperationNotice,
  useRelationOperation,
  RelationPendingOperations,
} from "./relation-operation";
import { RelationHistory } from "./relation-history";
import { IssueReferenceLink } from "./reference";
function RelationRow({
  workspaceId,
  relation,
  currentId,
  ready,
  unlink,
}: {
  workspaceId: string;
  relation: Relation;
  currentId: string;
  ready: boolean;
  unlink: boolean;
}) {
  const { t } = useLocale();
  const other =
    relation.source.id === currentId ? relation.target : relation.source;
  const operation = useRelationOperation(
    workspaceId,
    relation.source.id,
    "remove:" + relation.id,
    t("relations.unlink", { identifier: other.identifier }),
  );
  return (
    <li className="space-y-3 rounded-md border p-3">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <IssueReferenceLink reference={other} />
        {unlink && (
          <Button
            type="button"
            variant="outline"
            size="sm"
            className="h-auto max-w-full text-left break-words whitespace-normal"
            disabled={!ready || operation.locked}
            onClick={() => {
              const action =
                relation.kind === "parent"
                  ? "parent"
                  : relation.kind === "duplicate"
                    ? "duplicate"
                    : relation.kind === "blocks"
                      ? "blocks-unlink"
                      : "related-unlink";
              const path = {
                workspaceID: workspaceId,
                issueID: relation.source.id,
              };
              const expected_versions = expectedVersions([
                relation.source,
                relation.target,
              ]);
              if (action === "parent" || action === "duplicate")
                operation.submit({
                  action,
                  path,
                  body: { target_issue_id: null, expected_versions },
                });
              else
                operation.submit({
                  action,
                  path,
                  body: {
                    target_issue_id: relation.target.id,
                    expected_versions,
                  },
                });
            }}
          >
            {t("relations.unlink", { identifier: other.identifier })}
          </Button>
        )}
      </div>
      {operation.entry && (
        <RelationOperationNotice
          operationKey={operation.operationKey}
          entry={operation.entry}
        />
      )}
    </li>
  );
}
const groups = [
  "parent",
  "children",
  "blocks",
  "blocked_by",
  "related",
  "duplicate_of",
  "duplicates",
] as const;
function groupOf(relation: Relation, issueId: string) {
  if (relation.kind === "parent")
    return relation.source.id === issueId ? "parent" : "children";
  if (relation.kind === "blocks")
    return relation.source.id === issueId ? "blocks" : "blocked_by";
  if (relation.kind === "duplicate")
    return relation.source.id === issueId ? "duplicate_of" : "duplicates";
  return "related";
}
function RelationSettings({
  workspaceId,
  state,
  ready,
}: {
  workspaceId: string;
  state: RelationState;
  ready: boolean;
}) {
  const { t } = useLocale();
  const [editing, setEditing] = useDrawerDraft<
    "parent" | "duplicate" | "add" | null
  >("relations.editing", null);
  const [generation, setGeneration] = useDrawerDraft("relations.generation", 0);
  return editing ? (
    <RelationForm
      key={generation}
      draftGeneration={generation}
      workspaceId={workspaceId}
      state={state}
      mode={editing}
      ready={ready}
      onDone={() => {
        setEditing(null);
        setGeneration((n) => n + 1);
      }}
    />
  ) : state.capabilities.link ? (
    <div className="flex flex-wrap gap-3">
      {(["parent", "duplicate", "add"] as const).map((mode) => (
        <Button
          type="button"
          variant="outline"
          disabled={!ready}
          key={mode}
          onClick={() => setEditing(mode)}
        >
          {t(
            mode === "parent"
              ? "relations.set_parent"
              : mode === "duplicate"
                ? "relations.set_duplicate"
                : "relations.add",
          )}
        </Button>
      ))}
    </div>
  ) : null;
}
export function IssueRelations({
  workspaceId,
  issueId,
  metadataVersion,
}: {
  workspaceId: string;
  issueId: string;
  metadataVersion: number;
}) {
  const { t } = useLocale();
  const { user } = useAuth();
  const workspace = useWorkspace(workspaceId);
  const allowed = workspace.isSuccess && !workspace.isError;
  const relations = useRelations(
    workspaceId,
    issueId,
    allowed,
    metadataVersion,
  );
  const ready =
    allowed &&
    !workspace.isFetching &&
    relations.isSuccess &&
    !relations.isError &&
    !relations.isFetching;
  return (
    <>
      <RelationPendingOperations workspaceId={workspaceId} />
      <Section title={t("relations.title")}>
        <LoadNotice
          pending={relations.isPending}
          error={relations.isError || workspace.isError}
          retry={() => {
            void workspace.refetch();
            void relations.refetch();
          }}
        />
        {relations.data && (
          <div hidden={!allowed || relations.isError}>
            <Button
              type="button"
              variant="ghost"
              disabled={!ready}
              onClick={() => relations.refetch()}
            >
              {t("relations.review")}
            </Button>
            {relations.data.state.canonical_duplicate && (
              <p className="space-x-2 break-words">
                <span>{t("relations.canonical")}:</span>
                <IssueReferenceLink
                  reference={relations.data.state.canonical_duplicate}
                />
              </p>
            )}
            {groups.map((group) => {
              const rows = relations.data.items.filter(
                (relation) => groupOf(relation, issueId) === group,
              );
              return (
                <section className="space-y-3" key={group}>
                  <h3 className="font-medium">{t("relations." + group)}</h3>
                  {rows.length ? (
                    <ul className="space-y-3">
                      {rows.map((relation) => (
                        <RelationRow
                          key={relation.id}
                          workspaceId={workspaceId}
                          relation={relation}
                          currentId={issueId}
                          ready={ready}
                          unlink={relations.data.state.capabilities.unlink}
                        />
                      ))}
                    </ul>
                  ) : (
                    <p className="text-muted-foreground text-sm">
                      {t("relations.none")}
                    </p>
                  )}
                </section>
              );
            })}
            <RelationSettings
              key={JSON.stringify([workspaceId, user?.uuid, issueId])}
              workspaceId={workspaceId}
              state={relations.data.state}
              ready={ready}
            />
          </div>
        )}
      </Section>
      <RelationHistory
        workspaceId={workspaceId}
        issueId={issueId}
        enabled={allowed && relations.isSuccess && !relations.isError}
      />
    </>
  );
}
