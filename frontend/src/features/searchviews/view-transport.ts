import type { RequestBodyOption } from "openapi-fetch";
import type { components, operations } from "@/generated/api";
import { validateColumns, type Column } from "./query.ts";
type Canonical = components["schemas"]["ProductSavedViewsCreateInput"];
type Transport = RequestBodyOption<operations["saved_views_create"]>["body"];
type IssueTransport = Extract<Transport, { definition: { resource: "issue" } }>;
type ProjectTransport = Extract<
  Transport,
  { definition: { resource: "project" } }
>;
type TeamTransport = Extract<Transport, { definition: { resource: "team" } }>;
function issueColumns(columns: Column[]): columns is IssueTransport["columns"] {
  return validateColumns("issue", columns);
}
function projectColumns(
  columns: Column[],
): columns is ProjectTransport["columns"] {
  return validateColumns("project", columns);
}
function teamColumns(columns: Column[]): columns is TeamTransport["columns"] {
  return validateColumns("team", columns);
}
/**
 * openapi-fetch 0.17's Writable helper misclassifies exactly-null properties as
 * read-only. This argument only satisfies that helper. The public serializer
 * emits the frozen canonical JSON, including every required inactive null slot.
 */
export function viewTransport(canonical: Canonical) {
  const snapshot = structuredClone(canonical),
    raw = JSON.stringify(snapshot);
  function argument(): Transport {
    const definition = snapshot.definition,
      columns = snapshot.columns,
      name = snapshot.name;
    if (definition.resource === "issue" && issueColumns(columns))
      return {
        name,
        definition: { resource: "issue", issue: definition.issue },
        columns,
      };
    if (definition.resource === "project" && projectColumns(columns))
      return {
        name,
        definition: { resource: "project", project: definition.project },
        columns,
      };
    if (definition.resource === "team" && teamColumns(columns))
      return {
        name,
        definition: { resource: "team", team: definition.team },
        columns,
      };
    throw new Error("SEARCH_INVALID_COLUMNS");
  }
  return { body: argument(), bodySerializer: () => raw };
}
export function updateViewTransport(
  canonical: components["schemas"]["ProductSavedViewsUpdateInput"],
) {
  const snapshot = structuredClone(canonical),
    transport = viewTransport(snapshot),
    raw = JSON.stringify(snapshot);
  return {
    body: { ...transport.body, expected_revision: snapshot.expected_revision },
    bodySerializer: () => raw,
  };
}
