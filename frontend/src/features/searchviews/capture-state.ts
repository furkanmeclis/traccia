export type SearchResource = "issue" | "project" | "team";
export type CaptureScope = {
  actorID: string;
  workspaceID: string;
  resource: SearchResource;
};
export type CaptureSession = {
  id: string;
  request_id: string;
  workspace_id: string;
  resource: SearchResource;
  as_of: string;
  expires_at: string;
  total_count: string;
};
export type CapturedRead<T extends { request_id: string }> = {
  path: string;
  body: T;
};
export type CaptureTicket = {
  requestID: string;
  attempt: number;
  generation: number;
  signal: AbortSignal;
};
export type CaptureEntry<T extends { request_id: string }, P> = CaptureScope & {
  id: string;
  command: CapturedRead<T>;
  attempt: number;
  status:
    "pending" | "ready" | "uncertain" | "rejected" | "expired" | "forbidden";
  error: string | null;
  applied: { command: CapturedRead<T>; page: P } | null;
};
const same = (a?: string, b?: string) => a?.toLowerCase() === b?.toLowerCase();
export function captureScope(scope: CaptureScope) {
  return JSON.stringify([
    scope.actorID.toLowerCase(),
    scope.workspaceID.toLowerCase(),
    scope.resource,
  ]);
}
/** Read-session identity is request_id in the body, never a business receipt key. */
export function createCaptureStore<
  T extends { request_id: string },
  P extends { session: CaptureSession },
>() {
  let entries: ReadonlyMap<string, CaptureEntry<T, P>> = new Map(),
    generation = 0;
  let activeActor: string | undefined, activeWorkspace: string | undefined;
  const empty: ReadonlyMap<string, CaptureEntry<T, P>> = new Map(),
    listeners = new Set<() => void>(),
    controllers = new Map<string, AbortController>();
  const current = (entry: CaptureScope) =>
    same(entry.actorID, activeActor) &&
    same(entry.workspaceID, activeWorkspace);
  function publish(id: string, entry: CaptureEntry<T, P>) {
    entries = new Map(entries).set(id, entry);
    listeners.forEach((fn) => fn());
  }
  function matches(id: string, ticket: CaptureTicket) {
    const entry = entries.get(id);
    return (
      !!entry &&
      current(entry) &&
      ticket.generation === generation &&
      ticket.requestID === entry.command.body.request_id &&
      ticket.attempt === entry.attempt &&
      controllers.get(id)?.signal === ticket.signal
    );
  }
  function start(entry: CaptureEntry<T, P>) {
    controllers.get(entry.id)?.abort();
    controllers.set(entry.id, new AbortController());
    publish(entry.id, entry);
    return structuredClone(entry.command);
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
      controllers.forEach((c) => c.abort());
      controllers.clear();
      for (const [id, entry] of entries)
        if (entry.status === "pending")
          publish(id, { ...entry, status: "uncertain", error: null });
    },
    begin(scope: CaptureScope, build: (requestID: string) => CapturedRead<T>) {
      if (!current(scope)) throw new Error("SEARCH_CONTEXT_CHANGED");
      const id = captureScope(scope),
        requestID = crypto.randomUUID(),
        command = structuredClone(build(requestID));
      if (command.body.request_id !== requestID)
        throw new Error("SEARCH_REQUEST_ID_CHANGED");
      return start({
        ...scope,
        id,
        command,
        attempt: 1,
        status: "pending",
        error: null,
        applied: entries.get(id)?.applied ?? null,
      });
    },
    ticket(id: string): CaptureTicket {
      const entry = entries.get(id),
        signal = controllers.get(id)?.signal;
      if (!entry || !current(entry) || entry.status !== "pending" || !signal)
        throw new Error("SEARCH_CONTEXT_CHANGED");
      return {
        requestID: entry.command.body.request_id,
        attempt: entry.attempt,
        generation,
        signal,
      };
    },
    success(id: string, ticket: CaptureTicket, page: P) {
      if (!matches(id, ticket)) return false;
      const entry = entries.get(id)!;
      if (
        !same(page.session.request_id, entry.command.body.request_id) ||
        !same(page.session.workspace_id, entry.workspaceID) ||
        page.session.resource !== entry.resource
      )
        throw new Error("SEARCH_RESPONSE_SCOPE");
      if (
        typeof page.session.total_count !== "string" ||
        !/^(0|[1-9][0-9]*)$/.test(page.session.total_count)
      )
        throw new Error("SEARCH_INVALID_COUNT");
      controllers.delete(id);
      publish(id, {
        ...entry,
        status: "ready",
        error: null,
        applied: {
          command: structuredClone(entry.command),
          page: structuredClone(page),
        },
      });
      return true;
    },
    failed(
      id: string,
      ticket: CaptureTicket,
      error: string,
      status: Exclude<CaptureEntry<T, P>["status"], "pending" | "ready">,
    ) {
      if (!matches(id, ticket)) return false;
      controllers.delete(id);
      const entry = entries.get(id)!;
      publish(id, { ...entry, status, error });
      return true;
    },
    retry(id: string) {
      const entry = entries.get(id);
      if (!entry || !current(entry) || entry.status !== "uncertain")
        throw new Error("SEARCH_CAPTURE_NOT_RETRYABLE");
      return start({
        ...entry,
        attempt: entry.attempt + 1,
        status: "pending",
        error: null,
      });
    },
    expire(id: string, sessionID: string) {
      const entry = entries.get(id);
      if (
        !entry ||
        !current(entry) ||
        entry.status !== "ready" ||
        !same(entry.applied?.page.session.id, sessionID)
      )
        return false;
      publish(id, { ...entry, status: "expired", error: null });
      return true;
    },
  };
}
