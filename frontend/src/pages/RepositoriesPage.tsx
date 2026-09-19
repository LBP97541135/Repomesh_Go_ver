import { useCallback, useEffect, useMemo, useState } from "react";
import { UsersRound } from "lucide-react";
import { listRepositories, type RepositoryCard } from "../api/repositories";
import { allProjectRepositories } from "../api/projects";
import { gridSourceMode } from "../api/grid";
import { dayLabel, errText } from "../display";
import { AddRepositoryCard } from "../components/AddRepositoryCard";

import { ErrorPanel, LoadingLine } from "../components/StatusBlocks";

/** 仓库网格页(按仓库组织分组,2026-09-17 用户裁定;2026-09-20 从主线并入)。
 *
 *  分组键 = 仓库 URL 的 owner 段(github.com/{org}/{repo});解析不出的
 *  (单仓直连等)归「独立仓库」组置底。纯展示层分组,后端零改动。
 *  默认全部展开;用户收起的组记 localStorage。
 *
 *  本线保留项:**App 工作授权状态**(「就绪 / 不足」)——项目成员读面
 *  (`allProjectRepositories`) 按仓库 id 并入卡片;目录读面不返回它。 */

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

function RepositoryCardView({
  repo,
  appStatus,
  onManageTeam,
}: {
  repo: RepositoryCard;
  appStatus?: string;
  onManageTeam?: (repositoryId: string) => void;
}) {
  return (
    <div className="rounded-hard border border-line bg-panel px-4 py-3">
      <div className="flex items-baseline gap-3">
        <span className="font-mono text-[12.5px] text-tx">{repo.name}</span>
        {repo.languages.length > 0 && (
          <span className="text-[11px] text-tx2">{repo.languages.join(" / ")}</span>
        )}
        {appStatus && (
          <span className={`text-[11px] ${appStatus === "allowed" ? "text-emerald-400" : "text-amber"}`}>
            {appStatus === "allowed" ? "App 工作授权就绪" : "App 工作授权不足"}
          </span>
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
      <div className="mt-1 flex items-center gap-3 text-[11px] text-tx3">
        <span>
          {repo.profiledAt ? `画像 ${dayLabel(repo.profiledAt)}` : "尚未画像"}
          {repo.fingerprint ? ` · 指纹 ${repo.fingerprint.slice(0, 8)}` : ""}
        </span>
        {/* 团队管理入口（管理员可见）：仓库作用域的长生命周期编制，不在全局入口 */}
        {onManageTeam && (
          <button
            type="button"
            className="ml-auto flex flex-none items-center gap-1.5 rounded-hard border border-line px-2 py-[2px] text-[11px] text-tx2 hover:border-amber hover:text-amber-hi"
            onClick={() => onManageTeam(repo.id)}
            title="管理该仓库的团队（Leader 与 Worker 编制）"
          >
            <UsersRound size={12} strokeWidth={1.6} aria-hidden />
            团队
          </button>
        )}
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
  projectId,
  projectName,
  onNewIssue,
  isAdmin = false,
  onManageTeam,
}: {
  projectId: string;
  projectName: string;
  onNewIssue: () => void;
  /** 管理员才能管理仓库团队（服务端仍是权威：写路由每次都查 is_admin）。 */
  isAdmin?: boolean;
  onManageTeam?: (repositoryId: string) => void;
}) {
  void projectName;
  void onNewIssue;
  const [repos, setRepos] = useState<RepositoryCard[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [reload, setReload] = useState(0);
  const [addOpen, setAddOpen] = useState(false);
  const [presetOrgUrl, setPresetOrgUrl] = useState<string | null>(null);
  const [collapsed, setCollapsed] = useState<Set<string>>(() => new Set(readCollapsed()));
  /** 仓库 id → App 工作授权状态（项目成员读面；目录读面不返回它）。 */
  const [appStatusById, setAppStatusById] = useState<Record<string, string>>({});

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

  useEffect(() => {
    let cancelled = false;
    allProjectRepositories(projectId)
      .then((page) => {
        if (cancelled) return;
        const map: Record<string, string> = {};
        for (const r of page.items) {
          if (r.appCapability?.status) map[r.id] = r.appCapability.status;
        }
        setAppStatusById(map);
      })
      .catch(() => {
        /* 授权状态取不到就不显示这一项——不拿失败当「不足」 */
      });
    return () => {
      cancelled = true;
    };
  }, [projectId, reload]);

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
                      <RepositoryCardView
                        key={repo.id}
                        repo={repo}
                        appStatus={appStatusById[repo.id]}
                        onManageTeam={isAdmin ? onManageTeam : undefined}
                      />
                    ))}
                  </div>
                )}
              </section>
            );
          })}
        </div>
      )}
    </div>
  );
}
