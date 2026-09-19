import { useEffect, useState } from "react";
import { approveSpec, createSpec, getCurrentSpec, type SpecView } from "../api/pipeline";
import { resolveProjectId } from "../api/issues";
import { allProjectRepositories } from "../api/projects";
import { errText } from "../display";

/** 规格（spec）页 —— 把后端早已就绪、前端却**没有任何入口**的三个端点接上。
 *
 *  2026-09-20 审计发现：`createSpec` / `getCurrentSpec` / `approveSpec` 在
 *  `frontend/src/api/pipeline.ts` 里**只有定义、没有任何调用点** —— 等于死代码，
 *  界面上根本没有规格入口；而后端 `internal/spec` 的「建 / 批 / 查」三条路由
 *  一直是通的（`POST /projects/{id}/specs`、`GET /specs/current`、
 *  `POST /specs/{id}/approve`）。
 *
 *  本页按仓库看**当前生效规格**，可起草新版本、可审批（审批后该版本成为该仓库
 *  的当前规格）。写面只调真端点，失败如实上屏；没有规格就说没有，不摆假的。 */

export function SpecPage({ onToast }: { onToast: (msg: string) => void }) {
  const [projectId, setProjectId] = useState<string | null>(null);
  const [repos, setRepos] = useState<Array<{ id: string; name: string }> | null>(null);
  const [repo, setRepo] = useState("");
  const [spec, setSpec] = useState<SpecView | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  // 刚起草、尚未审批的版本。读面只有"当前生效规格"（approved），拿不到草稿列表，
  // 所以草稿就用手上这次 Create 的返回体显示 —— 不编一条"草稿列表"出来。
  const [draft, setDraft] = useState<SpecView | null>(null);
  const [title, setTitle] = useState("");
  const [content, setContent] = useState("");
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    resolveProjectId()
      .then((pid) => {
        setProjectId(pid);
        if (!pid) return;
        return allProjectRepositories(pid)
          .then((page) => {
            const items = page.items.map((r) => ({ id: r.id, name: r.displayName }));
            setRepos(items);
            if (items.length > 0) setRepo(items[0].name);
          })
          .catch(() => setRepos([]));
      })
      .catch(() => setProjectId(null));
  }, []);

  useEffect(() => {
    if (!projectId || !repo) return;
    let cancelled = false;
    setLoading(true);
    setError(null);
    setDraft(null);
    getCurrentSpec(projectId, repo)
      .then((view) => {
        // 2026-09-20 实测：后端在没有生效规格时**返回哨兵** SpecView{State:"none",
        // Version:0, Title:"none"}，而不是 404。那是"还没有"，不是一份叫 none 的
        // 规格 —— 直接照渲染会显示成「当前生效规格 v0 none」。如实按"没有"处理。
        if (!cancelled) setSpec(view && view.state !== "none" ? view : null);
      })
      .catch(() => {
        // 404 = 这个仓库还没有生效规格。那不是错误，如实显示"还没有"。
        if (!cancelled) setSpec(null);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [projectId, repo]);

  const submitDraft = () => {
    if (!projectId || !repo || !content.trim() || busy) return;
    setBusy(true);
    setError(null);
    createSpec(projectId, {
      repository: repo,
      title: title.trim() || "未命名规格",
      content,
      ...(spec ? { supersedes: spec.id } : {}),
    })
      .then((view) => {
        setDraft(view);
        setTitle("");
        setContent("");
        onToast("草稿已建立；审批后成为该仓库的当前规格");
      })
      .catch((err) => setError(errText(err)))
      .finally(() => setBusy(false));
  };

  const approveDraft = () => {
    if (!projectId || !draft || busy) return;
    setBusy(true);
    setError(null);
    approveSpec(projectId, draft.id)
      .then((view) => {
        setSpec(view);
        setDraft(null);
        onToast("规格已批准，现在是该仓库的当前规格");
      })
      .catch((err) => setError(errText(err)))
      .finally(() => setBusy(false));
  };

  return (
    <div className="px-6 py-5">
      <div className="mb-4 flex items-baseline gap-3">
        <h1 className="text-[15px] font-semibold text-cream">规格</h1>
        <span className="text-[11.5px] text-tx2">
          按仓库维护规格；审批后成为该仓库的当前生效规格，规划与执行都以它为准。
        </span>
      </div>

      {projectId === null && (
        <p className="text-[12.5px] text-tx3">没有可用的项目 —— 请先在「项目」页建立或选择项目。</p>
      )}

      {projectId !== null && (
        <div className="grid grid-cols-[minmax(220px,280px)_1fr] gap-5">
          {/* 左：仓库 */}
          <div className="rounded-hard border border-line bg-panel">
            <p className="border-b border-line px-3 py-2 text-[11.5px] text-tx2">仓库</p>
            {repos === null && <p className="px-3 py-3 text-[12px] text-tx3">正在读取…</p>}
            {repos !== null && repos.length === 0 && (
              <p className="px-3 py-3 text-[12px] text-tx3">这个项目还没有接入仓库。</p>
            )}
            {(repos ?? []).map((item) => {
              const active = repo === item.name;
              return (
                <button
                  key={item.id}
                  className={`block w-full border-b border-line px-3 py-2 text-left last:border-b-0 ${
                    active ? "bg-amber/10" : "hover:bg-well"
                  }`}
                  onClick={() => setRepo(item.name)}
                >
                  <span className="block truncate font-mono text-[12px] text-tx">{item.name}</span>
                </button>
              );
            })}
          </div>

          {/* 右：当前规格 + 起草 */}
          <div className="min-w-0 space-y-3">
            {error !== null && (
              <div className="rounded-hard border border-salmon bg-salmon-well px-3 py-2 text-[12px] text-salmon-hi">
                {error}
              </div>
            )}

            <div className="rounded-hard border border-line bg-panel px-4 py-3">
              <div className="flex items-baseline gap-2">
                <span className="text-[12.5px] text-cream">当前生效规格</span>
                {loading && <span className="text-[11px] text-tx3">读取中…</span>}
              </div>
              {!loading && spec === null && (
                <p className="mt-1.5 text-[12px] text-tx2">
                  这个仓库还没有生效规格。可以起草一份，审批后即为当前规格。
                </p>
              )}
              {spec !== null && (
                <div className="mt-1.5">
                  <div className="flex items-center gap-2">
                    <span className="font-mono text-[12.5px] text-tx">v{spec.version}</span>
                    <span className="text-[12.5px] text-cream">{spec.title}</span>
                    <span className="rounded-full bg-amber/20 px-2 py-[1px] text-[10.5px] text-amber-hi">
                      {spec.state === "approved" ? "已批准" : spec.state}
                    </span>
                  </div>
                  <pre className="mt-2 whitespace-pre-wrap break-words font-mono text-[11.5px] leading-[1.7] text-tx2">
                    {spec.content}
                  </pre>
                </div>
              )}
            </div>

            {draft !== null && (
              <div className="rounded-hard border border-amber bg-amber/10 px-4 py-3">
                <div className="flex items-center gap-2">
                  <span className="text-[12.5px] text-cream">刚起草的版本（待审批）</span>
                  <button
                    className="ml-auto rounded-hard border border-amber px-2.5 py-1 text-[11.5px] text-amber-hi hover:bg-amber/20 disabled:opacity-50"
                    disabled={busy}
                    onClick={approveDraft}
                  >
                    {busy ? "审批中…" : "批准并生效"}
                  </button>
                </div>
                <div className="mt-1.5 font-mono text-[11.5px] text-tx2">{draft.title}</div>
                <pre className="mt-1 whitespace-pre-wrap break-words font-mono text-[11.5px] leading-[1.7] text-tx2">
                  {draft.content}
                </pre>
              </div>
            )}

            <div className="rounded-hard border border-line bg-panel px-4 py-3">
              <p className="text-[12.5px] text-cream">起草新版本</p>
              <input
                className="mt-2 w-full rounded-hard border border-line bg-ink px-2.5 py-1.5 text-[12px] text-tx outline-none focus:border-amber"
                placeholder="标题（可留空）"
                value={title}
                onChange={(e) => setTitle(e.target.value)}
              />
              <textarea
                className="mt-2 h-32 w-full resize-y rounded-hard border border-line bg-ink px-2.5 py-1.5 font-mono text-[12px] leading-[1.7] text-tx outline-none focus:border-amber"
                placeholder="规格正文：这个仓库要遵守什么（接口约定、数据契约、边界条件…）"
                value={content}
                onChange={(e) => setContent(e.target.value)}
              />
              <div className="mt-2 flex items-center gap-3">
                <button
                  className="rounded-hard border border-amber bg-amber px-3 py-1.5 text-[12px] font-medium text-on-amber hover:bg-amber-hi disabled:opacity-50"
                  disabled={busy || !content.trim() || repo === ""}
                  onClick={submitDraft}
                >
                  {busy ? "提交中…" : "起草"}
                </button>
                <span className="text-[11px] text-tx3">
                  起草只是草稿；点「批准并生效」后才成为 {repo || "该仓库"} 的当前规格。
                </span>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
