import type { ReactNode } from "react";
import type { AuthenticatedPageProps } from "./projectPages";
import { Mark } from "./shared";

export type ProjectShellProps = AuthenticatedPageProps & { title: string; children: ReactNode; projectId?: string };

export function ProjectShell({ session, auth, navigate, title, children, projectId }: ProjectShellProps) {
  return <div className="workspace project-workspace">
    <header className="workspace-header">
      <a className="brand" href="/" onClick={(event) => { event.preventDefault(); navigate("/"); }}><Mark /><span>RepoMesh</span></a>
      <span className="header-divider">/</span><span className="muted route-title">{title}</span>
      <nav className="project-nav" aria-label="项目导航">
        <a href="/projects" onClick={(event) => { event.preventDefault(); navigate("/projects"); }}>项目</a>
        {projectId && <a href={`/projects/${encodeURIComponent(projectId)}`} onClick={(event) => { event.preventDefault(); navigate(`/projects/${encodeURIComponent(projectId)}`); }}>项目概览</a>}
        {projectId && <a href={`/projects/${encodeURIComponent(projectId)}/settings`} onClick={(event) => { event.preventDefault(); navigate(`/projects/${encodeURIComponent(projectId)}/settings`); }}>项目设置</a>}
      </nav>
      <span className="header-account" title="当前账号">{session.user.displayName}</span>
      <button className="signout" onClick={() => void auth.logout()}>退出登录</button>
    </header>
    <main className="workspace-main project-main">{children}</main>
  </div>;
}
