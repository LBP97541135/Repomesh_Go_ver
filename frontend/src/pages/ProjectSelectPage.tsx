import { useCallback, useEffect, useState } from "react";
import { createProject, listProjects, type ProjectListItem } from "../api/projects";
import { listRepositories } from "../api/repositories";
import type { RepositoryCard } from "../api/contract";
import { readActiveProject, setActiveProject } from "../api/activeProject";
import { errText } from "../display";

/** 项目页 —— 主界面流程的第 2 步（2026-09-19 用户裁定）。
 *
 *  `https://crazykitties.cn/` 作为主界面的顺序固定为：
 *  先登录 → 再建立或选择项目 → 最后进 issues 工作台。
 *  issue 必须挂在项目上，项目自带团队，issue 由该团队处理——所以这里必须先
 *  选定项目，选中项写进 activeProject，由 resolveProjectId() 供建 issue 使用。
 *
 *  只做两件事：列出可进入的项目、建立新项目。团队编排在「仓库」页按仓库建，
 *  本页只如实显示每个项目的仓库数，不代替团队页做决定。 */

export function ProjectSelectPage({ onEnterIssues }: { onEnterIssues: () => void }) {
  const [projects, setProjects] = useState<ProjectListItem[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [active, setActive] = useState<string | null>(() => readActiveProject());

  const [creating, setCreating] = useState(false);
  const [name, setName] = useState("");
  const [purpose, setPurpose] = useState("");
  const [repositories, setRepositories] = useState<RepositoryCard[] | null>(null);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [createError, setCreateError] = useState<string | null>(null);

  const reload = useCallback(() => {
    setError(null);
    listProjects({ limit: 100 })
      .then((page) => setProjects(page.items))
      .catch((err: unknown) => {
        setProjects([]);
        setError(errText(err));
      });
  }, []);

  useEffect(reload, [reload]);

  useEffect(() => {
    if (!creating || repositories !== null) return;
    listRepositories()
      .then(setRepositories)
      .catch(() => setRepositories([]));
  }, [creating, repositories]);

  const enter = (projectId: string) => {
    setActiveProject(projectId);
    setActive(projectId);
    onEnterIssues();
  };

  const submit = async () => {
    setCreateError(null);
    const trimmed = name.trim();
    if (trimmed === "") {
      setCreateError("请填写项目名称。");
      return;
    }
    if (selected.size === 0) {
      setCreateError("请至少选择一个仓库——issue 与团队都挂在项目上，项目需要仓库才能建团队。");
      return;
    }
    setBusy(true);
    try {
      const receipt = await createProject(
        {
          name: trimmed,
          purpose: purpose.trim() || trimmed,
          repositoryIds: [...selected],
        },
        crypto.randomUUID(),
      );
      setCreating(false);
      setName("");
      setPurpose("");
      setSelected(new Set());
      reload();
      enter(receipt.projectId);
    } catch (err) {
      setCreateError(errText(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="mx-auto max-w-[880px] px-8 py-10">
      <div className="mb-7 flex items-start justify-between gap-6">
        <div>
          <p className="eyebrow">第 2 步</p>
          <h1 className="mt-1 text-[18px] font-semibold text-cream">选择项目</h1>
          <p className="mt-1.5 text-[12.5px] text-tx2">
            issue 挂在项目上，项目自带团队并由团队处理需求。先选定项目，再进 issues 工作台。
          </p>
        </div>
        <button
          className="flex-none rounded-hard border border-line-strong px-3 py-[7px] text-[12.5px] text-cream hover:bg-amber/10"
          onClick={() => setCreating((v) => !v)}
        >
          {creating ? "取消" : "+ 新建项目"}
        </button>
      </div>

      {creating && (
        <section className="mb-7 rounded-hard border border-line bg-panel px-5 py-5">
          <h2 className="text-[14px] font-semibold text-cream">新建项目</h2>
          <label className="mt-4 block text-[12.5px] text-tx2">
            项目名称
            <input
              className="mt-1 w-full rounded-hard border border-line bg-well px-2.5 py-[7px] text-[13px] text-tx"
              value={name}
              onChange={(event) => setName(event.target.value)}
              placeholder="例如：电商结账链路"
            />
          </label>
          <label className="mt-3 block text-[12.5px] text-tx2">
            项目用途
            <textarea
              className="mt-1 w-full rounded-hard border border-line bg-well px-2.5 py-[7px] text-[13px] text-tx"
              rows={2}
              value={purpose}
              onChange={(event) => setPurpose(event.target.value)}
              placeholder="这个项目负责什么（可留空，默认取项目名称）"
            />
          </label>

          <div className="mt-4">
            <p className="text-[12.5px] text-tx2">
              选择仓库 <span className="text-tx3">（已选 {selected.size} 个；团队按仓库建立）</span>
            </p>
            {repositories === null && <p className="mt-2 text-[12px] text-tx3">正在读取仓库…</p>}
            {repositories !== null && repositories.length === 0 && (
              <p className="mt-2 text-[12px] text-salmon-hi">
                没有可用仓库——请先到「仓库」页添加仓库并完成授权。
              </p>
            )}
            <div className="mt-2 max-h-[220px] space-y-1 overflow-y-auto">
              {(repositories ?? []).map((repo) => {
                const id = String((repo as { id?: unknown }).id ?? "");
                const label =
                  String((repo as { fullName?: unknown }).fullName ?? "") ||
                  String((repo as { name?: unknown }).name ?? "") ||
                  id;
                const checked = selected.has(id);
                return (
                  <label
                    key={id}
                    className="flex cursor-pointer items-center gap-2.5 rounded-hard border border-line px-2.5 py-[6px] text-[12.5px] text-tx hover:bg-well"
                  >
                    <input
                      type="checkbox"
                      checked={checked}
                      onChange={() =>
                        setSelected((prev) => {
                          const next = new Set(prev);
                          if (next.has(id)) next.delete(id);
                          else next.add(id);
                          return next;
                        })
                      }
                    />
                    <span className="truncate">{label}</span>
                  </label>
                );
              })}
            </div>
          </div>

          {createError && (
            <p className="mt-3 rounded-hard border border-salmon-deep bg-salmon-well px-3 py-2 text-[12px] text-salmon-hi">
              {createError}
            </p>
          )}
          <button
            className="login-cta mt-4 rounded-hard px-4 py-[8px] text-[12.5px] font-extrabold tracking-[0.04em] disabled:opacity-60"
            onClick={() => void submit()}
            disabled={busy}
          >
            {busy ? "正在保存…" : "保存并进入工作台"}
          </button>
        </section>
      )}

      {error && (
        <p className="mb-5 rounded-hard border border-salmon-deep bg-salmon-well px-3 py-2 text-[12px] text-salmon-hi">
          项目列表加载失败：{error}
        </p>
      )}

      {projects === null && <p className="text-[12.5px] text-tx3">正在读取项目…</p>}
      {projects !== null && projects.length === 0 && !creating && (
        <div className="rounded-hard border border-line bg-panel px-5 py-6 text-[12.5px] text-tx2">
          还没有项目。点右上角「新建项目」，选好仓库后即可进入 issues 工作台。
        </div>
      )}

      <div className="space-y-2">
        {(projects ?? []).map((project) => (
          <div
            key={project.id}
            className="flex items-center justify-between gap-4 rounded-hard border border-line bg-panel px-4 py-3"
          >
            <div className="min-w-0">
              <div className="flex items-center gap-2">
                <span className="truncate text-[13.5px] font-medium text-cream">{project.name}</span>
                {active === project.id && (
                  <span className="flex-none rounded-full bg-amber/20 px-2 py-[1px] text-[10.5px] text-amber-hi">
                    当前项目
                  </span>
                )}
              </div>
              <div className="mt-0.5 truncate font-mono text-[10.5px] text-tx3">{project.id}</div>
            </div>
            <button
              className="flex-none rounded-hard border border-line-strong px-3 py-[6px] text-[12.5px] text-cream hover:bg-amber/10"
              onClick={() => enter(project.id)}
            >
              进入 issues 工作台
            </button>
          </div>
        ))}
      </div>
    </div>
  );
}
