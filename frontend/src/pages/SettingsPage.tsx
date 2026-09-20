import { useCallback, useEffect, useState } from "react";
import type { Account } from "../api/auth";
import type {
  CodingAgentAdapterView,
  CodingAgentsProbe,
  ConsoleAgentView,
  RuntimeKind,
  SetupStatusView,
} from "../api/contract";
import { fetchConsoleAgents, gridSourceMode } from "../api/grid";
import { fetchCodingAgents, fetchSetupStatus } from "../api/platformSetup";
import { LocalAccountsPanel } from "../components/LocalAccountsPanel";
import { AppInstallGuide } from "../components/AppInstallGuide";
import { reconnectGithubConnection } from "../api/auth";
import { LocalCliPage } from "./LocalCliPage";
import { TypeSafeSettings } from "../components/TypeSafeSettings";
import { ModelUsageSettings } from "../components/ModelUsageSettings";
import { ModelProvidersPage } from "./ModelProvidersPage";
import { SupervisionPolicySettings } from "../components/SupervisionPolicySettings";
import { AgentsPage } from "./AgentsPage";
import { SkillsPage } from "./SkillsPage";
import { Bot, FileCheck, Info, KeyRound, Server, Settings2, SquareTerminal, Users, type LucideIcon } from "lucide-react";
import { errText } from "../display";
import { fetchBackendVersion } from "../api/http";
import { applyTheme, readStoredTheme, type ThemeName } from "../theme";
import { useRuntimeRows } from "./useRuntimeRows";

/** 模型与 API 按仓库分析、AgentTeams 和观测评估标注用途。
 * 沿用现有配置与权限，同时保留 GitHub 重连、智能体和技能设置。 */

type CategoryKey = "general" | "models" | "account" | "platform" | "agents" | "skills" | "localcli" | "about";

/** 分类图标（lucide）：Trae 同款「图标 + 文字」导航项。 */
const CATEGORIES: { key: CategoryKey; label: string; icon: LucideIcon }[] = [
  { key: "general", label: "通用", icon: Settings2 },
  { key: "models", label: "模型与 API", icon: KeyRound },
  { key: "account", label: "账号与权限", icon: Users },
  { key: "platform", label: "平台", icon: Server },
  { key: "agents", label: "智能体", icon: Bot },
  { key: "skills", label: "技能", icon: FileCheck },
  { key: "localcli", label: "本地 CLI", icon: SquareTerminal },
  { key: "about", label: "关于", icon: Info },
];

/** GitHub 连接：凭据失效 / 需要重新授权 / 权限范围变了时的重连入口。
 *
 *  后端 reconnect 端点本来就有（与 switch 同级），此前只是前端没接——于是凭据
 *  一坏就只能"退出登录再登一遍"（2026-09-20 线上实测：那条路还不一定走通）。 */
function GitHubConnectionRow({ onToast }: { onToast: (text: string) => void }) {
  const [busy, setBusy] = useState(false);
  return (
    <SettingRow
      title="GitHub 连接"
      note="凭据失效、需要重新授权或权限范围变了时，从这里重新连接——保持当前登录与账号不变。"
    >
      <button
        type="button"
        className="rounded-hard border border-line px-3 py-1.5 text-[12px] text-tx2 transition-colors hover:border-amber hover:text-amber-hi disabled:opacity-50"
        disabled={busy}
        onClick={() => {
          setBusy(true);
          reconnectGithubConnection().catch((err: unknown) => {
            setBusy(false);
            onToast(`发起重连失败：${errText(err)}`);
          });
        }}
      >
        {busy ? "跳转中…" : "重新连接 GitHub"}
      </button>
    </SettingRow>
  );
}

/* ── Trae 式行与控件 ─────────────────────────────────────────────────────── */

/** 设置行：左「标题 + 说明小字」，右「控件或状态」。细分隔线、紧凑密度。 */
function SettingRow({
  title,
  note,
  children,
}: {
  title: React.ReactNode;
  note?: React.ReactNode;
  children?: React.ReactNode;
}) {
  return (
    <div className="flex items-center justify-between gap-6 border-b border-panel py-2.5 last:border-b-0">
      <div className="min-w-0">
        <p className="text-[12.5px] text-tx">{title}</p>
        {note != null && (
          <p className="mt-px max-w-[56ch] text-[11px] leading-relaxed text-tx3">{note}</p>
        )}
      </div>
      {children != null && <div className="flex-none">{children}</div>}
    </div>
  );
}

/** 右侧状态：色点 + 文案。idle（灰）是「选检未过 / 未安装」这类不是故障的态。 */
function StatusDot({ tone, label }: { tone: "ok" | "bad" | "idle" | "warn"; label: string }) {
  const dot = { ok: "bg-olive", bad: "bg-salmon", idle: "bg-line", warn: "bg-amber" }[tone];
  const text = { ok: "text-olive", bad: "text-salmon", idle: "text-tx3", warn: "text-amber" }[tone];
  return (
    <span className={`inline-flex items-center gap-1.5 whitespace-nowrap text-[11.5px] ${text}`}>
      <i className={`size-[7px] flex-none rounded-full not-italic ${dot}`} />
      {label}
    </span>
  );
}

/** Trae 同款右侧下拉：圆角、细边、聚焦琥珀。 */
function SelectControl({
  value,
  onChange,
  options,
}: {
  value: string;
  onChange: (value: string) => void;
  options: { value: string; label: string }[];
}) {
  return (
    <select
      value={value}
      onChange={(event) => onChange(event.target.value)}
      className="rounded-hard border border-line bg-well px-2.5 py-1.5 text-[12px] text-tx outline-none transition-colors hover:border-tx2 focus:border-amber"
    >
      {options.map((option) => (
        <option key={option.value} value={option.value}>
          {option.label}
        </option>
      ))}
    </select>
  );
}

/** 内容区分类标题（右侧窗格顶部）。 */
function CategoryTitle({ children }: { children: React.ReactNode }) {
  return <h2 className="pb-1 text-[13.5px] font-semibold text-cream">{children}</h2>;
}

/** 右侧长值（连接健康的探测计数之类），右对齐等宽。 */
function RowValue({ children }: { children: React.ReactNode }) {
  return <span className="block text-right font-mono text-[11.5px] leading-relaxed text-tx">{children}</span>;
}

/* ── 通用 ────────────────────────────────────────────────────────────────── */

const THEME_LABEL: Record<ThemeName, string> = {
  dark: "深色（默认）",
  light: "浅色",
};

/** 界面主题：唯一的非服务端设置，只写本机 localStorage（见 theme.ts），
 *  所以它不是本页的「写路径」。控件按定稿用 Trae 式下拉。 */
function GeneralCategory() {
  const [theme, setTheme] = useState<ThemeName>(readStoredTheme);
  const source = gridSourceMode();
  return (
    <>
      <CategoryTitle>通用</CategoryTitle>
      <SettingRow title="界面主题">
        <SelectControl
          value={theme}
          onChange={(value) => {
            const name = value as ThemeName;
            setTheme(name);
            applyTheme(name);
          }}
          options={(["dark", "light"] as const).map((name) => ({ value: name, label: THEME_LABEL[name] }))}
        />
      </SettingRow>
      <SettingRow title="数据源">
        <StatusDot
          tone={source === "live" ? "ok" : "warn"}
          label={source === "live" ? "live · 真实读模型" : "replay · 夹具回放"}
        />
      </SettingRow>
    </>
  );
}

/* ── 平台 ────────────────────────────────────────────────────────────────── */

/** 九项检查的中文标签。后端返回的是机器名（`checks` 的键与 `next_actions` 的元素
 *  同一套），这里只做措辞，**不判定通过与否**——`ready_for_project_creation` 由
 *  服务端算，前端重算一遍就是第二套判定。 */
const CHECK_LABEL: Record<string, string> = {
  model: "模型连接",
  database: "数据库",
  agentteams: "AgentTeams",
  matrix: "Matrix 消息面",
  internal_auth: "内部凭据",
  github_app: "GitHub App",
  administrator: "管理员账号",
  agent_directory: "智能体花名册",
  repositories: "仓库 catalog",
};

function PlatformCategory({
  setup,
  setupError,
  account,
  base,
  onConfigure,
  controller,
  backendVersion,
}: {
  setup: SetupStatusView | null;
  setupError: string | null;
  account: Account;
  base: string;
  onConfigure: () => void;
  controller: { value: string; note: string | null; loading: boolean };
  /** 后端自报版本（/healthz）。null = 取不到（如实显示"取不到"）。 */
  backendVersion: string | null;
}) {
  const requiredChecks = new Set(
    setup?.dependencies.filter((dependency) => dependency.required).map((item) => item.id) ?? [],
  );
  const blocking = setup?.next_actions.filter((name) => requiredChecks.has(name)) ?? [];
  return (
    <>
      <CategoryTitle>平台</CategoryTitle>
      {setupError ? (
        <p className="py-2 text-[11.5px] text-salmon">就绪检查取用失败：{setupError}</p>
      ) : setup === null ? (
        <p className="py-2 text-[11.5px] text-tx3">检查中…</p>
      ) : (
        <>
          <SettingRow
            title="可建项目"
            note={
              setup.ready_for_project_creation
                ? "全部必检通过"
                : // next_actions 混装必检与选检。照抄会把 GitHub App 这类
                  // 「这套部署没走 GitHub 交付」说成拦路项，所以这里只报
                  // 真正挡路的那几项。
                  `必检未过：${blocking.map((name) => CHECK_LABEL[name] ?? name).join(" · ")}`
            }
          >
            <StatusDot
              tone={setup.ready_for_project_creation ? "ok" : "bad"}
              label={setup.ready_for_project_creation ? "就绪" : "未就绪"}
            />
          </SettingRow>
          {Object.entries(setup.checks).map(([name, passed]) => (
            <SettingRow key={name} title={CHECK_LABEL[name] ?? name}>
              {/* 未通过的选检项用灰而非红：github_app 没配不是故障，
                  是这套部署没走 GitHub 交付。颜色区分必检与选检。 */}
              <StatusDot
                tone={passed ? "ok" : requiredChecks.has(name) ? "bad" : "idle"}
                label={passed ? "已就绪" : requiredChecks.has(name) ? "必检未过" : "选检未过"}
              />
            </SettingRow>
          ))}
          <p className="pt-2 text-[11px] text-tx3">
            账号 {setup.counts.accounts} · 智能体 {setup.counts.agents} · 仓库 {setup.counts.repositories}。
          </p>
          {Object.values(setup.checks).some((passed) => !passed) ? (
            <button
              className="mt-3 rounded-hard border border-amber px-3 py-1.5 text-[11.5px] text-amber hover:bg-amber/10"
              onClick={onConfigure}
            >
              去配置
            </button>
          ) : null}
        </>
      )}

      <h3 className="pb-1 pt-5 text-[11px] font-semibold tracking-widest text-tx3 uppercase">连接健康</h3>
      {/* ⚠️ 这个 note 必须接上：`controller.note` 由 SettingsPage 算好传进来，
          但这里此前**只传了 title 与 children**，note 被整个丢掉 —— 于是无论
          「不可达是契约规定的降级…」还是新加的"无事实：<成员名>"，**从来没有
          显示过**。用户看到的就是孤零零三个数字，问"这个信息有什么用"，
          根子在这。 */}
      <SettingRow title="AgentTeams Controller" note={controller.note}>
        {controller.loading ? (
          <span className="text-[11.5px] text-tx3">探测中…</span>
        ) : (
          <RowValue>{controller.value}</RowValue>
        )}
      </SettingRow>
      <SettingRow title="读模型 API">
        <RowValue>{base === "" ? "同源（经代理 /api）" : base}</RowValue>
      </SettingRow>
      <SettingRow title="本地身份服务">
        <RowValue>
          已登录 · {account.username} · {account.is_admin ? "管理员" : "个人账号"}
        </RowValue>
      </SettingRow>

      {/* ── 部署（F：可运维交付后台）────────────────────────────────────────
          评委要求"部署页展示组件健康、仓库凭据权限范围与版本信息 + 清晰启动配置说明"。
          组件健康在上面（就绪检查 + 连接健康）、凭据覆盖在下面的「GitHub App 授权」，
          这里补**版本信息**与**启动配置说明**这两块。 */}
      <h3 className="pb-1 pt-5 text-[11px] font-semibold tracking-widest text-tx3 uppercase">部署</h3>
      <SettingRow title="控制台版本（构建期）" note="前端产物构建时注入的 commit">
        <RowValue>{__APP_VERSION__}</RowValue>
      </SettingRow>
      <SettingRow
        title="服务端版本（运行时）"
        note={
          backendVersion === null
            ? "取不到 —— /healthz 没应答或没带 version。如实留空，不拿前端版本冒充后端。"
            : backendVersion === __APP_VERSION__
              ? "与前端构建版本一致"
              : "与前端构建版本**不一致** —— 前端产物与后端二进制不是同一次构建，值得查一下部署流水"
        }
      >
        <RowValue>{backendVersion ?? "取不到"}</RowValue>
      </SettingRow>
      <SettingRow title="服务端进程" note="三件套各自是独立的 systemd 单元；任一没起，对应能力即不可用">
        <RowValue>repomesh-web · repomesh-coordinator · repomesh-host-executor</RowValue>
      </SettingRow>
      <SettingRow
        title="启动配置"
        note="部署凭据只从服务器本地环境文件读，不进仓库、不进 GitHub Secrets"
      >
        <RowValue>/etc/repomesh/env</RowValue>
      </SettingRow>
      <SettingRow title="部署方式" note="push main 触发自托管 runner：构建三件套 → 迁移 → 安装 → 重启">
        <RowValue>.github/workflows/deploy.yml</RowValue>
      </SettingRow>
      <SettingRow
        title="故障排查"
        note="组件不健康时按这个顺序看：① systemctl status 三件套 ② journalctl -u repomesh-web -n 100 ③ /healthz 的 version 是否等于本次提交"
      >
        <RowValue>journalctl -u repomesh-web</RowValue>
      </SettingRow>
    </>
  );
}

/* ── 智能体 ──────────────────────────────────────────────────────────────── */

function AdapterRow({ adapter }: { adapter: CodingAgentAdapterView }) {
  // 「已装但没验过授权态」与「装了且验过」是两件事，与「没装」也是两件事。
  // 此前一律显示"无法判定"——那句话既没说清是哪种情况，也没说清下一步做什么，
  // 用户原话就是"这里需要优化"。现在按事实分三档说。
  const label = !adapter.installed
    ? "未安装"
    : adapter.auth_status === "authorized"
      ? "已安装 · 已授权"
      : adapter.auth_status === "unauthorized"
        ? "已安装 · 未授权"
        : "已安装 · 授权态未验证";
  const hint = !adapter.installed
    ? `在这台机器上没找到 ${adapter.adapter_id} 的可执行文件（${adapter.detail ?? "binary_not_found"}）`
    : adapter.auth_status === "unknown"
      ? `可执行文件：${adapter.executable ?? "—"}；授权态要实际跑一次才能判定，这里如实为"未验证"，不是"没认上"`
      : `可执行文件：${adapter.executable ?? "—"}`;
  return (
    <SettingRow title={adapter.display_name}>
      {/* 未安装就没有「认没认上」这回事：auth 恒为 unknown，是
          binary_not_found 的必然结果而不是第二个事实。 */}
      <span title={hint}>
        <StatusDot
          tone={!adapter.installed ? "idle" : adapter.auth_status === "authorized" ? "ok" : adapter.auth_status === "unauthorized" ? "bad" : "idle"}
          label={label}
        />
      </span>
    </SettingRow>
  );
}

function AgentsCategory({
  probe,
  probeFailure,
  kinds,
  agentsPhase,
  agentsError,
  runtimeObservable,
  rosterSize,
}: {
  probe: CodingAgentsProbe | null;
  probeFailure: string | null;
  kinds: RuntimeKind[];
  agentsPhase: string;
  agentsError: string | null;
  /** 拿到 Controller 观测值的成员数（= 「连接健康」里的「可达」）。 */
  runtimeObservable: number;
  /** 花名册成员总数。 */
  rosterSize: number;
}) {
  return (
    <>
      <CategoryTitle>智能体</CategoryTitle>
      <SettingRow
        title="运行时种类"
        note={
          agentsPhase === "loading"
            ? "探测中…"
            : kinds.length === 0 && !agentsError && rosterSize > 0
              ? // 用户原话："运行时种类 无回报" —— 光写"无回报"等于没说。两种成因要分开：
                //  · 有观测值、但回报的 runtime_kind 是空的 → 是**上游没回报种类**；
                //  · 一个观测值都没有 → 与「平台 · 连接健康」的「可达 0」是同一件事。
                runtimeObservable > 0
                ? `有 ${runtimeObservable} 个成员拿到了 Controller 观测值，但它们回报的 runtime_kind 是空的 —— 上游没有给出种类，不是探测失败，也不是这里没取。`
                : `种类取自**拿到 Controller 观测值的**成员；当前花名册 ${rosterSize} 个成员里 0 个有观测值（见「平台 · 连接健康」）—— 所以这里没有可回报的种类，不是探测失败。`
              : undefined
        }
      >
        {kinds.length > 0 ? (
          <span className="flex flex-wrap justify-end gap-1.5">
            {kinds.map((kind) => (
              <span key={kind} className="rounded-hard border border-bluegray px-2 py-px font-mono text-[11px] text-bluegray">
                {kind}
              </span>
            ))}
          </span>
        ) : (
          <StatusDot tone="idle" label={agentsError ? "取用失败" : "无回报"} />
        )}
      </SettingRow>

      <h3 className="pb-1 pt-5 text-[11px] font-semibold tracking-widest text-tx3 uppercase">
        Coding Agent 适配器
      </h3>
      {/* 探测口径必须写在脸上：这些结论是**在 API 进程所在的环境里**得到的，
          而 agent 实际跑在 host-executor 里。不写清这一点，"Codex CLI 无法判定"
          就会被读成"codex 坏了"，而事实往往是"这台机器上装了、但没验过授权态"。 */}
      {probe !== null && (
        <p className="pb-1 text-[10.5px] leading-[1.7] text-tx3">
          探测环境：<span className="font-mono">{probe.environment}</span> · {probe.note}
        </p>
      )}
      {probeFailure ? (
        <p className="py-2 text-[11.5px] text-salmon">适配器探测取用失败：{probeFailure}</p>
      ) : probe === null ? (
        <p className="py-2 text-[11.5px] text-tx3">探测中…</p>
      ) : probe.adapters.length === 0 ? (
        <p className="py-2 text-[11.5px] text-tx3">注册表里没有适配器清单。</p>
      ) : (
        <>
          {probe.adapters.map((adapter) => (
            <AdapterRow key={adapter.adapter_id} adapter={adapter} />
          ))}
        </>
      )}
    </>
  );
}

/* ── 关于 ────────────────────────────────────────────────────────────────── */

function AboutCategory({ account, base }: { account: Account; base: string }) {
  return (
    <>
      <CategoryTitle>关于</CategoryTitle>
      <SettingRow title="版本">
        <RowValue>{__APP_VERSION__}</RowValue>
      </SettingRow>
      <SettingRow title="数据源">
        <RowValue>{gridSourceMode() === "live" ? "live（真实读模型）" : "replay（夹具回放）"}</RowValue>
      </SettingRow>
      <SettingRow title="API 地址">
        <RowValue>{base === "" ? "同源（经代理 /api）" : base}</RowValue>
      </SettingRow>
      <SettingRow title="登录身份">
        <RowValue>
          {account.username} · {account.is_admin ? "管理员" : "个人账号"}
        </RowValue>
      </SettingRow>
    </>
  );
}

/* ── 页面骨架：左侧五类导航 + 右侧内容 ───────────────────────────────────── */

export function SettingsPage({
  account,
  projectId = null,
  projectName,
  onConfigure,
  initialCategory = "general",
  onToast = () => undefined,
  onOpenIssue = () => undefined,
}: {
  account: Account;
  onConfigure: () => void;
  projectId?: string | null;
  projectName?: string;
  /** 深链（#/settings/<section>）落到对应分类；仅挂载时生效 */
  initialCategory?: CategoryKey;
  /** 收编进来的智能体/技能子页要用的两个回调（由外壳注入） */
  onToast?: (text: string) => void;
  onOpenIssue?: (issueId: string) => void;
}) {
  const [category, setCategory] = useState<CategoryKey>(initialCategory);
  const fetcher = useCallback((withRuntime: boolean) => fetchConsoleAgents(withRuntime), []);
  const { rows, error, phase, probeError } = useRuntimeRows<ConsoleAgentView>(fetcher);

  // 就绪检查与适配器探测各自取各自的：一个失败不该把另一个也变成空白。
  const [setup, setSetup] = useState<SetupStatusView | null>(null);
  const [setupError, setSetupError] = useState<string | null>(null);
  /** 后端自报版本（`/healthz`）。与构建期注入的 `__APP_VERSION__` **分开显示**：
   *  两者不一致本身就是一条值得看见的事实（前端产物没换 / 后端换了，或反过来）。 */
  const [backendVersion, setBackendVersion] = useState<string | null>(null);
  const [probe, setProbe] = useState<CodingAgentsProbe | null>(null);
  const [probeFailure, setProbeFailure] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    fetchSetupStatus()
      .then((view) => !cancelled && setSetup(view))
      .catch((err: unknown) => !cancelled && setSetupError(errText(err)));
    fetchCodingAgents()
      .then((view) => !cancelled && setProbe(view))
      .catch((err: unknown) => !cancelled && setProbeFailure(errText(err)));
    fetchBackendVersion().then((v) => !cancelled && setBackendVersion(v));
    return () => {
      cancelled = true;
    };
  }, []);

  // 三态分别计数——合成一个「N 个健康」会把「没有这个资源」说成「不健康」
  const reachable = rows?.filter((a) => a.runtime !== null && a.runtime.reachable).length ?? 0;
  const unreachable = rows?.filter((a) => a.runtime !== null && !a.runtime.reachable).length ?? 0;
  const absent = rows?.filter((a) => a.runtime === null).length ?? 0;
  // 三态各自的**是谁** —— 只给计数等于让人拿着"不可达 1"去花名册里猜。
  // 用户原话："这个信息有什么用" —— 有用之处就在于能立刻指出是哪个成员、哪种态。
  //
  // ⚠️ 标识不能只取 `agentteams_resource_name`：线上实测「无事实」那 5 行的
  // **资源名就是空字符串**（后端拿空名去探上游，自然无事实 —— 它们是本地花名册里
  // 有、上游从未建过资源的行）。只取它的话会渲染成「无事实：、、、、」。
  // 回退到角色又会**重名**（实测渲染出 `leader、manager、manager、worker、worker`，
  // 还是认不出是谁），所以角色后面缀上 agent_id 末 4 位 —— 短、但唯一可辨。
  const labelOf = (a: ConsoleAgentView) => {
    if (a.agentteams_resource_name) return a.agentteams_resource_name;
    if (a.repository_name) return a.repository_name;
    const shortID = (a.agent_id ?? "").slice(-4);
    return shortID ? `${a.role || "agent"}·${shortID}` : a.role || "agent";
  };
  const unreachableNames = (rows ?? [])
    .filter((a) => a.runtime !== null && !a.runtime.reachable)
    .map(labelOf);
  const absentNames = (rows ?? [])
    .filter((a) => a.runtime === null)
    .map(labelOf);

  const kinds = [
    ...new Set(
      (rows ?? [])
        .map((a) => (a.runtime !== null && a.runtime.reachable ? a.runtime.runtime_kind : null))
        // ⚠️ **空串也要滤掉**。线上实测（DOM 取证）：那唯一一个"可达"的成员回报的
        // `runtime_kind` 是**空字符串**（不是 null），只滤 null 的话 kinds = [""] →
        // 界面上渲染出一个**空白胶囊**（有边框、没字）。看起来像"无回报"，
        // 其实"有值、值为空" —— 用户报的「运行时种类 无回报」根子在这。
        .filter((k): k is RuntimeKind => typeof k === "string" && k.length > 0),
    ),
  ];

  const base = import.meta.env.VITE_API_BASE ?? "";

  return (
    <div className="flex items-start gap-8">
      {/* 左侧分类导航：滚动时钉在内容区顶部（Trae 的常驻导航栏） */}
      <nav className="w-[176px] flex-none">
        <div className="sticky top-5">
          <h1 className="pb-1 text-[16px] font-semibold text-cream">设置</h1>
          <ul className="grid gap-0.5 pt-2">
            {CATEGORIES.map((item) => {
              const Icon = item.icon;
              return (
                <li key={item.key}>
                  <button
                    type="button"
                    aria-current={category === item.key ? "true" : undefined}
                    onClick={() => setCategory(item.key)}
                    className={`flex w-full items-center gap-2.5 rounded-hard border-l-2 px-2.5 py-1.5 text-left transition-colors ${
                      category === item.key
                        ? "border-amber bg-well text-tx"
                        : "border-transparent text-tx2 hover:bg-well/60 hover:text-tx"
                    }`}
                  >
                    <Icon
                      className={`size-[15px] flex-none ${
                        category === item.key ? "text-amber" : "text-tx3"
                      }`}
                    />
                    <span className="text-[12.5px]">{item.label}</span>
                  </button>
                </li>
              );
            })}
          </ul>
        </div>
      </nav>

      {/* 右侧内容区：只渲染当前分类 */}
      <div className="min-w-0 max-w-[720px] flex-1 pb-6">
        {category === "general" && <GeneralCategory />}
        {category === "account" && (
          <>
            <CategoryTitle>账号与权限</CategoryTitle>
            <GitHubConnectionRow onToast={onToast} />
            {/* GitHub App 授权（2026-09-20）：**常驻入口**——全站别处（外壳顶部那张卡、
                仓库页的就地提示）只在有缺口时出现，而「到底装没装、覆盖到哪几个仓」
                必须有个地方随时能查，所以这里 showWhenReady。
                respectDismissal=false：首页点过「稍后再说」不该把查状态的入口也关掉。 */}
            <h3 className="pb-1 pt-5 text-[11px] font-semibold tracking-widest text-tx3 uppercase">
              GitHub App 授权
            </h3>
            <AppInstallGuide variant="card" showWhenReady respectDismissal={false} />
            <LocalAccountsPanel account={account} />
          </>
        )}
        {category === "models" && (
          <>
            <ModelUsageSettings isAdmin={account.is_admin} />
            <TypeSafeSettings key={`${account.id}:${projectId}`} projectId={projectId} projectName={projectName} />
            {/* 供应商目录（模型来源 / Key / 连通性测试）从侧栏顶级入口收编到这里：
                侧栏不再单列「模型」，模型配置面收在设置里（2026-09-20 用户裁定）。 */}
            <div id="model-providers" className="mt-6 border-t border-line pt-4">
              <ModelProvidersPage embedded />
            </div>
          </>
        )}
        {category === "platform" && (
          <>
          <PlatformCategory
            setup={setup}
            setupError={setupError}
            account={account}
            base={base}
            onConfigure={onConfigure}
            backendVersion={backendVersion}
            controller={{
              loading: rows === null && !error,
              value:
                phase === "loading"
                  ? "探测中…"
                  : error
                    ? "花名册取用失败，无法观测"
                    : phase === "failed"
                      ? "探测请求失败"
                      : `可达 ${reachable} · 不可达 ${unreachable} · 无事实 ${absent}`,
              note:
                phase === "failed" && probeError
                  ? probeError.slice(0, 90)
                  : unreachable > 0
                    ? `不可达：${unreachableNames.join("、")}（探测失败或超时 —— 契约规定这是**降级**：HTTP 仍 200、持久化花名册不受影响，不等于团队故障）`
                    : absent > 0
                      ? `无事实：${absentNames.slice(0, 6).join("、")}${absentNames.length > 6 ? ` 等 ${absentNames.length} 个` : ""}（AgentTeams 未配置，或 Controller 报 404 —— 本地花名册有这一行、上游没有对应资源）`
                      : "花名册里的每个成员都拿到了 Controller 观测值",
            }}
          />
          {/* 监管策略（2026-09-21 用户要求搬进设置）：此前只有 issue 工作台的发现链
              卡片一个入口，想调人工审核强度得先找一个还没物化的 issue 点进去。 */}
          <CategoryTitle>监管策略</CategoryTitle>
          <SupervisionPolicySettings
            key={`policy:${projectId ?? "none"}`}
            projectId={projectId}
            projectName={projectName}
            onToast={onToast}
          />
          </>
        )}
        {category === "agents" && (
          <>
            <AgentsCategory
              probe={probe}
              probeFailure={probeFailure}
              kinds={kinds}
              agentsPhase={phase}
              agentsError={error}
              runtimeObservable={reachable}
              rosterSize={rows?.length ?? 0}
            />
            {/* 智能体花名册与写面（新建/删除）从侧栏顶级入口收编到这里 */}
            <div className="mt-6 border-t border-line pt-4">
              <AgentsPage onOpenIssue={onOpenIssue} embedded />
            </div>
          </>
        )}
        {category === "skills" && <SkillsPage onToast={onToast} embedded />}
        {category === "localcli" && <LocalCliPage embedded />}
        {category === "about" && <AboutCategory account={account} base={base} />}
      </div>
    </div>
  );
}
