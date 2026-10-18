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

test("hoisting preserves recursive resource targets without wire declaration fields", async () => {
  const { hoistSchema } = await import("./product-schema.mjs");
  const components = {};
  const body = { $id:"root.json", type:"object", additionalProperties:false, required:["content"], properties:{content:{type:"array",items:{$ref:"child.json#node"}}}, $defs:{Node:{$id:"child.json",$anchor:"node",type:"object",additionalProperties:false,required:["text","children"],properties:{text:{type:"string"},children:{type:"array",items:{$ref:"#node"}}}}} };
  components.Body = hoistSchema("Body",body,components);
  assert.equal(components.Body.$defs,undefined);
  assert.equal(components.Body.properties.content.items.$ref,"#/components/schemas/BodyDefinition0");
  assert.equal(components.BodyDefinition0.properties.children.items.$ref,"#/components/schemas/BodyDefinition0");
  const literal = {type:"string",const:{$defs:{literal:1}}};
  assert.deepEqual(hoistSchema("Literal",literal,{}),literal);
});

test("path fields are projected from same-instance compositions only", async () => {
  const { omitBoundFields } = await import("./product-schema.mjs");
  const original = { type:"object",properties:{view_id:{type:"string"},nested:{type:"object",properties:{view_id:{type:"string"}},required:["view_id"]}},required:["view_id","nested"],oneOf:[{type:"object",properties:{view_id:{type:"string"},name:{type:"string"}},required:["view_id","name"]}],const:{view_id:"literal"} };
  const projected=omitBoundFields(structuredClone(original),["view_id"]);
  assert.deepEqual(projected.required,["nested"]);
  assert.deepEqual(projected.oneOf[0].required,["name"]);
  assert.equal(projected.oneOf[0].properties.view_id,undefined);
  assert.deepEqual(projected.properties.nested.required,["view_id"]);
  assert.deepEqual(projected.const,{view_id:"literal"});
  assert.deepEqual(original.required,["view_id","nested"]);
});

test("binding projection preserves nested composition, refs, item data and multiplicity", async () => {
  const { omitBoundFields } = await import("./product-schema.mjs");
  const branch={type:"object",properties:{view_id:{type:"string"},value:{type:"integer"}},required:["view_id","value"],additionalProperties:false};
  const original={type:"object",properties:{view_id:{type:"string"},rows:{type:"array",items:structuredClone(branch)}},required:["view_id","rows"],allOf:[{anyOf:[{oneOf:[structuredClone(branch),structuredClone(branch)]},{$ref:"#/$defs/Keep",maxProperties:8}]}],$defs:{Keep:structuredClone(branch)},examples:[{view_id:"literal"}]};
  const actual=omitBoundFields(structuredClone(original),["view_id"]);
  assert.equal(actual.allOf[0].anyOf[0].oneOf.length,2);
  for (const value of actual.allOf[0].anyOf[0].oneOf) {
    assert.deepEqual(value.required,["value"]);
    assert.equal(value.properties.view_id,undefined);
    assert.equal(value.additionalProperties,false);
  }
  assert.deepEqual(actual.allOf[0].anyOf[1],original.allOf[0].anyOf[1]);
  assert.deepEqual(actual.properties.rows,original.properties.rows);
  assert.deepEqual(actual.$defs,original.$defs);
  assert.deepEqual(actual.examples,original.examples);
  assert.ok(original.allOf[0].anyOf[0].oneOf[0].properties.view_id);
});
