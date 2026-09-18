import { useEffect, useState } from "react";
import type { ReactNode } from "react";
import type { ApiError, Session } from "./api";
import { errorMessage } from "./api";

export function Mark({ large = false }: { large?: boolean }) {
  return <span className={large ? "mark mark-large" : "mark"} aria-hidden="true"><svg viewBox="0 0 24 24" fill="none"><rect x="3" y="3" width="6" height="6" rx="1.5" /><rect x="15" y="3" width="6" height="6" rx="1.5" /><rect x="3" y="15" width="6" height="6" rx="1.5" /><rect x="15" y="15" width="6" height="6" rx="1.5" /><path d="M9 6h6M6 9v6m12-6v6m-9 3h6" /></svg></span>;
}

export function AuthFrame({ title, description, children, purpose = "登录" }: { title: string; description: string; children: ReactNode; purpose?: string }) {
  return <main className="auth-stage"><section className="auth-box" aria-labelledby="auth-title"><Mark large /><p className="auth-kicker">RepoMesh · {purpose}</p><h1 id="auth-title">{title}</h1><p className="intro">{description}</p>{children}<p className="auth-foot">登录账号与仓库工作授权分别管理。</p></section></main>;
}

export function Account({ session }: { session: Session }) {
  return <div className="account"><span>当前账号</span><strong>{session.user.displayName}</strong></div>;
}

export function useRetryDelay(retryAt: number | null): number {
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    if (retryAt === null) return;
    setNow(Date.now());
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [retryAt]);
  return retryAt === null ? 0 : Math.max(0, Math.ceil((retryAt - now) / 1000));
}

export function ErrorNotice({ error }: { error: ApiError }) {
  const seconds = useRetryDelay(error.retryAt);
  return <p className="notice" role="alert">{errorMessage(error)}{seconds > 0 && <span className="retry-note">{seconds} 秒后可重试。</span>}</p>;
}

export function Observation({ at }: { at: string | null }) {
  if (at === null) return <span>尚无可靠观察时间</span>;
  return <time dateTime={at}>{new Date(at).toLocaleString("zh-CN", { hour12: false })}</time>;
}
