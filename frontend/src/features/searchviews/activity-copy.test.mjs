import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import vm from "node:vm";
import ts from "typescript";
import React from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { searchActivityKeys } from "./activity-copy.ts";
const require = createRequire(import.meta.url),
  src = new URL("../../", import.meta.url),
  translatedModule = { exports: {} },
  activityModule = { exports: {} };
const compile = (url) =>
  ts.transpileModule(readFileSync(url, "utf8"), {
    compilerOptions: {
      module: ts.ModuleKind.CommonJS,
      jsx: ts.JsxEmit.ReactJSX,
      esModuleInterop: true,
      target: ts.ScriptTarget.ES2022,
    },
  }).outputText;
vm.runInNewContext(
  compile(new URL("../../lib/i18n/messages.ts", import.meta.url)),
  {
    module: translatedModule,
    exports: translatedModule.exports,
    process: { env: { NODE_ENV: "production" } },
    require(name) {
      assert(name.startsWith("@/locales/"));
      return JSON.parse(readFileSync(new URL(name.slice(2), src), "utf8"));
    },
  },
);
let locale = "tr";
const fallbacks = [],
  sensitive = "SENSITIVE_PRIVATE_NAME_QUERY_COLUMNS_İ_👩🏽‍💻",
  expected = [
    "saved_views.created",
    "saved_views.updated",
    "saved_views.deleted",
    "saved_views.restored",
    "column_preferences.set",
  ];
const items = expected.map((event_name, index) => ({
  id: String(index),
  event_name,
  name: sensitive,
  occurred_at: "2026-10-18T00:00:00Z",
  actor_id: "actor",
  resource_type: event_name.startsWith("saved_views")
    ? "saved_views"
    : "column_preferences",
}));
const Box = ({ children }) => React.createElement("div", {}, children);
vm.runInNewContext(
  compile(new URL("../workspaces/workspace-activity.tsx", import.meta.url)),
  {
    module: activityModule,
    exports: activityModule.exports,
    Intl,
    Date,
    encodeURIComponent,
    require(name) {
      if (name === "@/features/searchviews/activity-copy")
        return { searchActivityKeys };
      if (name.includes("/attachments/copy"))
        return { attachmentActivityKeys: {} };
      if (name.includes("/issue-relations/copy"))
        return { relationActivityKeys: {} };
      if (name === "@/providers/locale-provider")
        return {
          useLocale: () => ({
            locale,
            t: (key, params) => {
              if (key === "workspaces.activity_changed") fallbacks.push(key);
              return translatedModule.exports.translate(locale, key, params);
            },
          }),
        };
      if (name === "@/providers/auth-provider")
        return { useAuth: () => ({ user: { uuid: "actor" } }) };
      if (name === "./queries")
        return {
          useWorkspaceActivity: () => ({
            data: { pages: [{ items }] },
            isError: false,
            isPending: false,
            isFetching: false,
          }),
        };
      if (name === "@/lib/api") return { ApiError: class extends Error {} };
      if (name === "next/link") return { default: Box };
      if (name === "lucide-react") return { History: Box, RefreshCw: Box };
      if (name === "@/components/ui/button") return { Button: Box };
      if (name === "@/components/ui/card")
        return Object.fromEntries(
          [
            "Card",
            "CardContent",
            "CardDescription",
            "CardHeader",
            "CardTitle",
          ].map((name) => [name, Box]),
        );
      return require(name);
    },
  },
);
test("actual workspace activity renders all five fixed private-preference labels in TR/EN, never interpolates supplied sensitive name or falls back", () => {
  assert.deepEqual(Object.keys(searchActivityKeys), expected);
  for (locale of ["tr", "en"]) {
    const html = renderToStaticMarkup(
      React.createElement(activityModule.exports.WorkspaceActivity, {
        workspaceId: "workspace",
      }),
    );
    for (const event of expected) {
      const key = searchActivityKeys[event],
        label = translatedModule.exports.translate(locale, key, {
          name: sensitive,
        });
      assert.notEqual(label, key);
      assert(!/[{}]/.test(label));
      assert(!label.includes(sensitive));
      assert(html.includes(label));
    }
    assert(!html.includes(sensitive));
    assert(!html.includes("workspaces.activity_changed"));
  }
  assert.deepEqual(fallbacks, []);
});
