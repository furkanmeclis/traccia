"use client";
import { documentClipboardHandlers } from "./clipboard";
import { toPlateValue, toPlateNodes, currentListNumbers } from "./adapter";
import type { Value, AnyPluginConfig } from "platejs";
import { layoutStyle } from "./layout";
import { useState, type ReactNode } from "react";
import {
  Plate,
  PlateContent,
  PlateElement,
  usePlateEditor,
  type PlateElementProps,
} from "platejs/react";
import { TablePlugin } from "@platejs/table/react";
import { useLocale } from "@/providers/locale-provider";
import { Button } from "@/components/ui/button";
import {
  documentPlugins,
  documentNodeIds,
  convertDocumentBlock,
  splitDocumentCell,
} from "./plate";
import {
  paragraph,
  plainText,
  parseClipboard,
  reseedSubtree,
  validateContent,
  safeLink,
  type Element,
} from "./contract";
export type MentionChoice = {
  kind: "user" | "issue" | "project";
  id: string;
  label: string;
  value?: string;
};
const mime = "application/vnd.traccia.plate+json";
function EditableElement(props: PlateElementProps) {
  const { element, children, editor } = props;
  const { t } = useLocale();
  const type = element.type;
  if (type === "mention")
    return (
      <PlateElement {...props} as="span">
        <span contentEditable={false} className="bg-muted rounded px-1">
          @{String(element.value)}
        </span>
        {children}
      </PlateElement>
    );
  if (type === "hr")
    return (
      <PlateElement {...props}>
        <span contentEditable={false}>
          <hr />
        </span>
        {children}
      </PlateElement>
    );
  if (type === "a")
    return (
      <PlateElement
        {...props}
        as="a"
        attributes={{
          ...props.attributes,
          href: safeLink(String(element.url)) ? String(element.url) : undefined,
          rel: "noopener noreferrer",
        }}
      >
        {children}
      </PlateElement>
    );
  if (type === "table")
    return (
      <PlateElement
        {...props}
        as="table"
        style={layoutStyle(element)}
        className="my-3 border-collapse"
      >
        {Array.isArray(element.colSizes) && (
          <colgroup>
            {element.colSizes.map((width, i) => (
              <col key={i} style={{ width: Number(width) }} />
            ))}
          </colgroup>
        )}
        <tbody>{children}</tbody>
      </PlateElement>
    );
  if (type === "td" || type === "th")
    return (
      <PlateElement
        {...props}
        as={type}
        attributes={{
          ...props.attributes,
          colSpan: Number(element.colSpan ?? 1),
          rowSpan: Number(element.rowSpan ?? 1),
        }}
        className="min-w-20 border p-2 align-top"
        style={layoutStyle(element)}
      >
        {children}
      </PlateElement>
    );
  if (type === "tr")
    return (
      <PlateElement {...props} as="tr" style={layoutStyle(element)}>
        {children}
      </PlateElement>
    );
  if (type === "code_block")
    return (
      <PlateElement
        {...props}
        as="pre"
        className="bg-muted my-2 overflow-x-auto rounded p-3 font-mono"
      >
        {children}
      </PlateElement>
    );
  if (type === "code_line")
    return (
      <PlateElement {...props} className="min-h-6">
        {children}
      </PlateElement>
    );
  if (type === "h1" || type === "h2" || type === "h3" || type === "blockquote")
    return (
      <PlateElement
        {...props}
        as={type}
        className={
          type === "h1"
            ? "text-2xl font-bold"
            : type === "h2"
              ? "text-xl font-semibold"
              : type === "h3"
                ? "text-lg font-semibold"
                : "border-l-2 pl-3"
        }
      >
        {children}
      </PlateElement>
    );
  return (
    <PlateElement
      {...props}
      as="p"
      className="min-h-6"
      style={{ paddingLeft: Number(element.indent ?? 0) * 16 }}
    >
      {element.listStyleType === "todo" ? (
        <span contentEditable={false}>
          <input
            type="checkbox"
            aria-label={String(
              element.children
                .map((c) => ("text" in c ? c.text : ""))
                .join("") || t("documents.todo"),
            )}
            checked={element.checked === true}
            onChange={(e) => {
              const path = editor.api.findPath(element);
              if (path)
                editor.tf.setNodes({ checked: e.target.checked }, { at: path });
            }}
          />
        </span>
      ) : element.listStyleType === "decimal" ? (
        <span contentEditable={false}>
          {String(
            currentListNumbers(editor.children).get(String(element.id)) ?? 1,
          )}
          .{" "}
        </span>
      ) : element.listStyleType === "disc" ? (
        <span contentEditable={false}>• </span>
      ) : null}
      {children}
    </PlateElement>
  );
}
const components = Object.fromEntries(
  [
    "p",
    "h1",
    "h2",
    "h3",
    "blockquote",
    "hr",
    "code_block",
    "code_line",
    "a",
    "mention",
    "table",
    "tr",
    "td",
    "th",
  ].map((type) => [type, EditableElement]),
);
export function DocumentEditor({
  initialValue,
  onChange,
  label,
  disabled = false,
  mentions = [],
  mentionsReady = false,
}: {
  initialValue: Element[];
  onChange: (content: Element[] | null, error: string | null) => void;
  label: string;
  disabled?: boolean;
  mentions?: MentionChoice[];
  mentionsReady?: boolean;
}) {
  const { t } = useLocale();
  const [error, setError] = useState<string | null>(null),
    [mention, setMention] = useState(""),
    [url, setUrl] = useState("");
  const editor = usePlateEditor<Value, AnyPluginConfig>({
    plugins: documentPlugins,
    nodeId: documentNodeIds,
    value: toPlateValue(initialValue),
    components,
  });
  const run = (fn: () => void) => {
    if (disabled) return;
    try {
      fn();
      editor.tf.focus();
    } catch (cause) {
      setError(String(cause));
    }
  };
  const button = (key: string, fn: () => void): ReactNode => (
    <Button
      key={key}
      type="button"
      size="sm"
      variant="outline"
      disabled={disabled}
      onMouseDown={(e) => e.preventDefault()}
      onClick={() => run(fn)}
    >
      {t("documents." + key)}
    </Button>
  );
  const insert = (node: Element) =>
    run(() => editor.tf.insertNodes(toPlateValue([node])[0]));
  const copy = (clipboard: DataTransfer) => {
    const fragment = editor.selection
      ? editor.api.fragment(editor.selection)
      : [];
    if (!fragment.length) return false;
    const raw = JSON.stringify({
      format: "plate",
      schema_version: 1,
      fragment,
    });
    try {
      const typed = parseClipboard(raw);
      clipboard.setData(mime, raw);
      clipboard.setData("text/plain", plainText(typed));
      return true;
    } catch (cause) {
      setError(String(cause));
      return false;
    }
  };
  return (
    <div className="space-y-2">
      <div
        role="toolbar"
        aria-label={t("documents.toolbar")}
        className="flex flex-wrap gap-1"
      >
        {["bold", "italic", "underline", "strikethrough", "code"].map((mark) =>
          button(mark, () => editor.tf.toggleMark(mark)),
        )}
        {["p", "h1", "h2", "h3", "blockquote"].map((type) =>
          button(type, () =>
            convertDocumentBlock(
              editor,
              type as "p" | "h1" | "h2" | "h3" | "blockquote",
            ),
          ),
        )}
        {button("bullets", () => convertDocumentBlock(editor, "p", "disc"))}
        {button("ordered", () => convertDocumentBlock(editor, "p", "decimal"))}
        {button("todo", () => convertDocumentBlock(editor, "p", "todo"))}
        {button("codeBlock", () =>
          insert({
            id: crypto.randomUUID(),
            type: "code_block",
            lang: "text",
            children: [
              {
                id: crypto.randomUUID(),
                type: "code_line",
                children: [{ text: "" }],
              },
            ],
          }),
        )}
        {button("table", () =>
          insert({
            id: crypto.randomUUID(),
            type: "table",
            colSizes: [160, 160],
            children: [0, 1].map(() => ({
              id: crypto.randomUUID(),
              type: "tr",
              children: [0, 1].map(() => ({
                id: crypto.randomUUID(),
                type: "td",
                children: [paragraph()],
              })),
            })),
          }),
        )}
        {button("mergeCells", () =>
          (() => {
            const cells = [
              ...editor.api.nodes({
                match: (n: { type?: unknown }) =>
                  "type" in n && (n.type === "td" || n.type === "th"),
              }),
            ];
            if (cells.length > 1)
              editor.getTransforms(TablePlugin).table.merge();
          })(),
        )}
        {button("splitCell", () => splitDocumentCell(editor))}
        {button("undo", () => editor.tf.undo())}
        {button("redo", () => editor.tf.redo())}
      </div>
      <div className="flex flex-wrap gap-2">
        <label className="flex min-w-0 flex-1 flex-col text-sm">
          {t("documents.linkUrl")}
          <input
            type="url"
            className="rounded border p-2"
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            disabled={disabled}
          />
        </label>
        <Button
          type="button"
          variant="outline"
          disabled={disabled || !safeLink(url)}
          onMouseDown={(e) => e.preventDefault()}
          onClick={() =>
            insert({
              id: crypto.randomUUID(),
              type: "a",
              url,
              children: [{ text: url }],
            })
          }
        >
          {t("documents.insertLink")}
        </Button>
        <label className="flex min-w-0 flex-1 flex-col text-sm">
          {t("documents.mention")}
          <select
            value={mention}
            onChange={(e) => setMention(e.target.value)}
            disabled={disabled || !mentionsReady}
            className="max-w-full rounded border p-2"
          >
            <option value="">{t("documents.chooseReference")}</option>
            {mentions.map((item) => (
              <option
                key={item.kind + ":" + item.id}
                value={item.kind + ":" + item.id}
              >
                {item.label}
              </option>
            ))}
          </select>
        </label>
        <Button
          type="button"
          variant="outline"
          disabled={disabled || !mentionsReady || !mention}
          onMouseDown={(e) => e.preventDefault()}
          onClick={() => {
            const item = mentions.find(
              (item) => item.kind + ":" + item.id === mention,
            );
            if (item)
              insert({
                id: crypto.randomUUID(),
                type: "mention",
                key: mention,
                value: item.value ?? item.label,
                target_kind: item.kind,
                target_id: item.id,
                children: [{ text: "" }],
              });
          }}
        >
          {t("documents.insertMention")}
        </Button>
      </div>
      <Plate
        editor={editor}
        onChange={() => {
          const value = structuredClone(editor.children);
          try {
            validateContent(value);
            setError(null);
            onChange(value, null);
          } catch (cause) {
            const message = String(cause);
            setError(message);
            onChange(null, message);
          }
        }}
      >
        <PlateContent
          readOnly={disabled}
          aria-label={label}
          aria-multiline
          className="min-h-48 overflow-x-auto rounded border p-3 break-words whitespace-pre-wrap focus-visible:outline-2"
          {...documentClipboardHandlers({
            copy,
            cut: () => {
              if (!disabled) editor.tf.deleteFragment();
            },
            paste: (clipboard) => {
              try {
                const raw = clipboard.getData(mime);
                if (raw) {
                  const parsed = parseClipboard(raw);
                  editor.tf.insertFragment(toPlateNodes(reseedSubtree(parsed)));
                } else {
                  editor.tf.insertText(clipboard.getData("text/plain"));
                }
                setError(null);
              } catch (cause) {
                setError(String(cause));
              }
            },
          })}
        />
      </Plate>
      {error && (
        <p role="alert" className="text-destructive text-sm">
          {t("documents.validation")}: {error}
        </p>
      )}
    </div>
  );
}
