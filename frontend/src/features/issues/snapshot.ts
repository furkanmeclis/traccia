export type CursorPage<T> = {
  items?: T[] | null;
  has_more: boolean;
  next_cursor?: string | null;
};
// Selectors are writable only after all pages have been read successfully.
export async function collectIssues<
  T extends { id: string },
  P extends CursorPage<T>,
>(load: (after?: string) => Promise<P>): Promise<P & { items: T[] }> {
  const items: T[] = [];
  const ids = new Set<string>();
  const cursors = new Set<string>();
  let first: P | undefined;
  let after: string | undefined;
  do {
    const page = await load(after);
    if (!first) first = structuredClone(page);
    for (const field of ["project_version", "workflow_version"] as const) {
      if (Reflect.get(first, field) !== Reflect.get(page, field))
        throw new Error("ISSUE_SELECTION_CHANGED");
    }
    const firstCapabilities = Reflect.get(first, "capabilities");
    const pageCapabilities = Reflect.get(page, "capabilities");
    if (firstCapabilities && pageCapabilities) {
      for (const key of Object.keys(firstCapabilities)) {
        Reflect.set(
          firstCapabilities,
          key,
          Reflect.get(firstCapabilities, key) === true &&
            Reflect.get(pageCapabilities, key) === true,
        );
      }
    }
    for (const item of page.items ?? []) {
      if (!ids.has(item.id)) {
        ids.add(item.id);
        items.push(item);
      }
    }
    if (page.has_more && (!page.next_cursor || cursors.has(page.next_cursor)))
      throw new Error("INCOMPLETE_PAGINATION");
    if (page.next_cursor) cursors.add(page.next_cursor);
    after = page.has_more ? page.next_cursor! : undefined;
  } while (after);
  return { ...first!, items };
}
export function validIssueTitle(value: string) {
  const title = value.trim();
  return (
    Array.from(title).length > 0 &&
    Array.from(title).length <= 255 &&
    !/[\p{Cc}\p{Zl}\p{Zp}]/u.test(title)
  );
}
export function validIssueDates(start: string, target: string) {
  return !start || !target || start <= target;
}
export function validIssueEffort(value: string) {
  return value === "" || (/^\d+$/.test(value) && Number(value) <= 1000);
}
