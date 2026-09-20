import { useEffect, useState } from "react";
import {
  fetchAppInstallation,
  type AppInstallationTarget,
  type AppInstallationView,
} from "../api/appInstallation";

/** GitHub App 安装引导（2026-09-20，用户诉求原话：别让「要装 GitHub App」这件事在
 *  使用过程中以一句报错的形式冒出来（比如建 issue 时突然「暂不能创建：
 *  NO_AVAILABLE_REPOSITORIES」），而是在**一开始就指引清楚**）。
 *
 *  两种呈现共用同一份数据与同一套逐行渲染：
 *   - `variant="card"`：完整引导卡（外壳首次引导 / 设置页常驻入口）；
 *   - `variant="inline"`：紧凑版（仓库页的缺口就地提示）。
 *
 *  三条不许含糊的语义（都来自后端给的**事实**，不许前端归并）：
 *   ① `suspended`（挂起）**不是**「未安装」；
 *   ② `installed + repositorySelection:"selected"` **不能断言「缺了仓」**——
 *      账号级看不到"某个仓在不在选中列表里"（线上实测：两个仓都归一个选装账号，
 *      而它们其实是好的）。这一行只说「安装按仓库挑选」+ 给设置页入口；
 *      逐仓的真相看仓库页那列 App 状态（`allProjectRepositories` 的结论）。
 *   ③ `unavailable` 非空时后端是在如实说「探测不了」——这时**不能显示成「没装」**，
 *      而且账号行一律不展示（数据不可信）。
 *  `canInstall` 目前恒为 null（判断组织角色要 `read:org` scope，本部署刻意不申请），
 *  所以组织行只给中性提示，**不做**「你有权 / 你无权」的判断。
 *
 *  「稍后再说」只压自动引导；设置页的常驻入口传 `respectDismissal={false}`，
 *  这样关掉首页提示不会把「到哪查状态」一起关掉。 */

/** 「稍后再说」的记忆键：只影响外壳那一次自动引导（理由见上）。 */
const DISMISS_KEY = "repomesh.appGuide.dismissed";

function readDismissed(): boolean {
  try {
    return window.localStorage.getItem(DISMISS_KEY) === "1";
  } catch {
    /* 隐私模式等存储不可用：只丢记忆，不影响本次渲染 */
    return false;
  }
}

/** 取数：两版共用。失败**静默**（`failed`）——首页弹一个红框吓人比这条引导缺席更糟，
 *  失败只在卡片上轻声说一句（见下）。 */
function useAppInstallation(): { view: AppInstallationView | null; failed: boolean } {
  const [state, setState] = useState<{ view: AppInstallationView | null; failed: boolean }>({
    view: null,
    failed: false,
  });
  useEffect(() => {
    let cancelled = false;
    const load = () => {
      fetchAppInstallation()
        .then((view) => {
          if (!cancelled) setState({ view, failed: false });
        })
        .catch(() => {
          // 已经有旧事实就留着旧事实：一次抖动不该把「已就绪」翻成失败
          if (!cancelled) setState((prev) => (prev.view ? prev : { view: null, failed: true }));
        });
    };
    load();
    // 安装发生在 GitHub 的另一个标签页：切回来重取一次，免得人刚装完、
    // 界面还在说「未安装」（那会让人把同一个链接再点一遍）。
    const onVisible = () => {
      if (document.visibilityState === "visible") load();
    };
    document.addEventListener("visibilitychange", onVisible);
    return () => {
      cancelled = true;
      document.removeEventListener("visibilitychange", onVisible);
    };
  }, []);
  return state;
}

/** 一行账号的覆盖事实 + 一个可点的去处。
 *
 *  颜色/字形沿用全站既有语义：✓ = 完成（olive）、⚠ = 待办/不足（amber），
 *  挂起用 salmon（它是「坏了」，不只是「没做」）。 */
function TargetRow({ target }: { target: AppInstallationTarget }) {
  const { login, kind, installed, repositorySelection, suspended, installUrl } = target;
  // 逐态照实说，不许归并：挂起 ≠ 没装。
  //
  // `selected` 那一行 **不能写成「只覆盖了部分仓库」**——2026-09-20 线上数据抓出来的：
  // 两个仓都归一个"装了但按仓库挑选"的账号，而那两个仓其实是好的。账号级**看不到**
  // "某个仓在不在选中列表里"，断言"缺了仓"就是假报，还会催人去补勾本来就好的仓。
  // 所以这一行只说事实（安装是按仓库挑选的）+ 给一个设置页入口，把"这个仓到底行不行"
  // 留给仓库页那列逐仓的 App 状态（那才是权威）。
  const state = !installed
    ? { tone: "text-amber", mark: "⚠", text: "未安装", action: "去安装 →" }
    : suspended
      ? { tone: "text-salmon", mark: "⚠", text: "安装已被挂起", action: "去安装设置 →" }
      : repositorySelection === "all"
        ? { tone: "text-olive", mark: "✓", text: "已覆盖名下全部仓库", action: null }
        : { tone: "text-tx2", mark: "·", text: "安装按仓库挑选（未覆盖全部）", action: "去安装设置 →" };
  return (
    <li className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
      <span className={`flex-none ${state.tone}`}>
        <span aria-hidden>{state.mark}</span>
        <span className="sr-only">{state.mark === "✓" ? "已完成" : "待办"}</span>
      </span>
      <span className="font-mono text-[11.5px] text-tx">{login}</span>
      <span className="text-[11px] text-tx3">
        （{kind === "organization" ? "组织" : "个人"}）
      </span>
      <span className={`text-[11.5px] ${state.tone}`}>· {state.text}</span>
      {state.action && (
        // installUrl 是后端算好的（没装 = 安装页直链带 suggested_target_id；
        // 已装 = 那个 installation 的设置页），**原样用，不要自己拼**。
        <a
          className="flex-none text-[11.5px] text-amber-hi underline-offset-2 hover:underline"
          href={installUrl}
          target="_blank"
          rel="noreferrer"
        >
          {state.action}
        </a>
      )}
      {/* canInstall 恒为 null，我们不知道这个人有没有权限——只给中性提示，不断言。
          已覆盖的行不给这句：那是纯噪声。 */}
      {kind === "organization" && state.action !== null && (
        <span className="w-full text-[11px] text-tx3">
          若 GitHub 提示你没有权限，需要该组织的所有者来装
        </span>
      )}
    </li>
  );
}

/** 一行「为什么」——两版共用。 */
function Why({ className = "" }: { className?: string }) {
  return (
    <p className={`text-[11.5px] leading-relaxed text-tx2 ${className}`.trim()}>
      agent 要以平台身份推代码、开 PR，凭的是 GitHub App 的安装授权；GitHub 要求在每个仓
      所属的账号上各装一次（个人号与组织是各自独立的安装目标，装在个人号上覆盖不到
      组织名下的仓）。
    </p>
  );
}

/** 最省事的那一步，必须写死：选 All repositories，将来新增的仓不用再来一次。 */
function AllRepositoriesHint({ className = "" }: { className?: string }) {
  return (
    <p className={`text-[11.5px] text-amber ${className}`.trim()}>
      安装时请选 All repositories —— 之后新增的仓库自动覆盖，不用再来一次。
    </p>
  );
}

export function AppInstallGuide({
  variant = "card",
  showWhenReady = false,
  respectDismissal = true,
}: {
  variant?: "card" | "inline";
  /** 设置页的常驻入口：**没有缺口时也要有一处能查状态**，所以那里在
   *  `uncoveredCount === 0` 时显示一行「GitHub App 授权就绪」而不是整块消失。 */
  showWhenReady?: boolean;
  /** 是否吃 localStorage 的「稍后再说」记忆。默认吃（外壳的自动引导是弹出来的）；
   *  设置页传 false——关掉提示不该把「到哪查状态」也一起关掉。 */
  respectDismissal?: boolean;
}) {
  const { view, failed } = useAppInstallation();
  const [dismissed, setDismissed] = useState(readDismissed);
  const dismissible = variant === "card" && respectDismissal;

  if (dismissible && dismissed) return null;

  // 取数失败：静默返回 null（不在首页弹红框）；卡片/常驻入口才轻声说一句。
  if (!view) {
    if (failed && (variant === "card" || showWhenReady)) {
      return <p className="mt-2 text-[11px] text-tx3">安装状态读取失败——稍后会自动重试。</p>;
    }
    return null;
  }

  const unavailable = (view.unavailable ?? "").trim();
  const hasGap = view.uncoveredCount > 0;

  // 全好了就不该再出现（除非调用方明确要常驻状态行）
  if (!hasGap && !unavailable && !showWhenReady) return null;

  // 常驻入口的「就绪」态：一行，不铺开整卡
  if (!hasGap && !unavailable) {
    return (
      <p className="mt-2 text-[11.5px] text-olive">
        <span aria-hidden>✓ </span>GitHub App 授权就绪 · 已覆盖全部仓库
      </p>
    );
  }

  // `unavailable` 非空 = App 侧探测失败（凭据没配 / GitHub 调不通）。如实显示它，
  // **并且不展示账号行**——这时的 targets 不反映真实安装情况，拿它说「没装」就是撒谎。
  const unavailableLine = unavailable ? (
    <p className="mt-1 text-[11.5px] text-tx2">{unavailable}</p>
  ) : null;
  const body = unavailable ? null : (
    <>
      <Why className="mt-1" />
      <AllRepositoriesHint className="mt-1" />
      <ul className="mt-2 grid gap-1">
        {view.targets.map((target) => (
          <TargetRow key={target.login} target={target} />
        ))}
      </ul>
    </>
  );

  if (variant === "inline") {
    return (
      <div className="mt-2 rounded-hard border border-amber/40 bg-amber-well px-3 py-2">
        <p className="text-[11.5px] font-medium text-amber">需要接入 GitHub App</p>
        {unavailableLine ?? body}
      </div>
    );
  }

  return (
    <div className="mt-2 rounded-hard border border-amber/40 bg-amber-well px-4 py-3">
      <div className="flex flex-wrap items-baseline gap-x-3">
        <h2 className="text-[13px] font-semibold text-cream">需要接入 GitHub App</h2>
        {unavailable ? null : (
          <span className="text-[11px] text-tx3">
            {view.uncoveredCount} 个仓库还没被覆盖 · {view.targets.length} 个账号
          </span>
        )}
      </div>
      {unavailableLine ?? body}
      {dismissible && (
        <button
          type="button"
          className="mt-2.5 rounded-hard border border-line px-2.5 py-[3px] text-[11.5px] text-tx2 hover:border-amber hover:text-amber-hi"
          onClick={() => {
            setDismissed(true);
            try {
              window.localStorage.setItem(DISMISS_KEY, "1");
            } catch {
              /* 存储不可用：本次仍关闭，只是下次刷新会再出现 */
            }
          }}
        >
          稍后再说
        </button>
      )}
    </div>
  );
}
