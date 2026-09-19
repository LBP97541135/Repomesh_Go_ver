import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import { ArrowLeft, Minus, Plus, UsersRound } from "lucide-react";
import { ApiError } from "../api/http";
import { listRepositories } from "../api/repositories";
import {
  RepositoryTeamConflictError,
  changeRepositoryTeam,
  createRepositoryTeam,
  getRepositoryTeam,
  repositoryTeamBusyLabels,
  repositoryTeamErrorCode,
  type RepositoryTeamMember,
  type RepositoryTeamSnapshot,
} from "../api/repositoryTeams";

type PageState = "loading" | "ready" | "absent" | "forbidden" | "error";
type MutationState = "idle" | "submitting" | "busy" | "stale" | "reconciliation" | "error";

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : "The Team request could not be completed.";
}

function MemberRow({ member, label }: { member: RepositoryTeamMember; label: string }) {
  return (
    <div className="grid gap-1 border-t border-line py-2.5 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center">
      <span className="font-mono text-[12px] text-tx">{label}</span>
      <span className="text-[11px] text-tx2">
        {member.runtime_phase} · {member.active_task_count} active task{member.active_task_count === 1 ? "" : "s"}
      </span>
    </div>
  );
}

export function RepositoryTeamPage({
  repositoryId,
  isAdmin,
  onBack,
  onToast,
}: {
  repositoryId: string;
  isAdmin: boolean;
  onBack: () => void;
  onToast: (text: string) => void;
}) {
  const [pageState, setPageState] = useState<PageState>("loading");
  const [team, setTeam] = useState<RepositoryTeamSnapshot | null>(null);
  const [capacity, setCapacity] = useState<number>(1);
  const [createError, setCreateError] = useState<string | null>(null);
  const [feedback, setFeedback] = useState<string | null>(null);
  const [mutationState, setMutationState] = useState<MutationState>("idle");
  const [busyLabels, setBusyLabels] = useState<string[]>([]);
  const [catalogRepositoryName, setCatalogRepositoryName] = useState<string | null>(null);
  const teamRequestGeneration = useRef(0);
  const catalogRequestGeneration = useRef(0);

  useLayoutEffect(() => {
    teamRequestGeneration.current += 1;
    catalogRequestGeneration.current += 1;
    setPageState("loading");
    setTeam(null);
    setCapacity(1);
    setCreateError(null);
    setFeedback(null);
    setMutationState("idle");
    setBusyLabels([]);
    setCatalogRepositoryName(null);
  }, [repositoryId]);

  const loadTeam = useCallback(async () => {
    const requestGeneration = ++teamRequestGeneration.current;
    setPageState("loading");
    setTeam(null);
    setCapacity(1);
    setCreateError(null);
    setFeedback(null);
    setMutationState("idle");
    setBusyLabels([]);
    try {
      const snapshot = await getRepositoryTeam(repositoryId);
      if (requestGeneration !== teamRequestGeneration.current) return;
      setTeam(snapshot);
      setCapacity(snapshot.workers.length);
      setPageState("ready");
    } catch (error) {
      if (requestGeneration !== teamRequestGeneration.current) return;
      if (error instanceof ApiError && error.status === 404) {
        setTeam(null);
        setCapacity(1);
        setPageState("absent");
      } else if (error instanceof ApiError && error.status === 403) {
        setPageState("forbidden");
      } else {
        setFeedback(errorMessage(error));
        setPageState("error");
      }
    }
  }, [repositoryId]);

  useEffect(() => {
    void loadTeam();
  }, [loadTeam]);

  useEffect(() => {
    let cancelled = false;
    const requestGeneration = ++catalogRequestGeneration.current;
    void listRepositories()
      .then((repositories) => {
        if (cancelled || requestGeneration !== catalogRequestGeneration.current) return;
        setCatalogRepositoryName(repositories.find((repository) => repository.id === repositoryId)?.name ?? null);
      })
      .catch(() => {
        if (!cancelled && requestGeneration === catalogRequestGeneration.current) setCatalogRepositoryName(null);
      });
    return () => {
      cancelled = true;
    };
  }, [repositoryId]);

  const submitCreate = async () => {
    if (!isAdmin || capacity < 1 || capacity > 20 || !Number.isInteger(capacity)) {
      setCreateError("Choose a Worker capacity from 1 to 20.");
      return;
    }
    setCreateError(null);
    setMutationState("submitting");
    const requestGeneration = teamRequestGeneration.current;
    try {
      const snapshot = await createRepositoryTeam(repositoryId, { worker_count: capacity });
      if (requestGeneration !== teamRequestGeneration.current) return;
      setTeam(snapshot);
      setCapacity(snapshot.workers.length);
      setPageState("ready");
      setMutationState("idle");
      onToast("Repository Team created.");
    } catch (error) {
      if (requestGeneration !== teamRequestGeneration.current) return;
      setCreateError(errorMessage(error));
      setMutationState("error");
    }
  };

  const submitCapacity = async () => {
    if (!team || !isAdmin) return;
    if (!Number.isInteger(capacity) || capacity < 1 || capacity > 20) {
      setFeedback("Choose a Worker capacity from 1 to 20.");
      setMutationState("error");
      return;
    }
    if (capacity === team.workers.length) {
      setFeedback("Choose a different Worker capacity before applying it.");
      setMutationState("error");
      return;
    }

    setFeedback(null);
    setBusyLabels([]);
    setMutationState("submitting");
    const requestGeneration = teamRequestGeneration.current;
    try {
      const snapshot = await changeRepositoryTeam(repositoryId, {
        worker_count: capacity,
        roster_revision: team.roster_revision,
      });
      if (requestGeneration !== teamRequestGeneration.current) return;
      setTeam(snapshot);
      setCapacity(snapshot.workers.length);
      setMutationState("idle");
      onToast("Repository Team capacity updated.");
    } catch (error) {
      if (requestGeneration !== teamRequestGeneration.current) return;
      if (error instanceof RepositoryTeamConflictError) {
        setTeam(error.current);
        setCapacity(error.current.workers.length);
        setFeedback("The roster changed elsewhere. Review the latest Team before applying a new capacity.");
        setMutationState("stale");
        return;
      }

      const code = repositoryTeamErrorCode(error);
      if (code === "workers_busy") {
        const labels = repositoryTeamBusyLabels(error);
        setBusyLabels(labels);
        setFeedback(labels.length ? "Selected Workers are still busy." : "A selected Worker is still busy.");
        setMutationState("busy");
      } else if (code === "reconciliation_required") {
        setFeedback("The Team needs reconciliation before another capacity change.");
        setMutationState("reconciliation");
      } else {
        setFeedback(errorMessage(error));
        setMutationState("error");
      }
    }
  };

  const currentCount = team?.workers.length ?? 0;
  const removedWorkers = team && capacity < currentCount ? team.workers.slice(capacity).map((worker) => worker.display_label) : [];
  const controlsDisabled = !isAdmin || mutationState === "submitting";
  const displayRepositoryName = team?.repository_name ?? catalogRepositoryName ?? "Repository";

  return (
    <div className="w-full max-w-[860px] min-w-0">
      <div className="flex flex-wrap items-center gap-3 border-b border-line pb-3">
        <button
          className="grid size-8 place-items-center rounded-hard text-tx2 hover:bg-panel hover:text-amber-hi"
          aria-label="Back to repositories"
          title="Back to repositories"
          onClick={onBack}
        >
          <ArrowLeft size={16} strokeWidth={1.5} aria-hidden="true" />
        </button>
        <div className="min-w-0">
          <h1 className="truncate text-[16px] font-semibold text-cream">Repository Team</h1>
          <p className="truncate text-[11.5px] text-tx2">{displayRepositoryName}</p>
        </div>
      </div>

      {pageState === "loading" && (
        <div className="mt-4 grid gap-2" aria-label="Loading Team roster">
          {Array.from({ length: 3 }, (_, index) => (
            <div key={index} data-testid="team-skeleton" className="h-11 animate-pulse rounded-hard border border-line bg-panel" />
          ))}
        </div>
      )}

      {pageState === "absent" && (
        <section className="mt-5 border-t border-line pt-4">
          <div className="flex items-center gap-2">
            <UsersRound size={16} strokeWidth={1.5} className="text-amber" aria-hidden="true" />
            <h2 className="min-w-0 break-words text-[13px] font-semibold text-cream">No Team for {displayRepositoryName}</h2>
          </div>
          {isAdmin ? (
            <div className="mt-4 grid max-w-[360px] gap-3">
              <label className="grid gap-1.5 text-[12px] text-tx2" htmlFor="new-team-capacity">
                Worker capacity
                <input
                  id="new-team-capacity"
                  aria-label="Worker capacity"
                  className="w-full rounded-hard border border-line bg-ink px-3 py-1.5 font-mono text-[13px] text-tx focus:border-amber focus:outline-none"
                  type="number"
                  min={1}
                  max={20}
                  value={Number.isFinite(capacity) ? capacity : ""}
                  onChange={(event) => setCapacity(event.currentTarget.valueAsNumber)}
                  disabled={mutationState === "submitting"}
                />
              </label>
              {createError && <p className="text-[12px] text-salmon" role="alert">{createError}</p>}
              <div className="flex flex-wrap items-center gap-2">
                <button
                  className="rounded-hard border border-line px-3 py-1.5 text-[12px] text-tx2 hover:border-amber hover:text-amber-hi"
                  onClick={onBack}
                  disabled={mutationState === "submitting"}
                >
                  Cancel
                </button>
                <button
                  className="rounded-hard bg-amber px-3 py-1.5 text-[12px] font-semibold text-on-amber hover:bg-amber-hi disabled:opacity-60"
                  onClick={() => void submitCreate()}
                  disabled={mutationState === "submitting"}
                >
                  {mutationState === "submitting" ? "Creating team..." : "Create team"}
                </button>
              </div>
            </div>
          ) : (
            <p className="mt-3 text-[12px] text-tx2">Only repository administrators can create this Team.</p>
          )}
        </section>
      )}

      {pageState === "forbidden" && (
        <section className="mt-5 border-t border-salmon/60 pt-4" role="alert">
          <h2 className="text-[13px] font-semibold text-salmon">This Team cannot be opened with the current session.</h2>
          <button className="mt-3 text-[12px] text-amber hover:text-amber-hi" onClick={() => void loadTeam()}>
            Retry team request
          </button>
        </section>
      )}

      {pageState === "error" && (
        <section className="mt-5 border-t border-salmon/60 pt-4" role="alert">
          <h2 className="text-[13px] font-semibold text-salmon">The Team could not be loaded.</h2>
          {feedback && <p className="mt-1 text-[12px] text-tx2">{feedback}</p>}
          <button className="mt-3 text-[12px] text-amber hover:text-amber-hi" onClick={() => void loadTeam()}>
            Retry team request
          </button>
        </section>
      )}

      {pageState === "ready" && team && (
        <section className="mt-5">
          <div className="flex flex-wrap items-center gap-2 border-b border-line pb-2.5">
            <span className="min-w-0 break-words text-[12px] text-cream">Leader · {team.repository_name}</span>
            <span className="rounded-hard border border-line px-2 py-px text-[11px] text-tx2">{team.runtime_status}</span>
          </div>
          <MemberRow member={team.leader} label="Leader" />
          {team.workers.map((worker) => (
            <MemberRow key={worker.id} member={worker} label={worker.display_label} />
          ))}

          <div className="mt-4 border-t border-line pt-3">
            <div className="flex flex-wrap items-center gap-3">
              <label className="text-[12px] text-tx2" htmlFor="team-capacity">Worker capacity</label>
              <div className="flex items-center gap-1.5">
                <button
                  className="grid size-8 place-items-center rounded-hard border border-line text-tx2 hover:border-amber hover:text-amber-hi disabled:opacity-40"
                  aria-label="Decrease worker capacity"
                  title="Decrease worker capacity"
                  onClick={() => setCapacity((value) => Math.max(1, value - 1))}
                  disabled={controlsDisabled || capacity <= 1}
                >
                  <Minus size={14} strokeWidth={1.5} aria-hidden="true" />
                </button>
                <input
                  id="team-capacity"
                  aria-label="Worker capacity"
                  className="h-8 w-[76px] rounded-hard border border-line bg-ink px-2 text-center font-mono text-[13px] text-tx focus:border-amber focus:outline-none disabled:opacity-50"
                  type="number"
                  min={1}
                  max={20}
                  value={Number.isFinite(capacity) ? capacity : ""}
                  onChange={(event) => setCapacity(event.currentTarget.valueAsNumber)}
                  disabled={controlsDisabled}
                />
                <button
                  className="grid size-8 place-items-center rounded-hard border border-line text-tx2 hover:border-amber hover:text-amber-hi disabled:opacity-40"
                  aria-label="Increase worker capacity"
                  title="Increase worker capacity"
                  onClick={() => setCapacity((value) => Math.min(20, value + 1))}
                  disabled={controlsDisabled || capacity >= 20}
                >
                  <Plus size={14} strokeWidth={1.5} aria-hidden="true" />
                </button>
              </div>
              <button
                className="rounded-hard bg-amber px-3 py-1.5 text-[12px] font-semibold text-on-amber hover:bg-amber-hi disabled:opacity-60"
                onClick={() => void submitCapacity()}
                disabled={controlsDisabled}
              >
                {mutationState === "submitting" ? "Updating..." : "Apply capacity"}
              </button>
            </div>
            {!isAdmin && <p className="mt-2 text-[11px] text-tx3">Only repository administrators can change capacity.</p>}
            {removedWorkers.length > 0 && (
              <p className="mt-2 text-[11.5px] text-tx2">
                {removedWorkers.join(", ")} will be removed when {removedWorkers.length === 1 ? "it is" : "they are"} idle.
              </p>
            )}
            <div className="mt-2 text-[12px]" aria-live="polite">
              {mutationState === "submitting" && <span className="text-tx2">Updating...</span>}
              {feedback && mutationState !== "submitting" && (
                <div className="flex flex-wrap items-center gap-2">
                  <span className={`break-words ${mutationState === "error" ? "text-salmon" : "text-tx2"}`}>{feedback}</span>
                  {(mutationState === "stale" || mutationState === "reconciliation" || mutationState === "error") && (
                    <button className="text-amber hover:text-amber-hi" onClick={() => void loadTeam()}>
                      Retry team request
                    </button>
                  )}
                  {mutationState === "busy" && busyLabels.length > 0 && (
                    <span className="font-mono text-tx2">{busyLabels.join(", ")}</span>
                  )}
                </div>
              )}
            </div>
          </div>
        </section>
      )}
    </div>
  );
}
