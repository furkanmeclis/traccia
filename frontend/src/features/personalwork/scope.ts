export type ProductScope = {
  workspaceID: string | null;
  teamID: string | null;
  projectID: string | null;
  issueID: string | null;
};
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
export const validID = (value: string) =>
  uuid.test(value) &&
  value.toLowerCase() !== "00000000-0000-0000-0000-000000000000";
export function productScope(pathname: string): ProductScope {
  const parts = pathname.split("/");
  const empty = {
    workspaceID: null,
    teamID: null,
    projectID: null,
    issueID: null,
  };
  if (parts[1] !== "w" || !validID(parts[2] ?? "")) return empty;
  const workspaceID = parts[2],
    id = validID(parts[4] ?? "") ? parts[4] : null;
  return {
    workspaceID,
    teamID: parts[3] === "teams" ? id : null,
    projectID: parts[3] === "projects" ? id : null,
    issueID: parts[3] === "issues" ? id : null,
  };
}
export function personalScope(actorID: string, workspaceID: string | null) {
  if (!validID(actorID) || (workspaceID !== null && !validID(workspaceID)))
    throw new Error("PERSONAL_SCOPE_INVALID");
  return JSON.stringify([
    actorID.toLowerCase(),
    workspaceID?.toLowerCase() ?? null,
  ]);
}
export async function collectSelector<T extends { id: string }>(
  load: (after?: string) => Promise<{
    items?: T[] | null;
    has_more: boolean;
    next_cursor?: string | null;
  }>,
  check: (item: T) => boolean = () => true,
) {
  const items: T[] = [],
    ids = new Set<string>(),
    cursors = new Set<string>();
  let after: string | undefined;
  do {
    const page = await load(after);
    for (const item of page.items ?? []) {
      const id = item.id.toLowerCase();
      if (!validID(item.id) || ids.has(id) || !check(item))
        throw new Error("PERSONAL_SELECTOR_CHANGED");
      ids.add(id);
      items.push(item);
    }
    if (page.has_more && (!page.next_cursor || cursors.has(page.next_cursor)))
      throw new Error("PERSONAL_SELECTOR_INCOMPLETE");
    if (page.next_cursor) cursors.add(page.next_cursor);
    after = page.has_more ? page.next_cursor! : undefined;
  } while (after);
  return items;
}
