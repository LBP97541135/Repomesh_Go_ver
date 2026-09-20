import { useEffect, useState } from "react";
import {
  closeSave,
  getProvider,
  listProviders,
  saveProvider,
  testModel,
  type ProviderSummary,
  type ProviderView,
} from "../api/modelProvidersAdmin";
import { errText } from "../display";

/** 共享模型供应商目录：仓库分析读取可用来源，项目执行引用固定模型配置。
 * 观测模型在同一设置分类的 Jev／DeepSeek 表单维护，保存到本地工作台。 */

/** `embedded`：收进设置页「模型与 API」分类时为 true——去掉页面级宽度与大标题，
 *  与其他从侧栏收编进设置的页面（技能 / 智能体 / 本地 CLI）一致。 */
export function ModelProvidersPage({ embedded = false }: { embedded?: boolean }) {
  const [providers, setProviders] = useState<ProviderSummary[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [selected, setSelected] = useState<ProviderSummary | null>(null);
  const [detail, setDetail] = useState<ProviderView | null>(null);
  const [detailError, setDetailError] = useState<string | null>(null);
  // 新建中转站（写面走异步保存信封：save → 若 committed 再 close）
  const [creating, setCreating] = useState(false);
  const [busy, setBusy] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  // 连通性测试结果（按模型行 id 存）。测试走 POST /api/model-tests，
  // 体要 providerId + providerRevision + modelRowId（模型行 id，不是 modelId 字符串）。
  const [testResults, setTestResults] = useState<Record<string, string>>({});
  const [testing, setTesting] = useState<string | null>(null);

  const runTest = async (modelRowId: string) => {
    if (!detail) return;
    setTesting(modelRowId);
    setTestResults((prev) => ({ ...prev, [modelRowId]: "测试中…" }));
    try {
      const result = await testModel({
        providerId: detail.id,
        providerRevision: detail.revision,
        modelRowId,
      });
      // 结果形状后端未在契约里锁定，如实把可读字段摊平显示，不猜成功/失败。
      const status = String(
        (result as { status?: unknown }).status ??
          (result as { outcome?: unknown }).outcome ??
          "已提交",
      );
      setTestResults((prev) => ({ ...prev, [modelRowId]: status }));
    } catch (err) {
      setTestResults((prev) => ({ ...prev, [modelRowId]: errText(err) }));
    } finally {
      setTesting(null);
    }
  };
  const [form, setForm] = useState({
    name: "",
    baseUrl: "",
    apiFormat: "openai_chat_completions",
    apiKey: "",
    modelId: "",
    displayName: "",
    // 后端把模型的这四个字段都当**必填**校验（internal/models/input.go:224-235）：
    // contextWindow / maxOutputTokens 必须是 1..2147483647 的正整数，且
    // maxOutputTokens <= contextWindow；reasoning / vision 必须是布尔。
    // 2026-09-20 线上实测：这个表单原来一个都不发，于是「新建中转站」**永远**
    // 422 models.contextWindow INVALID —— 一条路都走不通。默认值只是表单里的
    // 初值（人看得见、改得动），不是后端替谁编的数字。
    contextWindow: "128000",
    maxOutputTokens: "8192",
    reasoning: false,
    vision: false,
  });

  const submit = async () => {
    setFormError(null);
    if (form.name.trim() === "" || form.baseUrl.trim() === "" || form.modelId.trim() === "") {
      setFormError("中转站名称、接入地址、至少一个模型 ID 都是必填。");
      return;
    }
    if (form.apiKey.trim() === "") {
      setFormError("新建中转站必须提供 API Key（后端：providerId 缺席时 secret 必须 replace）。");
      return;
    }
    const contextWindow = Number(form.contextWindow.trim());
    const maxOutputTokens = Number(form.maxOutputTokens.trim());
    if (!Number.isInteger(contextWindow) || contextWindow < 1) {
      setFormError("上下文窗口必须是不小于 1 的整数。");
      return;
    }
    if (!Number.isInteger(maxOutputTokens) || maxOutputTokens < 1 || maxOutputTokens > contextWindow) {
      setFormError("最大输出必须是不小于 1、且不大于上下文窗口的整数。");
      return;
    }
    setBusy(true);
    try {
      const receipt = await saveProvider({
        name: form.name.trim(),
        baseUrl: form.baseUrl.trim(),
        apiFormat: form.apiFormat,
        secret: { mode: "replace", value: form.apiKey.trim() },
        models: [
          {
            modelId: form.modelId.trim(),
            displayName: form.displayName.trim() || form.modelId.trim(),
            contextWindow,
            maxOutputTokens,
            reasoning: form.reasoning,
            vision: form.vision,
          },
        ],
      });
      if (receipt.outcome !== "committed") {
        setFormError(`后端未提交：${JSON.stringify(receipt.error ?? receipt)}`);
        return;
      }
      // 收口：saveId 同时作幂等头（后端要求路径键 == 头键）
      if (receipt.saveId) await closeSave(receipt.saveId).catch(() => undefined);
      setNotice(`已保存中转站 ${form.name.trim()}`);
      setCreating(false);
      setForm({ ...form, name: "", baseUrl: "", apiKey: "", modelId: "", displayName: "" });
      setProviders(await listProviders());
    } catch (err) {
      setFormError(errText(err));
    } finally {
      setBusy(false);
    }
  };

  useEffect(() => {
    listProviders()
      .then(setProviders)
      .catch((err: unknown) => {
        setProviders([]);
        setError(errText(err));
      });
  }, []);

  useEffect(() => {
    if (!selected) {
      setDetail(null);
      setDetailError(null);
      return;
    }
    setDetail(null);
    setDetailError(null);
    getProvider(selected.id)
      .then(setDetail)
      .catch((err: unknown) => setDetailError(errText(err)));
  }, [selected]);

  return (
    <div className={embedded ? "" : "mx-auto max-w-[1040px] px-8 py-10"}>
      <div className={`${embedded ? "mb-3 border-b border-line pb-2.5 " : "mb-6 "}flex flex-wrap items-baseline gap-3`}>
        {!embedded && <h1 className="text-[16px] font-semibold text-cream">模型供应商</h1>}
        {providers && <span className="text-[11.5px] text-tx2">{providers.length} 个中转站</span>}
        <span className="text-[11.5px] text-tx3">仓库分析 / AgentTeams 模型来源</span>
        <button
          className="ml-auto rounded-hard border border-line-strong px-3 py-[5px] text-[12px] text-cream hover:bg-amber/10"
          onClick={() => {
            setCreating((v) => !v);
            setFormError(null);
          }}
        >
          {creating ? "取消" : "+ 新建中转站"}
        </button>
      </div>

      <section className="mb-5 rounded-hard border border-line bg-panel px-4 py-3 text-[11.5px] leading-relaxed text-tx2">
        <p><strong className="text-cream">仓库分析：</strong>用这里的 API 地址、模型和 Key 做需求与仓库的语义匹配。</p>
        <p className="mt-1"><strong className="text-cream">AgentTeams：</strong>这里管理模型来源，实际运行模型由项目执行配置决定；保存供应商不会自动切换团队模型。</p>
        <p className="mt-1"><strong className="text-cream">观测评估：</strong>在本页上方的 Jev／DeepSeek 表单配置（与这里的 Key 分开保存）。</p>
      </section>

      {notice && (
        <p className="mb-4 rounded-hard border border-line bg-panel px-3 py-2 text-[12px] text-tx2">
          {notice}
        </p>
      )}

      {creating && (
        <section className="mb-6 rounded-hard border border-line bg-panel px-5 py-5">
          <h2 className="text-[14px] font-semibold text-cream">新建中转站</h2>
          <p className="mt-1 text-[11.5px] text-tx2">
            用途：仓库分析与项目执行的模型来源。API Key 加密保存；观测模型 Key 通过本页上方表单管理。
          </p>
          <div className="mt-4 grid grid-cols-2 gap-3">
            <label className="text-[12.5px] text-tx2">
              名称
              <input
                className="mt-1 w-full rounded-hard border border-line bg-well px-2.5 py-[6px] text-[13px] text-tx"
                value={form.name}
                onChange={(e) => setForm({ ...form, name: e.target.value })}
                placeholder="例如：中转站 A"
              />
            </label>
            <label className="text-[12.5px] text-tx2">
              模型 API 地址（仓库分析 / 执行来源）
              <input
                className="mt-1 w-full rounded-hard border border-line bg-well px-2.5 py-[6px] font-mono text-[12.5px] text-tx"
                value={form.baseUrl}
                onChange={(e) => setForm({ ...form, baseUrl: e.target.value })}
                placeholder="https://api.example.com/v1"
              />
            </label>
            <label className="text-[12.5px] text-tx2">
              接口格式
              <select
                className="mt-1 w-full rounded-hard border border-line bg-well px-2.5 py-[6px] font-mono text-[12.5px] text-tx"
                value={form.apiFormat}
                onChange={(e) => setForm({ ...form, apiFormat: e.target.value })}
              >
                <option value="openai_chat_completions">openai_chat_completions</option>
              </select>
            </label>
            <label className="text-[12.5px] text-tx2">
              API Key（仓库分析 / 执行来源）
              <input
                type="password"
                className="mt-1 w-full rounded-hard border border-line bg-well px-2.5 py-[6px] font-mono text-[12.5px] text-tx"
                value={form.apiKey}
                onChange={(e) => setForm({ ...form, apiKey: e.target.value })}
                placeholder="sk-…"
              />
            </label>
            <label className="text-[12.5px] text-tx2">
              模型 ID
              <input
                className="mt-1 w-full rounded-hard border border-line bg-well px-2.5 py-[6px] font-mono text-[12.5px] text-tx"
                value={form.modelId}
                onChange={(e) => setForm({ ...form, modelId: e.target.value })}
                placeholder="例如：MiniMax-M2"
              />
            </label>
            <label className="text-[12.5px] text-tx2">
              显示名（可空）
              <input
                className="mt-1 w-full rounded-hard border border-line bg-well px-2.5 py-[6px] text-[12.5px] text-tx"
                value={form.displayName}
                onChange={(e) => setForm({ ...form, displayName: e.target.value })}
                placeholder="例如：MiniMax M2"
              />
            </label>
            <label className="text-[12.5px] text-tx2">
              上下文窗口（tokens，必填）
              <input
                className="mt-1 w-full rounded-hard border border-line bg-well px-2.5 py-[6px] font-mono text-[12.5px] text-tx"
                value={form.contextWindow}
                onChange={(e) => setForm({ ...form, contextWindow: e.target.value })}
                placeholder="例如：128000"
              />
            </label>
            <label className="text-[12.5px] text-tx2">
              最大输出（tokens，必填，不得大于上下文窗口）
              <input
                className="mt-1 w-full rounded-hard border border-line bg-well px-2.5 py-[6px] font-mono text-[12.5px] text-tx"
                value={form.maxOutputTokens}
                onChange={(e) => setForm({ ...form, maxOutputTokens: e.target.value })}
                placeholder="例如：8192"
              />
            </label>
            <label className="flex items-center gap-2 text-[12.5px] text-tx2">
              <input
                type="checkbox"
                checked={form.reasoning}
                onChange={(e) => setForm({ ...form, reasoning: e.target.checked })}
              />
              支持推理（reasoning）
            </label>
            <label className="flex items-center gap-2 text-[12.5px] text-tx2">
              <input
                type="checkbox"
                checked={form.vision}
                onChange={(e) => setForm({ ...form, vision: e.target.checked })}
              />
              支持视觉（vision）
            </label>
          </div>
          {formError && (
            <p className="mt-3 rounded-hard border border-salmon-deep bg-salmon-well px-3 py-2 text-[12px] text-salmon-hi">
              {formError}
            </p>
          )}
          <button
            className="login-cta mt-4 rounded-hard px-4 py-[7px] text-[12.5px] font-extrabold tracking-[0.04em] disabled:opacity-60"
            disabled={busy}
            onClick={() => void submit()}
          >
            {busy ? "正在保存…" : "保存中转站"}
          </button>
        </section>
      )}

      {error && (
        <p className="mb-4 rounded-hard border border-salmon-deep bg-salmon-well px-3 py-2 text-[12px] text-salmon-hi">
          供应商列表加载失败：{error}
        </p>
      )}

      <div className="grid grid-cols-[minmax(220px,280px)_1fr] gap-5">
        <div className="rounded-hard border border-line bg-panel">
          {providers === null && <p className="px-3 py-3 text-[12px] text-tx3">正在读取…</p>}
          {providers !== null && providers.length === 0 && (
            <p className="px-3 py-3 text-[12px] text-tx3">还没有配置中转站。</p>
          )}
          {(providers ?? []).map((provider) => {
            const active = selected?.id === provider.id;
            return (
              <button
                key={provider.id}
                className={`block w-full border-b border-line px-3 py-2 text-left last:border-b-0 ${
                  active ? "bg-amber/10" : "hover:bg-well"
                }`}
                onClick={() => setSelected(provider)}
              >
                <span className="block truncate text-[12.5px] text-tx">{provider.name}</span>
                <span className="mt-[2px] block truncate text-[11px] text-tx2">
                  {provider.modelCount} 个模型
                </span>
              </button>
            );
          })}
        </div>

        <div className="min-w-0">
          {selected === null ? (
            <div className="rounded-hard border border-line bg-panel px-4 py-6 text-[12.5px] text-tx2">
              选左侧一个中转站，查看它的接入地址与模型清单。
            </div>
          ) : detailError ? (
            <p className="rounded-hard border border-salmon-deep bg-salmon-well px-3 py-2 text-[12px] text-salmon-hi">
              详情加载失败：{detailError}
            </p>
          ) : detail === null ? (
            <p className="text-[12.5px] text-tx3">正在读取详情…</p>
          ) : (
            <>
              <div className="rounded-hard border border-line bg-panel px-4 py-3">
                <div className="text-[13px] text-cream">{detail.name}</div>
                <div className="mt-1.5 grid grid-cols-[80px_1fr] gap-y-1 text-[11.5px]">
                  <span className="text-tx3">接入地址</span>
                  <span className="truncate font-mono text-tx2">{detail.baseUrl}</span>
                  <span className="text-tx3">接口格式</span>
                  <span className="font-mono text-tx2">{detail.apiFormat}</span>
                </div>
              </div>

              <div className="mt-4">
                <div className="mb-2 text-[12.5px] text-cream">
                  模型清单 <span className="text-tx3">({(detail.models ?? []).length})</span>
                </div>
                {(detail.models ?? []).length === 0 && (
                  <p className="text-[12px] text-tx3">这个中转站下还没有登记模型。</p>
                )}
                <div className="space-y-1.5">
                  {(detail.models ?? []).map((model) => (
                    <div
                      key={model.modelId}
                      className="flex items-center justify-between gap-4 rounded-hard border border-line bg-panel px-3 py-2"
                    >
                      <span className="min-w-0 flex-1 truncate font-mono text-[12px] text-tx">
                        {model.displayName ?? model.modelId}
                      </span>
                      <span className="flex-none text-[11px] text-tx3">
                        {model.contextWindow ? `上下文 ${model.contextWindow}` : ""}
                        {model.reasoning ? " · 推理" : ""}
                        {model.vision ? " · 视觉" : ""}
                      </span>
                      {(() => {
                        const modelRowId = String((model as { id?: unknown }).id ?? "");
                        const result = testResults[modelRowId];
                        return (
                          <>
                            {result && (
                              <span className="max-w-[200px] flex-none truncate text-[11px] text-tx2" title={result}>
                                {result}
                              </span>
                            )}
                            <button
                              className="flex-none rounded-hard border border-line px-2 py-[2px] text-[11px] text-tx2 hover:border-amber hover:text-amber-hi disabled:opacity-50"
                              disabled={modelRowId === "" || testing === modelRowId}
                              onClick={() => void runTest(modelRowId)}
                            >
                              测试
                            </button>
                          </>
                        );
                      })()}
                    </div>
                  ))}
                </div>
              </div>
            </>
          )}
        </div>
      </div>
    </div>
  );
}
