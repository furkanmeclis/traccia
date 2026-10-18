import type { ViewDefinition } from "../searchviews/search.service";
import { samePortfolioDefinition } from "./query-equivalence.ts";
export type PersonalResource = "issue" | "project" | "team";
export type PortfolioScope = {
  actorID: string;
  workspaceID: string;
  resource: PersonalResource;
};
export type PortfolioSessionShape = {
  id: string;
  request_id: string;
  anchor_workspace_id: string;
  workspaces: { id: string; name: string; total_count: string }[];
  resource: PersonalResource;
  as_of: string;
  expires_at: string;
  total_count: string;
};
export type PortfolioRead<
  T extends {
    request_id: string;
    workspace_ids: string[];
    definition: ViewDefinition;
    assigned_to_me: boolean;
  },
> = {
  path: string;
  body: T;
};
export type PortfolioTicket = {
  requestID: string;
  attempt: number;
  generation: number;
  signal: AbortSignal;
};
export type PortfolioEntry<
  T extends {
    request_id: string;
    workspace_ids: string[];
    definition: ViewDefinition;
    assigned_to_me: boolean;
  },
  P,
> = PortfolioScope & {
  id: string;
  command: PortfolioRead<T>;
  attempt: number;
  status:
    | "idle"
    | "pending"
    | "ready"
    | "uncertain"
    | "rejected"
    | "expired"
    | "forbidden"
    | "scope_changed";
  error: string | null;
  applied: { command: PortfolioRead<T>; page: P } | null;
};
const same = (a?: string, b?: string) => a?.toLowerCase() === b?.toLowerCase();
export function portfolioScope(scope: PortfolioScope) {
  return JSON.stringify([
    scope.actorID.toLowerCase(),
    scope.workspaceID.toLowerCase(),
    scope.resource,
  ]);
}
/** Read-session identity is request_id in the body, never a business receipt key. */
export function createPortfolioStore<
  T extends {
    request_id: string;
    workspace_ids: string[];
    definition: ViewDefinition;
    assigned_to_me: boolean;
  },
  P extends {
    session: PortfolioSessionShape;
    definition: ViewDefinition;
    assigned_to_me: boolean;
  },
>() {
  let entries: ReadonlyMap<string, PortfolioEntry<T, P>> = new Map(),
    generation = 0;
  let activeActor: string | undefined, activeWorkspace: string | undefined;
  const empty: ReadonlyMap<string, PortfolioEntry<T, P>> = new Map(),
    listeners = new Set<() => void>(),
    controllers = new Map<string, AbortController>();
  const current = (entry: PortfolioScope) =>
    same(entry.actorID, activeActor) &&
    same(entry.workspaceID, activeWorkspace);
  function publish(id: string, entry: PortfolioEntry<T, P>) {
    entries = new Map(entries).set(id, entry);
    listeners.forEach((fn) => fn());
  }
  function matches(id: string, ticket: PortfolioTicket) {
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
  function start(entry: PortfolioEntry<T, P>) {
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
        publish(id, {
          ...entry,
          applied: null,
          status:
            entry.status === "pending"
              ? "uncertain"
              : entry.status === "ready"
                ? "idle"
                : entry.status,
          error: null,
        });
    },
    begin(
      scope: PortfolioScope,
      build: (requestID: string) => PortfolioRead<T>,
    ) {
      if (!current(scope)) throw new Error("PORTFOLIO_CONTEXT_CHANGED");
      const id = portfolioScope(scope),
        requestID = crypto.randomUUID(),
        command = structuredClone(build(requestID));
      if (command.body.request_id !== requestID)
        throw new Error("PORTFOLIO_REQUEST_ID_CHANGED");
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
    ticket(id: string): PortfolioTicket {
      const entry = entries.get(id),
        signal = controllers.get(id)?.signal;
      if (!entry || !current(entry) || entry.status !== "pending" || !signal)
        throw new Error("PORTFOLIO_CONTEXT_CHANGED");
      return {
        requestID: entry.command.body.request_id,
        attempt: entry.attempt,
        generation,
        signal,
      };
    },
    success(id: string, ticket: PortfolioTicket, page: P) {
      if (!matches(id, ticket)) return false;
      const entry = entries.get(id)!;
      if (
        !same(page.session.request_id, entry.command.body.request_id) ||
        !same(page.session.anchor_workspace_id, entry.workspaceID) ||
        page.session.resource !== entry.resource
      )
        throw new Error("PORTFOLIO_RESPONSE_SCOPE");
      if (
        typeof page.session.total_count !== "string" ||
        !/^(0|[1-9][0-9]*)$/.test(page.session.total_count)
      )
        throw new Error("PORTFOLIO_INVALID_COUNT");
      const expectedScopes = entry.command.body.workspace_ids
          .map((id) => id.toLowerCase())
          .sort(),
        actualScopes = page.session.workspaces
          .map((row) => row.id.toLowerCase())
          .sort();
      if (
        expectedScopes.length !== actualScopes.length ||
        new Set(actualScopes).size !== actualScopes.length ||
        expectedScopes.some((id, index) => id !== actualScopes[index])
      )
        throw new Error("PORTFOLIO_RESPONSE_SCOPE");
      if (
        page.session.workspaces.some(
          (row) =>
            typeof row.total_count !== "string" ||
            !/^(0|[1-9][0-9]*)$/.test(row.total_count),
        ) ||
        page.session.workspaces.reduce(
          (sum, row) => sum + BigInt(row.total_count),
          BigInt(0),
        ) !== BigInt(page.session.total_count)
      )
        throw new Error("PORTFOLIO_INVALID_COUNT");
      if (
        page.assigned_to_me !== entry.command.body.assigned_to_me ||
        !samePortfolioDefinition(page.definition, entry.command.body.definition)
      )
        throw new Error("PORTFOLIO_RESPONSE_QUERY");
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
      ticket: PortfolioTicket,
      error: string,
      status: Exclude<
        PortfolioEntry<T, P>["status"],
        "idle" | "pending" | "ready"
      >,
    ) {
      if (!matches(id, ticket)) return false;
      controllers.delete(id);
      const entry = entries.get(id)!;
      publish(id, { ...entry, applied: null, status, error });
      return true;
    },
    retry(id: string) {
      const entry = entries.get(id);
      if (!entry || !current(entry) || entry.status !== "uncertain")
        throw new Error("PORTFOLIO_CAPTURE_NOT_RETRYABLE");
      return start({
        ...entry,
        attempt: entry.attempt + 1,
        status: "pending",
        error: null,
      });
    },
    denyScopes(id: string, requestID: string) {
      const entry = entries.get(id);
      if (
        !entry ||
        !current(entry) ||
        !same(entry.command.body.request_id, requestID)
      )
        return false;
      controllers.get(id)?.abort();
      controllers.delete(id);
      publish(id, {
        ...entry,
        applied: null,
        status: "scope_changed",
        error: null,
      });
      return true;
    },
    block(
      id: string,
      sessionID: string,
      status: "scope_changed" | "forbidden",
    ) {
      const entry = entries.get(id);
      if (
        !entry ||
        !current(entry) ||
        entry.status !== "ready" ||
        !same(entry.applied?.page.session.id, sessionID)
      )
        return false;
      publish(id, { ...entry, applied: null, status, error: null });
      return true;
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
