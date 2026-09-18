# RepoMesh 现行文档索引

**2026-09-12 审查修订：** [多 agent 审查](../../../../reviews/2026-09-12-design-readiness/README.md)发现 Key 保存恢复、Issue 配置归属、测试观察及登录返回的缺口，修订和分阶段开工条件见[修订入口](../../../../current/design-readiness-revisions.md)。新终结协议、绑定时点及导航扩展仍为候选；原型一致性和 Skill 适用说明按既有语义修正。业务仍是骨架，真实执行的[五项协议门槛](../../../../current/execution-integration-gates.md)未全部完成。下方 CP16 和“当前／无剩余分歧”描述保留为此前记录，不代表本轮新修订已采用或系统已通过验收。

**当前：首批六项完整待审包已成稿。** 用户“你直接给我这六项的全部”授权一次集中交付，见[总包](../../../../current/first-batch-complete-review.md)及[六项原型入口](../../../../prototypes/index.html#review)。已补认证/发现、模型保存/测试/专用应用、来源S01—S08及页面恢复；全部新增决定待用户，不逐页停问。F01/F02/F03/F04已采用的具体UI保留，F09及Key三项未采用；旧已收口r3不重开。只页面和API设计，开发另做。认证r2为2／3，模型API/配置来源/F02/公共恢复各r1为1／3；双方本次文档及定点核对均通过，无剩余文档分歧，无技术待回复。所有新增D/T/A/P/S/R仍待用户统一采用。最新收发、轮次及TODO见页面日志CP16；下方按历史时间记录的“未成稿/技术0轮”等状态以本条和CP16为准。

用户“是的，继续下一个”已采用F01登录恢复所问三项UI，技术r2、2／3；随后“继续”推进当前[F05 API Key保存与原结果查询](../../../../current/model-key-save-design.md)，r1文档核对通过、1／3，三项新UI仍待用户。F09和认证三方向未因此采用，完整Key／测试／应用协议仍未完成。只页面／API设计，开发另做，准确收发与TODO见页面CP15。

用户“继续下一项”后，当前展示[F01登录与重连恢复r2](../../../../current/login-recovery-page-design.md)。沿原入口主题第二轮，三项新UI待审；F09三项及认证A／B／C方向未因“继续”自动采用。当前只页面／API设计，开发另做；最新原文／轮次／待回复与TODO见页面日志CP14。

**当前范围整理：[首批开发待做表建议](first-development-todo.md)**。首批范围已获用户确认；当前仅继续页面／API设计，开发之后单独进行。F04三项UI已采用，当前展示F09最小概览待裁决。

**当前继任工作（2026-09-11）：** [F02项目配置编辑讨论稿](../../../../prototypes/repomesh-project-settings-prototype.html)已进入展示，三段顺序、显式配置开关、摘要后确认已采用，用户要求主界面可找到；F02继任后端技术核对仍0／3。新页面ID、完整原文恢复和文件归属见[当前通信约定](design-communication.md)与[继任日志](design-communication-page-2026-09-11.md)。旧双方均已退出，不按下方历史身份继续投递。

**页面设计换会话交接（2026-09-11）：** 请从[新版页面／接口交接](HANDOFF-PAGE-API-DESIGN.md)开始，含已采用原型、F01—F15未完成设计表单、接口缺口及继任者通信/文件归属；[旧版已归档](../../../2026-09-11-page-api-handoff/HANDOFF-PAGE-API-DESIGN.md)。

**当前设置评审（RM-UI-MODEL-SETTINGS r3）：** [供应商与模型原型](../../../../prototypes/repomesh-model-provider-prototype.html)按用户参考图改为左供应商、右配置和模型列表；填写项核对AgentTeams锁定源码517caff，r3技术已ACK。用户回复“ok可以的”，采用当前供应商分栏及模型参数填写方式；多供应商映射、密钥及专用应用协议未完成。来源及边界见[模型设置专题](../../../../current/model-connection-settings-design.md)。

**当前入口评审（RM-UI-PROJECT-ENTRY r1）：** [登录与项目入口原型](../../../../prototypes/repomesh-project-entry-prototype.html)展示模拟登录、选仓、名称/用途、默认配置摘要及保存后继续配置；后端技术及落盘已核对；用户回复“正确”，采用已展示的“先选仓库、再填资料”两步流程及授权提示。模型设置另行展示讨论。规则见[项目配置专题](../../../../current/project-configuration-design.md)。只原型/文档，不接真实OAuth或密钥；完整认证/发现/Key接口仍需补齐。


**当前实现设计（RM-UI-MESSAGE-TARGET r3）：** 用户要求与后端讨论具体实现，双方已确认[消息与澄清五端点契约](../../../../current/conversation-message-clarification-api-contract.md)及[后端实体/事务/受控动作](../../../../current/backend-message-clarification-design.md)。明确请求直接解释，歧义答复绑定原问题，单问题唯一答案，原键幂等恢复；第3／3轮技术无分歧。双方最终落盘已互核，无遗漏实质分歧；仅文档，真实实现/验证未开始，完整B05/多源/更正/适配与执行尚未完成。消息控件仍待用户评审，原型保持。


**当前讨论（RM-UI-MESSAGE-TARGET r1）：** [消息目标与澄清原型](../../../../prototypes/repomesh-message-target-prototype.html)在主会话内展示Manager的目标问题、候选填入可编辑答复、带引用发送及可见目标。技术r1已ACK，具体控件待用户评审；仅预置脚本，不是语义识别或执行验证。详细规则见[消息目标专题](../../../../current/conversation-message-target-design.md)。

**已采用（RM-UI-ROOM-NAV r2）：** 按用户最新要求改为[常驻悬浮入口＋同页右栏](../../../../prototypes/repomesh-conversation-dock-prototype.html)：点击条目展开DAG/Leader/Issue内容，保留主会话；随窗口宽度收成图标，窄窗自动收起内容栏并可重新展开。替代r1固定索引栏与快捷离页方式；技术r2及落盘已核对，用户回复“正确的，进行下一项”采用已展示交互。当前只原型/文档，真实API/核权/恢复未实现。详细边界见[页面专题](../../../../current/conversation-issue-separation-design.md)。

**当前页面进展：** 用户“是的”采用[只读DAG](../../../../prototypes/repomesh-issue-dag-prototype.html)布局及图下节点详情（RM-UI-PLAN r2）。依新要求展示[会话右栏导航](../prototypes/repomesh-conversation-navigation-prototype.html)：按关联Issue索引DAG与只读Leader房间，可收起；仅限会话页。RM-UI-ROOM-NAV r1技术已ACK，具体宽度和排布待用户评审。规则见[页面专题“当前会话右侧导航”](../../../../current/conversation-issue-separation-design.md)。完整图/关联查询/消息/房间协议、真实实现与验证仍未完成。

**当前页面决定（RM-UI-01 r3）：** 用户指定创建 Issue 使用居中弹窗，保留原列表与侧栏。当前[弹窗讨论稿](../../../../prototypes/repomesh-issue-modal-prototype.html)已展示，用户回复“正确”采用当前大小和字段排布；后续新增交互继续展示讨论，创建接口与原键恢复不变。详见[页面专题](../../../../current/conversation-issue-separation-design.md)。此条覆盖下文原创建表单独立页的呈现描述。

**2026-09-10 新协作入口：** 当前为“页面接口设计师1”与“后端设计师1”，精确任务 ID、唯一文件负责人和压缩恢复步骤见[通信约定](design-communication.md)顶部；本轮原文分别保存在[页面日志](design-communication-page-2026-09-10.md)和[后端日志](design-communication-backend-2026-09-10.md)。旧角色 Prompt／日志仅作历史，不恢复旧任务。首批路由／创建恢复和关系补齐见[页面专题](../../../../current/conversation-issue-separation-design.md)、[项目配置](../../../../current/project-configuration-design.md)及[页面交接进展](HANDOFF-PAGE-API-DESIGN.md)。

更新：2026-09-10，新增基础工程入口。先读 [HANDOFF](HANDOFF.md) 了解当前基线、未完成工作和证据边界，再按下表进入专题。本文负责导航，交接页负责当前状态，ADR 记录取舍，专题维护详细规则。完整角色接手按[页面 Prompt](NEXT-SESSION-PROMPT.md)或[后端 Prompt](NEXT-BACKEND-SESSION-PROMPT.md)的阅读清单核对；其中旧协作指令不恢复为本轮任务授权。

## 如何判断哪条规则有效

- 先核对用户明确要求及其适用范围，再读对应 ADR、[持续设计授权及采用清单](../../../../current/design-delegation.md)和专题。按明确的后续确认／替代关系判断，不能只比较文件日期或 ADR 编号。
- `accepted` 表示列明的设计已采用；`proposed` 专项稿内可以包含已确认部分，须看分节说明；“暂缓”表示实施范围尚未启动。以上均不代表实现或验收完成。
- 后续改变只替代指明的规则。原确认时间、用户原话及当时未决事项保留历史含义，不倒填为当时已批准；[ADR 索引](../../../../adr/README.md)列出具体关系。
- 日志、旧原型和归档用于溯源，其中的“最新”“待批准”或操作指令属于记录时的任务。评审文档不因此自动恢复旧协作或开发任务。

## 入口、会话与创建契约

| 文档 | 用途与适用范围 |
| --- | --- |
| [领域语言](../../../../../CONTEXT.md) | Project、Conversation、Issue、仓库事项、角色与交付对象的定义。 |
| [会话、独立 Issue 与房间导航](../../../../current/conversation-issue-separation-design.md) | 当前页面关系及原型入口；领域决定见 ADR-0019。 |
| [页面设计原型目录](../prototypes/README.md) | 用户要求遵循既有设计；以“会话与独立 Issue”为当前视觉／导航基线，另两版保留演进关系；模拟行为以现行契约为准。 |
| [多 Issue 会话中的消息目标](../../../../current/conversation-message-target-design.md) | 自然语言明确目标、歧义先澄清；算法与消息协议继续设计。 |
| [Manager MCP 与 Issue 栏双入口](../../../../current/manager-create-issue-tool-design.md) | 统一创建、来源与恢复机制；完整 MCP Schema／可信来源操作协议待编制。 |
| [创建接口契约 v1](../../../../current/issue-page-create-api-contract.md) | 已采用的创建条件、关联会话、REST、幂等、详情／房间与 Issue SSE；尚未实现。 |
| [首批项目／列表浏览器契约 v1](../../../../current/first-batch-browser-api-contract.md) | RM-API-01 r3：12个项目、列表、配置引用端点，含项目创建／更新原键恢复；双方已互核，尚未实现。 |
| [首批后端持久化协议](../../../../current/backend-first-batch-persistence.md) | RM-B01-04 r2及RM-API-01 r3：最小关系、三类幂等操作、固定配置、队列、事件与故障用例；数据库／运行未验证。 |
| [建项前仓库分析与 Python 插件](../../../../current/issue-creation-repository-analysis.md) | 用户后续采用：仓库分析按钮、应用建议、独立作业与可选来源；历史分析／向量库后续考虑，尚未实现。 |
| [会话与 Issue 后端专项](../../../../current/draft-conversation-backend-design.md) | 已采用关系和创建机制的后端衔接；其余协议及恢复仍在设计，保留原文件名。 |
| [项目配置与授权变化](../../../../current/project-configuration-design.md) | J1—J3：配置、仓库范围和授权变化；项目先保存，运行触发见 ADR-0018 后续补充。 |

## 团队、执行、验证与交付

| 文档 | 用途与适用范围 |
| --- | --- |
| [团队执行策略](../../../../current/team-execution-policy.md) | A1—A5：成员、并发、调度与故障。 |
| [验证节点](../../../../current/verification-node-design.md) | 验收解释、检查结论、必检证据与独立验证。 |
| [联调环境](../../../../current/integration-environment-design.md) | C1—C6：固定组合、独立环境、配置与数据。 |
| [Draft PR 与人工审查](../../../../current/draft-pr-review-design.md) | E1—E5：draft 时机、检查、反馈及人工合并边界。 |
| [ChangeSet 设计](../../../../current/changeset-design.md) | CS1—CS14：归属、交付、部分成功与恢复。 |
| [ChangeSet 逻辑结构](../../../../current/changeset-structure.md) | 记录关系和字段示意；ChangeSet 的完整存储与查询 Schema 尚未冻结。 |
| [ADR-0003：计划许可与生效](../../../../adr/0003-plan-change-authorization-and-activation.md) | 许可、生效、重规划与后续审批规则；当前产品使用 YOLO。 |
| [Graph／Loop：复用上游 DAG](../../../../current/graph-loop-design.md) | 现行专题：最新确定源码基线、上游仓内 DAG 与 RepoMesh 跨仓／Loop 分工、结果采纳、换图、恢复及验证。 |
| [ADR-0007：Graph／Loop](../../../../adr/0007-graph-loop-plugin.md) | 流程与循环规则；ADR-0014／0015 的 09-09 补充明确复用原生 DAG，不重写完整仓内引擎。 |

## 架构与实施边界

| 文档 | 用途与适用范围 |
| --- | --- |
| [技术选型](../../../../current/technology-selection.md) | ADR-0010 已确认的整组技术方案。 |
| [基础工程开发说明](../../../../current/development-scaffold.md) · [工程验收记录](../../../../current/scaffold-verification.md) | 实际目录、三入口、配置与配套发布、检查及独立复核；骨架不代表业务完成。 |
| [阅读前文件清单](scaffold-source-inventory.md) · [ADR 审计](scaffold-adr-review.md) · [验证证据追溯](../../../../current/scaffold-agentteams-evidence.md) · [页面/接口/后端审计](scaffold-page-backend-review.md) | 本轮团队逐文件覆盖、证据层次和工程影响。 |
| [首期架构 v1](../../../../current/architecture-design-v1.md) | 各节区分已确认方向、建议和待验证能力，不将整稿视为已定稿。 |
| [ADR 全集与决策演进](../../../../adr/README.md) | ADR-0001—0020：组织、授权、计划、受控接入、进程、轮次、事务、资源、入口和 Python 分析插件。 |
| [Skill 工程占位](../../../../current/skill-engineering-design.md) | H1—H15 方向已同意，但整个模块按 ADR-0009 暂缓开发。 |

## 交接、评审与证据

| 文档 | 用途 |
| --- | --- |
| [HANDOFF](HANDOFF.md) | 当前状态、约束、后续工作与证据限制的共同入口。 |
| [页面／接口 handoff](HANDOFF-PAGE-API-DESIGN.md) · [后端 handoff](HANDOFF-BACKEND-DESIGN.md) | 正式开发前的缺口、优先级、分工、设计产物及开发就绪条件；当前接手入口。 |
| [旧页面 Prompt](NEXT-SESSION-PROMPT.md) · [旧后端 Prompt](NEXT-BACKEND-SESSION-PROMPT.md) | 保留原完整阅读清单、原型和历史协作来源；旧执行顺序及通信身份不恢复为当前指令。 |
| [持续设计授权](../../../../current/design-delegation.md) | 授权原话、P1—P4 及双入口／契约采用范围；不等于运行能力证明。 |
| [通信约定](design-communication.md) | 原设计协作任务身份、主题轮次、原文保存与恢复要求。 |
| [页面通信日志](design-communication-page-log.md) · [后端通信日志](design-communication-backend-log.md) | 原始通信与检查点，保留历史，不以摘要覆盖。 |
| [本次文档评审](document-review-2026-09-09.md) | 时间冲突、维护措施、未决问题与验证结果；不是新增产品 ADR。 |
| [旧项目分析插件调查](../../../../research/legacy-analysis-plugin-feasibility-2026-09-10.md) | 多 Agent 静态源码调查；后续仅先采用仓库分析按钮与 Python 插件，历史分析仍待后续设计。 |
| [AgentTeams 接入验证清单](../../../../plan/agentteams-validation-plan.md) | 09-09 直接核查锁定源码：控制旁路、pause／replan、并发写、任务身份、会话、就绪及隔离；已开始实际验证，见[执行状态](../../../../../validation/agentteams-2026-09-09/reports/validation-status.md)。 |
| [AgentTeams 调研](../../../../agentteams-survey-2026-09-07/agentteams-survey.md) · [API／CLI 调研](../../../../agentteams-survey-2026-09-07/agentteams-api-cli-survey-2026-09-07.md) | 锁定提交的静态依据，不能当作 RepoMesh 运行验收。 |
| Cursor Dashboard 历史报告（原引用文件当前缺失） | 原位置 `Cursor_Dashboard_Report_2026-09-07/report.md`；仅界面参考，非已采用的Manager布局。2026-09-10链接检查未找到原文件，不伪造替代来源。 |

## 保留原路径的历史入口

下列文件保留原型路径和确认过程；当前聊天关系及创建行为必须回到上面的现行专题。

| 历史文档 | 当前解读 |
| --- | --- |
| [第一版页面原型](page-interface-prototype.md) | A／B／C 布局已被否定，仅供溯源。 |
| [Draft Issue 与房间入口](draft-issue-and-room-entry-design.md) | 深色会话式视觉要求保留；聊天整体转正和旧事项绑定已替代。 |
| [项目先创建入口](project-first-entry-design.md) | 项目先保存和 DESIGN-001 确认过程保留；旧 Draft Issue 原型不是当前生命周期。 |
| [ChangeSet 旧讨论入口](changeset-design-discussion.md) | 兼容旧链接，转至已确认设计与结构说明。 |

[总导航](../README.md) · [历史归档](../../../README.md) · [本次整理前交接快照](../../../2026-09-09-before-document-review/README.md)

## 后续维护方式

修改一项决定时，同时检查其 ADR、直接关联专题、架构场景及验收描述，再更新 HANDOFF 的当前状态；导航只调整入口与适用范围。保留原 `date` 和确认来源，在变更文件中记录修订日期。已被替代的正文或图必须在所在章节标明历史，避免只有页首提醒而后文仍像现行规则。通信原文不重写，完整协议没有编制或运行尚未验证时须分别说明。
