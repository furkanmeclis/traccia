import type { CaptureSession } from "./capture-state.ts";
export type SnapshotPage<R extends { id: string }> = {
  session: CaptureSession;
  query: unknown;
  items: R[];
  has_more: boolean;
  next_cursor: string;
};
function sameJSON(a: unknown, b: unknown): boolean {
  if (a === b) return true;
  if (Array.isArray(a) || Array.isArray(b))
    return (
      Array.isArray(a) &&
      Array.isArray(b) &&
      a.length === b.length &&
      a.every((v, i) => sameJSON(v, b[i]))
    );
  if (typeof a !== "object" || !a || typeof b !== "object" || !b) return false;
  const left = a as Record<string, unknown>,
    right = b as Record<string, unknown>,
    keys = Object.keys(left);
  return (
    keys.length === Object.keys(right).length &&
    keys.every((k) => Object.hasOwn(right, k) && sameJSON(left[k], right[k]))
  );
}
/** Pages remain one server capture; repeated rows/cursors are errors, never silently dropped. */
export function collectSnapshotRows<R extends { id: string }>(
  pages: SnapshotPage<R>[],
): R[] {
  const first = pages[0];
  if (!first) return [];
  const rows: R[] = [],
    ids = new Set<string>(),
    cursors = new Set<string>();
  for (let index = 0; index < pages.length; index++) {
    const page = pages[index];
    if (
      !sameJSON(page.session, first.session) ||
      !sameJSON(page.query, first.query) ||
      !Array.isArray(page.items) ||
      typeof page.session.total_count !== "string" ||
      !/^(0|[1-9][0-9]*)$/.test(page.session.total_count)
    )
      throw new Error("SEARCH_PAGE_SNAPSHOT_CHANGED");
    if (page.has_more && (!page.next_cursor || cursors.has(page.next_cursor)))
      throw new Error("SEARCH_PAGE_CURSOR_INVALID");
    if (!page.has_more && page.next_cursor)
      throw new Error("SEARCH_PAGE_CURSOR_INVALID");
    if (index < pages.length - 1 && !page.has_more)
      throw new Error("SEARCH_PAGE_AFTER_END");
    if (page.has_more) cursors.add(page.next_cursor);
    for (const row of page.items) {
      if (!row || typeof row.id !== "string" || ids.has(row.id.toLowerCase()))
        throw new Error("SEARCH_PAGE_DUPLICATE_ROW");
      ids.add(row.id.toLowerCase());
      rows.push(row);
    }
  }
  if (
    BigInt(rows.length) > BigInt(first.session.total_count) ||
    (!pages.at(-1)!.has_more &&
      BigInt(rows.length) !== BigInt(first.session.total_count))
  )
    throw new Error("SEARCH_PAGE_COUNT_MISMATCH");
  return rows;
}
