import { createViewOperationStore } from "../searchviews/operation-state.ts";
import {
  createQuickDraftStore,
  type QuickIssueContext,
} from "./draft-state.ts";
import type { QuickCommand } from "./commands";
const baseOperations = createViewOperationStore<QuickCommand>();
export const quickOperations = {
  ...baseOperations,
  discard(id: string) {
    const entry = baseOperations.get(id);
    if (entry && quickDrafts.get(entry.target)?.uncertain)
      throw new Error("QUICK_UNCERTAIN_RETAINED");
    baseOperations.discard(id);
  },
};
export const quickDrafts = createQuickDraftStore((id) =>
  [...quickOperations.snapshot().values()].some((entry) => entry.target === id),
);
let active: (QuickIssueContext & { draftID: string | null }) | null = null;
let generation = 0;
export function activateQuickContext(
  actorID?: string,
  workspaceID?: string,
  draftID: string | null = null,
) {
  const next =
    actorID && workspaceID
      ? {
          actorID: actorID.toLowerCase(),
          workspaceID: workspaceID.toLowerCase(),
          draftID,
        }
      : null;
  if (JSON.stringify(active) === JSON.stringify(next)) return generation;
  for (const entry of quickOperations.snapshot().values())
    if (entry.pending) quickDrafts.markUncertain(entry.target);
  active = next;
  generation++;
  quickOperations.activate(actorID, workspaceID);
  return generation;
}
export function quickContextTicket() {
  return generation;
}
export function quickContextMatches(
  scope: QuickIssueContext,
  draftID: string,
  ticket: number,
) {
  return (
    ticket === generation &&
    active?.actorID === scope.actorID.toLowerCase() &&
    active.workspaceID === scope.workspaceID.toLowerCase() &&
    active.draftID === draftID
  );
}

export function quickWorkspaceMatches(scope: QuickIssueContext) {
  return (
    active?.actorID === scope.actorID.toLowerCase() &&
    active.workspaceID === scope.workspaceID.toLowerCase()
  );
}
