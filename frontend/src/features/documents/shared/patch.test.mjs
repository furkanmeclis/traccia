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
