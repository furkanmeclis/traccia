"use client";
import { useState } from "react";
import { useDrawerDraft } from "@/features/issue-drawer/draft-context";
import Link from "next/link";
import { useQuery } from "@tanstack/react-query";
import { useAuth } from "@/providers/auth-provider";
import { useLocale } from "@/providers/locale-provider";
import { Button } from "@/components/ui/button";
import { Section, LoadNotice } from "@/features/issues/issue-ui";
import { useWorkspace } from "@/features/workspaces/queries";
import { documentService, type Document, type Owner } from "./document.service";
import {
  useDocument,
  useDocumentMentions,
  useDocumentRevisions,
  useDocumentBacklinks,
  documentKeys,
} from "./queries";
import {
  DocumentOperationNotice,
  useDocumentOperation,
  DocumentPendingOperations,
} from "./document-operation";
import { DocumentEditor } from "./shared/editor";
import { DocumentRenderer } from "./shared/renderer";
import { WidgetEditorHarness } from "./shared/harness";
import {
  canonical,
  paragraph,
  parseCanonical,
  type Element,
} from "./shared/contract";
import { nodePatch, sectionPatch, type Patch } from "./shared/patch";
function DocumentSettings({
  workspaceId,
  owner,
  document,
  ready,
  review,
}: {
  workspaceId: string;
  owner: Owner;
  document: Document | null;
  ready: boolean;
  review: () => Promise<unknown>;
}) {
  const { t } = useLocale();
  const [initial, setInitial] = useDrawerDraft<Element[]>(
      "document.initial",
      () => document?.content ?? [paragraph()],
    ),
    [draft, setDraft] = useDrawerDraft<Element[]>("document.draft", initial),
    [baseContent, setBaseContent] = useDrawerDraft<Element[]>(
      "document.baseContent",
      initial,
    ),
    [baseline, setBaseline] = useDrawerDraft(
      "document.baseline",
      document?.revision ?? null,
    ),
    [generation, setGeneration] = useDrawerDraft("document.generation", 0),
    [raw, setRaw] = useDrawerDraft("document.raw", ""),
    [error, setError] = useDrawerDraft<string | null>("document.error", null),
    [patchError, setPatchError] = useDrawerDraft<string | null>(
      "document.patchError",
      null,
    ),
    [target, setTarget] = useDrawerDraft(
      "document.target",
      initial[0]?.id ?? "",
    );
  const mentions = useDocumentMentions(workspaceId, true),
    mentionsReady =
      mentions.isSuccess && !mentions.isError && !mentions.isFetching;
  const operation = useDocumentOperation(
    workspaceId,
    owner,
    "document-edit",
    t("documents.save"),
    (saved) => {
      setInitial(saved.content);
      setBaseContent(saved.content);
      setDraft(saved.content);
      setBaseline(saved.revision);
      setGeneration((n) => n + 1);
      setError(null);
      setTarget((prior) =>
        saved.content.some((n: Element) => n.id === prior)
          ? prior
          : (saved.content[0]?.id ?? ""),
      );
    },
  );
  const stale = baseline !== (document?.revision ?? null),
    dirty = JSON.stringify(baseContent) !== JSON.stringify(draft),
    canSave = ready && !stale && !operation.locked && !error;
  function patch(patches: Patch[]) {
    if (!document || !canSave) return;
    operation.submit({
      action: "document.patch",
      workspaceID: workspaceId,
      owner,
      documentID: document.id,
      body: {
        expected_revision: baseline!,
        format: "plate",
        schema_version: 1,
        patches,
      },
    });
  }
  return (
    <div className="space-y-4">
      <form
        className="space-y-4"
        onSubmit={(e) => {
          e.preventDefault();
          if (!canSave) return;
          try {
            const value = canonical(draft);
            if (document)
              operation.submit({
                action: "document.replace",
                workspaceID: workspaceId,
                owner,
                documentID: document.id,
                body: { ...value, expected_revision: baseline! },
              });
            else
              operation.submit({
                action: "document.create",
                workspaceID: workspaceId,
                owner,
                body: { ...value, owner_kind: owner.kind, owner_id: owner.id },
              });
          } catch (cause) {
            setError(String(cause));
          }
        }}
      >
        <LoadNotice
          pending={mentions.isPending}
          error={mentions.isError}
          retry={() => mentions.refetch()}
        />
        <DocumentEditor
          key={generation}
          initialValue={draft}
          label={t("documents.editor")}
          disabled={!ready || operation.locked}
          onChange={(value, error) => {
            if (value) setDraft(value);
            setError(error);
            setPatchError(null);
          }}
          mentions={mentions.data ?? []}
          mentionsReady={mentionsReady}
        />
        {stale && <p role="alert">{t("documents.stale")}</p>}
        <div className="flex flex-wrap gap-2">
          <Button type="submit" disabled={!canSave}>
            {t(document ? "documents.save" : "documents.create")}
          </Button>
          <Button
            type="button"
            variant="outline"
            disabled={operation.locked}
            onClick={() => void review()}
          >
            {t("documents.refresh")}
          </Button>
          <Button
            type="button"
            variant="outline"
            disabled={operation.locked || !ready}
            onClick={() => {
              const next = document?.content ?? [paragraph()];
              setInitial(next);
              setBaseContent(next);
              setDraft(next);
              setBaseline(document?.revision ?? null);
              setGeneration((n) => n + 1);
              setError(null);
            }}
          >
            {t("documents.resetDraft")}
          </Button>
        </div>
      </form>
      {operation.entry && (
        <DocumentOperationNotice
          operationKey={operation.operationKey}
          entry={operation.entry}
        />
      )}
      <details className="rounded border p-3">
        <summary>{t("documents.importJson")}</summary>
        <label className="block text-sm">
          {t("documents.importJson")}
          <textarea
            value={raw}
            onChange={(e) => setRaw(e.target.value)}
            className="min-h-28 w-full rounded border p-2 font-mono"
          />
        </label>
        <Button
          type="button"
          variant="outline"
          disabled={!ready || operation.locked}
          onClick={() => {
            try {
              const next = parseCanonical(raw).content;
              setInitial(next);
              setTarget(next[0]?.id ?? "");
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
      </details>
      {error && (
        <p role="alert">
          {t("documents.validation")}: {error}
        </p>
      )}
      {document && (
        <details className="space-y-3 rounded border p-3">
          <summary>{t("documents.patchTools")}</summary>
          <label className="block text-sm">
            {t("documents.node")}
            <select
              className="w-full rounded border p-2"
              value={target}
              onChange={(e) => setTarget(e.target.value)}
            >
              {draft.map((n) => (
                <option key={n.id} value={n.id}>
                  {n.type} · {n.id}
                </option>
              ))}
            </select>
          </label>
          <p className="text-muted-foreground text-sm">
            {t("documents.patchHelp")}
          </p>
          {patchError && <p role="alert">{patchError}</p>}
          <div className="flex flex-wrap gap-2">
            <Button
              type="button"
              disabled={!canSave || !target}
              onClick={() => {
                try {
                  patch([nodePatch(baseContent, draft, target)]);
                } catch (cause) {
                  setPatchError(
                    t(
                      "documents." +
                        String(cause instanceof Error ? cause.message : cause),
                    ),
                  );
                }
              }}
            >
              {t("documents.saveNode")}
            </Button>
            <Button
              type="button"
              disabled={
                !canSave ||
                !target ||
                !/^h[123]$/.test(draft.find((n) => n.id === target)?.type ?? "")
              }
              onClick={() => {
                try {
                  patch([sectionPatch(baseContent, draft, target)]);
                } catch (cause) {
                  setPatchError(
                    t(
                      "documents." +
                        String(cause instanceof Error ? cause.message : cause),
                    ),
                  );
                }
              }}
            >
              {t("documents.saveSection")}
            </Button>
            <Button
              type="button"
              variant="outline"
              disabled={!canSave || dirty}
              onClick={() =>
                patch([
                  {
                    op: "insert_nodes",
                    parent_id: null,
                    before_id: target || null,
                    nodes: [paragraph()],
                  },
                ])
              }
            >
              {t("documents.insertNode")}
            </Button>
            <Button
              type="button"
              variant="outline"
              disabled={!canSave || dirty || draft.length < 2 || !target}
              onClick={() => patch([{ op: "remove_node", node_id: target }])}
            >
              {t("documents.removeNode")}
            </Button>
            <Button
              type="button"
              variant="outline"
              disabled={
                !canSave || dirty || draft.findIndex((n) => n.id === target) < 1
              }
              onClick={() => {
                const i = draft.findIndex((n) => n.id === target);
                patch([
                  {
                    op: "move_node",
                    node_id: target,
                    parent_id: null,
                    before_id: draft[i - 1].id,
                  },
                ]);
              }}
            >
              {t("documents.moveUp")}
            </Button>
          </div>
        </details>
      )}
      <WidgetEditorHarness
        workspaceId={workspaceId}
        mentions={mentions.data ?? []}
        mentionsReady={mentionsReady}
      />
    </div>
  );
}
function DocumentHistory({
  workspaceId,
  owner,
  document,
  ready,
}: {
  workspaceId: string;
  owner: Owner;
  document: Document;
  ready: boolean;
}) {
  const { t } = useLocale();
  const { user } = useAuth();
  const list = useDocumentRevisions(workspaceId, document.id, true),
    [revision, setRevision] = useState(""),
    [format, setFormat] = useState<"" | "json" | "html" | "markdown">("");
  const selected = useQuery({
    queryKey: [
      ...documentKeys.all(workspaceId),
      user?.uuid,
      document.id,
      "revision",
      revision,
    ],
    enabled: !!revision,
    queryFn: () => documentService.revision(workspaceId, document.id, revision),
  });
  const exported = useQuery({
    queryKey: [
      ...documentKeys.all(workspaceId),
      user?.uuid,
      document.id,
      document.revision,
      "export",
      format,
    ],
    enabled: !!format,
    queryFn: () =>
      documentService.export(workspaceId, document.id, format || "json"),
  });
  const restore = useDocumentOperation(
    workspaceId,
    owner,
    "document-restore",
    t("documents.restore"),
  );
  return (
    <Section title={t("documents.revisions")}>
      <LoadNotice
        pending={list.isPending}
        error={list.isError}
        retry={() => list.refetch()}
      />
      <ul className="space-y-2">
        {list.data?.pages
          .flatMap((p) => p.items ?? [])
          .map((item) => (
            <li key={item.id} className="flex flex-wrap items-center gap-2">
              <Button
                type="button"
                variant="outline"
                onClick={() => setRevision(item.id)}
              >
                {item.id}
              </Button>
              <span className="text-sm break-all">
                {item.kind} · {item.actor_id} · {item.occurred_at}
              </span>
            </li>
          ))}
      </ul>
      {list.hasNextPage && (
        <Button
          type="button"
          variant="outline"
          disabled={list.isFetchingNextPage}
          onClick={() => void list.fetchNextPage()}
        >
          {t("issues.load_more")}
        </Button>
      )}
      {!!revision && (
        <div className="space-y-3 rounded border p-3">
          <LoadNotice
            pending={selected.isPending}
            error={selected.isError}
            retry={() => selected.refetch()}
          />
          {selected.data && !selected.isError && (
            <>
              <DocumentRenderer
                content={selected.data.document.content}
                workspaceId={workspaceId}
              />
              <details>
                <summary>{t("documents.fullJson")}</summary>
                <pre className="max-h-96 overflow-auto whitespace-pre-wrap">
                  {JSON.stringify(selected.data.document, null, 2)}
                </pre>
              </details>
              <Button
                type="button"
                disabled={!ready || restore.locked || selected.isFetching}
                onClick={() =>
                  restore.submit({
                    action: "document.restore",
                    workspaceID: workspaceId,
                    owner,
                    documentID: document.id,
                    body: {
                      expected_revision: document.revision,
                      revision_id: revision,
                    },
                  })
                }
              >
                {t("documents.restore")}
              </Button>
              <p className="text-muted-foreground text-sm">
                {t("documents.restoreHelp")}
              </p>
            </>
          )}
        </div>
      )}
      {restore.entry && (
        <DocumentOperationNotice
          operationKey={restore.operationKey}
          entry={restore.entry}
        />
      )}
      <div className="flex flex-wrap gap-2">
        {(["json", "html", "markdown"] as const).map((value) => (
          <Button
            type="button"
            variant="outline"
            key={value}
            onClick={() => setFormat(value)}
          >
            {t("documents.export")}: {value.toUpperCase()}
          </Button>
        ))}
      </div>
      {format && (
        <div className="space-y-2">
          <LoadNotice
            pending={exported.isPending}
            error={exported.isError}
            retry={() => exported.refetch()}
          />
          {format === "markdown" && <p>{t("documents.markdownLoss")}</p>}
          {exported.data && !exported.isError && (
            <>
              <p>
                {t("documents.exportRevision")}: {exported.data.revision}
              </p>
              {!!exported.data.losses?.length && (
                <p role="status">
                  {t("documents.exportLosses")}:{" "}
                  {exported.data.losses.join(", ")}
                </p>
              )}
              <pre
                className="max-h-96 overflow-auto whitespace-pre-wrap"
                data-testid="document-export"
              >
                {exported.data.content}
              </pre>
              <Button
                type="button"
                variant="outline"
                onClick={() => {
                  const url = URL.createObjectURL(
                    new Blob([exported.data.content], {
                      type: "text/plain;charset=utf-8",
                    }),
                  );
                  const a = window.document.createElement("a");
                  a.href = url;
                  a.download = `document.${format === "markdown" ? "md" : format}`;
                  a.click();
                  URL.revokeObjectURL(url);
                }}
              >
                {t("documents.download")}
              </Button>
            </>
          )}
        </div>
      )}
    </Section>
  );
}
export function DocumentPanel({
  workspaceId,
  owner,
  metadataVersion,
}: {
  workspaceId: string;
  owner: Owner;
  metadataVersion: number;
}) {
  const { t } = useLocale();
  const { user } = useAuth();
  const workspace = useWorkspace(workspaceId),
    allowed = workspace.isSuccess && !workspace.isError;
  const query = useDocument(workspaceId, owner, allowed, metadataVersion),
    backlinks = useDocumentBacklinks(workspaceId, owner, allowed);
  const ready =
    allowed &&
    !workspace.isFetching &&
    query.isSuccess &&
    !query.isError &&
    !query.isFetching;
  const current = query.data?.document ?? null;
  return (
    <Section
      title={t(
        owner.kind === "issue"
          ? "documents.description"
          : "documents.projectContent",
      )}
    >
      <DocumentPendingOperations workspaceId={workspaceId} />
      <LoadNotice
        pending={query.isPending}
        error={query.isError}
        retry={() => query.refetch()}
      />
      {query.data && (
        <div hidden={!allowed || query.isError} className="space-y-5">
          {current ? (
            <>
              <p className="text-muted-foreground text-xs break-all">
                {t("documents.revision")}: {current.revision}
              </p>
              <DocumentRenderer
                content={current.content}
                workspaceId={workspaceId}
              />
            </>
          ) : (
            <p>{t("documents.empty")}</p>
          )}
          <DocumentSettings
            key={JSON.stringify([
              workspaceId,
              user?.uuid,
              owner.kind,
              owner.id,
            ])}
            workspaceId={workspaceId}
            owner={owner}
            document={current}
            ready={ready && query.data.capabilities.manage}
            review={() => query.refetch({ throwOnError: true })}
          />
          {current && (
            <DocumentHistory
              workspaceId={workspaceId}
              owner={owner}
              document={current}
              ready={ready && query.data.capabilities.manage}
            />
          )}
        </div>
      )}
      <details>
        <summary>{t("documents.backlinks")}</summary>
        <LoadNotice
          pending={backlinks.isPending}
          error={backlinks.isError}
          retry={() => backlinks.refetch()}
        />
        <ul className="space-y-2">
          {backlinks.data?.pages
            .flatMap((p) => p.items ?? [])
            .map((item) => (
              <li key={item.id}>
                <Link
                  className="underline"
                  href={`/w/${workspaceId}/${item.owner_kind === "issue" ? "issues" : "projects"}/${item.owner_id}`}
                >
                  {item.value} · {item.node_id}
                </Link>
              </li>
            ))}
        </ul>
        {backlinks.hasNextPage && (
          <Button
            type="button"
            disabled={backlinks.isFetchingNextPage}
            onClick={() => void backlinks.fetchNextPage()}
          >
            {t("issues.load_more")}
          </Button>
        )}
      </details>
    </Section>
  );
}
