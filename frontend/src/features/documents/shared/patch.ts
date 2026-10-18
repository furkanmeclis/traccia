import {
  sectionMembers,
  canonical,
  type Element,
  type Block,
} from "./contract.ts";
export type Patch =
  | { op: "replace_node"; node_id: string; node: Element }
  | {
      op: "insert_nodes";
      parent_id: string | null;
      before_id: string | null;
      nodes: Element[];
    }
  | { op: "remove_node"; node_id: string }
  | {
      op: "move_node";
      node_id: string;
      parent_id: string | null;
      before_id: string | null;
    }
  | {
      op: "replace_section";
      heading_id: string;
      expected_member_ids: string[];
      nodes: Block[];
    };
// JSON object property order carries no content meaning. Array order and every
// own property/value still matter; comparison never rewrites either tree.
function same(a: unknown, b: unknown): boolean {
  if (Object.is(a, b)) return true;
  if (
    a === null ||
    b === null ||
    typeof a !== "object" ||
    typeof b !== "object"
  )
    return false;
  if (Array.isArray(a) || Array.isArray(b)) {
    return (
      Array.isArray(a) &&
      Array.isArray(b) &&
      a.length === b.length &&
      a.every((value, index) => same(value, b[index]))
    );
  }
  const keys = Object.keys(a);
  return (
    keys.length === Object.keys(b).length &&
    keys.every(
      (key) =>
        Object.prototype.hasOwnProperty.call(b, key) &&
        same(Reflect.get(a, key), Reflect.get(b, key)),
    )
  );
}
/** Never silently discard sibling draft changes when saving a targeted patch. */
export function nodePatch(
  baseline: Element[],
  draft: Element[],
  id: string,
): Patch {
  const before = baseline.find((n) => n.id === id),
    after = draft.find((n) => n.id === id);
  if (
    !before ||
    !after ||
    !same(
      baseline.filter((n) => n.id !== id),
      draft.filter((n) => n.id !== id),
    ) ||
    !same(
      baseline.map((n) => n.id),
      draft.map((n) => n.id),
    )
  )
    throw new Error("outsideNode");
  return { op: "replace_node", node_id: id, node: structuredClone(after) };
}
export function sectionPatch(
  baseline: Element[],
  draft: Element[],
  id: string,
): Patch {
  const before = sectionMembers(canonical(baseline).content, id),
    after = sectionMembers(canonical(draft).content, id),
    oldIds = new Set(before.map((n) => n.id)),
    newIds = new Set(after.map((n) => n.id));
  if (
    !same(
      baseline.filter((n) => !oldIds.has(n.id)),
      draft.filter((n) => !newIds.has(n.id)),
    )
  )
    throw new Error("outsideSection");
  return {
    op: "replace_section",
    heading_id: id,
    expected_member_ids: before.map((n) => n.id),
    nodes: structuredClone(after),
  };
}
