import test from "node:test";
import assert from "node:assert/strict";
import { samePortfolioDefinition } from "./query-equivalence.ts";
import { emptyQuery } from "../searchviews/query.ts";
test("portfolio response pairing accepts declared UUID spelling/integer zero identity only and never rewrites canonical request text", () => {
  const id = "ABCDEF12-1234-1234-1234-123456789ABC",
    left = {
      resource: "issue",
      issue: {
        ...emptyQuery(),
        search: "  İ STRASSE e\u0301\r\n ",
        filter: {
          kind: "all",
          children: [
            { kind: "uuid", field: "id", op: "in", ids: [id] },
            { kind: "text", field: "title", op: "eq", value: id },
            { kind: "number", field: "effort", op: "eq", number: -0 },
          ],
        },
      },
      project: null,
      team: null,
    },
    before = structuredClone(left),
    right = structuredClone(left);
  right.issue.filter.children[0].ids[0] = id.toLowerCase();
  right.issue.filter.children[2].number = 0;
  assert(samePortfolioDefinition(left, right));
  assert.deepEqual(left, before);
  for (const change of [
    (value) => (value.issue.search = value.issue.search.trim()),
    (value) => (value.issue.filter.children[1].value = id.toLowerCase()),
    (value) => value.issue.filter.children.reverse(),
    (value) => (value.issue.filter.children[0].op = "not_in"),
    (value) =>
      value.issue.sort.push({ field: "id", direction: "desc", nulls: "last" }),
  ]) {
    const changed = structuredClone(right);
    change(changed);
    assert(!samePortfolioDefinition(left, changed));
  }
});
