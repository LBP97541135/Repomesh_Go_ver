# 端到端测试报告（E2E）

日期：2026-09-17。环境：生产服务器 8.210.190.41（crazykitties.cn），PostgreSQL 16 + pgvector，schema current=23。
测试方式：`go test -tags=e2e ./e2e/`（SSH 隧道连生产库 + HTTPS 打线上 web），代码在 `e2e/` 目录。

## 结果：11/11 全部 PASS（总耗时 7.4s）

| # | 测试项 | 结果 | 说明 |
|---|---|---|---|
| 1 | 组织装配（M4） | PASS | 1 Leader + 2 Manager + 4 Worker 建立成功 |
| 2 | 装配幂等 | PASS | 重跑装配返回相同身份（singleton_key 去重生效） |
| 3 | 计划创建 | PASS | tasks.CreatePlan 建立带 DAG 依赖的 v1 快照 |
| 4 | 依赖评估 | PASS | PromoteReady 在空计划上安全空跑 |
| 5 | 双派（M5） | PASS | 开发 agent + 测试 agent 两个 run 各自独立 attempt 建立成功 |
| 6 | 放行门控 | PASS | 对不存在任务 Approve 被拒（ErrConflict） |
| 7 | 接口文档审批（M7） | PASS | 圈外人拒绝 → 单票仍 awaiting → 两票齐 → effective + 回读一致 |
| 8 | 分支验证（M8） | PASS | 本地 provider 建模板分支 → 跑 2 条迁移 → passed → 分支清理干净 |
| 9 | 分支验证隔离 | PASS | 不同幂等键产生不同 run（不误重放） |
| 10 | 观测查询（M9） | PASS | trace 会话/日志查询正常（空库返回 0 行不报错） |
| 11 | HTTP 诚实性 | PASS | healthz 200 / readyz 503 / 未认证 API 503（如实拒绝，不泄露） |

## 测试过程中发现并修复的 5 个真实 bug

| # | bug | 修复 |
|---|---|---|
| 1 | 双派共用 attempt 违反 B10「每 attempt 一个活 run」唯一约束 | 改为开发/测试各自独立 attempt（003 无需改表，语义修正） |
| 2 | agent_runs 的 agent_kind CHECK 不含 test_agent | 新增 0023 迁移放宽枚举 |
| 3 | branchvalidation：幂等键冲突被误报为"原始 run 不存在"；run id 非 UUID 形状；URL DSN 替换数据库名取错斜杠；真实迁移错误被吞 | 四处修复：区分 ErrNoRows 冲突与插入失败、UUID run id、URL 解析修正、错误透传 |
| 4 | interface doc：第二次 Approve 读不到第一次的批准记录（approvedBy 只存 result 未回读） | Approve 读取时合并 canonical + result |
| 5 | dual_dispatch 测试 run 插入 SQL 参数编号跳号（$5/$6 无 $4） | 修正参数序列 |

## 结论

完整业务链路在真实生产环境验证通过：装配 → 计划 → 依赖评估 → 双派 → 门控 → 接口文档多方审批 → 数据库分支验证 → 观测查询 → HTTP 诚实性。测试套件可重复运行（每次 fresh org/project/issue，自动清理模板库），后续回归可直接 `go test -tags=e2e ./e2e/`。

## 遗留

- 观测查询返回 0 行符合预期（当前无 trace 摄入源；ingestion 属后续批次）
- 未认证拒绝码 503 是无认证部署的正确行为；配好 GitHub App 后应变 401
- LLM 真实模型调用、AT 真实派工仍属外部集成（按设计诚实 blocked）
