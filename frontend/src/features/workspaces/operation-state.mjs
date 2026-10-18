/** @typedef {Object} PendingOperation
 * @property {string} scope
 * @property {string} label
 * @property {import("./operation-submission").OperationSubmission<unknown>} submission
 * @property {boolean} pending
 * @property {boolean} uncertain
 * @property {boolean} conflict
 * @property {boolean} validationRejected
 * @property {boolean} reviewed
 * @property {boolean} reviewing
 * @property {boolean} reviewError
 * @property {() => void} retry
 * @property {() => void} review
 */
// Each domain owns a store. Memory survives row filtering and remounts only.
export function createOperationStore() {
  /** @type {ReadonlyMap<string, PendingOperation>} */
  let snapshot = new Map();
  /** @type {ReadonlyMap<string, PendingOperation>} */
  const empty = new Map();
  /** @type {Set<() => void>} */
  const listeners = new Set();
  return {
    /** @param {() => void} listener */
    subscribe(listener) {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    },
    snapshot: () => snapshot,
    serverSnapshot: () => empty,
    /** @param {string} key */
    get: (key) => snapshot.get(key),
    /** @param {string} key @param {PendingOperation} value */
    set(key, value) {
      snapshot = new Map(snapshot).set(key, value);
      listeners.forEach((listener) => listener());
    },
    /** @param {string} key @param {Partial<PendingOperation>} update */
    patch(key, update) {
      const value = snapshot.get(key);
      if (value) this.set(key, { ...value, ...update });
    },
    /** @param {string} key */
    clear(key) {
      const next = new Map(snapshot);
      next.delete(key);
      snapshot = next;
      listeners.forEach((listener) => listener());
    },
  };
}
/** @param {string | undefined} actor @param {string} workspace @param {string} [aggregate] */
export function operationScope(actor, workspace, aggregate) {
  return JSON.stringify([actor, workspace, aggregate ?? null]);
}
/** @param {PendingOperation} previous @param {string} [code] @param {number} [status] */
export function operationFailure(previous, code, status) {
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
