import { useCallback, useEffect, useMemo, useState } from "react";
import { listRepositories, type RepositoryCard } from "../api/repositories";
import { gridSourceMode } from "../api/grid";
import { dayLabel, errText } from "../display";
import { AddRepositoryCard } from "../components/AddRepositoryCard";
import { ProvisionTeamModal } from "../components/ProvisionTeamModal";

import { ErrorPanel, LoadingLine } from "../components/StatusBlocks";

/** 仓库网格页(按仓库组织分组,2026-09-17 用户裁定)。
 *
 *  分组键 = 仓库 URL 的 owner 段(github.com/{org}/{repo});解析不出的
 *  (单仓直连等)归「独立仓库」组置底。纯展示层分组,后端零改动。
 *  默认全部展开;用户收起的组记 localStorage;有进行中扫描的组在
 *  新扫描落定时自动展开(当前扫描为同步完成,暂无进行中态可判)。 */

/** 组织 owner 段:https(s)://host/{org}/{repo} 或 git@host:{org}/{repo}。 */
function orgOf(url: string): string | null {
  const m =
    url.match(/(?:github|gitlab)\.com[/:]([^/]+)\//i) ??
    url.match(/^[^/]+\/([^/]+)\//);
  return m ? m[1] : null;
}

/** 组头的扫描入口地址:取组内首个仓库的站点 origin + /{org}。 */
function orgScanUrl(url: string, org: string): string | null {
  try {
    return `${new URL(url).origin}/${org}`;
  } catch {
    return null;
  }
}

function RepositoryCardView({ repo, onProvision }: { repo: RepositoryCard; onProvision: () => void }) {
  return (
    <div className="rounded-hard border border-line bg-panel px-4 py-3">
      <div className="flex items-baseline gap-3">
        <span className="font-mono text-[12.5px] text-tx">{repo.name}</span>
        {repo.languages.length > 0 && (
          <span className="text-[11px] text-tx2">{repo.languages.join(" / ")}</span>
        )}
        {repo.scanStatus && (
          <span
            className={`ml-auto text-[11px] ${
              repo.scanStatus === "ok" ? "text-emerald-400" : "text-amber"
            }`}
          >
            扫描{repo.scanStatus === "ok" ? "完成" : "失败"}
          </span>
        )}
      </div>
      <a
        className="mt-0.5 block font-mono text-[10.5px] text-tx3 hover:text-tx2"
        href={repo.url}
        target="_blank"
        rel="noreferrer"
      >
        {repo.url}
      </a>
      {repo.description && <p className="mt-1 text-[11.5px] text-tx2">{repo.description}</p>}
      <div className="mt-1 text-[11px] text-tx3">
        {repo.profiledAt ? `画像 ${dayLabel(repo.profiledAt)}` : "尚未画像"}
        {repo.fingerprint ? ` · 指纹 ${repo.fingerprint.slice(0, 8)}` : ""}
        {/* 2026-09-19：建团入口接回。此前 ProvisionTeamModal 无人引用（死代码），
            所以仓库页既看不到建团入口、agent_teams 也恒为 0 行。 */}
        <button
          className="ml-2 rounded-hard border border-line px-2 py-[2px] text-[11px] text-tx2 hover:border-amber hover:text-amber-hi"
          onClick={(event) => {
            event.stopPropagation();
            onProvision();
          }}
        >
          建团
        </button>
      </div>
    </div>
  );
}

const COLLAPSED_KEY = "repomesh.repos.collapsedOrgs";

function readCollapsed(): string[] {
  try {
    const raw = window.localStorage.getItem(COLLAPSED_KEY);
    return raw ? (JSON.parse(raw) as string[]) : [];
  } catch {
    return [];
  }
}

export function RepositoriesPage({
  // ConsoleShell 仍按旧签名传这三个 props——下划线占位，页面改造收尾时统一定去留
  organizationId: _organizationId,
  onOpenIssue: _onOpenIssue,
}: {
  organizationId?: string | null;
  onOpenIssue?: (issueId: string) => void;
}) {
  const [repos, setRepos] = useState<RepositoryCard[] | null>(null);
  const [provisionTarget, setProvisionTarget] = useState<RepositoryCard | null>(null);
  const [toast, setToast] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [reload, setReload] = useState(0);
  const [addOpen, setAddOpen] = useState(false);
  const [presetOrgUrl, setPresetOrgUrl] = useState<string | null>(null);
  const [collapsed, setCollapsed] = useState<Set<string>>(() => new Set(readCollapsed()));

  const refresh = useCallback(() => setReload((n) => n + 1), []);

  useEffect(() => {
    let cancelled = false;
    setRepos(null);
    setError(null);
    listRepositories()
      .then((rows) => !cancelled && setRepos(rows))
      .catch((err: unknown) => !cancelled && setError(errText(err)));
    return () => {
      cancelled = true;
    };
  }, [reload]);

  /** 分组:按 URL owner 段;解析不出归「独立仓库」置底。组内按名称排序。 */
  const groups = useMemo(() => {
    if (!repos) return [];
    const map = new Map<string, RepositoryCard[]>();
    const solo: RepositoryCard[] = [];
    repos.forEach((r) => {
      const org = orgOf(r.url);
      if (org) map.set(org, [...(map.get(org) ?? []), r]);
      else solo.push(r);
    });
    const list = [...map.entries()]
      .sort((a, b) => a[0].localeCompare(b[0]))
      .map(([org, rs]) => ({
        org,
        repos: [...rs].sort((a, b) => a.name.localeCompare(b.name)),
        host: orgScanUrl(rs[0]?.url ?? "", org),
        solo: false,
      }));
    if (solo.length) {
      list.push({ org: "独立仓库", repos: solo, host: null, solo: true });
    }
    return list;
  }, [repos]);

  const toggle = (org: string) => {
    setCollapsed((prev) => {
      const next = new Set(prev);
      if (next.has(org)) next.delete(org);
      else next.add(org);
      try {
        window.localStorage.setItem(COLLAPSED_KEY, JSON.stringify([...next]));
      } catch {
        /* 隐私模式等存储不可用:只丢记忆,不影响本次展开 */
      }
      return next;
    });
  };

  const scanThisOrg = (org: string, host: string | null) => {
    if (!host) return;
    setPresetOrgUrl(`${host}/${org}`);
    setAddOpen(true);
  };

  return (
    <div className="max-w-[860px]">
      <div className="flex items-baseline gap-3 border-b border-line pb-3">
        <h1 className="text-[16px] font-semibold text-cream">仓库</h1>
        {repos && <span className="text-[11.5px] text-tx2">{repos.length} 个</span>}
        <button
          className="ml-auto rounded-hard border border-line px-2.5 py-[3px] text-[11.5px] text-tx2 hover:border-amber hover:text-amber-hi"
          onClick={() => setAddOpen((v) => !v)}
        >
          + 添加仓库
        </button>
      </div>

      {/* 卡片在收起时也保持挂载:轮询活在它内部,收起卡片不该中断一次在跑的扫描 */}
      <AddRepositoryCard
        open={addOpen}
        mode={gridSourceMode()}
        presetOrgUrl={addOpen ? presetOrgUrl : null}
        onClose={() => {
          setAddOpen(false);
          setPresetOrgUrl(null);
        }}
        onRestored={() => setAddOpen(true)}
        onScanSettled={refresh}
      />

      {error ? (
        <ErrorPanel title="仓库目录加载失败" message={error} onRetry={() => setReload((n) => n + 1)} />
      ) : repos === null ? (
        <LoadingLine />
      ) : repos.length === 0 ? (
        <div className="py-8 text-center text-[12.5px] text-tx3">
          目录里还没有仓库——先添加并完成扫描
        </div>
      ) : (
        <div className="mt-4 space-y-3">
          {groups.map(({ org, repos: rs, host, solo }) => {
            const isCollapsed = collapsed.has(org);
            return (
              <section key={org}>
                <button
                  className="flex w-full items-center gap-2 rounded-hard px-1 py-1.5 text-left hover:text-tx"
                  onClick={() => toggle(org)}
                >
                  <span className="text-[10px] text-tx3">{isCollapsed ? "▶" : "▼"}</span>
                  <span className="text-[13px] font-semibold text-cream">{org}</span>
                  <span className="text-[11px] text-tx3">
                    {rs.length} 个仓库
                    {rs.some((r) => r.scanStatus === "failed") && " · 有失败"}
                  </span>
                  <span className="flex-1" />
                  {!solo && host && (
                    <span
                      className="text-[11px] text-tx3 underline-offset-2 hover:text-amber hover:underline"
                      onClick={(e) => {
                        e.stopPropagation();
                        scanThisOrg(org, host);
                      }}
                    >
                      扫描此组织
                    </span>
                  )}
                </button>
                {!isCollapsed && (
                  <div className="mt-1.5 grid gap-2 pl-4">
                    {rs.map((repo) => (
                      <RepositoryCardView key={repo.id} repo={repo} onProvision={() => setProvisionTarget(repo)} />
                    ))}
                  </div>
                )}
              </section>
            );
          })}
        </div>
      )}

      <p className="pt-4 text-[11px] text-tx3">
        圈定提交会同时落一条「范围确认」决策单(历史决策页可见)。
        
      </p>

      {toast && (
        <div className="fixed bottom-6 left-1/2 z-30 -translate-x-1/2 rounded-hard border border-line bg-panel px-4 py-2 text-[12.5px] text-tx shadow-float">
          {toast}
        </div>
      )}

      {provisionTarget && (
        <ProvisionTeamModal
          open
          repositoryId={provisionTarget.id}
          repositoryName={provisionTarget.name}
          organizationId=""
          onClose={() => setProvisionTarget(null)}
          onProvisioned={() => {
            setToast("已提交建团，正在刷新仓库目录…");
            setReload((n) => n + 1);
          }}
          onToast={setToast}
        />
      )}
    </div>
  );
}
