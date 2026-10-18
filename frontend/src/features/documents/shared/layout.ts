import type { CSSProperties } from "react";
import type { Border } from "./contract";
/** Values reach CSS through individual properties, never raw CSS or HTML. */
export function layoutStyle(node: {
  type: string;
  size?: number;
  background?: string;
  borders?: Partial<Record<"top" | "right" | "bottom" | "left", Border>>;
  marginLeft?: number;
}): CSSProperties {
  const style: CSSProperties = {};
  if (node.type === "table" && node.marginLeft !== undefined)
    style.marginLeft = node.marginLeft;
  if (node.type === "tr" && node.size !== undefined) style.height = node.size;
  if (node.type === "td" || node.type === "th") {
    if (node.size !== undefined) style.width = node.size;
    if (node.background !== undefined) style.backgroundColor = node.background;
    for (const [edge, border] of Object.entries(node.borders ?? {}))
      if (border) {
        const prefix = `border${edge[0].toUpperCase()}${edge.slice(1)}`;
        Object.assign(style, {
          [prefix + "Color"]: border.color,
          [prefix + "Style"]: border.style,
          [prefix + "Width"]: border.size,
        });
      }
  }
  return style;
}
