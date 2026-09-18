import { useCallback, useEffect, useState } from "react";
import type { AgentRole, ConsoleAgentView } from "../api/contract";
import { listAgents, type AgentRosterRow } from "../api/agents";
import { fetchConsoleAgents } from "../api/grid";
import { runtimeDisplay, shortId, type RuntimePhase } from "../display";
import { ErrorPanel, LoadingLine } from "../components/StatusBlocks";
import { useRuntimeRows } from "./useRuntimeRows";

/** 智能体花名册（CONS-44 / 契约 v0.2 §4.3）。
 *
 *  **与原型的三处有意偏离，都是诚实数据要求的**：
 *   1. 原型的状态列写「在岗 / 执行中 / 休眠」——**醒睡态没有数据源**。`awake` 恒 null
 *      （`DesiredRuntimeState` 是我们下发的期望态，不是观测态，拿它冒充即编造）。
 *      本页状态列 = `status` 的启用态（active|disabled，agent_directory 持久化）
 *      + `active_task_count` 派生的在途任务数，两者都是真事实；
 *   2. 原型的「时长」列（3h12m / 22m）**无源**：Controller 响应里没有任何时间字段，
 *      `uptime_seconds` 恒 null。列保留但整列显「未接入」，并在页脚写明补齐路径——
 *      删掉这列会让缺口消失于无形，填上假数字则更糟；
 *   3. 原型的运行时列直接写 `claude-code`；真实情况是 `runtime_kind` 只在探测通时
 *      才有值，不可达/未配置时没有——按 §4.4 三态呈现。确认由 Bridge 接入的成员
 *      （`kind: "external"`）连探通了也没有：平台核实的是「容器不归 Controller 管」，
 *      它跑的是哪个 CLI 平台不观测，所以这一格只写 External，不写任何 CLI 名字。 */

const ROLE_LABEL: Record<AgentRole, string> = {
  organization_leader: "组织 leader",
  repository_leader: "仓库 leader",
  worker: "worker",
};

function AgentRow({
  agent,
  phase,
  onOpenIssue,
}: {
  agent: ConsoleAgentView;
  phase: RuntimePhase;
  onOpenIssue: (issueId: string) => void;
}) {
  const rt = agent.runtime;
  const d = runtimeDisplay(phase, rt);
  const disabled = agent.status === "disabled";
  // runtime_kind 只在探测通时可得；否则复用三态措辞，不留空也不编
  const runtimeKind = rt !== null && rt.reachable ? rt.runtime_kind : null;

  return (
    <tr className={`border-b border-panel ${disabled ? "text-tx2" : ""}`}>
      <td className="py-2 pr-3 align-top">
        <div className="font-mono text-[12px] text-tx">{agent.agentteams_resource_name}</div>
        <div className="text-[10.5px] text-tx2">{ROLE_LABEL[agent.role] ?? agent.role}</div>
      </td>

      <td className="py-2 pr-3 align-top">
        <span className="flex items-center gap-1.5 text-[11.5px]">
          <i className={`size-1.5 rounded-full ${disabled ? "bg-tx3" : "bg-olive"}`} />
          {disabled ? "已停用" : "已启用"}
        </span>
        {agent.active_task_count > 0 && (
          <div className="mt-px text-[10.5px] text-bluegray">{agent.active_task_count} 个在途任务</div>
        )}
      </td>

      <td className="py-2 pr-3 align-top text-[11.5px] text-tx2">
        <div>{agent.repository_name ?? (agent.role === "organization_leader" ? "组织级" : "未归属仓库")}</div>
        {agent.issue_id ? (
          <button
            className="mt-px font-mono text-[10.5px] text-tx2 hover:text-amber-hi"
            title={`issue ${agent.issue_id}`}
            onClick={() => onOpenIssue(agent.issue_id!)}
          >
            #{shortId(agent.issue_id)}
          </button>
        ) : (
          <div className="mt-px text-[10.5px] text-tx3">未驻扎团队</div>
        )}
      </td>

      <td className="py-2 pr-3 align-top">
        {/* external 不与降级三态同色：那三种是「没取到观测值」，它是**核实过的
            事实**，涂成同一档中性会让人以为运行时列又没读到东西 */}
        <div
          className={`font-mono text-[11.5px] ${d.kind === "external" ? "text-kraft" : "text-tx2"}`}
          title={d.hint}
        >
          {runtimeKind ?? d.label}
        </div>
        {runtimeKind && <div className="text-[10.5px] text-bluegray">{d.label}</div>}
      </td>

      {/* §4.4：uptime_seconds 恒 null，整列如实为「未接入」 */}
      <td className="py-2 text-right align-top text-[11.5px] text-tx3">未接入</td>
    </tr>
  );
}

export function AgentsPage({ onOpenIssue }: { onOpenIssue: (issueId: string) => void }) {
  const fetcher = useCallback((withRuntime: boolean) => fetchConsoleAgents(withRuntime), []);
  const { rows, error, phase, retry } = useRuntimeRows<ConsoleAgentView>(fetcher);

  /* console 族(/console/agents)后端未落地:404 时回退真名册
   * (GET /api/agents,agent_directory 池子)——真实数据,非夹具;
   * 运行时探测列在名册视图无来源,按「缺失字段直接隐藏」处理。 */
  const [roster, setRoster] = useState<AgentRosterRow[] | null>(null);
  useEffect(() => {
    if (!error || !error.includes("404") || roster !== null) return;
    let cancelled = false;
    listAgents()
      .then((rosterRows) => {
        if (cancelled) return;
        setRoster(rosterRows);
      })
      .catch(() => {
        if (cancelled) return;
        setRoster([]);
      });
    return () => {
      cancelled = true;
    };
  }, [error, roster]);

  const enabled = rows?.filter((a) => a.status === "active").length ?? 0;
  const busy = rows?.filter((a) => a.active_task_count > 0).length ?? 0;

  return (
    <div className="max-w-[860px]">
      <div className="flex items-baseline gap-3 border-b border-line pb-3">
        <h1 className="text-[16px] font-semibold text-cream">智能体</h1>
        {rows && (
          <span className="text-[11.5px] text-tx2">
            {rows.length} 个 · {enabled} 个已启用 · {busy} 个有在途任务
          </span>
        )}
      </div>

      {error && roster !== null ? (
        <>
          <p className="mb-3 text-[11.5px] text-tx3">
            运行时探测(/console/agents)服务端未接入,以下为智能体名册(agent_directory 池子,真实数据)。
          </p>
          <table className="w-full">
            <thead>
              <tr className="border-b border-line text-left">
                <th className="pb-1.5 text-[10.5px] uppercase tracking-[0.12em] text-tx2">智能体</th>
                <th className="pb-1.5 text-[10.5px] uppercase tracking-[0.12em] text-tx2">角色</th>
                <th className="pb-1.5 text-[10.5px] uppercase tracking-[0.12em] text-tx2">状态</th>
                <th className="pb-1.5 text-[10.5px] uppercase tracking-[0.12em] text-tx2">绑定仓库</th>
              </tr>
            </thead>
            <tbody>
              {roster.map((a) => (
                <tr key={a.id} className="border-b border-[#f1f1ef]">
                  <td className="py-2 font-mono text-[11px] text-tx">{a.singletonKey ?? a.id.slice(0, 8)}</td>
                  <td className="py-2 text-tx2">{a.role}</td>
                  <td className="py-2 text-tx2">{a.status}</td>
                  <td className="py-2 text-tx2">{a.repositoryId ?? "—"}</td>
                </tr>
              ))}
              {roster.length === 0 && (
                <tr><td colSpan={4} className="py-6 text-center text-tx3">名册为空——物化/组装后出现第一批智能体。</td></tr>
              )}
            </tbody>
          </table>
        </>
      ) : error ? (
        <ErrorPanel title="花名册加载失败" message={error} onRetry={retry} />
      ) : rows === null ? (
        <LoadingLine />
      ) : rows.length === 0 ? (
        <div className="py-8 text-center text-[12.5px] text-tx3">agent_directory 里还没有注册的智能体</div>
      ) : (
        <table className="mt-3 w-full">
          <thead>
            <tr className="border-b border-line text-left">
              <th className="pb-1.5 text-[10.5px] font-normal tracking-[0.12em] text-tx2 uppercase">智能体</th>
              <th className="pb-1.5 text-[10.5px] font-normal tracking-[0.12em] text-tx2 uppercase">状态</th>
              <th className="pb-1.5 text-[10.5px] font-normal tracking-[0.12em] text-tx2 uppercase">归属</th>
              <th className="pb-1.5 text-[10.5px] font-normal tracking-[0.12em] text-tx2 uppercase">运行时</th>
              <th
                className="pb-1.5 text-right text-[10.5px] font-normal tracking-[0.12em] text-tx2 uppercase"
                title="uptime_seconds 恒 null：Controller 响应里没有任何时间字段，与运行时是否可达无关"
              >
                时长
              </th>
            </tr>
          </thead>
          <tbody>
            {rows.map((agent) => (
              <AgentRow key={agent.agent_id} agent={agent} phase={phase} onOpenIssue={onOpenIssue} />
            ))}
          </tbody>
        </table>
      )}

    </div>
  );
}
