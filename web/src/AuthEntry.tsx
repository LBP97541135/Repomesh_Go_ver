import { useEffect, useRef, useState } from "react";
import { startAuthorization } from "./api";
import type { ApiError, Purpose } from "./api";
import { forgetAttempt, rememberAttempt, savedAttempt } from "./recovery";
import type { SavedAttempt } from "./recovery";
import { readLoginDestination } from "./routes";
import type { SessionController } from "./session";
import { Account, AuthFrame, ErrorNotice, useRetryDelay } from "./shared";

type StartState =
  | { kind: "idle" }
  | { kind: "recoverable"; attempt: SavedAttempt }
  | { kind: "sending"; attempt: SavedAttempt }
  | { kind: "uncertain"; attempt: SavedAttempt; error: ApiError };

export function AuthEntry({ auth, navigate }: { auth: SessionController; navigate: (path: string) => void }) {
  const [previous] = useState(savedAttempt);
  const [start, setStart] = useState<StartState>(() => previous === null ? { kind: "idle" } : { kind: "recoverable", attempt: previous });
  const [checking, setChecking] = useState(false);
  const busy = useRef(false);
  const alive = useRef(true);
  useEffect(() => {
    alive.current = true;
    return () => { alive.current = false; };
  }, []);
  const session = auth.state.kind === "authenticated" ? auth.state.session : null;
  const retryDelay = useRetryDelay(auth.state.kind === "unavailable" ? auth.state.error.retryAt : start.kind === "uncertain" ? start.error.retryAt : null);

  async function begin(retry?: SavedAttempt) {
    if (busy.current || retryDelay > 0) return;
    if (auth.state.kind !== "anonymous" && auth.state.kind !== "authenticated") return;
    const purpose: Purpose = session === null ? "login" : "reconnect";
    if (retry !== undefined && retry.purpose !== purpose) return;
    busy.current = true;
    const expected = auth.currentGeneration();
    const attempt = retry ?? { attemptId: crypto.randomUUID(), purpose, createdAt: new Date().toISOString(), destination: readLoginDestination() };
    rememberAttempt(attempt);
    window.history.replaceState(null, "", `/auth/result/${attempt.attemptId}`);
    setStart({ kind: "sending", attempt });
    const response = await startAuthorization({ id: attempt.attemptId, purpose, csrfToken: session?.csrfToken, destination: attempt.destination });
    busy.current = false;
    if (!alive.current || !auth.isCurrent(expected)) return;
    if (response.kind === "ok" && response.value.attemptId === attempt.attemptId) {
      auth.announce();
      window.location.assign(response.value.authorizationUrl);
      return;
    }
    const error: ApiError = response.kind === "error" ? response : { kind: "error", code: "INVALID_RESPONSE", status: 200, retryAt: null };
    setStart({ kind: "uncertain", attempt, error });
    if (error.status === 401) auth.unauthorized(expected);
    if (error.code === "SESSION_ALREADY_ACTIVE") void auth.refresh();
  }

  async function continueHome() {
    if (checking) return;
    setChecking(true);
    const current = await auth.refresh();
    if (!alive.current) return;
    setChecking(false);
    if (current?.kind === "ok") window.location.assign("/");
  }

  if (auth.state.kind === "checking") return <AuthFrame title="正在检查登录状态" description="确认当前账号后继续。"><div className="waiting" role="status">正在读取当前会话…</div></AuthFrame>;
  if (auth.state.kind === "unavailable") return <AuthFrame title="暂时无法确认登录状态" description="当前账号尚未核实，请检查连接后再试。"><ErrorNotice error={auth.state.error} /><button className="primary" disabled={retryDelay > 0} onClick={() => void auth.refresh()}>检查当前登录状态</button></AuthFrame>;

  const attempt = start.kind === "idle" ? previous : start.attempt;
  const isReconnect = session !== null;
  return <AuthFrame
    title={start.kind === "sending" ? "正在准备 GitHub 授权" : start.kind === "uncertain" ? "授权发起结果待确认" : start.kind === "recoverable" ? "继续核查原授权尝试" : isReconnect ? "重新连接 GitHub" : "开始使用 RepoMesh"}
    description={isReconnect ? "请继续使用当前绑定的 GitHub 账号。重新连接后，我们会单独确认这次结果。" : "使用 GitHub 登录，查看当前可参与的仓库。"}
    purpose={isReconnect ? "账号连接" : "登录"}
  >
    {session && <Account session={session} />}
    {start.kind === "uncertain" && <ErrorNotice error={start.error} />}
    {start.kind === "sending" ? <div className="waiting" role="status">正在发起授权，请稍候…</div> : start.kind === "uncertain" || start.kind === "recoverable" ? <>
      <button className="primary" disabled={retryDelay > 0} onClick={() => navigate(`/auth/result/${start.attempt.attemptId}`)}>查询原授权尝试</button>
      {start.attempt.purpose === (isReconnect ? "reconnect" : "login") && <button disabled={retryDelay > 0} onClick={() => void begin(start.attempt)}>使用原尝试重新发起授权</button>}
      <button className="text-button" disabled={retryDelay > 0} onClick={() => void begin()}>明确开始一次新的{isReconnect ? "重连" : "登录"}</button>
      <p className="small muted">重新发起会沿用原尝试编号。开始新尝试会使旧的未完成尝试失效。</p>
    </> : <button className="primary" onClick={() => void begin()}>{isReconnect ? "重新连接同一账号" : "使用 GitHub 登录"}</button>}
    {attempt && <a className="recovery-link" href={`/auth/result/${attempt.attemptId}`} onClick={(event) => { event.preventDefault(); navigate(`/auth/result/${attempt.attemptId}`); }}>查看原授权尝试结果</a>}
    {session && <button disabled={checking || start.kind === "sending"} onClick={() => void continueHome()}>{checking ? "正在核对当前账号…" : "稍后处理，返回工作区"}</button>}
    {session && <button className="text-button" disabled={start.kind === "sending"} onClick={() => { forgetAttempt(); void auth.logout(); }}>退出当前账号</button>}
    {start.kind === "uncertain" && <button className="text-button" onClick={() => void auth.refresh(true)}>检查当前登录状态</button>}
  </AuthFrame>;
}
