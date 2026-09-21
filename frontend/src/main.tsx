import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './index.css'
import { applyTheme, readStoredTheme } from './theme.ts'
import ConsoleShell from './ConsoleShell.tsx'
import { AppErrorBoundary } from './components/AppErrorBoundary.tsx'

// 渲染前把主题放到 <html> 上，避免首帧按默认深色闪一下
applyTheme(readStoredTheme())

// 最后一道兜底（2026-09-22）：壳层自己崩了也不留白屏 —— 白屏什么信息都不给，
// 人只能说"网站用不了了"。这里至少把错误原文摆在屏幕上。
createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <AppErrorBoundary label="控制台">
      <ConsoleShell />
    </AppErrorBoundary>
  </StrictMode>,
)
