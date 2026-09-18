# API、后端持久化与恢复审查

审查日期：2026-09-12。审查对象为当前工作区文档，未修改既有契约或代码。本报告的结论是设计审查，不是接口、数据库或 AgentTeams 运行验收。

当前可以按已采用契约开始独立的项目／Issue 持久化和查询切片。首批六包尚不宜作为无缺口的整体实施基线：认证、模型三操作、来源与恢复新增方案仍待采用，而且模型 Key 保存存在没有终结路径的故障场景。完整 Manager 工作闭环还缺可信消息／运行／执行适配，不能从管理闭环或上游读取接口推导完成。

## 高可信发现

### AB-01 · P1 · Key 保存请求未抵达服务器时，原操作恢复无法结束

**位置与有效性：** 这是当前六项待审包里的候选设计缺陷，尚不是已经运行的缺陷，也不是被历史规则替代的旧建议。

- [模型浏览器草案](../../current/model-settings-browser-api-draft.md)第 65 行要求发送后立即清掉 Key，未知时不重新索要 Key 重发；第 75 行规定没有操作记录的 404 仍未知；第 131 行要求只查询原 saveId，未知期间不换键重发。
- [模型内部草案](../../current/backend-model-operations-draft.md)第 20 行明确没有公开取消入口；第 78 行只能在已收到合法规范化输入、取得操作槽位后保存确定 rejected；第 80 行明确 503 等前置错误通常不占永久拒绝槽位，而含 Key 的浏览器保存只能查询。
- [恢复总表](../../current/first-batch-recovery-design.md)第 34—36 行进一步排除了 Key 原输入重试和按超时终结，只有查询／稍后处理。

**故障情景：** 用户首次保存供应商及 Key。浏览器发出请求后清掉 Key，网络在请求到达服务器之前中断；或者服务器在成功占用操作槽位前返回可恢复的 503。服务器没有 ModelOperation，后续 GET 永远返回 MODEL_SAVE_NOT_FOUND。没有旧请求继续运行，也没有后台主体可以生成 committed 或 rejected，但页面始终只能继续查询。重新输入 Key、重新保存或换键都不符合现行恢复规则。

**影响：** 普通网络故障就能使 F05 的第一次有效模型配置无法完成。由于有效模型配置又是正式创建 Issue 的前置条件，该问题阻塞首批管理闭环的失败恢复标准。现有 rejected 机制只解决已经取得槽位的拒绝，不能解决从未进入持久层的请求。

**最小修正建议：** 补一个受当前主体鉴权的原操作终结协议。它与保存争用相同唯一槽位：若保存已提交，返回原结果；否则原子记录“未提交且禁止后续保存”的无秘密终结占位，随后允许用户明确建立新保存操作。迟到原请求必须先命中该占位。另一条路线是采用经明确设计的受保护暂存和原键重试，但不能把等待时间当成旧请求已停止的证明。具体路线由后续设计决定，本次未代为采用。

**验收必须增加：** 请求尚未到达服务器就断网；持久槽位之前 503；终结与迟到保存并发；终结回执丢失；已经 committed 后请求终结。五种情况都应有唯一可查询结果，并且不重新使用旧键。

## 需要在相应批次前补齐的衔接

以下与 AB-01 分开。它们不是“整份后端不能开始”的理由，也没有把明确暂缓能力算成首批全部阻塞。

### AB-02 · P2 · 旧 Issue 固定模型配置的承诺尚未落到持久归属

- [模型内部草案](../../current/backend-model-operations-draft.md)第 43 行说旧项目／Issue 继续固定原 profile／secret 版本，第 148 行说应用不改变既有 Issue 配置；[模型浏览器草案](../../current/model-settings-browser-api-draft.md)第 123 行规定专用应用只改当前项目配置。
- [首批持久化设计](../../current/backend-first-batch-persistence.md)第 20 行定义 ProjectConfigRevision，但第 22、27、70 行的 Issue、创建操作和创建事务没有明确保存 Issue 到该配置修订的引用。创建请求的 expectedCreationContextRevision 见[创建契约](../../current/issue-page-create-api-contract.md)第 67、101 行，它是检测范围／配置／策略变化的不透明修订，不等于可还原的配置外键。

情景是 Issue A 在配置 C1 下建立，尚未交给 Manager；项目模型随后切换到 C2，再恢复 A 的待办。若处理器只读取当前项目配置，将违背旧 Issue 保持 C1 的承诺；如果禁止读取 C2，则缺少明确恢复 C1 的责任和身份。即使 ProjectConfigRevision 的历史仍在，没有绑定时点与关联也不能可靠选择 A 应用哪版。

这不阻塞首批“只保存管理事实且消费者未接入”的切片。**它阻塞运行接入前的配置语义收口。** 应明确绑定单位与时点，是 Issue 创建、某个工作请求、计划还是运行尝试；明确旧 Key 失效后的配置修复怎样作用于既有工作。然后在对应事务建立可追溯关联，给共享 Manager／不同 Issue 的运行配置传递定义实现约束。报告不擅自选择绑定时点；系统专项将继续交叉复核。

### AB-03 · P2 · 认证返回目标没有覆盖已有会话及消息恢复路由

- [认证浏览器草案](../../current/authentication-browser-api-draft.md)第 33 行将 Destination 限定为 home、project、issue、operation；operationKind 仅列项目创建／更新、Issue 创建、provider 保存、模型测试／应用。
- [恢复总表](../../current/first-batch-recovery-design.md)第 59—65 行新增首批已有会话只读页头；[消息契约](../../current/conversation-message-clarification-api-contract.md)第 149—153 行已经定义消息提交恢复路由及同主体登录恢复。

用户在已有会话页因 401 进入登录，或后续在消息提交恢复页重登，当前 Destination 联合类型无法表达原会话／消息操作。客户端只能降级回项目／首页，或者私自扩枚举、增加未经约定的地址旁路。恢复索引可以保留操作，但没有闭合“登录后回到原目标”的机器契约。

这是导航恢复的局部缺口，**不阻塞首批项目／Issue 管理数据层**。可以明确首批会话只读页回项目的降级行为；若承诺原页恢复，则为 conversation 增加受控目标，并在消息发送批次补齐 conversation_message 操作作用域所需的 conversationId。仍应由固定站内路由表生成地址并重新核权。

## 核查后未列为冲突的内容

| 对照项 | 结论 |
| --- | --- |
| 一会话多 Issue 与一 Issue 一个主会话 | accepted 的 RM-B01-04 r2 已明确首批反向基数和不可改绑；旧“继续设计”已由新章节定点覆盖。 |
| 页面 Issue 创建事务与后端记录 | Issue、主 ChangeSet、必要 Conversation、来源卡片、内容范围、规范化输入、幂等回执、事件及待办一致；来源卡片不冒充聊天。 |
| 三类原操作 | 作用域隔离、同键比较、重放优先于新修订检查、主库核查、404 仍未知和 410 不复活均一致。 |
| 元数据／配置修复与旧受限仓 | 更新只增仓，服务器保留原完整范围；只改资料／配置不强迫旧仓恢复读权，与候选列表隐藏受限身份相容。 |
| 模型专用应用与完整配置 PATCH | 新草案专门复制旧 execution 的引用和有效版本；已有 PATCH 明确重解析两引用。两者是不同动作，不是相同操作的互相矛盾定义。 |
| 消息答复并发 | 一个问题一个 Answer；不同键双答一胜一 409 且整笔不保存；同键重放返回原成功，与问题后来 resolved／superseded 分开。 |
| 消息目标 | root 直接解释与 answer 分支有互斥准入；导航、Markdown 和候选填入均不授予权威目标或执行许可。 |
| 持久队列与执行安全 | 领取代次限制本地写回；外发未知必须核查，不以租约到期推断外部停止。未接处理器可 blocked/INTEGRATION_NOT_AVAILABLE，不伪写 preparing／ready。 |
| SSE 和列表 | Issue SSE 只使 issue／rooms 快照失效；项目和会话列表采用查询刷新。事件每 Issue 串行分配提交序号，避免全局序列跨提交漏读。 |
| 模型测试费用 | 当前待审稿统一有限请求次数预算，费用未知与本地 consumed 分开；原金额示例已撤换，不再认定是双账本冲突。 |
| 登录／退出晚到 | 最新认证 r2 明确不发送可能清掉新会话的迟到删除 Cookie；旧接入 r1 的清 Cookie 描述应按页首后续字段来源解读，不作为新冲突。 |

## 结合 AgentTeams 调研的后端判断

本次新增完整读取 `docs/agentteams-survey-2026-09-07` 的三个文件。两个 Markdown 按行读完，HTML 去掉 style/script 和标签后完整读取正文，没有把 HTML 可视化当真实接口验证。

调研基线是 `eeaab64391ccaec9118e84977f538aefd40720d6`，当时仅静态调查。当前来源记录锁定 `517caff9280242a00a4d4c06365352b9e41659c6`；本次额外完整读取 [Controller 差异报告](../../../validation/agentteams-2026-09-10/reports/controller-delta.md)以及 [reply/thread 实测](../../../validation/agentteams-2026-09-09/reports/runtime-reply-thread-live.md)，区分旧观察与后续证据。没有在本次重跑这些实验，也没有核验整个 upstream 工作树。

| 后端依赖 | 09-07 调研依据 | 后续证据与本次结论 |
| --- | --- | --- |
| Project／Task 查询 | API／CLI 调研第 120—140 行列 workflow、artifact、spawns、history，身份含 team/project。 | 517caff 新增单 Task inspection、Mermaid 和 history 读取，差异报告第 15—20 行。不能沿用“无 Task 详情”的旧结论；也不能把 trace 的 project/task 提示当 instance/team 完整归属或审计。 |
| Manager 配置更新 | API／CLI 调研第 91—101 行记录旧 DTO／PUT 的局限。 | 差异报告第 87 行明确 modelProvider 已支持显式空字符串清空，缺省和 null 仍不修改；不能把旧“所有字符串不能清空”当现版本事实。当前后端专项 §7.8 已区分管理保存与真实运行生效。 |
| 实际派工 | API／CLI 调研第 301—321 行确认 taskflow 等为运行时本地 stdio MCP，非 Controller REST。 | 当前 Manager 创建机制第 37—45 行仍未编制完整受控 MCP Schema；后端消息稿第 63 行也默认未接凭据时不可用。不能凭 HTTP 客户端完成宣称 Manager 能安全派工。 |
| Worker 权限／取消／资源 | 旧调研第 161—179、400—402 行区分角色、phase、Ready，未承诺副作用停止。 | 差异报告第 5、26、33—35 行确认相关 TeamHarness／认证／生命周期和取消写路径未改；新增读取不是写旁路、停止或资源限制修复。当前待办纪律正确保留这些运行前置。 |
| 多 Issue 消息归属 | 旧调研第 358—371 行说明 room→session 及内存缓冲。 | reply/thread 实测第 43—56、68—70 行确认两种入站可触发模型，但输出不保留父事件／thread 且共享 room session。现行消息控制要求可信 request/source/question/answer 绑定，不能直接依赖 Matrix thread 实现。实际适配仍缺，属第二批硬依赖。 |
| 本地业务与上游事实 | 旧调研第 460—487 行说明上游不提供 RepoMesh 的配额、Git 权限、Attempt／工作副本和交付模型。 | 当前后端以 PostgreSQL 保存业务和待办，AgentTeams 只作运行／观察依赖，方向相容。没有证据可将完整系统状态直接投影成上游 completed 或 ready。 |

旧调研普通版第 113—126 行的“每 Issue 独立会话”、第 416 行的“明确单仓直接 Leader”，包括 HTML 的同义内容，是旧产品方案，不是当前规则。现行交接第 106—108、138—140 行已明确所有需求经 Manager、Conversation 与 Issue 分离及被替代范围。本报告没有把历史差异重复报成现行后端冲突；实现时应通过现行导航进入对应新关系。

## 分层开工结论

| 交付边界 | 本次判断 | 最低条件 |
| --- | --- | --- |
| 核心管理数据层 | 可以按现有 accepted 契约分块开发。 | 实现项目／Issue 唯一关系、短事务、固定项目配置、原键查询、内容范围核权及持久事件／待办；真实权限接入可在对应模块收口后集成，不能用模拟权限宣称验收。 |
| 首批六包真实管理闭环 | 尚未达到无缺口整体开工基线。 | 明确采用认证／模型／来源／恢复候选；解决 AB-01；准备真实 App、秘密基础、执行／预算来源和 owner 绑定。已有已采用切片无需等待整个产品设计。 |
| 真实会话和 Manager 工作闭环 | 尚不具备完整设计。 | 首条会话消息、可信运行身份、外部投递／接收与结果绑定、AB-02 配置归属、受控 MCP／停止恢复必须落定。五个消息端点只覆盖已有会话的局部。 |
| 完整多仓执行产品 | 不能宣布达到完整开发标准。 | B06/B07 的实例／房间拓扑、真实写路径控制、Attempt／资源、计划应用、结果采纳和恢复另行闭合；上游已有局部读取／实测不代替这些产品责任。 |

“可以开始切片开发”不表示本次审查授权直接编码。本轮未运行 Go/npm、数据库故障注入、OAuth、真实 Key 保存／模型测试或 AgentTeams 服务。

## 逐文件覆盖清单

| 文件 | 覆盖情况 |
| --- | --- |
| `AGENTS.md`、根 `README.md` | 完整读取，遵守当前授权和证据边界。 |
| `docs/current/HANDOFF.md`、`docs/current/README.md` | 完整读取，截断处另行补读，核对阶段替代。 |
| `docs/current/issue-page-create-api-contract.md` | 全文 334 行。 |
| `docs/current/first-batch-browser-api-contract.md` | 全文 293 行。 |
| `docs/current/conversation-message-clarification-api-contract.md` | 全文 159 行。 |
| `docs/current/authentication-browser-api-draft.md` | 全文 108 行。 |
| `docs/current/model-settings-browser-api-draft.md` | 全文 139 行。 |
| `docs/current/backend-first-batch-persistence.md` | 全文 218 行。 |
| `docs/current/backend-message-clarification-design.md` | 全文 258 行，截断的 50—138 行独立补读。 |
| `docs/current/backend-first-development-access-draft.md` | 全文 191 行。 |
| `docs/current/backend-first-batch-sources-draft.md` | 全文 156 行。 |
| `docs/current/backend-model-operations-draft.md` | 全文 173 行。 |
| `docs/current/draft-conversation-backend-design.md` | 全文 358 行。 |
| `docs/current/manager-create-issue-tool-design.md` | 全文 57 行。 |
| `docs/current/HANDOFF-BACKEND-DESIGN.md` | 全文，现行跳转入口。 |
| `docs/current/HANDOFF-BACKEND-DESIGN-2026-09-11.md` | 全文 208 行，旧任务身份仅作记录。 |
| `docs/current/NEXT-BACKEND-SESSION-PROMPT.md` | 全文 255 行，旧通信／执行指令未执行。 |
| `docs/current/first-development-todo.md` | 全文 64 行，核对首批与二批边界。 |
| `docs/current/first-batch-recovery-design.md` | 全文 82 行。 |
| `docs/current/project-configuration-design.md` | 定点检索配置引用与更新规则，未全文读取，页面／系统审查负责补充。 |
| `docs/current/first-batch-complete-review.md` | 定点检索配置与恢复承诺，未全文读取，主审负责总包。 |
| `docs/agentteams-survey-2026-09-07/agentteams-survey.md` | 全文 476 行。 |
| `docs/agentteams-survey-2026-09-07/agentteams-api-cli-survey-2026-09-07.md` | 全文 511 行。 |
| `docs/agentteams-survey-2026-09-07/agentteams-survey.html` | 完整正文提取阅读；未运行页面。 |
| `third_party/agentteams-source.json` | 全文，核对登记提交。 |
| `validation/agentteams-2026-09-10/reports/controller-delta.md` | 全文 89 行，后续版本差异证据。 |
| `validation/agentteams-2026-09-09/reports/runtime-reply-thread-live.md` | 全文 70 行，限定到原两条真实消息实验。 |

本分工没有重新完整读取全部 ADR／原型源码，也没有复制其他审查者的独立结论。AB-01 与主审和页面审查各自发现一致，AB-02 交系统审查继续核运行约束。本报告只写入本次独占文件。
