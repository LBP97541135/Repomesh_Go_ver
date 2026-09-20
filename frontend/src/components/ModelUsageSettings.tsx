import { ObservationModelSettings } from "./ObservationModelSettings";

export function ModelUsageSettings({ isAdmin }: { isAdmin: boolean }) {
  return <section className="max-w-[760px]">
    <h2 className="text-[13.5px] font-semibold text-cream">模型与 API</h2>
    <p className="mt-2 text-[11.5px] leading-relaxed text-tx2">在这里维护模型和 API Key：Jev 用于评分，DeepSeek 用于观测分析；下方的模型供应商用于仓库分析与项目执行。</p>
    <p className="mt-2 text-[11px] text-tx3">这里维护 Web 所在主机的观测配置；你电脑上单独运行的工作台，仍在它自己的设置页配置。</p>
    <ObservationModelSettings isAdmin={isAdmin} />
  </section>;
}
