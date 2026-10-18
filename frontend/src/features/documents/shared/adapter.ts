import type { Descendant, Value } from "platejs";
import {
  validateContent,
  listNumbers,
  type Node,
  type Element,
} from "./contract.ts";
/** Structural copy into Plate's open node type; canonical data remains closed. */
export function toPlateNodes(nodes: Node[]): Descendant[] {
  return nodes.map((node) =>
    "text" in node
      ? { ...node }
      : { ...node, children: toPlateNodes(node.children) },
  );
}
export function toPlateValue(nodes: Element[]): Value {
  return nodes.map((node) => ({
    ...node,
    children: toPlateNodes(node.children),
  }));
}
export function currentListNumbers(value: unknown) {
  try {
    validateContent(value);
    return listNumbers(value);
  } catch {
    return new Map<string, number>();
  }
}
