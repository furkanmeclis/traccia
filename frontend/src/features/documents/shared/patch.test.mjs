import test from "node:test";
import assert from "node:assert/strict";
import { nodePatch, sectionPatch } from "./patch.ts";
import { paragraph } from "./contract.ts";
test("targeted node patch preserves stable ID and refuses to discard sibling draft changes", () => {
  const v = [paragraph("a"), paragraph("b")],
    d = structuredClone(v);
  d[0].children[0].text = "new";
  assert.deepEqual(nodePatch(v, d, v[0].id), {
    op: "replace_node",
    node_id: v[0].id,
    node: d[0],
  });
  d[1].children[0].text = "dirty sibling";
  assert.throws(() => nodePatch(v, d, v[0].id), /outsideNode/);
});
test("section patch carries exact ordered baseline members and refuses outside changes", () => {
  const h = { ...paragraph("heading"), type: "h1" },
    a = paragraph("a"),
    b = paragraph("b"),
    next = { ...paragraph("next"), type: "h1" },
    v = [h, a, b, next],
    d = structuredClone(v);
  d.splice(1, 2, paragraph("new"));
  const p = sectionPatch(v, d, h.id);
  assert.deepEqual(p.expected_member_ids, [a.id, b.id]);
  assert.deepEqual(p.nodes, d.slice(1, 2));
  d.at(-1).children[0].text = "outside";
  assert.throws(() => sectionPatch(v, d, h.id), /outsideSection/);
});

function reordered(value) {
  if (Array.isArray(value)) return value.map(reordered);
  if (value && typeof value === "object")
    return Object.fromEntries(
      Object.entries(value)
        .reverse()
        .map(([key, item]) => [key, reordered(item)]),
    );
  return value;
}
const { documentFixture } = await import("./fixtures.ts");
function fixture() {
  const mixed = documentFixture();
  return [
    mixed[1],
    mixed[2],
    mixed[0],
    paragraph("selected section"),
    { ...paragraph("next heading"), type: "h1" },
    paragraph("outside tail"),
  ];
}
test("node patch ignores shuffled nested sibling JSON properties without mutating or normalizing either tree", () => {
  const baseline = fixture(),
    draft = reordered(structuredClone(baseline)),
    id = baseline.at(-1).id;
  draft.at(-1).children[0].text = "changed İ e\u0301 👩🏽‍💻\r\n";
  assert.notEqual(
    JSON.stringify(baseline.slice(0, -1)),
    JSON.stringify(draft.slice(0, -1)),
    "original stringify comparator rejects the same unchanged siblings",
  );
  assert.deepEqual(baseline.slice(0, -1), draft.slice(0, -1));
  const before = JSON.stringify(baseline),
    after = JSON.stringify(draft);
  assert.deepEqual(nodePatch(baseline, draft, id), {
    op: "replace_node",
    node_id: id,
    node: draft.at(-1),
  });
  assert.equal(JSON.stringify(baseline), before);
  assert.equal(JSON.stringify(draft), after);
});
test("section patch ignores shuffled nested paragraph/table properties outside its exact ordered section and preserves supplied JSON", () => {
  const baseline = fixture(),
    draft = reordered(structuredClone(baseline)),
    id = baseline[2].id;
  draft[3].children[0].text = "section changed İ\r\n";
  const outside = (nodes) => nodes.filter((_, index) => index !== 3);
  assert.notEqual(
    JSON.stringify(outside(baseline)),
    JSON.stringify(outside(draft)),
    "original stringify comparator rejects equivalent outside JSON",
  );
  assert.deepEqual(outside(baseline), outside(draft));
  const before = JSON.stringify(baseline),
    after = JSON.stringify(draft);
  assert.deepEqual(sectionPatch(baseline, draft, id), {
    op: "replace_section",
    heading_id: id,
    expected_member_ids: [baseline[3].id],
    nodes: [draft[3]],
  });
  assert.equal(JSON.stringify(baseline), before);
  assert.equal(JSON.stringify(draft), after);
});
test("both patch boundaries still reject real sibling text, property, UUID, missing key, boolean and array order/length changes after harmless key reordering", () => {
  const mutations = [
    (d) => {
      d[0].children[0].text += "outside";
    },
    (d) => {
      d[1].marginLeft = 1;
    },
    (d) => {
      d[0].id = crypto.randomUUID();
    },
    (d) => {
      delete d[1].marginLeft;
    },
    (d) => {
      d[0].checked = true;
    },
    (d) => {
      [d[0], d[1]] = [d[1], d[0]];
    },
    (d) => {
      d.unshift(paragraph("new outside"));
    },
    (d) => {
      d[1].children[0].children[0].borders.top.color = "#f00";
    },
    (d) => {
      d[1].children[0].children[0].children.push(paragraph("new cell block"));
    },
    (d) => {
      d[1].marginLeft = null;
    },
  ];
  for (const mutate of mutations) {
    const baseline = fixture(),
      draft = reordered(structuredClone(baseline));
    mutate(draft);
    assert.throws(
      () => nodePatch(baseline, draft, baseline.at(-1).id),
      /outsideNode/,
    );
    assert.throws(() => sectionPatch(baseline, draft, baseline[2].id));
  }
});
