"use client";
import { useState } from "react";
import { useLocale } from "@/providers/locale-provider";
import { IssueForm } from "@/features/issues/issue-form";
import { DocumentEditor } from "./shared/editor";
import { canonical, paragraph, type Element } from "./shared/contract";
import { useDocumentMentions } from "./queries";
import {
  useDocumentOperation,
  DocumentOperationNotice,
} from "./document-operation";
export function RichIssueForm({
  workspaceId,
  initialTeam,
  initialProject,
  canManage,
  reviewLatest,
  onDone,
}: {
  workspaceId: string;
  initialTeam?: string;
  initialProject?: string;
  canManage: boolean;
  reviewLatest: () => Promise<unknown>;
  onDone?: () => void;
}) {
  const { t } = useLocale();
  const [initial, setInitial] = useState(() => [paragraph()]),
    [draft, setDraft] = useState<Element[]>(initial),
    [error, setError] = useState<string | null>(null),
    [generation, setGeneration] = useState(0);
  const mentions = useDocumentMentions(workspaceId, true),
    owner = { kind: "issue" as const, id: "new" };
  const operation = useDocumentOperation(
    workspaceId,
    owner,
    "rich-issue-create",
    t("documents.createRich"),
    () => {
      const next = [paragraph()];
      setInitial(next);
      setDraft(next);
      setGeneration((n) => n + 1);
      setError(null);
      onDone?.();
    },
  );
  return (
    <div className="space-y-4">
      <DocumentEditor
        key={generation}
        initialValue={initial}
        label={t("documents.newIssueDescription")}
        disabled={!canManage || operation.locked}
        onChange={(value, error) => {
          if (value) setDraft(value);
          setError(error);
        }}
        mentions={mentions.data ?? []}
        mentionsReady={
          mentions.isSuccess && !mentions.isError && !mentions.isFetching
        }
      />
      {error && (
        <p role="alert">
          {t("documents.validation")}: {error}
        </p>
      )}
      <IssueForm
        key={"metadata:" + generation}
        workspaceId={workspaceId}
        initialTeam={initialTeam}
        initialProject={initialProject}
        canManage={canManage}
        reviewLatest={reviewLatest}
        createOverride={{
          locked: operation.locked,
          ready: !error,
          submit: (issue) => {
            try {
              operation.submit({
                action: "issue.create-rich",
                workspaceID: workspaceId,
                owner,
                body: { issue, ...canonical(draft) },
              });
            } catch (cause) {
              setError(String(cause));
            }
          },
        }}
      />
      {operation.entry && (
        <DocumentOperationNotice
          operationKey={operation.operationKey}
          entry={operation.entry}
        />
      )}
    </div>
  );
}
