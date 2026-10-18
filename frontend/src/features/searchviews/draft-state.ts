import {
  emptyQuery,
  defaultColumns,
  type Query,
  type Column,
} from "./query.ts";
import type { SearchResource } from "./capture-state.ts";
export type ViewDraft = {
  generation: number;
  resource: SearchResource;
  numericInputs: Record<string, string>;
  name: string;
  query: Query;
  columns: Column[];
  revision: string | null;
  baseline: { name: string; query: Query; columns: Column[] };
  raw: string;
  ttl: string;
  error: string | null;
};
export function draftScope(
  actorID: string,
  workspaceID: string,
  resource: SearchResource,
  target = "query",
) {
  return JSON.stringify([
    actorID.toLowerCase(),
    workspaceID.toLowerCase(),
    target.toLowerCase().startsWith("view:") ? "view" : resource,
    target.toLowerCase(),
  ]);
}
export function queryEqual(a: unknown, b: unknown): boolean {
  if (a === b) return true;
  if (Array.isArray(a) || Array.isArray(b))
    return (
      Array.isArray(a) &&
      Array.isArray(b) &&
      a.length === b.length &&
      a.every((v, i) => queryEqual(v, b[i]))
    );
  if (typeof a !== "object" || !a || typeof b !== "object" || !b) return false;
  const left = a as Record<string, unknown>,
    right = b as Record<string, unknown>,
    keys = Object.keys(left);
  return (
    keys.length === Object.keys(right).length &&
    keys.every((k) => Object.hasOwn(right, k) && queryEqual(left[k], right[k]))
  );
}
export function viewDraftChanged(draft: ViewDraft) {
  return !queryEqual(
    { name: draft.name, query: draft.query, columns: draft.columns },
    draft.baseline,
  );
}
export function createViewDraftStore() {
  let entries: ReadonlyMap<string, ViewDraft> = new Map();
  const empty: ReadonlyMap<string, ViewDraft> = new Map(),
    listeners = new Set<() => void>();
  function set(id: string, draft: ViewDraft) {
    entries = new Map(entries).set(id, draft);
    listeners.forEach((fn) => fn());
    return draft;
  }
  function fresh(
    resource: SearchResource,
    baseline?: {
      name: string;
      query: Query;
      columns: Column[];
      revision: string | null;
    },
  ): ViewDraft {
    const value = structuredClone(
      baseline ?? {
        name: "",
        query: emptyQuery(),
        columns: defaultColumns(resource),
        revision: null,
      },
    );
    return {
      ...value,
      resource,
      baseline: structuredClone({
        name: value.name,
        query: value.query,
        columns: value.columns,
      }),
      generation: 0,
      numericInputs: {},
      raw: "",
      ttl: "",
      error: null,
    };
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
    ensure(
      id: string,
      resource: SearchResource,
      baseline?: Parameters<typeof fresh>[1],
    ) {
      return entries.get(id) ?? set(id, fresh(resource, baseline));
    },
    patch(
      id: string,
      update: Partial<
        Pick<
          ViewDraft,
          | "name"
          | "query"
          | "columns"
          | "raw"
          | "error"
          | "numericInputs"
          | "ttl"
        >
      >,
    ) {
      const prior = entries.get(id);
      if (!prior) throw new Error("VIEW_DRAFT_MISSING");
      const shape = (query: Query): string => {
        const walk = (node: Query["filter"]): unknown =>
          !node
            ? null
            : node.kind === "all" || node.kind === "any"
              ? [node.kind, node.children.map(walk)]
              : node.kind === "not"
                ? [node.kind, walk(node.child)]
                : node.kind;
        return JSON.stringify(walk(query.filter));
      };
      return set(id, {
        ...prior,
        ...(update.query && shape(update.query) !== shape(prior.query)
          ? { numericInputs: {} }
          : {}),
        ...structuredClone(update),
      });
    },
    loadQuery(id: string, query: Query) {
      const prior = entries.get(id);
      if (!prior) throw new Error("VIEW_DRAFT_MISSING");
      return set(id, {
        ...prior,
        query: structuredClone(query),
        numericInputs: {},
        generation: prior.generation + 1,
        error: null,
      });
    },
    reset(
      id: string,
      resource: SearchResource,
      baseline?: Parameters<typeof fresh>[1],
    ) {
      return set(id, {
        ...fresh(resource, baseline),
        generation: (entries.get(id)?.generation ?? -1) + 1,
      });
    },
  };
}
export const viewDrafts = createViewDraftStore();
