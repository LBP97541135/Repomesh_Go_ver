# Review 修复报告：全流程功能可达性

日期：2026-09-17。分支 main，commit 04c50a2 → 67c6c0c → efc498e（lbp97541135）。

## 背景

全量 review 发现：底层包实现完整（27 包全编译、E2E 11/11），但 **9 处 M1-M9 服务只能被 e2e 调用，生产 web 进程无路由可达**；另有 Py 版对等缺口 6 项。本轮全部修复。

## A. P0：9 处仅 e2e 可达 → 全部 HTTP 化

新增 `internal/web/pipeline*.go`（Pipeline 聚合 + 20+ 路由）：

| 服务 | 新路由 | 状态 |
|---|---|---|
| M4 组织装配 | POST /api/organizations/{orgId}/assembly | 200 |
| M1 计划控制 | POST/GET plans、approve/reject、interrupt | 200 |
| M7 接口文档 | POST create / approve / GET | 200 |
| M8 分支验证 | POST start / GET / retry-cleanup | 200 |
| M9 观测 | traces、trace events、logs、alerts | 200（503 无库） |
| M6 交付 | POST releases（push+PR） | 200 |
| M5 双派 | 已在 dual dispatch 路径 | ✅ |

coordinator 主循环新增 DAG 调度 tick（PromoteReady→DispatchOne，经真实账本）。

**关键修复**：pipeline 服务改用独立 pgxpool——之前装配代码在 auth-config 块内，无认证部署（当前生产）下路由整族 404。现在所有部署模式下路由都存在，DB 失败时 handler 如实 503。

## B. P1：Py 对等缺口补齐

| 缺口 | 实现 | 路由 |
|---|---|---|
| joint_validation 任务类型 | internal/jointvalidation：契约逐条比对（clause 级 verdict，agent 不可翻转数据矛盾） | POST /api/projects/{pid}/joint-validation |
| 项目 release gate（终审） | internal/gates：全任务 done 才开闸，Leader 记录 release/reject | GET/POST /api/projects/{pid}/release-gate |
| Manager spec 生命周期 | internal/spec：draft→approve→published，按仓库存当前版 | POST /specs、/specs/{id}/approve、GET /specs/current |
| SCM webhook + change-sets | internal/scm：HMAC 签名验证、幂等 ingest、冻结、生命周期事件、merge-gate fail-closed | POST /api/delivery/github-webhook（403 拒绝无签名）、change-sets 3 路由 |
| 交接文档 | internal/handoff：plan+repo 幂等键 | POST/GET plans/{planId}/handoffs |
| llm_usage 成本查询 | UsageSummary（prompt/completion tokens 汇总） | GET /api/observability/usage |
| 拓扑 API | assembly.ListTopology | POST /topologies、GET /topology |

## C. 部署与回归

- 三二进制 + 前端已重新部署服务器（crazykitties.cn）
- E2E 回归 **11/11 PASS**（6.6s）
- 新路由生产验证：traces 503（无库时诚实）、webhook 403（拒绝无签名请求）、healthz 200
- schema current=23 pending=0

## D. 与 Py 版最终对比

**已对等或超越**：认证/扫描/项目/模型/Issue/消息/澄清/DAG 调度/装配/spec/gate/接口文档/分支验证/观测查询/交付 PR/技能治理/决策链 + Go 独有增量（预算账本、文档解析、AB arms、unknown CLI、settings 开关、E2E 套件）。

**明确未做（设计性排除，非遗漏）**：context 模块 Run Bundle（Py 侧也是候选态）、Matrix 房间传输（等 AT 部署）、真实模型调用（B09 传输适配器）、真实 AT 派工。全部在文档中如实标注 blocked/未接入。
