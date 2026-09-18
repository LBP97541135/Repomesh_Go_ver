import { useEffect, useRef, useState } from "react";
import { readAttempt } from "./api";
import type { ApiError, Attempt } from "./api";
import { savedAttempt } from "./recovery";
import { canContinueToDestination, clearLoginDestination } from "./routes";
import type { SessionController, SessionState } from "./session";
import { Account, AuthFrame, ErrorNotice, Observation, useRetryDelay } from "./shared";

type ResultState =
  | { kind: "loading" }
  | { kind: "received"; attempt: Attempt }
  | { kind: "unavailable"; error: ApiError };

function explanation(attempt: Attempt, current: SessionState): { title: string; description: string } {
  if (attempt.reasonCode === "ACCOUNT_MISMATCH") return { title: "请选择原来的 GitHub 账号", description: "这次选择的账号与当前绑定账号不同，未更换绑定。请重新连接同一账号。" };
  if (attempt.state === "superseded" || attempt.reasonCode === "NEWER_ATTEMPT") return { title: "已有更新的授权尝试", description: "请返回最新发起授权的页面。这次旧尝试不能继续完成授权。" };
  if (attempt.state === "cancelled") return { title: "这次授权未完成", description: "你已取消 GitHub 授权，可以稍后再试。" };
  if (attempt.state === "expired") return { title: "这次授权已过期", description: "请明确开始一次新的授权，再返回确认结果。" };
  if (attempt.state === "rejected") return { title: "无法完成这次授权", description: "浏览器绑定或授权验证未通过。请从当前浏览器重新开始。" };
  if (attempt.state === "confirmed" && current.kind === "anonymous") return { title: "当前登录尚未确认", description: "授权尝试已经确认，但当前浏览器没有有效登录。请重新登录，授权回执不能恢复登录凭据。" };
  if (attempt.state === "confirmed" && current.kind !== "authenticated") return { title: "当前登录状态待确认", description: "授权尝试已经确认，但暂时无法读取当前登录状态。请重新查询后继续。" };
  if (attempt.state === "confirmed" && attempt.purpose === "reconnect" && attempt.connection !== null) return attempt.connection.isCurrent
    ? { title: "本次连接已确认", description: "已取得这次重新连接对应的回执。继续前会再次核对当前账号。" }
    : { title: "本次已完成，连接后来有更新", description: "这次重连回执属于较早的连接。返回工作区后将读取当前连接与仓库资格。" };
  if (attempt.state === "confirmed" && attempt.purpose === "login") return { title: "当前登录已确认", description: "请核对当前账号，再明确继续进入工作区。" };
  if (attempt.state === "pending") return { title: "等待 GitHub 授权", description: "完成授权后将返回这里。可以查询这次结果，或稍后再处理。" };
  return { title: attempt.purpose === "reconnect" ? "连接结果待确认" : "授权结果待确认", description: attempt.purpose === "reconnect" ? "已有登录或连接有效，也不能证明这次重新连接成功。请查询本次结果。" : "暂时无法确认这次授权结果。已保留原尝试，请继续查询。" };
}

export function AuthResult({ id, auth, navigate }: { id: string; auth: SessionController; navigate: (path: string) => void }) {
  const [result, setResult] = useState<ResultState>({ kind: "loading" });
  const [busy, setBusy] = useState(false);
  const [note, setNote] = useState("");
  const query = useRef<(proceed?: boolean) => void>(() => {});
  const previous = savedAttempt();
  const purpose = result.kind === "received" ? result.attempt.purpose : previous?.attemptId === id ? previous.purpose : null;
  const session = auth.state.kind === "authenticated" ? auth.state.session : null;
  const retryDelay = useRetryDelay(result.kind === "unavailable" ? result.error.retryAt : null);
  const { generation, refresh, isCurrent, currentGeneration, unauthorized } = auth;

  useEffect(() => {
    let disposed = false;
    let inFlight = false;
    let timer: number | undefined;
    let retryAt = 0;
    let pollingStep = 0;
    let controller: AbortController | null = null;
    const delays = [2000, 5000, 10000, 30000];
    const schedule = (delay: number) => {
      window.clearTimeout(timer);
      if (!disposed && document.visibilityState === "visible") timer = window.setTimeout(() => void run(), delay);
    };
    async function run(proceed = false) {
      if (disposed || inFlight || Date.now() < retryAt || document.visibilityState !== "visible") return;
      inFlight = true;
      window.clearTimeout(timer);
      setBusy(true);
      setNote("");
      const startedGeneration = currentGeneration();
      const current = await refresh();
      if (disposed || currentGeneration() !== startedGeneration) return;
      if (current?.kind === "error" && current.status === 429) {
        inFlight = false;
        setBusy(false);
        setResult({ kind: "unavailable", error: current });
        retryAt = current.retryAt ?? Date.now() + 30000;
        schedule(Math.max(0, retryAt - Date.now()));
        return;
      }
      const expected = currentGeneration();
      controller = new AbortController();
      const response = await readAttempt({ id, signal: controller.signal });
      if (disposed || !isCurrent(expected)) return;
      inFlight = false;
      setBusy(false);
      if (response.kind === "error") {
        if (response.status === 401) {
          unauthorized(expected);
          navigate("/login");
          return;
        }
        setResult({ kind: "unavailable", error: response });
        retryAt = response.retryAt ?? (response.status === 429 ? Date.now() + 30000 : 0);
        if (response.status === 429) schedule(Math.max(0, retryAt - Date.now()));
        else if (response.status === 0 || response.status >= 500) schedule(Math.max(30000, retryAt - Date.now()));
        return;
      }
      if (response.value.attemptId !== id) {
        setResult({ kind: "unavailable", error: { kind: "error", code: "INVALID_RESPONSE", status: 200, retryAt: null } });
        return;
      }
      setResult({ kind: "received", attempt: response.value });
      if (proceed) {
        if (current?.kind === "ok" && canContinueToDestination(response.value, current.value)) {
          clearLoginDestination();
          window.location.assign(response.value.nextPage ?? "/");
          return;
        }
        setNote("当前登录、这次回执或返回资格尚未全部确认，请停留在此页重新查询。");
      }
      const attempt = response.value;
      if (attempt.state === "pending" || attempt.state === "unknown" || (attempt.state === "confirmed" && attempt.nextPage === null && current?.kind === "ok")) {
        schedule(delays[Math.min(pollingStep, delays.length - 1)]);
        pollingStep += 1;
      }
    }
    query.current = (proceed = false) => { void run(proceed); };
    const onVisibility = () => {
      if (document.visibilityState === "hidden") window.clearTimeout(timer);
      else schedule(Math.max(0, retryAt - Date.now()));
    };
    document.addEventListener("visibilitychange", onVisibility);
    void run();
    return () => {
      disposed = true;
      controller?.abort();
      window.clearTimeout(timer);
      document.removeEventListener("visibilitychange", onVisibility);
    };
  }, [id, generation, refresh, isCurrent, currentGeneration, unauthorized, navigate]);

  const copy = result.kind === "received" ? explanation(result.attempt, auth.state) : result.kind === "unavailable"
    ? { title: "暂时无法读取授权结果", description: "已保留原尝试编号，可以继续查询。" }
    : { title: "正在核对授权结果", description: "分别检查当前登录与这次授权尝试。" };
  return <AuthFrame title={copy.title} description={copy.description} purpose={purpose === "reconnect" ? "账号连接" : "登录结果"}>
    {session && <Account session={session} />}
    {result.kind === "unavailable" && <ErrorNotice error={result.error} />}
    {auth.state.kind === "unavailable" && <ErrorNotice error={auth.state.error} />}
    {result.kind === "received" && result.attempt.state === "confirmed" && result.attempt.nextPage === null && session && <p className="notice">返回资格尚未确认，请留在此页查询。</p>}
    {result.kind === "received" && canContinueToDestination(result.attempt, session) ? <button className="primary" disabled={busy} onClick={() => query.current(true)}>{busy ? "正在重新核对…" : "确认当前账号，继续"}</button> : <button className="primary" disabled={busy || retryDelay > 0} onClick={() => query.current()}>{busy ? "正在查询…" : purpose === "reconnect" ? "查询本次连接结果" : "查询本次授权结果"}</button>}
    {session ? <>
      <button disabled={busy} onClick={() => window.location.assign("/")}>稍后处理，返回工作区</button>
      <button className="text-button" disabled={busy || retryDelay > 0} onClick={() => navigate("/login")}>重新连接同一账号</button>
      <button className="text-button" disabled={busy} onClick={() => void auth.logout()}>退出当前账号</button>
    </> : <button disabled={busy || retryDelay > 0} onClick={() => navigate("/login")}>返回登录入口</button>}
    <p className="small muted">确认后只返回页面，不会重新提交任何业务操作。</p>
    <div className="attempt-details"><span>本次尝试</span><code>{id}</code>{result.kind === "received" && <small>结果观察于 <Observation at={result.attempt.observedAt} /></small>}</div>
    <p className="small notice-text" role="status" aria-live="polite">{note}</p>
  </AuthFrame>;
}
