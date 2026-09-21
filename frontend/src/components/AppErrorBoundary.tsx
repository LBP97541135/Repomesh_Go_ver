import { Component, type ErrorInfo, type ReactNode } from "react";

/** 渲染期兜底：**一个字段缺失不该把整页打没**。
 *
 *  2026-09-22 线上：① 失败的状态块缺了契约声明的字段 → 工作台 render 期抛
 *  TypeError → React 卸载整棵树 → **整页白屏**。用户看到的是"网站用不了了"，
 *  真正的原因只躺在浏览器控制台里，而没人会去开控制台。
 *
 *  这个边界把那种情况变成**看得见的错误**：内容区显示原因与原文、侧栏照常可用、
 *  换页自动重置（外层 `<main>` 的那个 key 随路由变）。同样的判断这个仓库已经
 *  做过一次 —— routes.ts 里对坏 hash 的处理写着"坏段原样返回，比白屏诚实"，
 *  这里是同一句话的渲染期版本。
 *
 *  刻意不做的事：不吞错、不静默重试、不把错误伪装成"加载中"。错误原文必须照呈现，
 *  因为它就是下一步要修的东西。 */
type Props = {
  /** 出错的位置说明，例如"这个页面"、"控制台"。 */
  label?: string;
  children: ReactNode;
};

type State = {
  error: Error | null;
  stack: string;
};

export class AppErrorBoundary extends Component<Props, State> {
  state: State = { error: null, stack: "" };

  static getDerivedStateFromError(error: Error): Partial<State> {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    // 同时打到控制台：出问题时人截图控制台就够，不必再去复现。
    console.error("[repomesh] 渲染失败", error, info.componentStack);
    this.setState({ stack: info.componentStack ?? "" });
  }

  render() {
    const { error, stack } = this.state;
    if (error === null) {
      return this.props.children;
    }
    return (
      <div className="m-6 rounded-hard border border-salmon/40 bg-salmon-well p-4">
        <p className="text-[13px] font-semibold text-salmon">
          {this.props.label ?? "这个页面"}出错了，已停下来，免得显示一份半截界面
        </p>
        <p className="mt-1.5 text-[12px] leading-[1.6] text-tx2">
          {error.message || String(error)}
        </p>
        <details className="mt-2">
          <summary className="cursor-pointer text-[11.5px] text-tx3">
            技术细节（复现问题时把这段贴给开发）
          </summary>
          <pre className="mt-1 max-h-64 overflow-auto whitespace-pre-wrap text-[11px] leading-[1.5] text-tx3">
            {[error.stack ?? "", stack].filter(Boolean).join("\n\n")}
          </pre>
        </details>
        <div className="mt-3 flex gap-2">
          <button
            type="button"
            className="rounded-hard border border-line px-3 py-1 text-[12px] text-tx2 hover:text-tx"
            onClick={() => this.setState({ error: null, stack: "" })}
          >
            重试这一步
          </button>
          <button
            type="button"
            className="rounded-hard border border-line px-3 py-1 text-[12px] text-tx2 hover:text-tx"
            onClick={() => window.location.reload()}
          >
            重新加载页面
          </button>
        </div>
      </div>
    );
  }
}
