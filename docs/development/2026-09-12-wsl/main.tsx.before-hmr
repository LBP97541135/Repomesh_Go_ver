import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import "./style.css";

function App() {
  return (
    <main>
      <header>
        <span className="mark" aria-hidden="true">
          R
        </span>
        <span>RepoMesh</span>
        <span className="stage">工程骨架</span>
      </header>
      <section aria-labelledby="title">
        <p className="eyebrow">多仓库协作 · 开发起点</p>
        <h1 id="title">从这里建立协作。</h1>
        <p className="intro">
          React、TypeScript 与 Go
          的基础工程已建立。当前页面仅说明实现范围，不展示实时运行状态。
        </p>
        <div className="processes">
          <article>
            <span className="number">01</span>
            <h2>Web</h2>
            <p>已提供静态页面与进程诊断入口。</p>
            <span className="tag">基础入口</span>
          </article>
          <article>
            <span className="number">02</span>
            <h2>后台协调</h2>
            <p>仅有可构建入口，任务处理尚未实现。</p>
            <span className="tag muted">未实现</span>
          </article>
          <article>
            <span className="number">03</span>
            <h2>受限主机执行</h2>
            <p>仅有可构建入口，主机操作尚未启用。</p>
            <span className="tag muted">未实现</span>
          </article>
        </div>
        <aside>
          <h2>当前范围</h2>
          <p>
            Issue、计划、权限、调度、GitHub 与 AgentTeams
            集成均未实现。仓库分析插件尚未实现，Skill 工程继续暂缓。
          </p>
        </aside>
      </section>
      <footer>骨架可构建不代表业务或 AgentTeams 集成验收完成。</footer>
    </main>
  );
}

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
