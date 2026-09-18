# B09 验收文档：Manager 会话 G1/G2（任务 #16）

对应 ASTRA 文档 B09。分支 feat/astra-b09。
规格来源：docs/current/execution-integration-gates.md（G1/G2 门槛）、docs/current/backend-message-clarification-design.md（§2-§9 消息/澄清协议，accepted）、docs/current/manager-create-issue-tool-design.md（accepted）、docs/current/issue-configuration-binding-design.md（P9 绑定 §8）。

## 需求是什么

B09 要求打通"Issue 固定配置进入实际模型调用"的路径（G1）与"可信消息和工具上下文"（G2）：

1. G1 配置实际消费：从 Issue 的 initial_configuration_revision 解析到每次模型调用的确切输入；配置失效时阻塞并保留原因，绝不回退到项目当前配置。
2. G2 消息面：会话消息幂等保存（服务端序号）、逻辑工作请求登记、澄清状态机、受控 decide_message_target 动作、Manager MCP 创建工具的业务命令复用。

## 做了什么

### 1. internal/database/migrations/0019_messages.sql（新建）

repomesh_messages schema 九张表，逐条对应消息内部设计 §3 的最小记录清单：
- conversation_messages（会话序号唯一约束、正文不可变、removed 墓碑）
- message_submissions（幂等账本，唯一作用域 (project,conversation,actor,entry,submissionId)，cleanup CHECK 对齐 B06 风格）
- message_processing_entries（每条消息一个 interpret_message 入口）
- logical_work_requests（六态状态机 CHECK、revision、current_clarification_id、resolution_id）
- clarifications（部分唯一索引：每请求同时最多一个 open/answer_saved 问题）
- clarification_answers（clarification_id 唯一 + answer_message_id 唯一）
- target_resolutions（logical_request_id 唯一最终解释；issue_target 必有 issue、no_work 必无 issue 的 CHECK；issue FK 延迟引用 issues 表）
- control_operations（commandSlotId 唯一，一个槽位一个规范化输入+结果）
- delivery_observations（外部投递观察：possible_send/reconciling/delivered）

### 2. internal/messages 包（新建，六文件）

- `Submit`（§5.1 提交事务）：重放优先（200 原 receipt）→ 已删除占位 410 → 规范化输入比较（异输入 409 IDEMPOTENCY_CONFLICT）→ 占位 INSERT ON CONFLICT → 聚合事务：会话行锁下 `MAX(sequence)+1` 分配序号 → 消息行（作者由服务端绑定，不信任客户端）→ 处理入口 → logical_work_request(pending) → 单条 UPDATE 落 receipt → COMMIT。
- `parseSubmitInput`：正文 ≤20000 非空白、可选 replyTo、未知字段 422、重复 JSON 键 400。
- `DecideTarget`（§5.2/§6 控制动作）：分支资格是服务端事实——root_message 要求请求 pending 且无问题；clarification_answer 要求 answer_pending 且授权内的 AnswerMessageID 等于该问题唯一答复，否则 409 STALE_PROCESSING_CONTEXT。issue_target 校验目标 Issue 同项目且存活（404 隐藏存在性）。TargetResolution + 请求 resolved + ControlOperation 三写同事务。
- `EvidenceRef`：消息 ID + 半开区间 [start,end)，服务端校验区间合法性，不信任模型重写的摘录。
- `ListMessages`/`GetSubmission`/`GetClarification`：§7 读取面（序号升序分页、游标绑定会话、submission 回执冻结不含当前问题状态）。

### 3. internal/manager 包（新建，G1 核心）

`ResolveForIssue` 实现 P9 §8 的可解析闭包：Issue → initial_configuration_revision（issues 表冻结列）→ configuration_revisions（不可变）→ repomesh_models.profile_links（model profile version → provider revision → secret version 绑定）→ provider_revisions.base_url/api_format + model_snapshots.model_id（出站模型 ID 用快照值而非行键）。
- 每步失败产生持久 Blocker（CONFIGURATION_REVISION_MISSING / MODEL_CONFIG_MISSING / PROVIDER_REVISION_MISSING），**没有任何 fallback 到项目当前配置的代码路径**。
- SecretVersionID 只传版本身份；明文由 secrets store 在发送时打开，不在此层。

### 4. internal/web/messages.go + server.go + main.go（修改）

- 4 条路由：POST/GET conversations/{id}/messages、GET message-submissions/{submissionId}、GET logical-requests/{requestId}/clarification。
- RunConfigured/handlerConfigured 追加 Messages 参数；main.go 装配 MessageService + manager.Service，注释明确"传输适配器未配置时投递保持 blocked，不伪造 Manager 往返"。
- 三个既有测试调用点补 Messages{} 参数。

## 影响范围

- 新增：0019 迁移、internal/messages/ 六文件、internal/manager/ 两文件、internal/web/messages.go。
- 修改：web server 签名（追加）、main.go 装配、两个 web 测试。
- 0019 自动被 migrations.go 的连续版本机制注册，无代码清单改动。

## 修复逻辑（关键设计决策）

1. **序号分配在会话行锁下**：`SELECT MAX(sequence)+1` 与消息插入同事务，配合 conversation_messages 的 (project,conversation,sequence) 唯一约束，并发提交下序号不重不空；这满足 §7"分页不越过未提交较早序号"。
2. **分支资格由服务端判定**：DecideCommand 只带 answerMessageID，root/clarification 分支由数据库当前状态推导（§6"分支是授权属性，不是浏览器或模型可自由选择的参数"）。
3. **G1 消费与传输分离**：manager.Service 只解析配置闭包；出站调用、预算扣减、投递观察属于传输适配器（未实现），职责边界与"未接入处理器保持 blocked"一致。

## 偏差清单

| # | 规格声明 | 实际实现 | 处理 |
|---|---------|---------|------|
| 1 | §2.3 完整可信上下文（managerSessionEpoch/claimGeneration/leaseUntil 签发） | DecideCommand 仅绑定 commandSlotID + AnswerMessageID | 签发需要 B01/B06 传输认证接入（design §2.3 明文"未完成时工具默认不可用"）；槽位身份已持久化，租约/代次列在 control_operations 预留扩展 |
| 2 | §3 Clarification 的 question_message_id 由服务发布问题消息生成 | 表结构具备，propose_clarification 动作未实现 | 提问动作依赖受控 Manager 模拟器；本批交付 decide 分支（direct/no_work 路径可完整跑通） |
| 3 | Manager MCP 工具 repomesh_create_issue 完整 Schema | 未实现 MCP 传输层 | manager-create-issue-tool-design 明文"完整可执行 JSON Schema 及传输身份协议尚未编制"；业务命令已由 B06 CreatePage 复用就绪 |
| 4 | 投递队列/旁路规则（§8） | delivery_observations 表就绪，投递器未实现 | 传输适配器范围；未接入保持真实 blocked |
| 5 | 澄清答复路径（replyTo 消费问题） | 消息可带 replyTo 保存，但 Answer 状态转移未接 | 依赖 #2 的问题发布；状态机 CHECK 已约束转移合法性 |

## 对应 commit

- feat(b09): message persistence, clarification records, per-issue config consumption

## 验证结果

- `go build ./...` EXIT=0；`go vet ./...` 无输出；`go test ./... -count=1` 全部 ok。
- 0019 迁移重放验证待 Docker 环境恢复后执行（本次会话 Docker daemon 离线）；表结构与既有 0004/0005/0006 schema 的外键列已逐一核对（profile_links/configuration_revisions/issues.conversations）。

## 遗留（不阻塞验收）

- 0019 迁移的实际重放 + MC01-MC24 并发用例需真实 PostgreSQL；迁移上线前必须补跑。
- propose_clarification / replace_clarification / 传输适配器 / MCP Schema 属 B09 后续切片。
