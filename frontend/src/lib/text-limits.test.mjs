import assert from "node:assert/strict";
import { test } from "node:test";
import { withinCodePointLimit, withinByteLimit } from "./text-limits.mjs";
test("Unicode text follows schema/Go code-point limits rather than UTF-16 truncation", () => {
  for (const limit of [100, 200, 2000]) {
    assert.equal(withinCodePointLimit("😀".repeat(limit), limit), true);
    assert.equal(withinCodePointLimit("😀".repeat(limit + 1), limit), false);
    assert.equal(withinCodePointLimit("İ".repeat(limit), limit), true);
  }
  assert.equal(withinCodePointLimit("e\u0301", 1), false);
  assert.equal(withinCodePointLimit("e\u0301", 2), true);
});
test("URL byte bounds remain independent from code-point text bounds", () => {
  assert.equal(withinByteLimit("a".repeat(2048), 2048), true);
  assert.equal(withinByteLimit("a".repeat(2049), 2048), false);
  assert.equal(withinByteLimit("😀".repeat(512), 2048), true);
  assert.equal(withinByteLimit("😀".repeat(513), 2048), false);
});
