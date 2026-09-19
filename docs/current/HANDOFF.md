# RepoMesh 当前交接

## 2026-09-19 与最新 main 集成

本地观测及前端用途入口已与 `c0cd44a` 主线整合，保留 GitHub 重连、智能体／技能设置、仓库团队和健康监控更新。主线已使用 0043／0044，观测迁移调整为 **0045_observation_facts.sql**；此前报告中的 0043 指首轮隔离试验版本，不改写其证据或已应用历史。合并树的 Go 构建／vet、589 个测试（另 2 条件跳过）、相关包 race、两套前端及浏览器导航验证通过。整合范围与限制见[合并检查](../development/2026-09-19-local-observation-02/MAIN-INTEGRATION.md)。

## 2026-09-19 控制台入口与模型用途同步

RepoMesh 前端的「观测」现在直接连接本地工作台；旧双入口与云地址偏好不再参与跳转。设置新增「模型与 API」，区分仓库分析、AgentTeams、观测评估，并在供应商表单、平台向导及工作台设置中标注用途。配置来源保持原有归属，不新增模型用途绑定或自动切换团队模型。HTTPS 控制台可导航到本地 HTTP 工作台，跨站 API 访问仍拒绝。前端构建／lint、相关 Go 检查及浏览器跳转验证通过；浏览器中的产品身份和读取接口使用只读夹具，目标工作台为真实进程，未借此宣称 OAuth 或模型保存验收。见[使用说明](../../frontend/README.md#本地观测入口与模型用途)。

## 2026-09-19 全本地观测工作台

用户进一步明确“观测和评测本地，模型可以配 DeepSeek”，采用 [ADR 0024](../adr/0024-local-observation-workbench.md)。默认入口已由云实验 Launcher 改为 `repomesh-observe serve`：本地页面、不可变证据查询、OTLP HTTP 接收、固定用例验收、CSV 和可选逐条 DeepSeek 评审。旧 Launcher 已停止；同一 18090 端口运行新的工作台，既有证据目录按只读来源挂载。

DeepSeek 密钥已保存在本机私有配置，连接测试及一次真实 `deepseek-flash` 契约评审通过，业务验收结论保持独立。当前 Go 数据库全量测试 587 通过、2 按条件跳过、0 失败；构建／vet、相关包 race、真实浏览器操作通过。具体证据与后续限制见[实施记录](../development/2026-09-19-local-observation-02/README.md)，启动和接口见[本地工作台说明](local-observation-workbench.md)。完整 DSH 运行链路仍未接入，不因本地工作台交付而标为通过。

## 2026-09-19 AgentLoop 本地观测与评估首轮实施

按[开发计划](../plan/agentloop-observability-implementation.md)完成本地切片：迁移 0043 与发现链事务历史、同步选仓输入快照、独立 `repomesh-observe` 采集／OTLP 导出／折扣验收／CSV／评分回读。官方 Collector 在 14318、官方 AgentLoop 本地 Launcher 在 18090 运行，使用真实 `agentloop` 模式；目标云空间未配置，未运行 DSH 或云端评估。

三个固定折扣产物分别得到 fail／pass／unknown，27 条 Trace 与真实 Collector 落盘身份逐一核对；独立 PostgreSQL 中实际发现用例保存、采集及导出通过。多 agent 开发后完成独立复核，修复并发锁、错误 OTLP 确认和证据／评分身份关联问题。完整结果及限制见[实施记录](../development/2026-09-19-agentloop-observability-01/README.md)，使用命令见[开发说明](agentloop-observation-development.md)。

最终 Go 全量构建／vet 通过，设置隔离数据库连接的测试为 582 通过、0 失败、2 按条件跳过；观测工具两包 race 检查和本地平台保护检查通过。逐字段验收保留已知失败、空引用不计证据等评估问题也已独立复验。

新增迁移只在独立测试库应用，共享业务服务和数据库未升级。现有 `Maintenance.Purge` 的主会话外键问题被独立确认为既有缺陷，本轮未改动；完整 DSH Trace、实际 Skill／模型计量、生产开销及泛化评测仍待后续验收。

## 2026-09-19 AgentLoop 观测与评估设计

新增 [AgentLoop 观测与评估 Spec](agentloop-observability-evaluation-spec.md)，以评委提出的漏仓、联调失败、返工、耗时和人工审查问题为主线，按用户明确的后续取消 CLI coding agent、转向 AgentTeams 原生 DeepSeekHarness 的方向设计。包含 Trace 身份与字段、版本清单、业务／运行事件、AgentLoop 接入与评估映射、折扣实验及分阶段验收。

Spec v0.2 根据后续维护要求补充：先外部读取，再补必要业务事实；本体集中保留事实、身份绑定和证据登记，运行时及 AgentLoop 格式转换放入适配层。增加契约版本、旧记录重放、故障隔离和升级验收，完整清单为 AC01—AC33；最小改动范围仍需实施时按具体缺口核对。

本次只交付设计和文档检查；未修改产品代码、创建云资源、接通 DSH 或运行评测。该 Spec 为 proposed，不将示例、平台文档能力或历史上游测试记作本项目已验收。其他实现和环境状态仍按下方相应检查点理解。

## 2026-09-19 项目、仓库与 Issue 范围修复

已按[修复计划](../plan/project-repository-scope-repair.md)完成本地代码改造：控制台统一显式项目上下文，先保存项目、再接入仓库、最后勾选 Issue 工作范围；发现到任务下发均检查项目及 Issue 仓库边界。迁移 0042 为计划关联 Issue、为任务建立规范仓库绑定，已有越界记录保留并隔离，带计划／任务的 Issue 只允许归档。

本轮检查及限制见[范围修复记录](../development/2026-09-19-project-scope/README.md)。迁移仅在隔离测试库执行，既有运行实例仍沿用此前二进制及迁移 39；尚未重启或应用到共享业务数据库。下方环境快照按记录时点理解。

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
