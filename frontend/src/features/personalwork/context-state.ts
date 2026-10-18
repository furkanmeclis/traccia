export function createPersonalContext() {
  let current: {
    actorID: string | null;
    workspaceID: string | null;
    generation: number;
  } = { actorID: null, workspaceID: null, generation: 0 };
  const server = current,
    listeners = new Set<() => void>();
  return {
    snapshot: () => current,
    serverSnapshot: () => server,
    subscribe(fn: () => void) {
      listeners.add(fn);
      return () => {
        listeners.delete(fn);
      };
    },
    activate(actorID: string | null, workspaceID: string | null) {
      actorID = actorID?.toLowerCase() ?? null;
      workspaceID = workspaceID?.toLowerCase() ?? null;
      if (current.actorID === actorID && current.workspaceID === workspaceID)
        return;
      current = { actorID, workspaceID, generation: current.generation + 1 };
      listeners.forEach((fn) => fn());
    },
    matches(ticket: typeof current) {
      return (
        ticket.generation === current.generation &&
        ticket.actorID === current.actorID &&
        ticket.workspaceID === current.workspaceID
      );
    },
  };
}
export const personalContext = createPersonalContext();
