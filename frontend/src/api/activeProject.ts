/** Account-scoped browsing preference. Only validated projects enter active memory. */
let actor: string | null = null;
let active: string | null = null;
const storageKey = () => `repomesh-active-project:${actor}`;
export function beginProjectSession(accountId: string): string | null {
  actor = accountId;
  active = null;
  try { return localStorage.getItem(storageKey()); } catch { return null; }
}
export function readActiveProject(): string | null { return active; }
export function setActiveProject(projectId: string): void {
  active = projectId;
  if (actor) try { localStorage.setItem(storageKey(), projectId); } catch { /* memory only */ }
}
export function clearActiveProject(): void {
  active = null;
  if (actor) try { localStorage.removeItem(storageKey()); } catch { /* memory only */ }
}
