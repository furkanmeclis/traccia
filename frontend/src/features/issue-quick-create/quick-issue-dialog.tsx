"use client";
import { useEffect, useRef, useState } from "react";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@/components/ui/dialog";
import { IssueForm } from "@/features/issues/issue-form";
import { useWorkspace } from "@/features/workspaces/queries";
import { useDocumentMentions } from "@/features/documents/queries";
import { DocumentEditor } from "@/features/documents/shared/editor";
import {
  paragraph,
  validateContent,
} from "@/features/documents/shared/contract";
import { useAuth } from "@/providers/auth-provider";
import { useLocale } from "@/providers/locale-provider";
import {
  initialMetadata,
  type QuickIssueCreated,
  type QuickIssueContext,
} from "./draft-state";
import {
  activateQuickContext,
  quickDrafts,
  quickContextMatches,
  quickContextTicket,
  quickWorkspaceMatches,
} from "./quick-store";
import {
  QuickOperationNotice,
  useQuickDrafts,
  useQuickOperation,
} from "./operation";
export type { QuickIssueCreated } from "./draft-state";
export type QuickIssueDialogProps = QuickIssueContext & {
  initialTeamID?: string;
  initialProjectID?: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreated: (created: QuickIssueCreated) => void;
  reviewLatest: () => Promise<unknown>;
  canCreate: boolean;
  returnFocus?: () => HTMLElement | null;
};
export function QuickIssueDialog(props: QuickIssueDialogProps) {
  const { user, isAuthenticated, bootstrapped } = useAuth();
  const trusted =
    bootstrapped &&
    isAuthenticated &&
    user?.uuid.toLowerCase() === props.actorID.toLowerCase();
  useEffect(() => {
    if (!props.open || !trusted)
      activateQuickContext(
        trusted ? props.actorID : undefined,
        trusted ? props.workspaceID : undefined,
        null,
      );
    return () => {
      activateQuickContext();
    };
  }, [trusted, props.actorID, props.workspaceID, props.open]);
  if (!props.open || !trusted) return null;
  return (
    <ActiveQuickIssue
      key={JSON.stringify([
        props.actorID.toLowerCase(),
        props.workspaceID.toLowerCase(),
      ])}
      {...props}
    />
  );
}
function ActiveQuickIssue(props: QuickIssueDialogProps) {
  const { t } = useLocale(),
    workspace = useWorkspace(props.workspaceID);
  const [draftID] = useState(
    () =>
      quickDrafts.ensure(
        props,
        initialMetadata(props.initialTeamID, props.initialProjectID),
        [paragraph()],
      ).id,
  );
  useQuickDrafts();
  const draft = quickDrafts.get(draftID),
    readable =
      workspace.isSuccess && !workspace.isError && !workspace.isFetching,
    currentScope = useRef(props),
    contentRef = useRef<HTMLDivElement | null>(null),
    opener = useRef<HTMLElement | null>(
      typeof document !== "undefined" &&
        document.activeElement instanceof HTMLElement
        ? document.activeElement
        : null,
    );
  useEffect(() => {
    currentScope.current = props;
  }, [props]);
  useEffect(() => {
    activateQuickContext(props.actorID, props.workspaceID, draftID);
    return () => {
      activateQuickContext(props.actorID, props.workspaceID, null);
    };
  }, [props.actorID, props.workspaceID, draftID]);
  const onCreated = (created: QuickIssueCreated) => {
    const scope = currentScope.current;
    if (
      created.actorID.toLowerCase() !== scope.actorID.toLowerCase() ||
      created.workspaceID.toLowerCase() !== scope.workspaceID.toLowerCase() ||
      created.draftID !== draftID
    )
      return;
    scope.onCreated(created);
  };
  const operation = useQuickOperation(
      draft,
      readable && props.canCreate,
      onCreated,
    ),
    mentions = useDocumentMentions(props.workspaceID, readable);
  if (!draft) return null;
  let validDescription = !draft.descriptionEnabled;
  if (draft.descriptionEnabled && !draft.editorError) {
    try {
      validateContent(draft.content);
      validDescription = true;
    } catch {
      validDescription = false;
    }
  }
  const locked = !!operation.entry || !!draft.createdIssueID,
    titleExact =
      draft.metadata.values.title === draft.metadata.values.title.trim();
  function restoreFocus() {
    const scope = currentScope.current;
    queueMicrotask(() => {
      if (!quickWorkspaceMatches(scope)) return;
      const focused = document.activeElement;
      if (
        focused instanceof HTMLElement &&
        focused !== document.body &&
        focused.isConnected &&
        !contentRef.current?.contains(focused)
      )
        return;
      const target = scope.returnFocus ? scope.returnFocus() : opener.current;
      if (
        target?.isConnected &&
        target.getClientRects().length > 0 &&
        !target.closest("[hidden],[inert]")
      )
        target.focus();
    });
  }
  return (
    <Dialog open onOpenChange={props.onOpenChange}>
      <DialogContent
        ref={contentRef}
        closeLabel={t("quickissue.close")}
        className="max-h-[90svh] max-w-3xl overflow-y-auto"
        onCloseAutoFocus={(event) => {
          event.preventDefault();
          restoreFocus();
        }}
      >
        <DialogHeader>
          <DialogTitle>{t("quickissue.title")}</DialogTitle>
          <DialogDescription>{t("quickissue.description")}</DialogDescription>
        </DialogHeader>
        {!readable && <p role="alert">{t("quickissue.accessRequired")}</p>}
        {draft.createdIssueID && readable ? (
          <div className="space-y-3">
            <p role="status">{t("quickissue.created")}</p>
            <Button
              type="button"
              onClick={() => {
                const ticket = quickContextTicket();
                if (
                  !quickContextMatches(props, draftID, ticket) ||
                  !draft.createdIssueID
                )
                  return;
                onCreated({
                  actorID: props.actorID,
                  workspaceID: props.workspaceID,
                  draftID,
                  issueID: draft.createdIssueID,
                });
                quickDrafts.complete(props, draftID, draft.revision);
              }}
            >
              {t("quickissue.openCreated")}
            </Button>
          </div>
        ) : (
          <>
            <label className="flex items-center gap-2">
              <input
                type="checkbox"
                checked={draft.descriptionEnabled}
                disabled={locked}
                onChange={(event) =>
                  quickDrafts.patch(props, draftID, {
                    descriptionEnabled: event.target.checked,
                  })
                }
              />
              {t("quickissue.includeDescription")}
            </label>
            {draft.descriptionEnabled && (
              <DocumentEditor
                initialValue={draft.content}
                label={t("quickissue.plateDescription")}
                disabled={locked || !readable || !props.canCreate}
                onChange={(content, error) => {
                  if (content)
                    quickDrafts.patch(props, draftID, {
                      content,
                      editorError: error,
                    });
                  else if (error)
                    quickDrafts.patch(props, draftID, { editorError: error });
                }}
                mentions={readable ? (mentions.data ?? []) : []}
                mentionsReady={
                  readable &&
                  mentions.isSuccess &&
                  !mentions.isError &&
                  !mentions.isFetching
                }
              />
            )}
            {readable ? (
              <IssueForm
                workspaceId={props.workspaceID}
                canManage={props.canCreate && !locked}
                reviewLatest={props.reviewLatest}
                controlledDraft={{
                  snapshot: draft.metadata,
                  onChange: (metadata) =>
                    quickDrafts.patch(props, draftID, { metadata }),
                }}
                createOverride={{
                  locked,
                  ready: validDescription && titleExact,
                  submit: (metadata) => {
                    void operation.submit(metadata);
                  },
                }}
              />
            ) : (
              <details>
                <summary>{t("quickissue.localDraft")}</summary>
                <pre className="max-h-64 overflow-auto text-xs break-all whitespace-pre-wrap">
                  {JSON.stringify(draft.metadata.values, null, 2)}
                </pre>
              </details>
            )}
            {!titleExact && <p role="alert">{t("quickissue.titleBoundary")}</p>}
          </>
        )}
        {(operation.error || draft.editorError) && (
          <p role="alert">{t("quickissue.validation")}</p>
        )}
        {operation.entry && (
          <QuickOperationNotice
            entry={operation.entry}
            ready={readable && props.canCreate}
            onCreated={onCreated}
          />
        )}
        <div className="flex flex-wrap gap-2">
          <Button
            type="button"
            variant="outline"
            onClick={() => props.onOpenChange(false)}
          >
            {t("quickissue.keepClose")}
          </Button>
          <Button
            type="button"
            variant="outline"
            disabled={locked || draft.uncertain}
            onClick={() => {
              if (quickDrafts.discard(props, draftID)) {
                props.onOpenChange(false);
              }
            }}
          >
            {t("quickissue.discard")}
          </Button>
        </div>
        <p className="text-muted-foreground text-sm">
          {t("quickissue.filesSeparate")}
        </p>
      </DialogContent>
    </Dialog>
  );
}
