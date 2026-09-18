import { useCallback, useEffect, useRef, useState } from "react";
import type { ApiError, Result } from "./api";
import { clearAllOperationInputs } from "./projectRecovery";
import type { SessionController } from "./session";

export type RequestContext = { actor: string; sessionGeneration: number; route: string; query: string };
export type RequestTicket = RequestContext & { requestGeneration: number; signal: AbortSignal };
export type RequestGate = { begin(): RequestTicket; accepts(ticket: RequestTicket): boolean; invalidate(): void };
export type SnapshotState<T> =
  | { kind: "loading" }
  | { kind: "ready"; value: T }
  | { kind: "error"; error: ApiError }
  | { kind: "inaccessible" };
export type SnapshotController<T> = { state: SnapshotState<T>; refresh(): Promise<void> };

function sameContext(left: RequestContext, right: RequestContext): boolean {
  return left.actor === right.actor && left.sessionGeneration === right.sessionGeneration && left.route === right.route && left.query === right.query;
}

export function useRequestGate(context: RequestContext): RequestGate {
  const contextRef = useRef(context);
  const generationRef = useRef(0);
  const controllerRef = useRef<AbortController | null>(null);
  if (!sameContext(contextRef.current, context)) {
    contextRef.current = context;
    generationRef.current += 1;
  }
  const gateRef = useRef<RequestGate | null>(null);
  if (gateRef.current === null) {
    gateRef.current = {
      begin() {
        controllerRef.current?.abort();
        const controller = new AbortController();
        controllerRef.current = controller;
        generationRef.current += 1;
        return { ...contextRef.current, requestGeneration: generationRef.current, signal: controller.signal };
      },
      accepts(ticket) {
        return !ticket.signal.aborted && ticket.requestGeneration === generationRef.current && sameContext(ticket, contextRef.current);
      },
      invalidate() {
        generationRef.current += 1;
        controllerRef.current?.abort();
        controllerRef.current = null;
      },
    };
  }
  useEffect(() => {
    return () => {
      gateRef.current?.invalidate();
    };
  }, [context.actor, context.query, context.route, context.sessionGeneration]);
  return gateRef.current;
}

export function useProjectSnapshot<T>(args: {
  context: RequestContext;
  auth: SessionController;
  load: (signal: AbortSignal) => Promise<Result<T>>;
  onInaccessible: () => void;
}): SnapshotController<T> {
  const key = `${args.context.actor}\u0000${args.context.sessionGeneration}\u0000${args.context.route}\u0000${args.context.query}`;
  const gate = useRequestGate(args.context);
  const unauthorized = args.auth.unauthorized;
  const loadRef = useRef(args.load);
  const inaccessibleRef = useRef(args.onInaccessible);
  loadRef.current = args.load;
  inaccessibleRef.current = args.onInaccessible;
  const [stored, setStored] = useState<{ key: string; state: SnapshotState<T> }>({ key, state: { kind: "loading" } });
  const flight = useRef<Promise<void> | null>(null);
  const queued = useRef(false);
  const mounted = useRef(true);

  const run = useCallback(async (): Promise<void> => {
    if (flight.current !== null) {
      queued.current = true;
      return flight.current;
    }
    const execute = async () => {
      do {
        queued.current = false;
        const ticket = gate.begin();
        setStored({ key, state: { kind: "loading" } });
        const result = await loadRef.current(ticket.signal);
        if (!mounted.current || !gate.accepts(ticket)) continue;
        if (result.kind === "ok") {
          setStored({ key, state: { kind: "ready", value: result.value } });
        } else if (result.status === 401) {
          clearAllOperationInputs();
          unauthorized(ticket.sessionGeneration);
          inaccessibleRef.current();
          setStored({ key, state: { kind: "inaccessible" } });
        } else if (result.code === "RESOURCE_NOT_FOUND") {
          inaccessibleRef.current();
          setStored({ key, state: { kind: "inaccessible" } });
        } else {
          setStored({ key, state: { kind: "error", error: result } });
        }
      } while (queued.current && mounted.current);
    };
    const promise = execute().finally(() => {
      if (flight.current === promise) flight.current = null;
    });
    flight.current = promise;
    return promise;
  }, [gate, key, unauthorized]);

  useEffect(() => {
    mounted.current = true;
    void run();
    return () => {
      mounted.current = false;
      queued.current = false;
      gate.invalidate();
      flight.current = null;
    };
  }, [gate, key, run]);

  return { state: stored.key === key ? stored.state : { kind: "loading" }, refresh: run };
}
