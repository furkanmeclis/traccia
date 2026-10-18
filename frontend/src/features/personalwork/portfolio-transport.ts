import type { RequestBodyOption } from "openapi-fetch";
import type { components, operations } from "@/generated/api";
type Canonical = components["schemas"]["ProductPersonalPortfoliosCreateInput"];
type Transport = RequestBodyOption<
  operations["personal_portfolios_create"]
>["body"];
/** Keep the generated canonical null branches intact despite Writable's null misclassification. */
export function portfolioTransport(canonical: Canonical) {
  const snapshot = structuredClone(canonical),
    raw = JSON.stringify(snapshot),
    definition = snapshot.definition;
  function argument(): Transport {
    const common = {
      request_id: snapshot.request_id,
      workspace_ids: snapshot.workspace_ids,
      assigned_to_me: snapshot.assigned_to_me,
      ttl_seconds: snapshot.ttl_seconds,
      limit: snapshot.limit,
    };
    if (definition.resource === "issue")
      return {
        ...common,
        definition: { resource: "issue", issue: definition.issue },
      };
    if (definition.resource === "project")
      return {
        ...common,
        definition: { resource: "project", project: definition.project },
      };
    return {
      ...common,
      definition: { resource: "team", team: definition.team },
    };
  }
  return { body: argument(), bodySerializer: () => raw };
}
