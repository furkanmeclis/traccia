import type { SearchResource } from "./capture-state";
export type Comparison = "eq" | "lt" | "lte" | "gt" | "gte";
export type Filter =
  | { kind: "all" | "any"; children: Filter[] }
  | { kind: "not"; child: Filter }
  | { kind: "uuid"; field: string; op: "in" | "not_in"; ids: string[] }
  | { kind: "text"; field: string; op: "contains" | "eq"; value: string }
  | { kind: "enum"; field: "priority"; op: "in" | "not_in"; values: string[] }
  | { kind: "number"; field: "effort"; op: Comparison; number: number }
  | { kind: "date"; field: string; op: Comparison; value: string }
  | { kind: "null"; field: string; op: "is_null" | "is_not_null" };
export type Query = {
  version: 1;
  archived: "active" | "archived" | "any";
  search: string;
  filter: Filter | null;
  sort: { field: string; direction: "asc" | "desc"; nulls: "first" | "last" }[];
};
export type Column = { key: string; width: number };
export const fields = {
  issue: {
    uuid: [
      "id",
      "team_id",
      "status_id",
      "assignee_id",
      "project_id",
      "milestone_id",
      "label_ids",
    ],
    text: ["title", "identifier"],
    nullable: [
      "assignee_id",
      "project_id",
      "milestone_id",
      "start_date",
      "target_date",
      "effort",
    ],
    date: ["start_date", "target_date"],
    sort: [
      "id",
      "identifier",
      "title",
      "priority",
      "effort",
      "start_date",
      "target_date",
      "created_at",
      "updated_at",
    ],
    columns: [
      "identifier",
      "title",
      "team_id",
      "status_id",
      "priority",
      "assignee_id",
      "project_id",
      "milestone_id",
      "label_ids",
      "effort",
      "start_date",
      "target_date",
      "created_at",
      "updated_at",
      "archived",
    ],
  },
  project: {
    uuid: ["id", "lead_user_id", "team_ids"],
    text: ["name", "summary"],
    nullable: ["lead_user_id", "start_date", "target_date"],
    date: ["start_date", "target_date"],
    sort: [
      "id",
      "name",
      "start_date",
      "target_date",
      "created_at",
      "updated_at",
    ],
    columns: [
      "name",
      "summary",
      "lead_user_id",
      "team_ids",
      "start_date",
      "target_date",
      "created_at",
      "updated_at",
      "archived",
    ],
  },
  team: {
    uuid: ["id"],
    text: ["name", "code"],
    nullable: [],
    date: [],
    sort: ["id", "name", "code", "created_at", "updated_at"],
    columns: ["name", "code", "created_at", "updated_at", "archived"],
  },
} satisfies Record<SearchResource, Record<string, string[]>>;
export const priorities = ["none", "urgent", "high", "normal", "low"];
export const comparisons: Comparison[] = ["eq", "lt", "lte", "gt", "gte"];
export function emptyQuery(): Query {
  return { version: 1, archived: "active", search: "", filter: null, sort: [] };
}
function record(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
function keys(value: Record<string, unknown>, expected: string[]) {
  return (
    Object.keys(value).length === expected.length &&
    expected.every((k) => Object.hasOwn(value, k))
  );
}
function oneOf(value: unknown, allowed: readonly string[]): value is string {
  return typeof value === "string" && allowed.includes(value);
}
export function validText(value: unknown, max = 256): value is string {
  if (
    typeof value !== "string" ||
    value.includes("\0") ||
    Array.from(value).length > max
  )
    return false;
  for (let i = 0; i < value.length; i++) {
    const c = value.charCodeAt(i);
    if (c >= 0xd800 && c <= 0xdbff) {
      const next = value.charCodeAt(++i);
      if (!(next >= 0xdc00 && next <= 0xdfff)) return false;
    } else if (c >= 0xdc00 && c <= 0xdfff) return false;
  }
  return true;
}
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
function validDate(value: unknown) {
  if (
    typeof value !== "string" ||
    !/^\d{4}-\d{2}-\d{2}$/.test(value) ||
    value.startsWith("0000-")
  )
    return false;
  const date = new Date(value + "T00:00:00Z");
  return (
    Number.isFinite(date.getTime()) && date.toISOString().slice(0, 10) === value
  );
}
/** Closed wire validation rejects invalid drafts; it never edits their text or fields. */
export function validateQuery(
  resource: SearchResource,
  value: unknown,
): value is Query {
  if (
    !record(value) ||
    !keys(value, ["version", "archived", "search", "filter", "sort"]) ||
    value.version !== 1 ||
    !oneOf(value.archived, ["active", "archived", "any"]) ||
    !validText(value.search) ||
    !Array.isArray(value.sort) ||
    value.sort.length > 5
  )
    return false;
  const allowed = fields[resource],
    sorts = new Set<string>();
  for (const sort of value.sort) {
    if (
      !record(sort) ||
      !keys(sort, ["field", "direction", "nulls"]) ||
      !oneOf(sort.field, allowed.sort) ||
      sorts.has(sort.field) ||
      !oneOf(sort.direction, ["asc", "desc"]) ||
      !oneOf(sort.nulls, ["first", "last"])
    )
      return false;
    sorts.add(sort.field);
  }
  let count = 0;
  function walk(node: unknown, depth: number): boolean {
    if (!record(node) || ++count > 128 || depth > 8) return false;
    switch (node.kind) {
      case "all":
      case "any":
        return (
          keys(node, ["kind", "children"]) &&
          Array.isArray(node.children) &&
          node.children.length > 0 &&
          node.children.length <= 128 &&
          node.children.every((child) => walk(child, depth + 1))
        );
      case "not":
        return keys(node, ["kind", "child"]) && walk(node.child, depth + 1);
      case "uuid":
        return (
          keys(node, ["kind", "field", "op", "ids"]) &&
          oneOf(node.field, allowed.uuid) &&
          oneOf(node.op, ["in", "not_in"]) &&
          Array.isArray(node.ids) &&
          node.ids.length > 0 &&
          node.ids.length <= 100 &&
          node.ids.every(
            (id) =>
              typeof id === "string" &&
              uuid.test(id) &&
              id.replaceAll("-", "") !== "0".repeat(32),
          ) &&
          new Set(node.ids.map((id) => id.toLowerCase())).size ===
            node.ids.length
        );
      case "text":
        return (
          keys(node, ["kind", "field", "op", "value"]) &&
          oneOf(node.field, allowed.text) &&
          oneOf(node.op, ["contains", "eq"]) &&
          validText(node.value) &&
          node.value.length > 0
        );
      case "enum":
        return (
          resource === "issue" &&
          keys(node, ["kind", "field", "op", "values"]) &&
          node.field === "priority" &&
          oneOf(node.op, ["in", "not_in"]) &&
          Array.isArray(node.values) &&
          node.values.length > 0 &&
          node.values.length <= 100 &&
          node.values.every((v) => oneOf(v, priorities)) &&
          new Set(node.values).size === node.values.length
        );
      case "number":
        return (
          resource === "issue" &&
          keys(node, ["kind", "field", "op", "number"]) &&
          node.field === "effort" &&
          oneOf(node.op, comparisons) &&
          typeof node.number === "number" &&
          Number.isInteger(node.number) &&
          node.number >= -2147483648 &&
          node.number <= 2147483647
        );
      case "date":
        return (
          keys(node, ["kind", "field", "op", "value"]) &&
          oneOf(node.field, allowed.date) &&
          oneOf(node.op, comparisons) &&
          validDate(node.value)
        );
      case "null":
        return (
          keys(node, ["kind", "field", "op"]) &&
          oneOf(node.field, allowed.nullable) &&
          oneOf(node.op, ["is_null", "is_not_null"])
        );
      default:
        return false;
    }
  }
  return value.filter === null || walk(value.filter, 1);
}
export function validateColumns(
  resource: SearchResource,
  value: unknown,
): value is Column[] {
  return (
    Array.isArray(value) &&
    value.length > 0 &&
    value.length <= fields[resource].columns.length &&
    value.every(
      (c) =>
        record(c) &&
        keys(c, ["key", "width"]) &&
        oneOf(c.key, fields[resource].columns) &&
        typeof c.width === "number" &&
        Number.isInteger(c.width) &&
        c.width >= 80 &&
        c.width <= 800,
    ) &&
    new Set(value.map((c) => c.key)).size === value.length
  );
}
export function defaultColumns(resource: SearchResource): Column[] {
  const chosen =
    resource === "issue"
      ? ["identifier", "title", "priority", "status_id"]
      : resource === "project"
        ? ["name", "summary", "target_date"]
        : ["name", "code"];
  return chosen.map((key) => ({
    key,
    width: key === "title" || key === "summary" ? 320 : 180,
  }));
}
