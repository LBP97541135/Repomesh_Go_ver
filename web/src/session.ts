import { useCallback, useEffect, useRef, useState } from "react";
import { endSession, readSession } from "./api";
import type { ApiError, Result, Session } from "./api";
import { forgetAttempt } from "./recovery";
import { clearAllOperationInputs } from "./projectRecovery";
import { clearModelRecovery } from "./modelRecovery";

export type SessionState =
  | { kind: "checking" }
  | { kind: "anonymous" }
  | { kind: "authenticated"; session: Session }
  | { kind: "unavailable"; error: ApiError };

const actorStorageKey = "repomesh.session.actor";

function reconcileStoredActor(actor: string | null): void {
  try {
    const previous = sessionStorage.getItem(actorStorageKey);
    if (actor === null) {
      sessionStorage.removeItem(actorStorageKey);
      return;
    }
    if (previous !== null && previous !== actor) {
      clearAllOperationInputs();
      clearModelRecovery();
    }
    sessionStorage.setItem(actorStorageKey, actor);
  } catch {
    if (actor === null) {
      clearAllOperationInputs();
      clearModelRecovery();
    }
  }
}

export function useSession() {
  const [state, setState] = useState<SessionState>({ kind: "checking" });
  const [generation, setGeneration] = useState(0);
  const version = useRef(0);
  const latest = useRef<SessionState>(state);
  const flight = useRef<{ controller: AbortController; promise: Promise<Result<Session> | null> } | null>(null);
  const channel = useRef<BroadcastChannel | null>(null);

  const update = useCallback((next: SessionState, invalidate: boolean) => {
    const previous = latest.current;
    if ((next.kind === "anonymous" && previous.kind === "authenticated") || (next.kind === "authenticated" && previous.kind === "authenticated" && next.session.user.id !== previous.session.user.id)) {
      clearAllOperationInputs();
      clearModelRecovery();
    }
    if (invalidate) {
      version.current += 1;
      setGeneration(version.current);
    }
    latest.current = next;
    setState(next);
  }, []);

  const isCurrent = useCallback((expected: number) => version.current === expected, []);
  const currentGeneration = useCallback(() => version.current, []);

  const invalidate = useCallback(() => {
    flight.current?.controller.abort();
    flight.current = null;
    update({ kind: "checking" }, true);
  }, [update]);

  const unauthorized = useCallback((expected: number) => {
    if (!isCurrent(expected)) return;
    clearAllOperationInputs();
    clearModelRecovery();
    reconcileStoredActor(null);
    flight.current?.controller.abort();
    flight.current = null;
    update({ kind: "anonymous" }, latest.current.kind !== "anonymous");
  }, [isCurrent, update]);

  const refresh = useCallback((clear = false): Promise<Result<Session> | null> => {
    if (clear && latest.current.kind !== "authenticated") {
      if (flight.current) return flight.current.promise;
      invalidate();
    }
    if (flight.current) return flight.current.promise;
    const expected = version.current;
    const controller = new AbortController();
    const promise = readSession(controller.signal).then((result) => {
      if (!isCurrent(expected) || controller.signal.aborted) return null;
      if (result.kind === "ok") {
        reconcileStoredActor(result.value.user.id);
        const previous = latest.current;
        const changed = previous.kind !== "authenticated" || previous.session.user.id !== result.value.user.id;
        update({ kind: "authenticated", session: result.value }, changed);
      } else if (result.status === 401) {
        clearAllOperationInputs();
        clearModelRecovery();
        reconcileStoredActor(null);
        update({ kind: "anonymous" }, latest.current.kind !== "anonymous");
      } else {
        update({ kind: "unavailable", error: result }, latest.current.kind === "authenticated");
      }
      return result;
    }).finally(() => {
      if (flight.current?.controller === controller) flight.current = null;
    });
    flight.current = { controller, promise };
    return promise;
  }, [invalidate, isCurrent, update]);

  const announce = useCallback(() => { channel.current?.postMessage("session-changed"); }, []);

  const logout = useCallback(async () => {
    const before = latest.current;
    if (before.kind !== "authenticated") return;
    const csrfToken = before.session.csrfToken;
    forgetAttempt();
    clearAllOperationInputs();
    clearModelRecovery();
    reconcileStoredActor(null);
    invalidate();
    announce();
    const expected = version.current;
    const result = await endSession(csrfToken);
    if (!isCurrent(expected)) return;
    update(result.kind === "ok" ? { kind: "anonymous" } : { kind: "unavailable", error: result }, false);
    announce();
  }, [announce, invalidate, isCurrent, update]);

  useEffect(() => {
    void refresh();
    const onVisible = () => {
      if (document.visibilityState === "visible") void refresh();
    };
    const onPageShow = (event: PageTransitionEvent) => {
      if (event.persisted) void refresh(true);
    };
    const onFocus = () => { void refresh(); };
    const broadcast = typeof BroadcastChannel === "undefined" ? null : new BroadcastChannel("repomesh-session");
    channel.current = broadcast;
    if (broadcast) broadcast.onmessage = (event: MessageEvent<unknown>) => {
      if (event.data === "session-changed") {
        clearAllOperationInputs();
        clearModelRecovery();
        void refresh(true);
      }
    };
    document.addEventListener("visibilitychange", onVisible);
    window.addEventListener("pageshow", onPageShow);
    window.addEventListener("focus", onFocus);
    return () => {
      flight.current?.controller.abort();
      flight.current = null;
      channel.current = null;
      broadcast?.close();
      document.removeEventListener("visibilitychange", onVisible);
      window.removeEventListener("pageshow", onPageShow);
      window.removeEventListener("focus", onFocus);
    };
  }, [refresh]);

  return { state, generation, currentGeneration, isCurrent, unauthorized, refresh, logout, announce };
}

export type SessionController = ReturnType<typeof useSession>;
