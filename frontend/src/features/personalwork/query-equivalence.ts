import { validateQuery, type Filter } from "../searchviews/query.ts";
import { queryEqual } from "../searchviews/draft-state.ts";
import type { ViewDefinition } from "../searchviews/search.service";
function filterIdentity(filter: Filter | null): Filter | null {
  if (!filter) return null;
  if (filter.kind === "uuid")
    return { ...filter, ids: filter.ids.map((id) => id.toLowerCase()) };
  if (filter.kind === "all" || filter.kind === "any")
    return {
      ...filter,
      children: filter.children.map((child) => filterIdentity(child)!),
    };
  if (filter.kind === "not")
    return { ...filter, child: filterIdentity(filter.child)! };
  return { ...filter };
}
function identity(definition: ViewDefinition) {
  const query = definition[definition.resource];
  if (
    !query ||
    !validateQuery(definition.resource, query) ||
    (["issue", "project", "team"] as const).some(
      (resource) =>
        resource !== definition.resource && definition[resource] !== null,
    )
  )
    throw new Error("PORTFOLIO_RESPONSE_QUERY");
  return {
    ...definition,
    [definition.resource]: { ...query, filter: filterIdentity(query.filter) },
  };
}
/** Compare declared UUID identities; preserve every original body/text spelling. */
export function samePortfolioDefinition(
  left: ViewDefinition,
  right: ViewDefinition,
) {
  return queryEqual(identity(left), identity(right));
}
