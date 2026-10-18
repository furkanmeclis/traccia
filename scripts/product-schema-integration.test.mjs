import test from "node:test";
import assert from "node:assert/strict";
test("hoisted recursive declarations generate body types without declaration fields", async () => {
  const { hoistSchema } = await import("./product-schema.mjs");
  const { default: openapiTS, astToString } = await import("../frontend/node_modules/openapi-typescript/dist/index.mjs");
  const components = {};
  const body = { $id:"root.json", type:"object", additionalProperties:false, required:["content"], properties:{content:{type:"array",items:{$ref:"child.json#node"}}}, $defs:{Node:{$id:"child.json",$anchor:"node",type:"object",additionalProperties:false,required:["text","children"],properties:{text:{type:"string"},children:{type:"array",items:{$ref:"#node"}}}}} };
  components.Body = hoistSchema("Body",body,components);
  assert.equal(components.Body.$defs,undefined);
  assert.equal(components.Body.properties.content.items.$ref,"#/components/schemas/BodyDefinition0");
  assert.equal(components.BodyDefinition0.properties.children.items.$ref,"#/components/schemas/BodyDefinition0");
  const code = astToString(await openapiTS({openapi:"3.1.0",info:{title:"fixture",version:"1"},paths:{},components:{schemas:components}}));
  assert.doesNotMatch(code,/\s\$defs:/);
  assert.match(code,/content: components\["schemas"\]\["BodyDefinition0"\]\[\]/);
  const literal = {type:"string",const:{$defs:{literal:1}}};
  assert.deepEqual(hoistSchema("Literal",literal,{}),literal);
});

test("actual generated bodies typecheck without declarations or path-owned fields", async () => {
  const { mkdtempSync, writeFileSync, rmSync } = await import("node:fs");
  const { tmpdir } = await import("node:os");
  const { join } = await import("node:path");
  const { fileURLToPath } = await import("node:url");
  const { execFileSync } = await import("node:child_process");
  const dir = mkdtempSync(join(tmpdir(),"traccia-contract-"));
  const id = "01234567-89ab-cdef-0123-456789abcdef";
  const content = [{id,type:"p",children:[{text:"Türkçe 🧭\r\n"}]}];
  const bodies = {
    ProductSavedViewsUpdateInput:{expected_revision:id,name:"Private",definition:{resource:"issue",issue:{version:1,archived:"active",search:"",filter:null,sort:[]},project:null,team:null},columns:[{key:"title",width:240}]},
    ProductDocumentsCreateInput:{owner_kind:"issue",owner_id:id,format:"plate",schema_version:1,content},
    ProductDocumentsRestoreInput:{expected_revision:id,revision_id:id},
    ProductIssuesCreateRichInput:{issue:{team_id:id,status_id:id,title:"rich",priority:"normal",assignee_id:null,project_id:null,milestone_id:null,start_date:null,target_date:null,effort:null,label_ids:[]},format:"plate",schema_version:1,content},
  };
  try {
    const api = fileURLToPath(new URL("../frontend/src/generated/api.d.ts",import.meta.url));
    const source = `import type {components} from ${JSON.stringify(api)};\n`+Object.entries(bodies).map(([name,value],index)=>`const body${index}:components["schemas"][${JSON.stringify(name)}]=${JSON.stringify(value)};`).join("\n");
    const file = join(dir,"bodies.ts");writeFileSync(file,source);
    execFileSync(process.execPath,[fileURLToPath(new URL("../frontend/node_modules/typescript/bin/tsc",import.meta.url)),"--noEmit","--strict","--skipLibCheck","--target","ES2022","--moduleResolution","bundler","--module","ESNext",file],{encoding:"utf8"});
  } finally {rmSync(dir,{recursive:true,force:true});}
});
