# api-design.md 写作规格（根代理拥有设计，子代理成稿）

目标文件：/workspace/docs/current/api-design.md。中文正文，英文标识符。Diátaxis 模式：reference（描述事实，不劝说）。段落短句，不用破折号，不用“不仅…而且”，标题用陈述式短语。

## 输入（按需阅读，不要全文复述）

- 数据库方案文本：/tmp/db-plan.txt（由 /workspace/docs/RepoMesh_Go版数据库重构方案.html 导出，44 张表、371 个字段，7 个方向）。字段表每行：字段名、类型、标记、说明。
- 术语：/workspace/CONTEXT.md。
- ADR：/workspace/docs/adr/README.md 与 0001-0021。全部 21 份都必须在正文中以 `ADR-00nn` 形式被引用至少一次（附录 B 的 ADR 对照表可以满足这一条，但正文关键处也要引）。
- 已采用契约（只需要它们的通用约定和路径）：/workspace/docs/current/issue-page-create-api-contract.md、first-batch-browser-api-contract.md、conversation-message-clarification-api-contract.md。
- 现有路由事实（已由调研得出，直接采用）：见下文“附录 C 素材”。

## 校验脚本会检查什么（必须满足）

脚本：/workspace/docs/development/2026-09-15-api-redesign-01/scripts/verify_api_doc.py。

1. 必须有二级标题 `## 附录 A：表与资源映射`，其下一个 Markdown 表，第一列是 `` `table_name` ``（44 张全部出现，不能多不能少），第二列写该表对应的端点，端点用反引号包住完整路径，例如 `` `/api/v1/tasks` ``、`` `/api/v1/tasks/{taskId}/assignments` ``。没有直接端点的表在第二列写“内部表”并说明由谁写入、从哪个端点间接可见。
2. 正文中每个端点写成 `` `GET /api/v1/...` `` 这种“方法 空格 路径”放在同一对反引号里的形式。脚本收集全部端点，取 `/api/v1/` 后第一段作为顶层资源，要求它出现在附录 A 端点列的某个路径里。所以顶层资源（如 `sessions`、`users`、`agents`、`task-assignments`、`plan-steps`、`events` 等）都要在附录 A 的端点列出现。
3. 每张表一个资源小节，标题形如 `### tasks（`tasks`）任务` 或 `### 任务 `tasks``，要求三级或四级标题里含有反引号包住的表名。该小节到下一个 `##` 或 `###` 之前的正文里，必须出现方案中该表的每个字段名（snake_case，反引号包住，`id` 可省略）。做法：每个资源小节放一个字段表，列为 `方案字段 | JSON 字段 | 读写 | 说明`；不对外暴露的字段（`password_hash`、`session_token_hash`、`value_encrypted`）在小节里单独一行写“不暴露：`password_hash`、…”。
4. 全部 21 个 `ADR-0001` … `ADR-0021` 字样必须出现。
5. 文中本地链接必须可解析（相对 docs/current/）。可用链接：`../RepoMesh_Go版数据库重构方案.html`、`../../CONTEXT.md`、`../adr/README.md`、`../adr/0003-plan-change-authorization-and-activation.md` 等 ADR 文件名（见 ls docs/adr）、`issue-page-create-api-contract.md`、`first-batch-browser-api-contract.md`、`conversation-message-clarification-api-contract.md`、`../archive/2026-09-15-api-database-catalog/README.md`、`../development/2026-09-15-api-redesign-01/README.md`（我随后会创建）。

## 文档结构（固定）

```
# RepoMesh API 设计（基于 Go 版数据库重构方案）
状态行：设计；更新日期 2026-09-15；来源；除附录 C 标“已实现”的端点外均未实现。
## 1. 范围与阅读方式
## 2. 通用约定
### 2.1 路径与版本
### 2.2 身份、租户与权限
### 2.3 请求与响应格式
### 2.4 错误
### 2.5 幂等与乐观锁
### 2.6 分页与过滤
### 2.7 异步操作、事件与 SSE
### 2.8 状态枚举的来源
## 3. 账号与身份（4 张表）
## 4. 项目与编制（5 张表）
## 5. 任务主线（4 张表）
## 6. 协作与上下文（5 张表）
## 7. 交付与验证（6 张表）
## 8. Skill 体系（7 张表）
## 9. 运行账本（13 张表）
## 附录 A：表与资源映射
## 附录 B：ADR 对照
## 附录 C：现有端点的去向
## 附录 D：本文提案的枚举值
## 附录 E：方案与 ADR、术语的冲突及待决事项
```

每个资源小节的固定顺序：一句话职责（来自方案）；端点表（列：方法与路径 | 用途 | 幂等 | 权限）；字段表；状态机或关键规则（有则写，引用 ADR）；关键请求或响应示例（只给 1 个，JSON 用 tab 缩进，只对 tasks、task_assignments、change_sets、review_requests、messages、skill_approvals、events 七个资源给示例，其余不给）。

## 通用约定（我已决定，照写）

- 前缀 `/api/v1`。现有 `/api/...` 端点的去向见附录 C。探针 `/healthz`、`/readyz` 不带前缀，保持现状。
- 身份：浏览器用户走 Cookie `__Host-repomesh-session`（对应 `users.session_token_hash`、`session_expires_at`），写操作要求单值 `Origin` 与 `X-CSRF-Token`（沿用已采用契约）。Agent 不直接调用本 HTTP API，Agent 侧请求经 ADR-0011 所述受控 MCP 入口进入后台协调进程，MCP Schema 未冻结（ADR-0006、执行门槛 G2），本文不定义。后台协调进程与 Web 同库，不经 HTTP。
- 租户：`organization_id` 一律由会话解析，请求体不接受、路径不出现。方案要求一人一组织（`users.organization_id`）。
- 格式：`application/json; charset=utf-8`，请求体上限 256 KiB，响应 `Cache-Control: no-store`。JSON 字段用 camelCase，字段表给出与方案 snake_case 列的对应。时间 RFC3339 UTC。ID 为 UUID。
- 错误：`{"error":{"code":"...","message":"...","fieldErrors":[{"field":"...","code":"..."}],"requestId":"..."}}`。状态码表：400 `INVALID_JSON`、`INVALID_IDEMPOTENCY_KEY`；401 `AUTHENTICATION_REQUIRED`；403 `ORIGIN_REJECTED`、`CSRF_REJECTED`、`FORBIDDEN`；404 `NOT_FOUND`（无权与不存在同样返回 404，沿用创建契约 §6）；409 `IDEMPOTENCY_CONFLICT`、`VERSION_CONFLICT`、`INVALID_TRANSITION`、`CURSOR_EXPIRED`；413 `REQUEST_TOO_LARGE`；415 `UNSUPPORTED_MEDIA_TYPE`；422 `VALIDATION_FAILED`；503 `RESULT_UNCONFIRMED`（外部结果未知，ADR-0016 先核查）、`FEATURE_DISABLED`（决策链关闭时，ADR-0021）。
- 幂等：所有创建型 POST 与状态动作 POST 要求 `Idempotency-Key` 头（UUID，恰好一个）。方案中只有 `agents`、`agent_teams`、`tasks`、`change_sets` 四张表有 `idempotency_key` 列；这四类资源的创建按“同键同输入 200 重放、同键异输入 409”执行。其余资源的重放保护在方案中没有落点，列入附录 E 待决，不得声称已保证。`handoffs.branch_validation_key` 唯一，可作自然幂等键。
- 乐观锁：`tasks.version`、`change_sets.version`。PATCH 与状态动作请求体带 `expectedVersion`，不符返回 409 `VERSION_CONFLICT`。沿用现有 `expectedProjectRevision` 的“请求体携带期望版本”做法。
- 分页：`cursor` 与 `limit`（默认 50，最大 100），响应 `items` 与 `nextCursor`。过滤参数逐资源列出，未列出的 query 键返回 422。
- 异步与事件（ADR-0016、ADR-0010、ADR-0013）：任何会触发外部动作的写操作，在同一本地事务写业务行和 `events` 行（`channel=outbox`），提交后返回 201 与资源快照；外部动作在提交后由后台协调进程执行；结果通过 GET 资源与 SSE 观察。创建成功只证明已登记，不证明执行、就绪或投递（ADR-0018、ADR-0019）。
- SSE：唯一端点 `GET /api/v1/events/stream`，query 至少一个过滤：`aggregateType`+`aggregateId`、`taskId`、`projectId`、`correlationId`。数据源是 `events` 表 `channel=state`。事件 `id` 为 `events.id`，`event` 名为 `event_type`，data 是 `{aggregateType, aggregateId, aggregateVersion, occurredAt}`，不带完整载荷，客户端收到后重新 GET 资源（沿用创建契约 §8 的“失效通知 + REST 快照”）。连接建立先发 `resync_required`。支持 `Last-Event-ID`，事件默认保留 24 小时，注释心跳 15 秒。不承诺补发（ADR-0010）。
- 状态枚举：方案把多数状态列写成“字符串”，没有给值。本文需要的枚举值全部集中在附录 D 并标为“提案”，正文引用时注明“见附录 D”。不把提案写成已采用。

## 各方向的资源设计（我已决定，照写；发现方案字段不够支撑 ADR 要求时写进附录 E，不要自行加字段）

### ③ 任务主线（最先写，其他方向围绕它）

- `tasks`：`GET /api/v1/projects/{projectId}/tasks`（过滤 `status`、`parentTaskId`、`repositoryId`、`assigneeAgentId`）、`POST /api/v1/projects/{projectId}/tasks`、`GET /api/v1/tasks/{taskId}`、`PATCH /api/v1/tasks/{taskId}`（title、instruction、acceptance、sourceRef、repositoryId，需 expectedVersion）、`POST /api/v1/tasks/{taskId}/subtasks`、`POST /api/v1/tasks/{taskId}/reserve`（assigneeAgentId；短事务同时写 `assignee_agent_id`、`reserved_at`、status，ADR-0017、ADR-0001 一个 Worker 同时只能有一个活跃任务）、`POST /api/v1/tasks/{taskId}/release`、`POST /api/v1/tasks/{taskId}/pause`（ADR-0001、ADR-0003、CONTEXT“暂停”：只停新派工，不代表在途已停）、`POST /api/v1/tasks/{taskId}/resume`、`POST /api/v1/tasks/{taskId}/cancel`、`POST /api/v1/tasks/{taskId}/complete`（写 result_summary；子任务完成不自动完成父任务，ADR-0001）。
  - 术语：CONTEXT 的 Issue 对应 `parent_task_id` 为空的顶层 task，`source_ref` 保存外部来源；CONTEXT 的 Task 对应子 task。方案没有独立 issues 表，附录 E 记录这与 ADR-0019“Issue 独立列表/详情”的关系。
  - 状态机（附录 D 提案）：`open`、`planned`、`reserved`、`in_progress`、`submitted`、`handed_off`、`done`、`blocked`、`paused`、`cancelled`。给出允许迁移表。
- `plan_steps`：`GET /api/v1/plans/{planId}/steps`、`POST /api/v1/plans/{planId}/steps`、`GET /api/v1/plan-steps/{stepId}`、`PATCH /api/v1/plan-steps/{stepId}`（status、output、dependsOn）。ADR-0003 §3.1：首期不提供直接编辑图节点连线的页面 API，这里的 PATCH 只供后台协调进程与 Manager 工具，浏览器只读。
- `task_assignments`：`GET /api/v1/tasks/{taskId}/assignments`、`POST /api/v1/tasks/{taskId}/assignments`（派单，生成 `generation` = 上一轮 + 1，`previous_attempt_id` 自动指向上一轮；ADR-0017 与 reserve 同事务）、`GET /api/v1/task-assignments/{assignmentId}`、`POST /api/v1/task-assignments/{assignmentId}/result`（attemptState、attemptReason、executionId、finishedAt；迟到结果不覆盖当前有效结果，ADR-0001、ADR-0007 D7、ADR-0015）。CONTEXT 的 Attempt 对应一行 assignment（一轮尝试一行）。
- `handoffs`：`GET /api/v1/tasks/{taskId}/handoffs`、`POST /api/v1/tasks/{taskId}/handoffs`（branchValidationKey 唯一即幂等）、`GET /api/v1/handoffs/{handoffId}`、`POST /api/v1/handoffs/{handoffId}/status`（status、evidence 追加）。

### ② 项目与编制

- `projects`：`GET /api/v1/projects`、`POST /api/v1/projects`（name、repositoryId）、`GET /api/v1/projects/{projectId}`、`PATCH /api/v1/projects/{projectId}`（name、status）。方案 `repository_id` 必填唯一，即一项目一仓库；与 CONTEXT“多仓库项目”、ADR-0002 J2、现有 `project_repositories` 冲突，写入附录 E，本文按方案。项目创建不触发实例准备（ADR-0018）。
- `repositories`：`GET /api/v1/repositories`（过滤 `q`）、`POST /api/v1/repositories`（name、url、description、topics、languages）、`GET /api/v1/repositories/{repositoryId}`、`PATCH /api/v1/repositories/{repositoryId}`、`POST /api/v1/repositories/{repositoryId}/profile`（触发画像，异步，完成后写 `profiled_at`、`topics`、`languages`，ADR-0020 的分析不触发实例准备）。`poll_last_at`、`poll_next_at`、`poll_failures` 只读，由后台轮询写。
- `plans`：`GET /api/v1/projects/{projectId}/plans`、`POST /api/v1/projects/{projectId}/plans`（requirementText、engineeringSpec、contracts、taskDag、executionBatches、createdByAgentId；每次内容变化新建一行并递增 `plan_version`，已存在版本不原地改写，ADR-0003 §1.1）、`GET /api/v1/plans/{planId}`、`POST /api/v1/plans/{planId}/revisions`（追加 `revisions` JSON 一条修订记录，不改已发布内容）。ADR-0003 要求提出、获准、应用、生效四个事实分开：获准经 `review_requests`（objectType=plan），生效事实在方案中没有列（plans 无 status，tasks 无当前生效计划指针），写入附录 E。ADR-0015 限额内推进下一轮不新建 plan 行。
- `agent_teams`：`GET /api/v1/projects/{projectId}/agent-teams`、`POST /api/v1/projects/{projectId}/agent-teams`、`GET /api/v1/agent-teams/{teamId}`、`PATCH /api/v1/agent-teams/{teamId}`（workerAgentIds、executionMode、requiredCheckpoints、humanGrants；改动需项目管理员，ADR-0005 §1）、`POST /api/v1/agent-teams/{teamId}/runtime`（action=start|stop，写 `runtime_status`；ADR-0018 首条消息或建项提交后异步准备；stop 不代表在途已停，ADR-0013）。`human_grants` 不扩大 Git 权限（ADR-0002、ADR-0003 §1.2）。
- `review_requests`：`GET /api/v1/review-requests`（过滤 `status`、`objectType`、`projectId`）、`GET /api/v1/review-requests/{requestId}`、`POST /api/v1/review-requests`（由后台协调进程代 Agent 发起；objectType、objectId、requestContent）、`POST /api/v1/review-requests/{requestId}/decision`（decision=approve|reject、note；`decided_by` 取会话用户，服务端强制，ADR-0003 §1.6；批准不等于生效，ADR-0003、ADR-0006 §3）。方案 object_type 列举“任务/编制/Skill 放行”，本文增加 `plan`，附录 E 记录。ADR-0004：缺失规则解释也走此表，objectType=`interpretation`（提案）。人工门禁与环境阻塞、资源等待分开表达（ADR-0006 §2、CONTEXT）。

### ① 账号与身份

- `organizations`：`GET /api/v1/organizations/current`、`PATCH /api/v1/organizations/current`（name，组织管理员）。
- `users` 与会话：`POST /api/v1/sessions`（username、password；成功 Set-Cookie，写 `session_token_hash`、`session_expires_at`）、`GET /api/v1/sessions/current`（user、csrfToken）、`DELETE /api/v1/sessions/current`；`GET /api/v1/users`、`POST /api/v1/users`（组织管理员）、`GET /api/v1/users/{userId}`、`PATCH /api/v1/users/{userId}`（displayName、orgRole、active）、`POST /api/v1/users/{userId}/password`。不暴露 `password_hash`、`session_token_hash`。方案是本地账号密码登录；现有 B02 GitHub 登录、连接、发现表在方案中没有落点，附录 E。
- `agents`：`GET /api/v1/agents`（过滤 `role`、`repositoryId`、`status`）、`POST /api/v1/agents`（幂等键写 `idempotency_key`；`singleton_key` 防重复）、`GET /api/v1/agents/{agentId}`、`PATCH /api/v1/agents/{agentId}`（status、responsibilityPaths、resourceRef、parentAgentId）。`role` 取方案值 `leader`、`manager`、`worker`、`db-test-team`。方案里 leader=总、manager=仓库，CONTEXT 与 ADR-0006 相反（Manager 是项目统一入口）。本文按方案值写，附录 E 第一条记录该倒置，要求用户裁定后同步 CONTEXT 或方案。
- `credentials`：`GET /api/v1/credentials`（不返回 `value_encrypted`，只返回 kind、key、appId、installationId、ownerLogin、updatedBy、updatedAt）、`PUT /api/v1/credentials/{key}`（kind、value 明文入参，服务端加密后写 `value_encrypted`）、`DELETE /api/v1/credentials/{key}`。GitHub App 安装信息对应 kind=`github-app`（ADR-0002）。方案没有密钥版本与轮换表（现有 `repomesh_secrets`），附录 E。

### ④ 协作与上下文

- `messages`：`GET /api/v1/messages`（必填过滤之一：`roomId`、`taskId`、`projectId`；`correlationId` 可选）、`POST /api/v1/messages`（人发消息：projectId、roomId、taskId?、recipientAgentId?、kind、subject、body；`sender_type=human`，`sender_user_id` 取会话；保存成功不等于 Manager 已接收，ADR-0018）、`GET /api/v1/messages/{messageId}`。实时更新走 `events/stream` 过滤 `aggregateType=message`。方案没有会话（conversation）实体，只有 `room_id` 字符串；ADR-0019 要求会话与 Issue 分资源，附录 E。
- `context_objects`：`GET /api/v1/projects/{projectId}/context-objects`、`POST /api/v1/projects/{projectId}/context-objects`、`GET /api/v1/context-objects/{objectId}`、`PUT /api/v1/context-objects/{objectId}/content`（新内容，`version_no` 加一，旧内容追加到 `version_history`）、`GET /api/v1/context-objects/{objectId}/versions`。
- `context_bundles`：`GET /api/v1/task-assignments/{assignmentId}/context-bundles`、`POST /api/v1/task-assignments/{assignmentId}/context-bundles`（items 含材料 id 与版本快照）、`GET /api/v1/context-bundles/{bundleId}`。
- `context_deltas`：`GET /api/v1/context-objects/{objectId}/deltas`、`POST /api/v1/context-objects/{objectId}/deltas`（baseVersion、changes）、`POST /api/v1/context-deltas/{deltaId}/sync`（agentIds 追加到 `synced_to`）。
- `context_access_events`：`GET /api/v1/context-objects/{objectId}/access-events`。只读；由后台协调进程在 Agent 读取时写入。附录 A 标“内部写入，只读端点”。

### ⑤ 交付与验证

- `change_sets`：`GET /api/v1/tasks/{taskId}/change-sets`、`POST /api/v1/tasks/{taskId}/change-sets`（ADR-0008：正式提交顶层 task 时建立允许暂空的主 ChangeSet；重放复用）、`GET /api/v1/change-sets/{changeSetId}`、`PATCH /api/v1/change-sets/{changeSetId}`（repositoryIds 条目、branch、payload；expectedVersion）、`POST /api/v1/change-sets/{changeSetId}/deliver`（生成 `scm_commands`：建分支、开 draft PR；只允许 draft PR，合并由人，ADR-0002、ADR-0006）、`POST /api/v1/change-sets/{changeSetId}/conflict`（conflictKind、conflictStatus、conflictDetail）、`POST /api/v1/change-sets/{changeSetId}/archive`（status→archived 终态）。ADR-0008 的 Candidate、Delivery Combination 在方案中只剩 `repository_ids` JSON 每仓一份 {仓库、提交、PR 号、交付状态}，收录、接受、入选、验证通过、可开 PR 五个事实无独立列，附录 E。
- `scm_commands`：`GET /api/v1/change-sets/{changeSetId}/scm-commands`、`GET /api/v1/scm-commands/{commandId}`、`POST /api/v1/scm-commands/{commandId}/retry`（未知结果先核查再重试，ADR-0016）。
- `scm_observations`：`GET /api/v1/change-sets/{changeSetId}/scm-observations`。只读；`consumed` 由后台写。
- `delivery_policies`：`GET /api/v1/projects/{projectId}/delivery-policy`、`PUT /api/v1/projects/{projectId}/delivery-policy`（一项目一条）。
- `validation_snapshots`：`GET /api/v1/projects/{projectId}/validation-snapshots`（过滤 `objectId`、`status`）、`GET /api/v1/validation-snapshots/{snapshotId}`。写入方是后台验证流程；ADR-0005 初判与复核分开记录，ADR-0015 验证通过与任务结束分别判断。
- `database_branch_validations`：`GET /api/v1/projects/{projectId}/database-branch-validations`（过滤 `repositoryId`、`candidateSha`、`status`）、`POST /api/v1/projects/{projectId}/database-branch-validations`（repositoryId、taskId?、candidateSha、sourceDatabaseRef、provider）、`GET /api/v1/database-branch-validations/{validationId}`、`POST /api/v1/database-branch-validations/{validationId}/cleanup`（置 `cleanup_pending`，实际清理由后台执行，停止请求与实际停止分开，ADR-0013）。

### ⑥ Skill 体系

先写一段状态说明：ADR-0009 把 Skill 工程整体暂缓并要求恢复前不冻结表与 API；2026-09-15 采用的数据库方案新增 7 张 Skill 表，本文据此给出接口设计。ADR-0009 的替代关系尚未写入 ADR，附录 E 记录；在 ADR 更新前本节端点不得实施。

- `skills`：`GET /api/v1/skills`（过滤 `targetAgentRole`）、`POST /api/v1/skills`、`GET /api/v1/skills/{skillId}`、`PATCH /api/v1/skills/{skillId}`（scenario、targetAgentRole）。
- `skill_versions`：`GET /api/v1/skills/{skillId}/versions`、`POST /api/v1/skills/{skillId}/versions`（version、content；服务端算 `content_hash`，status=draft）、`GET /api/v1/skill-versions/{versionId}`、`POST /api/v1/skill-versions/{versionId}/status`（action=start_evaluation|deprecate；发布只能由 `skill_approvals` 结论触发）。状态值取方案：draft、evaluating、published、deprecated。
- `skill_test_questions`：`GET /api/v1/skills/{skillId}/test-questions`、`POST /api/v1/skills/{skillId}/test-questions`（kind=success|business_failure|missing_precondition、question、expected；providedBy）。
- `skill_evaluation_runs`：`GET /api/v1/skill-versions/{versionId}/evaluation-runs`、`POST /api/v1/skill-versions/{versionId}/evaluation-runs`（questionIds；服务端为每题跑 with 与 without 两臂并生成 `blinded_label`）、`POST /api/v1/skill-evaluation-runs/{runId}/judgement`（result、judgedBy 取会话或 Agent）。
- `skill_approvals`：`GET /api/v1/skill-versions/{versionId}/approval`、`POST /api/v1/skill-versions/{versionId}/approval`（subjectRole；服务端按“Worker 的 Manager 审、Manager 的 Leader 审、Leader 的人工审”推导 `reviewer_kind`，审自己的产出时 `recused=true` 并上移）、`POST /api/v1/skill-approvals/{approvalId}/decision`（reviewStatus、note、conclusion；通过后由服务端把版本置 published，异步不阻塞任务）。
- `agent_skill_bindings`：`GET /api/v1/agents/{agentId}/skill-bindings`、`POST /api/v1/agents/{agentId}/skill-bindings`（versionId、source）、`DELETE /api/v1/agent-skill-bindings/{bindingId}`（置 `active=false`，不删行）。绑 Agent 不绑 Issue。
- `skill_update_suggestions`：`GET /api/v1/skills/{skillId}/update-suggestions`（过滤 `status`）、`POST /api/v1/skills/{skillId}/update-suggestions`（taskId、suggestion）、`POST /api/v1/skill-update-suggestions/{suggestionId}/decision`（status=accepted|rejected）。

### ⑦ 运行账本

- `events`：`GET /api/v1/events`（过滤 `channel`、`eventType`、`aggregateType`+`aggregateId`、`correlationId`、`taskId`、`from`、`to`）、`GET /api/v1/events/{eventId}`、`GET /api/v1/events/stream`（SSE，见 2.7）。`published_at`、`attempts`、`last_error` 只在 channel=outbox 有值。写入方只有服务端。
- `llm_usage`：`GET /api/v1/llm-usage`（过滤 `taskId`、`agentId`、`provider`、`model`、`from`、`to`）、`GET /api/v1/llm-usage/summary`（按 provider、model、operation 聚合 tokens 与 duration）。
- `alert_rules`：`GET /api/v1/alert-rules`、`POST /api/v1/alert-rules`、`GET /api/v1/alert-rules/{ruleId}`、`PATCH /api/v1/alert-rules/{ruleId}`、`DELETE /api/v1/alert-rules/{ruleId}`。
- `alert_events`：`GET /api/v1/alert-events`（过滤 `status`、`ruleId`）、`GET /api/v1/alert-events/{eventId}`、`POST /api/v1/alert-events/{eventId}/claim`、`POST /api/v1/alert-events/{eventId}/resolve`（note；`handled_by` 取会话用户）。状态取方案：fired、claimed、handling、closed。
- `trace_sessions` 与 `trace_events`：`GET /api/v1/trace-sessions`、`GET /api/v1/trace-sessions/{sessionId}`、`GET /api/v1/trace-sessions/{sessionId}/events`。只读。
- `log_entries`：`GET /api/v1/log-entries`（过滤 `level`、`source`、`from`、`to`）。只读。
- `recovery_cases`、`recovery_decisions`、`recovery_operations`：`GET /api/v1/recovery-cases`（过滤 `status`、`severity`、`objectType`）、`POST /api/v1/recovery-cases`、`GET /api/v1/recovery-cases/{caseId}`、`GET /api/v1/recovery-cases/{caseId}/decisions`、`POST /api/v1/recovery-cases/{caseId}/decisions`（decision；`decided_by` 取会话或后台）、`GET /api/v1/recovery-cases/{caseId}/operations`（只读，后台执行后写入）、`POST /api/v1/recovery-cases/{caseId}/close`。ADR-0014 与 ADR-0016：上游不可用或观察陈旧先核查再决定，未知状态不得释放资源（执行门槛 G4、ADR-0017）。
- `decision_chain_nodes` 与 `decision_embeddings`：`GET /api/v1/decision-chains`（过滤 `projectId`、`actorType`、`parentNodeId`）、`GET /api/v1/decision-chains/{nodeId}`、`GET /api/v1/decision-chains/semantic-search`（queryText、model、topK、minSimilarity；按 `model` 过滤，ADR-0021）、`POST /api/v1/decision-chains/embeddings/refresh`。写路径不调用 LLM（ADR-0021）。功能开关：现有 `PUT /api/settings/decision-chain` 依赖 `feature_settings` 表，该表不在方案 44 张内，附录 E；本文把开关端点列为 `GET /api/v1/settings/decision-chain`、`PUT /api/v1/settings/decision-chain`，落点待定。附录 A 的端点列因此要出现 `/api/v1/settings/decision-chain`（可放在 decision_chain_nodes 行）。
- `mcp_server_policies`：`GET /api/v1/mcp-server-policies`、`GET /api/v1/mcp-server-policies/{serverName}`、`PUT /api/v1/mcp-server-policies/{serverName}`。ADR-0011：策略约束 MCP 出站，不代替凭据与存储约束。

## 附录 B：ADR 对照

表格：ADR | 本文如何落实 | 方案中缺失的落点（若有）。21 行全写。要点已在上面各节分散给出，汇总即可。ADR-0007 的循环上限（修复 3 轮、诊断 2 轮、环境恢复 2 次、连续两轮同一失败结束）在 `task_assignments.generation` 与 `plans.execution_batches` 中体现，超限由后台协调进程拒绝新派单并返回 409 `INVALID_TRANSITION`。ADR-0012：上游 Project 引用放在 `agent_teams.resource_ref` 与 `task_assignments.dispatch_ref` JSON，须含实例、team、project_id。ADR-0014、ADR-0015：Graph 结果只读观察，经 `plans.task_dag`、`plan_steps.depends_on`、`events`；不提供换图写端点，换图协议未选（执行门槛 G5）。ADR-0020：`POST /api/v1/repositories/{repositoryId}/profile` 是可选分析，不扩仓、不建计划、不触发实例。

## 附录 C：现有端点的去向（素材，直接整理成表：现有端点 | 状态 | 去向）

已实现（代码有路由）：
- `GET /healthz`、`GET /readyz`：保留，不改。
- 认证 7 条：`GET /api/session`、`POST /api/auth/github/login`、`POST /api/auth/github/reconnect`、`GET /api/auth/github/callback`、`GET /api/auth/attempts/{id}`、`POST /api/auth/logout`、`GET /api/repositories`（GitHub 发现，需会话）。去向：方案改为本地账号登录（`POST /api/v1/sessions`）；GitHub 连接与仓库发现在方案中没有表，去向待用户裁定（附录 E）。
- 项目 8 条：`POST /api/projects`、`GET /api/project-creations/{id}`、`PATCH /api/projects/{id}`、`GET /api/projects/{id}/updates/{id}`、`GET /api/projects/{id}`、`GET /api/projects`、`GET /api/projects/{id}/repositories`、`GET /api/configuration-profiles`。去向：前六条由 `/api/v1/projects` 系列替代；原操作回执端点（project-creations、updates）在方案中无 `creation_operations`、`update_operations` 落点，重放保护待决；`configuration-profiles` 与模型／执行配置在方案中无表，待决。
- 模型 6 条：`GET /api/model-providers`、`GET /api/model-providers/{id}`、`GET /api/model-providers/{id}/versions/{revision}`、`POST /api/model-provider-saves`、`GET /api/model-provider-saves/{saveId}`、`POST /api/model-provider-saves/{saveId}/close`。去向：方案没有模型供应商与密钥版本表（现有 `repomesh_models.*`、`repomesh_sources.*`、`repomesh_secrets.*` 共 20 张），`llm_usage.provider/model` 只是字符串。待决。
- 扫描 10 条：`GET /api/repositories`（目录，无认证，与认证发现同路径）、`GET /api/repositories/url-type`、`POST /api/repositories`、`POST /api/scan-jobs`、`GET /api/scan-jobs/{id}`、`POST /api/scope/suggestions`、`POST /api/scope/check`、`POST /api/scope`、`GET /api/settings/scope-assist`、`PUT /api/settings/scope-assist`。去向：仓库档案并入 `/api/v1/repositories`；`scan-jobs` 与 `scope` 三条对应 ADR-0020 的建项前分析，方案无作业表，待决；`scope-assist` 开关依赖 `feature_settings`，待决。
- 决策链 7 条：`GET /api/decision-chains`、`GET /api/decision-chains/{id}`、`GET /api/decision-chains/similar`、`GET /api/decision-chains/semantic-search`、`POST /api/decision-chains/embeddings/refresh`、`GET /api/settings/decision-chain`、`PUT /api/settings/decision-chain`。去向：加 `/v1` 前缀保留；`similar` 合并进 `semantic-search`（provisional）；开关落点待决。

已采用未实现（契约有，代码无）：
- `GET /api/projects/{id}/issues`、`GET /api/projects/{id}/conversations`、`GET /api/projects/{id}/issue-creation-options`、`GET /api/projects/{id}/issue-creation-conversations`、`POST /api/projects/{id}/issues`、`GET /api/projects/{id}/issue-creations/{creationId}`、`GET /api/issues/{issueId}`、`GET /api/issues/{issueId}/rooms`、`GET /api/issues/{issueId}/events`（SSE）。去向：`issues` 由顶层 `tasks` 承接；`conversations` 无落点；Issue SSE 由 `events/stream` 承接。
- 消息 5 条：`POST/GET .../conversations/{conversationId}/messages`、`GET .../message-submissions/{id}`、`GET .../messages/{messageId}`、`GET .../clarifications/{clarificationId}`。去向：`/api/v1/messages`；澄清（clarification）无落点。

## 附录 E：冲突与待决（编号，按影响排序，每条写：现象、出处、本文的处理、需要谁裁定）

1. 角色命名倒置：方案 leader=总、manager=仓库；CONTEXT.md、ADR-0006 相反。
2. 一项目一仓库（`projects.repository_id` 必填唯一）vs CONTEXT 多仓库项目、ADR-0002 J2、现有 `project_repositories`；方案 `agent_teams.repository_id 可空（项目级编制）` 与 `change_sets.repository_ids` JSON 又隐含多仓。
3. 无会话实体：`messages.room_id` 字符串；ADR-0019、已采用消息契约要求 conversation 资源。
4. 无独立 Issue 实体：顶层 task 承接；ADR-0019 的 Issue 列表、详情、房间导航需要在 task 上表达；仓库事项（Repository Issue）无落点。
5. 计划生效指针缺失：ADR-0003 四个事实；`plans` 无 status，`tasks` 无当前生效计划列。
6. Candidate、Delivery Combination、result admission 缺失（ADR-0008、ADR-0015）。
7. Skill 7 张表 vs ADR-0009 暂缓与“不冻结表和 API”；需要 ADR-0009 补充替代关系。
8. 登录方式：方案本地密码；现有 B02 GitHub OAuth 登录、connections、bindings、attempts、discovery 4 表无落点；ADR-0002 的有效权限交集需要安装范围数据。
9. 已实现模型供应商、来源、密钥版本共 20 张表无落点；`credentials.value_encrypted` 的密钥管理未定义。
10. `feature_settings` 不在 44 张内，但 ADR-0021 的开关与现有 scope-assist 开关依赖它。
11. 原操作回执表（creation_operations、update_operations、save_operations）无落点；仅 4 张表有 `idempotency_key`。
12. 状态枚举大多未给值（附录 D 全是提案）。
13. Agent 调用凭证：`agents` 无令牌字段；MCP 入口协议未冻结（ADR-0006、ADR-0011、G2）。
14. 现有 40 张表到 44 张目标表没有迁移映射；方案的“原表”名（local_human_accounts 等）不是本仓库的表名。
15. 建项前分析作业（scan-jobs、scope）无作业表（ADR-0020）。
16. ADR-0021 尚未收录进 ADR 索引；0020 与 0021 关于 Python 路线的关系未写清。

## 写作规则

- 每个事实句带出处（方案字段名、ADR 编号或契约章节）。
- 不写“将支持”“未来”。不写营销句。不用“确保”“旨在”“赋能”。
- 不给未在上面列出的端点。若你认为缺端点，写进附录 E 最后一条“写作中发现”，不要加进正文。
- 表格中的竖线写成 `&#124;`。
- 完成后运行 `python3 docs/development/2026-09-15-api-redesign-01/scripts/verify_api_doc.py`，修到只剩 `docs/development/2026-09-15-api-redesign-01/README.md` 缺失这一类失败（该文件由根代理写）。把最终输出贴回。
