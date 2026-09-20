import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { UsersRound } from "lucide-react";
import { listRepositories, type RepositoryCard } from "../api/repositories";
import { allProjectRepositories, updateProject } from "../api/projects";
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
 *  (`allProjectRepositories`) 按仓库 id 并入卡片;目录读面不返回它。
 *
 *  2026-09-20 补上「接入本项目」(用户实测:新建项目后建 issue 报
 *  NO_AVAILABLE_REPOSITORIES)。**目录 ≠ 项目**:「+ 添加仓库」只把仓登记进组织
 *  目录并扫描,项目关联是 `project_repositories` 另一张表;而挂仓此前只有建项目
 *  那一刻会做(`createProject` 带 repositoryIds,而建项页传的是空数组),于是
 *  新建项目之后再无入口——后端 `repositoryIdsToAdd` 一直能加,前端只是没人调。
 *  现在两条路都补上:
 *   ① 卡片上给未接入的仓一个「接入本项目」动作(解已有仓的封);
 *   ② 添加/扫描完成后,把这次**新登记**的仓自动接入(用户要的"添加仓库就该挂上")。
 *  接入只要求发起人的参与权(`CheckProjectObservation` 不看 App)——App 是
 *  建 issue 时"这个仓可不可选"那道闸,两件事分开。 */

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
  inProject,
  attachBusy,
  onAttach,
  onManageTeam,
}: {
  repo: RepositoryCard;
  appStatus?: string;
  /** 这个仓在不在**当前项目**里（`project_repositories`）。undefined = 读面还没到。 */
  inProject?: boolean;
  attachBusy?: boolean;
  onAttach?: (repositoryId: string) => void;
  onManageTeam?: (repositoryId: string) => void;
}) {
  return (
    <div className="rounded-hard border border-line bg-panel px-4 py-3">
      <div className="flex items-baseline gap-3">
        <span className="font-mono text-[12.5px] text-tx">{repo.name}</span>
        {repo.languages.length > 0 && (
          <span className="text-[11px] text-tx2">{repo.languages.join(" / ")}</span>
        )}
        {/* 接入状态是卡片上**最要紧**的一条：没接入的仓，建 issue 时一个都选不出来，
            界面必须当场说清，而不是等人撞在 NO_AVAILABLE_REPOSITORIES 上再回来找。 */}
        {inProject === true && <span className="text-[11px] text-olive">已接入本项目</span>}
        {inProject === false && <span className="text-[11px] text-amber">未接入本项目</span>}
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
        {inProject === false && onAttach && (
          <button
            type="button"
            className="ml-auto flex flex-none items-center gap-1.5 rounded-hard border border-line px-2 py-[2px] text-[11px] text-tx2 hover:border-amber hover:text-amber-hi disabled:opacity-50"
            onClick={() => onAttach(repo.id)}
            disabled={attachBusy}
            title="把这个仓库接入当前项目——不接入的话，建 issue 时它不出现在可选仓库里"
          >
            {attachBusy ? "接入中…" : "接入本项目"}
          </button>
        )}
        {/* 团队管理入口（管理员可见）：仓库作用域的长生命周期编制，不在全局入口 */}
        {onManageTeam && (
          <button
            type="button"
            className={`${inProject === false && onAttach ? "" : "ml-auto"} flex flex-none items-center gap-1.5 rounded-hard border border-line px-2 py-[2px] text-[11px] text-tx2 hover:border-amber hover:text-amber-hi`}
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
  /** 本项目已接入的仓库 id。null = 还没读到——此时不显示接入状态，也不拿它做判断。 */
  const [attachedIds, setAttachedIds] = useState<Set<string> | null>(null);
  const [attachBusy, setAttachBusy] = useState<string | null>(null);
  const [notice, setNotice] = useState<{ kind: "ok" | "err"; text: string } | null>(null);
  /** 建项目更新要的 `expectedProjectRevision`：与 attachedIds 同一次读面拿回。 */
  const projectRevisionRef = useRef<string | null>(null);
  /** 上一次看到的目录 id 集合——用来认出「这次扫描新登记了哪些仓」。 */
  const knownIdsRef = useRef<Set<string> | null>(null);
  /** 已接入 / 正在接入的 id（ref，因为下面的自动接入跑在回调里，拿不到最新 state）。 */
  const attachedIdsRef = useRef<Set<string>>(new Set());
  const inFlightRef = useRef<Set<string>>(new Set());

  const refresh = useCallback(() => setReload((n) => n + 1), []);

  /** 把仓库接入本项目。**只加不移**——后端 `repositoryIdsToAdd` 就只支持加。 */
  const attachRepos = useCallback(
    async (ids: string[], source: "manual" | "scan") => {
      const revision = projectRevisionRef.current;
      const fresh = ids.filter((id) => !attachedIdsRef.current.has(id) && !inFlightRef.current.has(id));
      if (fresh.length === 0) return;
      if (!revision) {
        setNotice({ kind: "err", text: "项目版本还没取到——等列表刷出来再试一次" });
        return;
      }
      fresh.forEach((id) => inFlightRef.current.add(id));
      if (source === "manual") setAttachBusy(fresh[0]);
      setNotice(null);
      try {
        await updateProject(
          projectId,
          { expectedProjectRevision: revision, repositoryIdsToAdd: fresh },
          crypto.randomUUID(),
        );
        setNotice({
          kind: "ok",
          text: source === "scan" ? `已自动接入本次新登记的 ${fresh.length} 个仓库` : "已接入本项目",
        });
        setReload((n) => n + 1);
      } catch (err) {
        // 失败就撤掉在途标记，好让人再点一次；已接入集合由列表刷新来纠正
        setNotice({ kind: "err", text: `接入失败：${errText(err)}` });
      } finally {
        fresh.forEach((id) => inFlightRef.current.delete(id));
        if (source === "manual") setAttachBusy(null);
      }
    },
    [projectId],
  );

  useEffect(() => {
    let cancelled = false;
    setRepos(null);
    setError(null);
    listRepositories()
      .then((rows) => {
        if (cancelled) return;
        setRepos(rows);
        // 扫完一轮目录后，把**这次新登记**的仓接进当前项目（用户裁定：
        // 「+ 添加仓库」不该只登记目录、不挂项目）。首次加载没有对照基线，不触发。
        const ids = new Set(rows.map((r) => r.id));
        const known = knownIdsRef.current;
        knownIdsRef.current = ids;
        if (known) {
          const registeredNow = rows.filter((r) => !known.has(r.id)).map((r) => r.id);
          if (registeredNow.length > 0) void attachRepos(registeredNow, "scan");
        }
      })
      .catch((err: unknown) => !cancelled && setError(errText(err)));
    return () => {
      cancelled = true;
    };
  }, [reload, attachRepos]);

  useEffect(() => {
    let cancelled = false;
    allProjectRepositories(projectId)
      .then((page) => {
        if (cancelled) return;
        const map: Record<string, string> = {};
        const attached = new Set<string>();
        for (const r of page.items) {
          attached.add(r.id);
          if (r.appCapability?.status) map[r.id] = r.appCapability.status;
        }
        setAppStatusById(map);
        setAttachedIds(attached);
        attachedIdsRef.current = attached;
        projectRevisionRef.current = page.projectRevision;
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

      {/* 接入结果回执：成功/失败都要看得见，不然人只会看到"点了没反应" */}
      {notice && (
        <p
          role="status"
          className={`mt-2 rounded-hard border px-3 py-2 text-[11.5px] ${
            notice.kind === "ok" ? "border-olive/40 bg-olive/5 text-olive" : "border-salmon/40 bg-salmon-well text-salmon"
          }`}
        >
          {notice.text}
        </p>
      )}
      {/* 项目一个仓都没接入时把话说死：建 issue 会直接 0 个可选，别让人自己去猜 */}
      {attachedIds !== null && attachedIds.size === 0 && repos !== null && repos.length > 0 && (
        <p className="mt-2 rounded-hard border border-amber/40 bg-amber-well px-3 py-2 text-[11.5px] text-amber">
          本项目还没有接入任何仓库——建 issue 时会一个都选不出来。在下面任意一张卡片上点「接入本项目」。
        </p>
      )}

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
                        inProject={attachedIds === null ? undefined : attachedIds.has(repo.id)}
                        attachBusy={attachBusy === repo.id}
                        onAttach={(id) => void attachRepos([id], "manual")}
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
