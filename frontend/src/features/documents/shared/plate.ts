import { toPlateValue } from "./adapter.ts";
import { splitCellTable } from "./table.ts";
import type { Value, AnyPluginConfig } from "platejs";
import { createPlateEditor, createPlatePlugin } from "platejs/react";
import {
  H1Plugin,
  H2Plugin,
  H3Plugin,
  BlockquotePlugin,
  HorizontalRulePlugin,
  BoldPlugin,
  ItalicPlugin,
  UnderlinePlugin,
  StrikethroughPlugin,
  CodePlugin,
} from "@platejs/basic-nodes/react";
import { CodeBlockPlugin, CodeLinePlugin } from "@platejs/code-block/react";
import { IndentPlugin } from "@platejs/indent/react";
import { ListPlugin } from "@platejs/list/react";
import { LinkPlugin } from "@platejs/link/react";
import { MentionPlugin } from "@platejs/mention/react";
import {
  TablePlugin,
  TableRowPlugin,
  TableCellPlugin,
  TableCellHeaderPlugin,
} from "@platejs/table/react";
import { validateContent, type Element } from "./contract.ts";
const PreserveCanonicalStructure = createPlatePlugin({
  key: "preserveCanonicalStructure",
}).overrideEditor(({ editor, tf: { normalizeNode, apply } }) => {
  let applying = 0;
  return {
    transforms: {
      apply(operation) {
        // ListPlugin otherwise rewrites canonical listStart as a side effect of any edit.
        if (
          applying > 0 &&
          operation.type === "set_node" &&
          [
            ...Object.keys(operation.properties),
            ...Object.keys(operation.newProperties),
          ].every((key) => key === "listStart")
        )
          return;
        if (
          operation.type === "split_node" &&
          operation.properties.listStyleType === "decimal" &&
          editor.api.isSaving() !== false
        )
          delete operation.properties.listStart;
        const historySplit =
          operation.type === "split_node" &&
          editor.api.isSaving() === false &&
          operation.properties.listStyleType === "decimal"
            ? { ...operation.properties }
            : null;
        applying++;
        try {
          apply(
            historySplit && operation.type === "split_node"
              ? { ...operation, properties: { ...historySplit } }
              : operation,
          );
          if (
            historySplit &&
            operation.type === "split_node" &&
            historySplit.listRestart !== undefined
          ) {
            const path = [...operation.path];
            path[path.length - 1]++;
            editor.tf.setNodes(
              { listRestart: historySplit.listRestart },
              { at: path },
            );
          }
        } finally {
          applying--;
        }
      },
      normalizeNode(entry, options) {
        const [node] = entry;
        if (
          "type" in node &&
          ((node.type === "tr" &&
            Array.isArray(node.children) &&
            node.children.length === 0) ||
            (node.type === "p" && "listStyleType" in node) ||
            node.type === "blockquote")
        ) {
          try {
            validateContent(editor.children);
            return;
          } catch {
            /* Never repair invalid imports. The editor reports them through canonical validation. */
          }
        }
        normalizeNode(entry, options);
      },
    },
  };
});
export const documentPlugins = [
  H1Plugin,
  H2Plugin,
  H3Plugin,
  BlockquotePlugin,
  HorizontalRulePlugin,
  BoldPlugin,
  ItalicPlugin,
  UnderlinePlugin,
  StrikethroughPlugin,
  CodePlugin,
  CodeBlockPlugin,
  CodeLinePlugin,
  IndentPlugin,
  ListPlugin,
  LinkPlugin,
  MentionPlugin,
  TablePlugin,
  TableRowPlugin,
  TableCellPlugin,
  TableCellHeaderPlugin,
  PreserveCanonicalStructure,
];
export const documentNodeIds = {
  idCreator: () => crypto.randomUUID(),
  filterInline: false,
  filterText: true,
  initialValueIds: false as const,
  reuseId: true,
};
export function createDocumentEditor(value: Element[]) {
  return createPlateEditor<Value, AnyPluginConfig>({
    plugins: documentPlugins,
    nodeId: documentNodeIds,
    value: toPlateValue(value),
  });
}
/** User-requested conversion removes properties that do not belong to its new type. */
export function convertDocumentBlock(
  editor: ReturnType<typeof createDocumentEditor>,
  type: "p" | "h1" | "h2" | "h3" | "blockquote",
  listStyleType?: "disc" | "decimal" | "todo",
) {
  const match = (node: { type?: unknown }) =>
    ["p", "h1", "h2", "h3", "blockquote"].includes(String(node.type));
  editor.tf.withoutNormalizing(() => {
    editor.tf.unsetNodes(
      ["indent", "listStyleType", "checked", "listStart", "listRestart"],
      { match },
    );
    editor.tf.setNodes(
      {
        type,
        ...(listStyleType ? { listStyleType, indent: 1 } : {}),
        ...(listStyleType === "decimal" ? { listStart: 1 } : {}),
        ...(listStyleType === "todo" ? { checked: false } : {}),
      },
      { match },
    );
  });
}
export function splitDocumentCell(
  editor: ReturnType<typeof createDocumentEditor>,
) {
  const selected = editor.api.above({
    match: (n: { type?: unknown }) =>
      "type" in n && (n.type === "td" || n.type === "th"),
  })?.[0];
  if (!selected) return;
  for (const [node, path] of editor.api.nodes({
    at: [],
    match: { type: "table" },
  })) {
    try {
      const candidate: unknown = [node];
      validateContent(candidate);
      const table = candidate[0];
      if (table.type !== "table") continue;
      const next = splitCellTable(table, String(selected.id));
      const content: unknown = structuredClone(editor.children);
      validateContent(content);
      let siblings: import("./contract.ts").Node[] = content;
      for (const part of path.slice(0, -1)) {
        const ancestor = siblings[part];
        if (!("children" in ancestor)) throw new Error("Invalid table path");
        siblings = ancestor.children;
      }
      siblings[path.at(-1)!] = next;
      validateContent(content);
      editor.tf.withNewBatch(() =>
        editor.tf.replaceNodes(toPlateValue([next])[0], { at: path }),
      );
      const entry = [
        ...editor.api.nodes({ at: [], match: { id: selected.id } }),
      ][0];
      if (entry) editor.tf.select(editor.api.start(entry[1]));
      return;
    } catch (error) {
      if (!(error instanceof Error) || error.message !== "Cell not found")
        throw error;
    }
  }
}
