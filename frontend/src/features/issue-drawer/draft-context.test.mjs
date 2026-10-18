import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import vm from "node:vm";
import ts from "typescript";
import { createDrawerDrafts } from "./drafts.ts";
// Execute the actual hook with a deterministic lifecycle harness. The old
// component-only useState implementation loses this snapshot on remount.
test("actual draft hook restores unsaved content and CAS, ignores late unmounted callbacks, and leaves non-drawer callers local", () => {
  const store = createDrawerDrafts();
  let slots = [],
    cursor = 0,
    effects = [],
    scope = "actor/workspace/issue-a";
  const react = {
    createContext: () => ({}),
    useContext: () => scope,
    useState(initial) {
      const i = cursor++;
      if (!(i in slots))
        slots[i] = typeof initial === "function" ? initial() : initial;
      return [
        slots[i],
        (value) => {
          slots[i] = value;
        },
      ];
    },
    useRef(initial) {
      const i = cursor++;
      if (!(i in slots)) slots[i] = { current: initial };
      return slots[i];
    },
    useEffect(fn) {
      effects.push(fn());
    },
  };
  const sandboxModule = { exports: {} };
  vm.runInNewContext(
    ts.transpileModule(
      readFileSync(new URL("./draft-context.tsx", import.meta.url), "utf8"),
      { compilerOptions: { module: ts.ModuleKind.CommonJS } },
    ).outputText,
    {
      module: sandboxModule,
      exports: sandboxModule.exports,
      require: (name) => (name === "react" ? react : { drawerDrafts: store }),
    },
  );
  const hook = sandboxModule.exports.useDrawerDraft;
  function mount(initial) {
    slots = [];
    effects = [];
    cursor = 0;
    return hook("description", initial);
  }
  const original = {
    revision: "head-a",
    content: [{ id: "stable", text: "İ😀\r\n" }],
  };
  const [baseline, change] = mount(original);
  assert.equal(baseline.revision, "head-a");
  change({ ...baseline, content: [{ id: "stable", text: "dirty" }] });
  effects.forEach((cleanup) => cleanup?.());
  scope = "actor/workspace/issue-b";
  mount({ revision: "b", content: [] });
  scope = "actor/workspace/issue-a";
  const [restored, reopened] = mount({
    revision: "fresh-server-head",
    content: [],
  });
  assert.equal(restored.revision, "head-a");
  assert.equal(restored.content[0].text, "dirty");
  change({ revision: "late-old-callback", content: [] });
  reopened((prior) => ({
    ...prior,
    content: [{ id: "stable", text: "newer edit" }],
  }));
  effects.forEach((cleanup) => cleanup?.());
  const [final] = mount({ revision: "ignored" });
  assert.equal(final.revision, "head-a");
  assert.equal(final.content[0].text, "newer edit");
  scope = null;
  const [, localChange] = mount("local");
  localChange("dirty local");
  effects.forEach((cleanup) => cleanup?.());
  assert.equal(mount("fresh local")[0], "fresh local");
});
