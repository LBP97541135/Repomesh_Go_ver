import { Activity, ArrowUpRight, Bot, GitBranch } from "lucide-react";
import { observeWorkbenchURL } from "../runtimeConfig";

/** Usage labels describe existing consumers; this is not a new model binding. */
export function ModelUsageSettings() {
  let evaluationURL = "";
  let addressError = "";
  try { evaluationURL = observeWorkbenchURL("settings"); }
  catch (error) { addressError = error instanceof Error ? error.message : "工作台地址无效"; }
  const linkClass = "mt-4 inline-flex items-center gap-1 text-[12px] text-amber hover:text-amber-hi";
  return (
    <section className="max-w-[760px]">
      <h2 className="text-[13.5px] font-semibold text-cream">模型与 API</h2>
      <p className="mt-2 text-[11.5px] leading-relaxed text-tx2">
        按用途找到对应的 API 地址、模型名和 API Key 配置。仓库分析与项目执行可引用模型供应商；观测评估在本地工作台单独配置。
      </p>
      <div className="mt-5 grid gap-4">
        <article className="rounded-hard border border-line bg-panel p-5">
          <h3 className="flex items-center gap-2 text-[13px] font-semibold text-cream"><GitBranch size={16} />仓库分析用模型</h3>
          <p className="mt-2 text-[11.5px] leading-relaxed text-tx2">用于需求与仓库名片的语义匹配、候选仓库召回。API 地址和 API Key 来自 RepoMesh 的模型供应商配置。</p>
          <p className="mt-2 text-[11px] text-tx3">未配置可用模型时，仓库发现会退回关键词分析。</p>
          <a href="#/models" className={linkClass}>配置仓库分析 API / Key →</a>
        </article>
        <article className="rounded-hard border border-line bg-panel p-5">
          <h3 className="flex items-center gap-2 text-[13px] font-semibold text-cream"><Bot size={16} />AgentTeams 用模型</h3>
          <p className="mt-2 text-[11.5px] leading-relaxed text-tx2">用于团队智能体的规划与执行。模型供应商保存 API 地址和 Key，项目执行配置决定实际采用的模型。</p>
          <p className="mt-2 text-[11px] text-tx3">保存供应商不会自动切换团队模型。原生 DSH 执行接入尚未完成。</p>
          <a href="#/models" className={linkClass}>管理 AgentTeams 模型来源 →</a>
        </article>
        <article className="rounded-hard border border-line bg-panel p-5">
          <h3 className="flex items-center gap-2 text-[13px] font-semibold text-cream"><Activity size={16} />观测评估用模型</h3>
          <p className="mt-2 text-[11.5px] leading-relaxed text-tx2">用于本地验收报告的 AI 契约评审。DeepSeek 模型和 API Key 保存在本地观测工作台，与仓库分析和团队执行配置分开。</p>
          <p className="mt-2 text-[11px] text-tx3">手动评审时才调用模型；本地 Trace、证据查看和规则验收无需模型 Key。</p>
          {addressError ? <p role="alert" className="mt-3 text-[11px] text-salmon">{addressError}</p> : <a href={evaluationURL} className={linkClass}>配置观测评估 API / Key <ArrowUpRight size={13} /></a>}
        </article>
      </div>
    </section>
  );
}
