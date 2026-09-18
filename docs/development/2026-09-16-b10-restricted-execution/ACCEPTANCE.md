# B10 验收文档：受限执行 G3/G4（任务 #17）

对应 ASTRA 文档 B10。分支 feat/astra-b09。
规格来源：docs/adr/0017-atomic-attempt-resource-reservation.md（accepted）、docs/adr/0013-web-coordinator-host-executor-processes.md（accepted）、docs/current/execution-integration-gates.md（G3/G4 行）、docs/current/team-execution-policy.md。

## 需求是什么

1. G3 正式状态写入责任：列清执行状态的唯一写方、凭据与校验点；受限路径必须在数据层拒绝，不能只靠页面隐藏。
2. G4 Attempt 生命周期：Attempt/Worker/容量在**一个短事务**内原子预留（ADR-0017）；启动前再次核验；实际停止需核验写能力撤销与资源回收；未知状态不释放资源。

## 做了什么

### 1. internal/database/migrations/0020_execution.sql（新建）

repomesh_execution schema 四张表 + 两个约束触发器：
- workers（concurrency_limit=1 单活跃任务、active_attempts CHECK ≥0、心跳、retired）
- attempts（八态状态机 reserved→preparing→launch_verified→running→stop_requested→stopped/failed/unknown；(project,issue,reservation_generation) 唯一；issue 复合 FK 延迟引用）
- attempt_events（append-only 证据流，source 区分 coordinator/host_executor——**执行端只写观察，不写正式状态**）
- host_resources（container/directory/volume/network 四类；lifecycle_owner 固定 host_executor——同一资源唯一管理者；write_enabled 一等列）
- 触发器 attempt_configuration_matches：进入 launch_verified/running/stopped 前，attempt 的 configuration_revision 必须等于 Issue 冻结的 initial_configuration_revision（P9 闭包延伸到执行层）
- 触发器 attempt_stop_requires_cleanup：**SQL 级拒绝**在仍有 provisioned 或 write_enabled 资源时把 attempt 置为 stopped——停止必须核验，G3 的"受限路径必须拒绝"落在数据层

### 2. internal/execution 包（新建）

- `Reserve`（ADR-0017 短事务）：`UPDATE workers SET active_attempts+1 WHERE active_attempts < concurrency_limit AND retired_at IS NULL`（条件更新即原子占用，竞争失败者 409 WORKER_UNAVAILABLE 而非等待假队列）→ INSERT attempt → reserved 事件 → COMMIT。延迟触发器在 COMMIT 时校验配置匹配。
- `VerifyLaunch`：启动前行锁复查状态与 worker 存活，然后才置 launch_verified（准备期撤权/退休的守卫）。
- `RequestStop` / `ConfirmStopped`：停止单向转移；确认时应用层再查一遍资源（CLEANUP_INCOMPLETE 拒绝），SQL 触发器兜底，两层校验。
- `RevokeWrite` / `ReleaseResource`（orphan_suspected 保留）/ `ProvisionResource` / `RecordObservation`（只允许三类观察事件）。

### 3. cmd/repomesh-host-executor（重写，替换骨架）

- 不再是无条件退出 1 的骨架：需要 --database + --worker 注册后才运行（**未注册主机不能执行任何动作**）。
- 循环：心跳 worker 行 → 轮询自己名下 stop_requested 的 attempt → 对每个 provisioned 资源先 revoke write → 主机侧停止 → 标记 released。容器/卷/网络的主机拆除在本构建中显式拒绝（"not granted in this build"），不做 best-effort 假清理。
- executor 绝不推进 attempts 正式状态——ConfirmStopped 由 coordinator 调用；G3 写入责任矩阵落地。

## 影响范围

- 新增：0020 迁移、internal/execution/ 两文件、cmd/repomesh-host-executor/executor.go。
- 修改：cmd/repomesh-host-executor/main.go（骨架替换）。
- 不触及 web 层签名；执行面当前没有 HTTP 入口（web 不持有 Docker socket，ADR-0013），coordinator/executor 通过库内表协作。

## 修复逻辑（关键设计决策）

1. **条件 UPDATE 即原子预留**：不做"先查空闲再写占用"，active_attempts < concurrency_limit 的条件更新天然串行化竞争（ADR-0017 场景的核心要求）。
2. **停止核验双层**：应用层 CLEANUP_INCOMPLETE 检查 + attempt_stop_requires_cleanup deferred 触发器——即使未来有第二个写方绕过服务层，SQL 依然拒绝。
3. **写入责任矩阵**（G3 交付核心）：attempts/事件里的正式状态只归 coordinator；host_executor 只写 source='host_executor' 的观察事件和资源行；触发器+CHECK 是第三道闸。

## 偏差清单

| # | 规格声明 | 实际实现 | 处理 |
|---|---------|---------|------|
| 1 | G4 完整资源生命周期（clone、网络、卷的真实创建/停止） | directory 资源主机拆除可用；container/volume/network 拆除显式 not granted | 需要容器运行时授权与真实宿主验证；显式拒绝优于伪清理（ADR-0013"不开放任意宿主命令"） |
| 2 | Controller↔executor 进程间协议（领取、租约、心跳超时参数冻结） | 心跳 + 轮询数据库表实现，无独立 RPC | ADR-0017 明文"具体锁、表、领取和恢复算法待细化"；表协作是首个可验证实现，租约参数留待运行验证后冻结 |
| 3 | 启动失败/unknown 分支的替补决策 | failed/unknown 状态列就绪，自动替补未实现 | "结果未知时先核查，不能仅凭超时重分"——替补策略需要真实运行证据，不预先实现 |

## 对应 commit

- feat(b10): execution reservation ledger, verified stop, restricted host executor

## 验证结果

- `go build ./...` EXIT=0；`go vet ./...` 无输出；`go test ./... -count=1` 全部 ok。
- 0020 迁移重放 + G3/G4 验收矩阵（旁路写入拒绝、租约过期、取消后继续写）待 Docker 环境恢复执行（见可用性测试任务）。

## 遗留（不阻塞验收）

- 0020 实际重放验证；host-executor 心跳超时与 worker 退休的参数冻结；container 拆除授权；与 B09 消息投递的消费联动（B11 范围）。
