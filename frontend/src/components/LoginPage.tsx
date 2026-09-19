import { useState } from "react";
import { startGithubLogin, type Account } from "../api/auth";

/** 登录门（2026-09-16 裁定：GitHub OAuth，本地账号体系作废）。
 *
 *  流程：点击 → `POST /api/auth/github/login`（幂等键 + destination home）→
 *  跳转 GitHub 授权页 → 回调由后端换令牌并种会话 cookie → 重定向回首页，
 *  ConsoleShell 用 `GET /api/session` 确认登录态。 */
/** onAuthenticated 保留兼容旧签名：OAuth 流程经整页跳转回首页后由
 *  ConsoleShell 重新拉取会话，本组件内不再使用。 */
/** 配色与版式于 2026-09-19 重构：左品牌面 / 右登录面两栏（窄屏收一栏），
 *  舞台加琥珀光晕与细网格。样式全在 index.css 的 `.login-*` 段，深浅主题通用。
 *
 *  **换账号不在这里**：已登录时由侧栏「切换账号」打 `/api/auth/github/switch`，
 *  那条路径允许带活跃会话发起，回调以 identity_generation+1 作废旧会话。
 *  登录页只在未登录时出现，故只承载首次登录。 */
export function LoginPage({ onAuthenticated: _onAuthenticated }: { onAuthenticated?: (account: Account) => void } = {}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const login = () => {
    setBusy(true);
    setError(null);
    try {
      startGithubLogin();
      // 整页跳转离开本页；停留说明跳转被浏览器拦截或网络失败。
    } catch (err) {
      setBusy(false);
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  return (
    <div className="login-stage grid h-screen place-items-center px-6 py-10">
      <div className="login-card w-full max-w-[980px] overflow-hidden rounded-hard border border-line-strong bg-panel shadow-pop">
        {/* 左：品牌面 */}
        <section className="login-brand">
          <div className="flex items-center gap-3">
            <span className="grid size-[42px] flex-none place-items-center rounded-hard bg-amber font-mono text-[19px] font-extrabold text-on-amber shadow-card">
              R
            </span>
            <div>
              <strong className="block font-mono text-[15px] tracking-[0.16em] text-cream">REPOMESH</strong>
              <span className="microlabel">交付控制平面</span>
            </div>
          </div>

          <h2 className="mt-9 text-[21px] leading-[1.35] font-semibold text-cream">
            多仓库协作交付
            <br />
            <span className="text-amber-hi">从需求到 PR 的闭环</span>
          </h2>

          <ul className="mt-7 space-y-3 text-[12.5px] text-tx2">
            <li className="flex gap-2.5">
              <span className="mt-[6px] size-[5px] flex-none rounded-full bg-amber" aria-hidden="true" />
              <span>自动托管规划：分析、候选评分、分类、审批、物化</span>
            </li>
            <li className="flex gap-2.5">
              <span className="mt-[6px] size-[5px] flex-none rounded-full bg-amber" aria-hidden="true" />
              <span>DAG 派工到每个仓库，由编码智能体真实改动</span>
            </li>
            <li className="flex gap-2.5">
              <span className="mt-[6px] size-[5px] flex-none rounded-full bg-amber" aria-hidden="true" />
              <span>以 GitHub App 身份推送分支并创建 PR</span>
            </li>
          </ul>
        </section>

        {/* 右：登录面 */}
        <section className="login-pane">
          <h1 className="text-[16px] font-semibold text-cream">登录控制平面</h1>
          <p className="mt-1.5 text-[12.5px] text-tx2">
            使用 GitHub 账号登录。会话由后端 httpOnly cookie 持有，前端不存任何凭据。
          </p>

          {error && (
            <div className="mt-5 rounded-hard border border-salmon-deep bg-salmon-well px-3 py-2 text-[12px] text-salmon-hi">
              {error}
            </div>
          )}

          <button
            className="login-cta mt-6 w-full rounded-hard py-[10px] text-[13px] font-extrabold tracking-[0.04em] transition-colors"
            onClick={login}
            disabled={busy}
          >
            {busy ? "正在跳转 GitHub…" : "使用 GitHub 登录"}
          </button>

          <div className="mt-6 space-y-1.5 border-t border-line pt-4 text-[11.5px] text-tx2">
            <p>首次使用需在 GitHub 上授权本应用（仅读取身份）。</p>
            <p className="text-tx3">
              每个浏览器的会话互相隔离；已登录时可在侧栏「切换账号」换成另一个 GitHub 账号。
            </p>
          </div>
        </section>
      </div>
    </div>
  );
}
