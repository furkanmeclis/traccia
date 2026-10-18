import type { OperationSubmission } from "../workspaces/operation-submission";
export type PendingProjectOperation = {
  scope: string;
  label: string;
  submission: OperationSubmission<unknown>;
  pending: boolean;
  uncertain: boolean;
  conflict: boolean;
  validationRejected: boolean;
  reviewed: boolean;
  reviewing: boolean;
  reviewError: boolean;
  retry: () => void;
  review: () => void;
};
// In-memory operation ownership survives row filtering, authorization read errors
// and component remounts. Actor/workspace/project scope prevents cross-user reuse.
let snapshot: ReadonlyMap<string, PendingProjectOperation> = new Map();
const listeners = new Set<() => void>();
export const projectOperationStore = {
  subscribe(listener: () => void) {
    listeners.add(listener);
    return () => {
      listeners.delete(listener);
    };
  },
  snapshot: () => snapshot,
  serverSnapshot: () => empty,
  get: (key: string) => snapshot.get(key),
  set(key: string, value: PendingProjectOperation) {
    snapshot = new Map(snapshot).set(key, value);
    listeners.forEach((listener) => listener());
  },
  patch(key: string, update: Partial<PendingProjectOperation>) {
    const value = snapshot.get(key);
    if (value) this.set(key, { ...value, ...update });
  },
  clear(key: string) {
    const next = new Map(snapshot);
    next.delete(key);
    snapshot = next;
    listeners.forEach((listener) => listener());
  },
};
const empty: ReadonlyMap<string, PendingProjectOperation> = new Map();
export function projectOperationScope(
  actor: string | undefined,
  workspace: string,
  project?: string,
) {
  return JSON.stringify([actor, workspace, project ?? null]);
}
export function operationFailure(
  previous: PendingProjectOperation,
  code?: string,
  status?: number,
): Pick<
  PendingProjectOperation,
  | "pending"
  | "uncertain"
  | "conflict"
  | "validationRejected"
  | "reviewed"
  | "reviewError"
> {
  return {
    pending: false,
    reviewed: false,
    reviewError: false,
    uncertain:
      previous.uncertain ||
      status === undefined ||
      status >= 500 ||
      status === 408,
    conflict: code === "VERSION_CONFLICT" || code === "IDEMPOTENCY_CONFLICT",
    validationRejected: code === "VALIDATION_ERROR" && !previous.uncertain,
  };
}
