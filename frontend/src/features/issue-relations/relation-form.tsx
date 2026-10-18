"use client";
import { useId, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useAuth } from "@/providers/auth-provider";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { useLocale } from "@/providers/locale-provider";
import { LoadNotice } from "@/features/issues/issue-ui";
import {
  useRelationCandidates,
  useRelationState,
  relationKeys,
} from "./queries";
import {
  relationService,
  type IssueReference,
  type RelationState,
  type RelationAction,
} from "./relation.service";
import {
  useRelationOperation,
  RelationOperationNotice,
} from "./relation-operation";
import { expectedVersions } from "./snapshot";
export function RelationForm({
  workspaceId,
  state,
  mode,
  ready,
  onDone,
}: {
  workspaceId: string;
  state: RelationState;
  mode: "parent" | "duplicate" | "add";
  ready: boolean;
  onDone: () => void;
}) {
  const { t } = useLocale();
  const id = useId();
  const cache = useQueryClient();
  const { user } = useAuth();
  const selectionSequence = useRef(0);
  const [baseline] = useState(state);
  const previous =
    mode === "parent"
      ? baseline.parent
      : mode === "duplicate"
        ? baseline.duplicate_of
        : null;
  const [targetID, setTargetID] = useState(previous?.id ?? "");
  const [target, setTarget] = useState<IssueReference | null>(previous);
  const [selection, setSelection] = useState({ pending: false, error: false });
  const [action, setAction] = useState<RelationAction>(
    mode === "parent"
      ? "parent"
      : mode === "duplicate"
        ? "duplicate"
        : "blocks-link",
  );
  const candidates = useRelationCandidates(workspaceId, true);
  const operation = useRelationOperation(
    workspaceId,
    baseline.issue.id,
    "form:" + mode,
    t(
      mode === "parent"
        ? "relations.set_parent"
        : mode === "duplicate"
          ? "relations.set_duplicate"
          : "relations.add",
    ),
    onDone,
  );
  const currentTarget = useRelationState(workspaceId, targetID, !!targetID);
  const currentPrevious =
    mode === "parent"
      ? state.parent
      : mode === "duplicate"
        ? state.duplicate_of
        : null;
  const stale =
    previous?.id !== currentPrevious?.id ||
    previous?.relation_version !== currentPrevious?.relation_version ||
    state.issue.relation_version !== baseline.issue.relation_version ||
    (!!target &&
      currentTarget.isSuccess &&
      currentTarget.data.issue.relation_version !== target.relation_version);
  const loaded =
    candidates.isSuccess &&
    !candidates.isError &&
    !candidates.isFetching &&
    !selection.pending &&
    !selection.error &&
    (!targetID ||
      (currentTarget.isSuccess &&
        !currentTarget.isError &&
        !currentTarget.isFetching));
  const editable =
    ready && state.capabilities.link && loaded && !stale && !operation.locked;
  const valid =
    !!target &&
    target.active &&
    currentTarget.data?.issue.active &&
    target.id !== baseline.issue.id &&
    (mode === "add" || target.id !== previous?.id);
  async function choose(value: string) {
    const sequence = ++selectionSequence.current;
    setTargetID(value);
    setTarget(null);
    if (!value) {
      setSelection({ pending: false, error: false });
      return;
    }
    setSelection({ pending: true, error: false });
    try {
      const latest = await cache.fetchQuery({
        queryKey: relationKeys.state(workspaceId, user?.uuid, value),
        queryFn: () => relationService.state(workspaceId, value),
        staleTime: 0,
      });
      if (selectionSequence.current === sequence) {
        setTarget(latest.issue);
        setSelection({ pending: false, error: false });
      }
    } catch {
      if (selectionSequence.current === sequence)
        setSelection({ pending: false, error: true });
    }
  }
  return (
    <form
      className="space-y-4"
      onSubmit={(event) => {
        event.preventDefault();
        if (!editable || !valid || !target) return;
        operation.submit({
          action,
          path: { workspaceID: workspaceId, issueID: baseline.issue.id },
          body: {
            target_issue_id: target.id,
            expected_versions: expectedVersions([
              baseline.issue,
              ...(previous ? [previous] : []),
              target,
            ]),
          },
        });
      }}
    >
      <LoadNotice
        pending={
          candidates.isPending ||
          selection.pending ||
          (!!targetID && currentTarget.isPending)
        }
        error={
          candidates.isError ||
          selection.error ||
          (!!targetID && currentTarget.isError)
        }
        retry={() => {
          void candidates.refetch();
          if (targetID) void choose(targetID);
        }}
      />
      <fieldset
        disabled={
          !ready || !state.capabilities.link || stale || operation.locked
        }
        className="space-y-4"
      >
        {mode === "add" && (
          <div className="space-y-2">
            <Label htmlFor={id + "-kind"}>{t("relations.kind")}</Label>
            <select
              id={id + "-kind"}
              className="border-input bg-background h-10 w-full rounded-md border px-3 text-sm"
              value={action}
              onChange={(event) =>
                setAction(event.target.value as RelationAction)
              }
            >
              <option value="blocks-link">{t("relations.blocks")}</option>
              <option value="related-link">{t("relations.related")}</option>
            </select>
          </div>
        )}
        <div className="space-y-2">
          <Label htmlFor={id + "-target"}>
            {t(
              mode === "parent"
                ? "relations.parent"
                : mode === "duplicate"
                  ? "relations.duplicate_of"
                  : "relations.target",
            )}
          </Label>
          <select
            id={id + "-target"}
            required
            className="border-input bg-background h-10 w-full rounded-md border px-3 text-sm"
            disabled={
              !candidates.isSuccess ||
              candidates.isError ||
              candidates.isFetching ||
              selection.pending
            }
            value={targetID}
            onChange={(event) => void choose(event.target.value)}
          >
            <option value="">{t("relations.choose")}</option>
            {targetID &&
              !candidates.data?.some((item) => item.id === targetID) && (
                <option value={targetID} disabled>
                  {previous?.identifier ?? t("relations.inactive")}
                </option>
              )}
            {candidates.data
              ?.filter((item) => item.id !== baseline.issue.id)
              .map((item) => (
                <option key={item.id} value={item.id}>
                  {item.identifier} · {item.title}
                </option>
              ))}
          </select>
        </div>
      </fieldset>
      {target &&
        currentTarget.isSuccess &&
        !currentTarget.data.issue.active && (
          <p role="alert">{t("relations.target_inactive")}</p>
        )}
      {stale && <p role="alert">{t("relations.draft_stale")}</p>}
      <div className="flex flex-wrap gap-3">
        <Button type="submit" disabled={!editable || !valid}>
          {t("relations.save")}
        </Button>
        <Button
          type="button"
          variant="outline"
          disabled={operation.locked}
          onClick={onDone}
        >
          {t(stale ? "relations.discard_draft" : "relations.cancel")}
        </Button>
      </div>
      {operation.entry && (
        <RelationOperationNotice
          operationKey={operation.operationKey}
          entry={operation.entry}
        />
      )}
    </form>
  );
}
