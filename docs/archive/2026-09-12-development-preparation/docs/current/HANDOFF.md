# RepoMesh 产品、架构与基础工程交接

**2026-09-12 审查修订：** [多 agent 审查](../../../../reviews/2026-09-12-design-readiness/README.md)发现 Key 保存恢复、Issue 配置归属、测试观察及登录返回的缺口，修订和分阶段开工条件见[修订入口](../../../../current/design-readiness-revisions.md)。新终结协议、绑定时点及导航扩展仍为候选；原型一致性和 Skill 适用说明按既有语义修正。业务仍是骨架，真实执行的[五项协议门槛](../../../../current/execution-integration-gates.md)未全部完成。下方 CP16 和“当前／无剩余分歧”描述保留为此前记录，不代表本轮新修订已采用或系统已通过验收。

**当前：首批六项完整待审包已成稿。** 用户“你直接给我这六项的全部”授权一次集中交付，见[总包](../../../../current/first-batch-complete-review.md)及[六项原型入口](../../../../prototypes/index.html#review)。已补认证/发现、模型保存/测试/专用应用、来源S01—S08及页面恢复；全部新增决定待用户，不逐页停问。F01/F02/F03/F04已采用的具体UI保留，F09及Key三项未采用；旧已收口r3不重开。只页面和API设计，开发另做。认证r2为2／3，模型API/配置来源/F02/公共恢复各r1为1／3；双方本次文档及定点核对均通过，无剩余文档分歧，无技术待回复。所有新增D/T/A/P/S/R仍待用户统一采用。最新收发、轮次及TODO见页面日志CP16；下方按历史时间记录的“未成稿/技术0轮”等状态以本条和CP16为准。

用户“是的，继续下一个”已采用F01登录恢复所问三项UI，技术r2、2／3；随后“继续”推进当前[F05 API Key保存与原结果查询](../../../../current/model-key-save-design.md)，r1文档核对通过、1／3，三项新UI仍待用户。F09和认证三方向未因此采用，完整Key／测试／应用协议仍未完成。只页面／API设计，开发另做，准确收发与TODO见页面CP15。

用户“继续下一项”后，当前展示[F01登录与重连恢复r2](../../../../current/login-recovery-page-design.md)。沿原入口主题第二轮，三项新UI待审；F09三项及认证A／B／C方向未因“继续”自动采用。当前只页面／API设计，开发另做；最新原文／轮次／待回复与TODO见页面日志CP14。

**当前继任工作（2026-09-11）：** 页面新任务已恢复双方原文及最终版本；新ID和独占归属见[通信约定](design-communication.md)，已核实继任后端设计师2并建立通信，不恢复旧设计师。当前展示[F02项目配置编辑](../../../../prototypes/repomesh-project-settings-prototype.html)，规则及未完范围见[项目配置专题](../../../../current/project-configuration-design.md)。三段顺序、显式配置开关、摘要后确认已获用户同意，并要求主界面可找到；F02新技术核对仍0／3，F01—F15保留待办；只原型和文档，不新增业务实现／真实验证结论。

**页面设计换会话交接（2026-09-11）：** 请从[新版页面／接口交接](HANDOFF-PAGE-API-DESIGN.md)开始，含已采用原型、F01—F15未完成设计表单、接口缺口及继任者通信/文件归属；[旧版已归档](../../../2026-09-11-page-api-handoff/HANDOFF-PAGE-API-DESIGN.md)。

**当前设置评审（RM-UI-MODEL-SETTINGS r3）：** [供应商与模型原型](../../../../prototypes/repomesh-model-provider-prototype.html)按用户参考图改为左供应商、右配置和模型列表；填写项核对AgentTeams锁定源码517caff，r3技术已ACK。用户回复“ok可以的”，采用当前供应商分栏及模型参数填写方式；多供应商映射、密钥及专用应用协议未完成。来源及边界见[模型设置专题](../../../../current/model-connection-settings-design.md)。

**当前入口评审（RM-UI-PROJECT-ENTRY r1）：** [登录与项目入口原型](../../../../prototypes/repomesh-project-entry-prototype.html)展示模拟登录、选仓、名称/用途、默认配置摘要及保存后继续配置；后端技术及落盘已核对；用户回复“正确”，采用已展示的“先选仓库、再填资料”两步流程及授权提示。模型设置另行展示讨论。规则见[项目配置专题](../../../../current/project-configuration-design.md)。只原型/文档，不接真实OAuth或密钥；完整认证/发现/Key接口仍需补齐。


**当前实现设计（RM-UI-MESSAGE-TARGET r3）：** 用户要求与后端讨论具体实现，双方已确认[消息与澄清五端点契约](../../../../current/conversation-message-clarification-api-contract.md)及[后端实体/事务/受控动作](../../../../current/backend-message-clarification-design.md)。明确请求直接解释，歧义答复绑定原问题，单问题唯一答案，原键幂等恢复；第3／3轮技术无分歧。双方最终落盘已互核，无遗漏实质分歧；仅文档，真实实现/验证未开始，完整B05/多源/更正/适配与执行尚未完成。消息控件仍待用户评审，原型保持。


**当前讨论（RM-UI-MESSAGE-TARGET r1）：** [消息目标与澄清原型](../../../../prototypes/repomesh-message-target-prototype.html)在主会话内展示Manager的目标问题、候选填入可编辑答复、带引用发送及可见目标。技术r1已ACK，具体控件待用户评审；仅预置脚本，不是语义识别或执行验证。详细规则见[消息目标专题](../../../../current/conversation-message-target-design.md)。

**已采用（RM-UI-ROOM-NAV r2）：** 按用户最新要求改为[常驻悬浮入口＋同页右栏](../../../../prototypes/repomesh-conversation-dock-prototype.html)：点击条目展开DAG/Leader/Issue内容，保留主会话；随窗口宽度收成图标，窄窗自动收起内容栏并可重新展开。替代r1固定索引栏与快捷离页方式；技术r2及落盘已核对，用户回复“正确的，进行下一项”采用已展示交互。当前只原型/文档，真实API/核权/恢复未实现。详细边界见[页面专题](../../../../current/conversation-issue-separation-design.md)。

**当前页面进展：** 用户“是的”采用[只读DAG](../../../../prototypes/repomesh-issue-dag-prototype.html)布局及图下节点详情（RM-UI-PLAN r2）。依新要求展示[会话右栏导航](../prototypes/repomesh-conversation-navigation-prototype.html)：按关联Issue索引DAG与只读Leader房间，可收起；仅限会话页。RM-UI-ROOM-NAV r1技术已ACK，具体宽度和排布待用户评审。规则见[页面专题“当前会话右侧导航”](../../../../current/conversation-issue-separation-design.md)。完整图/关联查询/消息/房间协议、真实实现与验证仍未完成。

**当前页面决定（RM-UI-01 r3）：** 用户指定创建 Issue 使用居中弹窗，保留原列表与侧栏。当前[弹窗讨论稿](../../../../prototypes/repomesh-issue-modal-prototype.html)已展示，用户回复“正确”采用当前大小和字段排布；后续新增交互继续展示讨论，创建接口与原键恢复不变。详见[页面专题](../../../../current/conversation-issue-separation-design.md)。此条覆盖下文原创建表单独立页的呈现描述。

**2026-09-10 新双任务设计协作：** 用户授权“页面接口设计师1”与“后端设计师1”共同维护设计，职责及精确 ID 见[当前通信约定](design-communication.md)。本阶段只产出文档／独立原型，业务实现状态仍为下述骨架。页面已补首批路由、创建未知恢复和失权清理（RM-PAGE-01 r1）；双方采用首批单主会话、同 Issue 同仓 0..1 仓库事项、只增内容保护范围及项目配置修复保留受限仓（RM-B01-04 r2）。详见[页面交接进展](HANDOFF-PAGE-API-DESIGN.md)、[后端交接](HANDOFF-BACKEND-DESIGN.md)。原文涉及旧任务身份、旧“本轮只编写交接”的范围只作历史；不恢复旧任务，未完成部分不因新协作视为已完成。

首批[项目／列表浏览器补充契约](../../../../current/first-batch-browser-api-contract.md)已按RM-API-01 r3完成双方落盘互核；[后端持久化](../../../../current/backend-first-batch-persistence.md)与之同版本。完整认证、全部参与仓库发现、Key设置、会话消息及运行协议仍需继续。用户追加要求“每次到具体的页面设计，需要展示原型和我讨论”，用户进一步要求遵循 docs/prototypes/ 既有设计，三方案选择已撤回；当前沿用会话与独立 Issue 原型做增量并逐页展示讨论；技术契约收口不等于具体页面最终采用、业务实现或真实验收。

更新：2026-09-10，补充新版上游验证事实。入口与创建设计以双方持续授权落实回执和任务交接记录为基线；09-09 后续用户采用按最新版本复用上游 DAG，已补充 ADR-0014／0015 并整理 [Graph／Loop 专题](../../../../current/graph-loop-design.md)。实际工作区：`D:\Project4work\Repomesh_Go_ver`。

本文只维护**当前状态、约束和接续范围**，细则由 ADR 与专题承载。已采用不等于已实现；目前已有 Go 三入口及 React／TypeScript／Vite 最小骨架，业务仍未实现，既有本地内存原型保留为设计参考。上游已有组件、双实例真实部署、模型往返及执行权限证据，尚无 RepoMesh 全链路运行验收，范围见第 6 节。

## 0. 本轮基础工程阶段

用户本轮明确授权创建基础代码、工程配置和必要依赖，替代旧交接在本任务范围内的“仅设计、不编写代码”。既有产品决定、权限边界和暂缓模块继续有效；历史协作指令不恢复旧任务、不联系旧协作者。三位本轮 agent 分别覆盖 ADR/领域、AgentTeams 证据、页面/接口/后端文档；主 agent 实施骨架，非主要实现者独立复核。

实际工程与命令见[根 README](../../../../../README.md)、[基础工程开发说明](../../../../current/development-scaffold.md)，检查与独立复核见[验收记录](../../../../current/scaffold-verification.md)。Web 提供静态骨架页、`/healthz` 200（仅进程存活）、`/readyz` 503（业务未实现）；未知业务 API 返回 404。后台协调与受限主机执行只提供版本和未实现诊断，默认以 1 退出，不接受任务或执行主机操作。三个入口共用一个产品 Go module，前端与三个二进制同版本配套构建。

Issue、计划、调度、权限、GitHub、AgentTeams、数据库/队列/对象存储仍未实现。Graph 保持未来后台进程内模块，Skill 不建代码占位；Python 仓库分析只记录 ADR-0020 扩展边界。`validation/go.mod` 只隔离历史实验编译范围，未改旧报告、脚本、证据或重跑实验。骨架完成不等于旧验证全部通过，也不等于业务或 AgentTeams 集成验收完成。

## 1. 接手入口与资料优先级

**后续上游源码整理（2026-09-10）：** 用户要求删除旧独立 AgentTeams 仓库并重新克隆到 RepoMesh。新源码位于 `third_party/AgentTeams/`，官方 main 本次核对为 `517caff9280242a00a4d4c06365352b9e41659c6`，工作树干净且 Git 完整性检查通过。旧目录的全部文件先归档校验，再移入 Windows 回收站；两轮 validation 保持原样。来源、重建和备份见[上游说明](../../../../../third_party/README.md)。该 clone 独立于产品 Go module 和发布包，不构成 AgentTeams 接入或新增验收。

- 正式开发前的设计缺口分为[页面／接口 handoff](HANDOFF-PAGE-API-DESIGN.md)与[后端 handoff](HANDOFF-BACKEND-DESIGN.md)，分别维护优先级、设计产物和开发就绪条件。[旧页面 Prompt](NEXT-SESSION-PROMPT.md)及[旧后端 Prompt](NEXT-BACKEND-SESSION-PROMPT.md)保留完整阅读清单、原型及历史协作来源；旧指令不恢复为当前任务授权。
- [现行索引](README.md)提供主题入口；[CONTEXT](../../../../../CONTEXT.md)只维护术语定义；[总导航](../README.md)区分现行与历史资料。
- 用户最新明确决定及其适用范围优先；ADR 与专题结合**具体后续补充和定点替代说明**阅读，不能单按编号、文件日期或 `accepted` 标记覆盖全部旧规则。[创建接口契约 v1](../../../../current/issue-page-create-api-contract.md)是当前创建 REST／Issue SSE 的字段基线。
- 旧 Prompt、原型和通信日志用于溯源。其中的“最新”“待批准”“下一步”只描述记录当时，不自动成为本次任务指令。设计接手时仍按通信约定完整读取日志，文档评审不代表恢复旧设计任务或联系旧协作者。
- 本次压缩整理前的三份交接原文保存在[评审前快照](../../../2026-09-09-before-document-review/README.md)。其他历史入口见[归档索引](../../../README.md)，历史内容不恢复为当前规则。

## 2. 当前设计状态

**旧项目调查后的新增选择：** 用户采用创建 Issue 表单中的“仓库分析”按钮及 Python 插件。当前只推进仓库关联分析；历史决策分析和向量存储仍属后续能力。交互、独立分析作业、可选来源关联及迁移范围见[仓库分析专题](../../../../current/issue-creation-repository-analysis.md)，进程扩展见 [ADR-0020](../../../../adr/0020-python-repository-analysis-plugin.md)。本条补充设计，保留下方 09-10 上游验证事实；实际按钮、接口和插件均未实现。

| 范围 | 当前基线与权威入口 |
| --- | --- |
| A—E：团队执行、验证、联调、Graph／Loop、draft PR | 已确认。见[团队执行策略](../../../../current/team-execution-policy.md)、[验证节点](../../../../current/verification-node-design.md)、[联调环境](../../../../current/integration-environment-design.md)、[PR 审查](../../../../current/draft-pr-review-design.md)及 ADR-0001—0007。 |
| ChangeSet | CS1—CS14 已确认；F／G 以 CS9—CS12 为准。见[设计](../../../../current/changeset-design.md)及[结构说明](../../../../current/changeset-structure.md)；结构示例不冻结数据库实现。 |
| H：Skill 工程 | H1—H15 方向已同意，但整个模块待定占位、暂不开发。见[ADR-0009](../../../../adr/0009-skill-engineering-deferred.md)及[专题](../../../../current/skill-engineering-design.md)。 |
| I：执行模式与审批 | 当前统一 YOLO；后续审批主要规则已确认，能力后续开发。见[ADR-0003](../../../../adr/0003-plan-change-authorization-and-activation.md)及[ADR-0006](../../../../adr/0006-manager-entry-modes-and-skill-driven-execution.md)。 |
| J：项目配置与授权变化 | J1—J3 已确认，技术接入和恢复细节继续设计。见[项目配置](../../../../current/project-configuration-design.md)。 |
| 技术栈 | 整组方案及模块化单体已采用，选型收口。见[ADR-0010](../../../../adr/0010-technology-stack-and-modular-monolith.md)及[技术选型](../../../../current/technology-selection.md)。具体版本及未点名的库、对象存储产品未选定。 |
| 架构 | ADR-0011—0017 的受控接入、上游映射、三类进程、Graph、轮次 DAG、事务待办、资源预留已采用。09-09 补充明确复用原生仓内 DAG，RepoMesh 管跨仓及 Loop。见[架构 v1](../../../../current/architecture-design-v1.md)及 [Graph／Loop 专题](../../../../current/graph-loop-design.md)；具体接入仍有未完成设计。 |
| 会话、Issue 与准备时序 | [ADR-0019](../../../../adr/0019-conversation-issue-separation.md)确认会话与独立 Issue 分离；现行准备触发按[ADR-0018 后续补充](../../../../adr/0018-provision-instance-after-first-draft.md)及创建契约。旧草稿转正和每项独占用户主会话的绑定已被替代。 |
| K：页面与接口 | 已进入细化阶段。[当前页面专题](../../../../current/conversation-issue-separation-design.md)、[消息目标方向](../../../../current/conversation-message-target-design.md)、[双入口创建机制](../../../../current/manager-create-issue-tool-design.md)及[创建契约 v1](../../../../current/issue-page-create-api-contract.md)已有采用范围；其余页面、消息和运行协议继续设计。 |
| 建项前仓库分析 | 可选按钮 → 受控 Python 分析 → 查看并应用建议 → 最终选仓建项。Go 管身份、固定材料、作业、结果及同事务来源；失败可手动创建，分析不触发 AgentTeams 准备。见[专题](../../../../current/issue-creation-repository-analysis.md)。 |
| 持续授权 | 用户原话“之后的待决定都批准，按照建议来。”见[授权与采用清单](../../../../current/design-delegation.md)。后续推荐方案对齐、记录理由与代价后推进并报告；已有否决、暂缓和业务约束保持，未写完／待验证不等于待批准。 |

### 现行创建与运行入口

页面提交之前，可独立执行仓库分析并将建议应用到选仓；它只产生辅助分析记录。正式提交可带 `repositoryAnalysisId`，分析来源随以下创建事务保存。完成分析本身不进入图中的实例准备流程。

```text
保存长期项目资料（不启动实例）
    |
    +-- 首次保存业务会话及首条消息
    |       -> 消息和对应待办先持久化
    |
    +-- 页面创建 Issue（默认新会话，也可选择有权的已有会话）
            -> 必要新会话、来源、快照、幂等结果、
               Issue、主 ChangeSet、对应待办及事件同事务保存
    |
提交后异步核验条件，准备／恢复项目实例及关联主房间
    |
实际 Manager／主房间就绪 -> 投递已保存消息或接手已有 Issue
    |
明确工作目标，核验当前计划、权限、预算与资源后推进
```

Manager 受控 MCP 与页面共用创建／结果查询业务逻辑；Manager 接手页面已建 Issue 时沿用该 Issue，不再次创建。项目创建、空会话和只读访问本身不启动；已有实例／房间复用，外部调用在事务后。创建成功、运行就绪、消息传输、Manager 接收／处理与 Worker 派工分别取证，准备失败保留业务事实。具体字段见[创建契约](../../../../current/issue-page-create-api-contract.md)，创建事务的后续补充见[ADR-0016](../../../../adr/0016-transactional-background-work.md)。

### 原型与实现完成度

当前原型为 `repomesh-conversations-issues.html`，准确路径、正文源和截图见[页面接手 Prompt](NEXT-SESSION-PROMPT.md)与[当前页面专题](../../../../current/conversation-issue-separation-design.md)。

已演示独立 Issue 列表／详情、默认新会话创建、来源记录、主／只读 Leader 房间导航。**可选已有会话、页面提交后自动准备和仓库分析按钮尚未补入原型**；真实 MCP／REST／SSE、权限、数据库事务、Python 插件及上游运行接入均未实现。原型状态不覆盖已采用契约，浏览器走查不构成后端验收。

## 3. 必须保留的业务与执行约束

| 方面 | 必须保留的约束及细则入口 |
| --- | --- |
| 项目与角色 | 每个长期多仓项目独立 AgentTeams 实例，一个长期 Manager，每仓长期 Team／Leader；所有需求包括单仓均由 Manager 接收。首期最多一台服务器，已有双实例低负载资源样本，尚未确定可承载容量。见[ADR-0001](../../../../adr/0001-agentteams-issue-concurrency-and-isolation.md)及[资源实测](../../../../../validation/agentteams-2026-09-09/reports/capacity-live.md)。 |
| 会话与事项 | 会话可无 Issue，也可关联多个独立 Issue；各项独立目标、计划、验收、默认主 ChangeSet 和执行归属。内部仓库事项可关联 GitHub Issue；首期不默认创建、评论、同步或关闭外部 Issue。重复导入先查映射，不自动合并需求。见[ADR-0019](../../../../adr/0019-conversation-issue-separation.md)。 |
| 消息目标与证据 | 人通过 Manager 沟通，明确自然语言工作目标；歧义先澄清，导航来源不覆盖正文明确目标。准备期允许保存补充，按服务端保存顺序尝试投递，前序未知先核查。保存、投递、接收／处理分开；新会话多事项下的顺序范围和 runtime 映射继续设计／待验证。见[消息目标](../../../../current/conversation-message-target-design.md)和[后端专项稿](../../../../current/draft-conversation-backend-design.md)。 |
| 权限与配置 | 账号访问、App 能力、项目范围和本次工作范围共同核验，不放大发起人有效 Git 权限。正式提交可执行 Issue 前核对必要配置；部分失权不整体禁止仍有权的工作。停止无权操作、阻塞依赖，历史读取按当前权限；扩仓不自动改变在途 Issue。见[项目配置](../../../../current/project-configuration-design.md)及[ADR-0002](../../../../adr/0002-github-app-authorization-and-draft-pr-delivery.md)。 |
| YOLO 与计划 | 当前不插入人工审批或额外开工门禁；缺失规则可由 Manager 解释并记录来源，不能覆盖用户要求、放大预算权限或降低验收。Plan Version、许可、生效分开；受影响上游 Project 停止新派工，在途及 submitted 收敛后应用、读回，多目标部分应用不宣布整体生效。受限 YOLO 不隐式改为人工审批等待。见[ADR-0003](../../../../adr/0003-plan-change-authorization-and-activation.md)、[ADR-0004](../../../../adr/0004-acceptance-rule-clarification.md)、[ADR-0012](../../../../adr/0012-issue-scoped-upstream-projects.md)。 |
| Worker 与资源 | Worker 单活跃 Attempt；每次 Attempt 独立副本、分支、构建目录和记录。Worker 并发默认 1，不含 Manager／Leader／验证负责人，并受项目／全局额度约束。Attempt、Worker、容量统一预留，环境在事务外准备、启动前再核验；失联或超时先核查旧写入能力，不直接释放名额或开替补。见[团队执行策略](../../../../current/team-execution-policy.md)和[ADR-0017](../../../../adr/0017-atomic-attempt-resource-reservation.md)。 |
| Graph 与循环 | 复用上游仓内当轮 DAG 的建图、依赖校验和候选就绪计算；进程内 Graph 模块管跨仓前驱、业务结果适用性和 Loop，后台核验后派工。默认修复 3 轮、诊断 2 轮、环境恢复 2 次；预算或时限先到先停，换计划不清零，连续两轮同一失败且无新增有效证据停止。自然语言调整，直接编辑图延后。版本、职责和恢复见 [Graph／Loop 专题](../../../../current/graph-loop-design.md)及 ADR-0007／0014／0015。 |
| 验证与联调 | 验证小组默认关闭，Manager 组织；正式验证者与本轮业务作者分离，业务 Team 修缺陷、验证侧独立重验。验证绑定实际固定 commit 组合、配置、数据与前提，未修改但参与仓库也固定基线；必检证据满足才通过。五类结果、停用收尾与重验规则见[验证节点](../../../../current/verification-node-design.md)；mock 不证明真实链路，验证环境不向业务仓推修复，见[联调环境](../../../../current/integration-environment-design.md)。 |
| PR 与完成 | Agent 只推工作分支并开 draft PR，同一 Issue 同仓默认持续更新一个 PR。有效验证、内部复核及必需检查满足后推荐审查，由人转正式 PR、按仓库规则合并；YOLO 不授予自动合并权。完成须满足交付、验收、必需 PR 合并及最终实际组合证据；无代码工作按交付约定判断，部署另记。见[PR 审查](../../../../current/draft-pr-review-design.md)和[ChangeSet](../../../../current/changeset-design.md)。 |
| ChangeSet 与恢复历史 | Candidate 正式提交即收录，未接受也保留；正式组合从内部初审接受候选选定，未修改仓库固定基线，新候选不静默替换在途组合。版本或前提变化重判证据；部分合并保留成功事实，不自动撤销。本轮修复沿用原 ChangeSet，完成后新增范围另建关联事项；撤销已合并代码由人发起关联恢复工作。事实、判断、更正、幂等和保留规则见[ChangeSet](../../../../current/changeset-design.md)。 |
| 执行边界 | Web／Agent 不直接持有宿主 Docker socket；受限主机执行进程落实明确归属的环境操作。AgentTeams 接入通过受控工具及实际执行约束，不能以提示词代替控制。普通容器不承诺 VM 级或恶意多租户隔离，关键数据需服务器外备份。见[ADR-0011](../../../../adr/0011-agentteams-controlled-integration.md)、[ADR-0013](../../../../adr/0013-web-coordinator-host-executor-processes.md)和[团队执行策略](../../../../current/team-execution-policy.md)。 |

## 4. 未完成事项与下一步

**本次缺口交接（2026-09-10）：** 用户要求将上一轮评估落成两份独立交接，见[页面／接口 P01—P07](HANDOFF-PAGE-API-DESIGN.md)及[后端 B01—B08](HANDOFF-BACKEND-DESIGN.md)。推荐先收口登录／权限、项目保存、手动建项、列表与详情所需的页面状态、最小数据约束、接口与失败验收，再接运行准备／Manager、分析与受控执行。无需先冻结全产品设计；该排期不表示新增产品决定或本次已启动业务开发。下表为保留的候选事项索引，编号不覆盖新 handoff 的推荐顺序。

以下是基础工程之后保留的业务设计与实现候选，不属于本轮骨架授权的执行清单。仓库分析仍按[专题 §5](../../../../current/issue-creation-repository-analysis.md#5-页面接口与后端交付顺序)记录页面状态、Python 裁剪、Go 作业／来源关联与验证方向；本轮不实施。下表原有工作保留，不能因旧清单把这一新增用户选择遗漏，或误以为仅剩 Manager MCP。

| 顺序 | 接续工作 | 当前边界 |
| --- | --- | --- |
| 1 | 补齐 Manager 创建／结果查询 MCP 的完整 Schema、可信身份／消息来源与逻辑操作承接协议。 | 名称和业务职责已采用，完整 `inputSchema`／`outputSchema`、传输接入和可信操作生成／恢复尚未编制。不能用模型自报身份、随机传输号或会话唯一键替代。见[双入口机制](../../../../current/manager-create-issue-tool-design.md)。 |
| 2 | 深化 Issue 列表／详情、内部仓库事项粒度与生命周期、Leader 房间拓扑。 | 一会话多独立 Issue 和实际房间导航已采用；Issue 的反向主会话关系、仓库事项数量和共享房间映射仍需具体设计。 |
| 3 | 细化会话消息保存、投递、澄清、更正、并发恢复和晚到结果。 | 保留明确目标、前序未知核查及原归属；不把语义理解、结构校验和实际权限混为一项保证。 |
| 4 | 将可选已有会话与页面提交后自动准备补入本地原型。 | 已采用设计，仅补演示完成度；按对应页面场景走查，不宣称真实接口已实现。 |
| 5 | 完善其他列表／快照、项目创建、全站登录、权限变化、模块职责及数据库约束。 | 创建契约的 REST、Issue SSE、分页／长度参数已有基线；项目／会话列表通知、其他 API 和具体存储算法未完成，不能概括成“所有接口字段均未确认”。 |
| 6 | 细化运行准备、配置门槛、容量、无 Issue 讨论的工具／费用／预算归属及恢复。 | 启动触发已采用；实际 runtime、房间／session、运行身份、凭据、故障收敛和资源能力仍需实现后的验证。 |
| 7 | 按 Graph／Loop 复用方向细化执行 Adapter、结果映射与换图恢复。 | 从单仓单轮原生 DAG 的受控闭环开始；跨仓／业务放行、去重和累计限制由 RepoMesh 验证，不先重写通用仓内 DAG 引擎。 |

后续设计参考[持续授权](../../../../current/design-delegation.md)、[通信约定](design-communication.md)和两份角色 Prompt 的历史记录，但按新任务实际授权确定范围与协作者。本轮已实施基础代码；该事实不恢复旧任务，也不授权本轮开展 MCP、业务页面或业务后端集成。

## 5. 已替代、已排除与延后

- **已替代：** 首次输入即正式 Issue、会话整体转正、每条 Issue 独占用户主会话。首条正式 Issue 后才准备实例先由 ADR-0018 替代，旧 Draft Issue 触发再由 ADR-0019 与 ADR-0018 后续补充衔接到现行会话／页面建项入口。
- **已否定：** 第一版 A／B／C 整体布局与入口。保留用户截图的深色会话式风格、左侧项目分组；旧[原型记录](page-interface-prototype.md)、[草稿入口](draft-issue-and-room-entry-design.md)及[项目先建立记录](project-first-entry-design.md)只提供对应历史和仍有效的局部规则。
- **未采用：** 固定三栏、项目默认私有／邀请成员的旧建议；项目创建即启动、单仓需求直达 Leader、每条 Issue 复制整套团队；自动扩仓、合并或回滚；承诺仅暂停某个上游分支。
- **延后：** Skill 工程整体开发、后续审批能力、直接编辑图。Skill 工程不创建空代码、接口、功能开关或页面；已有方向与延后范围见[ADR-0009](../../../../adr/0009-skill-engineering-deferred.md)。
- **仍暂停：** `grill-with-docs` 工作流。历史授权原本仅允许设计与可丢弃原型；本轮新增允许基础代码、配置及必要依赖，不扩大为业务集成、外部服务部署、远端写入或恢复旧 Codex 任务。

## 6. 通信现状与证据限制

**09-10 增量：** 上游锁定 `517caff9280242a00a4d4c06365352b9e41659c6`，见[差异与官方测试](../../../../../validation/agentteams-2026-09-10/reports/controller-delta.md)。三包 311 个通过事件＝252 顶层测试＋59 子测试；rv-c 的[真实 API／CLI 报告](../../../../../validation/agentteams-2026-09-10/reports/controller-api-live.md)包含 51 项主观察、31 项真实 SA TeamLeader 范围检查，不能累加成独立业务验收通过数。TeamLeader 身份来自实际 membership，本 Team 200、跨 Team 404；匿名／无效 token 的 401 另记为认证拒绝，不冒称普通 Worker 角色隔离。

**09-10 双在线补测：** [c/d 双实例读取矩阵](../../../../../validation/agentteams-2026-09-10/reports/controller-twin-read-live.md)完成 **313 个断言、260 次 Controller HTTP**；两实例各有两个同名 Team，使用 Admin 与真实 SA TeamLeader 验证新增读取路径。**30 次错实例请求均为 401，每次来源前后请求均为 200；24 个夹具原字节哈希前后不变**。这是停止／未托管引用和元数据夹具的读取契约验证，不是业务验收。原 51／31 项保留独立历史计数；本次在线、TLS 与来源身份有效的对照补充旧时钟异常观察，不覆盖或改写其原始证据。

**09-10 存储与模型补证：** [真实S3 trace](../../../../../validation/agentteams-2026-09-10/reports/mermaid-storage-read-live.md)补齐20项检查：JSON前后正对照读取TaskMeta，Mermaid true/false有限窗口内目标TaskMeta的HEAD／GET均0。[新版Manager实际推理](../../../../../validation/agentteams-2026-09-10/reports/manager-model-smoke-new.md)随后通过一次Matrix消息返回精确标记，原生会话及用量确认1次模型调用、未观察到工具调用；Console503和后置读探针错误保留。以上均不代替RepoMesh业务验收。

接手时新增注意：`format=json` 返回 400；Task inspection 有 TaskMeta 时读取原始状态，否则回退规范化图状态，history/trace 不证明迁移审计或实际执行。`end` 的 Mermaid HTTP 200 原字节已在[浏览器中解析失败](../../../../../validation/agentteams-2026-09-10/reports/mermaid-browser-live.md)；CLI 详情传 `--team` 仍返回同名歧义 409。09-09 历史结论保留，源码未变部分只复用旧版限定证据，不宣布新版全套已过；CFG01—05已完成到真实REST／CLI／CR、存储投影及QwenPaw运行进程内存配置，见[Manager实测](../../../../../validation/agentteams-2026-09-10/reports/manager-config-live.md)；CFG阶段未以配置读回代替推理；后续实际推理见上述模型专项。时钟回退导致的证书错误已单独恢复，失效绑定由REST先清空，正确CLI随后重复清空。本次补充不改变 RepoMesh 业务 REST／SSE 设计。

截至双方交接记录：后端持续授权回执已被页面实际收到并保存在页面 CP15，双方创建基线同步完成，**无待回复设计问题**。页面最后发送交接通知，后端已保存，通知无需回复；不能从旧检查点恢复“等待回执”。见[页面日志](design-communication-page-log.md)末尾和[后端日志](design-communication-backend-log.md)末尾。

COMM-001 已收口；DESIGN-001／003 保持 1／3，DESIGN-002／004 保持 3／3，不清零或重开第四轮产品讨论。旧任务身份是交接证据，实际继续协作前按角色 Prompt 核实接替关系。每次通信即时保存原文，压缩恢复按[通信约定](design-communication.md)完整读取双方日志并补检查点，不以摘要覆盖历史，也不声称安装了压缩钩子。

AgentTeams 的[静态机制调研](../../../../agentteams-survey-2026-09-07/agentteams-survey.md)和[API／CLI 调研](../../../../agentteams-survey-2026-09-07/agentteams-api-cli-survey-2026-09-07.md)固定提交 `eeaab64391ccaec9118e84977f538aefd40720d6`。2026-09-09 重新查询官方 main 仍为该提交，最新正式 release 为 v1.2.3；采用最新确定源码作为 Graph／Loop 接入基线。后续已从该源码构建四个官方 Dockerfile 镜像并启动两套独立验证环境，见[部署记录](../../../../../validation/agentteams-2026-09-09/reports/deployment-run.md)；这不是 RepoMesh 产品部署完成。

2026-09-09 的[接入验证清单](../../../../plan/agentteams-validation-plan.md)已开始实际执行，逐项状态和专项证据见[执行记录](../../../../../validation/agentteams-2026-09-09/reports/validation-status.md)。真实服务已确认 paused replan 返回 409、原生 Worker 请求的 CPU／内存限制未生效；[Worker 自有凭据实验](../../../../../validation/agentteams-2026-09-09/reports/worker-auth-live.md)进一步确认 REST 拒绝不能覆盖 shared 写权限及原生工具 role 参数覆盖。另已核查双实例六个实际模型响应，以及 Manager 启用内置 Matrix、Leader／Worker 启用自定义通道的差异。缺失附件仍可能被接受、取消后 Project 与 Task 终态不一致等结果见[产物与副作用报告](../../../../../validation/agentteams-2026-09-09/reports/worker-side-effects-live.md)。这些原生缺口复现不等于 RepoMesh 业务验收通过，也未决定采用哪种补丁。版本与证据分级集中见 [Graph／Loop 专题](../../../../current/graph-loop-design.md)，受控 resume／replan 与 paused replan 补丁尚未选定。

随后补测原生 DAG 的17个就绪阶段、通知结果未知后的两种进程恢复、同名 Worker 删除重建及认证缓存到期、checkpoint真实响应和双Worker官方回归成本，入口为[按原清单复核完成度](../../../../../validation/agentteams-2026-09-09/reports/completion-audit.md)。旧SA只在缓存窗口暂时有效，旧Matrix令牌与存储凭据的撤销行为不同；checkpoint代理200实际返回HTML。各项限定证据应进入运行准备与恢复设计，不能改写为业务验收已完成。

另已补齐[跨Team同名Project读写](../../../../../validation/agentteams-2026-09-09/reports/project-team-disambiguation-live.md)及[原生采集耗时](../../../../../validation/agentteams-2026-09-09/reports/observation-latency-live.md)。[真实runtime回复实验](../../../../../validation/agentteams-2026-09-09/reports/runtime-reply-thread-live.md)确认普通reply／thread均获处理，但出站未保留入站父消息／thread关系，两者共用房间session。页面和后端须保留这一适配缺口，不能从Matrix事件持久化推断线程回复或Issue归属已正确实现；本轮业务REST／SSE接口无变化。

- replan 遇到 in-progress／submitted 会拒绝，pause 不停止在途，也不保证权限撤销的强制生效。
- taskflow 委派／提交主要依赖本地 stdio MCP，不能虚构 REST 任务启动端点；原生 DAG／Loop 或 Dashboard HITL 不证明 RepoMesh 完整编排、审批已实现。
- history 读取不证明实时订阅；QwenPaw 的 room／session 静态观察不证明其他 runtime 或多事项完整上下文恢复。内存 ready 观察和房间缓冲不能作为可靠持久事实，不能依赖未接通的 `remoteSkills` 字段。
- 已有文档链接／JSON、本地原型、上游组件与有限真实服务实验各自只证明相应范围。RepoMesh 的 API／MCP、权限、事务、幂等、队列、SSE、容量、执行隔离和全链路恢复仍待实现与验收，不能从上游局部成功推导整体通过。原检查记录及执行方保留在日志、角色 Prompt 和验证材料中。
