import {
  createOperationStore,
  operationFailure as sharedOperationFailure,
  type PendingOperation,
} from "../workspaces/operation-state.mjs";
export type { PendingOperation as PendingDocumentOperation } from "../workspaces/operation-state.mjs";
export { operationScope as documentOperationScope } from "../workspaces/operation-state.mjs";
export const documentOperationStore = createOperationStore();
export function operationFailure(
  previous: PendingOperation,
  code?: string,
  status?: number,
) {
  const failure = sharedOperationFailure(previous, code, status);
  return { ...failure, conflict: failure.conflict || code === "CONFLICT" };
}
