/** GitHub App 安装状态域（Go：`GET /api/access/app-installation`，只读，需登录）。
 *
 *  **按域拆分的样板文件**：走 `apiRequest`，不往 client.ts / http.ts 加东西。
 *
 *  这个读面存在的理由（用户 2026-09-20 诉求）：别让「要装 GitHub App」这件事在使用
 *  过程中以一句报错的形式冒出来（建 issue 时突然 `NO_AVAILABLE_REPOSITORIES`），
 *  而是在**一开始就指引清楚**。
 *
 *  为什么必须由人去网页上点：agent 要以平台身份推代码、开 PR，用的是 App 的
 *  installation token，而 App 的权限来自它被**安装**在账号上——安装粒度是**账号**
 *  （个人号与组织是各自独立的安装目标，装在个人号上覆盖不到组织名下的仓）。
 *  GitHub **没有**创建安装的 API，所以后端能给的是一组**可点的链接**
 *  （`installUrl` **直接用，不要自己拼**）。
 *
 *  形状唯一来源：`internal/access/app_installation.go` 的 json tag（已用真凭据实测）。 */
import { apiRequest } from "./http";

/** 一个需要（或已经）装了 App 的账号——界面上就是一行。
 *  粒度是**账号**不是仓库：一个组织装一次（选了 All repositories 的话）就覆盖
 *  它名下所有仓，所以这个读面回答的是「还差哪几个账号」。 */
export interface AppInstallationTarget {
  login: string;
  /** `user | organization`。组织名下的仓，装在个人号上覆盖不到。 */
  kind: "user" | "organization";
  /** false = 这个账号完全没装。 */
  installed: boolean;
  /** `all | selected`，**只在 installed:true 时有意义**。
   *  selected = 只装了部分仓，缺的要去那个 installation 的设置页补勾。 */
  repositorySelection?: string;
  /** 安装被挂起 = 等于没覆盖。**不是「没装」**，界面别把这两种态合成一句。 */
  suspended?: boolean;
  /** 没装时 = 安装页直链（带 suggested_target_id，点进去直接落在那个账号上）；
   *  已装时 = 那个 installation 的**设置页**。 */
  installUrl: string;
  /** 当前用户有没有权限在这个账号上装。
   *  **目前恒为 null**——判断组织角色要 `read:org` scope，而本部署的登录流程刻意
   *  不申请任何 scope。所以界面**不做**「你有权/你无权」的判断，只给中性文案。
   *  字段保留在契约里，将来加 scope 时形状不用再改。 */
  canInstall: boolean | null;
  /** 这个账号名下、**当前用户项目里**涉及到的仓（`owner/name`）。 */
  repositories: string[];
}

export interface AppInstallationView {
  /** 按 login 升序。 */
  targets: AppInstallationTarget[];
  /** 还没被任何安装覆盖到的仓数。**0 = 这件事不该再出现在界面上**。 */
  uncoveredCount: number;
  /** 非空 = App 侧探测失败（部署没配 App 凭据 / GitHub 调不通 / 取 App 信息失败）。
   *  **要如实显示这句话，不要显示成「没装」**——那是在撒谎。 */
  unavailable?: string;
}

/** GET /api/access/app-installation — 我的仓归哪些账号所有、哪些还没装 App。
 *  只读：这里不创建安装（GitHub 也没有这个 API）。 */
export function fetchAppInstallation(): Promise<AppInstallationView> {
  return apiRequest<AppInstallationView>("GET", "/access/app-installation");
}
