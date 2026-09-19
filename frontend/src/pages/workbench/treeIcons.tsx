/** 任务树/焦点栏的线性图标 —— 与用户确认原型 dispatch-tree-linear.html
 *  同款（1.2 描边、圆头、Linear 风），替代 lucide 通用图标保持视觉一致。 */

import type { ReactNode } from "react";

function Ic({ size = 12, className = "", children }: { size?: number; className?: string; children: ReactNode }) {
  return (
    <svg
      viewBox="0 0 24 24"
      width={size}
      height={size}
      className={className}
      fill="none"
      stroke="currentColor"
      strokeWidth={1.2}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden
    >
      {children}
    </svg>
  );
}

/** Manager / Leader / Worker 的「人」形 */
export function IconUser(props: { size?: number; className?: string }) {
  return (
    <Ic {...props}>
      <circle cx="12" cy="8" r="3.5" />
      <path d="M5 20c0-3.5 3-5.5 7-5.5s7 2 7 5.5" />
    </Ic>
  );
}

/** 等待/待审：时钟 */
export function IconClock(props: { size?: number; className?: string }) {
  return (
    <Ic {...props}>
      <circle cx="12" cy="12" r="8" />
      <path d="M12 8v4l2.5 2.5" />
    </Ic>
  );
}

/** 已完成：圈里打勾 */
export function IconCheck(props: { size?: number; className?: string }) {
  return (
    <Ic {...props}>
      <circle cx="12" cy="12" r="8" />
      <path d="m8.5 12 2.5 2.5 4.5-4.5" />
    </Ic>
  );
}

/** 进行中：同心圆 */
export function IconRun(props: { size?: number; className?: string }) {
  return (
    <Ic {...props}>
      <circle cx="12" cy="12" r="8" />
      <circle cx="12" cy="12" r="2.5" />
    </Ic>
  );
}

/** 测试组：烧瓶 */
export function IconFlask(props: { size?: number; className?: string }) {
  return (
    <Ic {...props}>
      <path d="M9 3h6M10 3v6l-5 8.5A2 2 0 0 0 6.7 21h10.6a2 2 0 0 0 1.7-3l-5-8.5V3" />
    </Ic>
  );
}

/** 展开箭头 */
export function IconChevron(props: { size?: number; className?: string }) {
  return (
    <Ic {...props}>
      <path d="m9 6 6 6-6 6" />
    </Ic>
  );
}

/** 输入框发送箭头 */
export function IconSend(props: { size?: number; className?: string }) {
  return (
    <Ic {...props}>
      <path d="M12 19V5M6 11l6-6 6 6" />
    </Ic>
  );
}

/** 全程 AI:闪电(自动、不停顿) */
export function IconBolt(props: { size?: number; className?: string }) {
  return (
    <Ic {...props}>
      <path d="M13 3 5.5 13.5H11l-1 7.5 8-11h-5.5L13 3Z" />
    </Ic>
  );
}

/** 房间:房子轮廓(右栏门牌用,2026-09-18 房间样式提案) */
export function IconHouse(props: { size?: number; className?: string }) {
  return (
    <Ic {...props}>
      <path d="M3 11.5 12 4l9 7.5" />
      <path d="M5.5 10.5V20h13v-9.5" />
      <path d="M10 20v-5.5h4V20" />
    </Ic>
  );
}
