import { createOperationStore } from "../workspaces/operation-state.mjs";
export type { PendingOperation as PendingIssueOperation } from "../workspaces/operation-state.mjs";
export {
  operationScope as issueOperationScope,
  operationFailure,
} from "../workspaces/operation-state.mjs";
export const issueOperationStore = createOperationStore();
