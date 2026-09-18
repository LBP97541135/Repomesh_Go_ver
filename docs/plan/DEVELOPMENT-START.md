# 开发前阅读与行动指南

当前批次状态以[施工计划](IMPLEMENTATION-PLAN.md)和[当前交接](../current/HANDOFF.md)为准。B04 现为 `INTEGRATED_LOCAL_VERIFIED`，见[收口 01](../development/2026-09-13-b04-closeout-01/README.md)。B05／B06 仍 TODO。

2026-09-13 B04—B06 前置设计已形成[本轮交付](../development/2026-09-13-b04-b06-design-01/README.md)，含架构比较、事务/核心声明、C05/C06、P9及DB/CB验收映射；[独立复核](../development/2026-09-13-b04-b06-design-01/REVIEW.md)已通过，无开放P0/P1/P2。该段保留设计当时的「新推荐仍待采用」记录。B09仅本轮四项数据兼容问题的静态部分覆盖，完整G1/G2未完成。

本页提供开发阅读清单与依赖顺序，不新增产品决定。用户已解除 B03 等待 B02 VERIFIED 的旧门槛，项目管理代码已合入主目录，B03 为 `INTEGRATED_LOCAL_VERIFIED`，[最终独立复核](../development/2026-09-12-b03-integration-01/FINAL-INDEPENDENT-REVIEW.md)无开放 P0/P1/P2；businessReady=false，不是外部、部署或整批业务 VERIFIED。B04—B06 设计见顶部所链交付。旧[交接](../development/2026-09-12-b04-handoff-01/HANDOFF.md)和[Prompt](../archive/2026-09-13-plan-organization/NEXT-TASK-B04-PROMPT.md)保留历史，不自动恢复未授权批次。B02 外部验收按用户决定暂停，未来真实账号验证只使用主账号 A；历史跨账号失败和未完成恢复仍保留。B03 的具体实现与验证边界见[项目管理开发说明](../current/project-development.md)。后续按明确采用的基线完成管理闭环，不要求先实现全部 F01—F15。

首次接手或需要重新了解全局，先按 [Agent 全局阅读指南](../current/AGENT-READING-GUIDE.md) 建立产品、架构与实现的联系，再按本页选择实施材料和验证步骤。

## 所有人先读的入口

1. [AGENTS.md](../../AGENTS.md)：工程目录、三进程权限边界、修改与验证要求。
2. [根 README](../../README.md)：工具版本、构建／启动命令和当前骨架行为。
3. [总交接 HANDOFF](../current/HANDOFF.md)：当前完成度、未实现能力及下一阶段边界。
4. [现行文档索引](../current/README.md)：每个主题的唯一规则来源及采用／候选／历史区分。
5. [CONTEXT.md](../../CONTEXT.md)：Project、Conversation、Issue、ChangeSet、实际 room 与运行 session 的区别。
6. [Astra 前置设计分工](ASTRA-DESIGN-PREPARATION.md)：B04—B11 的设计责任、可提前程度、接口声明与实现交接要求；派工必须遵守其中的单项范围、排除项、文件／操作边界和交付终点。
7. [施工计划](IMPLEMENTATION-PLAN.md)：区分前置设计待办、产品实施依赖及实际验收状态。

`accepted` 只覆盖明确采用的章节，`proposed` 中引用的旧规则不因此失效；后续修订也不会自动继承整篇旧文档的采用状态。按具体替代说明判断，不按文件日期或原型能否点击判断。

## 按角色继续阅读

| 角色 | 文件 | 要解决的问题与采用范围 |
| --- | --- | --- |
| 前端 | [页面／接口交接](../current/HANDOFF-PAGE-API-DESIGN.md)、[原型导航](../prototypes/README.md)、[会话与独立 Issue](../current/conversation-issue-separation-design.md) | 找到当前页面基线。会话／Issue 分离、创建弹窗／关联控件／提交反馈及列明的 F01—F04 UI 已采用；原型模拟不是接口或运行能力。 |
| 前端 | [项目配置](../current/project-configuration-design.md)、[模型设置](../current/model-connection-settings-design.md)、[模型应用](../current/model-project-apply-design.md) | 区分资料修复、明确增仓、完整配置 PATCH 和只换模型。已采用三段表单、显式配置开关、模型分栏及指定应用呈现；秘密保存按 B04 采用范围已实现，后续应用协议仍待采用。 |
| 前端 | [最小详情](../current/issue-overview-minimal-design.md)、[页面恢复](../current/first-batch-recovery-design.md)、[登录恢复](../current/login-recovery-page-design.md) | 实现加载、受限、未知和原操作返回。首批范围已收拢；详情新 UI 和其余恢复矩阵保持原采用边界；B02 认证机器规则及页面已按[采用记录](../current/b02-authentication-adoption.md)实施。 |
| API | [首批浏览器契约](../current/first-batch-browser-api-contract.md)、[Issue 创建契约](../current/issue-page-create-api-contract.md) | 已采用的项目／列表、创建／更新原操作、详情／rooms／Issue SSE 字段；与页面、数据库共享同一语义，不从 HTML 样例另造字段。 |
| API | [认证浏览器候选](../current/authentication-browser-api-draft.md)、[模型浏览器候选](../current/model-settings-browser-api-draft.md) | B02 登录／重连及固定 Destination 已采用；供应商保存与安全终结已按[B04 采用记录](../current/b04-model-sources-adoption.md)实现，单模型测试和专用应用仍待 B05 采用。不得私自用任意 returnUrl 或现行完整 PATCH 替代新协议。 |
| 后端 | [后端交接](../current/HANDOFF-BACKEND-DESIGN.md)、[首批持久化](../current/backend-first-batch-persistence.md) | 项目／Issue 关系、唯一约束、事务、幂等、持久待办和事件的已采用范围；新增 §2.5 Issue 配置绑定仍是候选，不是数据库已实现。 |
| 后端 | [配置来源候选](../current/backend-first-batch-sources-draft.md)、[模型内部操作候选](../current/backend-model-operations-draft.md)、[Issue 配置绑定候选](../current/issue-configuration-binding-design.md) | 明确 owner、不可变版本、秘密存储、有限测试次数、出站限制、保存与终结的互斥，以及 Issue 创建时绑定原配置。其中来源与秘密保存按 B04 已采用范围实现；模型测试、应用和 Issue 配置绑定仍待后续采用。 |
| 后端／运行适配 | [架构](../current/architecture-design-v1.md)、[ADR 索引](../adr/README.md)、[Graph／Loop](../current/graph-loop-design.md)、[执行接入门槛](../current/execution-integration-gates.md) | 三进程和受控宿主边界、复用原生仓内 DAG 的方向已采用；G1—G5 的具体调用、写入、生命周期与换图协议未全部收口。 |
| 后续会话批次 | [消息／澄清五端点](../current/conversation-message-clarification-api-contract.md)、[消息内部设计](../current/backend-message-clarification-design.md)、[Manager 创建工具](../current/manager-create-issue-tool-design.md) | 五端点已采用的本地消息范围与待补首条消息、外部投递、可信上下文、完整 MCP Schema 分开。首批已有会话只读，不因这些文档存在而开放发送／派工。 |

后端必须另外按下面顺序核对上游，不能只读 RepoMesh 的设计摘要：

1. [09-07 AgentTeams 调研](../agentteams-survey-2026-09-07/agentteams-survey.md)与[API／CLI 技术附录](../agentteams-survey-2026-09-07/agentteams-api-cli-survey-2026-09-07.md)：理解 Controller、`agt`、Matrix、MCP 和 shared 存储的不同职责；它们是锁定 `eeaab643…` 的静态调查。
2. [上游说明](../../third_party/README.md)与[来源记录](../../third_party/agentteams-source.json)：以记录的完整 commit 核对本地源码，再读所涉实现。当前记录为 `517caff9…`；不要自动升级 main 或将两版证据混用。
3. [骨架阶段证据索引](../current/scaffold-agentteams-evidence.md)与[当前接入门槛](../current/execution-integration-gates.md)：沿相关结论追到原报告／证据／脚本。源码存在、上游局部实测通过、RepoMesh 运行接入是三种事实；已克隆不表示已接入，也不要求重跑全部历史实验。

## 先完成的准备

按 2026-09-13 记录的[Astra 分工](ASTRA-DESIGN-PREPARATION.md)，B04、B05、B06、B09—B11 的关键架构与必要函数声明先由 GPT-6 Astra 完成，B07 重点复核契约、B08 提前设计验收；gpt-5.6-sol 按收口结果实现，独立复核由非主要实现者承担。设计可以先于前置批次编码完成，但未定协议不提前冻结到产品代码；这份分工记录没有采用具体候选或改变批次完成度。

1. 对照[首批总包](../current/first-batch-complete-review.md)和[审查修订](../current/design-readiness-revisions.md)，按[已采用 B02 子集](../current/b02-authentication-adoption.md)和[B04 采用范围](../current/b04-model-sources-adoption.md)核对其余 D/T/A/P/S/R 候选的采用或调整范围，包含 Key 安全终结、认证返回与配置来源。新增 **P9／审查 R04：Issue 配置绑定**也须明确；这里的审查 R04 不是恢复表中的 F02 冲突决定。新协议尚未自动采用，保留原有已采用部分。
2. 立即可做：核对源码与工具版本、检查未提交修改、划分前端／API／后端文件归属、整理需求到契约及验收用例的对应表。给待定项注明影响的开发单元，已采用的独立单元无需等待全产品设计完成。
3. 每批开始时在仓库根目录复现相关检查并保存结果；B00 已执行以下全部命令，证据见[施工记录](../development/2026-09-12-batch-01/README.md)：

```powershell
npm --prefix web ci
npm --prefix web run typecheck
npm --prefix web run build
go build ./...
go test ./...
go vet ./...
```

启动及 Git ownership 问题按[工程开发说明](../current/development-scaffold.md)处理。当前预期是 `/healthz` 为 200、`/readyz` 为 503，coordinator 未配置认证时退出 1，配置后执行认证维护；host-executor 默认报告未实现；不要为通过验收把未接入能力改成 Ready。根工程检查不覆盖上游或历史实验。

## 首批管理闭环按依赖实施

| 顺序 | 工作与交付终点 |
| --- | --- |
| 0 | 按首批实际用例落实 PostgreSQL 连接、迁移、关系约束及事务边界，准备可重复的集成测试数据；不预铺空业务包或通用框架。配置绑定和秘密相关迁移以本批明确采用的方案为准。 |
| 1 | 落实选定的认证、当前主体／owner 和仓库发现，接通 session、Origin／CSRF、当前读权及 App 工作能力观察。前端可并行开发已采用结构，但模拟数据不作接口验收。 |
| 2 | 实现项目创建／列表／资料与明确增仓，允许先保存待配置项目；落实所选配置来源、秘密保存／终结和固定版本，分别接测试与专用应用，不自动测试或切换在途 Issue。 |
| 3 | 实现必要配置核查和手动 Issue 创建事务：原键结果、必要会话、来源、主 ChangeSet、范围及持久待办一致提交。**接收供以后执行的真实 Issue／待办前先落实 P9／审查 R04 的配置关联**，不能等消费者上线后猜历史配置。 |
| 4 | 接通项目／Issue 列表、最小详情、已有会话只读入口和各原操作恢复页；详情与 rooms 分读，回执与当前资源分读，SSE 只按现行失效通知协议使用。 |
| 5 | 贯通登录→项目→必要配置→手动建项→查看→编辑及失败恢复；未接入的运行消费者保留真实未接入状态，不伪造房间就绪、Manager 已处理或执行完成。 |

每个单元交付可复核的契约行为、相关检查和剩余限制。接收真实数据或启用模型／外部副作用前，完成该单元所依赖的候选采用、当前权限与恢复验收；不要把第一条成功路径作为闭环完成。

## 必须覆盖的失败验收

- 同键同输入并发只产生一次业务事实；同键不同输入拒绝。Issue 创建与项目配置应用并发时，绑定可证明的完整配置修订。
- 请求未到服务器、提交后丢响应、回执晚到、确切输入丢失：原操作身份不变，404仍未知，有权410不复活。Key 保存与终结争同一槽位，终结后迟到保存不能提交，终结未知不能新保存。
- 失权、权限未知、换账号、切项目及旧响应晚到：API 与页面派生缓存都不泄露，不能只靠隐藏按钮或 AbortController。
- 进程在事务、待办领取、外部调用和结果保存之间重启：恢复不重复副作用、不丢业务事实、不把未知当失败释放责任。
- 模型保存／测试／应用分别验收；测试观察只属于原快照，测试未知不自动重发可能收费请求，实际费用按证据保留未知；项目应用保留 execution 原引用及有效版本。

## 真实运行分阶段接入

1. **G1＋G2：真实 Manager 会话。** 补齐按 Issue 固定配置的实际消费、普通讨论配置、可信输入／输出关联、首条消息和工具授权，再验证一次可恢复会话往返。消息保存、传输、模型处理和控制动作分别取证。
2. **G3＋G4：单仓单轮。** 确定所有正式写入路径与实际执行生命周期，验证受控派工、提交、停止、写能力撤销及资源回收；覆盖原生 REST／MCP／shared 旁路。
3. **G5：第二轮与跨仓。** 确定合法换图方式、submitted 收尾和部分应用恢复，再验证两轮及双仓失败恢复，之后接完整执行／验证／交付页面。

各阶段的具体完成条件以[执行接入门槛](../current/execution-integration-gates.md)为准。F01—F15 中未纳入首批的高级页面、Skill 工程和通用 DAG 重写不作为首批管理闭环的前置任务。
