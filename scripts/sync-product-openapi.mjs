// Sync the product-only OpenAPI blocks from the bound Go operation catalog.
// Inherited platform schemas are deliberately kept outside these blocks.
import { execFileSync } from "node:child_process";
import { readFileSync, writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("../", import.meta.url));
const catalog = JSON.parse(execFileSync("go", ["run", "./cmd/product-contracts"], {
  cwd: `${root}backend`, encoding: "utf8",
}));
const file = `${root}backend/docs/openapi.yaml`;
let source = readFileSync(file, "utf8");
const ref = (name) => ({ $ref: `#/components/schemas/${name}` });
const paths = {};
const schemas = {};
for (const contract of catalog) {
  const meta = contract.metadata;
  const name = `Product${meta.id.split(/[._]/).map((part) => part[0].toUpperCase() + part.slice(1)).join("")}`;
  const input = structuredClone(contract.input_schema);
  const output = structuredClone(contract.output_schema);
  delete input.$schema;
  delete output.$schema;
  if (["POST", "PATCH", "PUT"].includes(meta.http_method)) schemas[`${name}Input`] = input;
  schemas[`${name}Output`] = output;
  schemas[`${name}Envelope`] = {
    type: "object", required: ["success", "data", "meta"],
    properties: { success: { type: "boolean", const: true }, data: ref(`${name}Output`), meta: ref("ResponseMeta") },
  };
  const parameters = [...meta.http_path.matchAll(/\{([^}]+)\}/g)].map((match) => ({
    name: match[1], in: "path", required: true, schema: { type: "string", format: "uuid" },
  }));
  if (meta.id.endsWith(".list")) {
    parameters.push({ name: "limit", in: "query", schema: { type: "integer", minimum: 1, maximum: 100, default: 20 } });
    parameters.push({ name: "after", in: "query", schema: { type: "string", format: "uuid" }, description: "UUID keyset cursor from next_cursor; omitted on first page." });
  }
  if(meta.idempotent) parameters.push({name:"Idempotency-Key",in:"header",required:true,schema:{type:"string",minLength:1,maxLength:200},description:"One stable key per complete immutable mutation input; retries replay the original result."});
  const status = meta.http_method === "POST" ? "201" : "200";
  const responses = { [status]: {
    description: meta.id === "credentials.create" ? "Created once. Exact token result may be replayed with the same authorized idempotency key; no-store." : "Success",
    headers: { "Cache-Control": { schema: { type: "string", const: "no-store" } } },
    content: { "application/json": { schema: ref(`${name}Envelope`) } },
  } };
  for (const [code, response] of [[400, "BadRequest"], [401, "Unauthenticated"], [403, "Forbidden"], [404, "NotFound"], [500, "InternalError"]]) {
    responses[code] = { $ref: `#/components/responses/${response}` };
  }
  if(meta.idempotent) responses[409]={description:"IDEMPOTENCY_CONFLICT: key reused with different input; VERSION_CONFLICT: stale expected_version.",content:{"application/json":{schema:ref("Error")}}};
  const operation = {
    tags: ["Workspaces"], operationId: meta.id.replaceAll(".", "_"),
    summary: meta.id, "x-operation-id": meta.id, "x-operation-version": meta.version,
    description: "Product membership and credential grants are checked in the shared application authorizer. No global administrator bypass. API audience credentials only on HTTP; MCP audience uses the future MCP transport.",
    security: [{ bearerAuth: [] }, { productCredential: [] }], parameters, responses,
  };
  if (["POST", "PATCH", "PUT"].includes(meta.http_method)) {
    operation.requestBody = { required: true, content: { "application/json": { schema: ref(`${name}Input`) } } };
  }
  (paths[meta.http_path] ??= {})[meta.http_method.toLowerCase()] = operation;
}
// JSON flow objects are valid YAML 1.2; pretty-printing preserves readability and
// removes the need for a separate YAML generator dependency.
const render = (value, indent) => JSON.stringify(value, null, 2).split("\n").map((line) => " ".repeat(indent) + line).join("\n");
const pathBlock = Object.entries(paths).map(([key, value]) => `  ${key}:\n${render(value, 4)}\n`).join("\n");
const schemaBlock = Object.entries(schemas).map(([key, value]) => `    ${key}:\n${render(value, 6)}\n`).join("\n");
function replaceBlock(text, name, block, anchor, indent) {
  const start = `${" ".repeat(indent)}# BEGIN GENERATED PRODUCT ${name}`;
  const end = `${" ".repeat(indent)}# END GENERATED PRODUCT ${name}`;
  const replacement = `${start}\n${block}\n${end}\n`;
  const existingStart = text.indexOf(start);
  if (existingStart !== -1) {
    const existingEnd = text.indexOf(end, existingStart);
    if (existingEnd === -1) throw new Error(`Missing product block terminator: ${name}`);
    return text.slice(0, existingStart) + replacement + text.slice(existingEnd + end.length + 1);
  }
  if (!text.includes(anchor)) throw new Error(`Missing OpenAPI anchor: ${anchor}`);
  return text.replace(anchor, `${anchor}${replacement}`);
}
source = replaceBlock(source, "PATHS", pathBlock, "paths:\n", 2);
source = replaceBlock(source, "SCHEMAS", schemaBlock, "  schemas:\n", 4);
if (!source.includes("    productCredential:\n")) {
  source = source.replace("  securitySchemes:\n", "  securitySchemes:\n    productCredential:\n      type: http\n      scheme: bearer\n      bearerFormat: Opaque workspace credential\n      description: Revocable workspace credential; api audience on HTTP, bounded scopes.\n");
}
if (!source.includes("  - name: Workspaces\n")) {
  source = source.replace("tags:\n", "tags:\n  - name: Workspaces\n    description: Traccia product workspaces and independent tool credentials\n");
}
writeFileSync(file, source);
console.log(`Synced ${catalog.length} product operation contracts.`);
