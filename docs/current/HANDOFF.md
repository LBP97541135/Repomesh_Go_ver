# RepoMesh 当前交接

更新：2026-09-14。本次B05/B06五项设计收口基于`43d8c2a`。产品运行结果沿用各报告锁定的源码版本，本次未重跑验收。

开发从[全局阅读指南](AGENT-READING-GUIDE.md)、[计划导航](../plan/README.md)和[现行专题索引](README.md)进入。批次依赖及验收入口统一维护在[施工计划](../plan/IMPLEMENTATION-PLAN.md)。

## 当前完成度

| 范围 | 状态与已交付内容 | 依据与限制 |
| --- | --- | --- |
| B00、B01 | VERIFIED；工程基线、PostgreSQL 连接与显式迁移 | [数据库基础记录](../development/2026-09-12-batch-01/README.md)。 |
| B02 | IN_PROGRESS；本地认证、会话、授权恢复和仓库发现已实现；外部验收 PAUSED_BY_USER | [采用范围](b02-authentication-adoption.md)、[本地记录](../development/2026-09-12-batch-02/README.md)。完整外部验收未通过，历史失败和恢复责任见下节。 |
| B03 | INTEGRATED_LOCAL_VERIFIED；项目创建、列表、资料编辑、明确增仓、固定配置及原操作恢复 | [主目录集成](../development/2026-09-12-b03-integration-01/README.md)、[最终独立复核](../development/2026-09-12-b03-integration-01/FINAL-INDEPENDENT-REVIEW.md)。非整批业务 VERIFIED。 |
| B04 | INTEGRATED_LOCAL_VERIFIED；D01—D04、U04.1—U04.4 授权范围已结束 | [采用记录](b04-model-sources-adoption.md)、[验收报告](../development/2026-09-13-b04-acceptance-01/README.md)。模型供应商保存、安全终结、不可变版本、六个 HTTP 端点及部署来源导入已实现。非整批 VERIFIED。 |
| B05、B06 | 五项定点设计已收口；产品实施 TODO | [本次收口](../development/2026-09-14-b05-b06-design-closeout-01/README.md)、[原设计交付](../development/2026-09-13-b04-b06-design-01/README.md)。测试预览/handler、unknown关闭、逐路径锁序与共同owner account边界、schema2 execution形状和窗口scope约束已固定为后续实现基线；D05—D08的其余候选、数值、C05、C06、P9及产品功能仍未采用或实施。 |
| B07—B11 | 后续实施 TODO | Issue 查询、管理闭环及真实运行接入未完成。B09 的四项数据兼容问题仅有静态部分覆盖，完整 G1、G2 未完成。 |

B04 验收报告的运行基线为 `621592d`，包含收口后保存校验留页与 vault 不变量修复。报告记录 Go 测试 260 通过、0 失败、2 跳过，前端测试 32 通过；具体跳过原因、S01—S12、浏览器夹具与历史失败以报告为准。这些结果不证明真实供应商 Key、真实模型请求或计费可用。

`businessReady=false`，`/healthz` 只表示 Web 存活，`/readyz` 仍为 503。host-executor 尚未实现。Issue、真实消息、AgentTeams 运行、Docker 执行和受控 Python 仓库分析尚未接入。

## B02 外部暂停与恢复责任

用户已解除 B03 等待 B02 整批 VERIFIED 的旧顺序条件，并决定优先开发主要功能。今后真实账号验证只使用主账号 A。跨账号 LIVE-05 和 LIVE-08-USER-READ 为 DEFERRED_BY_USER，不计为 PASS，不自动建立下一轮第二账号实验。

LIVE-09、LIVE-10 的既有通过结果保留。second-account-02 的 LIVE-05 为历史 FAIL、LIVE-08 为 NOT_RUN，外部恢复记录为 RESTORE_IN_PROGRESS，见[暂停交接](../development/2026-09-12-b026-second-account-02/PAUSE.md)。原记录中的 B installation、协作者、OAuth grant 和 App 可见性仍需在获授权的恢复任务中逐项核查；没有新的证据证明已清理。

历史 PID、浏览器 target、账号状态和端口只代表记录时刻。本次没有核查这些服务是否仍在运行，不据旧 PID 操作环境，也不恢复旧观察器或账号流程。

## 后续实施入口

1. 先核对[施工计划](../plan/IMPLEMENTATION-PLAN.md)中的批次和依赖，再按本次用户授权确定范围。
2. 进入 B05 前，明确采用 D05、D06、C05、C06，并对齐已实现的 B04 类型与版本语义。来源见[设计决定表](../development/2026-09-13-b04-b06-design-01/DECISIONS.md)和[B05 设计](../development/2026-09-13-b04-b06-design-01/B05.md)。B04 的完成不自动授权 B05 或真实付费请求。
3. 进入 B06 前，按[B06 设计](../development/2026-09-13-b04-b06-design-01/B06.md)与[配置绑定专题](issue-configuration-binding-design.md)收口 P9。接收真实 Issue 与待办前落实配置关联。
4. 运行接入按[执行门槛](execution-integration-gates.md)分阶段推进。Graph 保持后台协调进程内模块，仓内 DAG 复用上游；Skill 工程仍暂缓。

工程命令与配置见[根 README](../../README.md)。当前主开发副本为 `/home/xubohan/projects/Repomesh_Go_ver`；历史 B03 worktree、旧发布包、秘密配置和实验记录保留，不自动重新合并、覆盖或清理。

## 历史与文档整理

2026-09-13，施工计划、设计分工、开发行动指南和 AgentTeams 验证清单迁至 `docs/plan/`。B03、B04 旧任务提示已归档。整理前的累计交接和原始字节见[本次归档](../archive/2026-09-13-plan-organization/README.md)，此前文档演进见[历史总导航](../archive/README.md)。

页面 F01—F15 入口见[页面交接](HANDOFF-PAGE-API-DESIGN.md)，旧后端 B01—B08 专题入口见[后端交接](HANDOFF-BACKEND-DESIGN.md)。这些专题编号与施工计划编号分别解释。历史任务指令、协作名单和运行记录不自动成为新的授权。

## 数据库方案与 API 设计入口

2026-09-15 起，正式数据库方案是 [Go 版数据库重构方案](../RepoMesh_Go版数据库重构方案.html)：7 个功能方向 44 张目标表，含 Skill 体系 7 张新表与运行账本 13 张。对应接口见 [API 设计](api-design.md)，按同样 7 个方向组织，并给出每张表到资源的映射、状态机、幂等与错误约定，以及与现有已实现端点的对应关系。两者都是设计：目标表尚无迁移文件，现有 `0001` 至 `0008` 迁移的 40 张表到目标表的迁移映射未定义；新 API 中除“已实现”标记的端点外都未实现。

原 `docs/api-database/` 目录（B00-B11 批次视角、63→34 合表）已删除，B05-B11 物理合表专题一并归档，说明见 [归档记录](../archive/2026-09-15-api-database-catalog/README.md)。本轮改动、校验脚本与限制见 [制作记录](../development/2026-09-15-api-redesign-01/README.md)。
