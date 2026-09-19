import { useEffect, useState } from "react";
import type { FormEvent } from "react";
import { readRepositories } from "./api";
import type { ApiError, RepositoryPage, Session } from "./api";
import { savedAttempt } from "./recovery";
import type { SessionController } from "./session";
import { ErrorNotice, Mark, Observation, useRetryDelay } from "./shared";

type PageState =
  | { kind: "loading" }
  | { kind: "ready"; page: RepositoryPage }
  | { kind: "unavailable"; error: ApiError };

type PageRequest = { query: string; cursor: string | null; previous: (string | null)[]; revision: number; refresh: boolean; autoRetries: number };

export function RepositoryHome({ session, auth, navigate }: { session: Session; auth: SessionController; navigate: (path: string) => void }) {
  const [search, setSearch] = useState("");
  const [request, setRequest] = useState<PageRequest>({ query: "", cursor: null, previous: [], revision: 0, refresh: false, autoRetries: 0 });
  const [page, setPage] = useState<PageState>({ kind: "loading" });
  const [previousAttempt] = useState(savedAttempt);
  const { currentGeneration, isCurrent, unauthorized } = auth;
  const retryDelay = useRetryDelay(page.kind === "unavailable" ? page.error.retryAt : null);

  useEffect(() => {
    const expected = currentGeneration();
    let cancelled = false;
    let retryTimer = 0;
    setPage({ kind: "loading" });
    void readRepositories({ query: request.query, cursor: request.cursor, refresh: request.refresh }).then((response) => {
      if (cancelled || !isCurrent(expected)) return;
      if (response.kind === "error") {
        if (response.status === 401) unauthorized(expected);
        if ((response.code === "RESULT_UNCONFIRMED" || response.code === "ABORTED" || response.code === "NETWORK_ERROR") && request.autoRetries < 3) {
          retryTimer = window.setTimeout(() => {
            if (cancelled || !isCurrent(expected)) return;
            setRequest((before) => ({ ...before, revision: before.revision + 1, refresh: false, autoRetries: before.autoRetries + 1 }));
          }, 2000);
          return;
        }
        setPage({ kind: "unavailable", error: response });
      } else setPage({ kind: "ready", page: response.value });
    });
    return () => {
      cancelled = true;
      if (retryTimer !== 0) window.clearTimeout(retryTimer);
    };
  }, [request, currentGeneration, isCurrent, unauthorized]);

  function submitSearch(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (retryDelay > 0 || Array.from(search).length > 200) return;
    setRequest((before) => ({ query: search, cursor: null, previous: [], revision: before.revision + 1, refresh: false, autoRetries: 0 }));
  }

  const connectionLabel = { connected: "GitHub 已连接", missing: "GitHub 连接待恢复", unknown: "GitHub 连接待确认" }[session.githubConnection.status];
  return <div className="workspace">
    <header className="workspace-header"><a className="brand" href="/" onClick={(event) => { event.preventDefault(); navigate("/"); }}><Mark /><span>RepoMesh</span></a><span className="header-divider">/</span><span className="muted">工作区</span><button className="signout" onClick={() => void auth.logout()}>退出登录</button></header>
    <main className="workspace-main">
      <div className="page-heading"><div><p className="eyebrow">你的工作区</p><h1>仓库与账号连接</h1><p className="muted">查看当前账号，以及已发现并核实可读的仓库。</p></div><div className="heading-actions"><button className="outlined" onClick={() => { window.location.href = "/"; }}>Issue 交付控制台</button><button className="outlined" onClick={() => navigate("/projects")}>项目</button><button className="outlined" onClick={() => navigate("/settings/models")}>模型设置</button><button className="primary compact" onClick={() => navigate("/projects/new")}>新建项目</button><span className="pill good">已登录</span></div></div>
      <section className="connection-card" aria-labelledby="account-title"><div className="avatar" aria-hidden="true">{Array.from(session.user.displayName)[0]}</div><div className="connection-account"><h2 id="account-title">{session.user.displayName}</h2><p><span className={session.githubConnection.status === "connected" ? "status-dot good-dot" : "status-dot amber-dot"} />{connectionLabel}</p><p className="small muted">连接观察于 <Observation at={session.githubConnection.observedAt} /></p></div><button className="outlined" onClick={() => navigate("/login")}>重新连接 GitHub</button></section>
      {session.githubConnection.status !== "connected" && <p className="notice">{session.githubConnection.status === "missing" ? "GitHub 连接需要恢复。请重新连接当前绑定的账号。" : "目前无法确认 GitHub 连接状态。可以重新读取当前登录，或显式重连同一账号。"}<button className="inline-button" onClick={() => void auth.refresh()}>检查当前登录状态</button></p>}
      {previousAttempt && <div className="recovery-row"><span>有一条可查询的授权尝试记录</span><a href={`/auth/result/${previousAttempt.attemptId}`} onClick={(event) => { event.preventDefault(); navigate(`/auth/result/${previousAttempt.attemptId}`); }}>查询原尝试结果</a></div>}
      <section className="repositories" aria-labelledby="repositories-title"><div className="section-heading"><div><h2 id="repositories-title">当前发现的仓库</h2><p className="small muted">用户读取资格和 App 工作授权分别核实。</p></div><button disabled={retryDelay > 0 || page.kind === "loading"} onClick={() => setRequest((before) => ({ ...before, cursor: null, previous: [], revision: before.revision + 1, refresh: true, autoRetries: 0 }))}>重新发现</button></div>
        <form className="search-form" role="search" onSubmit={submitSearch}><label className="sr-only" htmlFor="repository-search">搜索已发现的仓库</label><svg aria-hidden="true" viewBox="0 0 24 24" fill="none"><circle cx="10.5" cy="10.5" r="6.5" /><path d="m16 16 4.5 4.5" /></svg><input id="repository-search" type="search" placeholder="搜索已发现的仓库" value={search} onChange={(event) => setSearch(event.target.value)} /><button type="submit" disabled={retryDelay > 0 || Array.from(search).length > 200}>搜索</button></form>
        {page.kind === "loading" && <div className="empty-state" role="status"><span className="spinner" aria-hidden="true" /><p>正在读取当前可披露的仓库…</p></div>}
        {page.kind === "unavailable" && <div className="repository-error"><ErrorNotice error={page.error} /><p className="small muted">当前未取得可靠列表，不能据此判断账号没有仓库。</p><button className="outlined" disabled={retryDelay > 0} onClick={() => setRequest((before) => ({ ...before, cursor: null, previous: [], revision: before.revision + 1, refresh: false, autoRetries: 0 }))}>{page.error.code === "CURSOR_EXPIRED" || page.error.code === "INVALID_CURSOR" ? "从第一页重新读取" : "重试读取仓库"}</button></div>}
        {page.kind === "ready" && <>
          <div className="coverage" role="status"><span className={page.page.coverage.status === "complete" ? "pill good" : "pill amber"}>{page.page.coverage.status === "complete" ? "当前发现范围完整" : page.page.coverage.status === "partial" ? "部分发现" : "发现覆盖待确认"}</span><p>{page.page.coverage.status === "partial" ? "当前只展示已发现并核实可读的仓库，可能还有未发现的仓库。" : page.page.coverage.status === "unknown" ? "暂时无法确认发现范围，当前列表不代表账号的全部仓库。" : "当前发现范围已完成核查，每个仓库的资格仍会重新核实。"}</p>{page.page.coverage.reasonCodes.includes("APP_INSTALLATION_SCOPE") && <p>单一 App 安装范围外的仓库可能缺席。请联系部署管理员补齐 App 工作授权。</p>}</div>
          {page.page.items.length === 0 ? <div className="empty-state"><p>{page.page.nextCursor !== null ? "这一页暂时没有可披露的仓库" : request.query ? "当前发现结果中没有匹配仓库" : "当前没有可展示的仓库"}</p><span className="small muted">{page.page.nextCursor !== null ? "发现结果仍有后续页面，可以继续翻页。" : page.page.coverage.status === "complete" ? "可以重新发现，检查后续变化。" : "发现覆盖尚不完整，不能据此认定账号没有其他仓库。"}</span></div> : <ul className="repository-list">{page.page.items.map((repository) => <li key={repository.id}><span className="repo-icon" aria-hidden="true"><svg viewBox="0 0 24 24" fill="none"><path d="M5 3h12a2 2 0 0 1 2 2v15H6a3 3 0 0 1-3-3V5a2 2 0 0 1 2-2Z" /><path d="M3 17a3 3 0 0 1 3-3h13M7 6v5" /></svg></span><div className="repo-name"><strong>{repository.displayName}</strong><span className="small muted">读取资格已核实 · <Observation at={repository.userParticipation.observedAt} /></span></div><span className={repository.appCapability.status === "allowed" ? "pill good" : "pill amber"}>{repository.appCapability.status === "allowed" ? "App 能力已核实" : repository.appCapability.status === "denied" ? "App 工作授权不足" : "App 能力待确认"}</span></li>)}</ul>}
          <div className="pagination"><span className="small muted">第 {request.previous.length + 1} 页 · 本页 {page.page.items.length} 个仓库</span><div><button disabled={request.previous.length === 0} onClick={() => setRequest((before) => ({ ...before, cursor: before.previous[before.previous.length - 1] ?? null, previous: before.previous.slice(0, -1), refresh: false, autoRetries: 0 }))}>上一页</button><button disabled={page.page.nextCursor === null} onClick={() => { const next = page.page.nextCursor; if (next !== null) setRequest((before) => ({ ...before, cursor: next, previous: [...before.previous, before.cursor], refresh: false, autoRetries: 0 })); }}>下一页</button></div></div>
          {page.page.nextCursor === null && <p className="small muted end-note">{page.page.coverage.status === "complete" ? "当前发现范围已全部加载。" : "已到当前发现结果末页，账号仍可能有未发现的仓库。"}</p>}
          <p className="small muted">发现观察于 <Observation at={page.page.coverage.observedAt} /></p>
        </>}
      </section>
      <footer className="workspace-footer">项目管理已开放；Issue 创建和运行准备仍未实现。</footer>
    </main>
  </div>;
}
