# AgentTeams 调研：RepoMesh 真正能用到什么

> 日期：2026-09-07  
> 调查对象：[`agentscope-ai/AgentTeams`](https://github.com/agentscope-ai/AgentTeams)  
> 源码基线：`eeaab64391ccaec9118e84977f538aefd40720d6`  
> 调查方式：只读核查官方源码、官方文档和官方 Dashboard；没有安装、部署或调用真实服务。  
> 详细的 API、CLI 和源码清单见：[AgentTeams API、CLI、Matrix 与 Harness 接口调查](./agentteams-api-cli-survey-2026-09-07.md)

> 2026-09-09 后续索引：本文保留 09-07 静态调研及其原日期。最新 main 经重新查询仍为上述提交，最新 release 为 v1.2.3；用户已采用复用上游原生仓内 DAG，由 RepoMesh 管跨仓依赖、业务放行及 Loop。现行职责和后续组件／真实 HTTP＋MinIO 证据见 [Graph／Loop 专题](../current/graph-loop-design.md)，不将后续实验倒填为本文当时已运行。

## 先说结论

AgentTeams 能为 RepoMesh 提供一套已经成形的“团队协作底座”：它会创建和管理 Manager、Leader、Worker，让这些 Agent 在 Matrix 房间里通信，通过共享存储交换计划和产物，并通过 Controller 管理运行时。

但它没有替 RepoMesh 做完产品层：多仓项目、仓库授权、Issue 队列、全局容量、单 Worker 单任务、独立工作副本、Codex/Claude Code/Qoder 执行适配、验证和交付门禁，都还需要 RepoMesh 自己负责。

最容易混淆的四件事是：

1. AgentTeams 的 `Project` 不是 RepoMesh 的多仓项目，更接近一次 Issue 的工作计划或执行记录。
2. Matrix 房间是会话边界之一，但消息不是业务状态的唯一事实源。
3. 创建 Worker 的接口返回成功，只表示 Controller 已接单，不表示 Worker 已经可用。
4. AgentTeams 目前没有完整接入 Codex、Claude Code、Qoder 这类 coding harness。

## 如何阅读本文

- **已核实**：在锁定提交的源码或官方文档中能够直接找到。
- **RepoMesh 已确认**：我们已经决定采用的产品或架构原则，不代表上游自动提供。
- **建议**：为了让 RepoMesh 可控、可替换而提出的设计。
- **待验证**：静态看源码仍不足以证明，需要以后实际部署测试。

## 1. AgentTeams 到底负责什么

用人话说，AgentTeams 更像一家“Agent 公司”的办公系统：

- Controller 是行政和运维系统，负责建账号、建房间、发凭据、拉起或停掉 Agent。
- Manager 是总负责人，接收范围不明或跨团队的工作。
- Team Leader 是某个团队的负责人，负责拆任务、派任务和验收。
- Worker 是具体干活的人。
- Matrix 是会议室和聊天系统。
- MinIO/OSS 是共享文件柜，也是 Project、Task 和产物的持久存储。
- Higress/AI Gateway 是模型和外部服务的统一出口，真实高价值密钥留在网关侧。
- TeamHarness 是一组协作工具，让 Agent 能建任务房间、发消息、同步文件、记录计划和提交结果。

```text
人类 / RepoMesh
      |
      v
AgentTeams Controller  --------  管理 Manager / Worker / Team / Human
      |
      +---- Matrix              房间、消息、成员、唤醒
      +---- MinIO / OSS         计划、任务、共享文件、产物、历史
      +---- AI Gateway          LLM 与 MCP 的受控出口
      `---- Docker / Kubernetes 运行 Manager 和 Worker

Manager
  `---- Team Leader
          `---- Workers
```

**已核实：** Controller 只负责资源和运行状态，不理解 RepoMesh 的仓库、Issue、排队、工作副本和交付语义。[Controller 路由](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/internal/server/http.go)

## 2. 房间、上下文和 session 是怎么回事

### 2.1 不是所有人都在同一个房间

AgentTeams 会使用多种 Matrix 房间，不同房间承担不同职责：

```text
Admin DM       人类管理员与 Manager
Worker Room    某个 Worker 的工作与沟通
Leader Room    Manager 与某个 Leader
Team Room      Leader 与本团队 Workers
Leader DM      Leader 的直接会话
Task Room      按需要创建的任务/协作房间
```

所以，不能把 AgentTeams 描述为“所有参与者共享同一个大群”。房间本身就是可见范围、消息路由和上下文隔离的一部分。

### 2.2 当前 QwenPaw 怎么映射 session

在当前 QwenPaw Matrix channel 中，一个 Matrix 房间会映射为一个稳定会话：

```text
Matrix room_id
    |
    +--> session_id = "matrix:" + room_id
    +--> user_id    = room_id
    `--> 真正的发送者放在 channel metadata 中
```

这意味着，对于同一个 QwenPaw bot/runtime：

- 同一房间里的连续消息会进入这个 bot 的同一条房间会话。
- 换一个房间，就会得到另一个 session。
- session 的隔离粒度首先是房间，不是发送消息的个人。

这不表示多个 Manager、Leader、Worker 进程共享同一个全局 session store。每个 Agent runtime 仍有自己的运行时和会话存储，只是都用 `room_id` 作为各自的房间会话键。

[QwenPaw Matrix channel 源码](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/plugins/agentteams-matrix-channel/agentteams_matrix/channel.py)

### 2.3 `@mention` 是唤醒条件，不等于上下文开关

群房间可以要求 `@mention` 才触发 Agent 回答。不过，当前 QwenPaw channel 对普通的非 DM、非 thread 群聊文本，会为每个房间暂存最近的未触发消息，默认最多 50 条；下一次真正触发并成功入队时，这些消息可以一起进入上下文，然后清空临时缓冲。thread 中未 mention 的事件会直接忽略，slash/thread 路径也不会拼接这份缓冲。缓冲只在内存中，进程重启后会丢失。

因此更准确的说法是：

> `@mention` 主要决定这条消息是否立即唤醒 Agent；未触发的消息不一定被丢弃，是否进入后续上下文取决于具体 runtime/channel 的实现。

这个行为不能无条件推广到所有运行时。DeepSeek Harness 等其他 adapter 需要分别验证。

### 2.4 RepoMesh 应该怎么用

**RepoMesh 已确认：** 每个 issue 有独立会话、计划和任务记录，但不会重新创建 Manager 或整个团队。

**建议：** 为每个 issue 建立显式映射，不要只靠 Matrix 历史反推：

```text
Issue
  `---- IssueSession
          +---- matrix_room_id
          +---- runtime_session_id
          +---- plan_id / task_ids
          `---- active_attempt_ids
```

Task Room 可以作为 issue 主房间，但这是 RepoMesh 的产品规则，不是 AgentTeams 已经强制执行的规则。计划、任务、队列、执行尝试和交付状态仍应保存在 RepoMesh 数据库中。

## 3. Manager 自己的 harness 是什么

Manager 不是一个独立发明出来的“大模型运行时”。当前主路径中，它运行在 QwenPaw 上，也可以看到历史上的 OpenClaw 支持。QwenPaw 负责：

- 调用模型；
- 维护运行时 session；
- 执行工具循环；
- 加载 prompt、skills 和 MCP 工具；
- 连接 Matrix channel。

AgentTeams 在这个运行时外面补上团队协作能力：Controller 管资源，Matrix 管通信，TeamHarness 管计划、任务和共享文件。

```text
Manager
  |
  +---- QwenPaw runtime         模型、session、工具循环
  +---- Matrix channel          收发消息、房间上下文
  +---- TeamHarness MCP         项目、任务、文件、房间、产物
  `---- agt / Controller REST   管理团队和 Worker 运行状态
```

这里有一条重要边界：很多“Manager 应该怎么派活”的限制来自 prompt、skill 和协作约定，并不是 Controller 强制执行的安全策略。RepoMesh 不能把提示词当成权限系统。

## 4. AgentTeams 有没有接 Coding Harness

答案是：有相关尝试，但还没有一层可以直接满足 RepoMesh 的通用 coding harness。

### 已经实际存在

- **QwenPaw adapter**：当前 TeamHarness 的主要实际接入路径。[QwenPaw adapter](https://github.com/agentscope-ai/AgentTeams/tree/eeaab64391ccaec9118e84977f538aefd40720d6/plugins/teamharness/adapters/qwenpaw)
- **DeepSeek Harness**：有实际 runtime 和 adapter，但官方明确标记为 experimental，而且绑定 release candidate。[DSH README](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/deepseek-harness/README.md)

### 不能算正式接入

- **Claude Code adapter**：目录存在，但目前主要是占位 README 和记录日志的安装脚本，不是可用的完整适配器。[Claude Code adapter 目录](https://github.com/agentscope-ai/AgentTeams/tree/eeaab64391ccaec9118e84977f538aefd40720d6/plugins/teamharness/adapters/claude-code)
- **coding-cli-management**：`skills-alpha` 中有一条让 Manager 代替 Worker 调用 `claude`、`gemini`、`qodercli` 的实验路径。它使用高权限、非交互参数，不包含 Codex，也不是正式的 TeamHarness runtime adapter。[alpha skill](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/manager/agent/skills-alpha/coding-cli-management/SKILL.md)
- **Codex**：锁定提交中没有正式 adapter。

**RepoMesh 已确认：** 一个 Worker 同时只执行一个任务，每次执行尝试使用独立工作副本。

**建议：** RepoMesh 自己定义稳定的 `CodingHarnessAdapter`：

```text
prepare_attempt
start
stream_events
cancel
collect_artifacts
classify_result
cleanup
```

下面再接 Codex、Claude Code、Qoder、DSH 或其他执行器。这样 AgentTeams adapter 以后成熟时可以接进来，但不会反过来控制 RepoMesh 的任务模型。

## 5. 对外到底有哪些接口

不要用“总共有 13 个公开 API”这类数字描述 AgentTeams。它有多个性质不同的接口面，其中一些还是 Matrix、MinIO、Higress 等依赖系统的标准接口。

| 接口面 | 人话解释 | RepoMesh 是否建议直接使用 |
|---|---|---|
| Controller REST `/api/v1` | 管理 Manager、Worker、Team、Human，控制 Worker 生命周期，读取和干预 TeamHarness Project | **是，使用锁版本的窄客户端** |
| `agt` | Controller REST 的官方运维命令行；不是另一个调度器 | 人工运维可用，程序不要 shell-out |
| Kubernetes CRD | `Worker/Team/Human/Manager` 四类声明式资源 | in-cluster 可用；首阶段不要让 RepoMesh 强耦合 |
| TeamHarness MCP | Agent 在运行时里调用的协作工具 | 可复用语义，外面包 adapter |
| WorkerFlow MCP | 单个 Worker 内创建临时子 Agent | 不能代替 RepoMesh 的长期 Worker 调度 |
| Matrix Client/AppService | 房间、消息、成员和休眠 Worker 唤醒 | 通过 transport adapter 使用 |
| Dashboard BFF | 官方 Dashboard 的组合后端 | 可作参考，不是 Controller SDK |
| `agentteams plugin` CLI | 管理本地 `.agentteams/` 插件 | 与 Controller 管理无关 |

### Controller REST 的主要能力

Controller 默认监听 `:8090`。主要路由组包括：

- Manager、Worker、Team、Human 的增删查改；Human 没有更新端点。
- Worker 的 `wake`、`sleep`、`ensure-ready`、`ready`、`status`。
- TeamHarness Project 的创建、工作流、历史、产物、衍生会话，以及 pause/resume/replan/cancel/complete。
- 包上传、网关 consumer、短期存储凭据、Matrix token 和 AppService 管理。
- `/healthz`、`/api/v1/status`、`/api/v1/version`。

锁定提交中共有 13 条 Project 路由模式，另有 1 条 Worker checkpoint 路由。详细清单见[专项报告](./agentteams-api-cli-survey-2026-09-07.md#3-controller-rest-api-全路由)。

### 接口成熟度要保守看待

- 没有找到 Controller REST 的 OpenAPI、Swagger UI 或官方生成 SDK。
- URL 虽然叫 `/api/v1`，但不能据此推断已经有正式的长期兼容承诺。
- REST DTO 不是 CRD 的完整镜像，一些 CRD 字段没有通过 REST 暴露。
- `PUT` 不是标准 JSON Merge Patch。很多字符串只有非空时才更新，所以不一定能用空字符串清除字段。
- Manager 请求 DTO 中声明的 `remoteSkills` 在当前 handler 中没有真正写入，会被静默忽略。
- `/api/v1/version` 需要认证，但没有经过与其他资源路由相同的细粒度授权。

**建议：** RepoMesh 只接自己真正需要的一小组端点，并为每个端点做固定版本的契约测试和写后读回检查。

## 6. 创建成功不等于已经就绪

这是 RepoMesh 页面和状态机必须正确表达的地方。

调用 `POST /api/v1/workers` 后，Controller 先写入 Worker CR，然后异步 reconcile。刚创建时通常只能得到 `Pending`；`matrixUserID`、`roomID` 等字段可能仍为空。

```text
用户点击“创建 Worker”
        |
        v
Controller 接受请求 / 写入 CR
        |
        v
Pending -> Starting -> Running
   |          |
   `----------+----> Failed

Running 之后还要结合 ready、heartbeat、房间和团队成员状态判断是否真的可接单
```

当前 Worker reconcile 中实际观察到的 phase 包括：

```text
Pending / Starting / Running / Stopping / Sleeping / Stopped / Failed
```

不能把 `Updating` 当成当前 Worker 已实现的稳定状态。Human 还可能出现 `Degraded`，旧文档容易漏掉。

`Ready` 也不是 Worker CRD 的 phase。当前 `/status` 会在 backend 已运行且 Controller 内存中的 ready map 为真时，把结果临时显示为 Ready；Controller 重启会丢掉这份内存状态，直到 Worker 再次报告 ready。因此 RepoMesh 应分别保存“上游原始 phase”和“当前是否 ready”。

**建议：** RepoMesh 页面至少区分：

```text
未创建 -> 已提交 -> 准备中 -> 已就绪 -> 受限/降级 -> 失败
```

“团队就绪”也应是 RepoMesh 的组合判断，而不是只看某一次 POST 是否返回 200/201：

- Manager 可运行；
- Team CR 已 reconcile；
- Leader 和必需 Workers 已运行；
- Matrix 身份与必要房间存在；
- TeamHarness/关键工具健康；
- RepoMesh 还能拿到 Worker lease 和服务器容量。

## 7. 权限：谁能做什么

生产/正常部署使用 Bearer token。主要身份包括 Admin、Manager、Team Leader、L2 Human 和 Worker。

| 身份 | 当前代码级能力概要 |
|---|---|
| Admin / Manager | Controller 管理面全权限 |
| Team Leader | 读取本团队 Worker/Project；更新和控制权限范围内已有 Worker；创建和干预本团队 Project；**不能创建 Worker 资源** |
| L2 Human | 在 `accessibleTeams` 内读取 Team/Worker/Project、创建和干预 Project；只能修改范围内 Worker 的 `skills` |
| Worker | 读取和汇报自身状态，刷新自身短期凭据；不能任意读取 Project |

Team Leader 创建 Worker 的请求会在当前 handler 中被明确拒绝并返回 409。因此，虽然 Leader 负责业务派工，Worker 基础设施的创建仍属于 Manager/Admin/控制面职责。[资源 handler](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/internal/server/resource_handler.go)

还有一个安全问题需要单独记录：`Human.status.initialPassword` 的类型注释声称只展示一次，但当前实现会把它保存在 status 中，后续 GET/CLI 仍可能再次返回。RepoMesh 不应把这个字段展示在普通详情页，也不能假定它已经被服务端一次性销毁。

## 8. 两个 Project 不是一回事

### RepoMesh Project

这是用户创建的多仓项目。根据已确认决策：

- 一个 RepoMesh 管理多个相互独立的多仓项目；
- 每个项目拥有一个独立 AgentTeams 实例；
- 实例内只有一个 Manager；
- 每个仓库对应一支长期 Team。

### AgentTeams Project

这是 TeamHarness 的持久工作流对象，主要存放：

- 项目标题和来源；
- DAG/Loop 计划；
- Task 状态和结果；
- 历史快照、审计信息和产物引用。

它存放在对象存储中，不是第五种 CRD。真实身份是 `(team, project_id)`；同一个 `project_id` 在多个团队存在时，不带 `team` 查询会发生歧义并返回 409。写操作使用 ETag 做并发保护，并保留历史快照。

所以更合理的映射是：

```text
RepoMesh Project                 多仓项目/租户边界
  `---- RepoMesh Issue           用户提出的一项工作
          `---- IssueRun         一次计划与执行
                  `---- AgentTeams Project（可选执行镜像）
```

**建议：** RepoMesh 首阶段不要让 AgentTeams Project 成为唯一事实源。可以把它当作执行镜像和协作记录，RepoMesh 自己仍保存 Issue、Plan、Task、ExecutionAttempt、Queue 和 Delivery。

## 9. TeamHarness 实际提供什么

TeamHarness 通过 MCP 暴露以下工具：

| 工具 | 用途 |
|---|---|
| `message` | 给房间或外部 reply route 发消息 |
| `roomflow` | 创建、查看、描述和归档任务房间 |
| `filesync` | 在工作区和共享存储之间拉取、推送文件 |
| `artifact` | 把文件发布成 Matrix 文件消息 |
| `projectflow` | 创建计划、维护 DAG/Loop、接收结果、暂停或完成 Project |
| `taskflow` | 委托、确认、提交、检查和取消 Task |

[TeamHarness manifest](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/plugins/teamharness/plugin.yaml) / [MCP server](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/plugins/teamharness/mcp/server.py)

这些工具已经有真实实现，但插件清单仍是 `v1alpha1`，版本为 `0.1.0`。部分角色限制由 MCP server 检查，另一些依赖安装时给不同角色注入不同工具或提示词。因此应把它当作“可复用但仍在演化的协作层”。

WorkerFlow 只负责单个 Worker 内部的临时子 Agent。源码明确说明临时 Agent 不是 TeamHarness Worker，所以它不能承担 RepoMesh 的长期团队、跨 issue 排队或单 Worker 租约。

## 10. `agt` 命令行应该怎么理解

`agt` 是 Controller REST 的官方薄客户端，适合管理员或排障人员使用。它覆盖：

```text
agt apply
agt create worker|team|human|manager
agt get workers|teams|humans|managers|projects
agt update worker|team|manager
agt delete worker|team|human|manager
agt project create|pause|resume|replan|cancel|complete
agt worker wake|sleep|ensure-ready|status|report-ready
agt status
agt version
agt llm-preflight
agt rotate appservice-token
```

`agt create worker` 默认会在 POST 之后继续轮询 Worker status；`--no-wait` 只表示不等待真正就绪。这也再次证明“资源已创建”和“运行时已就绪”是两个状态。

需要注意：

- 官方文档中仍有 `agt get worker` 单数示例，当前 Cobra 命令实际注册的是复数 `agt get workers`。
- `agt apply` 没有 server-side apply、dry-run 或 prune。
- Project 相关 CLI 没有完整处理跨团队同名 Project 的 `team` 消歧。
- `agt` 并没有进入所有 Worker 镜像；Controller、Manager 和部分 Worker 镜像包含它，但 QwenPaw、DeepSeek Harness 镜像并不都包含。
- 官方安装器不会把 `agt` 安装成宿主机全局命令；不过可以从源码构建或从包含它的镜像中使用，所以也不能说“绝对没有宿主机使用方式”。

RepoMesh 程序不应通过执行 `agt` 命令来集成 AgentTeams。更稳妥的方式是直接调用一组很窄的 REST 接口。

## 11. RepoMesh 应复用什么、自己掌握什么

### 可以复用

- Manager、Worker、Team、Human 的资源管理和 reconcile。
- Worker 的 wake/sleep/ensure-ready/status。
- Matrix 房间、消息和 AppService 唤醒机制。
- TeamHarness 的 task room、文件同步、计划、任务和产物语义。
- Project workflow/history/artifact 作为执行观察和审计补充。

### RepoMesh 必须自己掌握

- 多仓项目和项目间隔离；
- Git 平台授权、仓库集合和仓库权限；
- Issue、路由、计划、任务、执行尝试和交付；
- 全局容量、项目额度、排队、公平性和 Worker lease；
- 单 Worker 单任务；
- 每次执行的独立工作副本；
- Codex/Claude Code/Qoder 等 coding harness 适配；
- 验证门禁、提交、PR 和最终交付状态；
- 跨实例可观测性、审计和故障恢复。

建议的边界：

```text
RepoMesh Control Plane（产品事实源）
  |
  +---- Project / RepositoryAuthorization / Instance
  +---- Issue / IssueSession / Plan / Task
  +---- ExecutionAttempt / WorkerLease / Queue / Delivery
  |
  `---- AgentTeamsInstanceAdapter
          +---- Controller REST
          +---- MatrixTransportAdapter
          `---- CodingHarnessAdapter
```

## 12. 从第一个多仓项目看完整链路

下面是把已确认决策和建议边界串起来后的产品主线。它不是声称上游已经全部实现。

```text
创建 RepoMesh 多仓项目                         [RepoMesh]
        |
授权并选择多个仓库                             [RepoMesh]
        |
准备独立 AgentTeams 实例                       [复用上游 + RepoMesh 编排]
        |
创建/检查 Manager、每仓 Team、Leader、Workers  [复用上游资源 API]
        |
等待团队达到 RepoMesh 的“就绪”条件             [RepoMesh 组合状态]
        |
接收 issue                                      [RepoMesh]
        |
跨仓/范围不明 -> Manager；明确单仓 -> Leader    [RepoMesh 已确认]
        |
创建 IssueSession 和可选 Task Room              [RepoMesh + Matrix]
        |
形成计划并写入任务记录                          [RepoMesh；可镜像到 TeamHarness]
        |
申请 Worker lease、排队或并行                    [RepoMesh]
        |
为每次尝试准备独立工作副本                       [RepoMesh + Coding Harness]
        |
执行、收集事件和产物                             [Harness + TeamHarness]
        |
验证、验收、提交或创建 PR                        [RepoMesh 门禁]
        |
交付并释放 Worker/容量                           [RepoMesh]
```

## 13. 已确认的上游文档/实现不一致

这些问题不一定都是严重 bug，但会影响 RepoMesh 集成：

1. Human `initialPassword` 的“一次展示”注释与实际持久返回行为不一致。
2. Manager REST DTO 声明 `remoteSkills`，当前 handler/CRD 没有真正接住。
3. REST `PUT` 的实际部分更新语义没有由正式 OpenAPI 描述。
4. CLI 的单复数、runtime help、Project `team` 参数与当前实现并不完全一致。
5. Project API 文档中仍残留“只有四个只读端点”的旧描述。
6. TeamHarness 自称 runtime-neutral，但当前基础安装器实际只自动探测 QwenPaw。
7. Claude Code adapter 目录的存在不代表功能已经完成。

因此，RepoMesh 应以锁定提交下的路由、handler、测试和真实契约测试为准，不能只看 README 或目录名。

## 14. 仍需部署验证

以下内容目前只能标记为待验证：

1. 同一台服务器上运行多个 embedded 实例时，Controller 地址、token、资源前缀、Matrix、对象存储前缀是否能完全隔离。
2. 创建 Manager、Workers、Team 后，完整 phase、错误、重试和恢复状态机是什么。
3. QwenPaw 重启、容器替换或 session 压缩以后，房间上下文和持久历史具体怎样恢复。
4. 多个 Issue 同时写 TeamHarness Project 时，ETag 冲突、重试和审计是否满足 RepoMesh 要求。
5. Worker sleep/wake、AppService mention 唤醒和 `ensure-ready` 的实际延迟与失败表现。
6. Matrix 房间成员变化后，旧消息、上下文缓冲和权限是否符合预期。
7. RepoMesh 自定义 CodingHarnessAdapter 如何安全接入 Codex、Claude Code、Qoder，并保证工作副本、取消、超时和产物回收。

在这些验证完成前，文档应使用“源码显示”“计划采用”“建议”或“待验证”，不应写成已经部署验证过的能力。

## 15. 主要源码入口

- [Controller 路由注册](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/internal/server/http.go)
- [Controller REST DTO](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/internal/server/types.go)
- [资源 handler 与权限细节](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/internal/server/resource_handler.go)
- [四种 CRD 的 Go 类型](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/api/v1beta1/types.go)
- [Project API 文档](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/docs/usage/project-workflow-api.md)
- [Project handler](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/internal/server/project_handler.go)
- [QwenPaw Matrix channel](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/plugins/agentteams-matrix-channel/agentteams_matrix/channel.py)
- [TeamHarness manifest](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/plugins/teamharness/plugin.yaml)
- [TeamHarness MCP server](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/plugins/teamharness/mcp/server.py)
- [`agt` 根命令](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/cmd/agt/main.go)

---

这份文档面向产品和架构讨论，刻意不复制每一个请求字段。需要实现时，以同日的[详细接口调查](./agentteams-api-cli-survey-2026-09-07.md)为索引，再回到锁定源码逐项确认。
