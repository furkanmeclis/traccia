import {
  createOperationStore,
  operationFailure as sharedOperationFailure,
  type PendingOperation,
} from "../workspaces/operation-state.mjs";
export type { PendingOperation as PendingRelationOperation } from "../workspaces/operation-state.mjs";
export { operationScope as relationOperationScope } from "../workspaces/operation-state.mjs";
export const relationOperationStore = createOperationStore();
// Graph conflicts also cover cycles, existing edges and inactive endpoints.
export function operationFailure(
  previous: PendingOperation,
  code?: string,
  status?: number,
) {
  const failure = sharedOperationFailure(previous, code, status);
  return { ...failure, conflict: failure.conflict || code === "CONFLICT" };
}
