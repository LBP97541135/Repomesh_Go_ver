# RepoMesh Go 版开发指南（M1-M9）

面向：晚上按步骤开发的实现者。
前置阅读：`docs/development/2026-09-16-dag-orchestration-design/DESIGN.md`（M1 定稿设计）、`docs/development/2026-09-16-full-pipeline-mapping/FULL-PIPELINE-MAP.md`（全流程映射，每个细节的依据）。
工程约定：每次改动后 `go build ./... && go vet ./... && go test ./... -count=1` 必须全绿；新迁移文件名连续（当前到 0022）；每个模块完成后写 ACCEPTANCE.md（格式参照 docs/development/2026-09-16-b06-issue-creation/）。

---

## M1：DAG 编排核心（无外部依赖，可立即开始）

**目标**：计划/DAG/九态任务机在 RepoMesh 内跑通，调度器能把 ready 任务写入执行账本。

### 步骤 1.1 迁移 0023_dag_orchestration.sql

新建 `internal/database/migrations/0023_dag_orchestration.sql`，schema `repomesh_orchestration`，四张表（DDL 已在 DESIGN.md §1，逐字采用）：
- `plans`：(project_id, issue_id, version) 唯一；部分唯一索引保证同 Issue 只一个 active；FK 延迟引用 `repomesh_issues.issues(project_id, id)`。
- `plan_tasks`：kind 二值（repository_task 带 repository_id+manager_id / joint_validation 二者皆 NULL）；depends_on jsonb（任务 ID 数组）。
- `task_runs`：(task_id, attempt_no) 唯一；九态 CHECK；active 部分索引（一任务一个进行中 run）；artifacts jsonb。
- `task_run_events`：append-only，(run_id, seq) 主键，source ∈ leader/manager/scheduler/agent。
- 触发器（DEFERRABLE）：joint_validation 的 depends_on ≥2；blocked 必有 blocked_reason；awaiting_input 必有 awaiting_request_id。
**验收**：db migrate 后 check 显示 target=23；手工 SQL 插环依赖被触发器拒绝。

### 步骤 1.2 internal/orchestration 包骨架

新建四个文件：
- `types.go`：Plan/PlanTask/TaskRun/RunEvent 结构体 + 九态常量 + Failure 类型（照抄 internal/execution/errors.go 的 Failure 模式）。
- `plans.go`：`SubmitPlan(ctx, principal, cmd)` —— 环检测（拓扑排序 Kahn 算法，DAG01）→ 校验所有 repository_id 属于项目（复用 internal/issues/query.go 的 readProjectRepositories 模式）→ 单事务插 plan(v=历史最大+1) + plan_tasks + 每 task 一个 task_runs(pending)。幂等：Idempotency-Key 唯一约束 + 重放返回原计划。
- `runs.go`：`ApproveTask`（blocked→succeeded，Manager）/ `RejectTask`（blocked→failed）/ `RetryTask`（failed→新 run attempt_no+1，重新走依赖评估置 pending）/ `AnswerTask`（awaiting_input→dispatched 新 run，关联 B09 的 logical_request_id）/ `CancelPlan`（active 计划全部进行中 run → cancelled + 调 execution.Service.RequestStop）。
- `state.go`：转移函数 `transitionRun(run, from, to)` —— 白名单转移表（DESIGN.md §2 的图），非法转移返回 409；每次转移写 task_run_events。

**验收**：单测覆盖 DAG01-06（环拒绝、串行派发条件、blocked 不解锁、reject/retry、答复重派、双 Leader 并发）——用 testdb 包（internal/testdb/postgres.go，Unix 标签，Windows 上只保证编译）。

### 步骤 1.3 依赖评估与调度器

- `internal/orchestration/scheduler.go`：`EvaluateReady(ctx)` —— 单事务：`FOR UPDATE SKIP LOCKED` 扫全部 pending run → 检查 depends_on 引用的任务是否都有 succeeded 终态 run（blocked 不算）→ 全满足则置 ready。返回派发数量。
- coordinator 集成：`cmd/repomesh-coordinator/main.go` 的轮询循环里加一个每秒 tick 调 EvaluateReady（现有循环结构照抄 access/models 双轮询模式）。
- 派发：ready 的 run → 调 `execution.Service.Reserve`（worker 空闲才成功）→ `execution.WriteTaskPackage`（workspace 按 attempt 约定路径）→ `execution.StartAgentRun`（agent_kind 取 plan_tasks.agent_kind）→ run 置 dispatched + agent_run_id 回填。无空闲 worker 则停在 ready。
**验收**：DAG02/03/07/10（串行派发、blocked 不解锁、重启不重复派发、双派竞争）。

### 步骤 1.4 web 路由（10 条）

新建 `internal/web/orchestration.go`，全部走 registerProjectRoute 族（照抄 internal/web/issues.go 模式）：
POST plans（幂等键）、GET plans（列表）、GET plans/{id}（任务图+run 状态）、POST activate、POST cancel、POST tasks/{id}/approve、/reject、/retry、/answer、GET delivery-evidence（交付证据投影：任务×run×agent_run×artifacts 连接查询）。
server.go 的 handlerConfigured 加 Orchestration 参数（照 Issues/Messages 追加模式），main.go 装配。
**验收**：go build/vet/test 全绿 + 新路由 401 未认证行为正确。

### 步骤 1.5 验收文档

`docs/development/<日期>-m1-dag-orchestration/ACCEPTANCE.md`（需求/做了什么/影响范围/关键决策/偏差清单/commit/验证结果）。

---

## M2：repomesh-runner（依赖：AT 上游部署）

**目标**：host-executor 升级为 AT 可管理的 Runner 进程，被 Controller 当 Worker 承载。

### 步骤 2.0 前置：部署 AgentTeams 上游

- 源码：`components/agentteams`（checkout @ d8b7630 锁定版本，见 third_party/agentteams-source.json）。
- 服务器部署 Controller + Matrix homeserver + MinIO（AT 的 helm chart：helm/agentteams/，values.yaml L304 已有 agentteams-repomesh-worker 镜像位）。
- 产出三件套：Controller 地址、RepoMesh 的 AT 管理账号 token（role=manager）、Matrix 账号。

### 步骤 2.1 Runner 入口适配

- cmd/repomesh-host-executor 增加环境变量约定（AT 以 PID 1 拉起本进程时注入）：`REPOMESH_RUNNER_WORKER_ID`、`REPOMESH_DATABASE_URL`。
- main.go 启动时若检测到 AT 注入的环境（AGENTTEAMS_* 变量存在）→ 跳过 worker 自注册（Controller 已管理），直接进 RunOne 循环；否则维持现有独立模式。
**验收**：两种模式都能启动；AT 模式下 status 上报走 Controller。

### 步骤 2.2 生命周期对齐

- Controller `POST /api/v1/workers/{name}/sleep` → Runner 收 SIGTERM → 优雅停：停止领新任务、进行中 agent 走既有停止协议（kill 进程组→RevokeWrite→释放资源）。
- `GET /api/v1/workers/{name}/status` 的数据源：agent_runs/attempts 实时状态（Runner 心跳时回写 AT 期望的字段）。
**验收**：wake/sleep 循环不丢任务、不重复执行。

---

## M3：AT 适配器（依赖：M2.0 的三件套）

### 步骤 3.1 internal/atadapter 包

- `controller.go`：REST 客户端——CreateWorker（POST /api/v1/workers，带 runtime=repomesh-runner + identity + AccessEntries 限定 scope）、GetWorkerStatus、Wake/Sleep。Bearer token 从 secrets store 读（不落代码）。
- `matrix.go`：Matrix Client API 最小封装——登录、join、send message（m.mentions）、read messages（/rooms/{id}/messages）。给 RepoMesh 一个专属 Matrix 账号入 Task Room。
- `filesync.go`：读 MinIO `shared/tasks/{id}/meta.json` 作为执行镜像（S3 SDK），仅用于对账展示；RepoMesh DB 永远是事实源。
**验收**：对 AT 环境的集成冒烟（建 Worker→status→sleep）。

### 步骤 3.2 Manager 沟通层接线

- B09 会话消息 → 同步发到对应 Task Room（room↔issue 映射表：新表或复用 AT roomflow 的 project-rooms 绑定文件）。
- Matrix 收到的 Manager 回复 → 写回 conversation_messages（author_kind='user'，actor=Matrix 账号映射的本地 actor）。
**验收**：RepoMesh 页面发消息，Manager 在 Matrix 客户端可见；反向亦然。

---

## M4：组织/仓库角色自动装配（依赖 M3）

### 步骤 4.1 装配服务

- `internal/orchestration/assembly.go`：`AssembleOrganization(ctx, orgLogin)` —— 扫描完成后（discovered_repositories 按组织分组）：
  1. 组织级：确保一个 Leader 绑定（本地 accounts 里创建/复用 + AT 侧建 Leader Worker 或绑定 Human）。
  2. 仓库级：每仓库建 AT Team（Controller CreateTeam——agt/REST）+ 一个 Manager Worker（runtime=repomesh-runner）+ N 个开发 Worker。
- 校验规则照抄 Py domain.py L186：leader 不得兼任 worker；每仓库唯一 Manager。
- 新表 `repomesh_orchestration.role_bindings`（actor ↔ AT user/room ↔ 仓库/组织 ↔ 角色三列唯一约束）。
**验收**：给定组织 token，一次调用产出完整拓扑；重复调用幂等。

### 步骤 4.2 范围确认流程接线

新 Issue 创建后自动：拉群（建 Task Room）→ 邀请该项目全部 Manager + Leader 的 Matrix 账号 → B09 澄清问题同步到房间 → 全部 Manager 确认范围（复用 B09 clarification 的 answer 语义，新增多方确认表：每 Manager 一票，全票才 resolved）。

---

## M5：测试 agent + Manager spec（依赖 M1）

### 步骤 5.1 spec 生命周期

- 0024 迁移：`repomesh_orchestration.task_specs`（task_id FK、version、body md、state: draft→in_review→approved、author=Manager、diff 用前后版本对比）。
- 路由：POST specs（Manager 起草）、POST specs/{id}/submit、POST approve（Leader 核准后任务才可派发）、GET specs/{id}/diff。
- 任务包生成改从 approved spec 渲染（WriteTaskPackage 增加 spec 输入；Issue 内容作为背景章节保留）。

### 步骤 5.2 双派与放行证据

- plan_tasks 增列 `test_agent_kind`（可空）——非空时调度器对每个任务派两个 run：开发 run + 测试 run（测试 run depends_on 开发 run 的 run_id，非任务级依赖）。
- 测试 run 的任务包 = 测试指令（跑什么命令、看什么 diff）；产出 artifacts 里的测试报告 + diff 摘要。
- Manager approve 页面/路由读两个 run 的证据；Py 对照：database_test_team 的 plan/approval/evidence 三件套 + api 四端点。

---

## M6：交付面——平台代 push/PR（依赖 M1）

### 步骤 6.1 push 与 PR 服务

- 0025 迁移：`repomesh_delivery.deliveries`（issue_id、plan_id、state: collecting→human_approved→delivered→pr_created→merged/failed、pr_url、commits jsonb）+ `delivery_commits`（仓库、分支、head sha、验证 run 引用）。
- `internal/delivery/service.go`：
  - `Collect(ctx, issueID)`：汇总该 Issue 全部 succeeded 任务 workspace 的 commit 清单（git log 工作区）。
  - `RequestHumanGate(ctx, deliveryID)`：总 Leader 总结（各仓测试结论+diff 统计）写会话通知人——Py: HumanDecisionNotifier 对等。
  - `Deliver(ctx, deliveryID)`：人工放行后——用 access 包的 GitHub App 安装凭证（InstallationToken，凭据不落 agent/不进日志）对每仓库 push 分支（分支名 `repomesh/issue-{id}`）→ GitHub API 建 PR（head/base/reviewers）。
- 路由：POST deliveries、POST {id}/human-gate、POST {id}/approve（人工放行）、GET {id}。
**验收**：对测试仓库真实建 PR 一次（需要 GitHub App 已配置）。

---

## M7：接口文档多方审批（依赖 M3 会话层）

- 0026 迁移：`repomesh_orchestration.interface_docs`（project_id、version、body、proposer=Manager、state: proposed→confirmed_by_all）+ `interface_doc_confirmations`（doc_id、manager_id、confirmed_at；(doc_id, manager_id) 唯一）。
- 流程：一个 Manager 提交 → 通知其他 Manager（Matrix+会话）→ 各自 confirm → 全票置 confirmed_by_all → 依赖该文档的任务才允许 ready（调度器检查）。
- 这是超出 Py 版的新能力（Py specification 只有单文档流），按用户原话实现。

---

## M8：数据库分支验证服务（依赖 M1）

- 表已在库（database_branch_validations，0012 迁移），写服务层：
- `internal/dbbranch/service.go`：七态生命周期（requested→provisioning→ready→validating→passed/failed→cleaning→cleaned）——provider 端口接口（ProvisionBranch/RunStages/DeleteBranch），本地 PostgreSQL provider 先行（CREATE DATABASE ... TEMPLATE 模板克隆），PolarDB 适配器留接口。
- 与 M1 接线：joint_validation 任务可携带 db_branch 需求 → 调度器先走分支验证再放行任务。
- Py 对照：review_validation/application.py DatabaseBranchValidationService（start/get/retry_cleanup/_execute/_cleanup）+ meta.json 证据协议。
- 评委建议②配套：GET delivery-evidence 扩展 db 分支验证记录。

---

## M9：观测/告警最小面（无硬依赖）

- 表已在库（events/llm_usage/trace_sessions/trace_events/alert_rules/alert_events，0014 迁移）。
- `internal/observe/service.go` + 路由：GET /observe/summary（任务吞吐、agent 运行时长分布、llm 用量）、GET /observe/alerts、POST /observe/alert-rules（阈值评估器：每分钟 coordinator tick 比对）。
- agent_runs/attempts 的事件流已写 AT 的 OTel 语义对齐（task.id/project.id span 标签），M9 只做查询投影。

---

## 推进顺序与并行度

```
M1（纯本地，立即可做）
 ├─→ M5（spec+测试 agent）
 ├─→ M6（交付面）          ← M2.0 部署 AT 后
 ├─→ M8（分支验证）
 └─→ M2/M3/M4（AT 线：先部署上游，再适配器，再装配）
M7（等 M3 会话层）
M9（随时可插）
```

晚上开发建议从 M1 步骤 1.1 开始，1.1-1.4 是一条完整可验收的链；M2.0 的 AT 部署可并行准备。
