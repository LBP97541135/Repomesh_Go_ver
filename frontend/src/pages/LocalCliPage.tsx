import { useEffect, useRef, useState } from "react";
import { ChevronDown } from "lucide-react";
import type { ExternalMemberReadinessView } from "../api/contract";
import { defaultClient } from "../api/client";
import {
  LAUNCHER_BASE,
  getRoster,
  getSecrets,
  probe,
  restartMember,
  stalePidFile,
  startMembers,
  stopMembers,
  type LauncherMember,
  type LauncherProbe,
  type RosterDocument,
  type SecretEntry,
  type StalePidFileDetail,
} from "../api/launcher";
import { ConnectionConfigSection, RosterSection, SecretsSection } from "../components/LocalCliConfig";
import { READINESS_LABEL, READINESS_SKIN, errText, eventTime, shortId } from "../display";
import { StalePidBlock } from "../components/StatusBlocks";

/** 本地 CLI 页（Task 5；2026-09-20 按全站风格重排）。
 *
 *  **三段式（用户裁定）**：① 连接状态卡（状态徽章 + 启停按钮，永远可操作）→
 *  ② 成员列表（卡片化行）→ ③ 配置区（连接/密钥/花名册，**始终显示**——编辑走
 *  RepoMesh 保存接口，不依赖启动器；此前被「启动器已连接」挡住，未连接时整页
 *  只剩说明文字，是「没有可操作的地方」的根因）→ ④ 命令行与参考（默认折叠）。
 *
 *  **两个数据源，两列并排，不合成一列。**「进程在跑」由本机启动器说（它数 PID 文件
 *  与进程），「成员就绪」由 RepoMesh 的租约说（Bridge 每 15 秒续一次，45 秒过期）。
 *  这两者**经常且合法地不一致**：进程刚起来还没续上第一次租约、进程还在但 Bridge
 *  卡住不再上报、进程被杀而租约还剩十几秒。物化门只认租约，可要重启的是进程。
 *
 *  **命令卡片一格都不删**（收进折叠区）。启动器是本机的一个可选进程：没装、没起、
 *  Origin 不在白名单，都是常态而不是故障，此时页面退回到命令行那条路。 */

const START_COMMAND = "powershell -NoProfile -File .\\scripts\\start-local-cli.ps1";
const DRY_RUN_COMMAND = `${START_COMMAND} -DryRun`;
const STOP_COMMAND =
  "powershell -NoProfile -File .\\scripts\\bridge-e1\\stop_members.ps1 " +
  "-Members .\\scripts\\bridge-e1\\members.json " +
  "-PidDir .\\output\\bridge-team\\e1\\pids";

/** 轮询间隔。取 5 秒与房间流同一档：租约 TTL 45 秒、续期 15 秒一次，5 秒足够让
 *  「刚起来」与「刚掉线」在这页上看得见，也不至于把一台本机进程问出压力。 */
const POLL_MS = 5000;

const PILL =
  "rounded-hard border px-1.5 py-px text-[10.5px] whitespace-nowrap";
const PILL_OK = `${PILL} border-olive/60 bg-olive/10 text-olive`;
const PILL_IDLE = `${PILL} border-line text-tx3`;

function CommandCard({
  title,
  command,
  note,
}: {
  title: string;
  command: string;
  note: string;
}) {
  const [copyState, setCopyState] = useState<"idle" | "copied" | "failed">("idle");

  const copy = () => {
    navigator.clipboard
      .writeText(command)
      .then(() => {
        setCopyState("copied");
        window.setTimeout(() => setCopyState("idle"), 1800);
      })
      .catch(() => setCopyState("failed"));
  };

  return (
    <div className="rounded-hard border border-line bg-panel px-3 py-3">
      <div className="mb-2 flex items-center justify-between gap-3">
        <div>
          <div className="text-[12.5px] font-semibold text-cream">{title}</div>
          <div className="mt-px text-[10.5px] text-tx3">{note}</div>
        </div>
        <button
          className="flex-none rounded-hard border border-amber/60 px-2 py-1 text-[11px] text-amber hover:bg-amber/10 hover:text-amber-hi"
          onClick={copy}
        >
          {copyState === "copied" ? "已复制" : copyState === "failed" ? "复制失败" : "复制命令"}
        </button>
      </div>
      <pre className="overflow-x-auto rounded-hard border border-line bg-ink-deep px-3 py-2 font-mono text-[11px] leading-5 text-tx">
        <code>{command}</code>
      </pre>
    </div>
  );
}

function Requirement({ children }: { children: React.ReactNode }) {
  return (
    <li className="flex gap-2 border-b border-panel py-2 text-[11.5px] text-tx2">
      <span className="mt-[2px] text-olive">◆</span>
      <span>{children}</span>
    </li>
  );
}

/** 一行成员（卡片化行）。就绪那一格有**三种**空，不能糊成一种：
 *   - `known === false`：租约表这一轮（且此前从未）没取到——界面对这个成员的就绪
 *     一无所知。写「未上报」就是拿一次自己的取数失败去指控一个可能好好的成员，
 *     一个配错的动作 token 会让整列对着六个健康成员说它们没上报；
 *   - `known && readiness === null`：取到了，表里没有这个 agent——它确实从没上报过
 *     （或 RepoMesh 不认这个 id）；
 *   - 有值：服务端派生的三态照原样显示。 */
function MemberRow({
  member,
  readiness,
  known,
  busy,
  onRestart,
}: {
  member: LauncherMember;
  readiness: ExternalMemberReadinessView | null;
  known: boolean;
  busy: boolean;
  onRestart: () => void;
}) {
  /** 就绪未知时**只按进程判**：拿一个取不到的事实去点亮每一行的「重启」，
   *  是把「我不知道」说成「都坏了」。 */
  const healthy = known ? member.running && readiness?.status === "ready" : member.running;

  return (
    <div className="flex items-start gap-3 rounded-[8px] border border-panel bg-well/40 px-3 py-2.5">
      <i
        className={`mt-[5px] size-2 flex-none rounded-full ${member.running ? "bg-olive" : "bg-tx3"}`}
        title={member.running ? "启动器看到进程" : "启动器没有看到进程"}
      />
      <div className="min-w-0 flex-1">
        <div className="truncate font-mono text-[12px] text-tx">
          {member.displayName}
          <span className="ml-2 text-[10.5px] text-tx3">
            {member.role} · {shortId(member.agentId)}
          </span>
        </div>
        {/* 日志路径挂 title 而不占一列：它是绝对路径，排进来会把行挤没，
            但成员起不来时它就是下一步要看的东西 */}
        <div
          className="mt-px font-mono text-[10.5px] text-tx3"
          title={member.logPath ?? "启动器未记录日志路径"}
        >
          {member.running ? "运行中" : "未运行"}
          {member.pid === null ? " · 无 PID 文件" : ` · PID ${member.pid}`}
        </div>
      </div>

      <div className="mt-px flex-none text-right">
        {!known ? (
          <span className={PILL_IDLE} title="租约状态这一轮没取到（原因见页底）——这不是对该成员的判断">
            未能获取
          </span>
        ) : readiness === null ? (
          <span className={PILL_IDLE}>未上报</span>
        ) : (
          <>
            <span className={`${PILL} ${READINESS_SKIN[readiness.status]}`}>
              {READINESS_LABEL[readiness.status]}
            </span>
            <div className="mt-px font-mono text-[10px] text-tx3">
              {readiness.stoppedAt === null
                ? `到期 ${eventTime(readiness.expiresAt)}`
                : `已报停止 ${eventTime(readiness.stoppedAt)}`}
            </div>
          </>
        )}
      </div>

      <div className="mt-px w-[56px] flex-none text-right">
        {/* 「进程在跑但租约不 ready」也给重启：那正是 Bridge 还活着却不再上报的形态，
            而物化门只认租约，光看进程列会以为没事 */}
        {healthy ? (
          <span className="text-[10.5px] text-tx3">—</span>
        ) : (
          <button
            className="rounded-hard border border-amber/60 px-2 py-1 text-[11px] text-amber hover:bg-amber/10 hover:text-amber-hi disabled:cursor-not-allowed disabled:opacity-40"
            disabled={busy}
            onClick={onRestart}
          >
            重启
          </button>
        )}
      </div>
    </div>
  );
}

/** `embedded`：收进设置页「本地 CLI」分类时为 true——去掉页面级大标题与外框，
 *  分类标题由设置页提供；独立路由（#/settings/local-cli 旧链接）仍带头部。 */
export function LocalCliPage({ embedded = false }: { embedded?: boolean }) {
  /** null = 首次探测还没回来。三态本身在 `api/launcher.ts` 的 `LauncherProbe`。 */
  const [launcher, setLauncher] = useState<LauncherProbe | null>(null);
  const [readiness, setReadiness] = useState<ExternalMemberReadinessView[] | null>(null);
  const [readinessError, setReadinessError] = useState<string | null>(null);
  const [tick, setTick] = useState(0);
  /** 在途写操作的键："start" / "stop" / 某个 agentId。同时只允许一个。 */
  const [busy, setBusy] = useState<string | null>(null);
  const [opError, setOpError] = useState<string | null>(null);
  const [blocked, setBlocked] = useState<StalePidFileDetail | null>(null);
  const [lastRefresh, setLastRefresh] = useState<Date | null>(null);

  // 配置文档（roster + 密钥掩码）：只在挂载与保存后取，不跟 5s 轮询——
  // 它们是操作者编辑的对象，不该在打字时被轮询重置
  const [roster, setRoster] = useState<RosterDocument | null>(null);
  const [secrets, setSecrets] = useState<SecretEntry[] | null>(null);
  const [configError, setConfigError] = useState<string | null>(null);
  const [configTick, setConfigTick] = useState(0);
  const [showCommands, setShowCommands] = useState(false);
  const commandsRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    let cancelled = false;
    Promise.all([getRoster(), getSecrets()])
      .then(([rosterRes, secretsRes]) => {
        if (cancelled) return;
        setRoster(rosterRes.document);
        setSecrets(secretsRes.entries);
        setConfigError(null);
      })
      .catch((err: unknown) => {
        if (!cancelled) setConfigError(errText(err));
      });
    return () => {
      cancelled = true;
    };
  }, [configTick]);

  const reloadConfig = () => {
    setConfigTick((n) => n + 1);
    setTick((n) => n + 1);
  };

  // 5s 心跳（同 observe 各页与房间流的写法）
  useEffect(() => {
    const t = window.setInterval(() => setTick((n) => n + 1), POLL_MS);
    return () => window.clearInterval(t);
  }, []);

  // 两个源同一拍取：并排的两列必须来自同一次刷新，否则它们的不一致有一半是取数时差
  useEffect(() => {
    let cancelled = false;
    probe().then((result) => {
      if (cancelled) return;
      setLauncher(result);
      setLastRefresh(new Date());
    });
    defaultClient()
      .getExternalMemberReadiness()
      .then((page) => {
        if (cancelled) return;
        setReadiness(page.members);
        setReadinessError(null);
      })
      // 刷新失败保留上一轮的行，只标注这一轮没取到——清空会让整列在一次瞬时失败里消失
      .catch((err: unknown) => !cancelled && setReadinessError(errText(err)));
    return () => {
      cancelled = true;
    };
  }, [tick]);

  /** 三个写操作共用一条路：置忙 → 清上一次的拒绝 → 成功就重取两个源。
   *  **不消费返回体**（它只有进程事实，没有租约），一律等下一拍的两个源。 */
  const run = (key: string, operation: () => Promise<unknown>) => {
    setBusy(key);
    setOpError(null);
    setBlocked(null);
    operation()
      .then(() => setTick((n) => n + 1))
      .catch((err: unknown) => {
        setBlocked(stalePidFile(err));
        setOpError(errText(err));
      })
      .finally(() => setBusy(null));
  };

  const status = launcher?.kind === "ok" ? launcher.status : null;
  /** 至少收到过一份租约表。**这一位就是「未能获取」与「未上报」的分界**：没有它，
   *  一个配错的动作 token 会让整列理直气壮地说六个健康成员都没上报过。 */
  const readinessKnown = readiness !== null;
  const readinessOf = (agentId: string) => readiness?.find((m) => m.agentId === agentId) ?? null;

  /** 启动器不在时，把人送到命令行那条路（展开折叠区并滚过去），不留死按钮。 */
  const openCommands = () => {
    setShowCommands(true);
    window.setTimeout(() => commandsRef.current?.scrollIntoView({ behavior: "smooth", block: "start" }), 50);
  };

  return (
    <div className={embedded ? "" : "max-w-[860px]"}>
      {!embedded && (
        <div className="flex items-baseline gap-3 border-b border-line pb-3">
          <h1 className="text-[16px] font-semibold text-cream">本地 CLI</h1>
          <span className="microlabel">External · Codex</span>
        </div>
      )}

      {/* ── ① 连接状态卡：无论启动器在不在，这一块都有可操作的出口 ── */}
      <section className="mt-3 rounded-hard border border-line bg-panel px-4 py-3.5">
        <div className="flex flex-wrap items-center gap-2">
          {launcher === null && (
            <span className={PILL_IDLE}>正在探测本机启动器…</span>
          )}
          {launcher?.kind === "ok" && (
            <>
              <span className={PILL_OK}>
                <span className="mr-1 inline-block size-1.5 rounded-full bg-olive align-middle" />
                启动器已连接
              </span>
              <span className="font-mono text-[10.5px] text-tx3">{LAUNCHER_BASE}</span>
            </>
          )}
          {launcher?.kind === "launcher_unavailable" && (
            <>
              <span className={`${PILL} border-amber/60 bg-amber-well text-amber`}>
                <span className="mr-1 inline-block size-1.5 rounded-full bg-amber align-middle" />
                启动器未连接
              </span>
              <span className="font-mono text-[10.5px] text-tx3">{LAUNCHER_BASE}</span>
            </>
          )}
          {launcher?.kind === "refused" && (
            <span className={`${PILL} border-salmon/60 bg-salmon/10 text-salmon`}>启动器报错</span>
          )}

          <span className="ml-auto flex items-center gap-2 text-[10.5px] text-tx3">
            {lastRefresh && <span>刷新于 {lastRefresh.toLocaleTimeString()}</span>}
            <span>每 {POLL_MS / 1000} 秒自动</span>
            <button
              type="button"
              className="rounded-hard border border-line px-2 py-px text-[11px] text-tx2 transition-colors hover:border-amber hover:text-amber-hi"
              onClick={() => setTick((n) => n + 1)}
            >
              刷新
            </button>
          </span>
        </div>

        {/* 摘要徽章：进程事实与租约事实各一枚，不合并 */}
        {status && (
          <div className="mt-2.5 flex flex-wrap items-center gap-2">
            <span className={PILL_IDLE}>
              进程 {status.members.filter((m) => m.running).length}/{status.members.length} 运行
            </span>
            {readiness !== null && (
              <span className={PILL_IDLE}>
                就绪 {readiness.filter((r) => r.status === "ready").length}/{readiness.length}
              </span>
            )}
          </div>
        )}

        {/* 未连接的两个常态：给原因与出路，不给故障脸 */}
        {launcher?.kind === "launcher_unavailable" && (
          <p className="mt-2.5 text-[11.5px] leading-[1.7] text-tx2">
            {LAUNCHER_BASE} 没有应答：启动器未运行，或控制台地址不在其 allowedOrigins 白名单里。
            可以用本页命令行入口启动，配置编辑不受影响。
            <span className="mt-0.5 block font-mono text-[10.5px] break-all text-tx3">{launcher.message}</span>
          </p>
        )}
        {launcher?.kind === "refused" && (
          <p className="mt-2.5 text-[11.5px] leading-[1.7] text-salmon">
            启动器返回了错误，原文如下：
            <span className="mt-0.5 block font-mono text-[10.5px] break-all text-tx3">{launcher.message}</span>
          </p>
        )}

        {/* 操作行：连接时是启停；未连接时把人送去命令行入口——这一块永远有按钮可点 */}
        <div className="mt-3 flex flex-wrap items-center gap-2">
          {status && (
            <>
              <button
                className="rounded-hard bg-amber px-4 py-2 text-[12.5px] font-extrabold text-on-amber hover:bg-amber-hi disabled:cursor-not-allowed disabled:opacity-40"
                disabled={busy !== null}
                onClick={() => run("start", startMembers)}
              >
                {busy === "start" ? "启动中…" : "启动全部成员"}
              </button>
              <button
                className="rounded-hard border border-line px-3 py-2 text-[12.5px] text-tx hover:border-amber hover:text-amber-hi disabled:cursor-not-allowed disabled:opacity-40"
                disabled={busy !== null}
                onClick={() => run("stop", stopMembers)}
              >
                {busy === "stop" ? "停止中…" : "停止全部"}
              </button>
            </>
          )}
          {launcher?.kind === "launcher_unavailable" && (
            <button
              className="rounded-hard border border-amber/60 bg-amber-well px-3 py-2 text-[12px] font-semibold text-amber hover:bg-amber-well/80"
              onClick={openCommands}
            >
              查看命令行启动方式
            </button>
          )}
        </div>

        {blocked && <StalePidBlock detail={blocked} />}

        {opError && !blocked && (
          // 启动器 detail 原文。404（重启一个它不认的成员）、连不上（没起，或来源不在
          // 白名单——写请求带自定义头，那一趟被拦在预检，连发都没发出去）各是一件不同的
          // 事，归并成「操作失败」会把可自助解决的配置问题说成故障
          <div className="mt-3 border-l-2 border-salmon bg-salmon-well px-3 py-2 text-[12px] leading-[1.7] break-words text-salmon-hi">
            <b className="mr-1.5 font-mono tracking-[0.08em]">启动器拒绝</b>
            {opError}
          </div>
        )}
      </section>

      {/* ── ② 成员（卡片化行）── */}
      {status && (
        <section className="mt-4 rounded-hard border border-line bg-panel px-4 py-3.5">
          <div className="mb-2.5 flex items-baseline justify-between gap-3">
            <span className="eyebrow">
              成员 <span className="font-mono normal-case">roster {status.rosterVersion}</span>
            </span>
            <span className="text-[10.5px] text-tx3">每 {POLL_MS / 1000} 秒刷新</span>
          </div>

          {status.members.length === 0 ? (
            <p className="py-4 text-center text-[12px] text-tx3">
              启动器的 roster 里没有成员（config 的 subset 过滤掉了全部条目？）
            </p>
          ) : (
            <div className="flex flex-col gap-1.5">
              {status.members.map((member) => (
                <MemberRow
                  key={member.agentId}
                  member={member}
                  readiness={readinessOf(member.agentId)}
                  known={readinessKnown}
                  busy={busy !== null}
                  onRestart={() => run(member.agentId, () => restartMember(member.agentId))}
                />
              ))}
            </div>
          )}

          <p className="pt-2.5 text-[10.5px] leading-[1.7] text-tx3">
            「进程」与「就绪」可能短暂不一致，物化门只认<b className="text-tx2">就绪</b>。
            {readinessError && ` 就绪取用失败：${readinessError.slice(0, 80)}`}
          </p>
        </section>
      )}

      {/* ── ③ 配置：连接/密钥/花名册。编辑走 RepoMesh 保存接口，不依赖启动器，
             因此**始终显示**（2026-09-20 用户裁定）——未连接不再整块消失 ── */}
      <section className="mt-4">
        {configError && (
          <div className="mb-3 rounded-hard border border-salmon/60 bg-salmon/10 px-3 py-2 text-[11.5px] text-salmon">
            {configError}
          </div>
        )}
        {roster === null || secrets === null ? (
          <div className="rounded-hard border border-line bg-panel px-4 py-5 text-center text-[12px] text-tx3">
            {configError ? "配置读取失败，可在上方重试保存后自动恢复。" : "正在读取连接配置…"}
          </div>
        ) : (
          <>
            <ConnectionConfigSection
              key={`conn-${configTick}`}
              roster={roster}
              onSaved={reloadConfig}
              onError={setConfigError}
            />
            <SecretsSection
              key={`sec-${configTick}`}
              roster={roster}
              secrets={secrets}
              onSaved={reloadConfig}
              onError={setConfigError}
            />
            <RosterSection
              key={`ros-${configTick}`}
              roster={roster}
              onSaved={reloadConfig}
              onError={setConfigError}
            />
          </>
        )}
      </section>

      {/* ── ④ 命令行与参考：默认折叠（2026-09-20 用户裁定），要查再展开 ── */}
      <div ref={commandsRef} className="mt-4">
        <button
          type="button"
          className="flex w-full items-center gap-2 rounded-hard border border-line bg-panel px-4 py-2.5 text-left transition-colors hover:border-amber/60"
          onClick={() => setShowCommands((v) => !v)}
        >
          <span className="eyebrow">命令行与参考</span>
          <span className="text-[10.5px] text-tx3">启动器不在时走这条 · 启动前置 · 默认路径</span>
          <ChevronDown
            size={13}
            strokeWidth={1.5}
            className={`ml-auto flex-none text-tx3 transition-transform ${showCommands ? "" : "-rotate-90"}`}
          />
        </button>
        {showCommands && (
          <div className="mt-3 rounded-hard border border-line bg-panel px-4 py-3.5">
            <p className="mb-2.5 text-[11.5px] text-tx3">在仓库根目录的 PowerShell 中执行：</p>
            <div className="grid gap-3">
              <CommandCard
                title="先预检命令"
                command={DRY_RUN_COMMAND}
                note="不启动 Bridge；展示成员、角色与统一 workspace root。"
              />
              <CommandCard
                title="启动全部本地成员"
                command={START_COMMAND}
                note="每个成员一个隐藏进程；PID 与日志写入 output/bridge-team/e1。"
              />
              <CommandCard
                title="停止全部本地成员"
                command={STOP_COMMAND}
                note="停止前按 PID 和命令行复核进程身份，避免误杀。"
              />
            </div>

            <div className="eyebrow mt-5 mb-1">启动前置</div>
            <ul>
              <Requirement>
                `scripts/bridge-e1/members.json` 已配置，且六个 External member 已完成 provision/binding。
              </Requirement>
              <Requirement>
                `output/bridge-team/e1/enrollments` 中已有对应 enrollment，credential locator 只引用环境变量。
              </Requirement>
              <Requirement>
                `output/bridge-team/e1-members.env` 已包含成员自己的 RepoMesh 与 Matrix token；页面和脚本均不回显值。
              </Requirement>
              <Requirement>
                每个成员的私有 `codex-home` 已有 `auth.json`；Leader 不获得 workspace，Worker 统一使用控制面 workspace root。
              </Requirement>
            </ul>

            <div className="eyebrow mt-5 mb-1.5">默认路径</div>
            <dl className="grid grid-cols-[150px_1fr] gap-x-3 gap-y-2 text-[11.5px]">
              <dt className="text-tx3">Python</dt>
              <dd className="font-mono text-tx">.venv\Scripts\python.exe</dd>
              <dt className="text-tx3">Workspace root</dt>
              <dd className="font-mono text-tx">
                $env:REPOMESH_RUNNER_WORKSPACE_ROOT；未设置时为仓库同级 .repomesh-e1\workspaces
              </dd>
              <dt className="text-tx3">PID / logs</dt>
              <dd className="font-mono text-tx">output\bridge-team\e1\pids / logs</dd>
              <dt className="text-tx3">状态事实</dt>
              <dd className="text-tx2">
                进程列来自本机启动器，就绪列来自 RepoMesh 租约。本页不合成第三种状态。
              </dd>
            </dl>
          </div>
        )}
      </div>
    </div>
  );
}
