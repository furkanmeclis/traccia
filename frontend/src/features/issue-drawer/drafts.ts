// Local editor snapshots are separate from immutable command/receipt stores.
export function createDrawerDrafts() {
  const scopes = new Map<string, Map<string, unknown>>();
  return {
    read<T>(scope: string, slot: string, initial: () => T): T {
      let values = scopes.get(scope);
      if (!values) scopes.set(scope, (values = new Map()));
      if (!values.has(slot)) values.set(slot, initial());
      return values.get(slot) as T;
    },
    write<T>(scope: string, slot: string, value: T) {
      let values = scopes.get(scope);
      if (!values) scopes.set(scope, (values = new Map()));
      values.set(slot, value);
    },
  };
}
export const drawerDrafts = createDrawerDrafts();
export function drawerScope(
  actorID: string,
  workspaceID: string,
  issueID: string,
) {
  return JSON.stringify([actorID, workspaceID, issueID]);
}
