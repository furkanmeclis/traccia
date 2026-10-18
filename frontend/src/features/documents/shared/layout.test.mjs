import test from "node:test";
import assert from "node:assert/strict";
import { layoutStyle } from "./layout.ts";
test("table geometry and safe individual cell styles render identically across shared surfaces", () => {
  assert.deepEqual(layoutStyle({ type: "table", marginLeft: 24 }), {
    marginLeft: 24,
  });
  assert.deepEqual(layoutStyle({ type: "tr", size: 48 }), { height: 48 });
  assert.deepEqual(
    layoutStyle({
      type: "td",
      size: 160,
      background: "#eef",
      borders: { top: { color: "#123", style: "dashed", size: 2 } },
    }),
    {
      width: 160,
      backgroundColor: "#eef",
      borderTopColor: "#123",
      borderTopStyle: "dashed",
      borderTopWidth: 2,
    },
  );
});
