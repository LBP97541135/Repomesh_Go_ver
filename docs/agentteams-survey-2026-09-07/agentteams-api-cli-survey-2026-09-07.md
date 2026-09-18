# AgentTeams API、CLI、Matrix 与 Harness 接口调查

> 日期：2026-09-07  
> 调查对象：`agentscope-ai/AgentTeams`  
> 锁定提交：`eeaab64391ccaec9118e84977f538aefd40720d6`  
> 调查方式：只读静态核查官方源码、官方文档及官方 Dashboard 源码；未安装、部署或进行真实接口调用。
> 定位：这是一份供实现和契约核对使用的技术附录；产品和架构讨论请先读[人话版调研](./agentteams-survey.md)。

> 2026-09-09 后续索引：最新 main 重新核对仍为本文锁定提交，最新 release 为 v1.2.3。已采用的原生 DAG 复用、RepoMesh 跨仓／Loop 分工及后续验证证据集中见 [Graph／Loop 专题](../current/graph-loop-design.md)。本文继续保留静态接口依据，不以工具能力存在宣称受控派工、原生 Loop 全权执行或 RepoMesh 验收已经完成。

## 1. 结论摘要

1. **AgentTeams 确实公开了一套可直接调用的 Controller HTTP API。** 主入口是 `agentteams-controller` 的 `/api/v1`，默认监听 `:8090`；涵盖 Worker、Team、Human、Manager CRUD，Worker 生命周期，Project 工作流读取与人工干预，包上传、网关消费者、短期凭据和 Matrix AppService 管理。[路由源码](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/internal/server/http.go) / [配置源码](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/internal/config/config.go)
2. **`agt` 是 Controller REST API 的官方运维 CLI。** 它不是另一个调度引擎；除 `llm-preflight` 外，主要命令都是 REST 的薄封装。[CLI 根命令](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/cmd/agt/main.go) / [HTTP 客户端](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/cmd/agt/client.go)
3. **另有一个同名体系下的 `agentteams` 插件 CLI，不要与 `agt` 混淆。** 它只管理本地 `.agentteams/` 插件目录，支持 `plugin install/list/update/uninstall`，不调用 Controller，也不管理集群或 Worker 生命周期。[插件 CLI 源码](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/plugins/cli/src/agentteams_cli/main.py) / [插件说明](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/plugins/README.md)
4. **Matrix 是 AgentTeams 的通信总线，也是会话隔离的重要实现边界。** QwenPaw Matrix Channel 将房间映射为 `session_id = matrix:{room_id}`，并把 `room_id` 同时作为请求 `user_id`，使同一房间参与者共享一个会话、不同房间天然隔离；未触发机器人的群聊消息另有每房间、默认 50 条的临时上下文缓冲。[QwenPaw Matrix Channel](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/plugins/agentteams-matrix-channel/agentteams_matrix/channel.py)
5. **TeamHarness 已有实际 MCP 工具面，但其插件清单仍是 `v1alpha1`、版本 `0.1.0`。** 工具包括 `message`、`roomflow`、`filesync`、`artifact`、`projectflow`、`taskflow`；业务状态主要保存在对象存储的共享目录，不是 Controller CRD 中的 Project 资源。[TeamHarness 清单](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/plugins/teamharness/plugin.yaml) / [MCP server](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/plugins/teamharness/mcp/server.py)
6. **AgentTeams 不能被表述为已经正式接入 Codex、Claude Code、Qoder 等通用 coding harness。** 当前真正可工作的主适配器是 QwenPaw；DeepSeek Harness 是实际接入但明确标注为 experimental。`adapters/claude-code` 只是占位 README 和写日志的空壳脚本。另有 `skills-alpha/coding-cli-management` 可由 Manager 代 Worker 调用 `claude`、`gemini`、`qodercli`，但这是 alpha 技能、权限全自动模式，不是正式 TeamHarness Claude Code adapter，也不含 Codex。[Claude Code 占位适配器](https://github.com/agentscope-ai/AgentTeams/tree/eeaab64391ccaec9118e84977f538aefd40720d6/plugins/teamharness/adapters/claude-code) / [coding-cli alpha skill](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/manager/agent/skills-alpha/coding-cli-management/SKILL.md) / [执行脚本](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/manager/agent/skills-alpha/coding-cli-management/scripts/run-coding-cli.sh)
7. **没有找到 Controller REST 的 OpenAPI/Swagger、Swagger UI 或官方生成 SDK。** 仓库中的 `openAPIV3Schema` 是 Kubernetes CRD schema，不是 `/api/v1` 的 OpenAPI；REST 契约目前由 Go 路由、请求结构、手写 Markdown、`agt` 和 Dashboard 消费代码共同定义。[Controller 路由](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/internal/server/http.go) / [REST DTO](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/internal/server/types.go) / [CRD schema 目录](https://github.com/agentscope-ai/AgentTeams/tree/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/config/crd)
8. **对 RepoMesh 最适合的复用方式是“锁版本的服务端适配器”，不是直接把 `agt` 或 Dashboard 当产品后端。** RepoMesh 应通过自己的控制面调用每个 AgentTeams 实例的窄 REST 子集，并保存 RepoMesh 自己的多仓项目、仓库授权、Issue、计划、任务、执行尝试、队列与额度状态。

基线核对：调查时官方 `origin/main` 与交接锁定提交完全相同，均为 `eeaab64391ccaec9118e84977f538aefd40720d6`，因此本报告不需要额外区分 AgentTeams 当前 `main`。该提交位于 `v1.2.3` 之后 5 个提交。官方配套 Dashboard 版本为独立仓库的 `v1.2.4`；其当前 `main` 已继续加入 HITL 和 `org.agentteams.run v1` 治理能力，不能反推为锁定 AgentTeams Controller 已拥有的契约。[AgentTeams 提交](https://github.com/agentscope-ai/AgentTeams/commit/eeaab64391ccaec9118e84977f538aefd40720d6) / [Dashboard v1.2.4](https://github.com/agentteams-group/agentteams-dashboard/tree/v1.2.4)

## 2. 接口分层

```text
RepoMesh（建议新增的上层控制面）
  |
  | 服务端 Bearer 调用；每个实例独立 URL/凭据
  v
AgentTeams Controller :8090
  |-- /api/v1/...                 管理面 REST
  |-- Kubernetes CR client       Worker/Team/Human/Manager v1beta1
  |-- Object Storage             TeamHarness Project/Task/Artifact/History
  |-- Matrix Client API          用户、房间、消息、成员、同步
  `-- Matrix AppService receiver /_matrix/app/v1/...

运行时
  |-- QwenPaw Matrix Channel     room_id -> matrix:{room_id} session
  |-- TeamHarness MCP stdio      团队协作与项目/任务工具
  |-- WorkerFlow MCP stdio       Worker 内部临时子 agent
  `-- experimental DSH bridge    DeepSeek Harness Worker

官方调用方
  |-- agt                        Controller REST 薄客户端
  |-- Dashboard Next.js BFF      Controller + Matrix + 自有存储/基础设施 API
  `-- agentteams plugin CLI      本地插件目录，不调用 Controller
```

必须区分两个“Project”：

- **RepoMesh Project**：用户创建的多仓库项目，按已确认设计拥有一个独立 AgentTeams 实例。
- **AgentTeams/TeamHarness Project**：一个团队内的持久工作流记录，存放在 `teams/{team}/shared/projects/...` 或全局 shared 前缀，更接近 RepoMesh 的 `IssueRun/WorkPlan`，并不是 AgentTeams 的租户/实例资源。[Project API 文档](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/docs/usage/project-workflow-api.md)

## 3. Controller REST API 全路由

以下为锁定提交 `http.go` 注册的完整路由表。默认地址来自 `AGENTTEAMS_HTTP_ADDR`，缺省 `:8090`。

### 3.1 健康、版本与资源管理

| 方法 | 路径 | 作用 | 认证/备注 |
|---|---|---|---|
| GET | `/healthz` | Controller 健康检查 | 唯一明确无认证的管理入口 |
| GET | `/api/v1/status` | `kubeMode`、Worker/Team/Human 总数 | Bearer + 任意获准角色 |
| GET | `/api/v1/version` | Controller 版本、运行模式 | 只认证，不做细粒度授权 |
| POST | `/api/v1/workers` | 创建 Worker | 资源写权限；响应表示资源已受理，不表示运行时已就绪 |
| GET | `/api/v1/workers` | Worker 列表；支持 `?team=` | 按角色/团队过滤 |
| GET | `/api/v1/workers/{name}` | Worker 详情 | 范围控制 |
| PUT | `/api/v1/workers/{name}` | 更新 Worker | 非完整替换；字符串通常仅在非空时更新，slice/pointer 在非 nil 时更新，不能等同标准 JSON Merge Patch |
| DELETE | `/api/v1/workers/{name}` | 删除 Worker | 高风险操作 |
| POST | `/api/v1/teams` | 创建 Team | 资源写权限 |
| GET | `/api/v1/teams` | Team 列表 | 按范围过滤 |
| GET | `/api/v1/teams/{name}` | Team 详情 | 范围控制 |
| PUT | `/api/v1/teams/{name}` | 更新 Team | 部分字段更新 |
| DELETE | `/api/v1/teams/{name}` | 删除 Team | 高风险操作 |
| POST | `/api/v1/humans` | 创建 Human | 管理面 |
| GET | `/api/v1/humans` | Human 列表 | 管理面 |
| GET | `/api/v1/humans/{name}` | Human 详情 | 管理面 |
| DELETE | `/api/v1/humans/{name}` | 删除 Human | **无 Human 更新端点** |
| POST | `/api/v1/managers` | 创建 Manager | 管理面 |
| GET | `/api/v1/managers` | Manager 列表 | 管理面 |
| GET | `/api/v1/managers/{name}` | Manager 详情 | 管理面 |
| PUT | `/api/v1/managers/{name}` | 更新 Manager | 部分字段更新 |
| DELETE | `/api/v1/managers/{name}` | 删除 Manager | 高风险操作 |
| POST | `/api/v1/packages` | 上传 Worker 包 | `multipart/form-data`；`file` 字段 |

权威来源：[路由](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/internal/server/http.go)、[请求/响应结构](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/internal/server/types.go)、[资源 handler](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/internal/server/resource_handler.go)。

主要请求字段：

- Worker 创建 DTO 声明：`name`、`workerName`、`model`、`modelProvider`、`runtime`、`image`、`identity`、`soul`、`agents`、`skills`、`remoteSkills`、`mcpServers`、`package`、`expose`、`channelPolicy`、`resources`、`containerManaged`、`state`。但 create handler 没有复制 `remoteSkills`，创建时发送该字段会被忽略；update handler 才会写入它。
- Worker 更新字段与创建大体相同但不含 `name`。字符串通常只在非空时更新，slice/pointer 只在非 nil 时更新，因此这不是完整替换或标准 JSON Merge Patch。
- Team 创建 DTO 声明：`name`、`teamName`、`description`、`admin`、`humanMembers`、`workerMembers`、`heartbeatEvery`、`peerMentions`、`channelPolicy`。Team 更新 DTO 也声明 `humanMembers`，但当前 update handler 没有复制该字段；不能依赖 Team PUT 更新 Human 成员。
- Human 创建：`name`、`displayName`、`email`、`permissionLevel`、`accessibleTeams`、`accessibleWorkers`、`note`。`permissionLevel` 的 CRD 实际语义是 `1=Admin, 2=Team, 3=Worker`；CLI help 写成 `0-100`，两者不一致。
- Manager 创建/更新 DTO 声明：`name`、`model`、`modelProvider`、`runtime`、`image`、`soul`、`agents`、`skills`、`remoteSkills`、`mcpServers`、`package`、`config`、`state`、`resources`。更新不含 `name`。但锁定提交的 `ManagerSpec` 没有 `remoteSkills` 字段，Create/Update handler 也没有复制该值；发送 `remoteSkills` 会被静默忽略，不能视为有效能力。

创建 Worker 时，handler 写入 CR 后立即把当时的对象转换为响应。刚创建的对象通常会被归一化为 `Pending`，`matrixUserID`、`roomID` 等由 reconcile 填充的 status 字段仍可能为空。调用方必须继续读取 `/workers/{name}/status` 或 GET 资源，不能把 POST 成功直接显示为“Worker 已就绪”。

`Human.status.initialPassword` 的类型注释声称只展示一次，但锁定提交会把 legacy password identity 的初始密码保存在 status，并通过资源 GET/`agt get` 再次映射输出；外部 SSO 身份则为空。这是注释与实现不一致的敏感信息风险，RepoMesh 不应在普通详情接口或页面中透传该字段。

REST DTO 并不暴露所有 CRD 字段，例如 Worker CRD 还包含 `accessEntries`、`credentialBindings`、`deployMode`、`env`、`labels`、`volumes`、`mounts` 等；所以 REST 不是 Kubernetes CRD 的无损通用代理。当前 decoder 也未启用 `DisallowUnknownFields`，再加上上述 `remoteSkills` 例子，RepoMesh 客户端必须通过读回与契约测试确认写入真正生效。

### 3.2 Worker 生命周期与检查点

| 方法 | 路径 | 作用 | 备注 |
|---|---|---|---|
| POST | `/api/v1/workers/{name}/wake` | 唤醒 Worker | 调整目标状态/拉起运行时 |
| POST | `/api/v1/workers/{name}/sleep` | 休眠 Worker | 生命周期控制 |
| POST | `/api/v1/workers/{name}/ensure-ready` | 确保就绪 | 适合接单前按需拉起 |
| POST | `/api/v1/workers/{name}/ready` | Worker 主动报告就绪/心跳 | Worker 自身可调用 |
| GET | `/api/v1/workers/{name}/status` | 运行时状态 | `agt create worker` 会轮询它 |
| GET | `/api/v1/workers/{name}/checkpoints/{sub}` | 代理 QwenPaw checkpoint | `sub` 只允许 `graph` 或 `status`；仅 embedded 模式；依赖 QwenPaw 2.1 能力 |

来源：[生命周期路由与检查点路由](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/internal/server/http.go)、[checkpoint handler](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/internal/server/worker_checkpoints.go)、[Project API 文档末尾的 checkpoint 说明](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/docs/usage/project-workflow-api.md)。

### 3.3 Project 工作流读取与人工干预

| 方法 | 路径 | 作用 | 关键参数/约束 |
|---|---|---|---|
| GET | `/api/v1/projects` | 列出可见 Project | `?team=` |
| GET | `/api/v1/projects/{id}/workflow` | 读取工作流图 | `?team=`；`?includeTasks=true` |
| GET | `/api/v1/projects/{id}/tasks/{taskId}/artifact` | 读取任务声明的 artifact | `?team=`；可选 `?path=`；严格限定到声明路径 |
| GET | `/api/v1/projects/{id}/spawns` | 项目衍生会话列表 | `?team=`；读取各 Worker `chats.json` |
| GET | `/api/v1/projects/{id}/spawns/{sessionId}/messages` | 衍生会话消息 | `?team=`；`?limit=` 默认 20、上限 50；只读 QwenPaw 历史库 |
| GET | `/api/v1/projects/{id}/history` | 变更历史索引 | `?team=`；新到旧 |
| GET | `/api/v1/projects/{id}/history/{timestamp}` | 历史快照 | `timestamp` 为 19 位 Unix 纳秒字符串 |
| POST | `/api/v1/projects` | 创建 Project | `{title, source, requester, team_id, project_id?, source_room_id?}` |
| POST | `/api/v1/projects/{id}/pause` | 暂停新调度 | `reason?`；不打断正在执行的任务 |
| POST | `/api/v1/projects/{id}/resume` | 恢复 | 仅 paused 状态可恢复 |
| POST | `/api/v1/projects/{id}/replan` | 重写 DAG | `tasks[]`；校验 ID、依赖和环；有 in-progress/submitted 时拒绝 |
| POST | `/api/v1/projects/{id}/tasks/{taskId}/cancel` | 取消任务 | `reason` 必填，`replacementTaskId?` |
| POST | `/api/v1/projects/{id}/complete` | 标记项目完成 | 所有任务必须处于终态 |

关键语义：

- 只有 TeamHarness `projectflow create_project/create_quick_project` 或 REST 创建并同步到共享存储的 Project 才能被 Controller 看见。它不是 CRD。
- Project 的真实标识是 `(team, project_id)`，因为 `project_id` 只保证在 Worker/团队工作空间内唯一。同一 ID 在多团队存在时，不带 `team` 会返回 `409`。
- 写操作使用对象存储 ETag 条件写；并发冲突返回 `409`。每次写入保留前一份快照，默认只留最近 50 份，并记录 `updated_by/updated_at` 等审计字段。
- 对 `source_room_id` 的干预通知是 best effort；通知失败不等于状态写入失败。
- `spawns/messages` 是观察接口，不是对衍生 agent 的控制接口。

权威来源：[Project API 文档](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/docs/usage/project-workflow-api.md)、[Project handler](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/internal/server/project_handler.go)。文档开头仍称“四个只读端点”，但同一文档后文及当前路由已经包含更多读取和写入端点，属于文档残留措辞。

### 3.4 网关、凭据、AppService 与内部代理

| 方法 | 路径 | 作用 | 定位 |
|---|---|---|---|
| POST | `/api/v1/gateway/consumers` | 创建网关消费者 | 管理/内部集成；请求 `name, credential_key` |
| POST | `/api/v1/gateway/consumers/{id}/bind` | 绑定消费者 | 管理/内部集成 |
| DELETE | `/api/v1/gateway/consumers/{id}` | 删除消费者 | 无 GET 列表端点 |
| POST | `/api/v1/credentials/sts` | 刷新对象存储/云 STS | 自身份范围，不接受目标 Worker 名 |
| POST | `/api/v1/credentials/matrix-token` | 刷新 Matrix token | 自身份范围 |
| POST | `/api/v1/appservice/rotate-token` | 轮换 AppService token | 管理接口；敏感 |
| PUT | `/_matrix/app/v1/transactions/{txnId}` | Matrix AS 推送事务 | 仅启用 AppService 且配置 `hs_token` 时注册；由 homeserver 调用 |
| GET | `/_matrix/app/v1/users/{userId}` | Matrix AS 用户查询 | `hs_token` 认证，不是普通管理 API |
| GET | `/_matrix/app/v1/rooms/{roomAlias}` | Matrix AS 房间别名查询 | `hs_token` 认证 |
| ALL | `/docker/...` | embedded 模式 Docker API 受限代理 | 仅 embedded + socket；内部/高权限面，不建议 RepoMesh 使用 |

来源：[Controller 路由](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/internal/server/http.go)、[AppService handler](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/internal/server/appservice_handler.go)、[Docker 代理安全校验](https://github.com/agentscope-ai/AgentTeams/tree/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/internal/proxy)。

## 4. REST 认证与权限

生产/正常部署采用 `Authorization: Bearer <token>`，认证链依次是：

1. Kubernetes ServiceAccount token，通过 TokenReview 校验，默认 audience 为 `agentteams-controller`；再根据 SA 名和 CR/Team 成员关系解析为 Admin、Manager、Team Leader 或 Worker。
2. 若 SA 校验失败，再按 Matrix access token 调用 `/_matrix/client/v3/account/whoami`，匹配 `Human` CR，当前只接受 `permissionLevel=2`，并加载 `accessibleTeams`。

没有 REST config 的开发模式会禁用认证；这不能作为生产安全假设。来源：[App 认证装配](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/internal/app/app.go)、[Bearer middleware](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/internal/auth/middleware.go)、[SA authenticator](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/internal/auth/authenticator.go)、[Matrix authenticator](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/internal/auth/matrix_authenticator.go)。

| 身份 | 实际代码级能力摘要 |
|---|---|
| Admin / Manager | Controller API 全权限 |
| Team Leader | 读取本团队 Project/Worker；更新和控制本团队已有 Worker；本团队 Project 创建与干预；**不能创建 Worker**，`CreateWorker` handler 会明确返回 `409`；不能管理 Human/Manager/Gateway |
| L2 Human | 读取 `accessibleTeams` 中的 Team/Worker/Project；可在范围内创建与干预 Project；锁定提交还允许只更新本团队 Worker 的 `skills` 字段，其他字段拒绝 |
| Worker | 自身 Worker get/status/ready，以及自身份 STS/Matrix token 刷新；不能读取 Project |

来源：[authorizer](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/internal/auth/authorizer.go)、[L2 Worker 字段白名单实现](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/internal/server/resource_handler.go)。源码中 `MatrixTokenAuthenticator` 和角色常量附近仍称 L2 Human 为“read-only”，但 `authorizer.go` 与 handler 已授予上述 Project 写和 `skills` 自服务能力，应以实际授权路径为准，并把该不一致视为接口成熟度风险。

这里必须区分“authorizer 允许访问资源路径”和“handler 最终接受操作”。Team Leader 的团队范围检查不等于拥有 Worker 创建权；创建请求会在 handler 层被拒绝。上一版报告把二者混为一谈，现已更正。

## 5. `agt` CLI 完整命令树与 REST 映射

连接配置：

- `AGENTTEAMS_CONTROLLER_URL`，默认 `http://localhost:8090`。
- token 优先级：`AGENTTEAMS_AUTH_TOKEN` → `AGENTTEAMS_AUTH_TOKEN_FILE` → 空 token。
- 没有全局 `--server/--token` flag；环境变量是主要入口。

来源：[root command](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/cmd/agt/main.go)、[client](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/cmd/agt/client.go)。

### 5.1 命令树

```text
agt
├─ apply
│  ├─ -f, --file FILE...                         YAML 多文档顺序 upsert
│  └─ worker --name --model --zip --runtime --image --identity
│             --soul --soul-file --skills --package --expose
├─ create
│  ├─ worker --name --model --runtime --image --identity --soul --soul-file
│  │          --skills --package --expose -o --wait-timeout --no-wait
│  ├─ team --name --team-name --leader-name --leader-heartbeat-every
│  │        --workers --description --admin --admin-matrix-id --peer-mentions
│  ├─ human --name --display-name --email --permission-level
│  │         --accessible-teams --accessible-workers --note
│  └─ manager --name --model --runtime --image --soul
├─ get
│  ├─ workers [name] --team -o json
│  ├─ teams [name] -o json
│  ├─ humans [name] -o json
│  ├─ managers [name] -o json
│  └─ projects [name] --team --mermaid -o json
├─ update
│  ├─ worker --name --model --runtime --image --identity --soul
│  │          --skills --package --expose --state
│  ├─ team --name --team-name --description --leader-name
│  │        --leader-heartbeat-every --workers --peer-mentions
│  └─ manager --name --model --runtime --image --soul
├─ delete
│  ├─ worker <name>
│  ├─ team <name>
│  ├─ human <name>
│  └─ manager <name>
├─ project
│  ├─ create --title --source --requester --team --id --room
│  ├─ pause <project-id> --reason
│  ├─ resume <project-id>
│  ├─ replan <project-id> --tasks FILE
│  ├─ cancel <project-id> <task-id> --reason --replacement
│  └─ complete <project-id>
├─ worker
│  ├─ wake --name
│  ├─ sleep --name
│  ├─ ensure-ready --name
│  ├─ status (--name | --team) -o json
│  └─ report-ready [--name] [--heartbeat] [--interval 60s]
├─ status -o json
├─ version -o json
├─ llm-preflight --provider --api-key --base-url --model
│                 --timeout --retries --strict
└─ rotate
   └─ appservice-token --as-token --hs-token
```

### 5.2 映射和行为

| CLI | REST 映射/行为 |
|---|---|
| `agt apply -f` | YAML 的 `kind` 映射为复数资源路径；先 GET 判断存在，再 POST 或 PUT。无 server-side apply、dry-run 或 prune |
| `agt apply worker --zip` | 先 `POST /packages`，再 POST/PUT Worker |
| `agt create worker` | `POST /workers`，默认继续轮询 `GET /workers/{name}/status`，默认超时 3 分钟；`--no-wait` 只等 Controller 接受 |
| `agt create team/human/manager` | 对应 POST 资源端点 |
| `agt get ...` | 对应 GET 单项/列表端点；源码有效子命令是复数 `workers/teams/humans/managers/projects` |
| `agt update ...` | 对应 PUT；CLI 只暴露 REST DTO 的一部分字段；没有 Human update |
| `agt delete ...` | 对应 DELETE |
| `agt project ...` | 对应 Project create/pause/resume/replan/cancel/complete POST |
| `agt worker ...` | 对应 Worker lifecycle/status；`report-ready --heartbeat` 会按间隔持续调用 `/ready` |
| `agt status/version` | 对应 `/api/v1/status` 和 `/api/v1/version` |
| `agt rotate appservice-token` | `POST /api/v1/appservice/rotate-token` |
| `agt llm-preflight` | **例外：不调用 Controller REST**，直接调用模型提供商的 OpenAI-compatible `/chat/completions` |

源码入口：[apply](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/cmd/agt/apply.go)、[create](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/cmd/agt/create.go)、[get](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/cmd/agt/get.go)、[update](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/cmd/agt/update.go)、[delete](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/cmd/agt/delete.go)、[project](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/cmd/agt/project_cmd.go)、[worker](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/cmd/agt/worker_cmd.go)、[preflight](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/cmd/agt/llm_preflight.go)。

`agt` 的镜像分布也不是“每个 Worker 都有”。Controller、Manager、OpenClaw、CoPaw、Hermes、OpenHuman 等镜像会复制该二进制，但当前 QwenPaw 和 DeepSeek Harness Dockerfile 没有复制它。官方安装器也不会把 `agt` 安装成宿主机全局命令；源码构建或从含有它的容器中运行仍然可行。

### 5.3 已确认的 CLI 缺口

- `agt get projects ID --team TEAM` 的源码在详情分支没有把 `--team` 拼入 URL；当多团队存在同名 Project 时，REST 可以用 `?team=` 消歧，CLI 当前会得到 `409`。
- `agt project pause/resume/replan/cancel/complete` 均没有 `--team`，同样无法操作跨团队重名 Project。
- CLI 没有直接覆盖 artifact、spawns、spawn messages、history、checkpoint、gateway consumers、credentials 等已存在 REST 端点。
- 官方部分文档仍出现 `agt get worker` 单数示例，但 Cobra 源码只注册复数 `workers`，未见 alias。
- Worker `--runtime` help 列出 `openhuman`，但当前 CRD 文档明确说显式 `spec.runtime: openhuman` 会被拒绝；Manager 的不同命令 help 对可选 runtime 也不完全一致。

因此 RepoMesh 不应 shell-out `agt` 作为核心集成层；应直接调用 REST，并为 `(team, project_id)` 建模。

## 6. `agentteams` 插件 CLI

```text
agentteams plugin install NAME (--package PATH | --source DIR)
agentteams plugin list
agentteams plugin update NAME (--package PATH | --source DIR)
agentteams plugin uninstall NAME
```

它解包本地插件、执行生命周期脚本并把安装状态写到 `.agentteams/`。它不认识 Controller URL、Bearer token、Worker CR，也不进行集群部署。来源：[CLI main.py](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/plugins/cli/src/agentteams_cli/main.py)、[plugin manager](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/plugins/cli/src/agentteams_cli/plugin_manager.py)、[README](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/plugins/README.md)。

插件 manifest schema 要求：

```yaml
apiVersion: agentteams.agentteam/v1alpha1
kind: AgentTeamPlugin
metadata:
  name: ...
  version: ...
```

JSON Schema 允许额外字段，约束较松。[plugin schema](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/plugins/schemas/plugin.schema.json)

## 7. TeamHarness 与 WorkerFlow MCP

两者都以 MCP `stdio` server 形式运行，握手响应协议版本 `2024-11-05`；工具结果主要以 JSON 编码文本返回。这是运行时本地工具接口，不是远程 HTTP API。

### 7.1 TeamHarness

| Tool | Actions | 作用/边界 |
|---|---|---|
| `health` | 无 action | 只检查 TeamHarness MCP wiring，不代表 Worker 或 Controller 健康 |
| `message` | `send` | Matrix 房间/外部 replyRoute 消息；Worker 和 remote-member 在 server 层被隐藏/拒绝，主要给 Leader/Manager |
| `roomflow` | `create_task_room`, `list_rooms`, `describe_room`, `archive_room` | 创建和管理任务房间，写 `room.meta`、名称、topic/tag、邀请成员 |
| `filesync` | `list`, `stat`, `pull`, `push` | 同步 `shared/`；`global-shared/` 只读；带路径与排除规则 |
| `artifact` | `publish_file` | 上传工作区文件到 Matrix media，再发送 `m.file`；有 traversal/敏感路径检查 |
| `projectflow` | `create_project`, `create_quick_project`, `resolve_project`, `plan_dag`, `plan_loop`, `ready_nodes`, `ready_loop_nodes`, `record_loop_iteration`, `accept_task_result`, `mark_requester_report_sent`, `pause_project`, `resume_project`, `complete_project` | 持久 Project 状态、计划、结果接收与完成；状态位于共享文件/对象存储 |
| `taskflow` | `delegate_task`, `ack_task`, `submit_task`, `check_task`, `cancel_task` | Leader 委托/检查/取消；Worker 或 remote-member 接收/提交；先持久化再用 Matrix 通知，含稳定 txn id |

来源：[plugin.yaml](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/plugins/teamharness/plugin.yaml)、[MCP schemas 与 dispatch](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/plugins/teamharness/mcp/server.py)、[message tool](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/plugins/teamharness/mcp/message_tool.py)、[roomflow tool](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/plugins/teamharness/mcp/roomflow_tool.py)。

边界判断：

- TeamHarness 自称 runtime-neutral collaboration base，但当前 `plugin.yaml` 只声明 `qwenpaw` adapter，基础 `scripts/install.sh` 也只探测 `qwenpaw`。[边界文档](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/docs/design/teamharness/boundary-and-contracts.md) / [安装脚本](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/plugins/teamharness/scripts/install.sh)
- `message` 与 `taskflow` 有明确角色检查；`projectflow` 的 server dispatch 没有同等清晰的通用调用者身份鉴权，较多依赖按角色注入 skill/tool 与提示约束。RepoMesh 不能把这类提示级约束当安全边界。
- `_PROJECTFLOW_MUTATING_ACTIONS` 中残留 `replan_project`，但公开 schema 和分支并不暴露该 action，说明 MCP action 契约仍在演化。

### 7.2 WorkerFlow

只有一个工具 `worker_agentflow`：

```text
list_agents
list_subagents
create_temp_agent
delete_temp_agent
cleanup_shared
workflow_run
workflow_start
workflow_update
workflow_finish
workflow_fail
```

它调用 Worker 本地 QwenPaw API，默认 `http://127.0.0.1:8088/api`，管理 `tmp-workerflow-*` 临时 agent 和内部 workflow。源码明确写明“Temporary agents are not TeamHarness Workers”。因此它是单 Worker 内部 fan-out，不是 RepoMesh 长期 Worker、跨仓队列或独立执行副本调度器。[WorkerFlow manifest](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/plugins/workerflow/plugin.yaml) / [MCP server](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/plugins/workerflow/mcp/server.py)

## 8. Matrix Client API 与会话管理

### 8.1 Controller 使用的 Matrix 能力

Controller 的 Matrix client 包装了以下 Matrix Client API：

- 用户与认证：`POST /register`、`POST /login`、`GET /account/whoami`、`PUT /profile/{userId}/displayname`。
- 房间：`POST /createRoom`、解析/删除 room alias、设置 `m.room.name` 和其他 state、join/leave。
- 消息：`PUT /rooms/{room}/send/m.room.message/{txnId}`。
- 成员：members、invite、kick、joined_rooms。
- 拉取：`GET /sync`。

TeamHarness 还直接使用 Matrix media upload、`m.file` 消息、`room.meta`、topic/tag、成员和消息接口。来源：[Controller Matrix client](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/internal/matrix/client.go)、[Matrix AppService](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/internal/matrix/appservice.go)、[TeamHarness MCP server](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/plugins/teamharness/mcp/server.py)。

### 8.2 房间、上下文与 session

QwenPaw Matrix Channel 的关键映射为：

```text
Matrix room !abc:server
  -> AgentRequest.session_id = "matrix:!abc:server"
  -> AgentRequest.user_id    = "!abc:server"
  -> 真正发送者保存在 channel_meta["sender_id"]
```

这表示对**同一个 QwenPaw bot/runtime**，同一房间内的不同发送者会进入该 bot 的同一条 room session；不是不同 Manager、Leader、Worker 进程共享一个全局 session store。另一个 issue 若使用另一个房间，会获得另一个房间 session。群房间可配置 mention gating；对普通非 DM、非 thread 群聊，未 mention 的最近消息先进入每房间的内存 history buffer，默认上限 50，后续触发并成功入队后才清空。thread 中未 mention 的事件会直接忽略，slash/thread 路径不会拼接该缓冲，进程重启也会丢失缓冲。[session 代码](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/plugins/agentteams-matrix-channel/agentteams_matrix/channel.py)

DeepSeek Harness adapter 采用同样的“每房间一个稳定 DSH session”原则，并把 session/投递状态持久化到对象存储以支持 Pod 替换恢复；但它明确是 experimental、固定在 DSH release candidate，且不支持 Matrix E2EE。[DeepSeek runtime README](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/deepseek-harness/README.md) / [adapter README](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/plugins/teamharness/adapters/deepseek-harness/README.md) / [兼容性报告](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/plugins/teamharness/adapters/deepseek-harness/COMPATIBILITY.md)

对 RepoMesh：可以把一个 issue 的主房间作为会话 transport key，但仍应显式保存 `IssueSession -> room_id -> runtime_session_id` 映射；Matrix 消息不应成为计划、队列、尝试和交付状态的唯一事实源。

## 9. Coding Harness 现状

### 9.1 实际存在

- **QwenPaw**：当前主要、实际安装的 TeamHarness adapter；QwenPaw plugin API 将 MCP/工具注册到运行时。[QwenPaw adapter](https://github.com/agentscope-ai/AgentTeams/tree/eeaab64391ccaec9118e84977f538aefd40720d6/plugins/teamharness/adapters/qwenpaw)
- **DeepSeek Harness**：实际 Worker runtime + Matrix bridge + TeamHarness adapter，但官方明确标记 experimental，固定 `@deepseek-ai/dsh@0.1.1-rc.2`，不支持 E2EE。[runtime README](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/deepseek-harness/README.md)

### 9.2 不应误判为正式接入

- `plugins/teamharness/adapters/claude-code/README.md` 只说未来 integration phase 定义 hooks；`install.sh/uninstall.sh` 仅可选写一行日志。基础安装器不会选择 Claude Code。[占位目录](https://github.com/agentscope-ai/AgentTeams/tree/eeaab64391ccaec9118e84977f538aefd40720d6/plugins/teamharness/adapters/claude-code)
- `manager/agent/skills-alpha/coding-cli-management` 是另一条 alpha 路径：Worker 发 `coding-request`，Manager 在 Worker workspace 运行 CLI，再返回结果。支持 `claude`、`gemini`、`qodercli`；执行参数分别包含 `--dangerously-skip-permissions`、`-y`、`--yolo`，默认超时 600 秒。它没有 Codex，并且权限模型明显不适合未经隔离和策略控制直接用于 RepoMesh 生产执行。[skill](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/manager/agent/skills-alpha/coding-cli-management/SKILL.md) / [detect script](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/manager/agent/skills-alpha/coding-cli-management/scripts/detect-available-cli.sh) / [run script](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/manager/agent/skills-alpha/coding-cli-management/scripts/run-coding-cli.sh)

结论：RepoMesh 仍需要自己的 `CodingHarnessAdapter` 抽象，至少规范 `prepare_attempt / start / stream / cancel / collect_artifacts / classify_result / cleanup`，并把每次执行绑定到独立工作副本、单 Worker 占用和权限策略。上游 alpha 脚本可作为命令形态参考，不能当成已完成方案。

## 10. CRD 接口

官方 Kubernetes API group/version 为 `agentteams.io/v1beta1`，注册四种 namespaced CR：

| Kind | 作用 | 核心 spec/status |
|---|---|---|
| `Worker` | 一个长期或独立 agent runtime | model/provider/runtime/image/identity/prompts/skills/MCP/package/channel/resources/state/deploy；status phase、Matrix identity、room、heartbeat、backend、ports |
| `Team` | Leader + Workers + humans 的稳定团队 | workerMembers 带 `team_leader/worker` role、admin、heartbeat、peer mentions；status 房间和成员就绪状态 |
| `Human` | Matrix 人类身份与范围 | permissionLevel、accessibleTeams/Workers、identitySource；status MatrixUserID/rooms |
| `Manager` | 实例级协调 agent | model/runtime/prompts/skills/MCP/config/state/resources；status room/version/welcomeSent |

来源：[Go API types](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/api/v1beta1/types.go)、[register.go](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/api/v1beta1/register.go)、[CRD YAML](https://github.com/agentscope-ai/AgentTeams/tree/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/config/crd)。

状态名必须以 reconcile 实现为准。当前 Worker `computeMemberPhase` 实际产生 `Pending / Starting / Running / Stopping / Sleeping / Stopped / Failed`，没有稳定的 Worker `Updating` 转换；Team 为 `Pending / Degraded / Active / Failed`，Human 为 `Pending / Active / Degraded / Failed`，Manager 为 `Pending / Running / Sleeping / Stopped / Failed`。类型注释、旧文档与实际状态机之间存在差异。

`Ready` 不是 Worker CRD phase。`/workers/{name}/status` 会在 backend running 且 Controller 内存 ready map 为真时临时覆盖为 Ready；Controller 重启后该内存状态会丢失，直到 Worker 再次 report-ready。RepoMesh 应分别保存上游原始 phase、ready 信号和自身产品状态。

CRD 是 incluster 模式下可通过标准 Kubernetes API/kubectl 管理的接口；`agt` 和 Controller REST 在其上提供更适合产品/运维的 DTO、权限与副作用处理。RepoMesh 当前最多一台服务器、同机多实例，若采用 embedded 模式，不应要求自己的控制面直接耦合每实例内部 K8s CR API；优先通过 Controller REST。

## 11. Dashboard 是否调用公开 API

是。官方 Dashboard `v1.2.4` 使用 Next.js API route 作为 BFF：浏览器不直接访问 Controller 或 Matrix；服务端读取 `AGENTTEAMS_CONTROLLER_URL`，有 ServiceAccount token 时覆盖浏览器 Authorization，并在每次请求重新读取轮换 token。默认 Controller 地址为 `http://agentteams-controller:8090`。[Dashboard README](https://github.com/agentteams-group/agentteams-dashboard/blob/v1.2.4/README.md) / [proxy-helper](https://github.com/agentteams-group/agentteams-dashboard/blob/v1.2.4/src/app/api/agentteams/proxy-helper.ts)

Dashboard 代理了 Worker/Team/Human/Manager CRUD、生命周期、status/version、packages、gateway consumers，以及 Project list/workflow/history/artifact/pause/resume/replan/cancel 等。但它不是 REST 契约的完整镜像：

- Dashboard `v1.2.4` 未代理当前 Controller 已有的 Project `POST create`、`POST complete`、spawns/messages。
- Dashboard 有 `GET gateway/consumers` BFF route，但锁定 Controller 没有该 GET；UI 对“不支持列表”做了兼容提示。
- Dashboard 有 `/api/v1/setup*`、存储 buckets、skills/Nacos、MCP CRUD、team-tasks、基础设施与日志等自身服务端 API；不能把这些 Next routes 当作 Controller 公共路由。
- Task Board 在 Controller Project API 退化时可通过 Dashboard 自有 `team-tasks` 路由扫描 MinIO，这进一步说明 Dashboard 是组合后端而非生成客户端。

来源：[Dashboard agentteams API routes](https://github.com/agentteams-group/agentteams-dashboard/tree/v1.2.4/src/app/api/agentteams)、[projects routes](https://github.com/agentteams-group/agentteams-dashboard/tree/v1.2.4/src/app/api/agentteams/projects)、[gateway consumers route](https://github.com/agentteams-group/agentteams-dashboard/blob/v1.2.4/src/app/api/agentteams/gateway/consumers/route.ts)、[team-tasks route](https://github.com/agentteams-group/agentteams-dashboard/blob/v1.2.4/src/app/api/agentteams/team-tasks/route.ts)。

## 12. OpenAPI、Swagger 与生成客户端

在锁定提交和当前同一 `main` 中：

- 未找到 Controller REST 的 `openapi.yaml/json`、Swagger annotations、Swagger UI、`/openapi`/`/swagger` 路由或官方生成客户端。
- `agentteams-controller/config/crd/*.yaml` 中的 `openAPIV3Schema` 仅描述 Kubernetes CRD。
- `go.mod` 中出现的 `go-openapi`、`kube-openapi` 间接依赖和阿里云 SDK `darabonba-openapi` 不构成 Controller REST 描述。
- 目前最可靠的契约顺序是：`internal/server/http.go` 路由 → handler/`types.go` → 测试 → `agt`/Dashboard 消费代码 → 手写文档。

这意味着 RepoMesh 若复用 REST，应维护一个**很窄的、锁 commit/version 的客户端**，并用契约测试覆盖请求字段、状态码、权限和版本探测；不要依据 URL 中的 `/api/v1` 推断已有正式兼容承诺。

## 13. 成熟度分类

| 层/接口 | 判断 | 依据与风险 |
|---|---|---|
| Worker/Team/Human/Manager CRD 与 `/api/v1` CRUD | 已发布、核心管理面；可复用但需锁版本 | v1.2.0 release notes 称 stable 并确立最终 Team/Worker contract；CRD 名仍为 v1beta1；无 REST OpenAPI/兼容政策。[README release notes](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/README.md) |
| `agt` 资源与生命周期命令 | 已发布官方运维入口 | 与 REST 紧密绑定；存在文档/参数/消歧缺口，不宜作为程序集成协议 |
| Project REST + `agt project` | v1.2.3 已发布，仍在快速演化 | 有 ETag、history、审计与代码级 auth，但文档开头残留只读描述，CLI 未完整传 `team` |
| Matrix Client API | 上游标准稳定；AgentTeams 使用方式为实现契约 | 房间/session、`room.meta`、alias、事件类型是 AgentTeams 约定；需做版本兼容 |
| Matrix AppService receiver | 协议型/部署内部接口 | 面向 homeserver，不是 RepoMesh 管理 API |
| TeamHarness MCP | 已有实际实现；插件契约仍为 v0.1/v1alpha1 | 官方边界文档称其中 prompts/skills/tools 为 stable，但 adapter、role enforcement、action schema 仍有演化痕迹 |
| WorkerFlow MCP | 实验性/运行时特定扩展 | QwenPaw 本地 API、临时 agent；不能等同长期 Worker 调度 |
| QwenPaw TeamHarness adapter | 当前实际主路径 | 清单和安装器实际支持；仍需与 QwenPaw 版本矩阵一起验证 |
| DeepSeek Harness | **experimental** | 固定 release candidate、不支持 E2EE、独立发布 |
| Claude Code TeamHarness adapter | **占位/未实现** | README + 记录日志脚本；基础安装器不探测它 |
| coding-cli-management | **skills-alpha** | 支持 claude/gemini/qodercli，但高权限非交互参数、Manager 代执行；不是正式 harness adapter |
| `/docker/`、credentials、gateway、AppService rotate | 内部/敏感面 | 功能真实存在，但不宜作为 RepoMesh 常规产品接口 |
| Dashboard Next API | 官方 BFF，但独立版本组合接口 | 可证明 Controller REST 可被前端消费；不是完整、同步、生成式 SDK |

## 14. 对 RepoMesh 的直接影响与建议

### 14.1 可复用

1. **实例内资源与就绪状态**：使用 Controller REST 创建 Manager、Workers、Teams，读取 status/phase，并在接单前调用 `ensure-ready`。
2. **运行时通信**：保留 Matrix 作为 Manager/Leader/Worker 的消息与可见协作通道；每个 issue 使用独立主房间时，可以获得房间级 session 隔离。
3. **工作流观察和有限干预**：可复用 Project workflow/history/artifact/spawns 读取，及 pause/resume/replan/cancel/complete，但必须始终带 team 维度并处理 `409`。
4. **TeamHarness 语义与 MCP**：可以复用 task room、共享文件、artifact、project/task action 的设计和部分实现；通过自有 adapter 隔离 v0.1/v1alpha1 变动。
5. **服务端代理安全模式**：借鉴 Dashboard 的 BFF 做法，由 RepoMesh 服务端持有每实例 Controller 凭据，浏览器不直连 Controller。

### 14.2 不能假定上游已经提供

- AgentTeams Controller 没有“一个 RepoMesh 管多个独立多仓项目/实例”的全局租户层。
- 没有 RepoMesh 所需的全局容量、项目额度、跨实例排队、公平调度和单 Worker 单任务租约。
- 没有 Git provider 授权、仓库集合、分支策略、独立 worktree/clone、提交/PR/交付门禁的完整领域模型。
- TeamHarness Project 不是外部 Issue，也不是 RepoMesh Project；上游状态不足以作为多 issue 队列唯一事实源。
- 没有稳定的 Codex/Claude Code/Qoder 通用 harness 适配层；alpha coding CLI 脚本不能替代执行尝试隔离、取消、恢复、日志和权限控制。
- Matrix 房间 session 能提供上下文隔离，但不自动解决 plan/task/attempt 的持久真相、上下文压缩、并发写冲突或长周期审计。

### 14.3 建议的 RepoMesh 集成边界

```text
RepoMesh Control Plane（事实源）
  Project / RepositoryAuthorization / AgentTeamsInstance
  Issue / IssueSession / Plan / Task / ExecutionAttempt
  WorkerLease / ProjectQuota / GlobalCapacity / Delivery
            |
            | InstanceAdapter（固定 AgentTeams commit/version）
            v
  Controller REST
    - resources: Manager / Worker / Team
    - lifecycle: ensure-ready / status / wake / sleep
    - workflow observation/intervention
            |
            +--> MatrixTransportAdapter
            |      room + event + runtime session mapping
            |
            `--> CodingHarnessAdapter
                   Codex / Claude / Qoder / DSH / ...
                   独立工作副本 + 单 Worker lease + attempt 日志/产物
```

建议 RepoMesh 第一阶段只依赖以下窄子集：

- `GET /healthz`、`GET /api/v1/version`、`GET /api/v1/status`。
- Manager/Worker/Team create/get/update，以及必要时 delete 补偿。
- Worker `ensure-ready/status/wake/sleep`。
- Project list/workflow/history/artifact；写端点延后到明确采用 TeamHarness Project 为 IssueRun 执行镜像后再接。

需要在实现前做的技术验证：

1. embedded 同机多实例下 Controller URL、token、namespace/resourcePrefix、Matrix homeserver/alias 和对象存储 prefix 能否完全隔离。
2. 真实创建 Manager → Workers → Team，测量从“已接受”到“真正就绪”的耗时、ready 重报、失败、重试和 Controller 重启后的恢复表现；accepted 与 ready 在静态源码中已经确认是不同状态。
3. QwenPaw/TeamHarness 版本组合下房间 session、history buffer、持久历史和重启恢复的具体边界。
4. Project REST 与 TeamHarness MCP 并发写 ETag 冲突的重试和审计表现。
5. `skills-alpha/coding-cli-management` 不纳入产品依赖；另行验证受控 harness adapter 和独立工作副本。
6. 为所有依赖端点建立固定版本契约测试，因为没有官方 OpenAPI/SDK。

## 15. 未验证与限制

- 未启动 Controller、Matrix、QwenPaw、对象存储或 Dashboard；所有状态码和行为来自官方实现/测试/文档静态交叉核对。
- 未对云上 AI Gateway、STS、Nacos、Higress 做真实调用。
- 未验证 AgentTeams 官方是否在仓库外另行发布了非链接的私有 OpenAPI/SDK；结论仅为官方开源仓库锁定提交及当前同一 `main` 中“未找到”。
- 未验证 Dashboard 当前 `main` 的 `org.agentteams.run v1` 与锁定 Controller 的可用兼容矩阵；本报告对 Dashboard API 以 AgentTeams v1.2.3 release notes 对应的 Dashboard `v1.2.4` 为准。
