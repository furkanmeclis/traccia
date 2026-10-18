/** Canonical JSON only; Markdown and HTML are derived exports. */
export type TextLeaf = {
  text: string;
  bold?: boolean;
  italic?: boolean;
  underline?: boolean;
  strikethrough?: boolean;
  code?: boolean;
};
export type Border = {
  color: string;
  style: "solid" | "dashed" | "dotted" | "none";
  size: number;
};
interface Base<K extends string, C> {
  id: string;
  type: K;
  children: C[];
}
export type Link = Base<"a", TextLeaf> & {
  url: string;
  target?: "_blank" | "_self";
};
export type Mention = Base<"mention", { text: "" }> & {
  key: string;
  value: string;
  target_kind: "user" | "issue" | "project";
  target_id: string;
};
export type Inline = TextLeaf | Link | Mention;
export type Paragraph = Base<"p", Inline> & {
  indent?: number;
  listStyleType?: "disc" | "decimal" | "todo";
  checked?: boolean;
  listStart?: number;
  listRestart?: number;
};
export type Heading = Base<"h1" | "h2" | "h3" | "blockquote", Inline>;
export type Rule = Base<"hr", { text: "" }>;
export type CodeLine = Base<"code_line", TextLeaf>;
export type CodeBlock = Base<"code_block", CodeLine> & { lang?: string };
export type Cell = Base<"td" | "th", Block> & {
  colSpan?: number;
  rowSpan?: number;
  size?: number;
  background?: string;
  borders?: Partial<Record<"top" | "right" | "bottom" | "left", Border>>;
};
export type Row = Base<"tr", Cell> & { size?: number };
export type Table = Base<"table", Row> & {
  colSizes?: number[];
  marginLeft?: number;
};
export type Block = Paragraph | Heading | Rule | CodeBlock | Table;
export type Element = Block | Link | Mention | CodeLine | Cell | Row;
export type Node = TextLeaf | Element;
export type CanonicalContent = {
  format: "plate";
  schema_version: 1;
  content: Block[];
};
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const marks = ["bold", "italic", "underline", "strikethrough", "code"];
const fields: Record<string, string[]> = {
  p: ["indent", "listStyleType", "checked", "listStart", "listRestart"],
  h1: [],
  h2: [],
  h3: [],
  blockquote: [],
  hr: [],
  code_block: ["lang"],
  code_line: [],
  a: ["url", "target"],
  mention: ["key", "value", "target_kind", "target_id"],
  table: ["colSizes", "marginLeft"],
  tr: ["size"],
  td: ["colSpan", "rowSpan", "size", "background", "borders"],
  th: ["colSpan", "rowSpan", "size", "background", "borders"],
};
function object(v: unknown): v is Record<string, unknown> {
  return !!v && typeof v === "object" && !Array.isArray(v);
}
function closed(v: Record<string, unknown>, keys: string[], path: string) {
  for (const key of Object.keys(v))
    if (!keys.includes(key))
      throw new Error(`${path}: unknown property ${key}`);
}
function finite(v: unknown, min = 0, max = 10000, integer = false) {
  return (
    typeof v === "number" &&
    Number.isFinite(v) &&
    v >= min &&
    v <= max &&
    (!integer || Number.isInteger(v))
  );
}
export function wellFormedString(value: string) {
  for (let i = 0; i < value.length; i++) {
    const code = value.charCodeAt(i);
    if (code >= 0xd800 && code <= 0xdbff) {
      const next = value.charCodeAt(++i);
      if (!(next >= 0xdc00 && next <= 0xdfff)) return false;
    } else if (code >= 0xdc00 && code <= 0xdfff) return false;
  }
  return true;
}
export function safeColor(value: string) {
  return /^(?:#[0-9a-fA-F]{3}|#[0-9a-fA-F]{4}|#[0-9a-fA-F]{6}|#[0-9a-fA-F]{8}|transparent|currentColor)$/.test(
    value,
  );
}
export function safeLink(url: string) {
  if (
    !url ||
    !wellFormedString(url) ||
    new TextEncoder().encode(url).length > 2048 ||
    url.trim() !== url ||
    /[\u0000-\u001f\u007f-\u009f]/.test(url) ||
    url.includes("\\")
  )
    return false;
  try {
    const parsed = new URL(url);
    if (parsed.username || parsed.password) return false;
    return parsed.protocol === "http:" || parsed.protocol === "https:"
      ? !!parsed.hostname
      : parsed.protocol === "mailto:" && !!parsed.pathname;
  } catch {
    return false;
  }
}
export function validateContent(value: unknown): asserts value is Block[] {
  if (!Array.isArray(value) || !value.length)
    throw new Error("content: expected non-empty array");
  if (new TextEncoder().encode(JSON.stringify(value)).length > 512 * 1024)
    throw new Error("content: exceeds 512 KiB");
  const ids = new Set<string>();
  let count = 0;
  function visit(
    n: unknown,
    path: string,
    parent: string | null,
    depth: number,
  ) {
    if (++count > 2000 || depth > 32)
      throw new Error(`${path}: document limit exceeded`);
    if (!object(n)) throw new Error(`${path}: expected node object`);
    if (
      Object.values(n).some(
        (value) => typeof value === "string" && !wellFormedString(value),
      )
    )
      throw new Error(`${path}: invalid Unicode surrogate`);
    if ("text" in n) {
      closed(n, ["text", ...marks], path);
      if (
        typeof n.text !== "string" ||
        marks.some((k) => k in n && typeof n[k] !== "boolean")
      )
        throw new Error(`${path}: invalid text leaf`);
      if (
        parent === null ||
        ["table", "tr", "td", "th", "code_block"].includes(parent)
      )
        throw new Error(`${path}: invalid text parent`);
      return;
    }
    const type = n.type;
    if (typeof type !== "string" || !(type in fields))
      throw new Error(`${path}: unknown node type`);
    closed(n, ["id", "type", "children", ...fields[type]], path);
    if (
      typeof n.id !== "string" ||
      !uuid.test(n.id) ||
      n.id.replaceAll("-", "").toLowerCase() ===
        "00000000000000000000000000000000" ||
      ids.has(n.id.toLowerCase())
    )
      throw new Error(`${path}: missing, invalid or duplicate UUID`);
    ids.add(n.id.toLowerCase());
    if (!Array.isArray(n.children) || (!n.children.length && type !== "tr"))
      throw new Error(`${path}: children required`);
    if (
      parent === null &&
      ["a", "mention", "tr", "td", "th", "code_line"].includes(type)
    )
      throw new Error(`${path}: invalid root`);
    if (
      (parent === "table" && type !== "tr") ||
      (parent === "tr" && !["td", "th"].includes(type)) ||
      (parent === "code_block" && type !== "code_line")
    )
      throw new Error(`${path}: invalid structural child`);
    if (
      parent &&
      ["p", "h1", "h2", "h3", "blockquote", "code_line"].includes(parent) &&
      !["a", "mention"].includes(type)
    )
      throw new Error(`${path}: invalid inline child`);
    if (
      (parent === "td" || parent === "th") &&
      ["a", "mention", "tr", "td", "th", "code_line"].includes(type)
    )
      throw new Error(`${path}: invalid cell child`);
    if (parent === "code_line" || parent === "hr")
      throw new Error(`${path}: text-only node`);
    if (
      type === "hr" &&
      (n.children.length !== 1 ||
        !object(n.children[0]) ||
        Object.keys(n.children[0]).length !== 1 ||
        n.children[0].text !== "")
    )
      throw new Error(`${path}: invalid horizontal rule`);
    if ((type === "table" || type === "tr") && n.children.length > 100)
      throw new Error(`${path}: table limit exceeded`);
    if (
      type === "code_block" &&
      "lang" in n &&
      (typeof n.lang !== "string" || !/^[A-Za-z0-9_+.-]{0,64}$/.test(n.lang))
    )
      throw new Error(`${path}: invalid language`);
    if (parent === "a" || parent === "mention")
      throw new Error(`${path}: nested inline`);
    if (
      type === "a" &&
      (typeof n.url !== "string" ||
        !safeLink(n.url) ||
        [...n.url].length > 2048 ||
        ("target" in n && !["_blank", "_self"].includes(String(n.target))))
    )
      throw new Error(`${path}: invalid link`);
    if (
      type === "mention" &&
      (!["user", "issue", "project"].includes(String(n.target_kind)) ||
        typeof n.target_id !== "string" ||
        !uuid.test(n.target_id) ||
        n.target_id.replaceAll("-", "").toLowerCase() ===
          "00000000000000000000000000000000" ||
        n.key !== `${n.target_kind}:${n.target_id}` ||
        typeof n.value !== "string" ||
        [...n.value].length > 255 ||
        n.children.length !== 1 ||
        !object(n.children[0]) ||
        Object.keys(n.children[0]).length !== 1 ||
        n.children[0].text !== "")
    )
      throw new Error(`${path}: invalid mention`);
    for (const key of [
      "indent",
      "listStart",
      "listRestart",
      "colSpan",
      "rowSpan",
      "size",
      "marginLeft",
    ])
      if (
        key in n &&
        !finite(
          n[key],
          ["listStart", "listRestart", "colSpan", "rowSpan"].includes(key)
            ? 1
            : 0,
          key === "indent"
            ? 16
            : ["colSpan", "rowSpan"].includes(key)
              ? 100
              : ["listStart", "listRestart"].includes(key)
                ? 1000000
                : 10000,
          ["indent", "listStart", "listRestart", "colSpan", "rowSpan"].includes(
            key,
          ),
        )
      )
        throw new Error(`${path}: invalid ${key}`);
    if (
      "listStyleType" in n &&
      !["disc", "decimal", "todo"].includes(String(n.listStyleType))
    )
      throw new Error(`${path}: invalid list style`);
    if (
      ("listStart" in n || "listRestart" in n) &&
      n.listStyleType !== "decimal"
    )
      throw new Error(`${path}: numbering requires decimal list`);
    if ("checked" in n && n.listStyleType !== "todo")
      throw new Error(`${path}: checked requires todo list`);
    if ("checked" in n && typeof n.checked !== "boolean")
      throw new Error(`${path}: invalid checked`);
    for (const key of ["lang", "background"])
      if (
        key in n &&
        (typeof n[key] !== "string" ||
          (key === "background" && !safeColor(n[key] as string)))
      )
        throw new Error(`${path}: invalid ${key}`);
    if (
      "colSizes" in n &&
      (!Array.isArray(n.colSizes) ||
        n.colSizes.length === 0 ||
        n.colSizes.length > 100 ||
        n.colSizes.some((v) => !finite(v)))
    )
      throw new Error(`${path}: invalid colSizes`);
    if ("borders" in n) {
      if (!object(n.borders)) throw new Error(`${path}: invalid borders`);
      closed(n.borders, ["top", "right", "bottom", "left"], path);
      for (const b of Object.values(n.borders)) {
        if (!object(b)) throw new Error(`${path}: invalid border`);
        closed(b, ["color", "style", "size"], path);
        if (
          typeof b.color !== "string" ||
          !safeColor(b.color) ||
          !["solid", "dashed", "dotted", "none"].includes(String(b.style)) ||
          !finite(b.size)
        )
          throw new Error(`${path}: invalid border`);
      }
    }
    if (type === "table") {
      const rows = n.children as Record<string, unknown>[];
      const occupied: boolean[][] = rows.map(() => []);
      let width = 0;
      rows.forEach((row, r) => {
        if (!Array.isArray(row.children))
          throw new Error(`${path}: invalid row`);
        let c = 0;
        for (const cell of row.children as Record<string, unknown>[]) {
          while (occupied[r][c]) c++;
          const cs = Number(cell.colSpan ?? 1),
            rs = Number(cell.rowSpan ?? 1);
          if (
            !finite(cs, 1, 100, true) ||
            !finite(rs, 1, 100, true) ||
            r + rs > rows.length ||
            c + cs > 100
          )
            throw new Error(`${path}: invalid merged geometry`);
          for (let y = r; y < r + rs; y++)
            for (let x = c; x < c + cs; x++) {
              if (occupied[y][x])
                throw new Error(`${path}: overlapping merged cells`);
              occupied[y][x] = true;
            }
          c += cs;
          width = Math.max(width, c);
        }
      });
      if (
        !width ||
        occupied.some(
          (row) =>
            row.length !== width ||
            Array.from({ length: width }, (_, i) => row[i]).some((v) => !v),
        ) ||
        (Array.isArray(n.colSizes) && n.colSizes.length !== width)
      )
        throw new Error(`${path}: incomplete table geometry`);
    }
    n.children.forEach((child, i) =>
      visit(child, `${path}.children[${i}]`, type, depth + 1),
    );
  }
  value.forEach((node, i) => visit(node, `content[${i}]`, null, 1));
}
/** Reject numeric precision loss before JSON.parse can hide it from validation. */
export function assertExactJsonNumbers(raw: string) {
  const normalized = (token: string) => {
    const [mantissa, exponent = "0"] = token.toLowerCase().split("e"),
      negative = mantissa.startsWith("-"),
      parts = mantissa.replace(/^-/, "").split(".");
    let digits = parts.join("").replace(/^0+/, "");
    if (!digits) return "0";
    let power = Number(exponent) - (parts[1]?.length ?? 0);
    while (digits.endsWith("0")) {
      digits = digits.slice(0, -1);
      power++;
    }
    return (negative ? "-" : "") + digits + "e" + power;
  };
  const tokens = raw.matchAll(
    /"(?:[^"\\]|\\.)*"|(-?(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?)/g,
  );
  for (const token of tokens) {
    if (token[1] === undefined) continue;
    const value = Number(token[1]);
    if (
      !Number.isFinite(value) ||
      normalized(token[1]) !== normalized(JSON.stringify(value))
    )
      throw new Error(
        "Unsupported numeric precision; original import retained",
      );
  }
}
export function assertUniqueJsonKeys(raw: string) {
  const stack: { object: boolean; expectKey: boolean; keys: Set<string> }[] =
    [];
  for (const match of raw.matchAll(/"(?:[^"\\]|\\.)*"|[{}\[\],:]/g)) {
    const token = match[0],
      top = stack.at(-1);
    if (token === "{" || token === "[")
      stack.push({
        object: token === "{",
        expectKey: token === "{",
        keys: new Set(),
      });
    else if (token === "}" || token === "]") stack.pop();
    else if (token === "," && top?.object) top.expectKey = true;
    else if (token === ":" && top?.object) top.expectKey = false;
    else if (token.startsWith('"') && top?.object && top.expectKey) {
      const key = JSON.parse(token) as string;
      if (top.keys.has(key))
        throw new Error("Duplicate JSON property; original import retained");
      top.keys.add(key);
      top.expectKey = false;
    }
  }
}
export function parseCanonical(raw: string): CanonicalContent {
  assertExactJsonNumbers(raw);
  assertUniqueJsonKeys(raw);
  const v: unknown = JSON.parse(raw);
  if (!object(v)) throw new Error("Expected canonical document object");
  closed(v, ["format", "schema_version", "content"], "document");
  if (v.format !== "plate" || v.schema_version !== 1)
    throw new Error("Unsupported document format or schema version");
  validateContent(v.content);
  return v as CanonicalContent;
}
export function reseedSubtree<T extends Node[]>(
  nodes: T,
  createId = () => crypto.randomUUID(),
): T {
  const copy = structuredClone(nodes);
  function visit(n: Node) {
    if ("children" in n) {
      n.id = createId();
      n.children.forEach(visit);
    }
  }
  copy.forEach(visit);
  return copy;
}
export function paragraph(text = ""): Paragraph {
  return { id: crypto.randomUUID(), type: "p", children: [{ text }] };
}
export function canonical(content: Element[]): CanonicalContent {
  validateContent(content);
  return {
    format: "plate",
    schema_version: 1,
    content: structuredClone(content),
  };
}
export function sectionMembers<T extends Element>(
  content: T[],
  headingId: string,
) {
  const start = content.findIndex((n) => n.id === headingId);
  if (start < 0 || !/^h[123]$/.test(content[start].type))
    throw new Error("Select a top-level heading");
  const rank = Number(content[start].type.slice(1));
  let end = start + 1;
  while (
    end < content.length &&
    !(
      /^h[123]$/.test(content[end].type) &&
      Number(content[end].type.slice(1)) <= rank
    )
  )
    end++;
  return content.slice(start + 1, end);
}
export function plainText(nodes: Node[]): string {
  return nodes
    .map((node) => {
      if ("text" in node) return node.text;
      if (node.type === "mention") return "@" + node.value;
      if (node.type === "code_block")
        return node.children.map((n) => plainText(n.children)).join("\n");
      if (node.type === "table")
        return node.children
          .map((row) =>
            row.children.map((cell) => plainText(cell.children)).join("\t"),
          )
          .join("\n");
      return plainText(node.children);
    })
    .join(
      nodes.some(
        (n) =>
          "children" in n && !["a", "mention", "code_line"].includes(n.type),
      )
        ? "\n"
        : "",
    );
}
/** Derive list counters for rendering without mutating canonical list properties. */
export function listNumbers(content: Node[]): Map<string, number> {
  const output = new Map<string, number>();
  function visit(nodes: Node[]) {
    const counters = new Map<number, { style: unknown; value: number }>();
    for (const node of nodes) {
      if ("text" in node) continue;
      if (node.type === "p" && node.listStyleType) {
        const indent = Number(node.indent ?? 0);
        for (const key of counters.keys())
          if (key > indent) counters.delete(key);
        const previous = counters.get(indent);
        const value =
          node.listRestart ??
          node.listStart ??
          (previous?.style === "decimal" ? previous.value + 1 : 1);
        counters.set(indent, { style: node.listStyleType, value });
        if (node.listStyleType === "decimal") output.set(node.id, value);
      } else counters.clear();
      if (
        ![
          "p",
          "h1",
          "h2",
          "h3",
          "blockquote",
          "a",
          "mention",
          "code_line",
        ].includes(node.type)
      )
        visit(node.children);
    }
  }
  visit(content);
  return output;
}
export function parseClipboard(raw: string): Node[] {
  assertExactJsonNumbers(raw);
  assertUniqueJsonKeys(raw);
  const value: unknown = JSON.parse(raw);
  if (!object(value)) throw new Error("Invalid clipboard document");
  closed(value, ["format", "schema_version", "fragment"], "clipboard");
  if (
    value.format !== "plate" ||
    value.schema_version !== 1 ||
    !Array.isArray(value.fragment)
  )
    throw new Error("Unsupported clipboard document version");
  try {
    validateContent(value.fragment);
    return value.fragment;
  } catch {
    const wrapper: unknown = [{ ...paragraph(), children: value.fragment }];
    validateContent(wrapper);
    const first = wrapper[0];
    if (first.type !== "p") throw new Error("Invalid inline fragment");
    return first.children;
  }
}
