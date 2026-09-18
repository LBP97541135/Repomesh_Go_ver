# 模块 1 设计文档：DAG 派工链（完整对等移植）

状态：设计定稿（经四轮问答确认），待实现。本文件是实现唯一依据。

## 0. 已确认决策汇总

| # | 决策点 | 结论 |
|---|---|---|
| 1 | 移植深度 | 完整对等移植 Python 版 task_orchestration 全部能力 |
| 2 | 计划归属 | Issue 级：一个 Issue 一份计划链，任务产出关联 Issue ChangeSet |
| 3 | 执行粒度 | 一任务一运行：Task = 一次 attempt + 一次 agent run |
| 4 | 角色层级 | 三级：总 Leader（提交计划/审批/协调）→ 仓库 Manager（仓库内管理/核验放行）→ Worker（真实 coding agent 进程） |
| 5 | 状态机 | 九态：pending → ready → dispatched → running → succeeded/failed/cancelled，含 blocked / awaiting_input |
| 6 | 依赖形状 | 任意 DAG：depends_on 任务 ID 列表 |
| 7 | 拦截时点 | 完成后必拦：succeeded 后自动置 blocked，Manager 核验产出后放行下游 |
| 8 | 澄清闭环 | 接 B09 链路：awaiting_input 与 logical_work_requests 共用问答记录 |
| 9 | 重试策略 | 人工重试：同一任务新 agent run，失败尝试全留证据 |
| 10 | 派发通道 | 直连账本：调度器写 agent_runs(pending)+任务包，host-executor 轮询领取 |
| 11 | 计划修订 | 版本化不可变：提交后不可改，修改开新版本，旧版留证据 |
| 12 | 跨仓演示 | 仓库任务 + 联合验证任务：验证任务依赖多仓任务，读产出做契约比对 |

## 1. 数据模型（迁移 0023_dag_orchestration.sql，schema: repomesh_orchestration）

### 1.1 plans（计划版本，不可变）

```sql
CREATE TABLE repomesh_orchestration.plans (
    id text PRIMARY KEY,                    -- pln_ 前缀
    project_id text NOT NULL,
    issue_id text NOT NULL,                 -- Issue 级归属（FK 延迟引用 issues）
    version integer NOT NULL CHECK (version > 0),
    state text NOT NULL CHECK (state IN ('draft','submitted','active','superseded','cancelled')),
    created_by text NOT NULL,               -- 总 Leader actor
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    superseded_by text,
    CONSTRAINT plans_project_issue_version UNIQUE (project_id, issue_id, version),
    CONSTRAINT plans_issue_fk FOREIGN KEY (project_id, issue_id)
        REFERENCES repomesh_issues.issues(project_id, id) DEFERRABLE INITIALLY DEFERRED
);
-- 同一 Issue 同时只有一个 active 计划
CREATE UNIQUE INDEX plans_one_active_per_issue
    ON repomesh_orchestration.plans(project_id, issue_id) WHERE state = 'active';
```

### 1.2 plan_tasks（DAG 任务节点，版本内不可变）

    CREATE TABLE repomesh_orchestration.plan_tasks (
        id text PRIMARY KEY,
        plan_id text NOT NULL REFERENCES repomesh_orchestration.plans(id),
        project_id text NOT NULL,
        issue_id text NOT NULL,
        seq integer NOT NULL CHECK (seq > 0),
        kind text NOT NULL CHECK (kind IN ('repository_task','joint_validation')),
        repository_id text,
        manager_id text,
        title text NOT NULL,
        instruction text NOT NULL,
        acceptance jsonb NOT NULL DEFAULT '[]',
        depends_on jsonb NOT NULL DEFAULT '[]',
        agent_kind text NOT NULL,
        CONSTRAINT plan_tasks_plan_seq UNIQUE (plan_id, seq),
        CONSTRAINT plan_tasks_kind_repo CHECK (
            (kind = 'repository_task' AND repository_id IS NOT NULL AND manager_id IS NOT NULL)
            OR (kind = 'joint_validation' AND repository_id IS NULL AND manager_id IS NULL)
        )
    );
```
### 1.3 task_runs（任务运行实例：一任务多 run，失败重试产生新 run）

    CREATE TABLE repomesh_orchestration.task_runs (
        id text PRIMARY KEY,                    -- run_ 前缀（复用 B10 agent_runs 的 run 空间语义）
        task_id text NOT NULL REFERENCES repomesh_orchestration.plan_tasks(id),
        attempt_id text NOT NULL REFERENCES repomesh_execution.attempts(id),
        agent_run_id text REFERENCES repomesh_execution.agent_runs(id),
        attempt_no integer NOT NULL CHECK (attempt_no > 0),
        state text NOT NULL CHECK (state IN (
            'pending','ready','dispatched','running',
            'succeeded','failed','cancelled','blocked','awaiting_input')),
        blocked_reason text,
        awaiting_request_id text,               -- B09 logical_work_requests.id
        started_at timestamptz,
        finished_at timestamptz,
        result_summary text,
        artifacts jsonb NOT NULL DEFAULT '[]',  -- 产出文件清单（交付证据）
        CONSTRAINT task_runs_task_attempt UNIQUE (task_id, attempt_no)
    );

    CREATE INDEX task_runs_active ON repomesh_orchestration.task_runs(task_id) WHERE state IN ('pending','ready','dispatched','running','blocked','awaiting_input');

### 1.4 聚合完整性触发器（沿 B06/B10 模式，SQL 层拒绝非法状态）

- plan_active_tasks_consistent：plan 置 active 时必须有 ≥1 个 plan_task；superseded 时不得有进行中 run。
- task_run_single_live：一任务同时只能有一个非终态 run（部分唯一索引已覆盖）。
- blocked_requires_reason：state=blocked 时 blocked_reason 非空；awaiting_input 时 awaiting_request_id 非空。
- joint_validation_depends：kind=joint_validation 的任务 depends_on 至少 2 个 repository_task。

## 2. 状态机与转移规则

    pending ──(依赖全部 succeeded→blocked 放行)──> ready ──(调度器领取)──> dispatched
    dispatched ──(agent_started 事件)──> running
    running ──(agent 退出 0)──> blocked（完成后必拦，等 Manager 核验）
    running ──(agent 退出非 0 且声明需澄清)──> awaiting_input（挂 B09 请求）
    running ──(agent 退出非 0 其他)──> failed（人工重试→新 run）
    blocked ──(Manager 核验放行)──> succeeded（终态，解锁下游依赖评估）
    blocked ──(Manager 打回)──> failed（可重试）
    awaiting_input ──(Leader 会话答复后重派)──> dispatched（新 run，同任务）
    任意非终态 ──(Leader 取消计划/任务)──> cancelled

规则要点：
1. 依赖评估看的是**下游任务引用的上游任务**是否有 succeeded 终态 run；blocked 不解锁。
2. dispatched→running 由 host-executor 的 agent_started 事件驱动；host-executor 崩溃恢复时按 agent_runs 实际 pid 存活核对，不凭空重置。
3. 每次转移写 task_run_events（attempt_id, seq, kind, source=leader/manager/scheduler/agent），append-only。

## 3. 服务层（internal/orchestration 包）

### 3.1 公开 API（web 路由，全部走 registerProjectRoute 族）

| 方法 | 路径 | 角色 | 语义 |
|---|---|---|---|
| POST | /api/projects/{pid}/issues/{iid}/plans | Leader | 提交计划（幂等键，创建 plan vN + plan_tasks + 全部 task_runs(pending)） |
| GET | /api/projects/{pid}/issues/{iid}/plans | 读取 | 计划版本列表 + 当前 active |
| GET | /api/projects/{pid}/plans/{planId} | 读取 | 计划详情：任务图 + 各任务当前 run 状态 |
| POST | /api/projects/{pid}/plans/{planId}/activate | Leader | draft→active（评审通过后） |
| POST | /api/projects/{pid}/plans/{planId}/cancel | Leader | 取消全部进行中 run |
| POST | /api/projects/{pid}/tasks/{taskId}/approve | Manager | blocked→succeeded（核验放行，写 artifacts 确认） |
| POST | /api/projects/{pid}/tasks/{taskId}/reject | Manager | blocked→failed（打回，附原因） |
| POST | /api/projects/{pid}/tasks/{taskId}/retry | Leader/Manager | failed→新 run（attempt_no+1，重新走依赖评估） |
| POST | /api/projects/{pid}/tasks/{taskId}/answer | Leader | awaiting_input→dispatched（关联 B09 答复） |
| GET | /api/projects/{pid}/issues/{iid}/delivery-evidence | 读取 | 交付证据投影（评委建议②：任务×run×agent×产物清单） |

### 3.2 调度器（coordinator 进程内新 goroutine）

每秒一轮（与既有 access/models 轮询并存）：
1. 扫 ready 候选：pending run 且 depends_on 全 succeeded → 置 ready（单事务，FOR UPDATE SKIP LOCKED 防双派）。
2. 派发 ready：创建 execution.attempt（Reserve，需要 worker 空闲）→ 创建 agent_runs(pending)（含任务包路径）→ run 置 dispatched。
3. 无空闲 worker 时 run 停在 ready，不假装排队位置。

### 3.3 与既有模块的接缝

- B06 issues：plan 的 Issue FK 复用 repomesh_issues.issues；任务包从 Issue title/description/criteria 生成（WriteTaskPackage 扩展 per-repository workspace）。
- B09 messages：awaiting_input 的问答记录直接引用 logical_work_requests.id；answer 路由走 ClarificationView 校验。
- B10 execution：task_runs.attempt_id/agent_run_id 是纯引用；调度器调用 execution.Service.Reserve/StartAgentRun，不绕过账本。
- P9 配置：任务包携带 Issue 的 initial_configuration_revision；agent 环境不含凭证。

## 4. 跨仓联合验证（决赛演示主轴）

1. Leader 提交计划：任务 A（仓库甲改动）+ 任务 B（仓库乙同步改动，depends_on [A 可选]）+ 任务 V（joint_validation，depends_on [A,B]）。
2. A/B 各自完成并经 Manager 放行（succeeded）。
3. V 被调度：任务包含两仓产出物路径 + 契约描述（如字段名/类型清单）；agent 执行比对脚本，输出一致/不一致结论到 result_summary。
4. 不一致 → V failed → Leader 修改开计划 v2（新版本化计划）→ 协调修改闭环。评委看到：单仓测试各自绿 → 联合验证红 → 证据清单定位到哪个仓库哪个接口 → v2 修复 → 全绿。

## 5. 验收映射

| 编号 | 场景 | 预期 |
|---|---|---|
| DAG01 | 提交含环的计划 | 422，环检测拒绝 |
| DAG02 | A→B→V 三任务串行派发 | V 仅在 A、B 均 succeeded 后 ready |
| DAG03 | A 完成后 Manager 未放行 | B 依赖 A 时停 ready，blocked 不解锁 |
| DAG04 | Manager reject 后 retry | 新 run attempt_no+1，旧 run 留痕 |
| DAG05 | agent 需澄清退出 | awaiting_input + B09 请求可查，答复后重派 |
| DAG06 | 双 Leader 并发提交同 Issue 计划 | 唯一 active 索引拒绝第二个 |
| DAG07 | 协调器重启 | dispatched/running run 按账本核对，不重复派发 |
| DAG08 | 联合验证发现契约不一致 | V failed，交付证据清单可定位仓库+接口 |
| DAG09 | 计划取消 | 进行中 run cancelled，attempt 走停止协议 |
| DAG10 | 调度器与 host-executor 竞争 | FOR UPDATE SKIP LOCKED 双派不可能 |

## 6. 实现顺序

1. 0023 迁移（四表+触发器）
2. internal/orchestration：types/state machine/dependency evaluation
3. scheduler goroutine（coordinator）
4. web 路由（10 条）+ 任务包扩展
5. 验收文档 + DAG01-10 用例
