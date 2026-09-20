# RepoMesh API 设计（基于 Go 版数据库重构方案）

状态：设计。更新日期：2026-09-16。2026-09-16 用户裁定五项并结案（附录 E 第 8、9、10、16 条与写作发现 24 的裁定行）：路径前缀定为 `/api`（无版本段，全文已更正）、登录保留 GitHub OAuth、决策链 `similar` 与 `semantic-search` 保持两条独立端点、`public.feature_settings` 补入方案表清单、模型供应商三 schema 保留；技能治理 22 条端点（20 条 `/api/skills` + 2 条开关）与 Issue 创建/读面（B06/B07，7 条）、模型测试与应用（B05/B04，6 条）已实现并登记（附录 C）。来源：[Go 版数据库重构方案](../RepoMesh_Go版数据库重构方案.html)的 44 张目标表、[ADR 索引](../adr/README.md)与 ADR-0001 至 ADR-0023、[B11 重规划协议](../plan/B11-REPLAN-PROTOCOL.md)、[领域语言](../../CONTEXT.md)、已采用的[创建契约](issue-page-create-api-contract.md)、[首批浏览器契约](first-batch-browser-api-contract.md)和[消息契约](conversation-message-clarification-api-contract.md)；制作与校验记录见[本轮记录](../development/2026-09-15-api-redesign-01/README.md)。除附录 C 标“已实现”的端点外，本文端点均未实现。

2026-09-19 已实现范围修正：项目创建 `repositoryIds` 允许 `[]`，操作顺序改为先保存项目、再通过 PATCH 明确接入仓库、最后在项目内创建 Issue。账号仓库目录不是项目工作范围。发现、审批、计划、物化与重规划限定到 Issue 已确认的项目仓库；迁移 0042 增加计划 Issue 关联和任务仓库规范绑定，拒绝新越界写入并隔离未确认的历史任务。创建条件与分页按[Issue 创建契约](issue-page-create-api-contract.md)；数据库兼容和验证见[本轮记录](../development/2026-09-19-project-scope/README.md)。本文较早的目标表设计不替代这一已实施约束。

## TypeSafe / Jev 增量接口（2026-09-20）

本次新增项目范围的 TypeSafe 设置和辅助判断接口，唯一完整契约见 [Spec §4](typesafe-verification-spec.md#4-浏览器及工具契约)。源码为 `internal/web/typesafe.go` 与 `internal/typesafe`；数据位于新增的 `repomesh_typesafe` schema，Key 使用既有 `repomesh_secrets`。此增量不改写下文历史目标表映射。

| 方法与路径 | 用途 | 权限与重放 |
| --- | --- | --- |
| `GET /api/projects/{projectId}/typesafe` | 脱敏配置、统一开关与 Skill 身份 | 登录的项目 owner |
| `POST /api/projects/{projectId}/typesafe` | 保存/替换/清除 Key，控制测试与审查 | owner + Origin/CSRF + expectedRevision |
| `POST /api/projects/{projectId}/typesafe/check` | 保存配置的合成推理检查 | owner + Origin/CSRF + expectedRevision；项目冷却 |
| `GET /api/projects/{projectId}/typesafe/evaluations` | 按 issueId 读取最近最多 100 条辅助判断 | owner；Issue 必须属于当前项目 |
| `POST /api/typesafe/evaluations` | 测试/审查运行的受限工具 | 短期 run Bearer；同 run/requestId 幂等，不接受浏览器会话代替 |

辅助判断按 test/code_review 标记来源，不改写测试 passed、审批或合并许可。公开模型供应商接口仍采用自身的 Chat Completions 契约，不能用它替代 TypeSafe 协议。

## 1. 范围与阅读方式

本文按方案的 7 个功能方向定义 RepoMesh Web 进程的浏览器 HTTP 接口，并维护 44 张表到资源的映射。每张表一个资源小节，顺序固定：一句话职责、端点表、字段表、状态机或关键规则、示例。示例只给 `tasks`、`task_assignments`、`change_sets`、`review_requests`、`messages`、`skill_approvals`、`events` 七个资源。

端点表的列是方法与路径、用途、幂等、权限。幂等列的取值：`天然` 指 GET、PUT、DELETE、PATCH 重复执行结果相同，PATCH 按字段覆盖；`键` 指要求 `Idempotency-Key` 且该表有 `idempotency_key` 列；`键（无落点）` 指要求 `Idempotency-Key` 但方案没有存放位置，重放保护待决（附录 E 第 11 条）；`自然键` 指以业务唯一列判重；`版本` 指请求体带 `expectedVersion`。权限列的取值见 2.2。

字段表的列是方案字段、JSON 字段、读写、说明。第一列是方案的 snake_case 列名。读写列的取值：`读` 表示只出现在响应中，由服务端写；`读写` 表示创建与 PATCH 均可提供；`创建时写` 表示只在创建请求中提供；`动作写` 表示由状态动作端点改变；`不暴露` 表示不进入任何响应。

本文引用 ADR 时写 `ADR-00nn`，引用方案时写表名与列名，引用已采用契约时写章节号。状态枚举的取值集中在附录 D，正文只引用；附录 D 全部是提案。方案与 ADR、术语的冲突在附录 E 编号列出，正文按方案写，不自行增减字段。

阅读顺序：先读第 2 节通用约定，再按方向读资源小节；查某张表对应的端点用附录 A；查某份 ADR 如何落实用附录 B；查现有 `/api/...` 端点的去向用附录 C。旧的 `docs/api-database/` 目录已删除，说明见[归档记录](../archive/2026-09-15-api-database-catalog/README.md)。

## 2. 通用约定

> **已实现端点的契约口径（2026-09-16）**：附录 C 标“已实现”的端点（认证、项目仓库、扫描圈定、决策链、技能治理等，共 61 条）其请求/响应**以代码实现为准**；本节 2.3–2.5 的全局约定（`items/nextCursor` 分页、201 创建、`FEATURE_DISABLED` 等错误码、`fieldErrors` 错误体、幂等键要求）只约束未实现的设计端点。已实现端点的已知出入：列表分页为 `nodes` + `offset`（非 `items`/`nextCursor`）；创建与动作返回 200（非 201）；503 为 `{"detail":...}` 明文（无错误码字段）；auth 错误体为 `{"error":{code,message,details}}`。技能治理 22 条端点（20 条 `/api/skills` + 2 条开关）的字段细节以 `internal/skills` 为准，按板块回填本册。

### 2.1 路径与版本

- 前缀 `/api`，无版本段——2026-09-16 用户裁定：沿用现行已实现端点的无版本前缀，不引入 `/api/v1`；本文写作时的 `/api/v1/...` 路径已全文更正为 `/api/...`。探针 `GET /healthz`、`GET /readyz` 不带 `/api`，保持现状；它们只表示进程存活，不表示业务就绪。现有 `/api/...` 端点的去向见附录 C。
- 资源名用 kebab-case 复数，例如 `task-assignments`、`review-requests`。路径参数用 `{camelCase}`，例如 `{taskId}`。
- 有父资源的列表与创建挂在父资源下，例如 `GET /api/projects/{projectId}/tasks`；`users`、`agents`、`skills`、`review-requests`、`messages`、`repositories`、`alert-rules`、`recovery-cases`、`mcp-server-policies` 等无父资源的挂顶层。读取、修改与状态动作用顶层路径，例如 `GET /api/tasks/{taskId}`。
- 状态动作用 POST，路径为资源路径后接动作名，例如 `POST /api/tasks/{taskId}/pause`；请求体只带该动作需要的字段。没有迁移表的状态列（`projects.status`、`agents.status`、`plan_steps.status`）由 PATCH 修改。
- 附录 A 的端点列包含全部顶层路径段；除探针外，正文出现的每个端点，其顶层路径段都能在附录 A 找到。

### 2.2 身份、租户与权限

浏览器用户走 Cookie `__Host-repomesh-session`，对应 `users.session_token_hash` 与 `users.session_expires_at`。Cookie 带 `HttpOnly`、`Secure`，`__Host-` 前缀要求 `Path=/` 且不带 `Domain`。写操作（POST、PUT、PATCH、DELETE）要求单值 `Origin` 头与 `X-CSRF-Token` 头——令牌在**现行实现**中由 `GET /api/session` 返回（`csrfToken` 字段）；设计稿写 `GET /api/sessions/current`（作废口径）。沿用创建契约 §6 与首批契约 §1。

Agent 不直接调用本 HTTP API。Agent 侧请求经 ADR-0011 所述受控 MCP 入口进入后台协调进程；MCP Schema 未冻结（ADR-0006 §3、[执行门槛](execution-integration-gates.md) G2），本文不定义。后台协调进程与 Web 同库，不经 HTTP（ADR-0013）。

租户：`organization_id` 一律由会话解析，请求体不接受、路径不出现。方案要求一人一组织（`users.organization_id`）。带 `organization_id` 列的表按该列过滤；不带该列的表经 `project_id` 关联到 `projects.organization_id` 过滤；两者都没有的表按平台级处理，见附录 E 写作中发现第 5 条。

权限列的取值：

| 取值 | 含义 |
| --- | --- |
| `公开` | 无会话即可调用：两个探针（`/healthz`、`/readyz`）、GitHub OAuth 登录入口（`POST /api/auth/github/login`、`GET /api/auth/github/callback`），以及决策链与技能治理的全部 GET 端点（as-built：组合根守卫对 GET 直接放行，见 `cmd/repomesh-web/main.go`）。写操作（PUT/POST）要求会话 + 单值 Origin + CSRF。原 `POST /api/sessions` 已作废（3.2 裁定注）。 |
| `本人` | 会话用户操作自己的 `users` 行。 |
| `成员` | 同组织的登录用户，且对路径所属项目可读。 |
| `组织管理员` | `users.org_role` 为组织管理员（附录 D）。 |
| `项目管理员` | ADR-0005 §1 与 ADR-0003 §1.8 需要的项目级角色。方案没有项目成员或项目角色表，本文暂以 `组织管理员` 执行，见附录 E 写作中发现第 1 条。 |
| `授权人` | 请求所属项目编制的 `agent_teams.human_grants` 中列出的用户；只用于评审批复（4.5，ADR-0022）。 |
| `后台` | 后台协调进程通过同一用例写入，含代 Agent 的调用。本文列出这些路径以固定资源形状；Web 不向浏览器会话开放它们，HTTP 层的实际调用方见附录 E 写作中发现第 4 条。 |

无权与不存在同样返回 404 `NOT_FOUND`（创建契约 §6）。错误正文不回显无权对象的名称或 ID。

方案中的字符串型操作者列（`credentials.updated_by`、`skills.created_by`、`skill_versions.created_by`、`recovery_decisions.decided_by`、`skill_update_suggestions.decided_by`、`events.actor_id`、`decision_chain_nodes.actor_id`）由服务端写入 `user:<userId>`、`agent:<agentId>` 或 `system` 三种形式之一，见附录 D。UUID 型操作者列（`review_requests.decided_by`、`alert_events.handled_by`、`skill_approvals.reviewer_user_id`）取会话用户，请求体不接受。

### 2.3 请求与响应格式

- 媒体类型 `application/json; charset=utf-8`。请求 `Content-Type` 不是 JSON 时返回 415 `UNSUPPORTED_MEDIA_TYPE`。
- 请求体上限 256 KiB，超过返回 413 `REQUEST_TOO_LARGE`（创建契约 §3）。
- 响应带 `Cache-Control: no-store`。
- JSON 字段用 camelCase；字段表给出与方案 snake_case 列的对应。
- 时间为 RFC3339 UTC 字符串，例如 `2026-09-15T09:30:00Z`。ID 为 UUID 字符串。
- 格式错误、重复 JSON 属性名、非法 Unicode 编码返回 400 `INVALID_JSON`；合法 JSON 的未知字段、非法长度、重复集合成员返回 422 `VALIDATION_FAILED`（首批契约 §1）。
- 创建型 POST 成功返回 201，带 `Location` 头与资源快照。状态动作 POST 成功返回 200 与资源快照；其中会触发外部动作的动作端点返回 201，见 2.7。PATCH 成功返回 200 与资源快照。PUT 首次建行返回 201，其后 200（适用 `credentials`、`delivery-policy`、`mcp-server-policies`）。DELETE 成功返回 204；软删除的 DELETE 返回 200 与快照（只有 `agent-skill-bindings`）。
- 列表响应形状为 `{"items":[...],"nextCursor":"..."}`，末页 `nextCursor` 为 `null`。

### 2.4 错误

错误正文形状：`{"error":{"code":"...","message":"...","fieldErrors":[{"field":"...","code":"..."}],"requestId":"..."}}`。

`requestId` 标识这次 HTTP 请求，不代替业务操作身份（创建契约 §6）。`fieldErrors` 只在 422 出现，其他状态码为空数组。

| HTTP | `error.code` | 触发条件 |
| --- | --- | --- |
| 400 | `INVALID_JSON` | 请求体不是合法 JSON、属性名重复、Unicode 非法。 |
| 400 | `INVALID_IDEMPOTENCY_KEY` | 要求 `Idempotency-Key` 的端点缺少该头、值不是 UUID、或出现多个值。 |
| 401 | `AUTHENTICATION_REQUIRED` | 无有效会话；登录失败也返回此码，不区分用户不存在与密码错误。 |
| 403 | `ORIGIN_REJECTED` | 写操作缺少 `Origin`、多值或不是允许来源。 |
| 403 | `CSRF_REJECTED` | 写操作缺少 `X-CSRF-Token` 或与会话不匹配。 |
| 403 | `FORBIDDEN` | 对象可见但当前权限不允许该动作，例如成员调用组织管理员端点。 |
| 404 | `NOT_FOUND` | 对象不存在或当前无权可见，两者同样返回。 |
| 409 | `IDEMPOTENCY_CONFLICT` | 同 `Idempotency-Key` 不同输入；`handoffs.branch_validation_key` 同键不同任务或载荷。 |
| 409 | `VERSION_CONFLICT` | `expectedVersion` 与 `tasks.version` 或 `change_sets.version` 不符；新建计划版本的 `baseVersion` 与最新 `plans.plan_version` 不符（4.3）。 |
| 409 | `INVALID_TRANSITION` | 状态动作不在允许迁移内；循环上限已到（ADR-0007）；占位冲突（ADR-0017）。 |
| 409 | `CURSOR_EXPIRED` | 分页游标过期或所绑定范围已变化，从第一页重新读取。 |
| 413 | `REQUEST_TOO_LARGE` | 请求体超过 256 KiB。 |
| 415 | `UNSUPPORTED_MEDIA_TYPE` | 请求媒体类型不是 JSON。 |
| 422 | `VALIDATION_FAILED` | 字段缺失、类型或长度不符、未知字段、未列出的 query 键、`limit` 越界、游标格式非法、唯一列冲突。 |
| 503 | `RESULT_UNCONFIRMED` | 外部结果或提交结果未知，客户端先按原键核查（ADR-0016）。 |
| 503 | `FEATURE_DISABLED` | 决策链开关关闭时调用 `decision-chains` 端点（ADR-0023）。 |

唯一列冲突（`users.username`、`agents.singleton_key`、`repositories.url`、`projects.repository_id`、`credentials.key`、`skills.name`、`skill_approvals.version_id`、`delivery_policies.project_id`、`decision_embeddings.node_id`、`mcp_server_policies.server_name`）返回 422 `VALIDATION_FAILED`，`fieldErrors[].code` 为 `DUPLICATE`；PUT 覆盖同键行不算冲突，`skill_approvals.version_id` 的重复发起按 8.5 处理。`handoffs.branch_validation_key` 同键按 2.5 返回 409 `IDEMPOTENCY_CONFLICT`，不在此列。固定状态码表中没有单独的唯一冲突码，见附录 E 写作中发现第 12 条。

### 2.5 幂等与乐观锁

所有创建型 POST 与状态动作 POST 要求 `Idempotency-Key` 头，值为 UUID，恰好一个；缺失、非法或多值返回 400 `INVALID_IDEMPOTENCY_KEY`。GitHub OAuth 登录端点要求恰好一个 `Idempotency-Key`（否则 422，见 `internal/web/auth.go`）；原 `POST /api/sessions` 设计不要求该头，已作废（3.2 裁定注；附录 E 写作中发现第 13 条随之失效）。PUT、DELETE、PATCH 不要求该头。

方案中只有 `agents`、`agent_teams`、`tasks`、`change_sets` 四张表有 `idempotency_key` 列。这四类资源的创建按“同键同输入 200 重放、同键异输入 409 `IDEMPOTENCY_CONFLICT`”执行：重放返回首次成功时的资源快照，不重新检查创建条件，仍检查当前读取权限（创建契约 §4）。同输入按解析后的允许字段比较，对象键顺序不影响相等，集合排序后比较（创建契约 §3）。方案没有输入摘要列，比较对象是已存行的对应列，见附录 E 写作中发现第 6 条。

其余资源的重放保护在方案中没有落点：服务端接受并记录该头，但不保证同键重放返回同一结果，见附录 E 第 11 条。本文不声称这些资源已有重放保护。

`handoffs.branch_validation_key` 唯一，作自然幂等键：同键同任务同载荷返回 200 与已有行；同键不同任务或不同载荷返回 409 `IDEMPOTENCY_CONFLICT`。

乐观锁只有 `tasks.version` 与 `change_sets.version`。这两类资源的 PATCH 与状态动作请求体带 `expectedVersion`，与当前值不符返回 409 `VERSION_CONFLICT`；成功后 `version` 加一。这沿用首批契约 §7 中 `expectedProjectRevision` 的“请求体携带期望版本”做法。`plans` 没有版本列，新建版本以请求体 `baseVersion` 比对最新 `plan_version`，不符同样返回 409 `VERSION_CONFLICT`（4.3）。其他资源没有版本列，PATCH 按最后写入生效。

### 2.6 分页与过滤

- 列表端点接受 `cursor` 与 `limit`。`limit` 默认 50，最大 100，超出 1 到 100 返回 422 `VALIDATION_FAILED`。
- `cursor` 不透明，绑定会话主体、资源范围、过滤条件与排序；格式非法返回 422 `VALIDATION_FAILED`，过期或范围变化返回 409 `CURSOR_EXPIRED`。
- 响应为 `items` 与 `nextCursor`。`nextCursor` 为 `null` 才表示遍历结束；允许 `items` 为空且 `nextCursor` 非空。
- 默认排序：有 `created_at`、`fired_at`、`logged_at`、`recorded_at`、`run_at`、`read_at`、`observed_at`、`bound_at` 之一的表按该列升序再按 `id`；其他表按 `id` 升序；资源小节可声明自己的排序列（`plans` 按 `plan_version`、`plan_steps` 按 `step_no`、`task_assignments` 按 `generation`）。不提供排序参数。
- 过滤参数逐资源列出。未列出的 query 键返回 422 `VALIDATION_FAILED`。
- `from`、`to` 为 RFC3339 UTC，含 `from`，不含 `to`。`q` 为大小写不敏感的子串匹配，最多 200 个 Unicode 标量，不解析通配符（首批契约 §8）。
- 每页重新核当前权限，跨页不持有数据库快照。

### 2.7 异步操作、事件与 SSE

任何会触发外部动作的写操作，在同一本地事务写业务行与 `events` 行（`channel=outbox`），提交后返回 201 与资源快照（ADR-0016、ADR-0010）。外部动作在提交后由后台协调进程执行，宿主能力经受限主机执行边界（ADR-0013）。结果通过 GET 资源与 SSE 观察。创建成功只证明已登记，不证明执行、就绪或投递（ADR-0018、ADR-0019）。会触发外部动作的状态动作端点有 `deliver`、`retry`、`profile`、`runtime`、`cleanup`、`refresh`，它们同样返回 201。

每次可见的业务状态变化在同一事务写一行 `events`（`channel=state`），`aggregate_type`、`aggregate_id`、`aggregate_version` 指向变化的资源。审计记录写 `channel=audit`，后台运行记录写 `channel=run`。写入方只有服务端。

SSE 的唯一端点是 `GET /api/events/stream`，媒体类型 `text/event-stream`。query 至少一个过滤：`aggregateType` 与 `aggregateId` 成对、`taskId`、`correlationId`；没有过滤返回 422 `VALIDATION_FAILED`。`events` 无 `project_id` 列，不提供 `projectId` 过滤，见附录 E 写作中发现第 23 条。数据源是 `events` 表 `channel=state`。事件帧的 `id` 为 `events.id`，`event` 名为 `events.event_type`，`data` 为 `{aggregateType, aggregateId, aggregateVersion, occurredAt}`，不带完整载荷。客户端收到后重新 GET 资源（创建契约 §8 的“失效通知 + REST 快照”）。

```text
id: 0d0b8b1e-4a2f-4c7e-8d0a-6a1f2b3c4d5e
event: task.status_changed
data: {"aggregateType":"task","aggregateId":"6f1d4c0e-2b7a-4a7e-9c1e-1c2f3a4b5d6e","aggregateVersion":4,"occurredAt":"2026-09-15T09:30:00Z"}

```

连接建立后先发 `resync_required`（`data` 为 `{}`），客户端重取快照。支持 `Last-Event-ID`：服务端从该 `events.id` 对应的 `recorded_at` 之后继续发送；该事件不在可重放窗口内时再次发送 `resync_required`。可重放窗口默认 24 小时，注释心跳 15 秒；两者是可调整运行参数。24 小时指 SSE 重放窗口，不是 `events` 行的删除策略，方案没有定义清理。不承诺补发，也不承诺只投递一次（ADR-0010）。每次发送前核当前权限，失权即关闭连接。

### 2.8 状态枚举的来源

方案把多数状态列写成“字符串”，没有给值。本文按三类处理：

1. 方案给出英文值的列，直接采用：`agents.role`（`leader`、`manager`、`worker`、`db-test-team`）、`credentials.kind`（`platform-kv`、`github-app`）、`messages.sender_type`（`agent`、`human`）、`events.channel`（`state`、`audit`、`outbox`、`run`）、`skill_test_questions.provided_by`（`manager`、`leader`、`human`）、`skill_approvals.subject_role`（`worker`、`manager`、`leader`）、`skill_approvals.reviewer_kind`（`manager`、`leader`、`human`）。
2. 方案给出中文描述的列，本文给英文令牌，标为提案：例如 `users.org_role`、`review_requests.status`、`skill_test_questions.kind`、`recovery_cases.status`、`alert_events.status`（方案“触发/认领/处理中/关闭”，本文 `fired`、`claimed`、`handling`、`closed`）、`skill_versions.status`（方案“草稿/评估中/已发布/废弃”，本文 `draft`、`evaluating`、`published`、`deprecated`）。
3. 方案没有描述的列，本文给值并给迁移规则，标为提案：例如 `tasks.status`、`change_sets.status`、`agent_teams.runtime_status`。

第 2、3 类全部集中在附录 D，正文引用时注明“见附录 D”。附录 D 是提案，不是已采用设计（附录 E 第 12 条）。

## 3. 账号与身份（4 张表）

方向职责：谁在这个平台上，即组织、人、Agent、集成凭据（方案 ①）。方案采用本地账号密码登录；现有 B02 GitHub 登录、连接与发现表在方案中没有落点（附录 E 第 8 条）。

### 3.1 组织 `organizations`

职责：最高层租户单位，一人只属一个组织，所有数据挂在组织下（方案 `organizations`）。

**端点**

| 方法与路径 | 用途 | 幂等 | 权限 |
| --- | --- | --- | --- |
| `GET /api/organizations/current` | 读取会话所属组织 | 天然 | 成员 |
| `PATCH /api/organizations/current` | 修改 `name` | 天然 | 组织管理员 |

**字段**

| 方案字段 | JSON 字段 | 读写 | 说明 |
| --- | --- | --- | --- |
| `id` | `id` | 读 | UUID。 |
| `name` | `name` | 读写 | 组织名，非空白，最多 200 个 Unicode 标量。 |
| `created_at` | `createdAt` | 读 | 创建时间。 |

**规则**

- 路径不出现组织 ID，`current` 由会话的 `users.organization_id` 解析（2.2）。
- 不提供组织的创建、列表与删除端点。组织与首个管理员的建立方式在方案中没有落点，见附录 E 写作中发现第 14 条。

### 3.2 用户与会话 `users`

职责：账号与登录态，一人只属一个组织；审批、逐级审核、驳回的动作主体（方案 `users`，由 `local_human_accounts`、`local_human_sessions` 并入）。

> **2026-09-16 用户裁定（附录 E 第 8 条结案）**：登录沿用现行 GitHub OAuth（`POST /api/auth/github/login`、`GET /api/auth/github/callback`、`POST /api/auth/github/reconnect`、`GET /api/auth/attempts/{id}`——均已实现，保留不改）；本地用户名密码登录**作废**，下文的 `POST /api/sessions` 设计仅作历史提案保留，不再实施。GitHub 安装范围的发现与候选抓取已由 access 发现链实现(worker + discovered_repositories)。

**端点**

| 方法与路径 | 用途 | 幂等 | 权限 |
| --- | --- | --- | --- |
| `POST /api/sessions` | （**作废**，见上方裁定注）登录：`username`、`password`；成功 `Set-Cookie`，写 `session_token_hash`、`session_expires_at` | 不要求键（2.5） | 公开 |
| `GET /api/sessions/current` | 当前会话：`user`、`csrfToken`、`expiresAt` | 天然 | 本人 |
| `DELETE /api/sessions/current` | 注销：清空 `session_token_hash`、`session_expires_at` | 天然 | 本人 |
| `GET /api/users` | 组织内用户列表；过滤 `active`、`orgRole` | 天然 | 成员 |
| `POST /api/users` | 建立用户：`username`、`displayName`、`password`、`orgRole`、`active` | 键（无落点） | 组织管理员 |
| `GET /api/users/{userId}` | 读取用户 | 天然 | 成员 |
| `PATCH /api/users/{userId}` | 修改 `displayName`、`orgRole`、`active` | 天然 | 组织管理员（`displayName` 限本人） |
| `POST /api/users/{userId}/password` | 改密：`newPassword`；本人须带 `currentPassword`，组织管理员为他人重置不带 | 键（无落点） | 本人或组织管理员 |

**字段**

| 方案字段 | JSON 字段 | 读写 | 说明 |
| --- | --- | --- | --- |
| `id` | `id` | 读 | UUID。 |
| `organization_id` | `organizationId` | 读 | 由会话解析（2.2）。 |
| `username` | `username` | 创建时写 | 登录名，唯一；冲突 422 `DUPLICATE`。 |
| `display_name` | `displayName` | 读写 | 显示名。 |
| `password_hash` | 不输出 | 不暴露 | 密码哈希，盐含在哈希内；请求体用 `password`、`newPassword` 传明文。 |
| `org_role` | `orgRole` | 读写 | 组织内角色，见附录 D。 |
| `active` | `active` | 读写 | `false` 时登录返回 401 `AUTHENTICATION_REQUIRED`，已有会话在下次请求失效。 |
| `session_token_hash` | 不输出 | 不暴露 | 当前会话令牌哈希；Cookie 明文只出现在 `Set-Cookie`。 |
| `session_expires_at` | `expiresAt` | 读 | 只在 `GET /api/sessions/current` 返回本人的过期时间。 |

不暴露：`password_hash`、`session_token_hash`。

**规则**

- 会话表 1:1 并入 `users`，一个用户同时只有一个有效会话；再次登录覆盖 `session_token_hash`，旧 Cookie 失效。见附录 E 写作中发现第 9 条。
- `GET /api/sessions/current` 不再沿用首批契约 §2 的 `githubConnection`，改为本地账号形状：`user{id, username, displayName, orgRole}` 与 `csrfToken`；`csrfToken` 绑定当前会话，不持久化到 URL。
- `PATCH` 不允许把最后一名组织管理员降级或停用，返回 409 `INVALID_TRANSITION`。
- 密码规则：最少 12 个 Unicode 标量，最多 256 个；不返回哈希算法名。

### 3.3 Agent 名册 `agents`

职责：Leader（总）、Manager（仓库）、Worker、数据库测试团队，每个可干活的身份（方案 `agents`，由 `agent_principals` 改造保留）。

**端点**

| 方法与路径 | 用途 | 幂等 | 权限 |
| --- | --- | --- | --- |
| `GET /api/agents` | 列表；过滤 `role`、`repositoryId`、`status`。每行携带 `projectId`（迁移 0053 新增：为空 = 组织级角色，如治理 leader） | 天然 | 成员 |
| `POST /api/agents` | 注册：`projectId`、`role`、`repositoryId`、`name`；`organization_id` **不由调用方传**，由项目反查（冗余租户戳）。幂等键是 `(project_id, singleton_key)`，`singleton_key` = `role:repository:name`（迁移 0053：此前是 `组织:角色:仓库:名字`，组织与账号 1:1，那道墙挡不住同一账号下的第二个项目） | 键 | 组织管理员 |
| `GET /api/agents/{agentId}` | 读取 | 天然 | 成员 |
| `PATCH /api/agents/{agentId}` | 修改 `status`、`responsibilityPaths`、`resourceRef`、`parentAgentId` | 天然 | 组织管理员 |

**字段**

| 方案字段 | JSON 字段 | 读写 | 说明 |
| --- | --- | --- | --- |
| `id` | `id` | 读 | UUID。 |
| `organization_id` | `organizationId` | 读 | 由会话解析。 |
| `role` | `role` | 创建时写 | 方案值 `leader`、`manager`、`worker`、`db-test-team`；方案 leader 为总、manager 为仓库，与 CONTEXT 相反，见附录 E 第 1 条。 |
| `parent_agent_id` | `parentAgentId` | 读写 | 直接上级；`worker` 指向 `manager`，`manager` 指向 `leader`。 |
| `repository_id` | `repositoryId` | 创建时写 | 绑定仓库。 |
| `responsibility_paths` | `responsibilityPaths` | 读写 | 负责的路径范围，字符串数组。 |
| `resource_ref` | `resourceRef` | 读写 | agentteams 资源绑定；键名见附录 D（ADR-0012）。 |
| `singleton_key` | `singletonKey` | 创建时写 | 防重复注册的固定标识，唯一；冲突 422 `DUPLICATE`。 |
| `status` | `status` | 读写 | 见附录 D。 |
| `idempotency_key` | `idempotencyKey` | 读 | 值来自 `Idempotency-Key` 头（2.5）。 |

**规则**

- `singleton_key` 与 `idempotency_key` 分工：前者防止同一身份被注册两次，后者防止同一请求被执行两次。
- `parent_agent_id` 与 `agent_teams` 的 `leader_agent_id`、`manager_agent_id`、`worker_agent_ids` 两处表达隶属，服务端不校验两者一致，见附录 E 写作中发现第 16 条。
- 方案没有 Agent 的调用凭证列；Agent 经 MCP 入口进入后台协调进程的身份如何核验待决（ADR-0011，附录 E 第 13 条）。

### 3.4 集成凭据 `credentials`

职责：GitHub App 注册结果与平台级配置项，存加密值，不含明文（方案 `credentials`，由 `platform_credentials`、`github_app_registrations` 并入）。

**端点**

| 方法与路径 | 用途 | 幂等 | 权限 |
| --- | --- | --- | --- |
| `GET /api/credentials` | 列表；不返回 `value_encrypted`，只返回 `kind`、`key`、`appId`、`installationId`、`ownerLogin`、`updatedBy`、`updatedAt` | 天然 | 组织管理员 |
| `PUT /api/credentials/{key}` | 写入或覆盖：`kind`、`value`（明文入参）、`appId`、`installationId`、`ownerLogin`；服务端加密后写 `value_encrypted` | 天然 | 组织管理员 |
| `DELETE /api/credentials/{key}` | 删除 | 天然 | 组织管理员 |

**字段**

| 方案字段 | JSON 字段 | 读写 | 说明 |
| --- | --- | --- | --- |
| `id` | `id` | 读 | UUID。 |
| `kind` | `kind` | 读写 | 方案值 `platform-kv`、`github-app`。GitHub App 安装信息对应 `github-app`（ADR-0002）。 |
| `key` | `key` | 读 | 路径参数，唯一；凭据键名或 App 标识。 |
| `value_encrypted` | 不输出 | 不暴露 | 加密后的值；请求体 `value` 为明文，任何响应不回显。 |
| `app_id` | `appId` | 读写 | GitHub App ID，`kind=github-app` 时必填。 |
| `installation_id` | `installationId` | 读写 | 安装 ID。 |
| `owner_login` | `ownerLogin` | 读写 | App 归属账号。 |
| `updated_by` | `updatedBy` | 读 | 服务端写 `user:<userId>`（2.2）。 |
| `updated_at` | `updatedAt` | 读 | 更新时间。 |

不暴露：`value_encrypted`。

**规则**

- `PUT` 首次写入返回 201，覆盖返回 200；`kind` 一旦写入不可改，变更返回 409 `INVALID_TRANSITION`。
- 方案没有密钥版本、包装根与轮换表（现有 `repomesh_secrets`），`value_encrypted` 的加密密钥管理未定义，见附录 E 第 9 条。
- 有效权限交集（ADR-0002）需要的安装范围数据不在本表，见附录 E 第 8 条。

## 4. 项目与编制（5 张表）

方向职责：拿什么仓库、开什么项目、做过什么规划、派什么班子、什么时候请人来审（方案 ②）。方案 `projects.repository_id` 必填唯一，即一项目一仓库；与 CONTEXT 的多仓库项目、ADR-0002 J2 及现有 `project_repositories` 冲突，本文按方案写（附录 E 第 2 条）。

### 4.1 项目 `projects`

职责：一次接入仓库开展工作的工作单元；原库 `project_id` 没有实体表，方案补上落点（方案 `projects`，新表）。

**端点**

| 方法与路径 | 用途 | 幂等 | 权限 |
| --- | --- | --- | --- |
| `GET /api/projects` | 列表；过滤 `status`、`q` | 天然 | 成员 |
| `POST /api/projects` | 建项：`name`、`repositoryId` | 键（无落点） | 成员 |
| `GET /api/projects/{projectId}` | 读取 | 天然 | 成员 |
| `PATCH /api/projects/{projectId}` | 修改 `name`、`status` | 天然 | 项目管理员 |

**字段**

| 方案字段 | JSON 字段 | 读写 | 说明 |
| --- | --- | --- | --- |
| `id` | `id` | 读 | UUID。 |
| `organization_id` | `organizationId` | 读 | 由会话解析。 |
| `repository_id` | `repositoryId` | 创建时写 | 对应仓库，唯一；同一仓库再建项目返回 422 `DUPLICATE`。 |
| `name` | `name` | 读写 | 项目名，非空白，最多 200 个 Unicode 标量。 |
| `status` | `status` | 读写 | 见附录 D。 |
| `created_at` | `createdAt` | 读 | 创建时间。 |

**规则**

- 项目创建不触发实例准备（ADR-0018）；实例经 `POST /api/agent-teams/{teamId}/runtime` 或首条消息异步准备。
- 首批契约 §6 的 `purpose`、`repositoryIds`、`configuration` 与 `projectRevision` 在方案中没有列；原操作回执端点的去向见附录 C，配置引用见附录 E 第 9 条。
- `status` 迁移只允许 `draft` 到 `active`、`active` 到 `archived`，其余 409 `INVALID_TRANSITION`（附录 D）。

### 4.2 仓库档案 `repositories`

职责：接入仓库的基础信息、画像结论、轮询进度（方案 `repositories`，由 `repositories`、`plan_snapshots` 画像部分、`scm_poll_cursors` 并入）。

**端点**

| 方法与路径 | 用途 | 幂等 | 权限 |
| --- | --- | --- | --- |
| `GET /api/repositories` | 列表；过滤 `q`（匹配 `name`、`url`） | 天然 | 成员 |
| `POST /api/repositories` | 登记：`name`、`url`、`description`、`topics`、`languages` | 键（无落点） | 成员 |
| `GET /api/repositories/{repositoryId}` | 读取 | 天然 | 成员 |
| `PATCH /api/repositories/{repositoryId}` | 修改 `name`、`description`、`topics`、`languages` | 天然 | 成员 |
| `POST /api/repositories/{repositoryId}/profile` | 触发画像，异步（201）；完成后后台写 `profiled_at`、`topics`、`languages` | 键（无落点） | 成员 |

**字段**

| 方案字段 | JSON 字段 | 读写 | 说明 |
| --- | --- | --- | --- |
| `id` | `id` | 读 | UUID。 |
| `name` | `name` | 读写 | 仓库名，索引。 |
| `url` | `url` | 创建时写 | 仓库地址，唯一；冲突 422 `DUPLICATE`。 |
| `description` | `description` | 读写 | 描述，可为空字符串。 |
| `topics` | `topics` | 读写 | 主题标签，字符串数组；画像完成后由后台覆盖。 |
| `languages` | `languages` | 读写 | 语言构成，对象；画像完成后由后台覆盖。 |
| `profiled_at` | `profiledAt` | 读 | 画像时间，后台写。 |
| `poll_last_at` | `pollLastAt` | 读 | 上次轮询，后台轮询写。 |
| `poll_next_at` | `pollNextAt` | 读 | 下次轮询，后台轮询写。 |
| `poll_failures` | `pollFailures` | 读 | 连续失败次数，创建时为 `0`，后台轮询写。 |

**规则**

- `profile` 是 ADR-0020 的可选分析：不扩仓、不建计划、不触发实例准备。请求登记后返回 201，结果经 GET 观察 `profiled_at` 变化，或订阅 `events/stream` 过滤 `aggregateType=repository`。
- 方案没有 `organization_id`，`url` 全局唯一，仓库档案按平台级共享，见附录 E 写作中发现第 5 条。
- 现有 `GET /api/repositories` 的 GitHub 发现语义（`userParticipation`、`appCapability`、`coverage`，首批契约 §3）在方案中没有列，见附录 E 第 8 条。

### 4.3 规划快照 `plans`

职责：对项目做过哪次规划分析；规格、版本史、执行计划全并进一张（方案 `plans`，由 `plan_snapshots`、`specifications`、`specification_versions`、`execution_plans`、`execution_plan_revisions` 并入）。

**端点**

| 方法与路径 | 用途 | 幂等 | 权限 |
| --- | --- | --- | --- |
| `GET /api/projects/{projectId}/plans` | 列表，按 `plan_version` 排列 | 天然 | 成员 |
| `POST /api/projects/{projectId}/plans` | 新建版本：`baseVersion`、`requirementText`、`engineeringSpec`、`contracts`、`taskDag`、`executionBatches`、`createdByAgentId`（可省略） | 键（无落点） | 后台（代 Manager） |
| `GET /api/plans/{planId}` | 读取 | 天然 | 成员 |
| `POST /api/plans/{planId}/revisions` | 追加一条修订记录到 `revisions`，不改已发布内容 | 键（无落点） | 后台（代 Manager） |
| `POST /api/plans/{planId}/apply` | 应用版本：按 v(n-1) 到 v(n) 的差异迁移任务，未变任务保持，修改或删除的任务置 `superseded`，新增任务创建；同事务写 `events` 与一条 `decision_chain_nodes`（`status=adjusted`） | 键（无落点） | 后台（代 Leader） |

**字段**

| 方案字段 | JSON 字段 | 读写 | 说明 |
| --- | --- | --- | --- |
| `id` | `id` | 读 | UUID。 |
| `project_id` | `projectId` | 读 | 路径参数。 |
| `plan_version` | `planVersion` | 读 | 字符串；服务端按项目递增，每次内容变化新建一行（ADR-0003 §1.1）。 |
| `requirement_text` | `requirementText` | 创建时写 | 需求原文。 |
| `engineering_spec` | `engineeringSpec` | 创建时写 | 工程规格。 |
| `contracts` | `contracts` | 创建时写 | 接口契约，JSON。 |
| `task_dag` | `taskDag` | 创建时写 | 任务依赖图，JSON；Graph 结果只读观察（ADR-0014）。 |
| `execution_batches` | `executionBatches` | 创建时写 | 执行批次，JSON；循环上限落点之一（ADR-0007 D3）。 |
| `revisions` | `revisions` | 读 | 修订史，JSON 数组；`POST .../revisions` 追加 `{summary, reason, revisedBy, revisedAt}`。 |
| `execution_plan_id` | `executionPlanId` | 读 | 对应执行计划；44 张表中没有目标表，见附录 E 写作中发现第 3 条。 |
| `created_by_agent_id` | `createdByAgentId` | 创建时写 | 规划者，可空。 |

**规则**

- 已存在版本不原地改写；修改目标、范围或约束产生新行（ADR-0003 §1.1）。ADR-0015 限额内推进下一轮属于执行进展，不新建 `plans` 行。
- 新版本是完整目标状态快照（B11 重规划协议 §2 步骤 5）。`POST /api/projects/{projectId}/plans` 建 v(n+1) 时请求体带 `baseVersion`（上一版 `plan_version`），不符返回 409 `VERSION_CONFLICT`；`executionBatches` 在建版本时做 DAG 拓扑校验，不满足依赖返回 422 `VALIDATION_FAILED`。
- `apply` 按 v(n-1) 到 v(n) 的差异迁移任务：未变任务保持，修改或删除的任务置 `superseded`（5.1），新增任务创建；同事务写 `events` 与一条 `decision_chain_nodes`（`status=adjusted`，B11 重规划协议 §2 双轴挂钩；落 `status` 列，见 9.11）。
- `apply` 前是否需要人工批准由 `agent_teams.execution_mode` 决定；需要时先建 `review_requests(objectType=plan)`，通过后才允许 `apply`（B11 重规划协议 §2 审批闸位）。
- ADR-0003 要求提出、获准、应用、生效四个事实分开。提出即 `POST .../plans`；获准经 `review_requests`（`objectType=plan`，4.5）；应用即 `POST /api/plans/{planId}/apply`；生效事实在方案中没有列，`plans` 无 `status`，`tasks` 无当前生效计划指针，见附录 E 第 5 条。
- ADR-0003 §3.1：首期不提供直接编辑图节点与连线的页面 API，`taskDag` 只能整份随新版本提交。

### 4.4 编制 `agent_teams`

职责：哪个仓库用哪套 Agent 班子、谁领导谁、执行策略、运行状态（方案 `agent_teams`，由 `agent_topologies`、`repository_agent_teams`、`topology_policy_drafts` 并入）。

**端点**

| 方法与路径 | 用途 | 幂等 | 权限 |
| --- | --- | --- | --- |
| `GET /api/projects/{projectId}/agent-teams` | 列表 | 天然 | 成员 |
| `POST /api/projects/{projectId}/agent-teams` | 建立编制：`repositoryId`、`leaderAgentId`、`managerAgentId`、`workerAgentIds`、`teamName`、`executionMode`、`requiredCheckpoints`、`humanGrants` | 键 | 项目管理员 |
| `GET /api/agent-teams/{teamId}` | 读取 | 天然 | 成员 |
| `PATCH /api/agent-teams/{teamId}` | 修改 `workerAgentIds`、`teamName`、`executionMode`、`requiredCheckpoints`、`humanGrants` | 天然 | 项目管理员（验证小组开关与成员配置，ADR-0005 §1）；其余改动暂用组织管理员 |
| `POST /api/agent-teams/{teamId}/runtime` | `action` 取 `start` 或 `stop`，写 `runtime_status`，异步（201） | 键（无落点） | 项目管理员 |

**字段**

| 方案字段 | JSON 字段 | 读写 | 说明 |
| --- | --- | --- | --- |
| `id` | `id` | 读 | UUID。 |
| `project_id` | `projectId` | 读 | 路径参数。 |
| `repository_id` | `repositoryId` | 创建时写 | 适用仓库；项目级编制时为 `null`。 |
| `leader_agent_id` | `leaderAgentId` | 创建时写 | 总 Leader（方案命名，附录 E 第 1 条）。 |
| `manager_agent_id` | `managerAgentId` | 创建时写 | 仓库 Manager（方案命名，附录 E 第 1 条）。 |
| `worker_agent_ids` | `workerAgentIds` | 读写 | Worker 名单，UUID 数组。 |
| `team_name` | `teamName` | 读写 | 执行面编制名，可空。 |
| `room_id` | `roomId` | 读 | 协作房间，后台准备后写。 |
| `execution_mode` | `executionMode` | 读写 | 自动化档位，见附录 D（ADR-0003 §1.2、ADR-0006 §2）。 |
| `required_checkpoints` | `requiredCheckpoints` | 读写 | 必要检查点，JSON。 |
| `human_grants` | `humanGrants` | 读写 | 人工授权项，JSON；不扩大 Git 权限（ADR-0002、ADR-0003 §1.2）。 |
| `runtime_status` | `runtimeStatus` | 读 | 见附录 D；`runtime` 动作写请求态，后台写结果态。 |
| `idempotency_key` | `idempotencyKey` | 读 | 值来自 `Idempotency-Key` 头。 |

**规则**

- ADR-0005 §1 只要求验证小组开关与成员配置由项目管理员操作；其余编制改动的授权来源方案未定义，暂用组织管理员。普通执行 Agent 不改项目级设置。
- `runtime start` 只登记请求；实例按 ADR-0018 在首条消息或建项提交后异步准备，`runtime_status` 由后台写回。
- `runtime stop` 只登记请求，不代表在途已停；停止事实由后台写回 `runtime_status`（ADR-0013）。
- `execution_mode` 当前阶段只允许 `yolo`（ADR-0006 §2）；`approval` 在附录 D 保留值，服务端拒绝写入并返回 422。
- ADR-0012 的上游 Project 引用：方案把 `resource_ref` 放在 `agents`，本表没有该列。本文把实例与 team 引用放在成员的 `agents.resource_ref`，把每次派单对应的上游 `project_id` 放在 `task_assignments.dispatch_ref`，键名见附录 D。编制级引用无落点，见附录 E 写作中发现第 21 条。
- `repository_id` 可空的项目级编制与 `projects` 一项目一仓库并存，见附录 E 第 2 条。

### 4.5 评审请求 `review_requests`

职责：机器拿不准请人审，人的批复一来一回一张表记完（方案 `review_requests`，由 `human_review_requests`、`checkpoint_decisions` 并入）。

**端点**

| 方法与路径 | 用途 | 幂等 | 权限 |
| --- | --- | --- | --- |
| `GET /api/review-requests` | 列表；过滤 `status`、`objectType`、`projectId` | 天然 | 成员 |
| `GET /api/review-requests/{requestId}` | 读取 | 天然 | 成员 |
| `POST /api/review-requests` | 发起：`projectId`、`objectType`、`objectId`、`requestContent`、`requestedByAgentId`（可省略） | 键（无落点） | 后台（代 Agent） |
| `POST /api/review-requests/{requestId}/decision` | 批复：`decision` 取 `approve` 或 `reject`，`note` | 键（无落点） | 授权人；漂移时组织管理员兜底（ADR-0022） |

**隔离（2026-09-20 落地，`List(actor, admin, projectId, status)`）**：`projectId` 不只是
「可选过滤」，它是可见性的开关。

- 带 `projectId`：先按 `ResolveProjectScopeAs` 解析归属，项目 `owner` 或管理员才拿得到，
  否则 404（不泄露「这个项目存在，只是不是你的」）；
- 不带 `projectId`：只返回**调用者自己名下项目**的单，**管理员也不例外**；
- 写路径同理：`POST /api/v1/projects/{projectId}/checkpoint-decisions` 与 `.../control`
  此前只比对「这张单属于该项目」，等于**任何登录账号**知道 id 就能拍板别人项目的检查点，
  现在同样先过归属解析。

此前 `List` 的判据是「管理员看全表、非管理员看 `assignee = 自己`」：前者是一次跨账号的
全库拉取（泄漏），后者会因生产者漏填 `assignee` 而让单子对**所有人**隐身（连项目属主
也看不见）。归属现在取自 `repomesh_projects.projects.owner`。

**字段**

| 方案字段 | JSON 字段 | 读写 | 说明 |
| --- | --- | --- | --- |
| `id` | `id` | 读 | UUID。 |
| `project_id` | `projectId` | 创建时写 | 所属项目。 |
| `object_type` | `objectType` | 创建时写 | 方案列举任务、编制、Skill 放行；本文增加 `plan`、`interpretation`，见附录 D 与附录 E 第 5 条。 |
| `object_id` | `objectId` | 创建时写 | 评审对象 ID。 |
| `request_content` | `requestContent` | 创建时写 | 请求内容快照，JSON。 |
| `status` | `status` | 动作写 | 见附录 D。 |
| `requested_by_agent_id` | `requestedByAgentId` | 创建时写 | 发起评审的 Agent，可空。 |
| `decided_by` | `decidedBy` | 读 | 服务端取会话用户，请求体不接受（ADR-0003 §1.6）；自动许可时为空。 |
| `decision_note` | `decisionNote` | 动作写 | 批复意见；自动许可时为 `auto:yolo`（附录 D）。 |
| `decided_at` | `decidedAt` | 读 | 批复时间。 |

**规则**

- `decision` 只允许 `pending` 到 `approved` 或 `rejected`，其余 409 `INVALID_TRANSITION`。拒绝保留请求与原因（ADR-0003 §1.5）。
- 人工对检查点的决议只能经 `POST /api/review-requests/{requestId}/decision` 写入，同事务写 `decided_by`、`decision_note`、`decided_at` 并把 `status` 置为终态；这是唯一落库入口（ADR-0022 决策 1）。
- 政策漂移兜底（ADR-0022 决策 2）：`pending` 请求本身就是“开单时卡点启用过”的事实，决议时不按当前 `agent_teams.execution_mode`、`required_checkpoints` 复查。决议权限分层：请求所属项目编制的 `agent_teams.human_grants` 中的授权人可决；编制已改为无人工卡点或授权人为空时，组织管理员可兜底清偿；其他账号 403 `FORBIDDEN`。
- 聊天批复的定位（ADR-0022 决策 1）：本文选择“不构成决议，仅是沟通”，房间消息不会改变本表。若将来改为把聊天批准适配成一次正式调用，决议记录必须带来源标注（`decision_note` 前缀 `chat:<messageId>`，提案，附录 D）；两种定位只能选一种。
- 存量漂移单的批量清偿也逐条走同一端点，每条留下 `decided_by`、`decision_note`、`decided_at`（ADR-0022 决策 4）。
- 批准不等于生效：`objectType=plan` 批准后进入“已获准，待应用”，应用经 `POST /api/plans/{planId}/apply`（4.3），读回由后台协调进程执行（ADR-0003 §1.4、ADR-0006 §3）。
- ADR-0004 的缺失规则解释也走此表，`objectType=interpretation`（提案）；解释与验收通过分开。
- 人工门禁与环境阻塞、资源等待分开表达（ADR-0006 §2、CONTEXT 人工门禁）：环境阻塞体现在 `tasks.status=blocked`，不产生本表行。
- 当前阶段统一 YOLO（ADR-0006 §2）。YOLO 档的自动许可也写一行本表：`status=approved`、`decided_by` 为空、`decision_note` 为 `auto:yolo`（提案），使 ADR-0003 §1.2 的许可事实可追溯。见附录 E 写作中发现第 7 条。

**示例**：`GET /api/review-requests/{requestId}` 已批复的快照。

```json
{
	"id": "b1f0c2d4-8e3a-4f6b-9a7c-2d1e0f9a8b7c",
	"projectId": "3c9e6679-7425-40de-944b-e07fc1f90ae7",
	"objectType": "plan",
	"objectId": "9a7b3c1d-5e2f-4a8b-b6c4-d3e2f1a0b9c8",
	"requestContent": {
		"planVersion": "2",
		"summary": "增加导出功能的验证步骤",
		"changes": ["新增步骤 4：导出校验"]
	},
	"status": "approved",
	"requestedByAgentId": "5d1c8e2a-3b4f-4c6d-8e9f-0a1b2c3d4e5f",
	"decidedBy": "7e2f9a1b-4c3d-4e5f-9a8b-1c2d3e4f5a6b",
	"decisionNote": "同意，按版本 2 应用。",
	"decidedAt": "2026-09-15T09:32:10Z"
}
```

## 5. 任务主线（4 张表）

方向职责：一条 Issue 怎么变成计划、拆步骤、派单、干完、交测试（方案 ③）。术语对应：CONTEXT 的 Issue 对应 `parent_task_id` 为空的顶层 task，`source_ref` 保存外部来源；CONTEXT 的 Task 对应子 task；CONTEXT 的 Attempt 对应 `task_assignments` 一行。方案没有独立 issues 表，与 ADR-0019 的 Issue 独立列表与详情的关系见附录 E 第 4 条。

### 5.1 任务 `tasks`

职责：任务主表，一条 Issue 一个任务，占位锁并入状态字段（方案 `tasks`，由 `tasks`、`worker_execution_reservations` 占位部分并入）。

**端点**

| 方法与路径 | 用途 | 幂等 | 权限 |
| --- | --- | --- | --- |
| `GET /api/projects/{projectId}/tasks` | 列表；过滤 `status`、`parentTaskId`、`repositoryId`、`assigneeAgentId` | 天然 | 成员 |
| `POST /api/projects/{projectId}/tasks` | 建立顶层任务（Issue）：`title`、`instruction`、`acceptance`、`sourceRef`、`repositoryId`；同事务建立主 `change_sets` 行（ADR-0008、ADR-0016） | 键 | 成员 |
| `GET /api/tasks/{taskId}` | 读取 | 天然 | 成员 |
| `PATCH /api/tasks/{taskId}` | 修改 `title`、`instruction`、`acceptance`、`sourceRef`、`repositoryId`；需 `expectedVersion` | 版本 | 成员 |
| `POST /api/tasks/{taskId}/subtasks` | 建立子任务，同字段；`parent_task_id` 指向路径任务 | 键 | 后台（代 Manager） |
| `POST /api/tasks/{taskId}/reserve` | 占位：`assigneeAgentId`、`expectedVersion`；短事务同时写 `assignee_agent_id`、`reserved_at`、`status`（ADR-0017） | 键（无落点）、版本 | 后台（代 Manager） |
| `POST /api/tasks/{taskId}/release` | 释放占位：`expectedVersion` | 键（无落点）、版本 | 后台（代 Manager） |
| `POST /api/tasks/{taskId}/pause` | 暂停：`expectedVersion`；只停新派工（ADR-0001、ADR-0003 §1.4、CONTEXT 暂停） | 键（无落点）、版本 | 成员 |
| `POST /api/tasks/{taskId}/resume` | 恢复：`expectedVersion` | 键（无落点）、版本 | 成员 |
| `POST /api/tasks/{taskId}/cancel` | 取消：`expectedVersion`；终态 | 键（无落点）、版本 | 成员 |
| `POST /api/tasks/{taskId}/complete` | 完成：`expectedVersion`、`resultSummary`；写 `result_summary` | 键（无落点）、版本 | 后台（代 Manager）或成员 |

**字段**

| 方案字段 | JSON 字段 | 读写 | 说明 |
| --- | --- | --- | --- |
| `id` | `id` | 读 | UUID。 |
| `organization_id` | `organizationId` | 读 | 由会话解析。 |
| `project_id` | `projectId` | 读 | 路径参数；子任务继承父任务。 |
| `repository_id` | `repositoryId` | 读写 | 关联仓库，可空。 |
| `parent_task_id` | `parentTaskId` | 读 | 父任务；顶层任务为 `null`；只由 `subtasks` 端点设定。 |
| `title` | `title` | 读写 | 标题，非空白，最多 200 个 Unicode 标量（创建契约 §3）。 |
| `instruction` | `instruction` | 读写 | 任务说明，最多 20,000 个 Unicode 标量。 |
| `acceptance` | `acceptance` | 读写 | 验收标准，文本；创建契约 §3 的 `acceptanceCriteria` 数组在此以换行连接保存。 |
| `source_ref` | `sourceRef` | 读写 | 来源溯源，JSON，例如 `{kind, number, title, url}`。 |
| `status` | `status` | 动作写 | 状态机，见下表与附录 D。 |
| `assigned_by_agent_id` | `assignedByAgentId` | 读 | 派单方；`POST .../assignments` 写入 `managerAgentId`。 |
| `assignee_agent_id` | `assigneeAgentId` | 读 | 当前执行者；`reserve` 写，`release` 清空。 |
| `reserved_at` | `reservedAt` | 读 | 占位时间；与 `assignee_agent_id` 同事务写。 |
| `result_summary` | `resultSummary` | 动作写 | 结果摘要；`complete` 写。 |
| `version` | `version` | 读 | 乐观锁版本，每次成功写加一（2.5）。 |
| `idempotency_key` | `idempotencyKey` | 读 | 值来自 `Idempotency-Key` 头。 |

**状态机**（值为附录 D 提案）

| 当前状态 | 目标状态 | 触发 | 说明 |
| --- | --- | --- | --- |
| `open` | `planned` | 后台：`plans` 行建立且 `plan_steps` 覆盖该任务 | 顶层任务进入已规划。 |
| `open`、`planned`、`submitted`、`blocked` | `reserved` | `reserve` | 目标 Worker 已有活跃任务（另一 `tasks` 行 `assignee_agent_id` 等于它且状态为 `reserved` 或 `in_progress`）时 409 `INVALID_TRANSITION`（ADR-0001、ADR-0017）。 |
| `reserved` | `in_progress` | 后台：派单写回 `dispatch_ref` | 环境准备在事务外，启动前再次核验（ADR-0017）。 |
| `reserved`、`in_progress`、`blocked` | `open` | `release` | `in_progress` 且当前派单 `attempt_state` 非终态时 409 `INVALID_TRANSITION`；未知状态不释放资源（ADR-0017、执行门槛 G4）。 |
| `in_progress` | `submitted` | 后台：当前派单 `attempt_state=succeeded` | 提交不等于验收（CONTEXT Candidate）。 |
| `in_progress` | `blocked` | 后台：当前派单 `attempt_state` 为 `failed` 或 `blocked` | 环境阻塞与人工门禁分开（ADR-0006 §2）。 |
| `submitted` | `handed_off` | `POST /api/tasks/{taskId}/handoffs` 成功 | 交给数据库测试团队。 |
| `planned`、`submitted`、`handed_off` | `done` | `complete` | 子任务完成不自动完成父任务；父任务由 Manager 另行 `complete`（ADR-0001）。`in_progress` 与 `blocked` 不能直接完成，先 `release` 或等待派单终态。 |
| 非终态 | `paused` | `pause` | 只停新派工，不代表在途已停；已交付变更不撤回（CONTEXT 暂停）。 |
| `paused` | 推导 | `resume` | 无 `assignee_agent_id` 回到 `open`；有 `reserved_at` 且当前派单非终态回到 `in_progress`；当前派单终态回到 `submitted`，其中 `attempt_state` 为 `failed` 或 `blocked` 时回到 `blocked`。方案没有暂停前状态列，见附录 E 写作中发现第 2 条。 |
| 非终态 | `cancelled` | `cancel` | 终态。 |
| 非终态 | `superseded` | 后台：`POST /api/plans/{planId}/apply` 写 | 终态；被新计划版本修改或删除的任务（B11 重规划协议 §2 步骤 5）。投影闸门封死：`task_assignments` 的 `result` 对其返回 409 `INVALID_TRANSITION`。 |

`done`、`cancelled`、`superseded` 为终态，任何动作返回 409 `INVALID_TRANSITION`。上表之外的迁移同样返回 409。

**规则**

- 顶层任务的创建同事务建立允许暂空的主 `change_sets` 行（ADR-0008、ADR-0016），经 `GET /api/tasks/{taskId}/change-sets` 读取；同键重放返回原任务，不重复建立变更集。
- `reserve` 是 ADR-0017 的短事务：三列一起写或一起不写；事务不跨越环境准备。一个 Worker 同时只能有一个活跃任务（ADR-0001）。
- `pause` 与 `cancel` 的原因没有列，请求体不接受 `reason`，见附录 E 写作中发现第 20 条。
- 创建契约 §3 的 `repositoryIds` 集合在本表只剩单个 `repository_id`，见附录 E 第 2 条。

**示例**：`GET /api/tasks/{taskId}` 顶层任务快照。

```json
{
	"id": "6f1d4c0e-2b7a-4a7e-9c1e-1c2f3a4b5d6e",
	"organizationId": "0a1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d",
	"projectId": "3c9e6679-7425-40de-944b-e07fc1f90ae7",
	"repositoryId": "c56a4180-65aa-42ec-a945-5fd21dec0538",
	"parentTaskId": null,
	"title": "增加订单归档",
	"instruction": "归档历史订单，保持原有读取权限。",
	"acceptance": "无读取权限的用户无法访问归档订单。",
	"sourceRef": {
		"kind": "issue_page",
		"number": 103
	},
	"status": "reserved",
	"assignedByAgentId": "5d1c8e2a-3b4f-4c6d-8e9f-0a1b2c3d4e5f",
	"assigneeAgentId": "8f7e6d5c-4b3a-4291-a0b9-c8d7e6f5a4b3",
	"reservedAt": "2026-09-15T09:30:00Z",
	"resultSummary": null,
	"version": 4,
	"idempotencyKey": "93d25876-e728-4f2e-b680-c7c5b951f163"
}
```

### 5.2 计划步骤 `plan_steps`

职责：Manager 拆出的每个具体步骤，谁做、依赖谁、什么状态（方案 `plan_steps`，由 `execution_plan_tasks` 独立保留，看板和依赖调度按行读）。

**端点**

| 方法与路径 | 用途 | 幂等 | 权限 |
| --- | --- | --- | --- |
| `GET /api/plans/{planId}/steps` | 列表，按 `step_no` 排列 | 天然 | 成员 |
| `POST /api/plans/{planId}/steps` | 新建步骤：`stepNo`、`content`、`assigneeRole`、`dependsOn` | 键（无落点） | 后台（代 Manager） |
| `GET /api/plan-steps/{stepId}` | 读取 | 天然 | 成员 |
| `PATCH /api/plan-steps/{stepId}` | 修改 `status`、`output` | 天然 | 后台（代 Manager） |

**字段**

| 方案字段 | JSON 字段 | 读写 | 说明 |
| --- | --- | --- | --- |
| `id` | `id` | 读 | UUID。 |
| `plan_id` | `planId` | 读 | 路径参数。 |
| `step_no` | `stepNo` | 创建时写 | 步骤序号；同计划内唯一为本文约束（提案）。 |
| `content` | `content` | 创建时写 | 步骤内容。 |
| `assignee_role` | `assigneeRole` | 创建时写 | 负责角色，取 `agents.role` 的值。 |
| `depends_on` | `dependsOn` | 创建时写 | 依赖步骤 ID 数组。 |
| `status` | `status` | 读写 | 见附录 D。 |
| `output` | `output` | 读写 | 产出物，JSON。 |

**规则**

- 依赖关系是上游 DAG 的读回（ADR-0014、ADR-0015），`dependsOn` 只在创建时写；本文不提供改连线的端点（ADR-0003 §3.1）。本节的 `POST` 与 `PATCH` 只供后台协调进程与 Manager 工具，浏览器只读。
- `dependsOn` 出现环时返回 422 `VALIDATION_FAILED`。换图协议未选（执行门槛 G5）。

### 5.3 派单 `task_assignments`

职责：一次派单一行，含每轮尝试的结果史与执行派发记录（方案 `task_assignments`，由 `leader_assignments`、`task_assignment_attempts`、`runner_dispatches` 并入）。

**端点**

| 方法与路径 | 用途 | 幂等 | 权限 |
| --- | --- | --- | --- |
| `GET /api/tasks/{taskId}/assignments` | 列表，按 `generation` 排列 | 天然 | 成员 |
| `POST /api/tasks/{taskId}/assignments` | 派单：`stepId`、`managerAgentId`、`workerAgentId`、`phase`、`safetyEnvelope`；服务端写 `generation`、`previous_attempt_id`；与 `reserve` 同事务（ADR-0017） | 键（无落点） | 后台（代 Manager） |
| `GET /api/task-assignments/{assignmentId}` | 读取 | 天然 | 成员 |
| `POST /api/task-assignments/{assignmentId}/result` | 写结果：`attemptState`、`attemptReason`、`executionId`、`finishedAt` | 键（无落点） | 后台（代 Worker 或 Runner） |

**字段**

| 方案字段 | JSON 字段 | 读写 | 说明 |
| --- | --- | --- | --- |
| `id` | `id` | 读 | UUID。 |
| `task_id` | `taskId` | 读 | 路径参数。 |
| `step_id` | `stepId` | 创建时写 | 对应计划步骤，可空。 |
| `manager_agent_id` | `managerAgentId` | 创建时写 | 派单 Manager；同时写入 `tasks.assigned_by_agent_id`。 |
| `worker_agent_id` | `workerAgentId` | 创建时写 | 接单 Worker；同时写入 `tasks.assignee_agent_id`。 |
| `phase` | `phase` | 创建时写 | 派单阶段，见附录 D；决定循环上限的计数口径（ADR-0007 D3）。 |
| `safety_envelope` | `safetyEnvelope` | 创建时写 | 安全边界，JSON。 |
| `generation` | `generation` | 读 | 第几轮尝试；服务端取同任务同 `phase` 的最大值加一，按 `phase` 分组计数为本文提案。 |
| `attempt_state` | `attemptState` | 动作写 | 尝试结果，见附录 D。 |
| `attempt_reason` | `attemptReason` | 动作写 | 失败原因。 |
| `previous_attempt_id` | `previousAttemptId` | 读 | 服务端自动指向同任务上一轮。 |
| `dispatch_ref` | `dispatchRef` | 读 | 执行派发记录，后台写；含实例、team、上游 `project_id`（ADR-0012），键名见附录 D。 |
| `execution_id` | `executionId` | 动作写 | 对应执行。 |
| `finished_at` | `finishedAt` | 动作写 | 完成时间。 |

**规则**

- `POST .../assignments` 在同一短事务内完成 `reserve` 的三列写入（ADR-0017）；任务已由其他 Worker 占位时 409 `INVALID_TRANSITION`。
- 循环上限（ADR-0007 D3、D4）：`phase=fix` 最多 3 轮，`phase=diagnose` 最多 2 轮，`phase=recover` 最多 2 次；连续两轮 `attempt_reason` 相同视为同一失败，结束循环。超限由后台协调进程拒绝新派单并返回 409 `INVALID_TRANSITION`。
- 迟到结果（ADR-0001、ADR-0007 D7、ADR-0015）：`result` 只改本行；只有 `generation` 最大的一行能改变 `tasks.status`。`attempt_state` 已是终态时再次 `result` 返回 409 `INVALID_TRANSITION`，旧结果不被覆盖；任务已 `superseded` 时同样 409（投影闸门，5.1）。
- `attempt_state=blocked` 的结果同事务写一条 `decision_chain_nodes`（`status=blocked`），不建独立反馈表（B11 重规划协议 §4；落 `status` 列，见 9.11）。
- 一轮尝试一行；重派新建行，不改旧行（CONTEXT Attempt）。

**示例**：`POST /api/tasks/{taskId}/assignments` 的 201 响应。

```json
{
	"id": "2e7c1a9b-6d4f-4b3a-8c2e-1f0d9e8b7a6c",
	"taskId": "6f1d4c0e-2b7a-4a7e-9c1e-1c2f3a4b5d6e",
	"stepId": "4b8d2f6e-1a3c-4e5f-9b7d-8c6a5e4f3d2b",
	"managerAgentId": "5d1c8e2a-3b4f-4c6d-8e9f-0a1b2c3d4e5f",
	"workerAgentId": "8f7e6d5c-4b3a-4291-a0b9-c8d7e6f5a4b3",
	"phase": "fix",
	"safetyEnvelope": {
		"allowedPaths": ["services/orders/"],
		"maxDurationSeconds": 3600
	},
	"generation": 2,
	"attemptState": "pending",
	"attemptReason": null,
	"previousAttemptId": "1d6b0f8a-5c3e-4a2b-9d1c-0e9f8a7b6c5d",
	"dispatchRef": null,
	"executionId": null,
	"finishedAt": null
}
```

### 5.4 数据库测试交接 `handoffs`

职责：交给测试团队验证的交接单，独立保留（方案 `handoffs`，由 `database_test_team_handoffs` 原样保留）。

**端点**

| 方法与路径 | 用途 | 幂等 | 权限 |
| --- | --- | --- | --- |
| `GET /api/tasks/{taskId}/handoffs` | 列表 | 天然 | 成员 |
| `POST /api/tasks/{taskId}/handoffs` | 建立交接：`branchValidationKey`、`payload`；成功后任务置 `handed_off` | 自然键 | 后台（代 Manager） |
| `GET /api/handoffs/{handoffId}` | 读取 | 天然 | 成员 |
| `POST /api/handoffs/{handoffId}/status` | 写 `status`，追加 `evidence` | 键（无落点） | 后台（代 `db-test-team`）或组织管理员 |

**字段**

| 方案字段 | JSON 字段 | 读写 | 说明 |
| --- | --- | --- | --- |
| `id` | `id` | 读 | UUID。 |
| `task_id` | `taskId` | 读 | 路径参数。 |
| `branch_validation_key` | `branchValidationKey` | 创建时写 | 分支验证键，唯一；自然幂等键（2.5）。 |
| `payload` | `payload` | 创建时写 | 交接材料，JSON。 |
| `evidence` | `evidence` | 动作写 | 验证证据，JSON 数组，创建时为 `[]`；`status` 动作只追加，不删除。 |
| `status` | `status` | 动作写 | 交接状态，见附录 D。 |
| `created_at` | `createdAt` | 读 | 创建时间。 |

**规则**

- 只允许 `submitted` 到 `validating`、`validating` 到 `passed` 或 `failed`、`submitted` 或 `validating` 到 `withdrawn`；其余 409 `INVALID_TRANSITION`。
- `passed` 不自动完成任务；任务结束由 `complete` 另行判断（ADR-0015）。数据库分支验证的过程记录在 `database_branch_validations`（7.6）。

## 6. 协作与上下文（5 张表）

方向职责：怎么说话、喂什么材料、谁读过什么（方案 ④）。方案没有会话（conversation）实体，只有 `messages.room_id` 字符串；ADR-0019 与已采用的消息契约要求会话资源，见附录 E 第 3 条。

### 6.1 消息 `messages`

职责：人与 Agent、Agent 之间交流的单条消息；时间线冗余表删除（方案 `messages`，`room_timeline_messages` 删除）。

**端点**

| 方法与路径 | 用途 | 幂等 | 权限 |
| --- | --- | --- | --- |
| `GET /api/messages` | 列表；必填过滤之一：`roomId`、`taskId`、`projectId`；可选 `correlationId` | 天然 | 成员 |
| `POST /api/messages` | 保存人发的消息：字段清单见字段表 | 键（无落点） | 成员 |
| `GET /api/messages/{messageId}` | 读取 | 天然 | 成员 |

**字段**

| 方案字段 | JSON 字段 | 读写 | 说明 |
| --- | --- | --- | --- |
| `id` | `id` | 读 | UUID。 |
| `organization_id` | `organizationId` | 读 | 由会话解析。 |
| `project_id` | `projectId` | 创建时写 | 所属项目。 |
| `task_id` | `taskId` | 创建时写 | 关联任务，可空。 |
| `sender_type` | `senderType` | 读 | 方案值 `agent`、`human`；浏览器创建固定为 `human`。 |
| `sender_agent_id` | `senderAgentId` | 读 | Agent 消息时由后台写。 |
| `sender_user_id` | `senderUserId` | 读 | 人工消息时取会话用户。 |
| `recipient_agent_id` | `recipientAgentId` | 创建时写 | 接收方，可空；空表示发给项目 Manager。 |
| `kind` | `kind` | 创建时写 | 消息类型，见附录 D。 |
| `subject` | `subject` | 创建时写 | 主题，必填；与消息契约 §2 的单字段 `content` 不一致，见附录 E 写作中发现第 8 条。 |
| `body` | `body` | 创建时写 | 正文，最多 20,000 个 Unicode 标量（消息契约 §2）。 |
| `room_id` | `roomId` | 创建时写 | 房间，必填；首条消息时房间可能尚未准备，见附录 E 写作中发现第 8 条。 |
| `status` | `status` | 读 | 见附录 D；后台按投递结果写。 |
| `correlation_id` | `correlationId` | 创建时写 | 关联链，可空；省略时服务端生成。 |

**规则**

- 浏览器创建的消息 `sender_type=human`，`sender_user_id` 取会话用户；保存成功返回 201 与消息快照，投递异步（2.7）。
- 保存成功不等于 Manager 已接收（ADR-0018）；`status` 由后台按实际投递结果写回，`saved` 只表示已落库。
- 首条会话消息持久提交后由后台异步准备或恢复实例与房间（ADR-0018）；消息端点不等待准备完成。
- 实时更新走 `GET /api/events/stream` 过滤 `aggregateType=message`；消息端点没有自己的 SSE。
- 消息契约 §2 的 `replyTo` 与澄清（clarification）在方案中没有落点，见附录 C 与附录 E 第 3 条。

**示例**：`POST /api/messages` 请求体。

```json
{
	"projectId": "3c9e6679-7425-40de-944b-e07fc1f90ae7",
	"roomId": "!orders-main:repomesh.local",
	"taskId": "6f1d4c0e-2b7a-4a7e-9c1e-1c2f3a4b5d6e",
	"recipientAgentId": null,
	"kind": "instruction",
	"subject": "订单归档范围",
	"body": "只归档 2024 年以前的已完成订单。",
	"correlationId": null
}
```

### 6.2 上下文材料 `context_objects`

职责：可喂给 Agent 的背景材料登记、当前内容与版本史（方案 `context_objects`，由 `context_objects`、`context_object_versions`、`handoff_docs` 并入）。

**端点**

| 方法与路径 | 用途 | 幂等 | 权限 |
| --- | --- | --- | --- |
| `GET /api/projects/{projectId}/context-objects` | 列表；过滤 `kind`、`repositoryId` | 天然 | 成员 |
| `POST /api/projects/{projectId}/context-objects` | 登记材料：`repositoryId`、`kind`、`name`、`content`、`sourceRef`；`version_no=1` | 键（无落点） | 成员 |
| `GET /api/context-objects/{objectId}` | 读取当前内容 | 天然 | 成员 |
| `PUT /api/context-objects/{objectId}/content` | 替换内容：`content`；`version_no` 加一，旧内容追加到 `version_history` | 天然 | 成员 |
| `GET /api/context-objects/{objectId}/versions` | 读取 `version_history` 与当前版本 | 天然 | 成员 |

**字段**

| 方案字段 | JSON 字段 | 读写 | 说明 |
| --- | --- | --- | --- |
| `id` | `id` | 读 | UUID。 |
| `project_id` | `projectId` | 读 | 路径参数。 |
| `repository_id` | `repositoryId` | 创建时写 | 来源仓库，可空。 |
| `kind` | `kind` | 创建时写 | 材料类型，见附录 D。 |
| `name` | `name` | 读写 | 名称。 |
| `content` | `content` | 读 | 当前内容；只经 `PUT .../content` 改变。 |
| `version_no` | `versionNo` | 读 | 当前版本号，从 1 起。 |
| `version_history` | `versionHistory` | 读 | 版本史，JSON 数组，每项 `{versionNo, content, replacedAt, replacedBy}`；只在 `versions` 端点返回。 |
| `source_ref` | `sourceRef` | 创建时写 | 来源或派生关系，JSON。 |
| `created_at` | `createdAt` | 读 | 创建时间。 |
| `updated_at` | `updatedAt` | 读 | 更新时间。 |

**规则**

- `PUT .../content` 无并发保护；`version_no` 可作乐观锁但不在 2.5 约定内，见附录 E 写作中发现第 17 条。
- 本表没有 PATCH 端点；`name` 的修改随 `PUT .../content` 一起提交时可选。

### 6.3 上下文包 `context_bundles`

职责：派活时打包发给 Agent 的材料包，包内清单并入（方案 `context_bundles`，由 `context_bundles`、`context_bundle_items` 并入）。

**端点**

| 方法与路径 | 用途 | 幂等 | 权限 |
| --- | --- | --- | --- |
| `GET /api/task-assignments/{assignmentId}/context-bundles` | 列表 | 天然 | 成员 |
| `POST /api/task-assignments/{assignmentId}/context-bundles` | 打包：`name`、`items`，每项含材料 `objectId` 与 `versionNo` 快照 | 键（无落点） | 后台（代 Manager） |
| `GET /api/context-bundles/{bundleId}` | 读取 | 天然 | 成员 |

**字段**

| 方案字段 | JSON 字段 | 读写 | 说明 |
| --- | --- | --- | --- |
| `id` | `id` | 读 | UUID。 |
| `assignment_id` | `assignmentId` | 读 | 路径参数，随哪次派单。 |
| `name` | `name` | 创建时写 | 包名。 |
| `items` | `items` | 创建时写 | 包内材料及版本快照，JSON 数组，每项 `{objectId, versionNo}`；`versionNo` 必须存在于该材料的当前版本或 `version_history`。 |
| `created_at` | `createdAt` | 读 | 创建时间。 |

**规则**

- 包内容不随材料后续更新改变；更新经 `context_deltas` 同步（6.4）。

### 6.4 上下文增量 `context_deltas`

职责：材料更新时的差量，明细并入（方案 `context_deltas`，由 `context_deltas`、`context_delta_items` 并入）。

**端点**

| 方法与路径 | 用途 | 幂等 | 权限 |
| --- | --- | --- | --- |
| `GET /api/context-objects/{objectId}/deltas` | 列表 | 天然 | 成员 |
| `POST /api/context-objects/{objectId}/deltas` | 登记差量：`baseVersion`、`changes` | 键（无落点） | 后台（代 Manager） |
| `POST /api/context-deltas/{deltaId}/sync` | 记录同步：`agentIds` 追加到 `synced_to` | 键（无落点） | 后台 |

**字段**

| 方案字段 | JSON 字段 | 读写 | 说明 |
| --- | --- | --- | --- |
| `id` | `id` | 读 | UUID。 |
| `object_id` | `objectId` | 读 | 路径参数，针对哪份材料。 |
| `base_version` | `baseVersion` | 创建时写 | 基于哪个 `version_no`。 |
| `changes` | `changes` | 创建时写 | 变化内容，JSON。 |
| `synced_to` | `syncedTo` | 动作写 | 已同步给谁，Agent ID 数组；`sync` 只追加。 |
| `created_at` | `createdAt` | 读 | 创建时间。 |

**规则**

- `baseVersion` 大于材料当前 `version_no` 时返回 422 `VALIDATION_FAILED`。
- `sync` 记录的是后台已完成的投递事实，不触发投递。

### 6.5 访问记录 `context_access_events`

职责：谁在什么时候读过哪份材料，审计与热度（方案 `context_access_events`，原样保留）。

**端点**

| 方法与路径 | 用途 | 幂等 | 权限 |
| --- | --- | --- | --- |
| `GET /api/context-objects/{objectId}/access-events` | 列表；过滤 `readerAgentId`、`from`、`to` | 天然 | 成员 |

**字段**

| 方案字段 | JSON 字段 | 读写 | 说明 |
| --- | --- | --- | --- |
| `id` | `id` | 读 | UUID。 |
| `object_id` | `objectId` | 读 | 路径参数。 |
| `reader_agent_id` | `readerAgentId` | 读 | 读者。 |
| `read_at` | `readAt` | 读 | 读取时间。 |

**规则**

- 只读端点；由后台协调进程在 Agent 读取材料时写入。附录 A 标“内部写入，只读端点”。

## 7. 交付与验证（6 张表）

方向职责：代码怎么交付回去、验没验过、门禁策略；数据库分支验证单独成表（方案 ⑤）。只允许 draft PR，合并由人（ADR-0002、ADR-0006 §2）。

### 7.1 变更集 `change_sets`

职责：一组代码改动，对应一个 PR；冲突、归档、Issue 归档全部字段化（方案 `change_sets`，由 `change_sets`、`change_set_repositories`、`conflict_cases`、`delivery_archives`、`issue_archives` 并入）。

**端点**

| 方法与路径 | 用途 | 幂等 | 权限 |
| --- | --- | --- | --- |
| `GET /api/tasks/{taskId}/change-sets` | 列表 | 天然 | 成员 |
| `POST /api/tasks/{taskId}/change-sets` | 建立主变更集：`repositoryIds`、`branch`、`payload`；顶层任务正式提交时已同事务建立，此处重放复用（ADR-0008） | 键 | 后台（代 Manager）或成员 |
| `GET /api/change-sets/{changeSetId}` | 读取 | 天然 | 成员 |
| `PATCH /api/change-sets/{changeSetId}` | 修改 `repositoryIds` 条目、`branch`、`payload`；需 `expectedVersion` | 版本 | 后台（代 Manager） |
| `POST /api/change-sets/{changeSetId}/deliver` | 交付：`expectedVersion`；首次生成 `scm_commands`（建分支、开 draft PR），再次生成推送分支、更新 PR 命令；异步（201） | 键（无落点）、版本 | 后台（代 Manager） |
| `POST /api/change-sets/{changeSetId}/conflict` | 记录冲突：`expectedVersion`、`conflictKind`、`conflictStatus`、`conflictDetail` | 键（无落点）、版本 | 后台 |
| `POST /api/change-sets/{changeSetId}/archive` | 归档：`expectedVersion`；`status` 置 `archived` 终态 | 键（无落点）、版本 | 成员 |

**字段**

| 方案字段 | JSON 字段 | 读写 | 说明 |
| --- | --- | --- | --- |
| `id` | `id` | 读 | UUID。 |
| `organization_id` | `organizationId` | 读 | 由会话解析。 |
| `task_id` | `taskId` | 读 | 来源任务，路径参数；方案可空，本文只经 `/tasks/{taskId}` 创建，无任务的变更集不在本文范围。 |
| `repository_ids` | `repositoryIds` | 读写 | 涉及仓库，JSON 数组，每仓一份 `{repositoryId, commitSha, prNumber, deliveryStatus}`，键名见附录 D。 |
| `status` | `status` | 动作写 | 状态机，见附录 D；含 `archived` 终态。 |
| `pr_url` | `prUrl` | 读 | PR 链接，后台按 `scm_commands.result` 写；多仓时只放一个，见附录 E 写作中发现第 19 条。 |
| `branch` | `branch` | 读写 | 分支名。 |
| `conflict_kind` | `conflictKind` | 动作写 | 冲突类型，见附录 D。 |
| `conflict_status` | `conflictStatus` | 动作写 | 冲突处理状态，见附录 D。 |
| `conflict_detail` | `conflictDetail` | 动作写 | 冲突细节与修复任务引用，JSON。 |
| `payload` | `payload` | 读写 | 变更载荷，JSON。 |
| `version` | `version` | 读 | 乐观锁版本（2.5）。 |
| `idempotency_key` | `idempotencyKey` | 读 | 值来自 `Idempotency-Key` 头。 |

**状态机**（值为附录 D 提案）

| 当前状态 | 目标状态 | 触发 |
| --- | --- | --- |
| `open` | `delivering` | `deliver`；每个 `repositoryIds` 条目生成 `create_branch` 与 `open_draft_pr` 两条 `scm_commands`。 |
| `delivered` | `delivering` | 再次 `deliver`；每个条目生成 `push_branch` 与 `update_pr` 两条 `scm_commands`。 |
| `delivering` | `delivered` | 后台：全部命令 `succeeded`，`pr_url` 与各条目 `prNumber` 写回。 |
| `delivering` | `open` 或 `delivered` | 后台：任一命令 `failed` 且不重试。任一 `repositoryIds` 条目的 `deliveryStatus` 已是 `draft_pr_opened` 时回到 `delivered`，否则回到 `open`。 |
| `open`、`delivering`、`delivered` | `conflicted` | `conflict` 且 `conflictStatus` 为 `detected` 或 `fix_task_created`。 |
| `conflicted` | `open` | `conflict` 且 `conflictStatus` 为 `resolved`。 |
| `conflicted` | `conflicted` | `conflict` 且 `conflictStatus` 为 `abandoned`；`status` 保持 `conflicted`，之后只能 `archive`。 |
| 任意非终态 | `archived` | `archive`；终态。 |

**规则**

- 一个顶层任务只有一个主变更集（ADR-0008 首期无嵌套）；`POST .../change-sets` 已存在时返回 200 与已有行。修复轮次继续原变更集，新增范围用新任务与新变更集。
- `deliver` 只生成 draft PR 命令；draft 转正式 PR 与合并由人和仓库规则控制（ADR-0002、ADR-0006 §2）。命令的执行与结果见 7.2、7.3。
- ADR-0008 的 Candidate、Delivery Combination 在方案中只剩 `repository_ids` 每仓一份条目；收录、接受、入选、验证通过、可开 PR 五个事实无独立列，见附录 E 第 6 条。
- `archived` 后任何写操作 409 `INVALID_TRANSITION`；未合并 PR 的取消留在原变更集（ADR-0008）。

**示例**：`POST /api/change-sets/{changeSetId}/deliver` 的 201 响应。

```json
{
	"id": "a3d5e7f9-1b2c-4d4e-8f6a-0b1c2d3e4f5a",
	"organizationId": "0a1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d",
	"taskId": "6f1d4c0e-2b7a-4a7e-9c1e-1c2f3a4b5d6e",
	"repositoryIds": [
		{
			"repositoryId": "c56a4180-65aa-42ec-a945-5fd21dec0538",
			"commitSha": "9fceb02d0ae598e95dc970b74767f19372d61af8",
			"prNumber": null,
			"deliveryStatus": "pending"
		}
	],
	"status": "delivering",
	"prUrl": null,
	"branch": "repomesh/task-6f1d4c0e",
	"conflictKind": null,
	"conflictStatus": null,
	"conflictDetail": null,
	"payload": {
		"summary": "归档历史订单"
	},
	"version": 3,
	"idempotencyKey": "5e0d3c2b-1a9f-4e8d-b7c6-a5b4c3d2e1f0"
}
```

### 7.2 SCM 命令 `scm_commands`

职责：发给托管平台的每条命令（建分支、开 PR）及结果（方案 `scm_commands`，原样保留）。

**端点**

| 方法与路径 | 用途 | 幂等 | 权限 |
| --- | --- | --- | --- |
| `GET /api/change-sets/{changeSetId}/scm-commands` | 列表；过滤 `status`、`commandType` | 天然 | 成员 |
| `GET /api/scm-commands/{commandId}` | 读取 | 天然 | 成员 |
| `POST /api/scm-commands/{commandId}/retry` | 重试，异步（201）；未知结果先核查再重试（ADR-0016） | 键（无落点） | 项目管理员 |

**字段**

| 方案字段 | JSON 字段 | 读写 | 说明 |
| --- | --- | --- | --- |
| `id` | `id` | 读 | UUID。 |
| `change_set_id` | `changeSetId` | 读 | 路径参数。 |
| `command_type` | `commandType` | 读 | 命令类型，见附录 D。 |
| `params` | `params` | 读 | 参数，JSON；不含凭据。 |
| `result` | `result` | 读 | 结果，JSON，后台写。 |
| `retry_count` | `retryCount` | 读 | 重试次数，`retry` 成功登记后加一。 |
| `status` | `status` | 读 | 见附录 D。 |

**规则**

- 只有 `failed`、`unknown` 允许 `retry`；`unknown` 时后台先核查托管平台实际状态，已成功则改写 `status=succeeded` 不重发（ADR-0016）。其余状态 409 `INVALID_TRANSITION`。
- 写入方只有 `deliver` 与后台；浏览器不直接创建命令。

### 7.3 SCM 观察 `scm_observations`

职责：从托管平台轮询回来的状态，合并没、CI 过没（方案 `scm_observations`，原样保留）。

**端点**

| 方法与路径 | 用途 | 幂等 | 权限 |
| --- | --- | --- | --- |
| `GET /api/change-sets/{changeSetId}/scm-observations` | 列表；过滤 `repositoryId`、`consumed`、`from`、`to` | 天然 | 成员 |

**字段**

| 方案字段 | JSON 字段 | 读写 | 说明 |
| --- | --- | --- | --- |
| `id` | `id` | 读 | UUID。 |
| `change_set_id` | `changeSetId` | 读 | 路径参数。 |
| `repository_id` | `repositoryId` | 读 | 哪个仓库。 |
| `observation` | `observation` | 读 | 观察内容，JSON。 |
| `consumed` | `consumed` | 读 | 是否已被后台消费为业务状态变化。 |
| `observed_at` | `observedAt` | 读 | 观察时间。 |

**规则**

- 只读；轮询由后台按 `repositories.poll_next_at` 执行，`consumed` 由后台写。观察是有时间的事实，不是持续授权（首批契约 §3.1）。

### 7.4 交付策略 `delivery_policies`

职责：什么条件下允许交付，审核门禁、流水线要求，运行时按项目读取（方案 `delivery_policies`，原样保留）。

**端点**

| 方法与路径 | 用途 | 幂等 | 权限 |
| --- | --- | --- | --- |
| `GET /api/projects/{projectId}/delivery-policy` | 读取，一项目一条 | 天然 | 成员 |
| `PUT /api/projects/{projectId}/delivery-policy` | 写入或覆盖：`policy` | 天然 | 项目管理员 |

**字段**

| 方案字段 | JSON 字段 | 读写 | 说明 |
| --- | --- | --- | --- |
| `id` | `id` | 读 | UUID。 |
| `project_id` | `projectId` | 读 | 路径参数，唯一。 |
| `policy` | `policy` | 读写 | 策略内容，JSON。 |
| `updated_at` | `updatedAt` | 读 | 更新时间。 |

**规则**

- 没有策略时 GET 返回 404 `NOT_FOUND`；`PUT` 首次返回 201，之后 200。
- `policy` 不能授予合并权或扩大 Git 权限（ADR-0002）；`deliver` 在后台执行前读取本表。

### 7.5 验证快照 `validation_snapshots`

职责：一次验证动作的完整结果存档，通用验证（方案 `validation_snapshots`，改造保留）。

**端点**

| 方法与路径 | 用途 | 幂等 | 权限 |
| --- | --- | --- | --- |
| `GET /api/projects/{projectId}/validation-snapshots` | 列表；过滤 `objectId`、`status` | 天然 | 成员 |
| `GET /api/validation-snapshots/{snapshotId}` | 读取 | 天然 | 成员 |

**字段**

| 方案字段 | JSON 字段 | 读写 | 说明 |
| --- | --- | --- | --- |
| `id` | `id` | 读 | UUID。 |
| `organization_id` | `organizationId` | 读 | 组织。 |
| `project_id` | `projectId` | 读 | 路径参数。 |
| `object_id` | `objectId` | 读 | 验证对象，可空；没有对象类型列，见附录 E 写作中发现第 15 条。 |
| `environment_hash` | `environmentHash` | 读 | 环境指纹。 |
| `status` | `status` | 读 | 方案给通过、失败；本文补 `undetermined`、`blocked`，见附录 D（ADR-0007 D2）。 |
| `payload` | `payload` | 读 | 完整结果，JSON。 |
| `expires_at` | `expiresAt` | 读 | 过期时间；过期后证据适用性需重判（ADR-0008）。 |

**规则**

- 写入方是后台验证流程。ADR-0005 §3 的初判与复核分别记录为两行，`payload.role` 区分 `worker` 与 `coordinator`。
- 验证通过与任务结束分别判断（ADR-0015）；本表 `passed` 不改变 `tasks.status`。

### 7.6 数据库分支验证 `database_branch_validations`

职责：为候选提交建数据库分支、跑验证（方案 `database_branch_validations`，原样保留）。

**端点**

| 方法与路径 | 用途 | 幂等 | 权限 |
| --- | --- | --- | --- |
| `GET /api/projects/{projectId}/database-branch-validations` | 列表；过滤 `repositoryId`、`candidateSha`、`status` | 天然 | 成员 |
| `POST /api/projects/{projectId}/database-branch-validations` | 发起：`repositoryId`、`taskId`、`candidateSha`、`sourceDatabaseRef`、`provider`；异步（201） | 键（无落点） | 后台（代 `db-test-team`）或成员 |
| `GET /api/database-branch-validations/{validationId}` | 读取 | 天然 | 成员 |
| `POST /api/database-branch-validations/{validationId}/cleanup` | 置 `cleanup_pending=true`，异步（201）；实际清理由后台执行 | 键（无落点） | 成员 |

**字段**

| 方案字段 | JSON 字段 | 读写 | 说明 |
| --- | --- | --- | --- |
| `id` | `id` | 读 | UUID。 |
| `organization_id` | `organizationId` | 读 | 组织。 |
| `project_id` | `projectId` | 读 | 路径参数。 |
| `repository_id` | `repositoryId` | 创建时写 | 所属仓库。 |
| `task_id` | `taskId` | 创建时写 | 关联任务，可空；同仓库可复验。 |
| `candidate_sha` | `candidateSha` | 创建时写 | 被验提交。 |
| `source_database_ref` | `sourceDatabaseRef` | 创建时写 | 来源数据库。 |
| `provider` | `provider` | 创建时写 | 分支提供方。 |
| `provider_branch_ref` | `providerBranchRef` | 读 | 提供方分支标识，后台建分支后写；创建时未知，见附录 E 写作中发现第 11 条。 |
| `engine_version` | `engineVersion` | 读 | 引擎版本，后台写。 |
| `status` | `status` | 读 | 见附录 D。 |
| `failure_code` | `failureCode` | 读 | 失败码。 |
| `cleanup_pending` | `cleanupPending` | 动作写 | 待清理标记，创建时为 `false`；`cleanup` 置 `true`，后台清理完成后写 `status=cleaned_up`。 |
| `results` | `results` | 读 | 验证结果，JSON；创建时为 `{}`，后台写。 |

**规则**

- 停止或清理请求与实际停止分开（ADR-0013）：`cleanup` 返回 201 只表示已登记。
- 建分支、跑验证由后台经受限主机执行边界执行（ADR-0013）；Web 不持有 Docker socket。

## 8. Skill 体系（7 张表）

状态说明：方案 ⑥ 新增 7 张表，本节据此给出接口设计。ADR-0009 要求恢复前不冻结表、API 与状态枚举，因此本节全部内容（路径、字段、枚举）都是随方案给出的设计，不是采用记录，ADR-0009 更新前不得实施。ADR-0009 H1、H11 要求 Issue 引用确定版本，方案改为绑 Agent（`agent_skill_bindings`），差异记入附录 E 第 7 条。**2026-09-16 现实更新**：技能治理已实现并合入 main（`internal/skills` + 迁移 0013，22 条端点已登记附录 C）——本节设计与之存在出入，绑定粒度等仍按附录 E 第 7 条待裁定，字段细节以实现为准。

方向职责：出题、AB 评估、盲选、逐级审核、绑定、沉淀（方案 ⑥）。Skill 绑 Agent 不绑 Issue（方案 `agent_skill_bindings`）。

### 8.1 Skill 主档 `skills`

职责：使用场景、给谁用；场景必填，“给谁用”是选择题（方案 `skills`，新表）。

**端点**

| 方法与路径 | 用途 | 幂等 | 权限 |
| --- | --- | --- | --- |
| `GET /api/skills` | 列表；过滤 `targetAgentRole` | 天然 | 成员 |
| `POST /api/skills` | 建立：`name`、`scenario`、`targetAgentRole` | 键（无落点） | 成员 |
| `GET /api/skills/{skillId}` | 读取 | 天然 | 成员 |
| `PATCH /api/skills/{skillId}` | 修改 `scenario`、`targetAgentRole` | 天然 | 成员 |

**字段**

| 方案字段 | JSON 字段 | 读写 | 说明 |
| --- | --- | --- | --- |
| `id` | `id` | 读 | UUID。 |
| `name` | `name` | 创建时写 | 技能名，唯一；冲突 422 `DUPLICATE`。 |
| `scenario` | `scenario` | 读写 | 使用场景，必填。 |
| `target_agent_role` | `targetAgentRole` | 读写 | 目标 Agent 角色，取 `agents.role` 的值。 |
| `created_by` | `createdBy` | 读 | 服务端写 `user:<userId>` 或 `agent:<agentId>`（2.2）。 |
| `created_at` | `createdAt` | 读 | 创建时间。 |

**规则**

- 方案没有 `organization_id`，Skill 主档按平台级共享，见附录 E 写作中发现第 5 条。ADR-0009 H3 的平台、项目、仓库归属没有列。

### 8.2 Skill 版本 `skill_versions`

职责：每次发布有版本号、状态机、SKILL.md 全文、内容指纹（方案 `skill_versions`，改造：加全文、去 `local_path`）。

**端点**

| 方法与路径 | 用途 | 幂等 | 权限 |
| --- | --- | --- | --- |
| `GET /api/skills/{skillId}/versions` | 列表；过滤 `status` | 天然 | 成员 |
| `POST /api/skills/{skillId}/versions` | 新建版本：`version`、`content`；服务端算 `content_hash`，`status=draft` | 键（无落点） | 成员 |
| `GET /api/skill-versions/{versionId}` | 读取 | 天然 | 成员 |
| `POST /api/skill-versions/{versionId}/status` | `action` 取 `start_evaluation` 或 `deprecate`；发布只能由 `skill_approvals` 结论触发 | 键（无落点） | 成员 |

**字段**

| 方案字段 | JSON 字段 | 读写 | 说明 |
| --- | --- | --- | --- |
| `id` | `id` | 读 | UUID。 |
| `skill_id` | `skillId` | 读 | 路径参数。 |
| `version` | `version` | 创建时写 | 版本号字符串；同技能内唯一为本文约束（提案）。 |
| `status` | `status` | 动作写 | 方案描述“草稿/评估中/已发布/废弃”，本文令牌 `draft`、`evaluating`、`published`、`deprecated`（附录 D）。 |
| `content` | `content` | 创建时写 | SKILL.md 全文。 |
| `content_hash` | `contentHash` | 读 | 内容指纹，服务端按 SHA-256 计算。 |
| `created_by` | `createdBy` | 读 | 服务端写操作者字符串（2.2）。 |

**规则**

- 迁移：`start_evaluation` 使 `draft` 到 `evaluating`；`deprecate` 使 `draft`、`evaluating` 或 `published` 到 `deprecated`；`published` 只由 `POST /api/skill-approvals/{approvalId}/decision` 通过后写入；审批驳回时服务端把版本从 `evaluating` 置 `deprecated`，这是方案 4 个状态值下的处理，驳回结论保存在 `skill_approvals.conclusion`。其余 409 `INVALID_TRANSITION`。
- 已发布版本内容不改，改内容新建版本；ADR-0009 H10 的项目默认与在途 Issue 沿用原版在方案中无落点。

### 8.3 测试题 `skill_test_questions`

职责：Manager 出题与人工共同确定，三类题（方案 `skill_test_questions`，新表）。

**端点**

| 方法与路径 | 用途 | 幂等 | 权限 |
| --- | --- | --- | --- |
| `GET /api/skills/{skillId}/test-questions` | 列表；过滤 `kind` | 天然 | 成员 |
| `POST /api/skills/{skillId}/test-questions` | 出题：`kind`、`question`、`expected`、`providedBy` | 键（无落点） | 成员或后台（代 Manager、Leader） |

**字段**

| 方案字段 | JSON 字段 | 读写 | 说明 |
| --- | --- | --- | --- |
| `id` | `id` | 读 | UUID。 |
| `skill_id` | `skillId` | 读 | 路径参数。 |
| `kind` | `kind` | 创建时写 | 题型 `success`、`business_failure`、`missing_precondition`，见附录 D（ADR-0009 H6）。 |
| `question` | `question` | 创建时写 | 题面。 |
| `expected` | `expected` | 创建时写 | 标准答案或预期，JSON。 |
| `provided_by` | `providedBy` | 创建时写 | 出题方，方案值 `manager`、`leader`、`human`。 |
| `created_at` | `createdAt` | 读 | 创建时间。 |

**规则**

- 题目不提供修改与删除端点；题面有误新出一题。

### 8.4 评估运行 `skill_evaluation_runs`

职责：AB 对照（带、不带 Skill）加抹标签盲选记录（方案 `skill_evaluation_runs`，新表）。

**端点**

| 方法与路径 | 用途 | 幂等 | 权限 |
| --- | --- | --- | --- |
| `GET /api/skill-versions/{versionId}/evaluation-runs` | 列表；过滤 `questionId`、`result` | 天然 | 成员 |
| `POST /api/skill-versions/{versionId}/evaluation-runs` | 发起：`questionIds`；服务端为每题跑 `with` 与 `without` 两臂并生成 `blinded_label`，异步（201） | 键（无落点） | 成员 |
| `POST /api/skill-evaluation-runs/{runId}/judgement` | 盲选判定：`result`；`judged_by` 取会话或 Agent | 键（无落点） | 成员或后台 |

**字段**

| 方案字段 | JSON 字段 | 读写 | 说明 |
| --- | --- | --- | --- |
| `id` | `id` | 读 | UUID。 |
| `version_id` | `versionId` | 读 | 路径参数，被评版本。 |
| `question_id` | `questionId` | 读 | 用哪道题；由 `questionIds` 展开。 |
| `arm` | `arm` | 读 | `with`、`without`，见附录 D。 |
| `blinded_label` | `blindedLabel` | 读 | 盲选标签，服务端随机生成；判定前响应不返回 `arm`。 |
| `answer` | `answer` | 读 | 作答内容，JSON；创建时为 `{}`，作答落库后由后台覆盖。 |
| `judged_by` | `judgedBy` | 动作写 | 服务端写操作者字符串（2.2）。 |
| `result` | `result` | 动作写 | 单题结论，见附录 D。 |
| `run_at` | `runAt` | 读 | 运行时间。 |

**规则**

- `POST .../evaluation-runs` 的 201 响应返回 `items`，每题两行。版本必须处于 `evaluating`，否则 409 `INVALID_TRANSITION`。
- 判定前 `arm` 对判定者隐藏；`judgement` 成功后才在响应中返回 `arm`。

### 8.5 逐级审核 `skill_approvals`

职责：谁的 Skill 谁的上级审，Worker 的 Manager 审、Manager 的 Leader 审、Leader 的人工审，异步不阻塞（方案 `skill_approvals`，新表）。

**端点**

| 方法与路径 | 用途 | 幂等 | 权限 |
| --- | --- | --- | --- |
| `GET /api/skill-versions/{versionId}/approval` | 读取该版本唯一的审核记录 | 天然 | 成员 |
| `POST /api/skill-versions/{versionId}/approval` | 发起审核：`subjectRole`；服务端推导 `reviewer_kind`，审自己的产出时 `recused=true` 并上移 | 自然键 | 成员或后台 |
| `POST /api/skill-approvals/{approvalId}/decision` | 批复：`reviewStatus`、`note`、`conclusion`；通过后服务端把版本置 `published` | 键（无落点） | `reviewer_kind=human` 时成员；其余后台 |

**字段**

| 方案字段 | JSON 字段 | 读写 | 说明 |
| --- | --- | --- | --- |
| `id` | `id` | 读 | UUID。 |
| `version_id` | `versionId` | 读 | 路径参数，唯一；一版本一条。 |
| `subject_role` | `subjectRole` | 创建时写 | 被审 Skill 归属角色 `worker`、`manager`、`leader`（方案）。 |
| `reviewer_kind` | `reviewerKind` | 读 | 审核方 `manager`、`leader`、`human`（方案）；`worker` 由 `manager` 审，`manager` 由 `leader` 审，`leader` 由 `human` 审。 |
| `reviewer_user_id` | `reviewerUserId` | 读 | 人工审核时取会话用户。 |
| `review_status` | `reviewStatus` | 动作写 | 见附录 D。 |
| `reviewed_at` | `reviewedAt` | 读 | 审核时间。 |
| `review_note` | `reviewNote` | 动作写 | 审核意见。 |
| `recused` | `recused` | 读 | 审核方是否回避。服务端把 `skill_versions.created_by` 解析成 `user:` 或 `agent:` 身份，与审核方身份（`reviewer_user_id`，或按 `reviewer_kind` 推导出的上级 Agent ID）比较；相同时为 `true`，`reviewer_kind` 上移一级。 |
| `conclusion` | `conclusion` | 动作写 | 最终结论，见附录 D。 |
| `decided_at` | `decidedAt` | 读 | 定论时间。 |

**规则**

- `version_id` 唯一：重复 `POST .../approval` 在 `pending` 时返回 200 与已有行；已定论时 409 `INVALID_TRANSITION`。驳回后同一版本不能再次发起，需新建版本，见附录 E 写作中发现第 10 条。
- `decision` 只允许 `pending` 到 `approved` 或 `rejected`。`approved` 且 `conclusion=publish` 时服务端把 `skill_versions.status` 从 `evaluating` 置 `published`；`rejected` 时服务端把版本置 `deprecated`，这是方案 4 个状态值下的处理，结论保存在 `conclusion`（8.2）；版本不在 `evaluating` 时 409。
- 审核异步，不阻塞任何任务（方案；ADR-0009 H2）。
- 角色名沿用方案的 leader 为总、manager 为仓库，见附录 E 第 1 条。

**示例**：`GET /api/skill-versions/{versionId}/approval` 回避上移后的快照。

```json
{
	"id": "c4e6a8b0-2d1f-4e3a-9b5c-7d8e9f0a1b2c",
	"versionId": "d5f7b9c1-3e2a-4f4b-8c6d-9e0f1a2b3c4d",
	"subjectRole": "manager",
	"reviewerKind": "human",
	"reviewerUserId": null,
	"reviewStatus": "pending",
	"reviewedAt": null,
	"reviewNote": null,
	"recused": true,
	"conclusion": null,
	"decidedAt": null
}
```

### 8.6 绑定 `agent_skill_bindings`

职责：哪个 Agent 挂载哪个版本；绑 Agent 不绑 Issue（方案 `agent_skill_bindings`，替代 `skill_snapshots`）。

**端点**

| 方法与路径 | 用途 | 幂等 | 权限 |
| --- | --- | --- | --- |
| `GET /api/agents/{agentId}/skill-bindings` | 列表；过滤 `active` | 天然 | 成员 |
| `POST /api/agents/{agentId}/skill-bindings` | 绑定：`versionId`、`source` | 键（无落点） | 组织管理员或后台 |
| `DELETE /api/agent-skill-bindings/{bindingId}` | 解绑：置 `active=false`，不删行 | 天然 | 组织管理员或后台 |

**字段**

| 方案字段 | JSON 字段 | 读写 | 说明 |
| --- | --- | --- | --- |
| `id` | `id` | 读 | UUID。 |
| `agent_id` | `agentId` | 读 | 路径参数。 |
| `version_id` | `versionId` | 创建时写 | 挂载版本；必须为 `published`，否则 409 `INVALID_TRANSITION`。 |
| `source` | `source` | 创建时写 | 来源，见附录 D。 |
| `bound_at` | `boundAt` | 读 | 绑定时间。 |
| `active` | `active` | 动作写 | 是否生效；`DELETE` 置 `false`。 |

**规则**

- 解绑保留历史行；重新绑定新建行。绑定成功不表示运行时已加载（ADR-0009 H12）。
- `DELETE` 返回 200 与更新后的行，不返回 204，因为行仍存在。

### 8.7 沉淀建议 `skill_update_suggestions`

职责：任务结束后提更新意见，进入异步审批队列（方案 `skill_update_suggestions`，新表）。

**端点**

| 方法与路径 | 用途 | 幂等 | 权限 |
| --- | --- | --- | --- |
| `GET /api/skills/{skillId}/update-suggestions` | 列表；过滤 `status` | 天然 | 成员 |
| `POST /api/skills/{skillId}/update-suggestions` | 提建议：`taskId`、`suggestion` | 键（无落点） | 成员或后台 |
| `POST /api/skill-update-suggestions/{suggestionId}/decision` | 拍板：`status` 取 `accepted` 或 `rejected` | 键（无落点） | 成员 |

**字段**

| 方案字段 | JSON 字段 | 读写 | 说明 |
| --- | --- | --- | --- |
| `id` | `id` | 读 | UUID。 |
| `task_id` | `taskId` | 创建时写 | 来源任务。 |
| `skill_id` | `skillId` | 读 | 路径参数。 |
| `suggestion` | `suggestion` | 创建时写 | 建议内容。 |
| `status` | `status` | 动作写 | 见附录 D。 |
| `decided_by` | `decidedBy` | 读 | 服务端写操作者字符串（2.2）。 |
| `decided_at` | `decidedAt` | 读 | 拍板时间。 |
| `created_at` | `createdAt` | 读 | 提出时间。 |

**规则**

- `decision` 只允许 `pending` 到 `accepted` 或 `rejected`。采纳不自动新建 `skill_versions` 行；新版本另行 `POST /api/skills/{skillId}/versions`（ADR-0009 H9）。

## 9. 运行账本（13 张表）

方向职责：平台底账，事件总账、用量、告警、追踪、日志、恢复、决策链、降级策略（方案 ⑦）。本方向写入方多为服务端，浏览器以读取与处置为主。

### 9.1 事件总账 `events`

职责：状态流水、审计、发件箱、运行事件，四张同构表合一（方案 `events`，由 `state_events`、`audit_events`、`outbox_events`、`runner_events` 并入）。

**端点**

| 方法与路径 | 用途 | 幂等 | 权限 |
| --- | --- | --- | --- |
| `GET /api/events` | 列表；过滤 `channel`、`eventType`、`aggregateType` 与 `aggregateId` 成对、`correlationId`、`taskId`、`from`、`to`；按 `recorded_at` 排序 | 天然 | 成员；`channel=audit` 限组织管理员 |
| `GET /api/events/{eventId}` | 读取 | 天然 | 成员 |
| `GET /api/events/stream` | SSE，见 2.7 | 天然 | 成员 |

**字段**

| 方案字段 | JSON 字段 | 读写 | 说明 |
| --- | --- | --- | --- |
| `id` | `id` | 读 | UUID；SSE 帧的 `id`。 |
| `channel` | `channel` | 读 | 方案值 `state`、`audit`、`outbox`、`run`。 |
| `event_type` | `eventType` | 读 | 事件类型；SSE 帧的 `event` 名，命名规则见附录 D。 |
| `occurred_at` | `occurredAt` | 读 | 发生时间。 |
| `recorded_at` | `recordedAt` | 读 | 落库时间；排序与 `Last-Event-ID` 重放依据。 |
| `actor_type` | `actorType` | 读 | 操作者类型，见附录 D。 |
| `actor_id` | `actorId` | 读 | 操作者字符串（2.2）。 |
| `organization_id` | `organizationId` | 读 | 组织，可空。 |
| `task_id` | `taskId` | 读 | 任务，可空。 |
| `run_id` | `runId` | 读 | 运行，可空。 |
| `correlation_id` | `correlationId` | 读 | 关联链，必填，索引；写操作的 `Idempotency-Key` 作为首个事件的关联链。无 `Idempotency-Key` 的后台写入用触发它的上游事件 `correlation_id`，没有则新建。 |
| `causation_id` | `causationId` | 读 | 起因事件。 |
| `aggregate_type` | `aggregateType` | 读 | 聚合对象类型，取资源名单数，见附录 D。 |
| `aggregate_id` | `aggregateId` | 读 | 聚合对象。 |
| `aggregate_version` | `aggregateVersion` | 读 | 对象版本；`tasks`、`change_sets` 取 `version`，其他表取该对象事件计数。 |
| `payload` | `payload` | 读 | 事件内容，JSON；SSE 帧不带。 |
| `published_at` | `publishedAt` | 读 | 发件通道专用：发布时间；只在 `channel=outbox` 有值。 |
| `attempts` | `attempts` | 读 | 发件通道专用：尝试次数；只在 `channel=outbox` 有值。 |
| `last_error` | `lastError` | 读 | 发件通道专用：错误；只在 `channel=outbox` 有值。 |

**规则**

- 写入方只有服务端；没有创建端点。业务写操作与事件同事务（ADR-0016）。
- `channel=outbox` 行是后台待办：后台领取、执行外部动作、写 `published_at` 与 `attempts`；失败写 `last_error`，未知结果先核查（ADR-0016）。
- `payload` 不含凭据、密钥或模型隐藏推理。`events.id` 为 UUID 无序，重放边界见附录 E 写作中发现第 18 条。

**示例**：`GET /api/events/{eventId}` 状态事件。

```json
{
	"id": "0d0b8b1e-4a2f-4c7e-8d0a-6a1f2b3c4d5e",
	"channel": "state",
	"eventType": "task.status_changed",
	"occurredAt": "2026-09-15T09:30:00Z",
	"recordedAt": "2026-09-15T09:30:00Z",
	"actorType": "agent",
	"actorId": "agent:5d1c8e2a-3b4f-4c6d-8e9f-0a1b2c3d4e5f",
	"organizationId": "0a1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d",
	"taskId": "6f1d4c0e-2b7a-4a7e-9c1e-1c2f3a4b5d6e",
	"runId": null,
	"correlationId": "a1e2c3d4-5f6a-4b7c-8d9e-0f1a2b3c4d5e",
	"causationId": null,
	"aggregateType": "task",
	"aggregateId": "6f1d4c0e-2b7a-4a7e-9c1e-1c2f3a4b5d6e",
	"aggregateVersion": 4,
	"payload": {
		"from": "planned",
		"to": "reserved",
		"assigneeAgentId": "8f7e6d5c-4b3a-4291-a0b9-c8d7e6f5a4b3"
	},
	"publishedAt": null,
	"attempts": null,
	"lastError": null
}
```

### 9.2 大模型用量 `llm_usage`

职责：每次调用的 token 与耗时，算账与告警的数据源（方案 `llm_usage`，原样保留）。

**端点**

| 方法与路径 | 用途 | 幂等 | 权限 |
| --- | --- | --- | --- |
| `GET /api/llm-usage` | 列表；过滤 `taskId`、`agentId`、`provider`、`model`、`from`、`to` | 天然 | 成员 |
| `GET /api/llm-usage/summary` | 按 `provider`、`model`、`operation` 聚合 `promptTokens`、`completionTokens`、`durationMs` 与调用次数 `calls`；接受同一组过滤 | 天然 | 成员 |

**字段**

| 方案字段 | JSON 字段 | 读写 | 说明 |
| --- | --- | --- | --- |
| `id` | `id` | 读 | UUID。 |
| `agent_id` | `agentId` | 读 | 调用方，可空。 |
| `task_id` | `taskId` | 读 | 关联任务，可空。 |
| `provider` | `provider` | 读 | 模型提供方，字符串；方案没有供应商表，见附录 E 第 9 条。 |
| `model` | `model` | 读 | 模型。 |
| `operation` | `operation` | 读 | 调用用途，例如规划、评审、出题。 |
| `prompt_tokens` | `promptTokens` | 读 | 输入 token。 |
| `completion_tokens` | `completionTokens` | 读 | 输出 token。 |
| `duration_ms` | `durationMs` | 读 | 耗时。 |
| `created_at` | `createdAt` | 读 | 调用时间。 |

**规则**

- 写入方是后台。方案没有 `organization_id`，租户过滤经 `task_id` 或 `agent_id` 关联；两者皆空的行无法归属，见附录 E 写作中发现第 5 条。

### 9.3 告警规则 `alert_rules`

职责：什么情况该报警（方案 `alert_rules`，原样保留）。

**端点**

| 方法与路径 | 用途 | 幂等 | 权限 |
| --- | --- | --- | --- |
| `GET /api/alert-rules` | 列表；过滤 `enabled` | 天然 | 成员 |
| `POST /api/alert-rules` | 建立：`name`、`metric`、`threshold`、`enabled` | 键（无落点） | 组织管理员 |
| `GET /api/alert-rules/{ruleId}` | 读取 | 天然 | 成员 |
| `PATCH /api/alert-rules/{ruleId}` | 修改 `name`、`metric`、`threshold`、`enabled` | 天然 | 组织管理员 |
| `DELETE /api/alert-rules/{ruleId}` | 删除规则行 | 天然 | 组织管理员 |

**字段**

| 方案字段 | JSON 字段 | 读写 | 说明 |
| --- | --- | --- | --- |
| `id` | `id` | 读 | UUID。 |
| `name` | `name` | 读写 | 规则名。 |
| `metric` | `metric` | 读写 | 监控指标，字符串，例如 `llm_usage.duration_ms`。 |
| `threshold` | `threshold` | 读写 | 阈值，JSON。 |
| `enabled` | `enabled` | 读写 | 是否启用。 |
| `created_at` | `createdAt` | 读 | 创建时间。 |

**规则**

- 存在关联 `alert_events` 时 `DELETE` 返回 409 `INVALID_TRANSITION`，改用 `PATCH` 置 `enabled=false`。

### 9.4 告警事件 `alert_events`

职责：规则命中产生的告警与处置动作一并记录（方案 `alert_events`，由 `alert_events`、`operational_responses` 并入）。

**端点**

| 方法与路径 | 用途 | 幂等 | 权限 |
| --- | --- | --- | --- |
| `GET /api/alert-events` | 列表；过滤 `status`、`ruleId`、`from`、`to` | 天然 | 成员 |
| `GET /api/alert-events/{eventId}` | 读取 | 天然 | 成员 |
| `POST /api/alert-events/{eventId}/claim` | 认领：`handled_by` 取会话用户 | 键（无落点） | 成员 |
| `POST /api/alert-events/{eventId}/resolve` | 关闭：`note`；`handled_by` 取会话用户 | 键（无落点） | 成员 |

**字段**

| 方案字段 | JSON 字段 | 读写 | 说明 |
| --- | --- | --- | --- |
| `id` | `id` | 读 | UUID。 |
| `rule_id` | `ruleId` | 读 | 命中规则。 |
| `metric_value` | `metricValue` | 读 | 触发时的值，JSON。 |
| `status` | `status` | 动作写 | 方案描述“触发/认领/处理中/关闭”，本文令牌 `fired`、`claimed`、`handling`、`closed`（附录 D）。 |
| `handled_by` | `handledBy` | 读 | 处置人，取会话用户。 |
| `handled_at` | `handledAt` | 读 | 处置时间。 |
| `note` | `note` | 动作写 | 处置备注。 |
| `fired_at` | `firedAt` | 读 | 触发时间。 |

**规则**

- 迁移：`claim` 使 `fired` 到 `claimed`；后台自动处置开始时写 `handling`；`resolve` 使 `claimed` 或 `handling` 到 `closed`。其余 409 `INVALID_TRANSITION`。
- 告警触发由后台按 `alert_rules` 评估 `llm_usage` 等数据源写入；浏览器不创建告警事件。

### 9.5 追踪会话 `trace_sessions`

职责：一次完整执行的链路入口（方案 `trace_sessions`，原样保留）。

**端点**

| 方法与路径 | 用途 | 幂等 | 权限 |
| --- | --- | --- | --- |
| `GET /api/trace-sessions` | 列表；过滤 `status`、`from`、`to` | 天然 | 成员 |
| `GET /api/trace-sessions/{sessionId}` | 读取 | 天然 | 成员 |

**字段**

| 方案字段 | JSON 字段 | 读写 | 说明 |
| --- | --- | --- | --- |
| `id` | `id` | 读 | UUID。 |
| `name` | `name` | 读 | 会话名。 |
| `status` | `status` | 读 | 见附录 D。 |
| `started_at` | `startedAt` | 读 | 开始。 |
| `ended_at` | `endedAt` | 读 | 结束，可空。 |

**规则**

- 只读；写入方是后台。`trace_links` 迁出到链路追踪系统（方案），本表不表达跨会话关联。

### 9.6 追踪事件 `trace_events`

职责：链路里的每一步 span（方案 `trace_events`，原样保留）。

**端点**

| 方法与路径 | 用途 | 幂等 | 权限 |
| --- | --- | --- | --- |
| `GET /api/trace-sessions/{sessionId}/events` | 列表；过滤 `parentSpanId`、`from`、`to` | 天然 | 成员 |

**字段**

| 方案字段 | JSON 字段 | 读写 | 说明 |
| --- | --- | --- | --- |
| `id` | `id` | 读 | UUID。 |
| `session_id` | `sessionId` | 读 | 路径参数。 |
| `parent_span_id` | `parentSpanId` | 读 | 父步骤，可空。 |
| `name` | `name` | 读 | 步骤名。 |
| `attributes` | `attributes` | 读 | 属性，JSON。 |
| `started_at` | `startedAt` | 读 | 开始。 |
| `ended_at` | `endedAt` | 读 | 结束，可空。 |

**规则**

- 只读；没有单条读取端点，按会话整体读。

### 9.7 运行日志 `log_entries`

职责：业务级结构化日志存档；应用流水日志另走日志管道（方案 `log_entries`，原样保留）。

**端点**

| 方法与路径 | 用途 | 幂等 | 权限 |
| --- | --- | --- | --- |
| `GET /api/log-entries` | 列表；过滤 `level`、`source`、`from`、`to` | 天然 | 组织管理员 |

**字段**

| 方案字段 | JSON 字段 | 读写 | 说明 |
| --- | --- | --- | --- |
| `id` | `id` | 读 | UUID。 |
| `level` | `level` | 读 | 级别，见附录 D。 |
| `source` | `source` | 读 | 来源，字符串。 |
| `message` | `message` | 读 | 内容。 |
| `context` | `context` | 读 | 上下文，JSON，可空。 |
| `logged_at` | `loggedAt` | 读 | 记录时间。 |

**规则**

- 只读；写入方是三个进程。`context` 不含凭据。

### 9.8 恢复案件 `recovery_cases`

职责：异常需要恢复时立案（方案 `recovery_cases`，原样保留）。

**端点**

| 方法与路径 | 用途 | 幂等 | 权限 |
| --- | --- | --- | --- |
| `GET /api/recovery-cases` | 列表；过滤 `status`、`severity`、`objectType` | 天然 | 成员 |
| `POST /api/recovery-cases` | 立案：`objectType`、`objectId`、`reason`、`severity` | 键（无落点） | 成员或后台 |
| `GET /api/recovery-cases/{caseId}` | 读取 | 天然 | 成员 |
| `POST /api/recovery-cases/{caseId}/close` | 关闭 | 键（无落点） | 组织管理员 |

**字段**

| 方案字段 | JSON 字段 | 读写 | 说明 |
| --- | --- | --- | --- |
| `id` | `id` | 读 | UUID。 |
| `object_type` | `objectType` | 创建时写 | 异常对象类型，取资源名单数（附录 D）。 |
| `object_id` | `objectId` | 创建时写 | 异常对象。 |
| `reason` | `reason` | 创建时写 | 异常原因。 |
| `severity` | `severity` | 创建时写 | 严重程度，见附录 D。 |
| `status` | `status` | 动作写 | 见附录 D。 |
| `created_at` | `createdAt` | 读 | 立案时间。 |

**规则**

- 迁移：立案为 `opened`；首条 `recovery_decisions` 写入后 `decided`；后台开始执行 `recovery_operations` 后 `executing`；`close` 到 `closed`。`close` 时存在未完成操作返回 409 `INVALID_TRANSITION`。后台在操作失败或超时时写 `recovery_operations.result` 为 `{"outcome":"failed"}` 或 `{"outcome":"timeout"}`，写入后允许 `close`。
- ADR-0014 与 ADR-0016：上游不可用或观察陈旧先核查再决定；未知状态不得释放 Worker 或容量（执行门槛 G4、ADR-0017）。

### 9.9 恢复决定 `recovery_decisions`

职责：案件怎么处理的决策（方案 `recovery_decisions`，原样保留）。

**端点**

| 方法与路径 | 用途 | 幂等 | 权限 |
| --- | --- | --- | --- |
| `GET /api/recovery-cases/{caseId}/decisions` | 列表 | 天然 | 成员 |
| `POST /api/recovery-cases/{caseId}/decisions` | 记录决定：`decision`；`decided_by` 取会话或后台 | 键（无落点） | 组织管理员或后台 |

**字段**

| 方案字段 | JSON 字段 | 读写 | 说明 |
| --- | --- | --- | --- |
| `id` | `id` | 读 | UUID。 |
| `case_id` | `caseId` | 读 | 路径参数。 |
| `decision` | `decision` | 创建时写 | 决策内容，字符串。 |
| `decided_by` | `decidedBy` | 读 | 服务端写操作者字符串（2.2）。 |
| `decided_at` | `decidedAt` | 读 | 决策时间。 |

**规则**

- 决定只追加，不修改；新决定不删除旧决定。案件 `closed` 后不接受新决定，409 `INVALID_TRANSITION`。

### 9.10 恢复操作 `recovery_operations`

职责：决策后实际执行的恢复动作，Worker 与平台侧动作合并记（方案 `recovery_operations`，由 `recovery_operations`、`worker_recovery_operations` 并入）。

**端点**

| 方法与路径 | 用途 | 幂等 | 权限 |
| --- | --- | --- | --- |
| `GET /api/recovery-cases/{caseId}/operations` | 列表 | 天然 | 成员 |

**字段**

| 方案字段 | JSON 字段 | 读写 | 说明 |
| --- | --- | --- | --- |
| `id` | `id` | 读 | UUID。 |
| `case_id` | `caseId` | 读 | 路径参数。 |
| `operation` | `operation` | 读 | 操作类型，字符串。 |
| `params` | `params` | 读 | 参数，JSON。 |
| `result` | `result` | 读 | 结果，JSON，可空表示未知或未完成；失败或超时由后台写 `{"outcome":"failed"}` 或 `{"outcome":"timeout"}`（附录 D）。 |
| `executed_at` | `executedAt` | 读 | 执行时间。 |

**规则**

- 只读；后台执行后写入。`result` 为空的操作不视为成功，也不视为失败（ADR-0016）。失败或超时时后台写 `result` 为 `{"outcome":"failed"}` 或 `{"outcome":"timeout"}`，案件随后允许 `close`（9.8）。

### 9.11 决策链 `decision_chain_nodes`

职责：把“为什么这么做”串成链（方案 `decision_chain_nodes`，原样保留）。

**端点**

| 方法与路径 | 用途 | 幂等 | 权限 |
| --- | --- | --- | --- |
| `GET /api/decision-chains` | 列表（已实现）；过滤 `requirementKey`、`keyword`、`repository`、`step`、`limit`（1..200，默认 50）、`offset`；响应 `{"nodes":[...],"mode":"list"}`，按 `createdAt` 倒序。设计稿的 `projectId`、`actorType`、`parentNodeId` 过滤为 E 板块后的扩展 | 天然 | 公开（as-built：GET 无会话，见 2.2） |
| `GET /api/decision-chains/{nodeId}` | 读取（已实现）；`nodeId` 为**决策节点 id**，不是 Python 版的项目 id | 天然 | 公开（同上） |
| `GET /api/decision-chains/similar` | 相似召回（已实现；2026-09-16 裁定保留，不并入 semantic-search）：`requirement` 必填、`repositoryIds` 逗号分隔——**值须为仓库名**（与 `affectedRepositories` 匹配，传内部 id 得不到结果）、`topK`（默认 5 上限 50）、`minSimilarity`、`mode`=`auto`（默认）/`semantic`/`structural`；语义失败或空结果透明降级，响应 `mode` 如实标注；结构化 = GIN 预筛 + Jaccard，带 `matchedRepositories` | 天然 | 公开（同上） |
| `GET /api/settings/decision-chain` | 读取开关（已实现）：`{"feature":"decision_chain","enabled":bool}` | 天然 | 公开（同上） |
| `PUT /api/settings/decision-chain` | 写开关：`enabled`；即时生效（已实现）；落 `feature_settings` 并留痕 `updatedBy`/`updatedAt` | 天然 | 成员（会话）；组织管理员校验待接 |

**字段**

| 方案字段 | JSON 字段 | 读写 | 说明 |
| --- | --- | --- | --- |
| `id` | `id` | 读 | UUID。 |
| `project_id` | `projectId` | 读 | 所属项目，可空。实现中为空串，E 板块后回填。 |
| `parent_node_id` | `parentNodeId` | 读 | 上级节点，可空。实现中为 NULL，E 板块后启用。 |
| `actor_type` | `actorType` | 读 | 决策者类型，见附录 D。实现恒写 `human`（`llm`/`service` 为五步链补全预留）。 |
| `actor_id` | `actorId` | 读 | 决策者字符串（2.2）。 |
| `action` | `action` | 读 | 决策动作。 |
| `rationale` | `rationale` | 读 | 理由。 |
| `context_ref` | `contextRef` | 读 | 上下文引用，JSON。 |
| `created_at` | `createdAt` | 读 | 创建时间。 |
| `event_id` | `eventId` | 创建时写 | 生产者幂等键，部分唯一索引去重（已实现）。 |
| `requirement_text` | `requirementText` | 创建时写 | 需求原文快照，规范化空白后落库（已实现）。 |
| `requirement_key` | `requirementKey` | 创建时写 | sha256 前 32 hex，检索与版本化主轴（已实现）。 |
| `step` | `step` | 创建时写 | 五步链步；第一期写 `confirmation`（已实现）。 |
| `version` | `version` | 动作写 | 同 (requirementKey, step) 下 max+1，同链写经 advisory lock 串行化（已实现）。 |
| `status` | `status` | 创建时写 | 枚举：`proposed`/`adjusted`/`confirmed`/`rejected`/`changes_requested`/`blocked`/`superseded`/`merged`/`closed`；第一期写 `confirmed`/`rejected`（已实现，写作发现 24 结案）。 |
| `affected_repositories` | `affectedRepositories` | 创建时写 | 受影响仓库名数组，GIN 索引支撑结构化召回（已实现）。 |
| `source` | `source` | 创建时写 | `event` / `backfill`（已实现）。 |

**规则**

- 写入方只有服务端：第一期生产者是范围圈定确认（ADR-0023）；`plans apply` 写 `adjusted` 节点，`task_assignments result` 的 `blocked` 结果写 `blocked` 节点（B11 重规划协议，4.3、5.3）。写路径不调用 LLM。
- 开关关闭时，`decision-chains` 全部端点返回 503，响应体为明文 `{"detail":"decision chain is disabled"}`（as-built，无 `FEATURE_DISABLED` 代码），不落新节点，数据保留（ADR-0023）。
- 开关依赖 `public.feature_settings` 表——2026-09-16 裁定补入方案表清单（附录 E 第 10 条结案）；该表已是平台级功能开关表（`skill_governance` 亦用，见 0013 迁移）。
- `similar` 与 `semantic-search` 为两条独立端点（2026-09-16 裁定，附录 E 第 16 条结案）：结构化召回在无 embedding 环境下是唯一召回路径，且落库即可命中；语义召回存在 refresh 前的新鲜度窗口。

### 9.12 决策向量 `decision_embeddings`

职责：决策的向量表示，支持语义检索（方案 `decision_embeddings`，原样保留）。

**端点**

| 方法与路径 | 用途 | 幂等 | 权限 |
| --- | --- | --- | --- |
| `GET /api/decision-chains/semantic-search` | 语义搜索（已实现）：`queryText` 必填、`topK`（默认 5 上限 50）、`minSimilarity`；检索一律按服务端配置的 `model` 过滤（ADR-0023，无 `model` 请求参数） | 天然 | 公开（as-built：GET 无会话，见 2.2） |
| `POST /api/decision-chains/embeddings/refresh` | 批量补嵌入（批 16，已实现）：出参 `{refreshed, failed, reason?}`，未配端点诚实返回 0 | 天然 | 成员 |

**字段**

| 方案字段 | JSON 字段 | 读写 | 说明 |
| --- | --- | --- | --- |
| `id` | `id` | 读 | UUID。 |
| `node_id` | `nodeId` | 读 | 对应决策节点，唯一。 |
| `embedding` | 不输出 | 不暴露 | 向量本体只在服务端比较；检索响应返回 `similarity`。方案写 JSON；ADR-0021 与 ADR-0023 采用 `vector(1024)` 加 HNSW 余弦索引，JSON 作无扩展环境的兜底双写；维度绑定嵌入模型，换模型需新迁移与全量重嵌（附录 E 第 17 条）。 |
| `model` | `model` | 读 | 向量模型；检索一律按 `model` 过滤，防止换模型后混库（ADR-0023）。 |

不暴露：`embedding`。

**规则**

- 检索响应为 `{"items":[{"node":{...},"similarity":0.87}]}`，`node` 为 9.11 的节点快照；`topK` 默认 10，最大 50；`minSimilarity` 取 0 到 1。
- 排序、截断与门槛在数据库内执行（`ORDER BY embedding <=> :query LIMIT :k`）；`pg_extension` 无 `vector` 时回落 JSON 余弦，结果排序一致，日志标明降级（ADR-0021）。
- 查询向量在读路径计算；写路径不调用 LLM，向量只经 `refresh` 异步沉淀（ADR-0023）。
- 方案写有 `node_id → decision_chain_nodes` 逻辑关联；是否建物理外键按 ADR-0023。孤儿向量由 `refresh` 幂等覆盖。开关关闭时两个端点同样返回 503 `FEATURE_DISABLED`。

### 9.13 降级策略 `mcp_server_policies`

职责：MCP 服务超时、重试、只读降级、写阻断（方案 `mcp_server_policies`，改造保留：补服务器标识）。

**端点**

| 方法与路径 | 用途 | 幂等 | 权限 |
| --- | --- | --- | --- |
| `GET /api/mcp-server-policies` | 列表 | 天然 | 组织管理员 |
| `GET /api/mcp-server-policies/{serverName}` | 读取 | 天然 | 组织管理员 |
| `PUT /api/mcp-server-policies/{serverName}` | 写入或覆盖：`timeoutSeconds`、`maxRetries`、`retryableOnlyReads`、`degradedBlockWrites`、`requiredTaskFeatures` | 天然 | 组织管理员 |

**字段**

| 方案字段 | JSON 字段 | 读写 | 说明 |
| --- | --- | --- | --- |
| `id` | `id` | 读 | UUID。 |
| `server_name` | `serverName` | 读 | 路径参数，唯一。 |
| `timeout_seconds` | `timeoutSeconds` | 读写 | 超时秒数，正整数。 |
| `max_retries` | `maxRetries` | 读写 | 最大重试，非负整数。 |
| `retryable_only_reads` | `retryableOnlyReads` | 读写 | 仅只读可重试。 |
| `degraded_block_writes` | `degradedBlockWrites` | 读写 | 降级时阻断写。 |
| `required_task_features` | `requiredTaskFeatures` | 读写 | 必需任务特性，JSON 数组。 |

**规则**

- ADR-0011：策略约束 MCP 出站，不代替凭据、存储与网络的实际约束。
- `PUT` 首次返回 201，之后 200；不提供删除端点，停用某服务器把 `degradedBlockWrites` 置 `true` 且 `maxRetries` 置 0。

## 附录 A：表与资源映射

端点列只写路径，方法见各资源小节。写入方列说明哪些进程写这张表。

| 表 | 端点 | 写入方与说明 |
| --- | --- | --- |
| `organizations` | `/api/organizations/current` | Web（PATCH）；建立方式无落点（附录 E 写作中发现第 14 条）。 |
| `users` | `/api/auth/github/*`（现行登录）、`/api/auth/attempts/{id}`、`/api/auth/logout`、`/api/session`；`/api/sessions`、`/api/sessions/current`、`/api/users`、`/api/users/{userId}`、`/api/users/{userId}/password`（作废设计） | Web；会话列由登录与注销写。 |
| `agents` | `/api/agents`、`/api/agents/{agentId}` | Web（注册、修改）；后台写 `resource_ref`、`status`。 |
| `credentials` | `/api/credentials`、`/api/credentials/{key}` | Web；`value_encrypted` 只由服务端加密写入。 |
| `projects` | `/api/projects`、`/api/projects/{projectId}` | Web。 |
| `repositories` | `/api/repositories`、`/api/repositories/{repositoryId}`、`/api/repositories/{repositoryId}/profile` | Web（登记、修改）；后台写画像与轮询列。 |
| `plans` | `/api/projects/{projectId}/plans`、`/api/plans/{planId}`、`/api/plans/{planId}/revisions`、`/api/plans/{planId}/apply` | 后台（代 Manager、Leader）；后台写 `execution_plan_id`；`apply` 迁移 `tasks` 并写 `decision_chain_nodes`。 |
| `agent_teams` | `/api/projects/{projectId}/agent-teams`、`/api/agent-teams/{teamId}`、`/api/agent-teams/{teamId}/runtime` | Web（建立、修改、runtime 请求）；后台写 `room_id`、`runtime_status` 结果态。 |
| `review_requests` | `/api/review-requests`、`/api/review-requests/{requestId}`、`/api/review-requests/{requestId}/decision` | 后台发起，Web 批复。 |
| `tasks` | `/api/projects/{projectId}/tasks`、`/api/tasks/{taskId}`、`/api/tasks/{taskId}/subtasks`、`/api/tasks/{taskId}/reserve`、`/api/tasks/{taskId}/release`、`/api/tasks/{taskId}/pause`、`/api/tasks/{taskId}/resume`、`/api/tasks/{taskId}/cancel`、`/api/tasks/{taskId}/complete` | Web 与后台；状态由动作端点与后台写，`plans apply` 置 `superseded`。 |
| `plan_steps` | `/api/plans/{planId}/steps`、`/api/plan-steps/{stepId}` | 后台（代 Manager）；浏览器只读。 |
| `task_assignments` | `/api/tasks/{taskId}/assignments`、`/api/task-assignments/{assignmentId}`、`/api/task-assignments/{assignmentId}/result` | 后台；`dispatch_ref` 由后台写。 |
| `handoffs` | `/api/tasks/{taskId}/handoffs`、`/api/handoffs/{handoffId}`、`/api/handoffs/{handoffId}/status` | 后台（代 Manager、`db-test-team`）。 |
| `messages` | `/api/messages`、`/api/messages/{messageId}` | Web（人发）与后台（Agent 发、投递状态）。 |
| `context_objects` | `/api/projects/{projectId}/context-objects`、`/api/context-objects/{objectId}`、`/api/context-objects/{objectId}/content`、`/api/context-objects/{objectId}/versions` | Web。 |
| `context_bundles` | `/api/task-assignments/{assignmentId}/context-bundles`、`/api/context-bundles/{bundleId}` | 后台（代 Manager）。 |
| `context_deltas` | `/api/context-objects/{objectId}/deltas`、`/api/context-deltas/{deltaId}/sync` | 后台。 |
| `context_access_events` | `/api/context-objects/{objectId}/access-events` | 内部写入，只读端点；后台在 Agent 读取材料时写。 |
| `change_sets` | `/api/tasks/{taskId}/change-sets`、`/api/change-sets/{changeSetId}`、`/api/change-sets/{changeSetId}/deliver`、`/api/change-sets/{changeSetId}/conflict`、`/api/change-sets/{changeSetId}/archive` | 后台与 Web；`pr_url` 由后台按命令结果写。 |
| `scm_commands` | `/api/change-sets/{changeSetId}/scm-commands`、`/api/scm-commands/{commandId}`、`/api/scm-commands/{commandId}/retry` | `deliver` 生成，后台执行并写结果。 |
| `scm_observations` | `/api/change-sets/{changeSetId}/scm-observations` | 内部写入，只读端点；后台轮询写，`consumed` 由后台写。 |
| `delivery_policies` | `/api/projects/{projectId}/delivery-policy` | Web（PUT）。 |
| `validation_snapshots` | `/api/projects/{projectId}/validation-snapshots`、`/api/validation-snapshots/{snapshotId}` | 内部写入，只读端点；后台验证流程写。 |
| `database_branch_validations` | `/api/projects/{projectId}/database-branch-validations`、`/api/database-branch-validations/{validationId}`、`/api/database-branch-validations/{validationId}/cleanup` | Web 与后台发起；后台经受限主机执行写结果。 |
| `skills` | `/api/skills`、`/api/skills/{skillId}` | Web。ADR 更新前不实施（第 8 节）。 |
| `skill_versions` | `/api/skills/{skillId}/versions`、`/api/skill-versions/{versionId}`、`/api/skill-versions/{versionId}/status` | Web；`published` 由审批结论写。 |
| `skill_test_questions` | `/api/skills/{skillId}/test-questions` | Web 与后台。 |
| `skill_evaluation_runs` | `/api/skill-versions/{versionId}/evaluation-runs`、`/api/skill-evaluation-runs/{runId}/judgement` | Web 发起，后台写 `answer`。 |
| `skill_approvals` | `/api/skill-versions/{versionId}/approval`、`/api/skill-approvals/{approvalId}/decision` | Web 与后台。 |
| `agent_skill_bindings` | `/api/agents/{agentId}/skill-bindings`、`/api/agent-skill-bindings/{bindingId}` | Web 与后台。 |
| `skill_update_suggestions` | `/api/skills/{skillId}/update-suggestions`、`/api/skill-update-suggestions/{suggestionId}/decision` | Web 与后台。 |
| `events` | `/api/events`、`/api/events/{eventId}`、`/api/events/stream` | 内部写入，只读端点；Web 与后台在业务事务内写。 |
| `llm_usage` | `/api/llm-usage`、`/api/llm-usage/summary` | 内部写入，只读端点；后台写。 |
| `alert_rules` | `/api/alert-rules`、`/api/alert-rules/{ruleId}` | Web。 |
| `alert_events` | `/api/alert-events`、`/api/alert-events/{eventId}`、`/api/alert-events/{eventId}/claim`、`/api/alert-events/{eventId}/resolve` | 后台触发，Web 处置。 |
| `trace_sessions` | `/api/trace-sessions`、`/api/trace-sessions/{sessionId}` | 内部写入，只读端点；后台写。 |
| `trace_events` | `/api/trace-sessions/{sessionId}/events` | 内部写入，只读端点；后台写。 |
| `log_entries` | `/api/log-entries` | 内部写入，只读端点；三个进程写。 |
| `recovery_cases` | `/api/recovery-cases`、`/api/recovery-cases/{caseId}`、`/api/recovery-cases/{caseId}/close` | Web 与后台。 |
| `recovery_decisions` | `/api/recovery-cases/{caseId}/decisions` | Web 与后台。 |
| `recovery_operations` | `/api/recovery-cases/{caseId}/operations` | 内部写入，只读端点；后台执行后写。 |
| `decision_chain_nodes` | `/api/decision-chains`、`/api/decision-chains/{nodeId}`、`/api/decision-chains/similar`、`/api/settings/decision-chain` | 内部写入，只读端点；范围圈定确认、`plans apply`、`blocked` 结果写入（9.11）。 |
| `feature_settings` | `/api/settings/decision-chain`、`/api/settings/skill-governance` | Web（写开关）；平台级功能开关表（2026-09-16 裁定补入，附录 E 第 10 条结案）。 |
| `decision_embeddings` | `/api/decision-chains/semantic-search`、`/api/decision-chains/embeddings/refresh` | 后台经 `refresh` 写。 |
| `mcp_server_policies` | `/api/mcp-server-policies`、`/api/mcp-server-policies/{serverName}` | Web（PUT）。 |

## 附录 B：ADR 对照

| ADR | 本文如何落实 | 方案中缺失的落点 |
| --- | --- | --- |
| [ADR-0001](../adr/0001-agentteams-issue-concurrency-and-isolation.md) 团队、Issue 并行与隔离 | `tasks.assignee_agent_id` 落实单 Worker 单活跃任务，`reserve` 冲突 409；Attempt 对应 `task_assignments` 一行；子任务完成不自动完成父任务；`pause` 只停新派工。 | 无。 |
| [ADR-0002](../adr/0002-github-app-authorization-and-draft-pr-delivery.md) GitHub App 授权与 draft PR | `credentials.kind=github-app`；`deliver` 只生成 draft PR 命令，合并由人；`human_grants` 与 `delivery_policies.policy` 不扩大 Git 权限。 | 有效权限交集需要的安装范围与用户授权数据无表（附录 E 第 8 条）；多仓项目（附录 E 第 2 条）。 |
| [ADR-0003](../adr/0003-plan-change-authorization-and-activation.md) 计划许可、应用与生效 | `plans` 每次变化新行、`plan_version` 递增（§1.1）；获准经 `review_requests(objectType=plan)`；`decided_by` 服务端取会话（§1.6）；`plan-steps` 写端点不对浏览器开放（§3.1）；`execution_mode` 两档（§1.2）。 | 生效指针（附录 E 第 5 条）；YOLO 自动许可以空 `decided_by` 表达，待确认（写作中发现第 7 条）；审批人资格数据（写作中发现第 1 条）。 |
| [ADR-0004](../adr/0004-acceptance-rule-clarification.md) 缺失业务规则的解释 | `review_requests.object_type=interpretation`（提案）；解释与验收通过分开，验证结果在 `validation_snapshots`。 | 方案 `object_type` 列举无此值（附录 E 第 5 条）。 |
| [ADR-0005](../adr/0005-optional-project-verification-group.md) 可选项目验证小组 | `agent_teams` 的验证小组开关与成员配置需项目管理员（§1）；`validation_snapshots` 初判与复核分别记录（§3）；`db-test-team` 角色承担数据库验证。 | 项目管理员无落点（写作中发现第 1 条）；验证小组开关无列；其余编制改动的授权来源方案未定义（4.4）。 |
| [ADR-0006](../adr/0006-manager-entry-modes-and-skill-driven-execution.md) Manager 入口、模式与 Skill 分工 | `execution_mode` 当前只允许 `yolo`（§2）；人工门禁走 `review_requests`，环境阻塞用 `tasks.blocked`（§2）；Agent 决定工具经 MCP 进入后台，不经 HTTP（§3）。 | 角色命名倒置（附录 E 第 1 条）；MCP Schema 未冻结（附录 E 第 13 条）。 |
| [ADR-0007](../adr/0007-graph-loop-plugin.md) Graph／Loop 行为 | 循环上限在 `task_assignments.generation` 与 `phase`、`plans.execution_batches` 体现：修复 3 轮、诊断 2 轮、环境恢复 2 次、连续两轮同一失败结束（D3、D4）；超限由后台拒绝新派单并返回 409 `INVALID_TRANSITION`；迟到结果不覆盖（D7）；`validation_snapshots.status` 补 `undetermined`、`blocked`（D2）。 | 计数口径依赖 `phase` 提案值。 |
| [ADR-0008](../adr/0008-changeset-attribution-and-history.md) ChangeSet 归属与历史 | 顶层任务正式提交时同事务建立允许暂空的主 `change_sets`；重放复用；`archived` 终态；首期无嵌套。 | Candidate、Delivery Combination 五个事实无独立列（附录 E 第 6 条）。 |
| [ADR-0009](../adr/0009-skill-engineering-deferred.md) Skill 工程延后 | 第 8 节内容是随方案给出的设计，不是采用记录，ADR 更新前不得实施；H2、H6、H9、H12 在规则中引用。 | 替代关系未写入 ADR（附录 E 第 7 条）；H1、H11 的 Issue 引用确定版本改为绑 Agent（附录 E 第 7 条）；H3 归属与 H10 项目默认无列。 |
| [ADR-0010](../adr/0010-technology-stack-and-modular-monolith.md) 技术栈与模块化单体 | PostgreSQL 单库；`events(channel=outbox)` 作持久队列；SSE 不承诺补发。 | 无。 |
| [ADR-0011](../adr/0011-agentteams-controlled-integration.md) AgentTeams 受控接入 | Agent 请求经受控 MCP 入口进入后台协调进程；`mcp_server_policies` 约束出站，不代替凭据与存储约束。 | Agent 调用凭证（附录 E 第 13 条）。 |
| [ADR-0012](../adr/0012-issue-scoped-upstream-projects.md) Issue 仓库委派与上游 Project | 上游 Project 引用放在 `agents.resource_ref` 与 `task_assignments.dispatch_ref` JSON，须含实例、team、`project_id`（键名见附录 D）。 | 编制级引用无列（写作中发现第 21 条）。 |
| [ADR-0013](../adr/0013-web-coordinator-host-executor-processes.md) Web、后台协调、主机执行 | Web 只读写库，外部动作由后台执行，宿主能力经受限主机执行；`runtime stop`、`cleanup` 是请求，停止事实另观察。 | 后台专用端点的 HTTP 调用方（写作中发现第 4 条）。 |
| [ADR-0014](../adr/0014-in-process-graph-plugin.md) 进程内 Graph 插件 | Graph 结果只读观察，经 `plans.task_dag`、`plan_steps.depends_on`、`events`；不提供换图写端点。 | 换图协议未选（执行门槛 G5）。 |
| [ADR-0015](../adr/0015-round-scoped-upstream-dags.md) 按获准轮次复用上游有限 DAG | 限额内推进下一轮不新建 `plans` 行；验证通过（`validation_snapshots`）与任务结束（`complete`）分别判断；迟到结果只改本行。 | result admission（附录 E 第 6 条）。 |
| [ADR-0016](../adr/0016-transactional-background-work.md) 业务与待办同事务 | 业务行与 `events(channel=outbox)` 同事务；未知结果 503 `RESULT_UNCONFIRMED`；`scm-commands retry` 先核查。 | 仅 4 张表有 `idempotency_key`（附录 E 第 11 条）；无输入摘要列（写作中发现第 6 条）。 |
| [ADR-0017](../adr/0017-atomic-attempt-resource-reservation.md) Attempt 与资源统一预留 | `reserve` 短事务写 `assignee_agent_id`、`reserved_at`、`status`；`assignments` 同事务；未知状态 `release` 返回 409。 | 容量与额度无列。 |
| [ADR-0018](../adr/0018-provision-instance-after-first-draft.md) 项目先保存、按需准备实例 | 项目创建不触发准备；`runtime start` 与首条消息异步准备；201 不证明就绪或接收。 | 首条消息时 `room_id` 必填（写作中发现第 8 条）。 |
| [ADR-0019](../adr/0019-conversation-issue-separation.md) 会话与独立 Issue 分离 | 未落实。Issue 暂由顶层 `tasks` 承接，会话无实体，Issue SSE 暂由 `events/stream` 承接。 | 无会话实体（附录 E 第 3 条）；无独立 Issue 与仓库事项（附录 E 第 4 条）。 |
| [ADR-0020](../adr/0020-python-repository-analysis-plugin.md) 建项前 Python 仓库分析 | `POST /api/repositories/{repositoryId}/profile` 是可选分析，不扩仓、不建计划、不触发实例。 | 分析作业表无（附录 E 第 15 条）。 |
| [ADR-0021](../adr/0021-pgvector-rag-storage-plugin.md) pgvector RAG 存储插件 | `semantic-search` 的排序、截断与门槛在数据库内执行；无 `vector` 扩展时回落 JSON 余弦并标明降级；`embedding` 维度绑定嵌入模型，换模型需新迁移与全量重嵌（9.12）。 | 方案 `decision_embeddings.embedding` 类型写为 JSON，与 ADR-0021 的 `vector(1024)` 不一致（附录 E 第 17 条）；与 ADR-0020 的时序说明已在 ADR 索引注明不冲突（2026-09-16）。 |
| [ADR-0022](../adr/0022-human-checkpoint-resolution-governance.md) 人工检查点决议治理 | `decision` 端点是唯一落库入口，同事务写 `decided_by`、`decision_note`、`decided_at` 与终态；`pending` 不按当前政策复查，授权人可决、漂移时组织管理员兜底、其他账号 403；聊天批复定位为沟通，不改本表；存量清偿逐条走同一端点（4.5）。 | `review_requests` 没有决议来源列与漂移标记列；监管策略入口（三档）在方案里只有 `agent_teams.execution_mode`，页面归属不在本文范围。 |
| [ADR-0023](../adr/0023-decision-chain-native-module-and-pgvector.md) 决策链原生模块与 pgvector | `decision-chains` 读端点；`similar` 与 `semantic-search` 两条独立（2026-09-16 裁定）；写路径不调 LLM；开关关闭 503。 | `feature_settings` 已裁定补入方案表清单（附录 E 第 10 条结案）；`similar` 与 `semantic-search` 的关系已裁定为并存（附录 E 第 16 条结案）。 |

## 附录 C：现有端点的去向

> **2026-09-17 增补**:登记 2026-09-16 之后新增落地的 60 条路由——发现/物化链(工作台对话流主线)、issue 归档与清除、观测两族(`/api/observe/*` 与 `/api/v1/observe/*` 并存)、管线扩展 M1-M9(计划生成/中断/交接单/规格/发布门禁/接口文档/分支验证/联合验证/任务审批)、humancontrol 控制面(审核队列/检查点决策/项目控制)、组织组装与 GitHub webhook。字段细节以 `internal/` 各实现包为准,按板块回填本册。

状态取 `已实现`（代码有路由）或 `已采用未实现`（契约有，代码无）。

| 现有端点 | 状态 | 去向 |
| --- | --- | --- |
| `GET /healthz` | 已实现 | 保留，不改。 |
| `GET /readyz` | 已实现 | 保留，不改。 |
| `GET /api/session` | 已实现 | `GET /api/sessions/current`（3.2）。 |
| `POST /api/auth/github/login` | 已实现 | 保留，不改（2026-09-16 裁定，附录 E 第 8 条结案）。 |
| `POST /api/auth/github/reconnect` | 已实现 | 保留，不改（2026-09-16 裁定，附录 E 第 8 条结案）。 |
| `GET /api/auth/github/callback` | 已实现 | 保留，不改（2026-09-16 裁定，附录 E 第 8 条结案）。 |
| `GET /api/auth/attempts/{id}` | 已实现 | 保留，不改（2026-09-16 裁定，附录 E 第 8 条结案）。 |
| `POST /api/auth/logout` | 已实现 | `DELETE /api/sessions/current`（3.2）。 |
| `GET /api/repositories`（GitHub 发现，需会话） | 已实现 | 仓库档案并入 `GET /api/repositories`；发现语义无表，待裁定（附录 E 第 8 条）。 |
| `POST /api/projects` | 已实现 | `POST /api/projects`（4.1）。 |
| `GET /api/project-creations/{id}` | 已实现 | 原操作回执无 `creation_operations` 落点，重放保护待决（附录 E 第 11 条）。 |
| `PATCH /api/projects/{id}` | 已实现 | `PATCH /api/projects/{projectId}`（4.1）。 |
| `GET /api/projects/{id}/updates/{id}` | 已实现 | 无 `update_operations` 落点，待决（附录 E 第 11 条）。 |
| `GET /api/projects/{id}` | 已实现 | `GET /api/projects/{projectId}`（4.1）。 |
| `GET /api/projects` | 已实现 | `GET /api/projects`（4.1）。 |
| `GET /api/projects/{id}/repositories` | 已实现 | 一项目一仓库，改读 `projects.repository_id`；多仓待裁定（附录 E 第 2 条）。 |
| `GET /api/configuration-profiles` | 已实现 | 保留，不改（2026-09-16 裁定，附录 E 第 9 条结案）；执行配置无表仍待方案作者。 |
| `POST /api/model-tests` | 已实现（2026-09-16，B05） | 模型连通性测试；字段以 `internal/models` 与 B05 交付为准。 |
| `GET /api/model-tests/{testId}` | 已实现（2026-09-16，B05） | 测试结果读取。 |
| `POST /api/model-test-previews` | 已实现（2026-09-16，B05） | 测试预览（不入库）。 |
| `POST /api/projects/{projectId}/model-applications` | 已实现（2026-09-16，B04 扩展） | 模型应用到项目；字段以 B04 采用记录为准。 |
| `GET /api/projects/{projectId}/model-applications/{applicationId}` | 已实现（2026-09-16，B04 扩展） | 应用结果读取。 |
| `POST /api/projects/{projectId}/model-application-previews` | 已实现（2026-09-16，B04 扩展） | 应用预览（不入库）。 |
| `GET /api/model-providers` | 已实现 | 保留，不改（2026-09-16 裁定，附录 E 第 9 条结案）。 |
| `GET /api/model-providers/{id}` | 已实现 | 保留，不改（2026-09-16 裁定，附录 E 第 9 条结案）。 |
| `GET /api/model-providers/{id}/versions/{revision}` | 已实现 | 保留，不改（2026-09-16 裁定，附录 E 第 9 条结案）。 |
| `POST /api/model-provider-saves` | 已实现 | 保留，不改（2026-09-16 裁定，附录 E 第 9 条结案）。 |
| `GET /api/model-provider-saves/{saveId}` | 已实现 | 保留，不改（2026-09-16 裁定，附录 E 第 9 条结案）。 |
| `POST /api/model-provider-saves/{saveId}/close` | 已实现 | 保留，不改（2026-09-16 裁定，附录 E 第 9 条结案）。 |
| `GET /api/repositories`（目录，无认证） | 已实现 | `GET /api/repositories`，改为需会话（4.2）。 |
| `GET /api/repositories/url-type` | 已实现 | 无对应端点；URL 类型判断留在客户端或并入 `POST /api/repositories` 校验（写作中发现第 22 条）。 |
| `GET /api/repositories/candidates` | 已实现（2026-09-16 裁定挪路径） | B02 可参与仓库候选（会话必需；`q`/`cursor`/`limit` 过滤）；原 `/api/repositories` 让位给扫描目录。 |
| `GET /api/repositories/dependents` | 已实现（2026-09-16，B11 合入新增） | 反向依赖查询：`?name=` 返回依赖 X 的仓库（scan 证据图 `Dependents`），供重规划受影响集合判定的前端展示与调试。 |
| `POST /api/repositories` | 已实现 | `POST /api/repositories`（4.2）。 |
| `POST /api/scan-jobs` | 已实现 | 对应 ADR-0020 建项前分析，方案无作业表，待决（附录 E 第 15 条）。 |
| `GET /api/scan-jobs/{id}` | 已实现 | 待决（附录 E 第 15 条）。 |
| `POST /api/scope/suggestions` | 已实现 | 待决（附录 E 第 15 条）。 |
| `POST /api/scope/check` | 已实现 | 待决（附录 E 第 15 条）。 |
| `POST /api/scope` | 已实现 | 待决（附录 E 第 15 条）。 |
| `GET /api/settings/scope-assist` | 已实现 | 依赖 `feature_settings`，待决（附录 E 第 10 条）。 |
| `PUT /api/settings/scope-assist` | 已实现 | 依赖 `feature_settings`，待决（附录 E 第 10 条）。 |
| `GET /api/decision-chains` | 已实现 | `GET /api/decision-chains`（9.11）。 |
| `GET /api/decision-chains/{id}` | 已实现 | `GET /api/decision-chains/{nodeId}`（9.11）。 |
| `GET /api/decision-chains/similar` | 已实现 | 保留为独立端点（2026-09-16 裁定，附录 E 第 16 条结案）；结构化召回为无 embedding 环境的唯一召回路径。 |
| `GET /api/decision-chains/semantic-search` | 已实现 | `GET /api/decision-chains/semantic-search`（9.12）。 |
| `POST /api/decision-chains/embeddings/refresh` | 已实现 | `POST /api/decision-chains/embeddings/refresh`（9.12）。 |
| `GET /api/settings/decision-chain` | 已实现 | `GET /api/settings/decision-chain`；开关落 `public.feature_settings`（2026-09-16 裁定，附录 E 第 10 条结案）。 |
| `PUT /api/settings/decision-chain` | 已实现 | `PUT /api/settings/decision-chain`；开关落 `public.feature_settings`（2026-09-16 裁定，附录 E 第 10 条结案）。 |
| `GET /api/projects/{projectId}/issues` | 已实现（2026-09-16，B06/B07） | 按创建契约 v1 路径实施；字段以 issue-page-create-api-contract.md 为准。 |
| `GET /api/projects/{id}/conversations` | 已采用未实现 | 无落点（附录 E 第 3 条）。 |
| `GET /api/projects/{projectId}/issue-creation-options` | 已实现（2026-09-16，B06） | 按创建契约 v1 实施。 |
| `GET /api/projects/{projectId}/issue-conversations` | 已实现（2026-09-16，B06） | 按创建契约 v1 实施；实现路径为 `issue-conversations`（会话实体仍无独立表，附录 E 第 3 条保持待决）。 |
| `POST /api/projects/{projectId}/issue-creations` | 已实现（2026-09-16，B06） | 按创建契约 v1 实施（幂等键、同键重放同回执）；字段以契约为准。实现路径为 `issue-creations`，非设计稿的 `tasks` 路径（E-4 的任务化设计与实现的取舍待后续裁定）。 |
| `GET /api/projects/{projectId}/issue-creations/{creationId}` | 已实现（2026-09-16，B06） | 独立回执端点存在（0017 迁移含创建操作落点；附录 E 第 11 条对 Issue 域结案，其余表仍待）。 |
| `GET /api/issues/{issueId}` | 已实现（2026-09-16，B07 读面） | 路径按创建契约 v1；字段以契约为准。 |
| `GET /api/issues/{issueId}/rooms` | 已实现（2026-09-16，B07 读面） | 路径按创建契约 v1；房间消息流本身未实现（附录 E 第 4 条保持待决）。 |
| `POST /api/issues/parse-document` | 已实现（2026-09-16，需求文档解析） | multipart `file` → `{filename,format,text,chars,truncated}`（10 MiB 上限、20,000 字符截断）；415 格式不支持、413 超限、422 无可提取文本（如扫描件）。只读不改库。 |
| `GET /api/agentteams/projects/{projectId}/workflow` | 已实现（2026-09-16，AgentTeams 执行进度） | 服务端适配器只读透传上游 Controller workflow（`?includeTasks=true`，`?team=` 消歧），浏览器不直连；未配置 Controller 返回 503 `AGENTTEAMS_NOT_CONFIGURED`。数据源，非本方案 44 表实体。 |
| `GET /api/issues/{issueId}/events`（SSE） | 已采用未实现 | `GET /api/events/stream` 过滤 `taskId`（2.7）。 |
| `POST /api/projects/{projectId}/conversations/{conversationId}/messages` | 已实现（2026-09-16，B09） | 按实现路径登记；消息持久化见 0019 迁移与 B09 交付。 |
| `GET /api/projects/{projectId}/conversations/{conversationId}/messages` | 已实现（2026-09-16，B09） | 按实现路径登记；支持游标分页。 |
| `GET /api/projects/{projectId}/message-submissions/{submissionId}` | 已实现（2026-09-16，B09） | 消息提交回执读取（0019 迁移落点；附录 E 第 11 条对消息域结案）。 |
| `GET .../messages/{messageId}` | 已采用未实现 | `GET /api/messages/{messageId}`（6.1）。 |
| `GET /api/projects/{projectId}/logical-requests/{requestId}/clarification` | 已实现（2026-09-16，B09） | 澄清记录读取（0019 迁移落点）。 |
| `GET /api/skills/versions` | 已实现 | 保留，不改（2026-09-16 登记；技能治理随 0009–0014 迁移与 `internal/skills` 落地，开关 `feature_settings.skill_governance`）。 |
| `POST /api/skills` | 已实现 | 保留，不改。 |
| `POST /api/skills/versions` | 已实现 | 保留，不改。 |
| `POST /api/skills/versions/{id}/evaluate` | 已实现 | 保留，不改。 |
| `POST /api/skills/versions/{id}/evaluations` | 已实现 | 保留，不改。 |
| `POST /api/skills/versions/{id}/canary` | 已实现 | 保留，不改。 |
| `POST /api/skills/versions/{id}/promote` | 已实现 | 保留，不改。 |
| `POST /api/skills/versions/{id}/rollback` | 已实现 | 保留，不改。 |
| `POST /api/skills/versions/{id}/release` | 已实现 | 保留，不改。 |
| `POST /api/skills/versions/{id}/questions` | 已实现 | 保留，不改。 |
| `POST /api/skills/versions/{id}/approvals` | 已实现 | 保留，不改。 |
| `POST /api/skills/approvals/{id}/decide` | 已实现 | 保留，不改。 |
| `POST /api/skills/arms` | 已实现 | 保留，不改。 |
| `POST /api/skills/assemble` | 已实现 | 保留，不改。 |
| `GET /api/skills/bindings` | 已实现 | 保留，不改。 |
| `POST /api/skills/bindings` | 已实现 | 保留，不改。 |
| `DELETE /api/skills/bindings/{id}` | 已实现 | 保留，不改。 |
| `POST /api/skills/suggestions` | 已实现 | 保留，不改。 |
| `POST /api/skills/suggestions/{id}/decide` | 已实现 | 保留，不改。 |
| `GET /api/skills/mcp-policies` | 已实现 | 保留，不改。 |
| `GET /api/settings/skill-governance` | 已实现 | 保留，不改（开关表同 `feature_settings`）。 |
| `PUT /api/settings/skill-governance` | 已实现 | 保留，不改。 |

## 附录 D：本文提案的枚举值

方案已给出的值不在此重复：`agents.role`、`credentials.kind`、`messages.sender_type`、`events.channel`、`skill_test_questions.provided_by`、`skill_approvals.subject_role`、`skill_approvals.reviewer_kind`（2.8 第 1 类）。下表全部为提案，采用前不能写成已定。

| 列或结构 | 提案值 | 说明 |
| --- | --- | --- |
| 字符串型操作者列（2.2） | `user:<userId>`、`agent:<agentId>`、`system` | 适用于 `credentials.updated_by`、`skills.created_by`、`skill_versions.created_by`、`recovery_decisions.decided_by`、`skill_update_suggestions.decided_by`、`events.actor_id`、`decision_chain_nodes.actor_id`、`skill_evaluation_runs.judged_by`。 |
| `users.org_role` | `org_admin`、`member` | 方案：组织管理员、普通成员。 |
| `alert_events.status` | `fired`、`claimed`、`handling`、`closed` | 方案：触发、认领、处理中、关闭。 |
| `skill_versions.status` | `draft`、`evaluating`、`published`、`deprecated` | 方案：草稿、评估中、已发布、废弃。 |
| `agents.status` | `active`、`suspended`、`retired` | `suspended` 不接受新派单。 |
| `agents.resource_ref` 键 | `instance`、`team`、`memberId` | ADR-0012 的上游实例与 team 引用。 |
| `projects.status` | `draft`、`active`、`archived` | 迁移见 4.1。 |
| `plans.revisions[]` 键 | `summary`、`reason`、`revisedBy`、`revisedAt` | `POST .../revisions` 追加。 |
| `agent_teams.execution_mode` | `yolo`、`approval` | ADR-0003 §1.2 两档；当前只允许 `yolo`（ADR-0006 §2）。 |
| `agent_teams.runtime_status` | `stopped`、`start_requested`、`preparing`、`ready`、`stop_requested`、`failed`、`unknown` | 请求态由 `runtime` 动作写，结果态由后台写。 |
| `review_requests.object_type` | `task`、`agent_team`、`skill_release`、`plan`、`interpretation` | 前三个是方案三值的令牌；`plan`、`interpretation` 为本文增加。 |
| `review_requests.status` | `pending`、`approved`、`rejected` | 方案：待审、通过、驳回。 |
| `review_requests.decision_note` 来源标注 | `auto:yolo`、`chat:<messageId>` | `auto:yolo` 为 YOLO 档自动许可，`decided_by` 为空；`chat:<messageId>` 只在将来把聊天批准适配为正式调用时使用，本文当前选择聊天不构成决议（4.5，ADR-0022）。 |
| `tasks.status` | `open`、`planned`、`reserved`、`in_progress`、`submitted`、`handed_off`、`done`、`blocked`、`paused`、`cancelled`、`superseded` | 迁移表见 5.1；终态 `done`、`cancelled`、`superseded`。`superseded` 由 `plans apply` 写，投影闸门封死，`task_assignments` 的 `result` 对其返回 409 `INVALID_TRANSITION`。 |
| `plan_steps.status` | `pending`、`ready`、`in_progress`、`done`、`blocked`、`skipped` | `ready` 表示依赖已满足。 |
| `task_assignments.phase` | `initial`、`fix`、`diagnose`、`recover` | 决定 ADR-0007 D3 上限的计数口径。 |
| `task_assignments.attempt_state` | `pending`、`running`、`succeeded`、`failed`、`blocked`、`cancelled`、`superseded` | 后五个为终态。 |
| `task_assignments.dispatch_ref` 键 | `instance`、`team`、`projectId`、`upstreamTaskId` | ADR-0012 要求含实例、team、`project_id`。 |
| `handoffs.status` | `submitted`、`validating`、`passed`、`failed`、`withdrawn` | 迁移见 5.4。 |
| `messages.kind` | `chat`、`instruction`、`question`、`answer`、`report`、`system` | 浏览器可用前四个。 |
| `messages.status` | `saved`、`delivered`、`acknowledged`、`failed` | `saved` 只表示落库（ADR-0018）。 |
| `context_objects.kind` | `document`、`spec`、`handoff_doc`、`reference`、`note` | `handoff_doc` 承接并入的 `handoff_docs`。 |
| `change_sets.status` | `open`、`delivering`、`delivered`、`conflicted`、`archived` | 迁移表见 7.1；终态 `archived`。 |
| `change_sets.repository_ids[]` 键 | `repositoryId`、`commitSha`、`prNumber`、`deliveryStatus` | 方案：仓库、提交、PR 号、交付状态。 |
| `change_sets.repository_ids[].deliveryStatus` | `pending`、`branch_created`、`draft_pr_opened`、`merged_by_human`、`closed` | 合并只由人完成（ADR-0002）。 |
| `change_sets.conflict_kind` | `merge`、`rebase`、`dependency` | 冲突类型。 |
| `change_sets.conflict_status` | `detected`、`fix_task_created`、`resolved`、`abandoned` | 与 `status` 的关系见 7.1。 |
| `scm_commands.command_type` | `create_branch`、`push_branch`、`open_draft_pr`、`update_pr` | 只有 draft PR，无合并命令。 |
| `scm_commands.status` | `pending`、`sent`、`succeeded`、`failed`、`unknown` | `unknown` 先核查再重试（ADR-0016）。 |
| `validation_snapshots.status` | `passed`、`failed`、`undetermined`、`blocked` | 前两个来自方案；后两个对应 ADR-0007 D2 的暂无法判定与环境阻塞。 |
| `database_branch_validations.status` | `requested`、`provisioning`、`running`、`passed`、`failed`、`cleaned_up` | `cleaned_up` 在 `cleanup_pending` 处理完成后写。 |
| `skill_test_questions.kind` | `success`、`business_failure`、`missing_precondition` | 方案：成功、业务失败、前提缺失。 |
| `skill_evaluation_runs.arm` | `with`、`without` | 方案：带、不带 Skill。 |
| `skill_evaluation_runs.result` | `pending`、`pass`、`fail`、`undetermined` | 单题结论。 |
| `skill_approvals.review_status` | `pending`、`approved`、`rejected` | 审核状态。 |
| `skill_approvals.conclusion` | `publish`、`reject` | 最终结论。 |
| `agent_skill_bindings.source` | `approval`、`revision_auto` | 方案：审批放行、修订自动。 |
| `skill_update_suggestions.status` | `pending`、`accepted`、`rejected` | 方案：待审、采纳、驳回。 |
| `events.event_type` 命名 | `<aggregateType>.<变化>`，例如 `task.status_changed`、`message.saved`、`change_set.delivered`；控制帧 `resync_required` | SSE 帧的 `event` 名。 |
| `events.actor_type`、`decision_chain_nodes.actor_type` | `user`、`agent`、`system` | 与操作者字符串前缀一致。 |
| `events.aggregate_type`、`recovery_cases.object_type` | 资源名单数 snake_case，例如 `task`、`task_assignment`、`change_set`、`message`、`repository`、`agent_team`、`plan`、`review_request`、`handoff`、`database_branch_validation`、`skill_version` | 也是 SSE 过滤 `aggregateType` 的取值。 |
| `trace_sessions.status` | `running`、`ended`、`failed` | 追踪会话。 |
| `log_entries.level` | `debug`、`info`、`warn`、`error` | 日志级别。 |
| `recovery_cases.severity` | `low`、`medium`、`high`、`critical` | 严重程度。 |
| `recovery_cases.status` | `opened`、`decided`、`executing`、`closed` | 方案：立案、决策、执行、关闭。 |
| `recovery_operations.result.outcome` | `failed`、`timeout` | 后台在操作失败或超时时写入；写入后案件允许 `close`（9.8）。 |
| `DELETE /api/observe/alert-rules/{param}` | 已实现(2026-09-17 登记) | 删除告警规则。 |
| `GET /api/issues/{param}/discovery` | 已实现(2026-09-17 登记) | 发现读投影(工作台 2.5s 轮询),分步状态见各步端点。 |
| `GET /api/issues/{param}/discovery/tasks/{param}` | 已实现(2026-09-17 登记) | 发现单步任务回执读取。 |
| `GET /api/observability/alerts` | 已实现(2026-09-17 登记) | 平台级告警读面。 |
| `GET /api/observability/logs` | 已实现(2026-09-17 登记) | 平台级日志读面。 |
| `GET /api/observability/traces` | 已实现(2026-09-17 登记) | 平台级轨迹读面。 |
| `GET /api/observability/traces/{param}/events` | 已实现(2026-09-17 登记) | 轨迹事件读面。 |
| `GET /api/observability/usage` | 已实现(2026-09-17 登记) | 用量汇总读面。 |
| `GET /api/observe/alert-rules` | 已实现(2026-09-17 登记) | 告警规则列表。 |
| `GET /api/observe/alerts` | 已实现(2026-09-17 登记) | 告警事件(游标分页)。 |
| `GET /api/observe/alerts/active` | 已实现(2026-09-17 登记) | 活跃告警。 |
| `GET /api/observe/issues` | 已实现(2026-09-17 登记) | 观测 issue 分组。 |
| `GET /api/observe/logs` | 已实现(2026-09-17 登记) | 日志查询。 |
| `GET /api/observe/logs/issues` | 已实现(2026-09-17 登记) | 按 issue 分组的日志。 |
| `GET /api/observe/summary` | 已实现(2026-09-17 登记) | 观测汇总(按天)。 |
| `GET /api/observe/trace/events` | 已实现(2026-09-17 登记) | 轨迹事件查询。 |
| `GET /api/observe/trace/issues` | 已实现(2026-09-17 登记) | 按 issue 分组的轨迹。 |
| `GET /api/observe/trace/sessions` | 已实现(2026-09-17 登记) | 推理会话列表。 |
| `GET /api/observe/trace/sessions/{param}/events` | 已实现(2026-09-17 登记) | 会话事件流。 |
| `GET /api/projects/{param}/branch-validations/{param}` | 已实现(2026-09-17 登记) | 分支验证详情。 |
| `GET /api/projects/{param}/change-sets/{param}/merge-gate` | 已实现(2026-09-17 登记) | merge gate 四门状态(pushed/pr/ciPassed/reviewed/open)。 |
| `GET /api/projects/{param}/interface-documents/{param}` | 已实现(2026-09-17 登记) | 接口文档读取。 |
| `GET /api/projects/{param}/plans/{param}` | 已实现(2026-09-17 登记) | 计划详情(规划快照读取)。 |
| `GET /api/projects/{param}/plans/{param}/handoffs` | 已实现(2026-09-17 登记) | 交接单列表。 |
| `GET /api/projects/{param}/release-gate` | 已实现(2026-09-17 登记) | 发布门禁评估。 |
| `GET /api/projects/{param}/specs/current` | 已实现(2026-09-17 登记) | 当前规格(?repository=)。 |
| `GET /api/projects/{param}/topology` | 已实现(2026-09-17 登记) | 拓扑读取。 |
| `GET /api/v1/review-requests` | 已实现(2026-09-17 登记) | 已实现;字段细节以 internal 实现为准,按板块回填本册。**2026-09-20 起按「项目 + 用户」隔离**:带 `projectId` 时要求 owner 或管理员(否则 404),不带时只回调用者自己名下的项目。 |
| `GET /api/v1/review-requests/events` | 已实现(2026-09-17 登记) | 已实现;字段细节以 internal 实现为准,按板块回填本册。SSE 与列表同一套隔离,按 `projectId` 分片(前端切项目即重订阅)。 |
| `POST /api/delivery/github-webhook` | 已实现(2026-09-17 登记) | GitHub webhook 入口(签名校验;非浏览器端点)。 |
| `POST /api/issues/{param}/archive` | 已实现(2026-09-17 登记) | 归档 issue(墓碑语义,非删除)。 |
| `POST /api/issues/{param}/discovery/analysis` | 已实现(2026-09-17 登记) | 步1 需求分析(含追问澄清)。 |
| `POST /api/issues/{param}/discovery/approval` | 已实现(2026-09-17 登记) | 分档审批(人工节点,带按钮消息)。 |
| `POST /api/issues/{param}/discovery/candidates` | 已实现(2026-09-17 登记) | 步2 仓库候选发现。 |
| `POST /api/issues/{param}/discovery/classification` | 已实现(2026-09-17 登记) | 步3 仓库分档(分类分级)。 |
| `POST /api/issues/{param}/discovery/materialize` | 已实现(2026-09-17 登记) | 物化:建任务、建团队、开房间(进入执行的关键一跃)。 |
| `POST /api/issues/{param}/discovery/plan` | 已实现(2026-09-17 登记) | 步4 规划草案。 |
| `POST /api/issues/{param}/purge` | 已实现(2026-09-17 登记) | 彻底清除(不可逆,仅归档后可用)。 |
| `POST /api/observe/alert-rules` | 已实现(2026-09-17 登记) | 创建告警规则。 |
| `POST /api/observe/alerts/evaluate` | 已实现(2026-09-17 登记) | 评估告警。 |
| `POST /api/organizations/{param}/assembly` | 已实现(2026-09-17 登记) | 组织编制组装(仓库+每仓 Worker 数+Leader 名 → M/L/W 层级)。 |
| `POST /api/projects/{param}/branch-validations` | 已实现(2026-09-17 登记) | 发起数据库分支验证。 |
| `POST /api/projects/{param}/branch-validations/{param}/retry-cleanup` | 已实现(2026-09-17 登记) | 重试清理。 |
| `POST /api/projects/{param}/change-sets` | 已实现(2026-09-17 登记) | 冻结变更集(→ changeSetId)。 |
| `POST /api/projects/{param}/change-sets/{param}/events` | 已实现(2026-09-17 登记) | 记录变更集事件。 |
| `POST /api/projects/{param}/interface-documents` | 已实现(2026-09-17 登记) | 创建接口文档。 |
| `POST /api/projects/{param}/interface-documents/{param}/approve` | 已实现(2026-09-17 登记) | 接口文档审批。 |
| `POST /api/projects/{param}/joint-validation` | 已实现(2026-09-17 登记) | 联合验证。 |
| `POST /api/projects/{param}/plans/{param}/handoffs` | 已实现(2026-09-17 登记) | 创建交接单(测试交接)。 |
| `POST /api/projects/{param}/plans/{param}/interrupt` | 已实现(2026-09-17 登记) | 中断执行{reason}。 |
| `POST /api/projects/{param}/release-gate` | 已实现(2026-09-17 登记) | 发布门禁决策{decision,summary}。 |
| `POST /api/projects/{param}/releases` | 已实现(2026-09-17 登记) | 创建发布。 |
| `POST /api/projects/{param}/specs` | 已实现(2026-09-17 登记) | 创建规格。 |
| `POST /api/projects/{param}/specs/{param}/approve` | 已实现(2026-09-17 登记) | 已实现;字段细节以 internal 实现为准,按板块回填本册。 |
| `POST /api/projects/{param}/tasks/{param}/approve` | 已实现(2026-09-17 登记) | 审批通过任务步{summary}。 |
| `POST /api/projects/{param}/tasks/{param}/reject` | 已实现(2026-09-17 登记) | 驳回任务步{reason}。 |
| `POST /api/projects/{param}/topologies` | 已实现(2026-09-17 登记) | 创建项目拓扑(编制落地:项目+仓库→Manager/Leader/Workers 层级)。**2026-09-20(迁移 0053)**:作用域由组织改为项目——业务链是 账号→项目→issue→仓库,组织只回答「这是哪个账号的数据」;原 `POST /api/organizations/{orgId}/assembly` 同步移除。请求体只留 `repositories` 与 `workersPerRepo`:`organizationId` 不再由调用方给(由项目反查),`leaderName` 也去掉——总领导名字由项目派生,一个项目只能有一个(ADR-0001 D02)。 |
| `POST /api/v1/projects/{param}/checkpoint-decisions` | 已实现(2026-09-17 登记) | 已实现;字段细节以 internal 实现为准,按板块回填本册。 |
| `POST /api/v1/projects/{param}/control` | 已实现(2026-09-17 登记) | 已实现;字段细节以 internal 实现为准,按板块回填本册。 |
| `PUT /api/observe/alert-rules/{param}` | 已实现(2026-09-17 登记) | 更新告警规则。 |

## 附录 E：方案与 ADR、术语的冲突及待决事项

按影响排序编号。每条写现象、出处、本文的处理、需要谁裁定。

1. **角色命名倒置。** 现象：方案 `agents.role` 的 leader 为总、manager 为仓库；CONTEXT 与 ADR-0006 相反，Manager 是项目统一入口，Leader 是仓库负责人。出处：方案 `agents`、`agent_teams.leader_agent_id`、`agent_teams.manager_agent_id`；CONTEXT 项目负责人、仓库负责人；ADR-0006 §1。本文的处理：按方案值写，字段说明标注差异。B11 重规划协议使用 TM（每仓一个 Team Manager）与 Leader（全组织裁决者），与方案一致；不一致的一方是 CONTEXT.md 与 ADR-0006 的用词。需要谁裁定：用户裁定后同步 CONTEXT 或方案。
2. **一项目一仓库。** 现象：`projects.repository_id` 必填唯一；CONTEXT 多仓库项目、ADR-0002 J2、现有 `project_repositories` 与创建契约 §3 的 `repositoryIds` 都是多仓；方案 `agent_teams.repository_id` 可空（项目级编制）与 `change_sets.repository_ids` JSON 又隐含多仓。出处：方案 `projects`、`agent_teams`、`change_sets`。本文的处理：按方案，`POST /api/projects` 只收单个 `repositoryId`；跨仓表达只剩 `change_sets.repository_ids`。需要谁裁定：用户。
3. **无会话实体。** 现象：`messages.room_id` 是字符串；ADR-0019 与消息契约要求 conversation 资源、`replyTo`、澄清。出处：方案 `messages`；ADR-0019；消息契约 §1 至 §4。本文的处理：消息按 `roomId` 过滤，会话列表、澄清端点无落点，附录 C 标注。需要谁裁定：用户与后端设计。
4. **无独立 Issue 实体。** 现象：顶层 `tasks` 承接 Issue；ADR-0019 的 Issue 列表、详情、房间导航需要在 task 上表达；仓库事项（Repository Issue）无落点。出处：方案 `tasks`；ADR-0019；CONTEXT 需求事项、仓库事项。本文的处理：`parentTaskId` 为空的过滤即 Issue 列表；`rooms` 端点无落点。需要谁裁定：用户。
5. **计划生效指针缺失。** 现象：ADR-0003 要求提出、获准、应用、生效四个事实分开；`plans` 无 `status`，`tasks` 无当前生效计划列；`review_requests.object_type` 方案列举无 `plan`；B11 重规划协议的计划状态 `active`、`deprecated`、`superseded` 在方案 `plans` 也没有列。出处：方案 `plans`、`tasks`、`review_requests`；ADR-0003 §1.1、§1.4；CONTEXT 计划生效；B11 重规划协议 §2 状态机。本文的处理：获准经 `review_requests(objectType=plan)`（本文增加值）；应用经 `POST /api/plans/{planId}/apply`；生效与计划状态不在任何端点表达。需要谁裁定：用户与方案作者。
6. **Candidate、Delivery Combination、result admission 缺失。** 现象：ADR-0008 的收录、接受、入选、验证通过、可开 PR 五个事实与 ADR-0015 的结果采纳在方案中只剩 `change_sets.repository_ids` 每仓一份条目。出处：方案 `change_sets`；ADR-0008 记录关系；ADR-0015；CONTEXT 候选结果、交付组合。本文的处理：条目含 `deliveryStatus`（附录 D），不声称覆盖五个事实。需要谁裁定：用户与方案作者。
7. **Skill 7 张表与 ADR-0009 暂缓冲突。** 现象：ADR-0009 要求恢复前不冻结表、API 与状态枚举；方案新增 7 张表。ADR-0009 H1、H11 要求 Issue 引用确定版本，方案改为 Skill 绑 Agent（`agent_skill_bindings`），不绑 Issue。出处：方案 ⑥；ADR-0009 静态证据节、H1、H11。本文的处理：第 8 节按方案给出设计并标为非采用记录，ADR 更新前不得实施。需要谁裁定：用户补充 ADR-0009 的替代关系，并裁定绑定粒度。**2026-09-16 现实更新**：技能治理已实现并合入 main（`internal/skills`，22 端点）——"不得实施"已成既成事实；仍待裁定的是 ADR-0009 替代关系与绑定粒度（绑 Agent 已是实现口径）。
8. **登录方式。** 现象：方案为本地密码登录；现有 B02 GitHub OAuth 登录、connections、bindings、attempts、discovery 4 表无落点；ADR-0002 有效权限交集需要安装范围数据。出处：方案 `users`、`credentials`；B02 采用记录；首批契约 §2、§3。本文的处理：`POST /api/sessions` 用户名密码；GitHub 相关端点在附录 C 标待裁定。需要谁裁定：用户。**裁定（2026-09-16，用户）**：保留现有 GitHub OAuth 登录与相关端点，本地用户名密码登录作废（见 3.2 裁定注）。结案。
9. **模型供应商、来源、密钥版本 20 张表无落点。** 现象：现有 `repomesh_models.*`、`repomesh_sources.*`、`repomesh_secrets.*` 在方案中没有对应表；`llm_usage.provider`、`model` 只是字符串；`credentials.value_encrypted` 的密钥管理未定义。出处：方案 `credentials`、`llm_usage`；现有迁移 `0001` 至 `0008`。本文的处理：模型端点在附录 C 标待决，`credentials` 不定义轮换。需要谁裁定：用户与方案作者。**裁定（2026-09-16，用户）**：保留现有 `repomesh_models` / `repomesh_sources` / `repomesh_secrets` 三 schema 与模型六端点（C 板块 as-built），44 张表清单相应修正为“47 张 + 现有保留”；模型资源小节按现状回填，`credentials` 轮换仍待方案作者。
10. **`feature_settings` 不在 44 张内。** 现象：ADR-0023 的决策链开关与现有 scope-assist 开关依赖 `public.feature_settings`。出处：ADR-0023 决策 5；现有 `PUT /api/settings/decision-chain`。本文的处理：列出 `GET /api/settings/decision-chain`、`PUT /api/settings/decision-chain`，落点待定。需要谁裁定：方案作者补表或用户改用其他落点。**裁定（2026-09-16，用户）**：`public.feature_settings`（feature 主键、enabled、updated_by、updated_at）补入方案表清单，作为平台级功能开关表；现被 decision_chain（0008）与 skill_governance（0013）使用。结案。
11. **原操作回执表无落点，仅 4 张表有 `idempotency_key`。** 现象：现有 `creation_operations`、`update_operations`、`save_operations` 无对应表；创建契约 §5 与首批契约 §6 的原操作查询、410 占位规则无法落地；其余 40 张表的创建重放保护缺失。出处：方案 `agents`、`agent_teams`、`tasks`、`change_sets`；ADR-0016 持续授权补充。本文的处理：要求全部创建与动作 POST 带头；只对 4 表声明重放语义。需要谁裁定：方案作者补表或用户接受 Redis 等外部落点（方案把 `idempotency_records` 迁出到 Redis）。
12. **状态枚举大多未给值。** 现象：附录 D 全是提案。出处：方案各 `status` 列。本文的处理：正文引用附录 D，不写成已采用。需要谁裁定：用户逐表确认。
13. **Agent 调用凭证。** 现象：`agents` 无令牌字段；MCP 入口协议未冻结。出处：方案 `agents`；ADR-0006 §3；ADR-0011；执行门槛 G2。本文的处理：Agent 不调用 HTTP，后台专用端点见 2.2。需要谁裁定：用户与后端设计。
14. **现有 40 张表到 44 张目标表没有迁移映射。** 现象：方案的“原表”名（`local_human_accounts`、`agent_principals` 等）不是本仓库的表名。出处：方案各表“由哪些表来”；现有迁移 `0001` 至 `0008`。本文的处理：不定义迁移。需要谁裁定：方案作者。**2026-09-16 更新**：迁移 `0009`–`0014` 已按方案落地 47 张表（纯增量，无 DROP），目标表已存在；仍缺的是**存量数据迁移映射**（Python 版旧库 → 新表），需方案作者或用户另立迁移方案。
15. **建项前分析作业无作业表。** 现象：现有 `scan-jobs`、`scope` 三条对应 ADR-0020 的建项前分析，方案没有作业表与结果表。出处：ADR-0020；方案 `repositories.profiled_at`。本文的处理：只保留 `POST /api/repositories/{repositoryId}/profile`，作业状态经 `events` 观察。需要谁裁定：用户。
16. **决策链检索端点关系与 ADR 时序说明待决。** 现象：ADR 索引已收录 0021 至 0023；仍待决的是现有 `GET /api/decision-chains/similar` 与 `semantic-search` 的关系，以及 ADR-0021 与 ADR-0020“向量库不作首期依赖”的时序说明。出处：ADR 索引 0020、0021、0023 行；ADR-0021 回滚与实现边界。本文的处理：`similar` 暂并入 `semantic-search`（附录 C）；检索规则按 ADR-0021 写（9.12）。需要谁裁定：用户。**裁定（2026-09-16，用户）**：`similar` 与 `semantic-search` 保持两条独立端点，不合并——similar 的结构化召回（同仓库 Jaccard）是无 embedding 环境下的唯一召回路径，且落库即可命中；语义召回存在 refresh 前的新鲜度窗口。结案（9.11 已按裁定更新）。
17. **`decision_embeddings.embedding` 类型不一致。** 现象：方案写为 JSON；ADR-0021 与 ADR-0023 采用 `vector(1024)` 加 HNSW 余弦索引，JSON 只作无扩展环境的兜底双写；维度绑定嵌入模型。出处：方案 `decision_embeddings`；ADR-0021 决策与理由；ADR-0023 决策 4。本文的处理：字段说明按 ADR 写，`embedding` 不暴露；检索按数据库内排序与降级规则写（9.12）。需要谁裁定：方案作者更新列类型。
18. **`feat/replan-mainline` 分支的任务表与本文未核对。** 现象：施工计划登记 `feat/replan-mainline` 分支的迁移 0009 建 `public.tasks`、`public.task_assignments`，并要求不另起第二套任务表。出处：施工计划；B11 重规划协议 §4 待建表。本文的处理：5.1、5.3 按方案字段写，与该分支的列是否一致未核对（分支未合入 main）。需要谁裁定：合入前由用户或方案作者核对列差异。

### 写作中发现

以下问题在成稿时发现，上面 18 条未覆盖。编号被正文引用。

1. **项目管理员与计划审批人资格无落点。** 方案没有项目成员或项目角色表；ADR-0005 §1、ADR-0003 §1.8 需要项目管理员与获授审批权限的人。本文暂以 `users.org_role` 组织管理员代替。需要方案作者补表或用户接受组织级权限。
2. **`tasks` 没有暂停前状态列。** `resume` 的目标状态只能由 `assignee_agent_id`、`reserved_at` 与当前派单 `attempt_state` 推导（5.1）；从 `planned` 暂停后恢复会回到 `open`。需要方案作者决定是否补列。
3. **`plans.execution_plan_id` 无目标表。** 方案把 `execution_plans` 并入 `plans`，该列的“对应执行计划”在 44 张表中没有落点。本文标为后台写、只读。需要方案作者澄清。
4. **后台专用端点的 HTTP 调用方未定义。** 2.2 规定后台协调进程不经 HTTP、Agent 经 MCP；多个端点的调用方又是“后台协调进程代 Agent”或“Manager 工具”。本文把它们标为 `后台`，Web 不向浏览器开放。需要用户决定是否引入内部服务凭证。
5. **多张表无 `organization_id`。** `repositories`、`skills` 及其 6 张从表、`credentials`、`alert_rules`、`alert_events`、`trace_sessions`、`trace_events`、`log_entries`、`recovery_cases` 及 2 张从表、`decision_chain_nodes`、`decision_embeddings`、`mcp_server_policies`、`llm_usage` 没有租户列；`plan_steps`、`handoffs`、`context_*`、`scm_*`、`delivery_policies` 只能经父表关联。本文把无法关联的表按平台级处理。需要用户裁定租户隔离范围。
6. **四张幂等表没有输入摘要列。** “同键异输入 409”只能比较已存行的对应列；无法保存创建契约 §3 的规范化摘要与 `schemaVersion`。需要方案作者决定是否补列。
7. **YOLO 自动许可的记录主体。** `review_requests.decided_by` 指向 `users`，ADR-0006 §2 当前阶段的自动许可没有用户主体。本文已用空 `decided_by` 与 `decision_note=auto:yolo` 表达（4.5），待确认。ADR-0022 要求所有决议落本表，自动许可行与此一致。
8. **`messages.room_id`、`subject` 必填。** ADR-0018 规定首条消息提交后才异步准备房间，此时 `room_id` 不存在；消息契约 §2 只有 `content` 一个字段。本文照方案要求两者必填。需要方案作者决定改为可空。
9. **一人一会话。** 会话表 1:1 并入 `users`，第二个浏览器登录使第一个失效。需要用户确认可接受。
10. **`skill_approvals.version_id` 唯一。** 驳回后同一版本不能再次发起审核，需新建版本。需要用户确认。
11. **`database_branch_validations` 必填列在创建时未知。** `provider_branch_ref`、`engine_version` 必填，但在后台建分支前没有值，本文由服务端写空字符串占位；`results`、`cleanup_pending` 同为必填，本文以 `{}`、`false` 为创建初值。需要方案作者改为可空或确认初值。
12. **唯一列冲突无专用错误码。** 固定状态码表没有唯一冲突码，本文用 422 `VALIDATION_FAILED` 与 `fieldErrors[].code=DUPLICATE`。需要用户确认。
13. **登录端点不要求 `Idempotency-Key`。** 通用约定要求所有创建型 POST 带头，本文把 `POST /api/sessions` 排除。需要用户确认。**结案（2026-09-16）**：该端点已作废（3.2 裁定注）；现行 GitHub OAuth 登录要求恰好一个 `Idempotency-Key`（`internal/web/auth.go`）。
14. **组织与首个管理员的引导无落点。** 方案把 `bootstrap_operations` 迁出到内存，没有建立组织的端点或表。需要用户决定引导方式。
15. **`validation_snapshots.object_id` 无对象类型。** 无法判断 `object_id` 指向哪张表，`objectId` 过滤跨类型。需要方案作者补 `object_type`。
16. **隶属关系两处表达。** `agents.parent_agent_id` 与 `agent_teams` 的 leader、manager、worker 名单可能不一致；本文不校验。需要方案作者确定以哪个为准。
17. **`context_objects.version_no` 未纳入乐观锁。** `PUT .../content` 无并发保护。需要用户决定是否扩大 2.5 的乐观锁范围。
18. **`events.id` 为 UUID 无序。** `Last-Event-ID` 重放依赖 `recorded_at`；同一时刻多条事件的重放边界需实现定义。需要后端设计确认。
19. **`change_sets.pr_url` 单列。** 多仓变更集有多个 PR，单列只能放一个；每仓 PR 号在 `repository_ids` 条目中。需要方案作者决定去掉该列或改为 JSON。
20. **任务动作没有原因字段。** `pause`、`cancel` 的原因没有列，本文不接受 `reason`。需要用户决定是否写入 `events.payload`。
21. **`agent_teams` 没有 `resource_ref`。** ADR-0012 的上游 Project 引用需要编制级落点，方案中 `resource_ref` 列只属于 `agents`。本文把成员级引用放在 `agents.resource_ref`，派单级放在 `task_assignments.dispatch_ref`；编制级引用无落点。需要方案作者或用户裁定。
22. **`GET /api/repositories/url-type` 无对应端点。** 本文建议并入 `POST /api/repositories` 的校验。需要用户确认。
23. **`events` 无 `project_id`，`tasks`、`messages` 无时间列。** 项目级订阅只能按 `taskId` 或 `correlationId` 逐个建立；`tasks`、`messages` 列表只能按 `id` 排，无法做时间线。建议方案补列。
24. **`decision_chain_nodes` 无 `status` 列。** B11 重规划协议要求 BLOCKED 上报与重规划分别落 `status=blocked`、`status=adjusted` 节点（4.3、5.3）；方案该表只有 `action`、`rationale`、`context_ref`。本文沿用协议用词，落点待方案作者补列或改用 `action`。**更正（2026-09-16）**：0008 迁移的 `decision_chain_nodes` **已有** `status` 列（`confirmed`/`rejected` 等，全枚举见附录 D 与 ADR-0023 设计）；本条按已解决处理，B11 的 `blocked`/`adjusted` 落点直接使用该列。
