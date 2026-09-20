import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { UsersRound } from "lucide-react";
import { listRepositories, type RepositoryCard } from "../api/repositories";
import { allProjectRepositories, updateProject } from "../api/projects";
import { gridSourceMode } from "../api/grid";
import { dayLabel, errText } from "../display";
import { AddRepositoryCard } from "../components/AddRepositoryCard";
import { AppInstallGuide } from "../components/AppInstallGuide";

import { ErrorPanel, LoadingLine } from "../components/StatusBlocks";

/** 项目仓库页 —— **本项目**的仓库（2026-09-20 改版）。
 *
 *  改版前这一页的身份是「账号仓库目录」：主列表打 `GET /api/scan/repositories`，
 *  后端按调用者的**组织**裁剪（`internal/scan/http.go` catalogForRequest），而组织
 *  与账号 1:1（迁移 0037）—— 于是它列出的是**这个账号下所有项目**扫到的全部仓库。
 *  用户实测：新建项目、一个仓都还没扫，进这一页却看到了**上一个项目**的仓库，
 *  以为数据串了。其实那些仓并没有接入新项目，但它们**长得就像本项目的仓**：大标题
 *  写「仓库」、旁边的计数是目录总数。这是页面定位错了，不是隔离漏了。
 *
 *  现在两段分明：
 *    ① **本项目已接入的仓库** —— 主体。以项目读面
 *       `GET /api/projects/{id}/repositories`（`WHERE pr.project_id=$1`，隔离正确）
 *       为准；卡片详情（语言 / 画像 / 扫描状态）用账号目录的同名行补全。目录里
 *       查不到那一行时**退化显示而不是隐藏** —— 那是两个读面，一个缺行不该让另一个
 *       的事实凭空消失。
 *    ② **账号目录里还能接入的仓库** —— 次级，默认折叠。承载原有的「接入本项目」
 *       「全部接入本项目」「扫描此组织」入口。这一段必须留着：把已扫过的仓接进
 *       本项目是**另一件事**（`project_repositories` 是另一张表），去掉它就再也没有
 *       入口把别的项目扫过的仓接进来了。
 *
 *  两个 id 空间仍按**全名**（`owner/name`）对齐：目录 id 是 32 位随机 hex、
 *  项目 id 是 `repo_<GitHub 数字 id>`，比 id 永远不相等。
 *
 *  分组键 = 仓库 URL 的 owner 段；解析不出的归「独立仓库」组置底。纯展示层分组。
 *  默认全部展开；用户收起的组记 localStorage。
 *
 *  本线保留项：**App 工作授权状态**（「就绪 / 不足」）——项目成员读面
 *  (`allProjectRepositories`) 按仓库全名并入卡片；目录读面不返回它。 */

/** 卡片行：账号目录的卡片，外加"目录里没有这一行"的退化标记（见 fallbackCard）。 */
type RepoRow = RepositoryCard & {
  /** 项目读面有、账号目录里没有这一行（拿不到语言/画像/扫描状态）。 */
  unscanned?: boolean;
};

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

/** App 授权徽标：**三态不许合并**（2026-09-20）。
 *
 *  起因是用户实测：两个仓明明是被 App 覆盖的（`AppCapability` 现测为 `allowed`，
 *  安装的覆盖清单里也有它们），卡片上却写着「App 工作授权不足」。
 *
 *  - `allowed` → 就绪；
 *  - `denied`  → 真的没覆盖，要去安装设置里把它加进选中列表；
 *  - 其它（`unknown` / 取不到 / 网络·限流失败）→ **未确认**，不是"不足"。
 *
 *  把第三种说成"不足"，等于把一次网络抖动说成授权问题——人会白跑一趟安装设置，
 *  回来还是那样。宁可说"没取到"，也不要替 GitHub 下一个它没下过的结论。
 *  状态**取不到时整枚不显示**（不摆一枚"未确认"给每个仓当噪声）。 */
function AppStatusPill({ status }: { status?: string }) {
  if (status === "allowed") return <span className="pill pill-done">App 授权就绪</span>;
  if (status === "denied") {
    return (
      <span className="pill pill-fail" title="GitHub App 没有覆盖这个仓库——去安装设置里把它加进选中列表">
        App 未覆盖此仓
      </span>
    );
  }
  if (!status || status === "unknown") {
    return (
      <span className="pill pill-meta" title="没能取到 GitHub App 的授权状态（网络或限流）——这不等于未授权，稍后刷新会自动重试">
        App 授权未确认
      </span>
    );
  }
  return <span className="pill pill-meta">App {status}</span>;
}

function RepositoryCardView({
  repo,
  appStatus,
  inProject,
  attachReadFailed,
  attachBusy,
  attachResult,
  onAttach,
  onManageTeam,
}: {
  repo: RepoRow;
  appStatus?: string;
  /** 这个仓在不在**当前项目**里（`project_repositories`）。undefined = 读面还没到。 */
  inProject?: boolean;
  /** 读面失败了（区别于"还没回来"）。两者都不该被读成"没接入"，但话要分开说。 */
  attachReadFailed?: boolean;
  attachBusy?: boolean;
  /** 这一次接入的结果，**就地**显示在这张卡上（成功/失败都显示）。
   *  此前只写页面顶部的横幅——人在下面卡片点、横幅在屏幕外，看到的就是"根本没反应"。 */
  attachResult?: { ok: boolean; text: string } | null;
  onAttach?: () => void;
  onManageTeam?: (repositoryId: string) => void;
}) {
  return (
    <div className="rounded-hard border border-line bg-panel px-4 py-3 transition-colors hover:border-line-strong">
      <div className="flex flex-wrap items-center gap-x-2.5 gap-y-1.5">
        <span className="font-mono text-[12.5px] text-tx">{repo.name}</span>
        {repo.languages.length > 0 && (
          <span className="text-[11px] text-tx2">{repo.languages.join(" / ")}</span>
        )}
        {/* 状态徽标统一走 `index.css` 的 .pill 族——高度/圆角/字号由它管，不在每张卡
            上各写一套。次序按"用户该先做什么"排：先接入，再授权。 */}
        <span className="flex flex-wrap items-center gap-1.5">
          {/* 接入状态是卡片上**最要紧**的一条：没接入的仓，建 issue 时一个都选不出来，
              界面必须当场说清，而不是等人撞在 NO_AVAILABLE_REPOSITORIES 上再回来找。
              **未知时要显式说"读取中"**：这个事实来自第二个读面（要对每个仓现探
              GitHub，48 个仓要 4~5 秒），而它在组件挂载之后才发起；此前这一档什么都
              不画，空白看起来就像"标签没了、接入丢了"（2026-09-20 用户实测）。 */}
          {inProject === undefined && !attachReadFailed && (
            <span className="pill pill-meta" title="正在读取本项目已接入的仓库清单">
              接入状态读取中…
            </span>
          )}
          {/* 读失败要说"没取到"，不能说成"没接入"，也不能一直挂着"读取中"当幌子：
              它只是没读到，刷新会重试。 */}
          {inProject === undefined && attachReadFailed && (
            <span className="pill pill-meta" title="没能读到本项目的仓库清单（服务端或网络问题）——这不等于没接入，刷新页面会重试">
              接入状态未取到
            </span>
          )}
          {inProject === true && <span className="pill pill-done">已接入本项目</span>}
          {inProject === false && <span className="pill pill-gate">未接入本项目</span>}
          {appStatus !== undefined && <AppStatusPill status={appStatus} />}
        </span>
        {repo.scanStatus && (
          <span className={`ml-auto pill ${repo.scanStatus === "ok" ? "pill-meta" : "pill-fail"}`}>
            扫描{repo.scanStatus === "ok" ? "完成" : "失败"}
          </span>
        )}
      </div>
      {repo.url && (
        <a
          className="mt-0.5 block font-mono text-[10.5px] text-tx3 hover:text-tx2"
          href={repo.url}
          target="_blank"
          rel="noreferrer"
        >
          {repo.url}
        </a>
      )}
      {repo.description && <p className="mt-1 text-[11.5px] text-tx2">{repo.description}</p>}
      {/* 退化卡片（见 fallbackCard）：如实说明详情为什么是空的，不装作没这回事 */}
      {repo.unscanned && (
        <p className="mt-1 text-[11.5px] text-tx3">
          账号目录里没有这个仓库的扫描记录，语言 / 画像等信息暂时取不到。
        </p>
      )}
      <div className="mt-1 flex items-center gap-3 text-[11px] text-tx3">
        {!repo.unscanned && (
          <span>
            {repo.profiledAt ? `画像 ${dayLabel(repo.profiledAt)}` : "尚未画像"}
            {repo.fingerprint ? ` · 指纹 ${repo.fingerprint.slice(0, 8)}` : ""}
          </span>
        )}
        {inProject === false && onAttach && (
          <button
            type="button"
            className="ml-auto flex flex-none items-center gap-1.5 rounded-hard border border-line px-2 py-[2px] text-[11px] text-tx2 hover:border-amber hover:text-amber-hi disabled:opacity-50"
            onClick={() => onAttach()}
            disabled={attachBusy}
            title="把这个仓库接入当前项目——不接入的话，建 issue 时它不出现在可选仓库里"
          >
            {attachBusy ? "接入中…" : "接入本项目"}
          </button>
        )}
        {/* 团队管理入口（管理员可见）：仓库作用域的长生命周期编制，不在全局入口。
            退化卡片没有目录 id，团队读面认的是扫描侧 id，所以这里不摆这一枚。 */}
        {onManageTeam && repo.id !== "" && (
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
      {attachResult && (
        <p className={`mt-1 text-[11px] ${attachResult.ok ? "text-olive" : "text-salmon"}`} role="status">
          {attachResult.text}
        </p>
      )}
    </div>
  );
}

const COLLAPSED_KEY = "repomesh.repos.collapsedOrgs";
/** 「账号目录里可接入的仓库」那一段的展开状态：**用户显式开合过才记**，
 *  没记过时按数据决定（本项目一个仓都没有 → 默认展开，因为那时这一段就是出路）。 */
const CATALOG_OPEN_KEY = "repomesh.repos.catalogOpen";

/** 目录卡片的**全名**（`owner/name`）。
 *
 *  2026-09-20：目录读面只给 `name`（不含 owner）与 `url`；项目读面给的是
 *  `displayName`（owner/name）。两边的 **id 是不同的空间**（目录 = 32 位随机 hex，
 *  项目 = `repo_<GitHub 数字 id>`）——直接比 id 永远不相等，所以一律按全名对齐。 */
function fullNameOf(repo: { name: string; url: string }): string {
  const org = orgOf(repo.url);
  return org ? `${org}/${repo.name}` : repo.name;
}

/** 项目读面有、账号目录里没有这一行时的兜底卡片。
 *
 *  两个读面各自成立：目录缺一行，不该让**本项目的仓**从列表里消失（那才是真的
 *  "数据丢了"）。地址按 GitHub 规范拼 —— 后端 `ResolveSelectedRepositories` 同样把
 *  host 固定为 github.com，两者同源。详情类字段留空并标记 `unscanned`，卡片如实说明。 */
function fallbackCard(fullName: string): RepoRow {
  const slash = fullName.indexOf("/");
  const owner = slash > 0 ? fullName.slice(0, slash) : "";
  const name = slash > 0 ? fullName.slice(slash + 1) : fullName;
  return {
    id: "",
    name: name || fullName,
    url: owner && name ? `https://github.com/${owner}/${name}` : "",
    description: "",
    topics: [],
    languages: [],
    unscanned: true,
  };
}

function readCollapsed(): string[] {
  try {
    const raw = window.localStorage.getItem(COLLAPSED_KEY);
    return raw ? (JSON.parse(raw) as string[]) : [];
  } catch {
    return [];
  }
}

function readCatalogOpen(): boolean | null {
  try {
    const raw = window.localStorage.getItem(CATALOG_OPEN_KEY);
    return raw === null ? null : raw === "1";
  } catch {
    return null;
  }
}

/** 分组:按 URL owner 段;解析不出归「独立仓库」置底。组内按名称排序。 */
function groupByOrg(cards: RepoRow[]) {
  const map = new Map<string, RepoRow[]>();
  const solo: RepoRow[] = [];
  cards.forEach((r) => {
    const org = r.url ? orgOf(r.url) : null;
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
    list.push({
      org: "独立仓库",
      repos: [...solo].sort((a, b) => a.name.localeCompare(b.name)),
      host: null,
      solo: true,
    });
  }
  return list;
}

/** 上一次读到的「本项目已接入哪些仓」快照，按项目驻留内存。
 *
 *  这个事实来自第二个读面（`/projects/{id}/repositories`），它要对**每个仓现探
 *  GitHub** 才能确认参与权与 App 覆盖——48 个仓实测 4~5 秒。而它是在组件挂载之后
 *  才发起的：卡片先画出来、标签 4 秒后才到。每次进仓库页都从零等一次，观感就是
 *  "绿色标签不见了，过一会又冒出来"（2026-09-20 用户实测；服务端日志里同样能看到
 *  目录先回、项目读面 4 秒后才回，中间还夹着一次 499——人等不及切走了）。
 *
 *  所以把上一次的结果留着，进页面**先按上次的样子画**，后台再刷新，差异才看得出来。
 *  **只驻内存、不落盘**：刷新浏览器就重来一次，不会让人看到上一个会话的陈旧事实。
 *  （拿它做接入判断的 `attachedNamesRef` 也一起种进去，否则批量接入会把已接入的仓
 *  当成待接入再发一遍。）
 *
 *  改版后这份快照还多担一层：主体列表本来就是"本项目有哪些仓"，所以它一就位，
 *  页面画的就是项目级事实（此前它只用来打徽标，列表仍是账号目录）。 */
const attachedSnapshot = new Map<
  string,
  { names: Set<string>; appStatus: Record<string, string> }
>();

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
  /** 账号目录（**不是**本项目列表，只用来补全卡片详情与提供"可接入"候选）。 */
  const [repos, setRepos] = useState<RepositoryCard[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [reload, setReload] = useState(0);
  const [addOpen, setAddOpen] = useState(false);
  const [presetOrgUrl, setPresetOrgUrl] = useState<string | null>(null);
  const [collapsed, setCollapsed] = useState<Set<string>>(() => new Set(readCollapsed()));
  const [catalogOpen, setCatalogOpen] = useState<boolean | null>(readCatalogOpen);
  /** 仓库 `owner/name` → App 工作授权状态（项目成员读面；目录读面不返回它）。
   *  **按名字对齐，不按 id**：目录的 id 是 32 位随机 hex、项目的 id 是
   *  `repo_<GitHub 数字 id>`，两个 id 空间，直接比永远不相等（此前就是这么错的，
   *  于是「已接入」与「App 状态」两个徽标从来没显示过）。 */
  const [appStatusByName, setAppStatusByName] = useState<Record<string, string>>(
    () => attachedSnapshot.get(projectId)?.appStatus ?? {},
  );
  /** 本项目已接入的仓库 `owner/name`。null = 还没读到——那时**主体列表不出结论**
   *  （不显示成"本项目没有仓库"，那是把"没读到"冒充成一个确定的事实）。
   *  初值取上一次的快照：进页面先按上次的样子画，后台再刷新（见 attachedSnapshot）。 */
  const [attachedNames, setAttachedNames] = useState<Set<string> | null>(
    () => attachedSnapshot.get(projectId)?.names ?? null,
  );
  /** 上面这个读面失败了没有。失败时页面说"没取到"而不是一直挂"读取中"。 */
  const [attachReadFailed, setAttachReadFailed] = useState(false);
  /** 手工接入的结果，就地显示在那张卡片上（不是只写页面顶部横幅）。 */
  const [attachResult, setAttachResult] = useState<{ name: string; ok: boolean; text: string } | null>(null);
  const [attachBusy, setAttachBusy] = useState<string | null>(null);
  /** 组织组的批量接入：在途的组名 + 就地回执（结果写在这组头上，不写屏幕外的顶栏）。 */
  const [orgBusy, setOrgBusy] = useState<string | null>(null);
  const [orgResult, setOrgResult] = useState<{ org: string; ok: boolean; text: string } | null>(null);
  const [notice, setNotice] = useState<{ kind: "ok" | "err"; text: string } | null>(null);
  /** 建项目更新要的 `expectedProjectRevision`：与 attachedIds 同一次读面拿回。 */
  const projectRevisionRef = useRef<string | null>(null);
  /** 上一次看到的目录 id 集合——用来认出「这次扫描新登记了哪些仓」。 */
  const knownIdsRef = useRef<Set<string> | null>(null);
  /** 已接入 / 正在接入的 id（ref，因为下面的自动接入跑在回调里，拿不到最新 state）。 */
  const attachedNamesRef = useRef<Set<string>>(attachedSnapshot.get(projectId)?.names ?? new Set<string>());
  const inFlightRef = useRef<Set<string>>(new Set());

  const refresh = useCallback(() => setReload((n) => n + 1), []);

  /** 把仓库接入本项目。**只加不移**——后端只支持加。
   *
   *  传的是 **URL 而不是 id**：接入接口要 `repo_<GitHub 数字 id>`，而目录页只有
   *  `owner/name` 与 URL。后端按 URL 取数字 id、登记进项目注册表再接入。
   *  （此前传目录 id，线上实测必得 422 —— 那是两个 id 空间。）
   *
   *  `source` 决定回执写在哪：`manual` 写单卡、`org` 写组头、`scan` 写顶栏——
   *  **写在离点击最近的地方**，写远了人看到的还是"点了没反应"。 */
  const attachRepos = useCallback(
    async (
      targets: Array<{ url: string; name: string }>,
      source: "manual" | "scan" | "org",
      org?: string,
    ) => {
      const revision = projectRevisionRef.current;
      const fresh = targets.filter((t) => !attachedNamesRef.current.has(t.name) && !inFlightRef.current.has(t.name));
      if (fresh.length === 0) {
        // 目标全在接入中或已接入（状态回读有延迟）。**不能静默返回**——那就是"点了没反应"。
        if (source === "org") setOrgResult({ org: org ?? "", ok: true, text: "这些仓库已经接入或正在接入本项目" });
        return;
      }
      const report = (ok: boolean, text: string) => {
        if (source === "org") setOrgResult({ org: org ?? "", ok, text });
        else if (source === "manual") setAttachResult({ name: fresh[0].name, ok, text });
        else setNotice({ kind: ok ? "ok" : "err", text });
      };
      if (!revision) {
        report(false, "项目版本还没取到——等列表刷出来再试一次");
        return;
      }
      fresh.forEach((t) => inFlightRef.current.add(t.name));
      if (source === "manual") setAttachBusy(fresh[0].name);
      if (source === "org") {
        setOrgBusy(org ?? "");
        setOrgResult(null);
      }
      setNotice(null);
      try {
        await updateProject(
          projectId,
          { expectedProjectRevision: revision, repositoryUrlsToAdd: fresh.map((t) => t.url) },
          crypto.randomUUID(),
        );
        report(
          true,
          source === "scan"
            ? `已自动接入本次新登记的 ${fresh.length} 个仓库`
            : source === "org"
              ? `已接入 ${fresh.length} 个仓库`
              : "已接入本项目",
        );
        setReload((n) => n + 1);
      } catch (err) {
        // 失败就撤掉在途标记，好让人再点一次；已接入集合由列表刷新来纠正。
        report(false, `接入失败：${errText(err)}`);
      } finally {
        fresh.forEach((t) => inFlightRef.current.delete(t.name));
        if (source === "manual") setAttachBusy(null);
        if (source === "org") setOrgBusy(null);
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
          const registeredNow = rows
            .filter((r) => !known.has(r.id))
            .map((r) => ({ url: r.url, name: fullNameOf(r) }));
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
          // 项目读面给 `repo_<数字 id>` + `displayName`（owner/name）。**按名字对齐**：
          // 目录的 id 是另一个空间，比 id 永远不相等。
          const name = r.displayName ?? r.id;
          attached.add(name);
          if (r.appCapability?.status) map[name] = r.appCapability.status;
        }
        setAppStatusByName(map);
        setAttachedNames(attached);
        attachedNamesRef.current = attached;
        projectRevisionRef.current = page.projectRevision;
        // 留一份给下次进页面用（见 attachedSnapshot）。**只在下一次真的读到时写**：
        // 失败不覆盖旧快照，免得一次网络抖动把已知事实抹成未知。
        attachedSnapshot.set(projectId, { names: attached, appStatus: map });
        setAttachReadFailed(false);
      })
      .catch(() => {
        /* 取不到就不显示接入状态——不拿失败当「不足」。已经种下上一次快照的，
           继续按上次的样子显示；从没读到过的，由页面说"没取到"。 */
        if (!cancelled) setAttachReadFailed(true);
      });
    return () => {
      cancelled = true;
    };
  }, [projectId, reload]);

  /** 目录卡片按全名索引 —— 用来给项目读面的行补上详情。 */
  const catalogByName = useMemo(() => {
    const map = new Map<string, RepositoryCard>();
    (repos ?? []).forEach((card) => map.set(fullNameOf(card), card));
    return map;
  }, [repos]);

  /** ① 主体：**本项目的仓库**。以项目读面为准；目录有同名行就用它的详情，没有就退化。 */
  const projectRows = useMemo<RepoRow[] | null>(() => {
    if (attachedNames === null) return null;
    return [...attachedNames]
      .sort((a, b) => a.localeCompare(b))
      .map((name) => catalogByName.get(name) ?? fallbackCard(name));
  }, [attachedNames, catalogByName]);

  /** ② 次级：账号目录里**还没接入本项目**的仓 —— 只剩它们才是"可接入候选"。 */
  const attachableCards = useMemo<RepoRow[] | null>(() => {
    if (repos === null || attachedNames === null) return null;
    return repos.filter((card) => !attachedNames.has(fullNameOf(card)));
  }, [repos, attachedNames]);

  const projectGroups = useMemo(() => groupByOrg(projectRows ?? []), [projectRows]);
  const catalogGroups = useMemo(() => groupByOrg(attachableCards ?? []), [attachableCards]);

  /** 没记过开合状态时：本项目一个仓都没有 → 展开（那时这一段就是唯一出路）。 */
  const catalogOpenEffective =
    catalogOpen ?? (attachedNames !== null && attachedNames.size === 0);

  const toggleCatalog = () => {
    const next = !catalogOpenEffective;
    setCatalogOpen(next);
    try {
      window.localStorage.setItem(CATALOG_OPEN_KEY, next ? "1" : "0");
    } catch {
      /* 隐私模式等存储不可用:只丢记忆,不影响本次展开 */
    }
  };

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
        {/* 计数口径 = **本项目已接入的仓**。改版前这里是账号目录总数，正是"看起来像
            本项目的列表"的根源；口径写在标签里，读的人不必去猜。 */}
        {attachedNames && (
          <span className="text-[11.5px] text-tx2">本项目 {attachedNames.size} 个</span>
        )}
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
      {/* GitHub App 缺口就地提示（2026-09-20）：卡片上那行「App 工作授权不足」是**逐仓**
          的结论，人看到它还得自己去找怎么补——这里给出账号级的原因和直链（安装页/安装
          设置页由后端算好），点一下就能补上。没有缺口时组件自己返回 null。 */}
      <AppInstallGuide variant="inline" />

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

      {/* ───────────────── ① 本项目的仓库（主体） ───────────────── */}
      {error ? (
        <ErrorPanel title="仓库列表加载失败" message={error} onRetry={refresh} />
      ) : attachedNames === null ? (
        /* 项目读面还没回来 / 失败。**这里绝不能说"本项目没有仓库"** —— 那是把
           "还没读到"冒充成一个确定的否定结论（App 状态徽标那一条同样立过这个规矩）。 */
        attachReadFailed ? (
          <ErrorPanel
            title="没能读到本项目的仓库清单"
            message="这不等于本项目没有仓库——服务端或网络问题，重试即可。"
            onRetry={refresh}
          />
        ) : (
          <LoadingLine />
        )
      ) : projectRows === null || projectRows.length === 0 ? (
        <p className="mt-2 rounded-hard border border-amber/40 bg-amber-well px-3 py-2 text-[11.5px] text-amber">
          本项目还没有接入任何仓库——建 issue 时会一个都选不出来。
          {attachableCards !== null && attachableCards.length > 0
            ? "在下面「账号目录里可接入的仓库」里点「接入本项目」，或点组织组头的「全部接入本项目」一次补齐。"
            : "先点「+ 添加仓库」扫一个组织或仓库，扫到的仓会自动接入本项目。"}
        </p>
      ) : (
        <div className="mt-4 space-y-3">
          {projectGroups.map(({ org, repos: rs }) => {
            const isCollapsed = collapsed.has(org);
            return (
              <section key={org}>
                <div className="flex w-full flex-wrap items-center gap-2 rounded-hard px-1 py-1.5">
                  <button
                    type="button"
                    className="flex flex-1 items-center gap-2 text-left hover:text-tx"
                    onClick={() => toggle(org)}
                    aria-expanded={!isCollapsed}
                  >
                    <span className="text-[10px] text-tx3">{isCollapsed ? "▶" : "▼"}</span>
                    <span className="text-[13px] font-semibold text-cream">{org}</span>
                    <span className="text-[11px] text-tx3">
                      {rs.length} 个仓库
                      {rs.some((r) => r.scanStatus === "failed") && " · 有失败"}
                    </span>
                  </button>
                </div>
                {!isCollapsed && (
                  <div className="mt-1.5 grid gap-2 pl-4">
                    {rs.map((repo) => (
                      <RepositoryCardView
                        key={fullNameOf(repo)}
                        repo={repo}
                        appStatus={appStatusByName[fullNameOf(repo)]}
                        inProject={true}
                        attachReadFailed={attachReadFailed}
                        attachResult={attachResult?.name === fullNameOf(repo) ? attachResult : null}
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

      {/* ───────────────── ② 账号目录里可接入的仓库（次级，默认折叠） ───────────────── */}
      {attachedNames !== null && (
        <section className="mt-8 border-t border-line pt-3">
          <button
            type="button"
            className="flex w-full flex-wrap items-center gap-2 rounded-hard px-1 py-1.5 text-left hover:text-tx"
            onClick={toggleCatalog}
            aria-expanded={catalogOpenEffective}
          >
            <span className="text-[10px] text-tx3">{catalogOpenEffective ? "▼" : "▶"}</span>
            <span className="text-[13px] font-semibold text-cream">账号目录里可接入的仓库</span>
            <span className="text-[11px] text-tx3">
              {attachableCards === null ? "读取中…" : `${attachableCards.length} 个未接入`}
            </span>
            <span className="ml-auto text-[11px] text-tx3">
              {catalogOpenEffective ? "收起" : "展开"}
            </span>
          </button>
          {/* 这一段为什么在：账号目录是**整个账号**扫过的仓（横跨你所有项目），
              把仓接进本项目是另一张表另一件事。所以它不是"本项目的仓库"，
              但它是唯一的接入入口 —— 收起来，别和主体混在一起看。 */}
          {catalogOpenEffective && (
            <>
              {attachableCards === null ? (
                <div className="mt-2 pl-4">
                  <LoadingLine />
                </div>
              ) : catalogGroups.length === 0 ? (
                <div className="py-4 pl-4 text-[12px] text-tx3">
                  账号目录里的仓库都已接入本项目。
                </div>
              ) : (
                <div className="mt-2 space-y-3">
                  {catalogGroups.map(({ org, repos: rs, host, solo }) => {
                    const isCollapsed = collapsed.has(org);
                    const busy = orgBusy === org;
                    return (
                      <section key={org}>
                        {/* 组头原先整体是一个 button，动作只能塞成 span。现在拆开：折叠是 button，
                            动作各自是真的 button（键盘可达），批量接入才放得进来。 */}
                        <div className="flex w-full flex-wrap items-center gap-2 rounded-hard px-1 py-1.5">
                          <button
                            type="button"
                            className="flex flex-1 items-center gap-2 text-left hover:text-tx"
                            onClick={() => toggle(org)}
                            aria-expanded={!isCollapsed}
                          >
                            <span className="text-[10px] text-tx3">{isCollapsed ? "▶" : "▼"}</span>
                            <span className="text-[13px] font-semibold text-cream">{org}</span>
                            <span className="text-[11px] text-tx3">
                              {rs.length} 个仓库
                              {rs.some((r) => r.scanStatus === "failed") && " · 有失败"}
                            </span>
                          </button>
                          <button
                            type="button"
                            className="flex flex-none items-center gap-1.5 rounded-hard border border-line px-2 py-[2px] text-[11px] text-tx2 hover:border-amber hover:text-amber-hi disabled:opacity-50"
                            onClick={() =>
                              void attachRepos(
                                rs.map((r) => ({ url: r.url, name: fullNameOf(r) })),
                                "org",
                                org,
                              )
                            }
                            disabled={busy}
                            title={`把这一组里还没接入的 ${rs.length} 个仓库一次性接入本项目`}
                          >
                            {busy ? "接入中…" : `全部接入本项目 (${rs.length})`}
                          </button>
                          {!solo && host && (
                            <button
                              type="button"
                              className="flex-none text-[11px] text-tx3 underline-offset-2 hover:text-amber hover:underline"
                              onClick={() => scanThisOrg(org, host)}
                            >
                              扫描此组织
                            </button>
                          )}
                        </div>
                        {orgResult?.org === org && (
                          <p
                            role="status"
                            className={`px-1 pb-0.5 text-[11px] ${orgResult.ok ? "text-olive" : "text-salmon"}`}
                          >
                            {orgResult.text}
                          </p>
                        )}
                        {!isCollapsed && (
                          <div className="mt-1.5 grid gap-2 pl-4">
                            {rs.map((repo) => (
                              <RepositoryCardView
                                key={fullNameOf(repo)}
                                repo={repo}
                                appStatus={appStatusByName[fullNameOf(repo)]}
                                inProject={false}
                                attachReadFailed={attachReadFailed}
                                attachBusy={attachBusy === fullNameOf(repo)}
                                attachResult={attachResult?.name === fullNameOf(repo) ? attachResult : null}
                                onAttach={() => void attachRepos([{ url: repo.url, name: fullNameOf(repo) }], "manual")}
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
            </>
          )}
        </section>
      )}
    </div>
  );
}
