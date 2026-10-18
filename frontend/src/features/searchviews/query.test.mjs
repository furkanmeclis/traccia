import test from "node:test";
import assert from "node:assert/strict";
import {
  emptyQuery,
  validateQuery,
  validateColumns,
  defaultColumns,
  validText,
} from "./query.ts";
const id = "abcdef12-1234-1234-1234-123456789abc";
const query = (filter) => ({ ...emptyQuery(), filter });
test("closed recursive query preserves whitespace, Unicode, CRLF and explicit nullable filters without normalization", () => {
  const q = {
    ...emptyQuery(),
    search: "  İ e\u0301 👩🏽‍💻\r\n ",
    filter: {
      kind: "all",
      children: [
        {
          kind: "not",
          child: { kind: "null", field: "effort", op: "is_null" },
        },
        {
          kind: "any",
          children: [
            { kind: "number", field: "effort", op: "gte", number: 0 },
            { kind: "text", field: "title", op: "contains", value: "  " },
          ],
        },
      ],
    },
    sort: [{ field: "target_date", direction: "asc", nulls: "first" }],
  };
  const before = structuredClone(q);
  assert(validateQuery("issue", q));
  assert.deepEqual(q, before);
  assert(validateQuery("team", emptyQuery()));
});
test("each concrete filter branch rejects unknown placeholder fields and cross-resource field/operator combinations", () => {
  const filters = [
    { kind: "uuid", field: "team_id", op: "in", ids: [id] },
    { kind: "text", field: "title", op: "eq", value: "x" },
    {
      kind: "enum",
      field: "priority",
      op: "not_in",
      values: ["none", "urgent"],
    },
    { kind: "number", field: "effort", op: "eq", number: -2147483648 },
    { kind: "date", field: "start_date", op: "lte", value: "2024-02-29" },
    { kind: "null", field: "assignee_id", op: "is_not_null" },
    {
      kind: "not",
      child: { kind: "text", field: "title", op: "eq", value: "x" },
    },
    {
      kind: "all",
      children: [{ kind: "uuid", field: "id", op: "in", ids: [id] }],
    },
  ];
  for (const filter of filters) {
    assert(validateQuery("issue", query(filter)));
    assert(!validateQuery("issue", query({ ...filter, irrelevant: null })));
  }
  for (const filter of filters.slice(0, 6))
    assert(!validateQuery("team", query(filter)));
  assert(
    !validateQuery(
      "issue",
      query({ kind: "text", field: "title", op: "in", value: "x" }),
    ),
  );
  assert(!validateQuery("issue", { ...emptyQuery(), unexpected: 1 }));
});
test("exact filter budgets, Unicode scalar text, UUID identity, int32 and real calendar dates reject rather than trim or round", () => {
  assert(validText("😀".repeat(256)));
  assert(!validText("😀".repeat(257)));
  for (const text of ["\0", "\ud800", "\udc00"])
    assert(!validateQuery("issue", { ...emptyQuery(), search: text }));
  assert(validText("\\u0000"));
  assert(validText("\t\r\n"));
  for (const ids of [
    [id, id.toUpperCase()],
    ["00000000-0000-0000-0000-000000000000"],
    Array.from({ length: 101 }, () => crypto.randomUUID()),
  ])
    assert(
      !validateQuery(
        "issue",
        query({ kind: "uuid", field: "id", op: "in", ids }),
      ),
    );
  for (const number of [2147483648, -2147483649, 0.1, NaN, Infinity])
    assert(
      !validateQuery(
        "issue",
        query({ kind: "number", field: "effort", op: "eq", number }),
      ),
    );
  for (const value of [
    "0000-01-01",
    "2023-02-29",
    "2024-02-30",
    "2024-2-01",
    "2024-01-01T00:00:00Z",
  ])
    assert(
      !validateQuery(
        "issue",
        query({ kind: "date", field: "start_date", op: "eq", value }),
      ),
    );
  let nested = { kind: "text", field: "title", op: "eq", value: "x" };
  for (let i = 0; i < 7; i++) nested = { kind: "not", child: nested };
  assert(validateQuery("issue", query(nested)));
  assert(!validateQuery("issue", query({ kind: "not", child: nested })));
  const leaf = { kind: "null", field: "effort", op: "is_null" };
  assert(
    validateQuery(
      "issue",
      query({ kind: "all", children: Array(127).fill(leaf) }),
    ),
  );
  assert(
    !validateQuery(
      "issue",
      query({ kind: "all", children: Array(128).fill(leaf) }),
    ),
  );
});
test("sort order is authoritative, duplicate fields and nonclosed values fail; ordered columns retain explicit widths and resource boundaries", () => {
  const q = {
    ...emptyQuery(),
    sort: [
      { field: "title", direction: "asc", nulls: "last" },
      { field: "id", direction: "desc", nulls: "first" },
    ],
  };
  assert(validateQuery("issue", q));
  assert(!validateQuery("issue", { ...q, sort: [q.sort[0], q.sort[0]] }));
  for (const resource of ["issue", "project", "team"])
    assert(validateColumns(resource, defaultColumns(resource)));
  const cols = [
    { key: "title", width: 800 },
    { key: "identifier", width: 80 },
  ];
  const before = structuredClone(cols);
  assert(validateColumns("issue", cols));
  assert.deepEqual(cols, before);
  for (const cols of [
    [],
    [{ key: "title", width: 79 }],
    [{ key: "title", width: 801 }],
    [{ key: "title", width: 100.5 }],
    [
      { key: "title", width: 100 },
      { key: "title", width: 200 },
    ],
    [{ key: "title", width: 100, hidden: false }],
  ])
    assert(!validateColumns("issue", cols));
  assert(!validateColumns("team", [{ key: "title", width: 100 }]));
});
