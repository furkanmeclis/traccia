import test from "node:test";
import assert from "node:assert/strict";
import { embedSchema, parseExactJSON, cloneExactJSON } from "./product-schema.mjs";

test("embedding resolves nested resources, anchors, and recursive references", () => {
  const schema = { $id: "relative.json", $defs: {
    child: { $id: "child.json", $anchor: "child", type: "object", properties: { self: { $ref: "#" }, value: { $ref: "relative.json#/$defs/value" } } },
    value: { type: "integer" },
  }, type: "object", properties: { child: { $ref: "child.json#child" } } };
  const actual = embedSchema("Fixture", schema);
  assert.equal(actual.properties.child.$ref, "#/components/schemas/Fixture/$defs/child");
  assert.equal(actual.$defs.child.properties.self.$ref, "#/components/schemas/Fixture/$defs/child");
  assert.equal(actual.$defs.child.properties.value.$ref, "#/components/schemas/Fixture/$defs/value");
  assert.equal(actual.$id, undefined);
  assert.equal(actual.$defs.child.$id, undefined);
});

test("embedding preserves literal JSON and schema properties named like keywords", () => {
  const schema = { type: "object", properties: { $schema: { type: "string" }, title: { type: "string" }, examples: { type: "string" } }, enum: [{ $ref: "#literal", $id: "literal" }], default: { $ref: "#default" } };
  const actual = embedSchema("Fixture", schema);
  assert.deepEqual(actual, schema);
});

test("external and unresolved references fail instead of publishing broken schemas", () => {
  assert.throws(() => embedSchema("Fixture", { $ref: "https://example.invalid/schema" }), /External/);
  assert.throws(() => embedSchema("Fixture", { $ref: "#/$defs/missing" }), /Unresolved/);
});


test("catalog parsing and cloning preserve exact integer and decimal constraints", () => {
  const raw = '{"type":"integer","maximum":9007199254740993,"multipleOf":0.1000000000000000001}';
  const value = cloneExactJSON(parseExactJSON(raw));
  assert.equal(JSON.stringify(embedSchema("Precise", value)), raw);
});
