// Flatten a standalone operation schema into an OpenAPI component without
// changing its resource-relative references or touching enum/default JSON data.
export function embedSchema(name, schema) {
  const originalBase = "https://traccia.invalid/operation.schema.json";
  const resourceURL = (url) => { const result = new URL(url); result.hash = ""; return result.href; };
  const escape = (value) => value.replaceAll("~", "~0").replaceAll("/", "~1");
  const pointer = (path) => path.map(escape).join("/");
  const singles = new Set(["items", "contains", "additionalProperties", "unevaluatedProperties", "propertyNames", "not", "if", "then", "else", "additionalItems", "contentSchema"]);
  const arrays = new Set(["anyOf", "oneOf", "allOf", "prefixItems"]);
  const maps = new Set(["properties", "patternProperties", "dependentSchemas", "$defs", "definitions"]);
  const scopes = new WeakMap();
  const resources = new Map([[originalBase, { schema, path: [] }]]);
  const anchors = new Map();
  const children = (node, path, callback) => {
    for (const [key, child] of Object.entries(node)) {
      if (maps.has(key) && child && typeof child === "object") {
        for (const [field, entry] of Object.entries(child)) callback(entry, [...path, key, field]);
      } else if (arrays.has(key) && Array.isArray(child)) {
        child.forEach((entry, index) => callback(entry, [...path, key, String(index)]));
      } else if (singles.has(key)) callback(child, [...path, key]);
    }
  };
  const index = (node, path, inherited) => {
    if (!node || typeof node !== "object" || Array.isArray(node)) return;
    const base = typeof node.$id === "string" ? new URL(node.$id, inherited).href : inherited;
    if (node.$id) {
      const id = resourceURL(base);
      if (resources.has(id) && resources.get(id).schema !== node) throw new Error(`Duplicate schema resource ${id}`);
      resources.set(id, { schema: node, path });
    }
    scopes.set(node, base);
    if (typeof node.$anchor === "string") anchors.set(`${resourceURL(base)}#${node.$anchor}`, path);
    children(node, path, (child, childPath) => index(child, childPath, base));
  };
  index(schema, [], originalBase);
  const resolve = (node, reference) => {
    const url = new URL(reference, scopes.get(node));
    const resource = resources.get(resourceURL(url));
    if (!resource) throw new Error(`External schema resource ${url.href}`);
    const fragment = decodeURIComponent(url.hash.slice(1));
    let path;
    if (!fragment) path = resource.path;
    else if (fragment.startsWith("/")) {
      const parts = fragment.slice(1).split("/").map((part) => part.replaceAll("~1", "/").replaceAll("~0", "~"));
      let target = resource.schema;
      for (const part of parts) {
        if (target === null || typeof target !== "object" || !(part in target)) throw new Error(`Unresolved schema reference ${url.href}`);
        target = target[part];
      }
      path = [...resource.path, ...parts];
    } else path = anchors.get(url.href);
    if (!path) throw new Error(`Unresolved schema anchor ${url.href}`);
    return `#/components/schemas/${escape(name)}${path.length ? `/${pointer(path)}` : ""}`;
  };
  const render = (node) => {
    if (!node || typeof node !== "object" || Array.isArray(node)) return node;
    const result = {};
    for (const [key, child] of Object.entries(node)) {
      if (["$schema", "$id", "$anchor"].includes(key)) continue;
      if (key === "$dynamicRef" || key === "$dynamicAnchor") throw new Error("Dynamic schema references require explicit OpenAPI coverage");
      if (key === "$ref") result[key] = resolve(node, child);
      else if (maps.has(key) && child && typeof child === "object") result[key] = Object.fromEntries(Object.entries(child).map(([field, entry]) => [field, render(entry)]));
      else if (arrays.has(key) && Array.isArray(child)) result[key] = child.map(render);
      else result[key] = singles.has(key) ? render(child) : child;
    }
    return result;
  };
  return render(schema);
}

// Node's source-aware reviver and raw JSON values preserve every numeric token,
// including integer constraints beyond 2^53 and precise decimal constraints.
export function parseExactJSON(source) {
  return JSON.parse(source, (_key, value, context) => typeof value === "number" ? JSON.rawJSON(context.source) : value);
}
export function cloneExactJSON(value) { return parseExactJSON(JSON.stringify(value)); }

// OpenAPI clients model nested declaration containers as instance fields. Hoist
// declarations after resolving their original resource scopes, then rewrite only
// schema references. Literal const/default/enum values remain data.
export function hoistSchema(name, schema, components) {
  const embedded = embedSchema(name, schema);
  const singles = new Set(["items", "contains", "additionalProperties", "unevaluatedProperties", "propertyNames", "not", "if", "then", "else", "additionalItems", "contentSchema"]);
  const arrays = new Set(["anyOf", "oneOf", "allOf", "prefixItems"]);
  const maps = new Set(["properties", "patternProperties", "dependentSchemas", "$defs", "definitions"]);
  const escape = value => value.replaceAll("~", "~0").replaceAll("/", "~1");
  const base = `#/components/schemas/${escape(name)}`;
  const declarations = [];
  const visit = (node, path, fn) => {
    if (!node || typeof node !== "object" || Array.isArray(node)) return node;
    return fn(node, path, (key, child) => {
      if (maps.has(key) && child && typeof child === "object") return Object.fromEntries(Object.entries(child).map(([field, entry]) => [field, visit(entry, `${path}/${key}/${escape(field)}`, fn)]));
      if (arrays.has(key) && Array.isArray(child)) return child.map((entry, index) => visit(entry, `${path}/${key}/${index}`, fn));
      return singles.has(key) ? visit(child, `${path}/${key}`, fn) : child;
    });
  };
  visit(embedded, base, (node, path, child) => {
    for (const [key, value] of Object.entries(node)) {
      if ((key === "$defs" || key === "definitions") && value && typeof value === "object") {
        for (const [field, entry] of Object.entries(value)) declarations.push({ path: `${path}/${key}/${escape(field)}`, name: `${name}Definition${declarations.length}`, node: entry });
      }
      child(key, value);
    }
    return node;
  });
  const ordered = [...declarations].sort((a,b) => b.path.length-a.path.length);
  const rewrite = reference => {
    const target = ordered.find(item => reference === item.path || reference.startsWith(`${item.path}/`));
    return target ? `#/components/schemas/${target.name}${reference.slice(target.path.length)}` : reference;
  };
  const render = node => visit(node, base, (value, _path, child) => Object.fromEntries(Object.entries(value).filter(([key]) => key !== "$defs" && key !== "definitions").map(([key, item]) => [key, key === "$ref" ? rewrite(item) : child(key,item)])));
  for (const declaration of declarations) {
    if (Object.hasOwn(components,declaration.name)) throw new Error(`Duplicate OpenAPI component ${declaration.name}`);
    components[declaration.name] = render(declaration.node);
  }
  return render(embedded);
}
