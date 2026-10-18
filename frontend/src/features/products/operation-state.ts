import { createOperationStore } from "../workspaces/operation-state.mjs";
export type { PendingOperation as PendingProductOperation } from "../workspaces/operation-state.mjs";
export {
  operationScope as productOperationScope,
  operationFailure,
} from "../workspaces/operation-state.mjs";
export const productOperationStore = createOperationStore();
