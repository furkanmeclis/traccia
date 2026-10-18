import test from "node:test";
import assert from "node:assert/strict";
import {
  canonical,
  paragraph,
  parseCanonical,
  reseedSubtree,
  sectionMembers,
  validateContent,
  listNumbers,
  parseClipboard,
} from "./contract.ts";
import { documentFixture as fixture } from "./fixtures.ts";
const e = (type, children, props = {}) => ({
  id: crypto.randomUUID(),
  type,
  children,
  ...props,
});
test("canonical roundtrip preserves whitespace, false flags, merged layout and every ID", () => {
  const v = fixture();
  assert.deepEqual(parseCanonical(JSON.stringify(canonical(v))).content, v);
});
test("closed imports reject unknown fields, versions and duplicate/leaf IDs without modifying original", () => {
  const v = fixture();
  const before = structuredClone(v);
  assert.throws(() =>
    parseCanonical(JSON.stringify({ ...canonical(v), schema_version: 2 })),
  );
  for (const mutate of [
    (x) => (x[0].unknown = true),
    (x) => (x[1].id = x[0].id),
    (x) => (x[0].children[0].id = crypto.randomUUID()),
    (x) => (x[0].type = "unknown"),
  ]) {
    const x = structuredClone(v);
    mutate(x);
    assert.throws(() => validateContent(x));
  }
  assert.deepEqual(v, before);
});
test("paste explicitly remaps all element IDs and leaves references/text intact", () => {
  const v = fixture();
  const copy = reseedSubtree(v);
  validateContent(copy);
  const ids = (x) =>
    x.flatMap((n) => ("children" in n ? [n.id, ...ids(n.children)] : []));
  assert.ok(ids(copy).every((id) => !ids(v).includes(id)));
  const strip = (x) =>
    x.map((n) =>
      "children" in n ? { ...n, id: null, children: strip(n.children) } : n,
    );
  assert.deepEqual(strip(copy), strip(v));
});
test("section boundaries use ordered top-level members and preserve heading", () => {
  const h = e("h1", [{ text: "start" }]),
    a = paragraph("a"),
    sub = e("h2", [{ text: "sub" }]),
    b = paragraph("b"),
    end = e("h1", [{ text: "next" }]);
  assert.deepEqual(
    sectionMembers([h, a, sub, b, end], h.id).map((x) => x.id),
    [a.id, sub.id, b.id],
  );
});
test("closed structural unions reject malformed cells, code lines, voids and empty colSizes", () => {
  const el = (type, children, props = {}) => ({
    id: crypto.randomUUID(),
    type,
    children,
    ...props,
  });
  for (const value of [
    [
      el("table", [
        el("tr", [
          el("td", [
            el("a", [{ text: "link" }], { url: "https://example.com" }),
          ]),
        ]),
      ]),
    ],
    [
      el("code_block", [
        el("code_line", [
          el("mention", [{ text: "" }], {
            key: "issue:00000000-0000-4000-8000-000000000001",
            value: "x",
            target_kind: "issue",
            target_id: "00000000-0000-4000-8000-000000000001",
          }),
        ]),
      ]),
    ],
    [el("hr", [{ text: "not empty" }])],
    [el("table", [el("tr", [el("td", [paragraph()])])], { colSizes: [] })],
  ])
    assert.throws(() => validateContent(value));
});
test("UUID duplicate identity is case insensitive without rewriting accepted ID strings", () => {
  const first = paragraph("a"),
    second = paragraph("b");
  first.id = "abcdefab-1234-4234-8234-abcdefabcdef";
  second.id = first.id.toUpperCase();
  assert.throws(() => validateContent([first, second]), /duplicate/);
  validateContent([second]);
  assert.equal(second.id, "ABCDEFAB-1234-4234-8234-ABCDEFABCDEF");
});
test("safe colors reject CSS injection while preserving original accepted spelling", () => {
  for (const color of [
    "#AbC",
    "#1234",
    "#aabbcc",
    "#AABBCCDD",
    "transparent",
    "currentColor",
  ]) {
    const v = fixture();
    v[2].children[0].children[0].background = color;
    validateContent(v);
  }
  for (const color of [
    "red; color:black",
    "url(https://example.com)",
    "var(--x)",
    "red",
    "Transparent",
  ]) {
    const v = fixture();
    v[2].children[0].children[0].background = color;
    assert.throws(() => validateContent(v));
  }
});
test("table occupancy admits fully covered rows and rejects holes, overlaps and mismatched widths", () => {
  const cell = (props = {}) => e("td", [paragraph()], props);
  validateContent([
    e("table", [e("tr", [cell({ rowSpan: 2 })]), e("tr", [])], {
      colSizes: [160],
    }),
  ]);
  for (const table of [
    e("table", [e("tr", [])]),
    e("table", [e("tr", [cell({ rowSpan: 2 })])]),
    e("table", [e("tr", [cell(), cell()]), e("tr", [cell()])]),
    e("table", [e("tr", [cell()])], { colSizes: [100, 100] }),
  ])
    assert.throws(() => validateContent([table]));
});
test("semantic bounds match backend language, mention, link and list contracts", () => {
  const valid = paragraph();
  valid.listStyleType = "decimal";
  valid.listStart = 1000000;
  valid.listRestart = 1000000;
  validateContent([valid]);
  valid.listStart = 1000001;
  assert.throws(() => validateContent([valid]));
  const code = e("code_block", [e("code_line", [{ text: "" }])], {
    lang: "C++",
  });
  validateContent([code]);
  code.lang = "x".repeat(65);
  assert.throws(() => validateContent([code]));
});
test("raw JSON imports reject unsupported numeric precision instead of rounding retained text", () => {
  const value = fixture();
  value[2].children[0].children[0].size = 0.1;
  const valid = JSON.stringify(canonical(value));
  assert.deepEqual(parseCanonical(valid).content, value);
  const invalid = valid.replace('"size":0.1', '"size":0.10000000000000001');
  assert.notEqual(invalid, valid);
  assert.throws(() => parseCanonical(invalid), /numeric precision/);
  const exponent = valid.replace('"size":0.1', '"size":1e-1');
  assert.deepEqual(parseCanonical(exponent).content, value);
});
test("ordered list rendering derives continuation/restart counters without rewriting JSON", () => {
  const p = (text, props = {}) => ({
      ...paragraph(text),
      listStyleType: "decimal",
      indent: 1,
      ...props,
    }),
    a = p("a"),
    b = p("b"),
    c = p("c", { indent: 2, listStart: 5 }),
    d = p("d"),
    restart = p("restart", { listRestart: 9 });
  const v = [a, b, c, d, restart],
    before = structuredClone(v);
  const numbers = listNumbers(v);
  assert.deepEqual(
    v.map((x) => numbers.get(x.id)),
    [1, 2, 5, 3, 9],
  );
  assert.deepEqual(v, before);
});
test("unpaired Unicode surrogate imports are rejected without changing valid emoji and combining text", () => {
  for (const text of [
    String.fromCharCode(0xd800),
    String.fromCharCode(0xdc00),
    "a" + String.fromCharCode(0xd800) + "b",
  ]) {
    const raw = JSON.stringify({
      format: "plate",
      schema_version: 1,
      content: [paragraph(text)],
    });
    assert.throws(() => parseCanonical(raw), /surrogate/);
  }
  validateContent([paragraph("İ 👩🏽‍💻 e\u0301\r\n")]);
});
test("raw imports reject duplicate properties including escaped aliases instead of silently overwriting", () => {
  const raw = JSON.stringify(canonical([paragraph("retained")]));
  assert.throws(
    () =>
      parseCanonical(
        raw.replace(
          '"schema_version":1',
          '"schema_version":2,"schema_version":1',
        ),
      ),
    /Duplicate/,
  );
  assert.throws(
    () =>
      parseCanonical(
        raw.replace(
          '"text":"retained"',
          '"text":"first","t\\u0065xt":"retained"',
        ),
      ),
    /Duplicate/,
  );
  assert.equal(parseCanonical(raw).content[0].children[0].text, "retained");
});
test("typed clipboard envelopes validate versions and inline fragments before explicit UUID reseeding", () => {
  const target = crypto.randomUUID(),
    mention = e("mention", [{ text: "" }], {
      key: "issue:" + target,
      value: "issue",
      target_kind: "issue",
      target_id: target,
    }),
    raw = JSON.stringify({
      format: "plate",
      schema_version: 1,
      fragment: [mention],
    });
  const parsed = parseClipboard(raw);
  assert.deepEqual(parsed, [mention]);
  const pasted = reseedSubtree(parsed);
  assert.notEqual(pasted[0].id, mention.id);
  assert.equal(pasted[0].target_id, target);
  assert.throws(() =>
    parseClipboard(raw.replace('"schema_version":1', '"schema_version":2')),
  );
});
