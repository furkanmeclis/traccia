import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import vm from "node:vm";
import ts from "typescript";
const src = new URL("../../", import.meta.url);
const translatedModule = { exports: {} };
const compiled = ts.transpileModule(
  readFileSync(new URL("../../lib/i18n/messages.ts", import.meta.url), "utf8"),
  {
    compilerOptions: { module: ts.ModuleKind.CommonJS, esModuleInterop: true },
  },
).outputText;
vm.runInNewContext(compiled, {
  module: translatedModule,
  exports: translatedModule.exports,
  process: { env: { NODE_ENV: "production" } },
  require(name) {
    assert(name.startsWith("@/locales/"));
    return JSON.parse(readFileSync(new URL(name.slice(2), src), "utf8"));
  },
});
const { translate } = translatedModule.exports;
test("real attachment translator renders limits, full verified bytes, bounded memory and retained chunk characters in TR and EN", () => {
  const values = {
    bytes: 1048832,
    chunk: 262144,
    max: 268435456,
    chars: 349528,
  };
  for (const locale of ["tr", "en"]) {
    const expected = {
      uploadHelp: ["bytes", "chunk"],
      memoryHelp: ["bytes", "max"],
      verified: ["bytes"],
      binaryRetained: ["chars"],
    };
    for (const [key, names] of Object.entries(expected)) {
      const rendered = translate(locale, "attachments." + key, values);
      for (const name of names)
        assert(
          rendered.includes(String(values[name])),
          locale + " " + key + " " + name,
        );
      assert(
        !/[{}]/.test(rendered),
        locale + " " + key + " unresolved interpolation",
      );
    }
  }
});
