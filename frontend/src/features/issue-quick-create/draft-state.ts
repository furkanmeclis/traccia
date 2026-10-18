import type { IssueFormSnapshot } from "../issues/issue-form";
import type { Element } from "../documents/shared/contract";

export type QuickIssueContext = { actorID: string; workspaceID: string };
export type QuickIssueCreated = QuickIssueContext & {
  draftID: string;
  issueID: string;
};
export type QuickIssueDraft = QuickIssueContext & {
  id: string;
  revision: number;
  metadata: IssueFormSnapshot;
  descriptionEnabled: boolean;
  content: Element[];
  editorError: string | null;
  uncertain: boolean;
  createdIssueID: string | null;
};
export const quickScope = (scope: QuickIssueContext) =>
  JSON.stringify([
    scope.actorID.toLowerCase(),
    scope.workspaceID.toLowerCase(),
  ]);
export function initialMetadata(
  initialTeamID = "",
  initialProjectID = "",
): IssueFormSnapshot {
  return {
    values: {
      title: "",
      priority: "none",
      team_id: initialTeamID,
      status_id: "",
      assignee_id: "",
      project_id: initialProjectID,
      milestone_id: "",
      start_date: "",
      target_date: "",
      effort: "",
      label_ids: [],
    },
  };
}
export function createQuickDraftStore(
  locked: (id: string) => boolean = () => false,
  newID: () => string = () => crypto.randomUUID(),
) {
  let entries: ReadonlyMap<string, QuickIssueDraft> = new Map();
  const active = new Map<string, string>(),
    empty: ReadonlyMap<string, QuickIssueDraft> = new Map(),
    listeners = new Set<() => void>();
  function publish(id: string, value: QuickIssueDraft | null) {
    const next = new Map(entries);
    if (value) next.set(id, value);
    else next.delete(id);
    entries = next;
    listeners.forEach((listener) => listener());
  }
  function current(scope: QuickIssueContext, id: string) {
    const value = entries.get(id);
    return value && quickScope(value) === quickScope(scope) ? value : undefined;
  }
  return {
    subscribe(listener: () => void) {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    },
    snapshot: () => entries,
    serverSnapshot: () => empty,
    get: (id: string) => entries.get(id),
    active(scope: QuickIssueContext) {
      return entries.get(active.get(quickScope(scope)) ?? "");
    },
    ensure(
      scope: QuickIssueContext,
      metadata: IssueFormSnapshot,
      content: Element[],
    ) {
      const prior = this.active(scope);
      if (prior) return prior;
      const draft: QuickIssueDraft = {
        ...scope,
        id: newID(),
        revision: 0,
        metadata: structuredClone(metadata),
        descriptionEnabled: false,
        content: structuredClone(content),
        editorError: null,
        uncertain: false,
        createdIssueID: null,
      };
      active.set(quickScope(scope), draft.id);
      publish(draft.id, draft);
      return draft;
    },
    patch(
      scope: QuickIssueContext,
      id: string,
      patch: Partial<
        Pick<
          QuickIssueDraft,
          "metadata" | "descriptionEnabled" | "content" | "editorError"
        >
      >,
    ) {
      const prior = current(scope, id);
      if (!prior || locked(id) || prior.createdIssueID) return false;
      publish(id, {
        ...prior,
        ...structuredClone(patch),
        revision: prior.revision + 1,
      });
      return true;
    },
    markUncertain(id: string) {
      const prior = entries.get(id);
      if (prior && !prior.uncertain) publish(id, { ...prior, uncertain: true });
    },
    rememberCreated(scope: QuickIssueContext, id: string, issueID: string) {
      const prior = current(scope, id);
      if (!prior) return false;
      publish(id, { ...prior, createdIssueID: issueID, uncertain: false });
      return true;
    },
    complete(scope: QuickIssueContext, id: string, submittedRevision: number) {
      const prior = current(scope, id);
      if (!prior || prior.revision !== submittedRevision) return false;
      if (active.get(quickScope(scope)) === id)
        active.delete(quickScope(scope));
      publish(id, null);
      return true;
    },
    discard(scope: QuickIssueContext, id: string) {
      const prior = current(scope, id);
      if (!prior || locked(id) || prior.uncertain) return false;
      if (active.get(quickScope(scope)) === id)
        active.delete(quickScope(scope));
      publish(id, null);
      return true;
    },
  };
}
