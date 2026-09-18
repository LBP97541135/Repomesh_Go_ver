# AgentTeams 接入：源码核查与待验证清单

调查日期：2026-09-09。本文回答当前 RepoMesh 设计依赖 AgentTeams 的哪些能力、哪些已有静态依据、哪些必须通过实验确认。本文保留编制时的调查与实验设计，**不是验收通过记录，也不新增产品决定**。后续已执行组件实验与最新源码部署；初轮过程保留在[09-09记录](../../validation/agentteams-2026-09-09/reports/validation-status.md)，当前新版进度和边界以[09-10验证状态](../../validation/agentteams-2026-09-10/reports/validation-status.md)为准。

后续更新（同日）：用户已采用按最新版本复用原生仓内 DAG，RepoMesh 保留跨仓／业务放行及有界 Loop；决定见 ADR-0014／0015 补充，当前分工、版本与证据见 [Graph／Loop 专题](../current/graph-loop-design.md)。重新查询官方最新 main 仍是下述锁定提交，最新 release 为 v1.2.3。后续[真实 HTTP／MinIO 原始记录](../../validation/agentteams-2026-09-09/evidence/controller-live-results.json)的 LIVE-C03／04 已覆盖 paused replan 和显式管理员陈旧写，部分状态摘要尚未同步；不将下文“编制时未运行”当成当前全部证据，也不扩大为部署 Worker 或 RepoMesh 验收。

### 09-10 新版增量记录（不覆盖原清单）

新基线 `517caff9280242a00a4d4c06365352b9e41659c6` 相对 09-09 的差异及三包官方回归见 [controller-delta.md](../../validation/agentteams-2026-09-10/reports/controller-delta.md)：**252 顶层＋59 子测试＝311 通过事件，0 fail/skip**。新增路径的[真实 HTTP／SA／CLI](../../validation/agentteams-2026-09-10/reports/controller-api-live.md)记录 **51 项主观察、31 项真实 SA TeamLeader 范围检查**；其计数不是独立业务场景或 AT-01—12 全套通过数。SA 归属与实际 Team membership 已核对，范围拒绝为跨 Team 404；匿名／无效 token 401 仅证明认证拒绝，普通 Worker 角色未由该组结果覆盖。

**09-10 双在线补测：** [c/d 双实例读取矩阵](../../validation/agentteams-2026-09-10/reports/controller-twin-read-live.md)完成 **313 个断言、260 次 Controller HTTP**；两实例各有两个同名 Team，使用 Admin 与真实 SA TeamLeader 验证新增读取路径。**30 次错实例请求均为 401，每次来源前后请求均为 200；24 个夹具原字节哈希前后不变**。这是停止／未托管引用和元数据夹具的读取契约验证，不是业务验收。原 51／31 项保留独立历史计数；本次在线、TLS 与来源身份有效的对照补充旧时钟异常观察，不覆盖或改写其原始证据。

**09-10 存储与模型补证：** [真实S3 trace](../../validation/agentteams-2026-09-10/reports/mermaid-storage-read-live.md)补齐20项检查：JSON前后正对照读取TaskMeta，Mermaid true/false有限窗口内目标TaskMeta的HEAD／GET均0。[新版Manager实际推理](../../validation/agentteams-2026-09-10/reports/manager-model-smoke-new.md)随后通过一次Matrix消息返回精确标记，原生会话及用量确认1次模型调用、未观察到工具调用；Console503和后置读探针错误保留。以上均不代替RepoMesh业务验收。

| 本轮涉及的子契约 | 已观察事实及保留边界 |
| --- | --- |
| AT-01／11：workflow 与单 Task 检查 | 缺省 JSON、Mermaid 输出与 inspection 已实际读取；`format=json` 为400。TaskMeta 原始 status 与缺失／破损／归属不符时的规范化 fallback 分开；history 读取、trace 提示、元数据中的产物路径不证明状态机审计、真实 span 或文件已存在。 |
| AT-05：新增读取路径范围 | 跨 Team 同名 Project 的 API 消歧及真实 TeamLeader 本 Team200／跨 Team404已观察；实际 CLI detail 即使传 `--team` 仍409，不能按API成功推定CLI已转发范围。 |
| 展示解析缺陷 | 有效 task ID `end` 的 Mermaid API200原字节在[真实浏览器](../../validation/agentteams-2026-09-10/reports/mermaid-browser-live.md)解析失败；51项观察包含这条缺陷，不标记渲染验收通过。 |

源码逐函数未变的旧状态机、CAS、取消、身份缓存等仍只引用 09-09 证据原范围，不改成新版已完整重跑或原缺口已修复。CFG01—05后续已完成真实REST／CLI／CR、配置投影及QwenPaw进程内存active model读回，见[Manager实测](../../validation/agentteams-2026-09-10/reports/manager-config-live.md)；保留错误尝试和时钟／证书恢复记录，CFG配置成功不外推为业务验收；后续基本推理单独见上述专项。下文保留 09-09 调查、实验设计及业务通过条件。

## 1. 结论与调查范围

首要问题是：RepoMesh 能否在复用 AgentTeams 团队、通信与运行生命周期的同时，实际控制工作目标、计划生效、派工、权限和故障恢复。仅启动几个 Agent、完成一次聊天或看到任务状态成功，不能证明这些设计成立。

现行要求来自 [ADR-0001](../adr/0001-agentteams-issue-concurrency-and-isolation.md) §8—§9、[受控接入 ADR-0011](../adr/0011-agentteams-controlled-integration.md)、ADR-0012—0019、[架构 v1](../current/architecture-design-v1.md) §12、[会话后端专项](../current/draft-conversation-backend-design.md)及[创建契约](../current/issue-page-create-api-contract.md)。本次由主 Agent 与独立研究 Agent 分工核对这些要求，并读取上游原始代码交叉验证。

上游范围固定为 `agentscope-ai/AgentTeams` 提交 `eeaab64391ccaec9118e84977f538aefd40720d6`。本机保留了该提交的 Git 对象库，位置为 `C:/Users/18092/AppData/Local/Temp/agentteams-api-survey-full-eeaab`；本次使用 `git show <commit>:<path>` 读取原始 blob，没有检出、修改或运行上游代码。现存 `D:/Project4work/AgentTeams` 的 HEAD 为另一提交，未混入结论。本次没有调查最新 main，也没有启动 Controller、Matrix、模型或 Docker 实验。

原 [API／CLI 调研](../agentteams-survey-2026-09-07/agentteams-api-cli-survey-2026-09-07.md)保存 09-07 的范围；其中旧聊天绑定、旧接入建议不能覆盖后续 ADR-0019 及当前创建契约。

## 2. 源码已经揭示的具体限制

这些不是“可能什么都支持，只差测一下”。其中有明确的契约限制和未覆盖路径，需要适配方案后再验证。它们尚不构成部署环境中的漏洞或故障复现。

### 2.1 原生 pause 后不能直接 REST replan

Controller 的 `PauseProject` 将状态设为 `paused`；`ReplanProject` 要求状态为 `active`、`plan_type` 为 `dag` 或尚未设置，且无 `in_progress`／`submitted` 任务，否则返回 409；明确设置的非 `dag` 类型不被接受。因此，文档的“停新派工 → 收敛 → 应用新计划”是业务要求，**不能直接翻译为 `pause → replan` 两次原生 REST 调用**。[暂停写入](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/internal/server/project_handler.go#L2137)、[replan 前置条件](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/internal/server/project_handler.go#L2193)。

需要验证的候选接法是：RepoMesh 自身派工门禁持续关闭时恢复上游 active 再重规划；或补丁允许受控的 paused replan。前一种须证明 resume 到 replan 的间隙没有其他派工路径，后一种须验证新的状态约束。本文不替两种接法作最终选择。下一轮有限 DAG 的更新也要经过此项，不能提前把上游 Project 完结后假定仍可直接 replan。

### 2.2 REST 校验不能覆盖原生 MCP 和文件写入

上游 Controller 的 Manager 身份具有广泛权限。TeamHarness `taskflow` 的工具 Schema 接收 `role`，`_role()` 优先使用传入值；如果把这一工具面直接作为可信业务角色边界，不能仅凭其 leader 检查证明真实调用者身份。[Controller 授权](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/internal/auth/authorizer.go#L41)、[工具 role 字段](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/plugins/teamharness/mcp/server.py#L460)、[角色解析](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/plugins/teamharness/mcp/server.py#L3628)。

`ready_nodes` 会对非 active Project 返回空，但 `delegate_task` 路径没有同样的 active 检查；`plan_dag` 也没有 REST replan 的在途／submitted 前置条件。绕过“先查 ready_nodes”的约定直接调用时，需要由受控适配阻止不获准的实际写入或派工，不能只要求 Agent 按提示操作。[ready_nodes](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/plugins/teamharness/mcp/server.py#L2974)、[delegate_task](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/plugins/teamharness/mcp/server.py#L4100)、[plan_dag](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/plugins/teamharness/mcp/server.py#L3397)。

ADR-0011 已允许为此增加受控工具／执行接入及小范围上游补丁。要验证的是所有相关路径是否落实同一约束，包括原生工具、Controller、共享文件与 Git／模型执行，而不是仅验证 RepoMesh 页面按钮是否禁用。

### 2.3 REST 与 MCP 共享状态的并发写不是同一套保证

Controller 写 Project 时使用读出的 ETag；MCP `_sync_project` 通过文件同步执行 `mc mirror --overwrite` 或 `mc cp`，该路径未携带同一条件写约束。先拉取再普通覆盖仍可能覆盖拉取之后发生的暂停或计划更新。[REST 条件写调用](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/internal/server/project_handler.go#L2152)、[MCP 同步](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/plugins/teamharness/mcp/server.py#L3823)、[覆盖上传命令](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/plugins/teamharness/mcp/server.py#L2494)。

另外，MCP 拉取失败时拒绝写入的动作集合没有包含 `plan_dag`／`plan_loop`；`plan_dag` 调用同步后未核对其布尔结果即返回 `ok: true`。所以工具成功回执不必然证明共享存储上的计划已保存，不能直接作为 RepoMesh 的计划生效依据。[动作集合与失败处理](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/plugins/teamharness/mcp/server.py#L3239)、[plan_dag 保存及回执](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/plugins/teamharness/mcp/server.py#L3397)。

需要先定义写入责任、并发控制和可核查的生效依据，再用延迟、断网和交错写入验证。不能把单个 REST 端点的冲突测试推广为整个多写入方系统已经可靠。

### 2.4 上游 task_id 不只是某个 Project 内的短编号

TeamHarness 的任务目录为 `shared/tasks/{task_id}`，不包含 `project_id`；委派通知的 Matrix 事务标识为 `delegate-{task_id}`。同一共享工作范围里，不同 Issue、不同轮次或新 Attempt 若都使用 `T1`，存在路径／操作身份冲突风险。具体是否被已有检查拒绝、通知是否被去重以及产物如何受影响，需要运行实验确认。[任务路径](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/plugins/teamharness/mcp/server.py#L2803)、[同步路径](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/plugins/teamharness/mcp/server.py#L3811)、[通知标识](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/plugins/teamharness/mcp/server.py#L4048)。

RepoMesh 到上游的具体标识编码尚未冻结。验收应保证不同业务执行身份不会碰撞，同时同一次逻辑操作重试保持原身份；不能简单地“每次重试都生成随机新任务”。

### 2.5 房间 session 不能直接证明一会话多 Issue 隔离

QwenPaw Matrix 通道按 `room_id` 构造 `session_id = matrix:{room_id}`，请求中的用户键也使用房间；同一 Agent 在同一房间里的消息落入同一房间 session。请求协调键同样按房间生成。[session 构造](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/plugins/agentteams-matrix-channel/agentteams_matrix/channel.py#L3120)、[房间协调键](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/plugins/agentteams-matrix-channel/agentteams_matrix/channel.py#L425)。

因此，ADR-0019 采用一会话多独立 Issue 后，仍需证明目标、计划、权限和结果可以分别归属，并验证长期记忆／工作文件是否混用。业务会话可以共同讨论多项；验收不是禁止 Manager 知道另一项，而是防止另一项的要求被当成当前操作的执行依据。也不能把不同 room 的 session 标识直接当作跨房并行性能证明。

### 2.6 Ready、探针、已读和模型处理是不同证据

创建 Worker 返回的是保存资源后的结果；生命周期 Handler 的 Ready 标记保存在内存 map，按 Worker 名称记录。`ensure-ready` 仍可能返回 Running，Controller 重启及旧运行实例迟到报告需要单独验证。[创建回执](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/internal/server/resource_handler.go#L124)、[Ready 状态与查询](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/internal/server/lifecycle_handler.go#L112)。

Matrix 通道对 readiness probe 直接回包，不将该探针送入 Agent 队列；普通消息的已读回执也在后续处理前发送。探针成功或已读不能证明 Manager 已用模型处理工作。[探针与已读处理](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/plugins/agentteams-matrix-channel/agentteams_matrix/channel.py#L2684)。

未提及机器人的消息另有内存历史缓冲，默认上限 50；这不是 RepoMesh 的持久投递队列或完整模型状态备份。[默认值](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/plugins/agentteams-matrix-channel/agentteams_matrix/channel.py#L295)、[内存记录及清理](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/plugins/agentteams-matrix-channel/agentteams_matrix/channel.py#L1931)。

### 2.7 Docker 资源字段和独立实例隔离不能靠配置名称推断

Worker 创建会保存 `resources`，但锁定版本的 Docker `dockerHostConfig`／`buildCreatePayload` 没有把 CPU、内存、进程数限制写入容器创建 payload。只能据此确认**这条原生创建路径没有完成该投影**，不能因此断言宿主其他管理层绝无约束；实际限制必须检查最终容器／cgroup，并通过受控负载验证。[资源保存](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/internal/server/resource_handler.go#L95)、[Docker 创建 payload](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/internal/backend/docker.go#L644)。

Manager 基础设施使用固定 Matrix 用户和 Gateway consumer 名称；embedded 挂载及控制台端口来自实例配置，默认主机端口为 18888。独立资源名字不足以证明隔离，必须对两个完整实例的配置、网络、卷、凭据和生命周期操作进行交叉验证。[Manager 身份](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/internal/service/provisioner.go#L1345)、[挂载与端口](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/internal/controller/manager_reconcile_container.go#L221)。

### 2.8 上游取消、状态摘要与历史也需要单独取证

TeamHarness 的 `cancel_task` 修改任务／Project 元数据并同步，没有在该动作中调用执行进程终止接口；因此取消记录不能作为释放 Worker 占用的唯一证明。Controller 的取消路径还分别写 Project 和任务文件，第一步成功后第二步可以失败，需要验证恢复后的收敛。[MCP 取消](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/plugins/teamharness/mcp/server.py#L4412)、[Controller 分步写入](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/internal/server/project_handler.go#L2546)。

Controller 图节点摘要把 `submitted` 与 `in_progress` 都显示为 `in-progress`，把 `cancelled` 与 `blocked` 都显示为 `blocked`。重规划收尾、业务验收和进程回收不能只依赖这份压缩状态。[状态映射](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/internal/server/project_handler.go#L207)。

上游历史快照和干预后的 Matrix 通知都是 best effort：失败可只记日志而不阻止主写。RepoMesh 需要自己的持久事实与恢复核查，不能把这些通知或上游有限历史当作完整审计保证。[历史快照](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/internal/server/project_handler.go#L1907)、[干预通知](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/internal/server/project_handler.go#L1988)。

## 3. 建议的验证清单

P0 表示应在依赖它的运行适配定稿前解决；P1 表示在已有最小闭环后完成可靠性与容量验证。本清单编制时所有项目均未运行；后续执行状态见[验证执行记录](../../validation/agentteams-2026-09-09/reports/validation-status.md)。其中“联调”需要 RepoMesh 适配或最小验证夹具，不能仅测试原生 AgentTeams 后就宣布通过。

| 编号／优先级 | 要验证什么 | 最小实验与通过条件 | 责任边界 |
| --- | --- | --- | --- |
| AT-01／P0 | 固定版本及实际契约 | 固定 Controller、TeamHarness、runtime、镜像 digest、Matrix 与存储版本；创建 Manager／Worker／Team，配置后逐项读回并确认实际加载。明确列出已生效、被忽略及不支持字段，不以 2xx 代替。 | 上游契约，RepoMesh 适配使用。 |
| AT-02／P0 | 实际执行受 RepoMesh 控制 | 在无许可、旧计划、无 Worker 占用、权限失效、预算耗尽时请求执行；分别经过受控工具及原生 MCP／REST／文件等可达路径。所有不获准动作都不能产生业务副作用，合法路径仍能完整执行。测试身份由可信适配绑定，不能由模型自报 role。 | 受控适配＋上游运行联调；依据 §2.2。 |
| AT-03／P0 | 原生 DAG 复用、停派工、重规划和下一轮 | 先通过真实受控 Adapter 验证原生建图、依赖／环校验、顺序／并行／汇合和 ready_nodes；assigned 重复就绪不得重复启动，上游 ready 不绕过跨仓与业务前驱。再验证 paused replan 409 对应的受控接法，覆盖 in_progress、submitted、空闲、completed、跨轮复用、多目标部分更新和崩溃。必要目标全读回才落实安排；范围内新轮次不产生新业务 Plan Version。 | 上游仓内 DAG＋RepoMesh 跨仓／Loop／执行控制；依据 §2.1 与 Graph／Loop 专题。 |
| AT-04／P0 | 共享状态不会被陈旧写覆盖 | 安排 MCP 拉取旧状态后延迟上传，此时 REST 暂停或改计划；再放行上传。另断开存储后调用 plan_dag／提交结果。必须阻止静默覆盖、识别未同步结果并保持未生效，恢复从权威记录核查。 | 控制面／MCP／存储共同验证；依据 §2.3。 |
| AT-05／P0 | Issue、轮次、Task、Attempt 标识映射 | 同 Team 两个 Issue 各有首任务，再做同项下一轮、同次重试和新 Attempt；另建不同 Team 下同名的 Project，要求显式指定 Team，引用绑定 `instance/team/project_id`。上游记录及通知不碰撞，重试不重复派工，新尝试不覆盖旧产物／被误去重；迟到结果仍绑定原尝试。 | RepoMesh 映射＋TeamHarness；依据 §2.4。 |
| AT-06／P0 | 一会话多 Issue 的正确目标与权限 | 同一主会话交错讨论两个目标、两个仓库范围；加入“这个也改一下”等歧义、从 A 页面明确操作 B、迟到结果、不同权限用户和 runtime 重启。明确目标保持原归属，歧义澄清，不借共享房间获得额外权限；补测不同房间与角色执行文件。 | 消息路由／可信上下文适配＋runtime；依据 §2.5。 |
| AT-07／P0 | 首次准备、恢复和真实就绪 | 首消息与页面建项并发触发；重复待办、部分建房失败、响应丢失、Controller／runtime 分别重启、旧 ready 报告迟到。只复用一套项目实例及正确会话关联，业务结果保留；分别证明资源存在、runtime 存活、房间可达、实际处理。 | RepoMesh 持久待办＋上游生命周期；依据 §2.6。 |
| AT-08／P0 | Worker 单活跃与旧 Attempt 停止 | 两项争同一 Worker，以及团队／项目／全局最后一个槽位；准备期间撤权或取消，启动端再次核验；启动超时但旧进程仍工作，随后停机、断网和再派工。占用不重复，未确认旧写入能力失效前不允许冲突执行；授权失效不能视为 CPU／内存已释放；新旧副本、分支、构建目录及结果分开。 | RepoMesh 资源／主机执行控制＋运行时；ADR-0017。 |
| AT-09／P0 | 同机多实例与资源强制 | 两个实例使用相同团队／成员短名称、接入同一测试仓库。交叉请求、读取／写入、停止和恢复必须保持项目边界；检查实际端口、挂载、凭据、网络与 cgroup，受控 CPU／内存负载不能越过拟定上限。 | 部署配置＋受限主机执行＋上游；依据 §2.7。 |
| AT-10／P1 | 消息投递与恢复 | 准备期连续保存消息，覆盖超过缓冲上限、未提及／提及、普通回复／thread、事件重放、投递响应丢失、runtime 重启和恢复。已保存消息不依赖内存缓冲存续；前序未知先核查，业务操作不因重放重复；不将已读当模型处理。 | RepoMesh 消息持久化／Matrix／runtime 联调。 |
| AT-11／P1 | 状态与产物证据可信 | 核查上游任务 submitted／completed、Project completed、artifact 的真实含义；覆盖附件缺失、存储上传失败、重复和晚到结果、当前读权变化。采集到的事实保留来源和版本，不把上游 completed 自动转换成 RepoMesh 已验收／已交付。 | 上游观测适配＋RepoMesh 业务判断。 |
| AT-12／P1 | 单机容量、并发与恢复成本 | 测量空闲实例、启动峰值、Manager／Leader、Worker、重验证负载、存储及模型调用，并测量跨项目额度争用时的吞吐和延迟。给出实测启动／恢复时间、CPU／内存／磁盘及采集延迟，证明控制面仍可响应；额度占用的原子正确性由 AT-08 验证，无测量前不宣称可支持固定项目／Worker 数。 | 上游资源观测＋RepoMesh 全局调度。 |

AT-01 的契约核对已有明确警示：Worker 创建未复制 `remoteSkills`、Manager 创建／更新亦未接通相同能力，见原 [API 调研 §3.1](../agentteams-survey-2026-09-07/agentteams-api-cli-survey-2026-09-07.md)。Skill 工程尚未开发，这些字段暂不成为首轮依赖；对实际选用的 `mcpServers`、runtime、模型配置等仍需“写入 → 读回 → 实际加载”核对。

runtime 组合也不能只写“最新版”：锁定源码的 QwenPaw Docker 构建默认和依赖元数据指向 2.0.1，而 checkpoint 代理注释描述 2.1 的能力。若测试或恢复方案依赖 checkpoint，必须核对实际镜像及可用端点，不能仅凭 Controller 有代理路由就认为 runtime 支持。[Docker 构建默认](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/qwenpaw/Dockerfile#L24)、[依赖元数据](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/qwenpaw/pyproject.toml#L13)、[checkpoint 代理说明](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/internal/server/worker_checkpoints.go#L3)。

## 4. 哪些不应交给 AgentTeams 证明

| 范围 | RepoMesh 自己必须完成的验证 |
| --- | --- |
| 创建事务与幂等 | Issue／主 ChangeSet／必要会话／来源／快照／待办同事务、原操作查询、删除占位、201／200／410 等是 RepoMesh 契约。上游 POST 成功不能证明这些成立。 |
| 浏览器 REST／SSE | Issue 失效通知、游标恢复、快照重查、断连和缓存清理由 RepoMesh 验证；上游可读 history 不能证明页面订阅完整。上游状态采集仅是 AT-11 的输入。 |
| 跨仓 Graph、Loop 与预算策略 | 仓内 DAG 建图、依赖校验及候选就绪计算复用上游；跨仓前驱、业务结果采纳、轮次上限、累计用量、许可／生效和实际派工由 RepoMesh 验证。AT-02／03 证明原生复用与控制同时成立，不另造通用仓内引擎替代验证。 |
| GitHub 授权与交付 | GitHub App 权限交集、分支限制、人工合并、固定组合验收及 ChangeSet 完成由 RepoMesh／GitHub 联调验证；不能以 TeamHarness completed 替代。Agent 实际 Git 操作的受控路径仍属于 AT-02／08。 |
| 业务验证者独立性 | 哪个 Worker 是本轮业务作者、谁可正式验证、证据是否适用于最终组合由 RepoMesh 管理；上游角色名或提交成功不作证明。 |

“不是上游独立负责”不表示可以不测，而是不能把 RepoMesh 尚未实现的约束误列为 AgentTeams 原生保证。

## 5. 首轮如何收敛，以及哪些后置

建议先以现有架构建议中的 QwenPaw 路径做一个固定版本最小环境；实际 runtime 组合仍需在验证计划中明确，不据本次调查宣布正式采用。先做 AT-01 的连接与契约检查，再集中解决以下三组可行性问题：

1. **控制与一致性：AT-02—05。** 一个原生工具旁路、一次暂停期间误派工或一次陈旧覆盖，都足以破坏计划与权限约束。需要先明确适配／补丁边界。
2. **会话与启动：AT-06—07。** 用当前一会话多 Issue 和双入口创建场景测试，避免只复现已经被替代的“每 Issue 一个房间”演示。
3. **执行与部署隔离：AT-08—09。** 验证两个事项争 Worker、旧尝试失联和两项目同机，不只验证单任务正常结束。

三组通过后，再扩大消息故障场景、状态采集和容量测量（AT-10—12）。固定版本上游测试即使通过，也不能替代 RepoMesh 适配层联调；正常链路通过也不意味着所有故障路径已覆盖。

当前后置项：

- **Skill 工程：** 按 ADR-0009 暂缓；未来恢复后再测按 Issue 固定版本、分发／实际加载、长期 Manager／Leader 热替换影响、发布与回退。首轮仅核实必要已有工具能实际加载。
- **审批模式及 Dashboard HITL：** 产品当前 YOLO。现在仍测许可、版本与实际执行控制，后续人工审批流程另验；不依赖较新 Dashboard 功能证明锁定 Controller 能力。
- **其他 coding runtime：** 不把 Codex／Claude Code／Qoder 的完整 TeamHarness 接入当作已具备能力。锁定仓库 Claude Code adapter 仍描述未来 hook 接入，DeepSeek Harness adapter 标为 experimental；需要时逐个验证，不同时建立多 runtime 验收平台。[Claude Code adapter](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/plugins/teamharness/adapters/claude-code/README.md)、[DeepSeek adapter](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/plugins/teamharness/adapters/deepseek-harness/README.md)。
- **超出首期的部署形态：** 不将多机高可用、共享管理域多租户或每 Agent 一 VM 加入本轮单机独立实例的必验范围。

## 6. 每项验证必须留下的结果

每个 AT 编号应记录版本／镜像与配置、具体场景、持久业务及上游标识、请求／响应、事件／状态读回、日志、实际副作用、故障注入与恢复过程。结果至少区分“原生支持”“需配置”“需 RepoMesh 适配／上游补丁”“当前不支持”“未运行”，并记录失败影响和负责修复的层。

通过条件必须包含最终记录、资源和实际操作，而不只有 Agent 的口头回复或某个 HTTP／工具成功值。材料须屏蔽真实密钥和令牌；普通聊天缓冲、模型隐藏推理不能替代可审计的操作来源和结果。

本文的调查阶段完成了文档与固定版本源码核查。后续已按 AT-01—12 开始实际验证，过程与结果统一维护在[验证执行记录](../../validation/agentteams-2026-09-09/reports/validation-status.md)；组件复现成功不能等同运行或业务验收通过。
