import {
  prepareSubmission,
  type OperationSubmission,
} from "../workspaces/operation-submission.ts";
export type ViewOperationScope = {
  actorID: string;
  workspaceID: string;
  target: string;
};
export type ViewAttempt = {
  key: string;
  attempt: number;
  generation: number;
  signal: AbortSignal;
};
export type ViewOperation<T> = ViewOperationScope & {
  id: string;
  label: string;
  submission: OperationSubmission<T>;
  attempt: number;
  pending: boolean;
  outcome: "uncertain" | "rejected";
  retryable: boolean;
  reviewed: boolean;
  reviewing: boolean;
  canRetry: boolean;
  reviewError: boolean;
  error: string | null;
};
const same = (a?: string, b?: string) => a?.toLowerCase() === b?.toLowerCase();
export function viewOperationKey(scope: ViewOperationScope, action: string) {
  return JSON.stringify([
    scope.actorID.toLowerCase(),
    scope.workspaceID.toLowerCase(),
    scope.target.toLowerCase(),
    action,
  ]);
}
export function createViewOperationStore<T>() {
  let entries: ReadonlyMap<string, ViewOperation<T>> = new Map(),
    generation = 0,
    activeActor: string | undefined,
    activeWorkspace: string | undefined;
  const empty: ReadonlyMap<string, ViewOperation<T>> = new Map(),
    listeners = new Set<() => void>(),
    controllers = new Map<string, AbortController>();
  const publish = (id: string, entry: ViewOperation<T> | null) => {
    const next = new Map(entries);
    if (entry) next.set(id, entry);
    else next.delete(id);
    entries = next;
    listeners.forEach((fn) => fn());
  };
  const current = (entry: ViewOperation<T>) =>
    same(entry.actorID, activeActor) &&
    same(entry.workspaceID, activeWorkspace);
  function patch(
    id: string,
    update: Partial<
      Omit<ViewOperation<T>, "submission" | "id" | keyof ViewOperationScope>
    >,
  ) {
    const entry = entries.get(id);
    if (entry) publish(id, { ...entry, ...update });
  }
  function matches(id: string, token: ViewAttempt) {
    const entry = entries.get(id);
    return (
      !!entry &&
      token.generation === generation &&
      entry.submission.key === token.key &&
      entry.attempt === token.attempt &&
      controllers.get(id)?.signal === token.signal
    );
  }
  return {
    subscribe(fn: () => void) {
      listeners.add(fn);
      return () => {
        listeners.delete(fn);
      };
    },
    snapshot: () => entries,
    serverSnapshot: () => empty,
    get: (id: string) => entries.get(id),
    activate(actorID?: string, workspaceID?: string) {
      if (same(actorID, activeActor) && same(workspaceID, activeWorkspace))
        return;
      activeActor = actorID;
      activeWorkspace = workspaceID;
      generation++;
      controllers.forEach((controller) => controller.abort());
      controllers.clear();
      for (const [id, entry] of entries)
        publish(id, {
          ...entry,
          pending: false,
          outcome: entry.pending ? "uncertain" : entry.outcome,
          reviewed: false,
          reviewing: false,
          canRetry: false,
        });
    },
    begin(scope: ViewOperationScope, action: string, label: string, input: T) {
      const id = viewOperationKey(scope, action);
      if (
        [...entries.values()].some(
          (entry) =>
            same(entry.actorID, scope.actorID) &&
            same(entry.workspaceID, scope.workspaceID) &&
            same(entry.target, scope.target),
        )
      )
        throw new Error("VIEW_REVIEW_REQUIRED");
      const submission = prepareSubmission<T>(null, input, () => input);
      const entry: ViewOperation<T> = {
        ...scope,
        id,
        label,
        submission,
        attempt: 1,
        pending: true,
        outcome: "uncertain",
        retryable: true,
        reviewed: false,
        reviewing: false,
        canRetry: false,
        reviewError: false,
        error: null,
      };
      if (!current(entry)) throw new Error("VIEW_CONTEXT_CHANGED");
      controllers.set(id, new AbortController());
      publish(id, entry);
      return entry;
    },
    signal: (id: string) => controllers.get(id)?.signal,
    ticket(id: string): ViewAttempt {
      const entry = entries.get(id),
        signal = controllers.get(id)?.signal;
      if (!entry || !signal || !current(entry))
        throw new Error("VIEW_CONTEXT_CHANGED");
      return {
        key: entry.submission.key,
        attempt: entry.attempt,
        generation,
        signal,
      };
    },
    success(id: string, token: ViewAttempt) {
      if (!matches(id, token)) return false;
      controllers.delete(id);
      publish(id, null);
      return true;
    },
    failed(id: string, token: ViewAttempt, error: string, status?: number) {
      if (!matches(id, token)) return false;
      controllers.delete(id);
      patch(id, {
        pending: false,
        outcome:
          status !== undefined && status < 500 && status !== 408
            ? "rejected"
            : "uncertain",
        retryable: status !== 400 && status !== 409,
        reviewed: false,
        reviewing: false,
        canRetry: false,
        reviewError: false,
        error,
      });
      return true;
    },
    async review(
      id: string,
      read: (entry: ViewOperation<T>) => Promise<boolean>,
    ) {
      const entry = entries.get(id),
        token = generation;
      if (!entry || entry.pending || entry.reviewing || !current(entry))
        return false;
      patch(id, {
        reviewed: false,
        reviewing: true,
        canRetry: false,
        reviewError: false,
      });
      try {
        const allowed = await read(entry);
        const latest = entries.get(id);
        if (
          token !== generation ||
          !latest ||
          !current(latest) ||
          latest.submission.key !== entry.submission.key ||
          latest.attempt !== entry.attempt
        )
          return false;
        patch(id, {
          reviewed: true,
          reviewing: false,
          canRetry: allowed && latest.retryable,
          reviewError: false,
        });
        return allowed && latest.retryable;
      } catch (error) {
        const latest = entries.get(id);
        if (
          token === generation &&
          latest?.submission.key === entry.submission.key &&
          latest.attempt === entry.attempt
        )
          patch(id, {
            reviewed: false,
            reviewing: false,
            canRetry: false,
            reviewError: true,
            error: error instanceof Error ? error.message : String(error),
          });
        return false;
      }
    },
    retry(id: string) {
      const entry = entries.get(id);
      if (
        !entry ||
        !current(entry) ||
        entry.pending ||
        entry.reviewing ||
        !entry.reviewed ||
        !entry.canRetry ||
        !entry.retryable
      )
        throw new Error("VIEW_REVIEW_REQUIRED");
      controllers.set(id, new AbortController());
      patch(id, {
        attempt: entry.attempt + 1,
        pending: true,
        outcome: "uncertain",
        reviewed: false,
        canRetry: false,
        reviewError: false,
        error: null,
      });
      return structuredClone(entry.submission);
    },
    discard(id: string) {
      const entry = entries.get(id);
      if (
        !entry ||
        !current(entry) ||
        entry.pending ||
        entry.reviewing ||
        !entry.reviewed
      )
        throw new Error("VIEW_REVIEW_REQUIRED");
      publish(id, null);
    },
  };
}
