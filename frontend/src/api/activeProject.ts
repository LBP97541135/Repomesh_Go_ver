/** 当前项目 —— 主界面「登录 → 建立/选择项目 → issues 工作台」的上下文。
 *
 *  2026-09-19 用户裁定：`https://crazykitties.cn/` 作为主界面，顺序必须是
 *  先登录、再建立或选择项目、最后进 issues 工作台。issue 必须挂在项目上，
 *  项目自带团队，issue 由该团队处理——所以「选中哪个项目」是主界面的显式状态，
 *  不再由前端盲取列表第一项（旧行为会把需求落进用户没选过的项目）。
 *
 *  存储用 localStorage：项目选择是「这台机器这个人」的浏览上下文，不是平台数据。
 *  隐私模式下 localStorage 不可用时退化成模块级内存，会话内选择仍然有效。 */

const STORAGE_KEY = "repomesh-active-project";

let memoryFallback: string | null = null;

export function readActiveProject(): string | null {
  try {
    return localStorage.getItem(STORAGE_KEY);
  } catch {
    return memoryFallback;
  }
}

export function setActiveProject(projectId: string): void {
  memoryFallback = projectId;
  try {
    localStorage.setItem(STORAGE_KEY, projectId);
  } catch {
    /* 存不进去就只当会话内选择（memoryFallback 已写入） */
  }
}

export function clearActiveProject(): void {
  memoryFallback = null;
  try {
    localStorage.removeItem(STORAGE_KEY);
  } catch {
    /* 同上 */
  }
}
