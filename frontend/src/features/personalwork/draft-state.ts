import { emptyQuery, type Query } from "../searchviews/query.ts";
import type { PersonalResource } from "./portfolio-state.ts";
export type HomeDraft = {
  resource: PersonalResource;
  query: Query;
  workspaceIDs: string[];
  assignedToMe: boolean;
  ttl: string;
  raw: string;
  numericInputs: Record<string, string>;
  error: string | null;
  generation: number;
};
export function homeDraftKey(
  actorID: string,
  anchorID: string,
  resource: PersonalResource,
) {
  return JSON.stringify([
    actorID.toLowerCase(),
    anchorID.toLowerCase(),
    resource,
  ]);
}
export function createHomeDrafts() {
  let entries: ReadonlyMap<string, HomeDraft> = new Map();
  const empty: ReadonlyMap<string, HomeDraft> = new Map(),
    listeners = new Set<() => void>();
  function publish(id: string, value: HomeDraft) {
    entries = new Map(entries).set(id, value);
    listeners.forEach((fn) => fn());
    return value;
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
    ensure(id: string, anchorID: string, resource: PersonalResource) {
      return (
        entries.get(id) ??
        publish(id, {
          resource,
          query: emptyQuery(),
          workspaceIDs: [anchorID],
          assignedToMe: resource === "issue",
          ttl: "",
          raw: "",
          numericInputs: {},
          error: null,
          generation: 0,
        })
      );
    },
    patch(
      id: string,
      update: Partial<Omit<HomeDraft, "resource" | "generation">>,
    ) {
      const prior = entries.get(id);
      if (!prior) throw new Error("PERSONAL_DRAFT_MISSING");
      return publish(id, { ...prior, ...structuredClone(update) });
    },
    loadQuery(id: string, query: Query) {
      const prior = entries.get(id);
      if (!prior) throw new Error("PERSONAL_DRAFT_MISSING");
      return publish(id, {
        ...prior,
        query: structuredClone(query),
        numericInputs: {},
        error: null,
        generation: prior.generation + 1,
      });
    },
  };
}
export const homeDrafts = createHomeDrafts();
