import { useCallback, useEffect, useMemo, useState } from "react";
import { ApiError } from "../api/client";
import type { DiscoveryEffectiveTier, TopologyPolicyDraftView } from "../api/contract";
import { fetchConsoleRepositories } from "../api/grid";
import { fetchPolicyDraft } from "../api/humanControl";
import { errText } from "../display";
import { SupervisionPolicyCard, type PolicyDraftState } from "./SupervisionPolicyCard";
import { SupervisionPolicyDialog } from "./SupervisionPolicyDialog";

/**
 * 设置 → 平台 里的「监管策略」段（2026-09-21 用户要求：把这个设置搬进设置里，
 * 方便调整人工审核策略）。
 *
 * 此前**唯一入口**在 issue 工作台的发现链卡片上——想改人工审核强度，得先找到
 * 某个还没物化的 issue 点进去。用户的原话是「这里可以做到设置里面嘛」。
 *
 * 三件事刻意与工作台那份保持一致，不另起一套：
 *   1. 读的是同一个端点（`GET /projects/{id}/policy-draft`），后端
 *      `ResolveProjectScope` 本来就同时认 issue id 与项目 id，所以这里传**项目 id**
 *      即可，不需要先找一个 issue；
 *   2. 渲染复用 `SupervisionPolicyCard`，四态（未设定 / 已设定 / 已冻结 / 读不到）
 *      的措辞与判定完全同一份代码；
 *   3. 编辑复用 `SupervisionPolicyDialog`——校验规则（非全自动至少一处卡点、
 *      至少一位审核人、仓库监督人必须指仓库）只有它一份，抄第二份就会分叉。
 *
 * 「已冻结」（首次物化时后端盖的 `frozen_at`）在这里**如实显示为只读**：那不是
 * 界面保守，是存储层的事实——`PUT` 会 409、`DELETE` 同样被拒。设置页不该给人
 * 一个点了会失败的按钮。
 */
export function SupervisionPolicySettings({
  projectId,
  projectName,
  onToast,
}: {
  /** 当前选中项目。null = 还没选项目，这一段如实说「先选项目」。 */
  projectId: string | null;
  projectName?: string;
  onToast?: (text: string) => void;
}) {
  const [draft, setDraft] = useState<PolicyDraftState>({ kind: "loading" });
  const [reload, setReload] = useState(0);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [repos, setRepos] = useState<Array<{ id: string; name: string }>>([]);

  const load = useCallback(async () => {
    if (!projectId) {
      setDraft({ kind: "loading" });
      return;
    }
    setDraft({ kind: "loading" });
    try {
      const view = await fetchPolicyDraft(projectId);
      setDraft(view.frozen ? { kind: "sealed", draft: view } : { kind: "set", draft: view });
    } catch (err) {
      // 四态分开呈现的理由见 SupervisionPolicyCard 顶部的注释：404 不是错误，
      // 401 重试没有意义，403 是「存在但读不到」——三者的界面动作完全不同。
      if (err instanceof ApiError && err.status === 404) {
        setDraft({ kind: "unset" });
        return;
      }
      if (err instanceof ApiError && err.status === 401) {
        setDraft({ kind: "unauthenticated", detail: errText(err) });
        return;
      }
      if (err instanceof ApiError && err.status === 403) {
        setDraft({ kind: "forbidden", detail: errText(err) });
        return;
      }
      setDraft({ kind: "error", message: errText(err) });
    }
  }, [projectId]);

  useEffect(() => {
    void load();
  }, [load, reload]);

  // 仓库目录：弹窗里「仓库监督人」的仓库下拉要它。取不到只是限不了仓库范围，
  // 不该让这一段打不开——所以单独 catch，失败就留空数组。
  useEffect(() => {
    let alive = true;
    fetchConsoleRepositories()
      .then((rows) => {
        if (!alive) return;
        setRepos(rows.map((row) => ({ id: row.id, name: row.name })));
      })
      .catch(() => {
        if (alive) setRepos([]);
      });
    return () => {
      alive = false;
    };
  }, []);

  /** 弹窗的仓库下拉是「发现链分档 ∩ 仓库目录」。设置页没有发现链，就把**项目仓库
   *  全量**当分档结果喂进去——语义上等价于「这些仓库都可能进计划」，不会漏掉
   *  任何可选仓库；至于某个仓库这次要不要动，是计划那一步的事。 */
  const effectiveTiers = useMemo<DiscoveryEffectiveTier[]>(
    () =>
      repos.map((row) => ({
        repository: row.name,
        tier: "required" as const,
        adjusted: false,
        original_tier: null,
      })),
    [repos],
  );

  if (!projectId) {
    return (
      <p className="py-2 text-[11.5px] text-tx3">
        监管策略按项目存放，先在上方选一个项目。
      </p>
    );
  }

  return (
    <div className="flex flex-col gap-2 py-1">
      <p className="text-[11.5px] text-tx2">
        当前项目：<span className="font-mono">{projectName ?? projectId}</span>
        。这里设的是<b className="text-tx">这个项目的监管强度</b>：全自动不停顿，
        人工参与会在你勾的卡点上停下来等人。首次物化时后端按这里的设定建出项目档案；
        那一刻之后这份策略会被盖章冻结（存储层的 <span className="font-mono">frozen_at</span>），
        届时这里只读，并如实告诉你冻结时间。
      </p>
      <SupervisionPolicyCard
        state={draft}
        onConfigure={() => setDialogOpen(true)}
        onRetry={() => setReload((n) => n + 1)}
      />
      <SupervisionPolicyDialog
        open={dialogOpen}
        projectId={projectId}
        issueTitle={projectName ? `${projectName}（项目设置）` : "项目设置"}
        effectiveTiers={effectiveTiers}
        // 设置页没有「这一次计划有多少任务」，所以代价预告按 null 走——
        // 弹窗对 null 的措辞是「每个任务各 1 次」，不拿 0 冒充。
        taskCount={null}
        onClose={() => setDialogOpen(false)}
        onSaved={(saved) => {
          setDialogOpen(false);
          setDraft(
            saved === null
              ? { kind: "unset" }
              : saved.frozen
                ? { kind: "sealed", draft: saved }
                : { kind: "set", draft: saved },
          );
          onToast?.(saved === null ? "监管策略已撤回" : "监管策略已保存");
        }}
      />
    </div>
  );
}
