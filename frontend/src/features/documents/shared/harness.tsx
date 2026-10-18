"use client";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import { useLocale } from "@/providers/locale-provider";
import { DocumentEditor, type MentionChoice } from "./editor";
import { DocumentRenderer } from "./renderer";
import { documentFixture } from "./fixtures";
import { canonical, parseCanonical, type Element } from "./contract";
/** Small embedded surface using the exact panel editor, contract and renderer. */
export function WidgetEditorHarness({
  mentions,
  mentionsReady,
  workspaceId,
}: {
  workspaceId: string;
  mentions: MentionChoice[];
  mentionsReady: boolean;
}) {
  const { t } = useLocale();
  const [initial, setInitial] = useState(documentFixture),
    [draft, setDraft] = useState<Element[]>(initial),
    [generation, setGeneration] = useState(0),
    [raw, setRaw] = useState(""),
    [error, setError] = useState<string | null>(null);
  return (
    <details className="rounded border p-3">
      <summary>{t("documents.harness")}</summary>
      <div
        className="mt-3 w-full max-w-[390px] space-y-3"
        data-testid="widget-editor-harness"
      >
        <DocumentEditor
          key={generation}
          initialValue={initial}
          label={t("documents.harnessEditor")}
          onChange={(value, error) => {
            if (value) setDraft(value);
            setError(error);
          }}
          mentions={mentions}
          mentionsReady={mentionsReady}
        />
        <Button
          type="button"
          disabled={!!error}
          onClick={() => {
            try {
              const next = parseCanonical(
                JSON.stringify(canonical(draft)),
              ).content;
              setInitial(next);
              setDraft(next);
              setGeneration((n) => n + 1);
              setError(null);
            } catch (cause) {
              setError(String(cause));
            }
          }}
        >
          {t("documents.roundtrip")}
        </Button>
        <label className="block text-sm">
          {t("documents.importJson")}
          <textarea
            className="min-h-24 w-full rounded border p-2 font-mono"
            value={raw}
            onChange={(e) => setRaw(e.target.value)}
          />
        </label>
        <Button
          type="button"
          onClick={() => {
            try {
              const next = parseCanonical(raw).content;
              setInitial(next);
              setDraft(next);
              setGeneration((n) => n + 1);
              setError(null);
            } catch (cause) {
              setError(String(cause));
            }
          }}
        >
          {t("documents.loadJson")}
        </Button>
        {error && (
          <p role="alert">
            {t("documents.validation")}: {error}
          </p>
        )}
        <DocumentRenderer content={draft} workspaceId={workspaceId} />
        <pre
          data-testid="widget-canonical-json"
          className="max-h-48 overflow-auto text-xs"
        >
          {JSON.stringify({
            format: "plate",
            schema_version: 1,
            content: draft,
          })}
        </pre>
      </div>
    </details>
  );
}
