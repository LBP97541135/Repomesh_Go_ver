import { useCallback, useEffect, useState } from "react";
import {
  listConfigurationProfiles,
  registerExecutionProfile,
  setDefaultConfigurationProfile,
  type ConfigurationProfileItem,
} from "../api/projects";
import { errText } from "../display";

/** 配置档案管理页（设置 → 配置档案）。
 *
 *  2026-09-22 新建。此前**整个前端没有"档案"这个概念承载的页面** ——
 *  `listConfigurationProfiles` 定义了却**零调用**，而后端 `defaults` 表
 *  （`inherit` 的解析来源）**只有导入路径能写**。线上实测 catmem 有一个完整可用的
 *  模型档案、却**没有任何办法把它设成默认**；执行档案更是**一个都建不出来** ——
 *  于是他的项目走 `inherit` 解析到空，建 issue 被「执行配置未完成」永久阻断，
 *  而且他自己解不开。
 *
 *  这个页面就是那个缺失的入口：**列档案 + 设为默认 + 建执行档案**。
 *
 *  为什么两类档案并排而不是分页：它们是**同一个决策的两半**（一个项目要同时有
 *  模型与执行两侧的默认才能建单），分开摆会让人只设一半就走了 —— 那正是
 *  catmem 踩的坑的形状。
 */
export function ProfilesPage({ onToast }: { onToast: (text: string) => void }) {
  const [model, setModel] = useState<ConfigurationProfileItem[] | null>(null);
  const [execution, setExecution] = useState<ConfigurationProfileItem[] | null>(null);
  const [modelDefault, setModelDefault] = useState<string | null>(null);
  const [executionDefault, setExecutionDefault] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [newName, setNewName] = useState("");
  const [newWorkers, setNewWorkers] = useState(4);

  const load = useCallback(async () => {
    try {
      const [m, e] = await Promise.all([
        listConfigurationProfiles("model"),
        listConfigurationProfiles("execution"),
      ]);
      setModel(m.items);
      setModelDefault(m.defaultProfileId);
      setExecution(e.items);
      setExecutionDefault(e.defaultProfileId);
      setError(null);
    } catch (reason) {
      setError(errText(reason));
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const apply = async (kind: "model" | "execution", profileId: string) => {
    setBusy(true);
    try {
      await setDefaultConfigurationProfile(kind, profileId);
      onToast(`已把该${kind === "model" ? "模型" : "执行"}档案设为默认。`);
      await load();
    } catch (reason) {
      onToast(`设为默认失败：${errText(reason)}`);
    } finally {
      setBusy(false);
    }
  };

  const createExecution = async () => {
    setBusy(true);
    try {
      const created = await registerExecutionProfile(newName.trim(), newWorkers);
      onToast(`执行档案已创建（${created.profileId.slice(0, 8)}…，v1）。`);
      setNewName("");
      await load();
    } catch (reason) {
      onToast(`创建执行档案失败：${errText(reason)}`);
    } finally {
      setBusy(false);
    }
  };

  const section = (
    kind: "model" | "execution",
    items: ConfigurationProfileItem[] | null,
    current: string | null,
  ) => (
    <section className="mt-5 rounded-hard border border-line bg-panel p-5">
      <h2 className="text-[13px] font-semibold text-cream">
        {kind === "model" ? "模型档案" : "执行档案"}
      </h2>
      <p className="mt-1 text-[11.5px] leading-relaxed text-tx2">
        {kind === "model"
          ? "项目没有显式钉住模型时，走 inherit 回退到这里的默认。"
          : "项目没有显式钉住执行档案时，走 inherit 回退到这里的默认；执行档案决定 worker 并发与预算/时限政策。"}
      </p>
      {items === null ? (
        <p className="mt-3 text-[12px] text-tx2">正在读取…</p>
      ) : items.length === 0 ? (
        <p className="mt-3 text-[12px] text-tx2">
          还没有{kind === "model" ? "模型" : "执行"}档案。
          {kind === "execution"
            ? "执行档案此前没有任何创建入口，用下面的表单建一条。"
            : "先在「模型与 API」里保存一个模型来源。"}
        </p>
      ) : (
        <ul className="mt-3 flex flex-col gap-2">
          {items.map((item) => (
            <li
              key={item.id}
              className="flex items-center gap-3 rounded-hard border border-line px-3 py-2"
            >
              <span className="min-w-0 flex-1 truncate text-[12.5px] text-cream">{item.name}</span>
              <span className="font-mono text-[10.5px] text-tx3">{item.id.slice(0, 8)}…</span>
              {current === item.id ? (
                <span className="rounded-hard border border-olive px-2 py-px text-[11px] text-olive">
                  默认
                </span>
              ) : (
                <button
                  type="button"
                  disabled={busy}
                  className="rounded-hard border border-line px-2.5 py-[3px] text-[11.5px] text-tx2 transition-colors hover:border-amber hover:text-amber-hi disabled:opacity-50"
                  onClick={() => void apply(kind, item.id)}
                >
                  设为默认
                </button>
              )}
            </li>
          ))}
        </ul>
      )}
    </section>
  );

  return (
    <div className="max-w-[820px]">
      <h1 className="text-[16px] font-semibold text-cream">配置档案</h1>
      <p className="mt-2 text-[12px] leading-[1.8] text-tx2">
        项目的固定配置里若把模型或执行档案写成 <span className="font-mono">inherit</span>，
        就回退到这里设的默认。**两侧都要有默认**，否则建 issue 会被「执行配置未完成」挡住 ——
        而在此之前这里根本没有页面，两类档案也都设不了默认。
      </p>
      {error && <p role="alert" className="mt-3 text-[12px] text-salmon">{error}</p>}

      {section("model", model, modelDefault)}
      {section("execution", execution, executionDefault)}

      <section className="mt-5 rounded-hard border border-line bg-panel p-5">
        <h2 className="text-[13px] font-semibold text-cream">新建执行档案</h2>
        <p className="mt-1 text-[11.5px] leading-relaxed text-tx2">
          预算与时限政策是**全局**的，服务端自动引用现成版本，不用在这里填。
          若服务端返回 409 POLICY_NOT_CONFIGURED，说明全局政策还没配 —— 那是部署侧的事。
        </p>
        <div className="mt-3 flex flex-wrap items-center gap-2">
          <input
            className="min-w-[200px] flex-1 rounded-hard border border-line bg-transparent px-3 py-1.5 text-[12.5px] text-cream outline-none focus:border-amber"
            placeholder="档案名，例如 Codex 标准执行"
            value={newName}
            onChange={(event) => setNewName(event.target.value)}
          />
          <label className="flex items-center gap-1.5 text-[11.5px] text-tx2">
            worker 并发
            <input
              type="number"
              min={1}
              max={16}
              className="w-[64px] rounded-hard border border-line bg-transparent px-2 py-1.5 text-[12.5px] text-cream outline-none focus:border-amber"
              value={newWorkers}
              onChange={(event) => setNewWorkers(Number(event.target.value))}
            />
          </label>
          <button
            type="button"
            disabled={busy || newName.trim() === "" || newWorkers < 1 || newWorkers > 16}
            className="rounded-hard border border-line px-3 py-1.5 text-[12px] text-tx2 transition-colors hover:border-amber hover:text-amber-hi disabled:opacity-50"
            onClick={() => void createExecution()}
          >
            创建执行档案
          </button>
        </div>
      </section>
    </div>
  );
}
