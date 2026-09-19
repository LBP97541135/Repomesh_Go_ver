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
  return error instanceof Error ? error.message : "团队请求未能完成。";
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
      onToast("团队已组建。");
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
      setFeedback("Worker 数量和当前编制相同，无需应用。");
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
      onToast("团队编制已更新。");
    } catch (error) {
      if (requestGeneration !== teamRequestGeneration.current) return;
      if (error instanceof RepositoryTeamConflictError) {
        setTeam(error.current);
        setCapacity(error.current.workers.length);
        setFeedback("花名册已被他处修改，请核对最新编制后再应用。");
        setMutationState("stale");
        return;
      }

      const code = repositoryTeamErrorCode(error);
      if (code === "workers_busy") {
        const labels = repositoryTeamBusyLabels(error);
        setBusyLabels(labels);
        setFeedback("选中的 Worker 仍有任务在执行，缩容被拒绝。");
        setMutationState("busy");
      } else if (code === "reconciliation_required") {
        setFeedback("团队需要先人工核对一次，才能再次调整编制。");
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
  const displayRepositoryName = team?.repository_name ?? catalogRepositoryName ?? "该仓库";

  return (
    <div className="w-full max-w-[860px] min-w-0">
      <div className="flex flex-wrap items-center gap-3 border-b border-line pb-3">
        <button
          className="grid size-8 place-items-center rounded-hard text-tx2 hover:bg-panel hover:text-amber-hi"
          aria-label="返回仓库列表"
          title="返回仓库列表"
          onClick={onBack}
        >
          <ArrowLeft size={16} strokeWidth={1.5} aria-hidden="true" />
        </button>
        <div className="min-w-0">
          <h1 className="truncate text-[16px] font-semibold text-cream">仓库团队</h1>
          <p className="truncate text-[11.5px] text-tx2">{displayRepositoryName}</p>
        </div>
      </div>

      {pageState === "loading" && (
        <div className="mt-4 grid gap-2" aria-label="团队花名册加载中">
          {Array.from({ length: 3 }, (_, index) => (
            <div key={index} data-testid="team-skeleton" className="h-11 animate-pulse rounded-hard border border-line bg-panel" />
          ))}
        </div>
      )}

      {pageState === "absent" && (
        <section className="mt-5 border-t border-line pt-4">
          <div className="flex items-center gap-2">
            <UsersRound size={16} strokeWidth={1.5} className="text-amber" aria-hidden="true" />
            <h2 className="min-w-0 break-words text-[13px] font-semibold text-cream">{displayRepositoryName} 还没有团队</h2>
          </div>
          {isAdmin ? (
            <div className="mt-4 grid max-w-[360px] gap-3">
              <label className="grid gap-1.5 text-[12px] text-tx2" htmlFor="new-team-capacity">
                Worker 数量
                <input
                  id="new-team-capacity"
                  aria-label="Worker 数量"
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
                  取消
                </button>
                <button
                  className="rounded-hard bg-amber px-3 py-1.5 text-[12px] font-semibold text-on-amber hover:bg-amber-hi disabled:opacity-60"
                  onClick={() => void submitCreate()}
                  disabled={mutationState === "submitting"}
                >
                  {mutationState === "submitting" ? "正在组建…" : "组建团队"}
                </button>
              </div>
            </div>
          ) : (
            <p className="mt-3 text-[12px] text-tx2">只有仓库管理员能组建团队。</p>
          )}
        </section>
      )}

      {pageState === "forbidden" && (
        <section className="mt-5 border-t border-salmon/60 pt-4" role="alert">
          <h2 className="text-[13px] font-semibold text-salmon">当前会话无权打开这个团队。</h2>
          <button className="mt-3 text-[12px] text-amber hover:text-amber-hi" onClick={() => void loadTeam()}>
            重试
          </button>
        </section>
      )}

      {pageState === "error" && (
        <section className="mt-5 border-t border-salmon/60 pt-4" role="alert">
          <h2 className="text-[13px] font-semibold text-salmon">团队读取失败。</h2>
          {feedback && <p className="mt-1 text-[12px] text-tx2">{feedback}</p>}
          <button className="mt-3 text-[12px] text-amber hover:text-amber-hi" onClick={() => void loadTeam()}>
            Retry team request
          </button>
        </section>
      )}

      {pageState === "ready" && team && (
        <section className="mt-5">
          <div className="flex flex-wrap items-center gap-2 border-b border-line pb-2.5">
            <UsersRound size={15} strokeWidth={1.5} className="flex-none text-amber" aria-hidden="true" />
            <span className="min-w-0 break-words text-[13px] font-semibold text-cream">{team.repository_name}</span>
            <span className="ml-auto flex-none rounded-hard border border-line px-2 py-px text-[11px] text-tx2">
              运行状态：{team.runtime_status}
            </span>
          </div>

          {/* 编制概览（用户裁定）：Leader 是谁、当前几名 Worker，一眼可见 */}
          <div className="mt-3 grid gap-1 text-[12px] text-tx2">
            <p>
              Leader：<span className="font-medium text-cream">Leader · {team.repository_name}</span>
            </p>
            <p>
              当前 Worker：<span className="font-mono font-medium text-cream">{team.workers.length}</span> 名
              <span className="text-tx3">（可设 1–20）</span>
            </p>
          </div>
          <MemberRow member={team.leader} label="Leader" />
          {team.workers.map((worker) => (
            <MemberRow key={worker.id} member={worker} label={worker.display_label} />
          ))}

          <div className="mt-4 border-t border-line pt-3">
            <div className="flex flex-wrap items-center gap-3">
              <label className="text-[12px] text-tx2" htmlFor="team-capacity">Worker 数量</label>
              <div className="flex items-center gap-1.5">
                <button
                  className="grid size-8 place-items-center rounded-hard border border-line text-tx2 hover:border-amber hover:text-amber-hi disabled:opacity-40"
                  aria-label="减少 Worker 数量"
                  title="减少 Worker 数量"
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
                  aria-label="增加 Worker 数量"
                  title="增加 Worker 数量"
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
                {mutationState === "submitting" ? "正在更新…" : "应用编制"}
              </button>
            </div>
            {!isAdmin && <p className="mt-2 text-[11px] text-tx3">只有仓库管理员能调整编制。</p>}
            {removedWorkers.length > 0 && (
              <p className="mt-2 text-[11.5px] text-tx2">
                {removedWorkers.join("、")} 空闲后将被移除。
              </p>
            )}
            <div className="mt-2 text-[12px]" aria-live="polite">
              {mutationState === "submitting" && <span className="text-tx2">正在更新…</span>}
              {feedback && mutationState !== "submitting" && (
                <div className="flex flex-wrap items-center gap-2">
                  <span className={`break-words ${mutationState === "error" ? "text-salmon" : "text-tx2"}`}>{feedback}</span>
                  {(mutationState === "stale" || mutationState === "reconciliation" || mutationState === "error") && (
                    <button className="text-amber hover:text-amber-hi" onClick={() => void loadTeam()}>
                      重试
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
