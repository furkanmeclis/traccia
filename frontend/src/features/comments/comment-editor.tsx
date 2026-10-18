"use client";
import { useEffect, useState, useSyncExternalStore } from "react";
import { Button } from "@/components/ui/button";
import { useLocale } from "@/providers/locale-provider";
import {
  DocumentEditor,
  type MentionChoice,
} from "@/features/documents/shared/editor";
import {
  canonical,
  parseCanonical,
  type CanonicalContent,
} from "@/features/documents/shared/contract";
import {
  nodePatch,
  sectionPatch,
  type Patch,
} from "@/features/documents/shared/patch";
import {
  commentDrafts as store,
  draftChanged,
  type CommentBaseline,
} from "./draft-state";
function PlateDraft({
  content,
  ...props
}: Omit<Parameters<typeof DocumentEditor>[0], "initialValue"> & {
  content: Parameters<typeof DocumentEditor>[0]["initialValue"];
}) {
  const [initial] = useState(() => structuredClone(content));
  return <DocumentEditor {...props} initialValue={initial} />;
}
export function CommentEditor({
  draftKey,
  baseline,
  currentRevision,
  ready,
  locked,
  mentions,
  mentionsReady,
  onSave,
  onPatch,
  patchReady = true,
  refresh,
}: {
  draftKey: string;
  baseline: CommentBaseline;
  currentRevision: string | null;
  ready: boolean;
  locked: boolean;
  mentions: MentionChoice[];
  mentionsReady: boolean;
  onSave: (value: CanonicalContent) => void;
  onPatch?: (patches: Patch[]) => void;
  patchReady?: boolean;
  refresh: () => Promise<unknown>;
}) {
  const { t } = useLocale(),
    drafts = useSyncExternalStore(
      store.subscribe,
      store.snapshot,
      store.serverSnapshot,
    ),
    draft = drafts.get(draftKey);
  useEffect(() => {
    store.ensure(draftKey, baseline);
  }, [draftKey, baseline]);
  if (!draft) return <p role="status">{t("comments.loadingEditor")}</p>;
  const stale = draft.revision !== currentRevision,
    canSave = ready && !locked && !stale && !draft.error;
  function patch(kind: "node" | "section") {
    if (!onPatch || !canSave || !patchReady) return;
    try {
      const value =
        kind === "node"
          ? nodePatch(draft!.baselineContent, draft!.content, draft!.target)
          : sectionPatch(draft!.baselineContent, draft!.content, draft!.target);
      onPatch([value]);
    } catch (error) {
      const code = error instanceof Error ? error.message : "";
      store.patch(draftKey, {
        error: t(
          code === "outsideNode" || code === "outsideSection"
            ? "comments.patchOutside"
            : "comments.invalidContent",
        ),
      });
    }
  }
  return (
    <div className="space-y-3">
      <form
        className="space-y-3"
        onSubmit={(event) => {
          event.preventDefault();
          if (!canSave) return;
          try {
            onSave(canonical(draft.content));
          } catch (error) {
            store.patch(draftKey, { error: String(error) });
          }
        }}
      >
        <PlateDraft
          key={draft.generation}
          content={draft.content}
          label={t(baseline ? "comments.editEditor" : "comments.createEditor")}
          disabled={!ready || locked}
          mentions={mentions}
          mentionsReady={mentionsReady}
          onChange={(content, error) =>
            store.patch(draftKey, { ...(content ? { content } : {}), error })
          }
        />
        {stale && <p role="alert">{t("comments.stale")}</p>}
        {draftChanged(draft) && (
          <p className="text-muted-foreground text-sm">{t("comments.dirty")}</p>
        )}
        <div className="flex flex-wrap gap-2">
          <Button type="submit" disabled={!canSave}>
            {t(baseline ? "comments.save" : "comments.create")}
          </Button>
          <Button
            type="button"
            variant="outline"
            disabled={locked}
            onClick={() => void refresh()}
          >
            {t("comments.refresh")}
          </Button>
          <Button
            type="button"
            variant="outline"
            disabled={!ready || locked}
            onClick={() => store.reset(draftKey, baseline)}
          >
            {t("comments.reset")}
          </Button>
        </div>
      </form>
      {draft.error && (
        <p role="alert">
          {t("comments.invalidContent")}: {draft.error}
        </p>
      )}
      <details className="rounded border p-3">
        <summary>{t("comments.import")}</summary>
        <label className="block text-sm">
          {t("comments.import")}
          <textarea
            className="min-h-28 w-full rounded border p-2 font-mono"
            value={draft.raw}
            onChange={(event) =>
              store.patch(draftKey, { raw: event.target.value })
            }
          />
        </label>
        <Button
          type="button"
          disabled={!ready || locked}
          onClick={() => {
            try {
              store.load(draftKey, parseCanonical(draft.raw).content);
            } catch (error) {
              store.patch(draftKey, { error: String(error) });
            }
          }}
        >
          {t("comments.loadJson")}
        </Button>
      </details>
      {onPatch && baseline && (
        <details className="space-y-2 rounded border p-3">
          <summary>{t("comments.patchTools")}</summary>
          <label className="block text-sm">
            {t("comments.node")}
            <select
              className="block w-full rounded border p-2"
              value={draft.target}
              onChange={(event) =>
                store.patch(draftKey, { target: event.target.value })
              }
            >
              {draft.content.map((node) => (
                <option key={node.id} value={node.id}>
                  {node.type} · {node.id}
                </option>
              ))}
            </select>
          </label>
          <p>{t("comments.patchHelp")}</p>
          <div className="flex flex-wrap gap-2">
            <Button
              type="button"
              disabled={!canSave || !patchReady}
              onClick={() => patch("node")}
            >
              {t("comments.patchNode")}
            </Button>
            <Button
              type="button"
              disabled={!canSave || !patchReady}
              onClick={() => patch("section")}
            >
              {t("comments.patchSection")}
            </Button>
          </div>
        </details>
      )}
    </div>
  );
}
