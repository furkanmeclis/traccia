import test from "node:test";
import assert from "node:assert/strict";
import { parseQuery, importQueryDraft } from "./query-import.ts";
import { emptyQuery } from "./query.ts";
test("advanced query import rejects duplicate keys, unsupported number precision, NUL and unknown fields while preserving exact accepted text/UUIDs", () => {
  const q = {
    ...emptyQuery(),
    search: "  İ e\u0301 👩🏽‍💻\r\n ",
    filter: {
      kind: "uuid",
      field: "id",
      op: "in",
      ids: ["ABCDEF12-1234-1234-1234-123456789ABC"],
    },
  };
  assert.deepEqual(parseQuery("issue", JSON.stringify(q)), q);
  for (const raw of [
    '{"version":1,"version":1,"archived":"active","search":"","filter":null,"sort":[]}',
    JSON.stringify({
      ...emptyQuery(),
      filter: { kind: "number", field: "effort", op: "eq", number: 2147483647 },
    }).replace("2147483647", "2147483647.00000001"),
    JSON.stringify({ ...emptyQuery(), search: "\0" }),
    JSON.stringify({ ...emptyQuery(), unknown: null }),
  ])
    assert.throws(() => parseQuery("issue", raw));
});

test("new raw1MiB guard accepts genuinely escaped semantically small JSON, but rejects canonical128KiB overflow without trimming parsed data", () => {
  const fixtureIDs = Array.from(
    { length: 100 },
    (_, index) =>
      "abcdef12-1234-1234-1234-" + index.toString(16).padStart(12, "0"),
  );
  const q = {
    ...emptyQuery(),
    search: "<>&\u2028\u2029".repeat(50),
    filter: {
      kind: "all",
      children: Array.from({ length: 10 }, () => ({
        kind: "uuid",
        field: "id",
        op: "in",
        ids: fixtureIDs,
      })),
    },
  };
  const canonical = JSON.stringify(q),
    raw = canonical.replace(/"(?:[^"\\]|\\.)*"/g, (token) =>
      token.replace(
        /[A-Za-z0-9]/g,
        (char) => "\\u" + char.charCodeAt(0).toString(16).padStart(4, "0"),
      ),
    );
  assert(raw.length > 131072 && raw.length < 1048576);
  assert(canonical.length < 131072);
  assert.deepEqual(parseQuery("issue", raw), q);
  const id = "abcdef12-1234-1234-1234-123456789abc",
    leaf = {
      kind: "uuid",
      field: "id",
      op: "in",
      ids: Array.from(
        { length: 100 },
        (_, i) => id.slice(0, -8) + i.toString(16).padStart(8, "0"),
      ),
    },
    overflow = {
      ...emptyQuery(),
      filter: {
        kind: "all",
        children: Array.from({ length: 40 }, () => structuredClone(leaf)),
      },
    };
  const oversized = JSON.stringify(overflow);
  assert(oversized.length > 131072 && oversized.length < 1048576);
  assert.throws(() => parseQuery("issue", oversized), /QUERY_BYTES/);
  assert.throws(
    () =>
      parseQuery("issue", JSON.stringify(emptyQuery()) + " ".repeat(1048576)),
    /QUERY_BYTES/,
  );
  assert.equal(overflow.filter.children[0].ids[0], leaf.ids[0]);
});

import { readFileSync } from "node:fs";
import vm from "node:vm";
import ts from "typescript";
const translated = { exports: {} };
vm.runInNewContext(
  ts.transpileModule(
    readFileSync(
      new URL("../../lib/i18n/messages.ts", import.meta.url),
      "utf8",
    ),
    {
      compilerOptions: {
        module: ts.ModuleKind.CommonJS,
        esModuleInterop: true,
      },
    },
  ).outputText,
  {
    module: translated,
    exports: translated.exports,
    process: { env: { NODE_ENV: "production" } },
    require(name) {
      return JSON.parse(
        readFileSync(
          new URL("../../" + name.slice(2), import.meta.url),
          "utf8",
        ),
      );
    },
  },
);
const { translate } = translated.exports;
test("actual raw import result localizes malformed JSON, closed-domain and byte errors without changing input", () => {
  const invalid = [
    "{broken",
    JSON.stringify({ ...emptyQuery(), unknown: true }),
    " ".repeat(1048577),
  ];
  for (const raw of invalid) {
    const before = raw;
    const result = importQueryDraft("issue", raw);
    assert.deepEqual(result, { query: null, error: "searchviews.invalid" });
    assert.equal(raw, before);
    for (const locale of ["tr", "en"]) {
      const messages = JSON.parse(
        readFileSync(
          new URL(`../../locales/${locale}/searchviews.json`, import.meta.url),
        ),
      );
      const copy = translate(locale, result.error);
      assert.equal(copy, messages.invalid);
      assert(
        !/SEARCH_|SyntaxError|position|Unexpected|Expected property/.test(copy),
      );
    }
  }
  const raw = JSON.stringify({ ...emptyQuery(), search: "  İ e\u0301 \r\n" });
  assert.deepEqual(importQueryDraft("issue", raw), {
    query: JSON.parse(raw),
    error: null,
  });
});
