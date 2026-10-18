import { createOperationStore } from "../workspaces/operation-state.mjs";
export type { PendingOperation as PendingProjectOperation } from "../workspaces/operation-state.mjs";
export {
  operationScope as projectOperationScope,
  operationFailure,
} from "../workspaces/operation-state.mjs";
export const projectOperationStore = createOperationStore();
