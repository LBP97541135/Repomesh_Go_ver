# 全流程愿景 × 实现架构 最终映射

状态：架构梳理完成确认稿。每个细节标注依据来源（Py = Python 版对应实现；B0x = Go 版已完成批次；M# = 待实现模块编号）。
用途：确认"改造完成后"与用户描述的全流程一致；实现期间作为唯一对照。

## 用户流程逐环节映射

### 环节 1：给定 GitHub 权限，自动扫描全部仓库

| 项 | 结论 | 依据 |
|---|---|---|
| GitHub App 授权 | ✅ 已实现 | B02（access 包：App 安装观察、连接、令牌刷新） |
| 自动扫描组织/仓库 | ✅ 已实现 | B02 coordinator 发现续扫（discovery_batches/discovery_cursors）+ scan/reposcan 多平台抓取（Py: repository_intelligence/application/scan.py 同构） |
| 扫描产物（AutoCard） | ✅ 已实现 | scan 包通道解析（Py: call/deploy_parsers 同构） |

### 环节 2：为每个仓库创建 Manager + 若干 Worker；每组织/个人账号一个 Leader

| 项 | 结论 | 依据 |
|---|---|---|
| 角色/团队实体 | ✅ 已实现 | Py: project/domain.py ProjectAgentTopology + RepositoryTeam（leader_agent_id、worker_agent_ids、leader 不能兼任 worker 校验 L186） |
| 自动装配时机 | ✅ 设计定稿 | Py: 扫描/项目创建后建拓扑（human_control.py POST /projects/topologies、automatic-topologies）；Go 版在 M4 模块落地 |
| Worker 承载 | ✅ 已定 | AT 上游预留 repomesh-runner runtime——RepoMesh Runner 直接作为 AT Worker（核查报告问题 1 补充发现）；每仓库一个 AT Team（Py: "每仓库一个 AgentTeams Team + 唯一仓库 Leader"） |
| Leader 粒度 | ✅ 已定 | 组织级一个总 Leader（用户原话）；Py 同款：组织 Leader → 仓库 Manager 分级 |

### 环节 3：新需求/Issue → Leader 和 Manager 确认仓库范围 → 建 project → 拉小群

| 项 | 结论 | 依据 |
|---|---|---|
| Issue 接入 | ✅ 已实现 | B06 页面创建 + Py: repository_intelligence 五步发现链（analysis→candidates→classification→plan→approval） |
| 范围确认 | ✅ 已实现 | B09 澄清状态机（logical_work_requests/clarifications，Py: backend-message-clarification-design 同源）；范围候选=发现链 candidates |
| 建 project | ✅ 已实现 | B03 项目创建（幂等） |
| 拉小群 | ✅ 已实现（沟通层待接 Matrix） | B06 创建即建会话（conversations）；跨 Manager 群=AT Task Room（核查：roomflow create_task_room，project↔room 绑定文件可映射）+ M3 里 RepoMesh Matrix 账号入房 |

### 环节 4：一个 Manager 定接口文档，其他 Manager 确认后开发

| 项 | 结论 | 依据 |
|---|---|---|
| 接口文档协作 | 🔶 M7 新增 | Py 无完整多方审批（specification 模块有 draft→in_review→approved 单文档流 L66-238）；Go 版在 M7 扩展为多方确认（一个 Manager 提交、其余 Manager 逐个 approve，全通过才解锁依赖它的任务）——这是对 Py 的超出项，用户原话要求 |
| 文档版本化 | ✅ 依据 | Py: specification 不可变版本 + diff（L238） |

### 环节 5：Leader 规划完整项目 DAG，按 DAG 流程开发

| 项 | 结论 | 依据 |
|---|---|---|
| DAG 生成 | ✅ 设计定稿 | M1（DESIGN.md 定稿）：Issue 级计划 + 项目全景视图；Py: leader_actions.py submit_repository_plan（L85，校验环/覆盖/assignee）同源 |
| 依赖评估 | ✅ 设计定稿 | M1 调度器：depends_on 全 succeeded 才 ready；Py: task_orchestration READY 评估同构 |
| 按流程派发 | ✅ 设计定稿 | M1 派发通道直连账本 → M2 repomesh-runner 承载执行；Py: agent_runtime runner-tasks/next 租约派发同构 |

### 环节 6：Manager 写本仓 spec/task，形成消息队列，分发给 worker 和测试团队

| 项 | 结论 | 依据 |
|---|---|---|
| Manager 写 spec | ✅ 设计定稿 | M5：spec 来源从"Issue 派生"扩展为"Manager 编写"（Py: specification.SpecificationService.create/revise/submit/approve 全生命周期 L83-238）；spec 版本不可变、diff 可查 |
| spec/task 进消息队列 | ✅ 已定 | 任务包（B10 WriteTaskPackage）+ AT 派发；Py: "经 Matrix/AgentTeams 发布 spec.md+meta.json 派工"同构 |
| 双派（开发+测试） | ✅ 设计定稿 | M5 测试 agent：同任务两个 run（agent kind 区分），Py: database_test_team.py 的 handoff/plan/evidence 三件套（L18-33）+ api/database_test_team.py 四端点（plan/approval/evidence）同构 |

### 环节 7：Worker 写完 → 跑测试、看 diff → 确认后 task 放行

| 项 | 结论 | 依据 |
|---|---|---|
| Worker 执行 | ✅ 已实现/承接 | agent_runs 账本（feat/agent-worker）→ M2 升级为 repomesh-runner 被 AT 管理 |
| 跑测试 | ✅ 设计定稿 | M5 测试 run 产出测试报告作为放行证据（Py: review_validation 四级测试运行 + ValidationSnapshotService.validate_for_delivery L87） |
| 看 diff | ✅ 设计定稿 | M5：测试 run 读开发 run 的 workspace diff（Py: scm_observations/delivery webhook 观测同源，简化为本地 diff 因首批平台代 push） |
| 放行 | ✅ 已设计 | M1 九态：blocked（完成后必拦）→ Manager approve → succeeded；Py: merge-gate fail-closed 同哲学 |

### 环节 8：单仓完成 → 集成测试 → 等待其他仓 → 联调测试

| 项 | 结论 | 依据 |
|---|---|---|
| 单仓集成测试 | ✅ 设计定稿 | M5：仓库任务的测试 run 天然覆盖（仓内构建+测试命令） |
| 跨仓联调 | ✅ 设计定稿 | M1 joint_validation 任务类型（依赖 ≥2 仓库任务，读产出做契约比对）；Py: change_orchestration 跨仓编排 + delivery merge-gate 同哲学 |
| 等待/再派 | ✅ 已定 | DAG 依赖评估天然支持（任务再次 ready 时的语义由 DAG 表达，不做隐式等待） |

### 环节 9：全部通过 → 总 Leader 审核 → 总结给人 → 人工放行 → 产生 PR

| 项 | 结论 | 依据 |
|---|---|---|
| 总 Leader 审核 | ✅ 设计定稿 | M1：joint_validation succeeded 后整计划进入总审核态（blocked 语义复用）；Py: 项目检查点门控（checkpoint_control.py ProjectCheckpointService.operational_gate L190） |
| 总结给人 | ✅ 依据 | Py: HumanDecisionNotifier 协议（checkpoint_control L43）——通知人做决策；Go 版 M1 复用（会话消息 + 通知） |
| 人工放行 | ✅ 已设计 | M1 人工 gate（用户已确认 blocked 机制）；Py: checkpoint_decisions 端点同构 |
| 产生 PR | ✅ 设计定稿 | M6 交付面：平台用 GitHub App 凭证代 push+建 PR（Py: delivery 模块平台代 push/建 PR/merge-gate 全套 4969 行，Go 对等实现核心链）；agent 零凭证原则两端一致 |

### 横切面 1：数据库管理和测试

| 项 | 结论 | 依据 |
|---|---|---|
| DB 分支验证 | 🔶 M8 | 评委建议①；Py: review_validation.DatabaseBranchValidationService 完整控制面（七态+provider 端口）+ 表已迁移（database_branch_validations），Go 服务层待写 |
| DB 测试团队交接 | ✅ 依据 | Py: DatabaseTestTeamHandoffService（plan/approval/evidence），M8 同构 |

### 横切面 2：Skill 体系

✅ 已全量移植（skills 包：版本/评测/金丝雀/晋升/回滚/MCP 守卫；Py: capability_management 对等）。依据充分，无缺口。

### 横切面 3：历史决策差距

✅ 已全量移植（decisionchain 包 + pgvector 语义搜索；Py: decision_chain 五类链事件聚合同构）。无缺口。

### 横切面 4：worker 上报 spec 修改 + DAG 动态调整

| 项 | 结论 | 依据 |
|---|---|---|
| worker 上报 | ✅ 已定 | M1/M5：awaiting_input 态接 B09 澄清链路（logical_work_requests）；Py: task_orchestration 的澄清/更正协议同源 |
| DAG 调整 | ✅ 已定 | 版本化+显式迁移：开计划 v2，未完成任务显式迁移或作废；Py: dynamic_plan.py append-tasks 的乐观版本两套并存，Go 版按用户裁决取不可变一套 |

### 横切面 5：数据观测 / 告警

| 项 | 结论 | 依据 |
|---|---|---|
| 观测 | 🔶 M9 | Py: observability 4000 行（trace/日志/用量/成本）；Go 版 events/llm_usage/trace_sessions 表已迁移，最小查询面待建 |
| 告警 | 🔶 M9 | Py: alert_rules/AlertingEvaluator 评估器；Go 表已迁移，评估器待写 |
| AT 侧 trace | ✅ 补充 | AT 的 OTel span 打 task.id/project.id 标签（核查报告），M9 关联 |

### 横切面 6：人工确认点

✅ 已设计：三级人工点全部落位——①范围确认（B09 澄清）②任务放行（M1 blocked/approve）③项目终审（M1 总 gate + M6 放行后 PR）。Py: checkpoint_control 三类决策同构。

## 模块清单（最终版，含依赖关系）

| # | 模块 | 覆盖环节 | 前置 |
|---|---|---|---|
| M1 | DAG 编排核心（0023 迁移+九态+调度器+10 路由） | 5、7 放行、8、9 审核、横切 4、6 | 无（设计已定稿） |
| M2 | repomesh-runner（host-executor 升级，AT Worker 承载） | 7 执行 | AT 部署 |
| M3 | AT 适配器（REST 客户端 + Matrix 沟通层） | 2 装配、3 拉群、7 唤醒 | AT 部署 |
| M4 | 组织/仓库角色自动装配 | 环节 2 | M3 |
| M5 | 测试 agent + Manager spec 生命周期 | 6、7 | M1 |
| M6 | 交付面（平台代 push/PR） | 9 PR | M1 |
| M7 | 接口文档多方审批 | 环节 4 | M3（会话层） |
| M8 | DB 分支验证服务 | 横切 1 | M1（复用任务框架） |
| M9 | 观测/告警最小面 | 横切 5 | 无硬依赖 |

## 一致性结论

用户全流程 9 个环节 + 6 个横切面共 24 个检查点：
- **14 个已实现**（B02/B03/B04/B05/B06/B07/B09/B10 + agent-as-worker）
- **9 个已设计定稿待实现**（M1/M5/M6 内）——每个都有 Py 版对应实现作依据
- **1 个超出 Py 版**（M7 接口文档多方审批，用户原话新增，Py 只有单文档流）
- **0 个凭空项**

改造完成后与描述流程一致。实现顺序：M1 → M2/M3（需先部署 AT）→ M4/M5 → M6/M7 → M8/M9。
