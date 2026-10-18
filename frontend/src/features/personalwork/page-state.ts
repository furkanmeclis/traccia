import { queryEqual } from "../searchviews/draft-state.ts";
import type {
  PortfolioSessionShape,
  PersonalResource,
} from "./portfolio-state.ts";
type Reference = { id: string; workspace_id: string };
export type PortfolioRowShape = {
  workspace_id: string;
  resource: PersonalResource;
  issue: Reference | null;
  project: Reference | null;
  team: Reference | null;
};
type PortfolioPage<R extends PortfolioRowShape> = {
  session: PortfolioSessionShape;
  definition: unknown;
  assigned_to_me: boolean;
  items: R[];
  has_more: boolean;
  next_cursor: string;
};
export function portfolioReference(row: PortfolioRowShape) {
  const reference =
    row.resource === "issue"
      ? row.issue
      : row.resource === "project"
        ? row.project
        : row.team;
  if (
    !reference ||
    row.workspace_id.toLowerCase() !== reference.workspace_id.toLowerCase() ||
    (row.resource !== "issue" && row.issue !== null) ||
    (row.resource !== "project" && row.project !== null) ||
    (row.resource !== "team" && row.team !== null)
  )
    throw new Error("PORTFOLIO_ROW_INVALID");
  return reference;
}
export function collectPortfolioRows<R extends PortfolioRowShape>(
  pages: PortfolioPage<R>[],
) {
  const first = pages[0];
  if (!first) return [];
  const rows: R[] = [],
    ids = new Set<string>(),
    cursors = new Set<string>();
  const scopes = new Set(
    first.session.workspaces.map((scope) => scope.id.toLowerCase()),
  );
  for (let index = 0; index < pages.length; index++) {
    const page = pages[index];
    if (
      !queryEqual(page.session, first.session) ||
      !queryEqual(page.definition, first.definition) ||
      page.assigned_to_me !== first.assigned_to_me
    )
      throw new Error("PORTFOLIO_PAGE_CHANGED");
    if (
      (page.has_more && (!page.next_cursor || cursors.has(page.next_cursor))) ||
      (!page.has_more && page.next_cursor) ||
      (index < pages.length - 1 && !page.has_more)
    )
      throw new Error("PORTFOLIO_PAGE_CURSOR");
    if (page.next_cursor) cursors.add(page.next_cursor);
    for (const row of page.items) {
      const reference = portfolioReference(row),
        key = JSON.stringify([
          row.workspace_id.toLowerCase(),
          row.resource,
          reference.id.toLowerCase(),
        ]);
      if (
        !scopes.has(row.workspace_id.toLowerCase()) ||
        row.resource !== first.session.resource ||
        ids.has(key)
      )
        throw new Error("PORTFOLIO_ROW_INVALID");
      ids.add(key);
      rows.push(row);
    }
  }
  if (
    !/^(0|[1-9][0-9]*)$/.test(first.session.total_count) ||
    BigInt(rows.length) > BigInt(first.session.total_count) ||
    (!pages.at(-1)!.has_more &&
      BigInt(rows.length) !== BigInt(first.session.total_count))
  )
    throw new Error("PORTFOLIO_PAGE_COUNT");
  return rows;
}
