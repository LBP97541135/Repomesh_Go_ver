import { useEffect, useRef, useState } from "react";
import type { Account } from "../api/auth";
import type { ProjectListItem } from "../api/projects";
import {
  Activity,
  Bot,
  ChevronDown,
  FileCheck,
  FolderKanban,
  History,
  Inbox,
  PanelLeftClose,
  PanelLeftOpen,
  Search,
  Settings,
  type LucideIcon,
} from "lucide-react";

/** v2 侧栏（CONS-40 → B-2 接线）＋ 2026-09-08 图标语言统一（用户三轮裁决）：
 *
 *  · 导航图标全量换 lucide 16px / 1.5 细线，自绘 SVG 退役；
 *  · 选中态＝圆角块（side-active 底 + 字重加重），左竖条语言退役；
 *  · 导航分「工作台 / 治理」两组（大写小节标题），历史决策与设置沉底；
 *  · 计数改 badge 胶囊（只展示真实数据，null 不显示）；
 *  · 侧栏可折叠成 64px 图标栏（悬停 tooltip）；
 *  · 工作区切换器 prompt 式双行（品牌方块 + REPOMESH + 当前工作区）；
 *  · 顶部搜索行接 ⌘K 命令面板（面板本体在 ConsoleShell）；
 *  · 「近期会话」列表按用户裁决整体移除（2026-09-08），会话入口收敛到
 *    issue 列表页与 ⌘K 搜索。
 *
 *  业务回路原样保留：工作区下拉/创建（A2 幂等键）、身份块、登出、
 *  回放模式提示。计数的 null 语义 = 数据源未提供，不显示 0 不编造。 */

export type NavKey =
  | "projects"
  | "skills"
  | "models"
  | "issues"
  | "reviews"
  | "repositories"
  | "agents"
  | "observe"
  | "decision-chains"
  | "settings";

const NAV_ICON: Record<NavKey, LucideIcon> = {
  projects: FolderKanban,
  skills: FileCheck,
  models: FolderKanban,
  issues: Inbox,
  reviews: FileCheck,
  repositories: FolderKanban,
  agents: Bot,
  observe: Activity,
  "decision-chains": History,
  settings: Settings,
};

const NAV_LABEL: Record<NavKey, string> = {
  projects: "项目",
  skills: "技能",
  models: "模型",
  issues: "issue",
  reviews: "审核",
  repositories: "仓库",
  agents: "智能体",
  observe: "观测",
  "decision-chains": "历史决策",
  settings: "设置",
};

/** 导航两组 + 底部区（历史决策/设置与身份块同区，prompt 的 bottom items 结构）。 */
/** 2026-09-16 用户裁定：reviews/teams/agents/observe 四项恢复常显，不再等
 *  Go 后端迁移（「迁一块点亮一块」口径暂停执行）。其中多数页面的接口后端
 *  尚无落点（/deliveries/*、/observe/*、/console/*），点开会看到
 *  not_implemented——先恢复入口，后端补齐即自动点亮。 */
const NAV_GROUPS: Array<{ heading: string; keys: NavKey[] }> = [
  { heading: "工作台", keys: ["projects", "issues", "reviews", "repositories"] },
  // 治理（2026-09-20 用户裁定）：历史决策归治理；智能体与技能已收编进设置
  { heading: "治理", keys: ["models", "observe", "decision-chains"] },
];
/** 底部常驻项：设置（恢复原样——图标 + 文字的一行，不跟分组抢位置）。 */
const NAV_BOTTOM: NavKey[] = ["settings"];

export function SidebarV2({
  account,
  nav,
  issueCount,
  reviewCount,
  onNavigate,
  onNewIssue,
  onLogout,
  onSwitchAccount,
  onOpenSearch,
  projects,
  activeProjectId,
  onSelectProject,
  onManageProjects,
}: {
  account: Account;
  nav: NavKey;
  /** issue 导航计数；null = 数据源未提供（不显示计数，不编造） */
  issueCount: number | null;
  /** 待办审核数；null = 取不到（未登录/流断），此时不显示计数而不是显示 0 */
  reviewCount: number | null;
  onNavigate: (nav: NavKey) => void;
  onNewIssue: () => void;
  onLogout: () => void;
  onSwitchAccount: () => void;
  /** 可切换的项目列表；null = 还没取到（显示"读取中"，不编造） */
  projects: ProjectListItem[] | null;
  /** 当前项目 id；null = 还没选过 */
  activeProjectId: string | null;
  onSelectProject: (projectId: string) => void;
  /** 打开「项目」页（管理/新建） */
  onManageProjects: () => void;
  /** 打开 ⌘K 命令面板（面板状态与快捷键监听在 ConsoleShell） */
  onOpenSearch?: () => void;
}) {
  const [dropOpen, setDropOpen] = useState(false);
  const [accountOpen, setAccountOpen] = useState(false);
  const [collapsed, setCollapsed] = useState(false);
  const dropRef = useRef<HTMLDivElement>(null);
  const accountRef = useRef<HTMLDivElement>(null);
  // 2026-09-19 用户裁定：左上角从「REPOMESH」改为**当前项目名**并承载项目切换；
  // 账号管理（切换账号/退出登录）移到左下角账号块。两处各自点外关闭。
  useEffect(() => {
    if (!accountOpen) return;
    const onDocClick = (e: MouseEvent) => {
      if (!accountRef.current?.contains(e.target as Node)) setAccountOpen(false);
    };
    document.addEventListener("click", onDocClick);
    return () => document.removeEventListener("click", onDocClick);
  }, [accountOpen]);
  useEffect(() => {
    if (!dropOpen) return;
    const onDocClick = (e: MouseEvent) => {
      if (!dropRef.current?.contains(e.target as Node)) setDropOpen(false);
    };
    document.addEventListener("click", onDocClick);
    return () => document.removeEventListener("click", onDocClick);
  }, [dropOpen]);

  const initial = (account.display_name || account.username).slice(0, 1);
  const activeProject = (projects ?? []).find((item) => item.id === activeProjectId) ?? null;

  const countOf = (key: NavKey): number | null =>
    key === "issues" ? issueCount : key === "reviews" ? reviewCount : null;

  const NavButton = ({ item }: { item: NavKey }) => {
    const active = nav === item;
    const Icon = NAV_ICON[item];
    const count = countOf(item);
    return (
      <button
        title={collapsed ? NAV_LABEL[item] : undefined}
        className={`group flex w-full items-center gap-2.5 rounded-[6px] px-2.5 py-[7px] text-left text-[13px] select-none transition-colors ${
          collapsed ? "justify-center px-0" : ""
        } ${
          active
            ? "bg-side-active font-medium text-cream"
            : "text-tx2 hover:bg-side-active/50 hover:text-tx"
        }`}
        onClick={() => onNavigate(item)}
      >
        <Icon size={16} strokeWidth={1.5} className="flex-none" />
        {!collapsed && <span className="min-w-0 flex-1 truncate tracking-wide">{NAV_LABEL[item]}</span>}
        {!collapsed && count !== null && (
          <span className="flex h-5 min-w-[20px] flex-none items-center justify-center rounded-full bg-amber/10 px-1.5 font-mono text-[10px] font-medium text-amber-hi">
            {count}
          </span>
        )}
      </button>
    );
  };

  const GroupHeading = ({ text }: { text: string }) =>
    collapsed ? (
      <div className="mx-auto my-2 h-px w-6 bg-line" />
    ) : (
      <div className="microlabel mb-1 mt-3 px-2.5 first:mt-0">{text}</div>
    );

  return (
    <aside
      className={`scrollbar-none relative flex flex-none flex-col overflow-y-auto border-r border-line bg-side-rail px-3 pt-3.5 pb-3 transition-[width] duration-200 ${
        collapsed ? "w-[64px]" : "w-[236px]"
      }`}
    >
      <div ref={dropRef} className="relative">
        <button
          title={collapsed ? (activeProject?.name ?? "选择项目") : undefined}
          className={`flex w-full items-center rounded-[8px] px-1.5 py-1.5 text-left transition-colors hover:bg-side-active/50 ${
            collapsed ? "justify-center px-0" : "gap-2.5"
          }`}
          onClick={(e) => {
            e.stopPropagation();
            // 收起态下这条菜单需要整条侧栏的宽度才放得下，先展开再开——
            // 否则它以一个 236px 的宽度飘在 64px 的轨道里，会被裁掉一半。
            if (collapsed) setCollapsed(false);
            setDropOpen((v) => !v);
          }}
        >
          <span className="grid size-[32px] flex-none place-items-center rounded-[6px] bg-chalk font-mono text-[14px] font-semibold text-on-chalk">
            {(activeProject?.name ?? "R").slice(0, 1).toUpperCase()}
          </span>
          {!collapsed && (
            <span className="min-w-0 flex-1">
              <span className="block truncate text-[12.5px] font-medium leading-none text-tx">
                {activeProject?.name ?? "选择项目"}
              </span>
            </span>
          )}
          {!collapsed && (
            <ChevronDown
              size={14}
              strokeWidth={1.5}
              className={`flex-none text-tx3 transition-transform ${dropOpen ? "rotate-180" : ""}`}
            />
          )}
        </button>

        {dropOpen && (
          /* 宽度用 w-full（= 侧栏内容区宽度）：此前写死 `w-[236px]`，而侧栏是
             `w-[236px] px-3`，内容区只有 212px —— 弹窗右边多出 24px 的偏移，
             一半在轨道外，还被 side-rail 的 overflow 裁掉（2026-09-20 实测）。
             侧栏宽度以后要变，这个数字也不该再抄一遍。 */
          <div className="absolute top-[52px] left-0 z-20 w-full rounded-[8px] border border-line bg-side-panel py-1 shadow-float">
            <div className="px-2.5 pt-1 pb-1.5 text-[10.5px] tracking-[0.1em] text-tx3">切换项目</div>
            <div className="max-h-[240px] overflow-y-auto">
              {projects === null && <div className="px-2.5 py-2 text-[12px] text-tx3">正在读取项目…</div>}
              {projects !== null && projects.length === 0 && (
                <div className="px-2.5 py-2 text-[12px] text-tx3">还没有项目，点下面「管理 / 新建项目」。</div>
              )}
              {(projects ?? []).map((item) => (
                <button
                  key={item.id}
                  className="flex w-full items-center gap-2 px-2.5 py-[6px] text-left text-[12.5px] text-tx hover:bg-amber/10"
                  onClick={() => {
                    setDropOpen(false);
                    onSelectProject(item.id);
                  }}
                >
                  <span className="min-w-0 flex-1 truncate">{item.name}</span>
                  {item.id === activeProjectId && <span className="flex-none text-amber-hi">✓</span>}
                </button>
              ))}
            </div>
            <button
              className="flex w-full items-center gap-2 border-t border-line px-2.5 pt-2 pb-1 text-left text-[12.5px] text-tx2 hover:bg-amber/10 hover:text-cream"
              onClick={() => {
                setDropOpen(false);
                onManageProjects();
              }}
            >
              管理 / 新建项目
            </button>
          </div>
        )}
      </div>

      {/* ⌘K 搜索入口 + 新建 issue（折叠态各自变方块） */}
      <div className={`mt-3 flex flex-col gap-1.5 ${collapsed ? "items-center" : ""}`}>
        <button
          title={collapsed ? "搜索（Ctrl+K）" : undefined}
          className={`flex items-center gap-2 rounded-[6px] border border-line px-2 py-[6px] text-left text-tx3 transition-colors hover:border-tx3/40 hover:text-tx2 ${
            collapsed ? "w-8 justify-center border-transparent px-0 hover:bg-side-active/50" : "w-full"
          }`}
          onClick={onOpenSearch}
        >
          <Search size={14} strokeWidth={1.5} className="flex-none" />
          {!collapsed && <span className="flex-1 text-[12px]">搜索…</span>}
          {!collapsed && (
            <kbd className="rounded-[4px] border border-line bg-panel px-1 font-mono text-[9.5px] text-tx3">
              Ctrl K
            </kbd>
          )}
        </button>
        <button
          title={collapsed ? "新建 issue" : undefined}
          className={`flex w-full items-center justify-center gap-1.5 rounded-[8px] bg-amber py-[7px] text-[12.5px] font-extrabold tracking-[0.04em] text-on-amber transition-[filter] hover:brightness-105 ${
            collapsed ? "w-8 text-[15px] leading-none" : ""
          }`}
          onClick={onNewIssue}
        >
          {collapsed ? "+" : "+ 新建 issue"}
        </button>
      </div>

      <nav className={`mt-2 flex min-h-0 flex-1 flex-col overflow-y-auto scrollbar-none ${collapsed ? "mt-3 gap-2" : ""}`}>
        {NAV_GROUPS.map((group) => (
          <div key={group.heading} className="flex flex-col gap-0.5">
            <GroupHeading text={group.heading} />
            {group.keys.map((key) => (
              <NavButton key={key} item={key} />
            ))}
          </div>
        ))}

        {/* 「近期会话」列表已按用户裁决（2026-09-08）整体移除：
            会话入口收敛到 issue 列表页与 ⌘K 搜索，不在侧栏重复挂一份。 */}
      </nav>

      <div className="mt-auto grid gap-0.5 border-t border-line pt-2">
        {collapsed && <div className="mx-auto mb-2 h-px w-6 bg-line" />}
        {NAV_BOTTOM.map((key) => (
          <NavButton key={key} item={key} />
        ))}
        {/* 账号块（2026-09-19 用户裁定）：账号管理归这里——切换账号 / 退出登录。
            项目切换在左上角，两处不再混在一个下拉里。 */}
        <div ref={accountRef} className="relative">
          <button
            title={collapsed ? account.display_name || account.username : undefined}
            className={`flex w-full items-center gap-2 rounded-[8px] px-2 py-1.5 text-left transition-colors hover:bg-side-active/50 ${
              collapsed ? "flex-col justify-center px-0" : ""
            }`}
            onClick={(e) => {
              e.stopPropagation();
              // 与左上角项目菜单同理：收起态先展开，别让菜单飘在轨道外面被裁。
              if (collapsed) setCollapsed(false);
              setAccountOpen((v) => !v);
            }}
          >
            <span className="grid size-7 flex-none place-items-center rounded-full bg-chip text-[12px] font-extrabold text-cream">
              {initial}
            </span>
            {!collapsed && (
              <div className="min-w-0 flex-1">
                <b className="block truncate text-[12px] text-tx">{account.display_name || account.username}</b>
                <small className="block truncate text-[10.5px] text-tx2">
                  {account.is_admin ? "管理员" : "个人账号"}
                </small>
              </div>
            )}
          </button>

          {accountOpen && (
            /* 宽度同 w-full（= 侧栏内容区）：此前写死 `w-[218px]`，比内容区 212px
               还宽 6px，同样会溢出到轨道外被裁。 */
            <div className="absolute bottom-[46px] left-0 z-20 w-full rounded-[8px] border border-line bg-side-panel py-1 shadow-float">
              <div className="flex items-center gap-2.5 px-2.5 pt-1 pb-2.5">
                <span className="grid size-[30px] flex-none place-items-center rounded-full bg-chip text-[12px] font-extrabold text-cream">
                  {initial}
                </span>
                <div className="min-w-0">
                  <div className="truncate text-[12.5px] text-tx">{account.display_name || account.username}</div>
                  <div className="truncate font-mono text-[10.5px] text-tx2">
                    {account.username}
                    {account.is_admin ? " · ADMIN" : ""}
                  </div>
                </div>
              </div>
              <button
                className="flex w-full items-center gap-2 border-t border-line px-2.5 pt-2 text-left text-[12.5px] text-tx2 hover:bg-amber/10 hover:text-cream"
                onClick={() => {
                  setAccountOpen(false);
                  onSwitchAccount();
                }}
              >
                切换账号
              </button>
              <button
                className="flex w-full items-center gap-2 border-t border-line px-2.5 pt-2 pb-1 text-left text-[12.5px] text-salmon hover:bg-salmon/10"
                onClick={() => {
                  setAccountOpen(false);
                  onLogout();
                }}
              >
                退出登录
              </button>
            </div>
          )}
        </div>
        <button
          title={collapsed ? "展开侧栏" : "收起侧栏"}
          className={`mt-0.5 grid h-7 place-items-center rounded-[6px] text-tx3 transition-colors hover:bg-side-active/50 hover:text-tx ${
            collapsed ? "w-full" : "w-7"
          }`}
          onClick={() => setCollapsed((v) => !v)}
        >
          {collapsed ? <PanelLeftOpen size={15} strokeWidth={1.5} /> : <PanelLeftClose size={15} strokeWidth={1.5} />}
        </button>
      </div>
    </aside>
  );
}
