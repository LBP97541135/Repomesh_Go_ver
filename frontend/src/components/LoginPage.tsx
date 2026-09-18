import { useState } from "react";
import { startGithubLogin, type Account } from "../api/auth";

/** 登录门（2026-09-16 裁定：GitHub OAuth，本地账号体系作废）。
 *
 *  流程：点击 → `POST /api/auth/github/login`（幂等键 + destination home）→
 *  跳转 GitHub 授权页 → 回调由后端换令牌并种会话 cookie → 重定向回首页，
 *  ConsoleShell 用 `GET /api/session` 确认登录态。 */
/** onAuthenticated 保留兼容旧签名：OAuth 流程经整页跳转回首页后由
 *  ConsoleShell 重新拉取会话，本组件内不再使用。 */
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
    <div className="grid h-screen place-items-center bg-ink px-6">
      <div className="w-full max-w-[560px]">
        <div className="mb-7 flex items-center gap-3">
          <span className="grid size-[38px] flex-none place-items-center rounded-hard bg-amber font-mono text-[17px] font-extrabold text-on-amber">
            R
          </span>
          <div>
            <strong className="block font-mono text-[14px] tracking-[0.14em] text-cream">REPOMESH</strong>
            <span className="microlabel">交付控制平面 · 本地部署</span>
          </div>
        </div>

        <div className="rounded-hard border border-line bg-panel px-7 pt-6 pb-7 shadow-float">
          <h1 className="text-[15px] font-semibold text-cream">登录控制平面</h1>
          <p className="mt-1 mb-4 text-[12px] text-tx2">
            使用 GitHub 账号登录。会话由后端 httpOnly cookie 持有，前端不存任何凭据。
          </p>

          {error && (
            <div className="mt-4 rounded-hard border border-salmon-deep bg-salmon-well px-3 py-2 text-[12px] text-salmon-hi">
              {error}
            </div>
          )}

          <button
            className="mt-5 w-full rounded-hard bg-amber py-[9px] text-[13px] font-extrabold tracking-[0.04em] text-on-amber hover:bg-amber-hi disabled:opacity-60"
            onClick={login}
            disabled={busy}
          >
            {busy ? "正在跳转 GitHub…" : "使用 GitHub 登录"}
          </button>

          <p className="mt-3 text-[11.5px] text-tx3">
            首次使用需要在 GitHub 上授权本应用（仅读取身份）。回调地址：/api/auth/github/callback
          </p>
        </div>
      </div>
    </div>
  );
}
