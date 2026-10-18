/** A token expires on every actor/workspace transition, including A→B→A. */
export class ReviewContext {
  private scope = "";
  private generation = 0;
  change(actor?: string, workspace?: string): boolean {
    const next = JSON.stringify([
      actor?.toLowerCase(),
      workspace?.toLowerCase(),
    ]);
    if (next === this.scope) return false;
    this.scope = next;
    this.generation++;
    return true;
  }
  token() {
    return this.generation;
  }
  current(token: number) {
    return token === this.generation;
  }
}
