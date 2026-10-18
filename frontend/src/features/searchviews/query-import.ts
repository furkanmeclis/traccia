import {
  assertExactJsonNumbers,
  assertUniqueJsonKeys,
} from "../documents/shared/contract.ts";
import { validateQuery, type Query } from "./query.ts";
import type { SearchResource } from "./capture-state.ts";
export const maxRawInputBytes = 1048576;
export const maxDomainInputBytes = 131072;
export function withinDomainBudget(value: unknown) {
  return (
    new TextEncoder().encode(JSON.stringify(value)).byteLength <=
    maxDomainInputBytes
  );
}
export function parseQuery(resource: SearchResource, raw: string): Query {
  if (new TextEncoder().encode(raw).byteLength > maxRawInputBytes)
    throw new Error("SEARCH_QUERY_BYTES");
  assertUniqueJsonKeys(raw);
  assertExactJsonNumbers(raw);
  const value: unknown = JSON.parse(raw);
  if (!validateQuery(resource, value)) throw new Error("SEARCH_INVALID_QUERY");
  if (!withinDomainBudget(value)) throw new Error("SEARCH_QUERY_BYTES");
  return value;
}

export function importQueryDraft(
  resource: SearchResource,
  raw: string,
):
  | { query: Query; error: null }
  | { query: null; error: "searchviews.invalid" } {
  try {
    return { query: parseQuery(resource, raw), error: null };
  } catch {
    return { query: null, error: "searchviews.invalid" };
  }
}
