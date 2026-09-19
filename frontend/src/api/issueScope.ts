/** issue 仓库范围的追加写面：`POST /api/projects/{pid}/issues/{iid}/scope/repositories`。
 *
 *  这是 ③「执行中人工打断 / 动态引入新仓库」的落点：**人确认之后**范围才扩张
 *  （用户已裁定：新仓库必须人工确认才生效，不允许 agent 自己改范围）。
 *
 *  服务端两条前置会显式拒绝并给出可自救的错误，这里原样上抛、不做自动重试：
 *   · 409 REPOSITORY_NOT_IN_PROJECT —— 仓库还没挂在本项目上（挂仓库是另一个动作）
 *   · 404 RESOURCE_NOT_FOUND —— issue 不存在或已删 */
import { apiRequest } from "./http";

export function appendIssueRepository(
  projectId: string,
  issueId: string,
  repositoryId: string,
): Promise<{ status: string }> {
  return apiRequest<{ status: string }>(
    "POST",
    `/projects/${encodeURIComponent(projectId)}/issues/${encodeURIComponent(issueId)}/scope/repositories`,
    { repositoryId },
  );
}