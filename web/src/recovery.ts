import { isAttemptId } from "./api";
import { parseDestination } from "./routes";
import type { Destination, SavedAuthorization } from "./routes";

export type SavedAttempt = SavedAuthorization;
const key = "repomesh.auth.attempt";

export function savedAttempt(): SavedAttempt | null {
  try {
    const raw: unknown = JSON.parse(sessionStorage.getItem(key) ?? "null");
    if (typeof raw !== "object" || raw === null || !("attemptId" in raw) || !("purpose" in raw) || !("createdAt" in raw)) return null;
    if (!isAttemptId(raw.attemptId) || (raw.purpose !== "login" && raw.purpose !== "reconnect") || typeof raw.createdAt !== "string" || !Number.isFinite(Date.parse(raw.createdAt))) return null;
    const destination: Destination = "destination" in raw ? parseDestination(raw.destination) ?? { kind: "home" } : { kind: "home" };
    return { attemptId: raw.attemptId, purpose: raw.purpose, createdAt: raw.createdAt, destination };
  } catch {
    return null;
  }
}

export function rememberAttempt(attempt: SavedAttempt): void {
  try { sessionStorage.setItem(key, JSON.stringify(attempt)); } catch { /* The result route remains available without browser storage. */ }
}

export function forgetAttempt(): void {
  try { sessionStorage.removeItem(key); } catch { /* Storage can be disabled by the browser. */ }
}
