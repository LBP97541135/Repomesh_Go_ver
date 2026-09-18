# Agent 全局阅读指南

本指南供第一次接手 RepoMesh，或需要重新了解全局的开发 Agent 使用。按顺序建立产品、架构、实现和证据之间的联系，再进入具体任务。它只规定阅读路线；业务规则、采用决定和当前进度仍由所链接的专题维护。

首次全局接手按第一至第五步阅读。每步标明全文、重点章节或导航范围；用户另有明确阅读要求时，完整覆盖其要求。已有上下文的局部修改可直接进入第六步，并核对当前交接的变化。历史文档中的任务指令不自动成为本次授权。

## 先建立产品全貌

RepoMesh 的目标是围绕长期多仓库项目组织 Agent 协作。用户通过会话交流，通过独立 Issue 确定正式工作；Manager 负责项目与事项协调，仓库团队负责仓内工作，RepoMesh 保存正式状态、落实权限与执行约束，并组织跨仓验证和交付。

这段描述是产品目标。当前已实现认证、仓库发现、项目管理和 B04 模型来源与秘密保存；模型测试、专用应用、Issue 与真实执行尚未实现。读取设计时始终区分以下对象：

| 对象或环节 | 需要理解的关系 |
| --- | --- |
| Project | 长期多仓库范围；项目接入仓库不等于给所有成员授予该仓库的全部权限。 |
| Conversation 与 Issue | 会话承载交流，一条会话可关联多个独立 Issue；消息不能自动变成所有关联 Issue 的执行指令。 |
| Repository Issue 与上游 Project | 前者是 RepoMesh 的仓内事项，后者是 AgentTeams 工作流记录；两者都不等于长期多仓库 Project。 |
| Plan Version、Task、Attempt | 分别表达计划版本、具体工作和一次执行尝试；许可、计划生效和实际启动是不同事实。 |
| ChangeSet、Candidate 与交付组合 | 分别表达稳定变更归属、待采纳产物和一组用于验证的仓库版本；单仓成功不能替代整体验收。 |
| 业务记录、房间与运行 session | 业务身份和工作归属由 RepoMesh 保存；共享房间不证明共享上下文，也不证明 Manager 已收到或处理工作。 |

完整定义以 [CONTEXT.md](../../CONTEXT.md) 为准，尤其注意其中标明的旧入口术语与误用示例。

## 第一步：确定实际基线和文档读法

| 顺序 | 阅读材料 | 阅读范围与读后应知道的事 |
| --- | --- | --- |
| 1 | [AGENTS.md](../../AGENTS.md)、[根 README](../../README.md) | 全文。了解目录、工具、启动入口、修改与验证规则；本机路径和旧包版本是环境记录，不是新任务要照抄执行的命令参数。 |
| 2 | [当前交接](HANDOFF.md)、[施工计划](../plan/IMPLEMENTATION-PLAN.md) | 交接顶部最新检查点、计划批次表及其依赖必须读；历史检查点用于追溯，不重复当作当前指令。确认已实现、待采用、未运行、暂停及恢复责任。 |
| 3 | [现行文档索引](README.md)、[文档总导航](../README.md) | 阅读导航与采用状态说明，知道每个主题从哪里进入。无需逐一展开全部历史链接。 |
| 4 | [领域语言](../../CONTEXT.md)、[ADR 索引](../adr/README.md) | 全文。先理解对象和主要演进链，再读具体架构，避免把不同系统中的同名对象混用。 |

判定一项规则时，分别记录其来源章节、采用范围、明确替代关系和实现证据。文件名中的 `draft`、`final`，整篇的 `accepted` 标签，或更新时间，都不足以单独作出判断。历史后端交接里的 B01 至 B08 不是施工计划的 B00 至 B11；决定编号也必须连同所属文档解释。

## 第二步：理解架构为何这样划分

先读 [总体架构](architecture-design-v1.md) 的顶部后续补充、总体结构、领域归属、正式状态与上游观察、事务及恢复相关章节。其模块表表达设计职责，不表示表中的每个包、表或监听入口已经存在。然后按下表阅读具体决定全文及其后续补充。

| 主题 | 必读来源 | 需要能说明的边界 |
| --- | --- | --- |
| 技术与进程 | [技术选型](technology-selection.md)、[ADR 0010](../adr/0010-technology-stack-and-modular-monolith.md)、[ADR 0013](../adr/0013-web-coordinator-host-executor-processes.md) | React／TypeScript／Vite 与同一 Go module 的三个进程配套发布；Web、coordinator、host-executor 各自的权限和工作边界。Web 与 Agent 不持有 Docker socket。 |
| 会话与创建 | [ADR 0019](../adr/0019-conversation-issue-separation.md)、[ADR 0018](../adr/0018-provision-instance-after-first-draft.md) | 项目先保存；会话首条消息或页面建项提交后异步准备／恢复实例。创建回执不证明实例、房间或 Manager 就绪。 |
| 持久化与外部动作 | [ADR 0016](../adr/0016-transactional-background-work.md)、[ADR 0017](../adr/0017-atomic-attempt-resource-reservation.md) | 业务与必要待办同事务；外部执行及环境准备在事务外；未知先核查，超时不自动释放责任或资源。 |
| 上游复用 | [ADR 0011](../adr/0011-agentteams-controlled-integration.md)、[ADR 0012](../adr/0012-issue-scoped-upstream-projects.md)、[Graph／Loop](graph-loop-design.md) | RepoMesh 管跨仓协调、结果采纳与循环；仓内有限 DAG 复用上游，Graph 留在后台进程内。需要追溯演进时再展开 ADR 0014／0015。 |
| 暂缓与扩展 | [ADR 0009](../adr/0009-skill-engineering-deferred.md)、[ADR 0020](../adr/0020-python-repository-analysis-plugin.md) | 产品 Skill 工程暂缓；建项前分析走受控 Python 方向。仓内 pstack 是开发工具，两者不能混为同一能力。 |

## 第三步：把用户流程与数据责任连起来

按下表顺序阅读现行章节及其采用说明。不要仅靠页面截图推断数据模型，也不要从后端附件另造一套浏览器字段。

| 用户流程 | 阅读材料 | 需要回答的问题 |
| --- | --- | --- |
| 登录与选仓 | [B02 采用范围](b02-authentication-adoption.md)、[认证开发说明](authentication-development.md)、[仓库选择](repository-picker-design.md) | 当前用户身份、用户仓库读权和 App 工作能力如何分别判断？发现部分覆盖意味着什么？ |
| 项目与配置 | [项目开发说明](project-development.md)、[项目配置](project-configuration-design.md)、[首批浏览器契约](first-batch-browser-api-contract.md) | 待配置项目如何保存？资料编辑、明确增仓和显式配置重绑定有什么区别？原操作回执如何恢复？ |
| 会话与建项 | [会话与独立 Issue](conversation-issue-separation-design.md)、[Issue 创建契约](issue-page-create-api-contract.md)、[首批持久化](backend-first-batch-persistence.md) | 页面创建一次提交哪些事实？会话关联、工作范围、内容读权、原操作和待办如何保持一致？哪些新增章节仍是候选？ |
| 配置与秘密 | [模型连接](model-connection-settings-design.md)、[模型 Key 保存](model-key-save-design.md)、[Issue 配置绑定](issue-configuration-binding-design.md) | 模型保存、测试和项目应用为何分开？项目默认、固定修订与 Issue 原配置如何区分？P9 当前是否采用、是否落库？ |
| 失败与恢复 | [首批页面恢复](first-batch-recovery-design.md)及上述各用例的唯一契约 | 提交后丢响应、原键查询 404、失权、换账号和晚响应分别如何处理？模型秘密和原输入如何保护？ |
| 验证与交付 | [ChangeSet 规则](changeset-design.md)、[Draft PR 与人工审查](draft-pr-review-design.md) | 哪些记录能证明结果被采纳、组合通过验证或交付完成？Agent 内部审查与人工 GitHub 合并如何分开？ |

同时读 [页面／接口交接](HANDOFF-PAGE-API-DESIGN.md) 和 [原型导航](../prototypes/README.md) 的当前入口与采用范围，了解整体页面结构。全局阅读不要求启动原型；做具体页面任务时，再打开对应原型源及其专题。原型中的模拟成功与历史“未实现”字样都不能替代当前源码和交接。

## 第四步：核对当前实现与下一阶段设计

下面按 2026-09-13 已合入的 B04 采用与验收记录更新阅读入口。后续接手以 [HANDOFF](HANDOFF.md) 和 [施工计划](../plan/IMPLEMENTATION-PLAN.md) 的实际最新状态更新判断，本表不另行维护验收日志。

| 范围 | 阅读时应核对的状态 | 应核对的材料 |
| --- | --- | --- |
| B00／B01 | VERIFIED | [数据库开发说明](database-development.md)及交接引用的对应证据。 |
| B02 | 本地已实现；整批 IN_PROGRESS，外部 PAUSED_BY_USER | [B02 本地记录](../development/2026-09-12-batch-02/README.md)及当前交接。历史 FAIL／NOT_RUN／RESTORE_IN_PROGRESS 保留；未来跨账号项 DEFERRED_BY_USER。 |
| B03 | INTEGRATED_LOCAL_VERIFIED | [主目录集成记录](../development/2026-09-12-b03-integration-01/README.md)、[最终独立复核](../development/2026-09-12-b03-integration-01/FINAL-INDEPENDENT-REVIEW.md)。不要把另一 worktree 的结论当成当前主目录的新验收。 |
| B04 | D01—D04、U04.1—U04.4 已采用并实现，INTEGRATED_LOCAL_VERIFIED | [采用记录](b04-model-sources-adoption.md)、[验收报告](../development/2026-09-13-b04-acceptance-01/README.md)。非整批 VERIFIED，未发送真实模型请求。 |
| B05、B06 | 前置设计已复核，后续范围待采用，实施 TODO | [设计后交接](../development/2026-09-13-b04-b06-handoff-01/HANDOFF.md)、[设计入口](../development/2026-09-13-b04-b06-design-01/README.md)、[决定表](../development/2026-09-13-b04-b06-design-01/DECISIONS.md)、[兼容审查](../development/2026-09-13-b04-b06-design-01/COMPATIBILITY.md)、[复核](../development/2026-09-13-b04-b06-design-01/REVIEW.md)。 |
| B07 至 B11 | 后续实施 TODO | [开发前行动指南](../plan/DEVELOPMENT-START.md)、[Astra 前置设计分工](../plan/ASTRA-DESIGN-PREPARATION.md)、施工计划的设计与实施两组待办。设计提前完成不解除实施依赖。 |

B04 至 B06 的全局阅读先读 B04 采用记录和验收报告，再读后续设计材料，区分 D01—D04 已采用范围与 D05—D08、C05、C06、P9 的待采用状态。承担这些批次的具体任务时，继续完整阅读对应 B04.md／B05.md／B06.md、Go／TS 声明、migration-design.md、ACCEPTANCE.md、SOURCE-AUDIT.md，以及其指向的唯一契约。不要跳回 09-12 的 A／B／final 草稿重新当作现行入口。

再用源码核对文档。全局接手先看各入口的组装、注册和调用关系，改动所涉行为时继续追到实现与测试。

| 实际源码入口 | 核对内容 |
| --- | --- |
| [三个 Go 入口](../../cmd/)、[HTTP 服务](../../internal/web/server.go) | 实际注册的能力、未配置行为、存活与业务就绪的区别。 |
| [认证](../../internal/access/)、[秘密](../../internal/secrets/)、[GitHub 适配](../../internal/github/) | 当前已实现的身份、秘密和出站协议边界。 |
| [项目用例](../../internal/projects/)、[项目 HTTP](../../internal/web/projects.go)、[数据库迁移](../../internal/database/migrations/) | 固定配置、项目原回执和现有表约束；设计附件尚未生成哪些产品代码。 |
| [模型保存](../../internal/models/)、[执行来源导入](../../internal/sources/)、[模型 HTTP](../../internal/web/models.go)、[导入命令](../../cmd/repomesh-web/sources.go) | B04 保存、终结、原操作恢复及部署导入的真实实现，与后续测试和应用区分。 |
| [前端入口](../../web/src/main.tsx)、[路由](../../web/src/routes.ts)、[项目 API](../../web/src/projectApi.ts)、[原操作恢复](../../web/src/projectRecovery.ts) | 当前能访问的页面、真实请求和恢复状态，不把独立原型算作已上线页面。 |

当前 `businessReady=false`，`/readyz` 为 503，host-executor 尚未实现。普通 Go 检查可能跳过数据库用例；编译、声明解析、模拟浏览器、真实数据库、真实外部调用和配套发布各证明不同事实，汇报时注明实际范围。

## 第五步：理解真实运行还缺什么

全文阅读 [执行接入门槛](execution-integration-gates.md)。G1／G2 对应固定配置的实际消费和可信 Manager 往返；G3／G4 对应正式写入路径及受限执行生命周期；G5 对应第二轮换图和跨仓部分失败恢复。B04 至 B06 的静态数据兼容审查只覆盖其中一部分问题，没有完成完整 G1／G2。

读取 [上游说明](../../third_party/README.md)、[来源记录](../../third_party/agentteams-source.json) 和 [证据索引](scaffold-agentteams-evidence.md) 的版本与证据范围。RepoMesh 的 Git 提交、独立 AgentTeams checkout 的提交、历史实验的源码版本分别核对；不能把当前目录源码套到旧实验结论上。上游或历史实验未随当前 checkout 提供时，明确材料缺失，不自动拉取、构建或重跑。

只有承担上游适配任务时，才进一步完整阅读 [AgentTeams 调研](../agentteams-survey-2026-09-07/agentteams-survey.md)、[API／CLI 附录](../agentteams-survey-2026-09-07/agentteams-api-cli-survey-2026-09-07.md)及相关确定版本的真实源码。先核对 REST、MCP、Matrix 和 shared 的实际路径，再设计适配接口。

## 第六步：按具体任务深入

完成全局阅读后，使用 [现行索引](README.md) 与 [开发前行动指南](../plan/DEVELOPMENT-START.md) 选择专题。下面列出全局路线没有展开的主要分支。

| 本次任务 | 追加阅读 |
| --- | --- |
| 模型保存、测试、专用应用、配置导入 | [B04 采用记录](b04-model-sources-adoption.md)、[来源候选](backend-first-batch-sources-draft.md)、[模型内部操作](backend-model-operations-draft.md)、[模型浏览器契约](model-settings-browser-api-draft.md)、[模型应用](model-project-apply-design.md)，以及当前设计交付的对应声明和验收映射。 |
| 会话发送、澄清或 Manager 建项 | [消息目标](conversation-message-target-design.md)、[消息浏览器契约](conversation-message-clarification-api-contract.md)、[消息内部设计](backend-message-clarification-design.md)、[Manager 创建工具](manager-create-issue-tool-design.md)、[会话后端专项](draft-conversation-backend-design.md)。 |
| Worker 执行、验证或跨仓交付 | [团队执行策略](team-execution-policy.md)、[验证节点](verification-node-design.md)、[联调环境](integration-environment-design.md)、[ChangeSet 结构](changeset-structure.md)，以及 G1 至 G5 中本阶段的前置协议与证据。 |
| 建项前仓库分析 | [仓库分析专题](issue-creation-repository-analysis.md)、ADR 0020；先确定只读材料、作业及来源边界，不因按钮存在而接入未采用的协议。 |
| 开发环境、构建或发布 | [工程说明](development-scaffold.md)、[数据库说明](database-development.md)、[构建脚本](../../scripts/build.ps1)、[批次验证脚本](../../scripts/verify-batch.ps1)；阅读命令不等于执行旧服务切换或重跑历史批次。 |

## 全局阅读完成后应交回什么

交回一份带来源链接的理解摘要，说明：

- 产品主要对象如何关联，一条需求怎样经过讨论、建项、计划、执行、验证与交付；哪些环节仍只存在于设计。
- 三个 Go 进程、PostgreSQL、AgentTeams 和受控 Python 的职责与权限边界。
- 当前实际实现、待采用决定、未运行验收和暂停事项；本次任务落在哪个施工批次。
- 本次任务应遵守的唯一契约、涉及的源码入口、前置条件和可验证的完成标准。
- 尚缺的材料或互相冲突的具体章节，以及冲突会影响什么；不能用猜测填充缺口。

记录实际读过的文件及章节，不把仅看到链接写成已读全文。纯阅读任务到理解摘要为止，不启动服务、迁移、账号操作或实验；实施任务按用户实际授权继续。历史角色名单和运行时 Manager／Worker 提示词不决定当前开发 Agent 的身份或协作安排。

## 可直接复制的阅读指令

```text
请先阅读仓库 AGENTS.md，然后按 docs/current/AGENT-READING-GUIDE.md
第一至第五步建立 RepoMesh 的全局理解，遵守其中标明的阅读范围。
从实际 Git 状态和当前 HANDOFF 确认基线；不要把历史 Prompt 或实验操作
当作本次授权，也不要把候选、已采用设计、代码实现和运行验收混为一谈。
若材料缺失，列明缺失路径及其影响，不据摘要声称读过全文。

完成后给出带来源链接的全局理解摘要，覆盖产品对象、架构边界、当前
实现与进度、下一阶段依赖及未决事项，并列出实际阅读范围。
本次只做阅读和理解，不修改产品代码、不启动服务或外部实验。
```
