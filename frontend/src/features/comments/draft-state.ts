import {
  canonical,
  paragraph,
  type Element,
} from "../documents/shared/contract.ts";
export type CommentBaseline = { revision: string; content: Element[] } | null;
export type CommentDraft = {
  initial: Element[];
  content: Element[];
  baselineContent: Element[];
  revision: string | null;
  generation: number;
  raw: string;
  error: string | null;
  target: string;
};
export function commentDraftScope(
  actor: string,
  workspace: string,
  issue: string,
  comment?: string,
) {
  return JSON.stringify([
    actor.toLowerCase(),
    workspace.toLowerCase(),
    issue.toLowerCase(),
    comment?.toLowerCase() ?? "create",
  ]);
}
function fresh(baseline: CommentBaseline, generation: number): CommentDraft {
  const content = baseline
    ? canonical(baseline.content).content
    : [paragraph()];
  return {
    initial: structuredClone(content),
    content: structuredClone(content),
    baselineContent: structuredClone(content),
    revision: baseline?.revision ?? null,
    generation,
    raw: "",
    error: null,
    target: content[0]?.id ?? "",
  };
}
export function draftChanged(draft: CommentDraft) {
  return (
    JSON.stringify(draft.content) !== JSON.stringify(draft.baselineContent)
  );
}
export function createCommentDraftStore() {
  let snapshot: ReadonlyMap<string, CommentDraft> = new Map();
  const empty: ReadonlyMap<string, CommentDraft> = new Map(),
    listeners = new Set<() => void>();
  function set(key: string, value: CommentDraft) {
    snapshot = new Map(snapshot).set(key, value);
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
    snapshot: () => snapshot,
    serverSnapshot: () => empty,
    get: (key: string) => snapshot.get(key),
    ensure(key: string, baseline: CommentBaseline) {
      return snapshot.get(key) ?? set(key, fresh(baseline, 0));
    },
    patch(
      key: string,
      update: Partial<
        Pick<CommentDraft, "content" | "raw" | "error" | "target">
      >,
    ) {
      const prior = snapshot.get(key);
      if (!prior) throw new Error("COMMENT_DRAFT_MISSING");
      return set(key, { ...prior, ...structuredClone(update) });
    },
    load(key: string, content: Element[]) {
      const prior = snapshot.get(key);
      if (!prior) throw new Error("COMMENT_DRAFT_MISSING");
      const next = canonical(content).content;
      return set(key, {
        ...prior,
        initial: structuredClone(next),
        content: structuredClone(next),
        generation: prior.generation + 1,
        error: null,
        target: next[0]?.id ?? "",
      });
    },
    reset(key: string, baseline: CommentBaseline) {
      return set(
        key,
        fresh(baseline, (snapshot.get(key)?.generation ?? -1) + 1),
      );
    },
  };
}
export const commentDrafts = createCommentDraftStore();
