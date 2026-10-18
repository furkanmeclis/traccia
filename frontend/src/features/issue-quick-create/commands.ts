import type { components } from "@/generated/api";
import { executeIssueCommand } from "@/features/issues/issue.service";
import { executeDocumentCommand } from "@/features/documents/document.service";
import { canonical } from "@/features/documents/shared/contract";
import type { QuickIssueDraft } from "./draft-state";
type S = components["schemas"];
export type QuickCommand =
  | {
      action: "issue.create";
      path: { workspaceID: string };
      body: S["ProductIssuesCreateInput"];
    }
  | {
      action: "issue.create-rich";
      workspaceID: string;
      owner: { kind: "issue"; id: "new" };
      body: S["ProductIssuesCreateRichInput"];
    };
export function captureQuickCommand(
  draft: QuickIssueDraft,
  metadata: S["ProductIssuesCreateInput"],
): QuickCommand {
  if (draft.descriptionEnabled)
    return {
      action: "issue.create-rich",
      workspaceID: draft.workspaceID,
      owner: { kind: "issue", id: "new" },
      body: { issue: structuredClone(metadata), ...canonical(draft.content) },
    };
  return {
    action: "issue.create",
    path: { workspaceID: draft.workspaceID },
    body: structuredClone(metadata),
  };
}
export async function executeQuickCommand(command: QuickCommand, key: string) {
  if (command.action === "issue.create") {
    const result = await executeIssueCommand(command, key);
    return result.issue.id;
  }
  const result = await executeDocumentCommand(command, key);
  if (result.document.owner_kind !== "issue")
    throw new Error("QUICK_RESULT_OWNER");
  return result.document.owner_id;
}
