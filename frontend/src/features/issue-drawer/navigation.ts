export function editingTarget(target: EventTarget | null) {
  return (
    target instanceof Element &&
    !!target.closest(
      'input,textarea,select,[contenteditable]:not([contenteditable="false"]),[role="textbox"]',
    )
  );
}
export function plainIssueClick(event: {
  button: number;
  defaultPrevented: boolean;
  metaKey: boolean;
  ctrlKey: boolean;
  shiftKey: boolean;
  altKey: boolean;
}) {
  return (
    !event.defaultPrevented &&
    event.button === 0 &&
    !event.metaKey &&
    !event.ctrlKey &&
    !event.shiftKey &&
    !event.altKey
  );
}
export function adjacentIssue(ids: string[], selected: string, delta: -1 | 1) {
  const index = ids.indexOf(selected);
  return index < 0 ? null : (ids[index + delta] ?? null);
}
export function visibleFocusTarget(
  node: HTMLElement | null,
): node is HTMLElement {
  return (
    !!node?.isConnected &&
    !node.closest("[hidden],[inert]") &&
    node.getClientRects().length > 0 &&
    !node.matches(":disabled")
  );
}
