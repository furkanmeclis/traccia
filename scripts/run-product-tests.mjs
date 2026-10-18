import { readdirSync } from "node:fs";
import { join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";

const frontend = fileURLToPath(new URL("../frontend/", import.meta.url));
export function discoverProductTests(directory = frontend) {
  const files = [];
  const walk = (path) => {
    for (const entry of readdirSync(path, { withFileTypes: true })) {
      const child = join(path, entry.name);
      if (entry.isDirectory()) walk(child);
      else if (entry.isFile() && entry.name.endsWith(".test.mjs")) files.push(child);
    }
  };
  walk(join(directory, "src"));
  return files.sort((a,b) => a < b ? -1 : a > b ? 1 : 0);
}
if (process.argv[1] && fileURLToPath(import.meta.url) === resolve(process.argv[1])) {
  const files = discoverProductTests();
  if (!files.length) throw new Error("No product regression tests discovered");
  if (process.argv.includes("--list")) console.log(JSON.stringify(files.map(file => relative(frontend,file)),null,2));
  else {
    console.log(`Discovered ${files.length} product regression files.`);
    const result = spawnSync(process.execPath,["--test",...files],{cwd:frontend,stdio:"inherit"});
    if (result.error) throw result.error;
    process.exitCode = result.status ?? 1;
  }
}
