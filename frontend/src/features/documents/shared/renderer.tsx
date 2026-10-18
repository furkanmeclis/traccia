"use client";
import { useLocale } from "@/providers/locale-provider";
import { layoutStyle } from "./layout";
import { Fragment, type ReactNode } from "react";
import { safeLink, listNumbers, type Node, type Element } from "./contract";
/** React escapes text. Only an explicit URL allowlist produces anchors. */
export function DocumentRenderer({
  content,
  workspaceId,
}: {
  content: Element[];
  workspaceId?: string;
}) {
  const { t } = useLocale();
  const numbers = listNumbers(content);
  const render = (node: Node, index: number): ReactNode => {
    if ("text" in node) {
      let child: ReactNode = node.text;
      if (node.code) child = <code>{child}</code>;
      if (node.bold) child = <strong>{child}</strong>;
      if (node.italic) child = <em>{child}</em>;
      if (node.underline) child = <u>{child}</u>;
      if (node.strikethrough) child = <s>{child}</s>;
      return <Fragment key={index}>{child}</Fragment>;
    }
    const children = node.children.map(render),
      key = node.id;
    switch (node.type) {
      case "h1":
        return <h1 key={key}>{children}</h1>;
      case "h2":
        return <h2 key={key}>{children}</h2>;
      case "h3":
        return <h3 key={key}>{children}</h3>;
      case "blockquote":
        return <blockquote key={key}>{children}</blockquote>;
      case "hr":
        return <hr key={key} />;
      case "code_block":
        return (
          <pre key={key} data-language={String(node.lang ?? "")}>
            <code>
              {node.children.map((line, i) => (
                <Fragment key={"id" in line ? line.id : i}>
                  {i > 0 ? "\n" : ""}
                  {line.children.map(render)}
                </Fragment>
              ))}
            </code>
          </pre>
        );
      case "code_line":
        return (
          <Fragment key={key}>
            {children}
            {"\n"}
          </Fragment>
        );
      case "a":
        return safeLink(String(node.url)) ? (
          <a
            key={key}
            href={String(node.url)}
            target={node.target === "_blank" ? "_blank" : undefined}
            rel="noopener noreferrer"
          >
            {children}
          </a>
        ) : (
          <span key={key}>{children}</span>
        );
      case "mention": {
        const chip = (
          <span
            className="bg-muted rounded px-1"
            data-target-kind={node.target_kind}
            data-target-id={node.target_id}
          >
            @{node.value}
          </span>
        );
        return workspaceId && node.target_kind !== "user" ? (
          <a
            key={key}
            href={`/w/${encodeURIComponent(workspaceId)}/${node.target_kind === "issue" ? "issues" : "projects"}/${encodeURIComponent(node.target_id)}`}
          >
            {chip}
          </a>
        ) : (
          <Fragment key={key}>{chip}</Fragment>
        );
      }
      case "table":
        return (
          <div key={key} className="overflow-x-auto">
            <table className="border-collapse" style={layoutStyle(node)}>
              {node.colSizes && (
                <colgroup>
                  {node.colSizes.map((width, i) => (
                    <col key={i} style={{ width }} />
                  ))}
                </colgroup>
              )}
              <tbody>{children}</tbody>
            </table>
          </div>
        );
      case "tr":
        return (
          <tr key={key} style={layoutStyle(node)}>
            {children}
          </tr>
        );
      case "td":
      case "th": {
        const Tag = node.type;
        return (
          <Tag
            key={key}
            className="border p-2 align-top"
            style={layoutStyle(node)}
            colSpan={Number(node.colSpan ?? 1)}
            rowSpan={Number(node.rowSpan ?? 1)}
          >
            {children}
          </Tag>
        );
      }
      default:
        return (
          <p key={key} style={{ paddingLeft: Number(node.indent ?? 0) * 16 }}>
            {node.listStyleType === "todo" ? (
              <input
                type="checkbox"
                checked={node.checked === true}
                readOnly
                tabIndex={-1}
                aria-label={
                  node.children
                    .map((c) => ("text" in c ? c.text : ""))
                    .join("") || t("documents.todo")
                }
              />
            ) : node.listStyleType === "decimal" ? (
              `${numbers.get(node.id) ?? 1}. `
            ) : node.listStyleType === "disc" ? (
              "• "
            ) : null}
            {children}
          </p>
        );
    }
  };
  return (
    <div className="[&_pre]:bg-muted space-y-2 break-words whitespace-pre-wrap [&_a]:underline [&_h1]:text-2xl [&_h2]:text-xl [&_h3]:text-lg [&_pre]:overflow-x-auto [&_pre]:p-3">
      {content.map(render)}
    </div>
  );
}
