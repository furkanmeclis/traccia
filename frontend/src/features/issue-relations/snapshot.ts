type Page<T> = {
  items?: T[] | null;
  has_more: boolean;
  next_cursor?: string | null;
};
// A selector becomes writable only after its entire snapshot has loaded.
export async function collectRelationPages<
  T extends { id: string },
  P extends Page<T>,
>(
  load: (after?: string) => Promise<P>,
  revision: (page: P) => unknown,
  permissions: (page: P) => Record<string, boolean> | undefined = (page) =>
    Reflect.get(page, "capabilities") as Record<string, boolean> | undefined,
) {
  const items: T[] = [];
  const ids = new Set<string>();
  const cursors = new Set<string>();
  let first: P | undefined;
  let after: string | undefined;
  do {
    const page = await load(after);
    if (first && revision(first) !== revision(page))
      throw new Error("RELATION_SNAPSHOT_CHANGED");
    first ??= structuredClone(page);
    const capabilities = permissions(first);
    const current = permissions(page);
    if (capabilities && current) {
      for (const key of Object.keys(capabilities))
        Reflect.set(
          capabilities,
          key,
          Reflect.get(capabilities, key) === true &&
            Reflect.get(current, key) === true,
        );
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

export function expectedVersions(
  refs: { id: string; relation_version: number }[],
) {
  const values = new Map<string, number>();
  for (const ref of refs) {
    if (values.has(ref.id) && values.get(ref.id) !== ref.relation_version)
      throw Error("RELATION_SNAPSHOT_CHANGED");
    values.set(ref.id, ref.relation_version);
  }
  return [...values]
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([issue_id, expected_version]) => ({ issue_id, expected_version }));
}
