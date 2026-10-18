import assert from "node:assert/strict";
import { test } from "node:test";
import { collectCatalog, safeCatalogUrl } from "./snapshot.ts";
const page = (
  items,
  version = 1,
  more = false,
  cursor = null,
  capabilities = { manage: true, restore: false },
) => ({
  items: items.map((id) => ({ id })),
  product_version: version,
  has_more: more,
  next_cursor: cursor,
  capabilities,
});
test("complete snapshots exhaust cursors, deduplicate rows and intersect same-filter capabilities", async () => {
  const calls = [];
  const first = page(["1"], 1, true, "next");
  const result = await collectCatalog(async (after) => {
    calls.push(after);
    return after
      ? page(["1", "2"], 1, false, null, { manage: false, restore: false })
      : first;
  });
  assert.deepEqual(calls, [undefined, "next"]);
  assert.deepEqual(result.items, [{ id: "1" }, { id: "2" }]);
  assert.equal(result.capabilities.manage, false);
  assert.equal(first.capabilities.manage, true);
});
test("incomplete or changing snapshots are rejected before writes", async () => {
  await assert.rejects(
    collectCatalog(async () => page(["1"], 1, true)),
    /INCOMPLETE_PAGINATION/,
  );
  await assert.rejects(
    collectCatalog(async () => page(["1"], 1, true, "repeat")),
    /INCOMPLETE_PAGINATION/,
  );
  await assert.rejects(
    collectCatalog(async (after) =>
      after ? page(["2"], 2) : page(["1"], 1, true, "next"),
    ),
    /PRODUCT_SNAPSHOT_CHANGED/,
  );
});
test("catalog links allow local HTTP metadata and reject credentials, unsafe protocols and secret query keys", () => {
  for (const value of [
    "http://localhost:3000",
    "https://192.168.1.2/path",
    "https://example.com/?ref=main",
  ])
    assert.ok(safeCatalogUrl(value));
  for (const value of [
    "javascript:alert(1)",
    "https://user:pass@example.com",
    "https://example.com/?TOKEN=secret",
    "https://example.com/?x-amz-signature=secret",
    "https://example.com/\npath",
    "https://example.com/" + "é".repeat(1100),
  ])
    assert.equal(safeCatalogUrl(value), null);
});
