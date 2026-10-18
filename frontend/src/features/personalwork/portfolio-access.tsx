import type { ReactNode } from "react";
export function capturedScopesAuthorized(
  anchorID: string,
  capturedWorkspaceIDs: readonly string[],
  currentWorkspaceIDs: readonly string[],
  complete: boolean,
) {
  if (!complete || !capturedWorkspaceIDs.length) return false;
  const current = new Set(currentWorkspaceIDs.map((id) => id.toLowerCase()));
  return (
    current.has(anchorID.toLowerCase()) &&
    capturedWorkspaceIDs.every((id) => current.has(id.toLowerCase()))
  );
}
/** Do not mount a cached result observer after current discovery removes any original scope. */
export function PortfolioAccessGate({
  anchorID,
  capturedWorkspaceIDs,
  currentWorkspaceIDs,
  complete,
  readable,
  children,
}: {
  anchorID: string;
  capturedWorkspaceIDs: readonly string[];
  currentWorkspaceIDs: readonly string[];
  complete: boolean;
  readable: boolean;
  children: ReactNode;
}) {
  if (
    !readable ||
    !capturedScopesAuthorized(
      anchorID,
      capturedWorkspaceIDs,
      currentWorkspaceIDs,
      complete,
    )
  )
    return null;
  return children;
}
