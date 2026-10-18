// Sync the product-only OpenAPI blocks from the bound Go operation catalog.
// Inherited platform schemas are deliberately kept outside these blocks.
import { execFileSync } from "node:child_process";
import { readFileSync, writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { hoistSchema, parseExactJSON, cloneExactJSON, omitBoundFields } from "./product-schema.mjs";

const root = fileURLToPath(new URL("../", import.meta.url));
const catalog = parseExactJSON(execFileSync("go", ["run", "./cmd/product-contracts"], {
  cwd: `${root}backend`, encoding: "utf8", maxBuffer: 64 * 1024 * 1024,
}));
const file = `${root}backend/docs/openapi.yaml`;
let source = readFileSync(file, "utf8");
const embedSchema = (name, schema) => hoistSchema(name, schema, schemas);
const ref = (name) => ({ $ref: `#/components/schemas/${name}` });

const paths = {};
const schemas = {};
const closed = (properties, required = Object.keys(properties)) => ({ type: "object", additionalProperties: false, required, properties });
schemas.ProductResponseMeta = closed({ request_id: { type: "string" } });
const domainCodes = [...new Set(catalog.flatMap((c) => c.error_schema.properties.code.enum))];
const statusCodes = {
  400: ["VALIDATION_ERROR"], 401: ["UNAUTHORIZED", "UNAUTHENTICATED"],
  403: ["FORBIDDEN"], 404: ["NOT_FOUND"], 408: ["CANCELED"],
  409: ["CONFLICT", "IDEMPOTENCY_CONFLICT", "VERSION_CONFLICT"],
  415: ["UNSUPPORTED_MEDIA_TYPE"], 500: ["INTERNAL_ERROR"], 503: ["OPERATION_UNAVAILABLE"],
};
for (const [status, codes] of Object.entries(statusCodes)) {
  const details = { type: "array", items: closed({ field: { type: "string" }, code: { type: "string" }, message: { type: "string" } }, ["message"]) };
  schemas[`ProductHTTPError${status}`] = closed({ success: { type: "boolean", const: false }, error: closed({ code: { type: "string", enum: codes }, message: { type: "string" }, details }, ["code", "message"]), meta: ref("ProductResponseMeta") });
}
const domainCodeSet = new Set(Object.entries(statusCodes).filter(([status]) => status !== "415").flatMap(([, codes]) => codes).filter((code) => code !== "UNAUTHENTICATED"));
if (domainCodes.some((code) => !domainCodeSet.has(code)) || [...domainCodeSet].some((code) => !domainCodes.includes(code))) throw new Error("HTTP error code mapping must cover the exact domain error set");
for (const contract of catalog) {
  const meta = contract.metadata;
  const name = `Product${meta.id.split(/[._]/).map((part) => part[0].toUpperCase() + part.slice(1)).join("")}`;
  const input = cloneExactJSON(contract.input_schema);
  const output = cloneExactJSON(contract.output_schema);
  delete input.$schema;
  delete output.$schema;
  const pathFields = Object.fromEntries([...meta.http_path.matchAll(/\{([^}]+)ID\}/g)].filter((m) => m[1] !== "workspace").map((m) => [`${m[1]}_id`, `${m[1]}ID`]));
  if(meta.http_path.includes("{identifier}")) pathFields.identifier="identifier";
  const bodyInput = cloneExactJSON(input);
  omitBoundFields(bodyInput, Object.keys(pathFields));
  if (["POST", "PATCH", "PUT"].includes(meta.http_method)) schemas[`${name}Input`] = embedSchema(`${name}Input`, bodyInput);
  const binding = Object.fromEntries(Object.entries(pathFields).map(([field,name])=>[field,`path:${name}`]));
  const queryFields = meta.http_method === "GET" ? Object.keys(input.properties ?? {}).filter((field)=>!pathFields[field]) : [];
  for(const field of queryFields) binding[field]=`query:${field}`;
  const defaultFor = (field) => field === "limit" ? 20 : input.properties[field].type === "boolean" ? false : "";
  schemas[`${name}Output`] = embedSchema(`${name}Output`, output);
  const error = cloneExactJSON(contract.error_schema);
  delete error.$schema;
  schemas[`${name}Error`] = embedSchema(`${name}Error`, error);
  schemas[`${name}DomainInput`] = embedSchema(`${name}DomainInput`, input);
  schemas[`${name}Envelope`] = {
    type: "object", additionalProperties: false, required: ["success", "data", "meta"],
    properties: { success: { type: "boolean", const: true }, data: ref(`${name}Output`), meta: ref("ProductResponseMeta") },
  };
  const parameters = [...meta.http_path.matchAll(/\{([^}]+)\}/g)].map((match) => ({
    name: match[1], in: "path", required: true, schema: match[1] === "identifier" ? input.properties.identifier : { type: "string", format: "uuid" },
  }));
  for(const field of queryFields) parameters.push({ name:field, in:"query", schema:input.properties[field], "x-binding-default":defaultFor(field) });
  if(meta.idempotent) parameters.push({name:"Idempotency-Key",in:"header",required:true,schema:{type:"string",minLength:1,maxLength:200},description:"One stable key per complete immutable mutation input; retries replay the original result."});
  const status = meta.http_method === "POST" ? "201" : "200";
  const responses = { [status]: {
    description: meta.id === "credentials.create" ? "Created once. Exact token result may be replayed with the same authorized idempotency key; no-store." : "Success",
    headers: { "Cache-Control": { schema: { type: "string", const: "no-store" } } },
    content: { "application/json": { schema: ref(`${name}Envelope`) } },
  } };
  if (meta.id === "attachments.download_chunk") {
    responses[status].headers["Content-Disposition"] = { schema: { type: "string" }, description: "Safe attachment filename; the JSON response carries a bounded base64 chunk." };
    responses[status].headers["X-Content-Type-Options"] = { schema: { type: "string", const: "nosniff" } };
  }
  for (const code of Object.keys(statusCodes)) {
    if (code === "415" && !["POST", "PATCH", "PUT"].includes(meta.http_method)) continue;
    responses[code] = { description: code === "401" ? "Verified application identity or HTTP authentication failed." : code === "415" ? "JSON request media type required." : "Application operation failed.", content: { "application/json": { schema: ref(`ProductHTTPError${code}`) } } };
  }
  const operation = {
    tags: ["Workspaces"], operationId: meta.id.replaceAll(".", "_"),
    summary: meta.id, "x-operation-id": meta.id, "x-operation-version": meta.version,
    "x-traccia-input-schema": ref(`${name}DomainInput`),
    "x-traccia-error-schema": ref(`${name}Error`),
    "x-traccia-input-binding": ["POST", "PATCH", "PUT"].includes(meta.http_method) ? (Object.keys(pathFields).length ? { ...binding, body: "body" } : "body") : Object.keys(binding).length ? binding : "empty",
    description: "Product membership and credential grants are checked in the shared application authorizer. No global administrator bypass. API audience credentials only on HTTP; MCP audience uses the shared MCP SDK binding. Network MCP transport remains roadmap work.",
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
if (process.argv.includes("--check")) {
  if (readFileSync(file, "utf8") !== source) {
    console.error("Product OpenAPI is stale; run node scripts/sync-product-openapi.mjs and regenerate frontend API types.");
    process.exitCode = 1;
  } else console.log(`Checked ${catalog.length} product operation contracts.`);
} else {
  writeFileSync(file, source);
  console.log(`Synced ${catalog.length} product operation contracts.`);
}
