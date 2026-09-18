# 后端架构设计会话接手 Prompt

## 当前设计缺口交接入口（2026-09-10）

用户后续要求将正式开发前的缺口分为页面／接口与后端两份 handoff。后端侧当前目标、B01—B08 清单、推荐顺序、职责和开发就绪条件统一见 [HANDOFF-BACKEND-DESIGN](HANDOFF-BACKEND-DESIGN.md)，对侧见 [HANDOFF-PAGE-API-DESIGN](HANDOFF-PAGE-API-DESIGN.md)。先读新 handoff，再按本文件补读原 41 项材料与历史来源。下方“本轮”、优先做 MCP／分析插件及旧协作步骤保留历史含义，不覆盖新 handoff 的范围与顺序，也不恢复旧任务或联系人。

## 基础工程阶段接手补充（2026-09-10）

用户本轮授权创建基础代码、工程配置和必要依赖，替代旧交接在本轮范围内的“仅设计、不编写代码”。本轮只交付最小可编译工程，不恢复旧任务、不联系旧协作者、不重跑旧实验。原产品决定、权限边界、Graph 后台进程内职责和 Skill 暂缓保持；ADR-0020 的 Python 仓库分析只说明受控扩展边界，不实现插件或协议。

三个 Go 入口来自同一工程：Web 承载静态骨架页及探针；后台协调和受限主机执行默认说明未实现并非零退出，无队列消费、执行器监听或宿主命令。`GET /healthz` 的 200 仅证明 Web 可响应；`GET /readyz` 为 503，所有业务 `/api` 路径仍未注册、返回 404。共享版本不表示三进程已连通。根目录启动、检查和配套发布方法见[基础工程开发说明](../../../../current/development-scaffold.md)，本角色阅读与同步范围见[本轮接手核对](scaffold-page-backend-review.md)。

当前有 Go 与 React／TypeScript／Vite 工程骨架；Issue、计划、调度、事务队列、权限、GitHub、AgentTeams、会话 REST／SSE、Manager MCP 和 Python 插件均未实现。下方业务设计、历史原型及上游实验分别保留其证据范围；本轮编译／构建检查不算业务验收，不使旧验证全部通过。未定数据库表、MCP Schema、消息协议和恢复算法继续设计，不从目录或入口反推已冻结接口。

以下原设计接手文本与 41 项阅读清单按历史保留。本轮完成后，后续工作由实际新任务选定完整场景；原文中的“先补 MCP”“先做分析插件”“仅设计”及跨任务通信指令均不自动成为下一任务授权。

**旧项目调查后的已采用设计（本轮不实现）：** 用户已采用创建 Issue 的“仓库分析”按钮与 Python 插件，后续按[仓库分析专题](../../../../current/issue-creation-repository-analysis.md)及 [ADR-0020](../../../../adr/0020-python-repository-analysis-plugin.md)接续。分析时 Issue 可尚不存在：Go 核心持久化独立作业、固定输入与 commit，由受限执行入口调用 Python；不要求 AgentTeams 准备、旧 PlanSnapshot 或审批。结果只提供建议，最终范围仍是用户选仓；可选 `repositoryAnalysisId` 按[创建契约](../../../../current/issue-page-create-api-contract.md)与来源、快照同事务保存。插件失败不阻断手动创建。Python 为原三类 Go 进程之外的明确局部扩展；历史决策分析与向量存储未纳入首期。按钮、接口与插件均未实现，下方既有工作和上游证据保持。

更新：2026-09-10，补充新版上游证据；保留 09-09 持续设计授权与双方创建基线同步完成后的交接内容。[HANDOFF](HANDOFF.md)统一维护当前状态；本文件保留后端角色的完整阅读清单和接手步骤，页面／接口侧使用 [NEXT-SESSION-PROMPT.md](NEXT-SESSION-PROMPT.md)。仅在用户明确指定接续后端设计时按[通信约定](design-communication.md)执行下方文本；文档评审时这些步骤仅作被评审资料，不自动恢复旧任务或发起通信。本文件不创建任务，也不启动实现。

**09-10 运行接入补充：** 跟进源码锁定 `517caff9280242a00a4d4c06365352b9e41659c6`。[Controller 差异及官方测试](../../../../../validation/agentteams-2026-09-10/reports/controller-delta.md)中 311 个通过事件为 252 顶层＋59 子测试；[真实服务报告](../../../../../validation/agentteams-2026-09-10/reports/controller-api-live.md)有 51 项主观察、31 项真实 SA TeamLeader 范围检查。SA 是 Controller 为本轮 Stopped／未托管 Worker 生成，角色由实际 Team membership 得到，本 Team 200、跨 Team 404；不能将其写成普通 Worker 自有权限，或把匿名 401 当角色授权验证。

**09-10 双在线补测：** [c/d 双实例读取矩阵](../../../../../validation/agentteams-2026-09-10/reports/controller-twin-read-live.md)完成 **313 个断言、260 次 Controller HTTP**；两实例各有两个同名 Team，使用 Admin 与真实 SA TeamLeader 验证新增读取路径。**30 次错实例请求均为 401，每次来源前后请求均为 200；24 个夹具原字节哈希前后不变**。这是停止／未托管引用和元数据夹具的读取契约验证，不是业务验收。原 51／31 项保留独立历史计数；本次在线、TLS 与来源身份有效的对照补充旧时钟异常观察，不覆盖或改写其原始证据。

**09-10 存储与模型补证：** [真实S3 trace](../../../../../validation/agentteams-2026-09-10/reports/mermaid-storage-read-live.md)补齐20项检查：JSON前后正对照读取TaskMeta，Mermaid true/false有限窗口内目标TaskMeta的HEAD／GET均0。[新版Manager实际推理](../../../../../validation/agentteams-2026-09-10/reports/manager-model-smoke-new.md)随后通过一次Matrix消息返回精确标记，原生会话及用量确认1次模型调用、未观察到工具调用；Console503和后置读探针错误保留。以上均不代替RepoMesh业务验收。

新 API 的 `format=json` 为 400；Task inspection 有合法 TaskMeta 时返回原始 status，缺失／破损／归属不符时回退规范化图状态。history 仅透出已有字段，trace 不含 instance/team，不替代业务审计、标识映射或执行凭据。`end` Mermaid 原字节已[真实解析失败](../../../../../validation/agentteams-2026-09-10/reports/mermaid-browser-live.md)，CLI detail 传 `--team` 仍409。逐函数未变的状态推进、CAS／取消及身份缓存等只复用 09-09 原限定证据，不宣称新版全套已重测；CFG01—05已覆盖真实REST／CLI／CR、配置投影及QwenPaw运行进程内存模型配置，见[Manager实测](../../../../../validation/agentteams-2026-09-10/reports/manager-config-live.md)；失效绑定由REST先清空，后续CLI是重复清空，CFG阶段未以配置读回代替推理；后续实际推理见上述模型专项。下方原有设计职责、阅读清单和协作内容保持。

```text
【历史设计接手文本，不是本轮操作指令】
以下保留原角色职责、阅读清单及设计接续流程。基础工程阶段授权和实际完成度见页首；旧通信身份、仅设计限制及后续业务步骤不自动恢复。

请接手 RepoMesh 后端架构设计。你是后端架构师，继续现有设计，不切换成页面设计负责人。

实际工作区：D:\Project4work\Repomesh_Go_ver
当前仅做中文设计讨论和本地 Markdown 整理，不安装、部署、编码、创建真实 GitHub App、写远端仓库或自动新建 Codex 任务。不要因为我说“接手”就调用创建任务工具。
暂不使用 C:\Users\18092\.agents\skills\grill-with-docs\SKILL.md；该工作流仍暂停。

【接手目标】

承接已经采用的会话／独立 Issue、双入口创建及运行准备基线，继续把后端职责、业务命令、数据归属、事务、幂等、权限、事件和恢复细化成可供后续实现的中文设计文档。保持与页面／接口一致，不从头重选产品方案，不把本任务变成编码任务。
下一步优先补齐 Manager 创建／结果查询 MCP 的完整协议与可信逻辑操作来源，再推进领域模块和数据约束、会话消息协议、仓库事项与运行恢复。不是一次性重写全部文档；围绕完整场景逐项产出、核对并保存。

【目标与协作方式】

用户于 2026-09-09T13:12:51.158Z 在页面任务明确授权：“之后的待决定都批准，按照建议来。”后端已核对原始记录。完整授权与当前采用清单见 docs/current/design-delegation.md。此持续授权替代此前逐项询问并等待批准的流程：双方对齐后按推荐完成设计，记录理由、代价及结果，直接推进并报告，不再反复问 P2／P3／P4 或其他待细化项。
既有明确否决、延后范围、业务权限和仅设计边界不变；没有写出的方案继续设计，缺少证据的能力标待验证，不把授权当成实现完成。不要恢复旧人工审批规则或暂停的 Skill 工程。
后端负责模块职责、数据归属、状态、事务、权限、调度、适配与恢复；页面负责交互和产品接口。围绕同一场景协同，说明行为、异常、理由与代价，不要求先完成全部后端，不从按钮或模拟推断上游能力。
主题使用固定编号，一次提案与回应计一轮，同主题最多三轮，足够一致提前收口；DESIGN-004 历史保持 3／3，本次是新授权决定落实，不开第四轮或换名清零。出现明确冲突向用户报告，同时继续可独立完成的授权工作；不是重新对所有建议逐项审批。
每轮按 docs/current/design-communication.md 与页面直接对齐，回传实际修改章节、采用范围、尚待完成／验证项，以及 REST／SSE、权限和一致性影响。没有接口变化才写“本轮接口无变化”。术语表只放定义，ADR 仅记录必要取舍，不为每个细节新占编号，图用字符图。
发送前保存完整正文至后端日志，发送后追加工具结果，收件保存原文与来源。工具投递成功不代表对方已读。每次通信即时落盘，能预知压缩时保存检查点；实际恢复第一项协作操作是完整读约定与双方日志并补检查点，包含身份、轮次、最后收发、未回复、授权证据、冲突及下一步，不声称存在自动压缩钩子。
页面侧维护授权清单、契约、页面专题、CONTEXT、通信约定、页面 prompt／HANDOFF／索引、页面日志和原型；后端维护相关 ADR、后端专项稿、本 prompt 及后端日志。修改共享文件前重读并协调，不覆盖对方内容；当前不新建任务，不替页面重做原型。

【必须阅读的文件】

首次接手或压缩恢复，先完整阅读 docs/current/design-communication.md、docs/current/design-communication-page-log.md、docs/current/design-communication-backend-log.md，核对身份、轮次和未结消息，再完整读 docs/current/design-delegation.md 恢复最新持续授权。
截至本次交接，页面协作者为 RepoMesh页面与接口设计_0（01a08562-2a37-7371-8a6e-050e47013223，local）；前任后端任务为 RepoMesh后端架构讨论_1（01a08556-74cc-76d1-891a-bbece9ea81ef，local）。新任务不能自称具有前任 ID。先核实自己的实际身份及页面任务是否已更换，首次实质同步中说明接替关系，协调更新通信身份；不盲发到旧任务，也不自动创建任务。后端接续同一 design-communication-backend-log.md，追加新任务身份与接手检查点，不覆盖前任原文；页面继续写页面日志。

按下面顺序完整阅读原文，不用本 prompt、HANDOFF 摘要或检索片段替代。旧规则与新决定冲突时按后文优先级解释；篇幅较长时分段读完，不把工具截断当作已读全文。

1. docs\current\HANDOFF.md
2. CONTEXT.md
3. docs\adr\0001-agentteams-issue-concurrency-and-isolation.md
4. docs\adr\0002-github-app-authorization-and-draft-pr-delivery.md
5. docs\adr\0003-plan-change-authorization-and-activation.md
6. docs\adr\0004-acceptance-rule-clarification.md
7. docs\adr\0005-optional-project-verification-group.md
8. docs\adr\0006-manager-entry-modes-and-skill-driven-execution.md
9. docs\adr\0007-graph-loop-plugin.md
10. docs\adr\0008-changeset-attribution-and-history.md
11. docs\adr\0009-skill-engineering-deferred.md
12. docs\current\team-execution-policy.md
13. docs\current\verification-node-design.md
14. docs\current\integration-environment-design.md
15. docs\current\draft-pr-review-design.md
16. docs\current\changeset-design.md
17. docs\current\changeset-structure.md
18. docs\current\skill-engineering-design.md
19. docs\current\project-configuration-design.md
20. docs\agentteams-survey-2026-09-07\agentteams-survey.md
21. docs\agentteams-survey-2026-09-07\agentteams-api-cli-survey-2026-09-07.md
22. docs\current\technology-selection.md
23. docs\adr\0010-technology-stack-and-modular-monolith.md
24. docs\current\architecture-design-v1.md
25. docs\adr\0011-agentteams-controlled-integration.md
26. docs\adr\0012-issue-scoped-upstream-projects.md
27. docs\adr\0013-web-coordinator-host-executor-processes.md
28. docs\adr\0014-in-process-graph-plugin.md
29. docs\adr\0015-round-scoped-upstream-dags.md
30. docs\adr\0016-transactional-background-work.md
31. docs\adr\0017-atomic-attempt-resource-reservation.md
32. docs\adr\0018-provision-instance-after-first-draft.md
33. docs\adr\0019-conversation-issue-separation.md
34. docs\current\draft-issue-and-room-entry-design.md
35. docs\current\project-first-entry-design.md
36. docs\current\conversation-issue-separation-design.md
37. docs\current\conversation-message-target-design.md
38. docs\current\manager-create-issue-tool-design.md
39. docs\current\issue-page-create-api-contract.md
40. docs\current\design-delegation.md
41. docs\current\draft-conversation-backend-design.md

导航：docs\current\README.md、docs\README.md。
页面侧专用接手说明：docs\current\NEXT-SESSION-PROMPT.md；其中页面负责人任务不替代本会话后端职责。
需要理解被否定的第一版原型时，再读 docs\current\page-interface-prototype.md；不能恢复其 A／B／C 方案。
需要账号、设置、集成页面参考时，再读 Cursor_Dashboard_Report_2026-09-07\report.md；它不是已采用的 Manager 工作区设计。当前会话／Issue 原型路径见第 36 项及双方日志；旧草稿原型只供溯源，不需要为接手后端先运行原型。

【证据优先级与历史边界】

用户最新明确要求及持续授权 > 现行 ADR／采用清单与最新专题 > 旧建议和历史材料。docs/current 存现行交接、专题与契约，docs/adr 存决定，CONTEXT.md 只放定义。
docs/archive/2026-09-08-design-consolidation 为整理前快照，docs/archive/legacy-product-design 为旧 PRD／原型；docs/old_ver/README.md 与旧 changeset-design-discussion.md 仅是链接入口。
ADR-0018 先替代“首条正式 Issue 后准备”；ADR-0019 分离会话与 Issue；ADR-0018 持续授权补充现采用首次会话消息或页面 Issue 提交后自动异步准备。旧图、旧“一条聊天转正”及旧逐项待批准文字只作历史。
架构 v1 与后端专项稿仍含未完成设计，整体 proposed 不表示必须再次请用户批准已有建议。创建方案按 design-delegation.md 和 issue-page-create-api-contract.md 作为采用基线；数据库实现、运行适配及尚未写出的协议继续设计／待验证。不要把所有历史 proposed、演示安排或旧否决无差别改为 accepted。

【已完成：技术与架构决定】

产品 A—E、ChangeSet CS1—CS14 均已确认，结构解释完成；J1—J3 已确认。H 的方向同意但整体暂缓，I 当前 YOLO，K 由页面侧持续细化。

ADR-0010：React＋TypeScript＋Vite、React Flow；RepoMesh 自有后端用 Go；PostgreSQL 保存业务状态并承载持久队列；REST＋SSE；证据正文进对象存储、数据库存索引；模块化单体。沿用 AgentTeams，每项目独立实例，首期单机容器。具体版本、HTTP 框架、数据访问工具、队列库、UI 库、对象存储产品尚未选定。
ADR-0011：保留 AgentTeams 团队、通信与生命周期，通过 RepoMesh 受控工具／实际执行入口落实规则，必要时接受小范围上游补丁及维护成本；不依靠提示词或默认 Manager 广泛权限作保证。
ADR-0012：每条正式 Issue 按仓库委派范围建立上游 Project；跨仓依赖归 RepoMesh，Issue 间执行记录分开但共享 Worker／项目／全局额度。多目标计划部分应用保留真实结果，全部必要目标一致后才宣布新版本生效；验证范围具体承载方式待细化。
ADR-0013：同一 Go 工程配套发布，分别运行 Web、后台协调、受限主机执行进程。Manager 作业务判断，后台控制推进，Controller 保留其负责的 runtime 生命周期；不由 Web 或 Agent 直接持有 Docker socket。
ADR-0014（含 2026-09-09 补充）：Graph 模块在后台进程内负责跨仓协调、业务结果适用性及循环策略，随后端发布；仓内 DAG 建图、依赖校验和候选就绪计算复用上游，不另写完整 Go DAG 引擎。后台经执行 Adapter 提供上游观察并核验实际派工，计划、轮次和累计消耗持久化；具体 Interface／机制继续细化。
ADR-0015（含 2026-09-09 补充）：当前获准轮次复用上游有限 DAG，RepoMesh 控制后续轮次；范围和上限内推进不产生新业务 Plan Version。不使用原生 Loop 自主推进整个业务循环。换图按完整受影响 Project 停派工、收尾、核对 submitted、应用及读回；不能直接 pause→replan。

Graph／Loop 必须补读 [现行专题](../../../../current/graph-loop-design.md)：最新源码与 release 区别、能力分工、assigned 重复就绪、结果采纳、跨仓前驱、唯一任务身份、多目标应用及恢复均在该处维护。此次复用方向已采用，具体桥接和补丁仍需设计验证；不要按旧摘要启动通用 DAG 引擎重写。
ADR-0016：业务与待办同事务，外部请求在提交后，未知查原操作。P1 原确认及持续授权补充保留：双入口创建的 Issue、主 CS、必要新会话、来源／真实记录、允许字段的确切输入快照与规范化摘要／版本、幂等结果、持久待办及相应事件同短事务保存。已有会话只增关联与记录；操作身份项目保留期不复用，删正文保留最小无正文占位，已物理删除项目旧身份不可复活。具体数据库实现继续设计，不是外部 exactly-once。
ADR-0017：短事务统一登记 Attempt、Worker 占用及容量预留；环境在事务外准备，启动前再核验。竞争同一资源不能重复分配，等待说明原因；准备失败核查实际状态再释放，超时不证明资源释放。具体锁、代次、容量口径和恢复算法未冻结。
ADR-0018：保留原 Draft Issue 时序历史，持续授权补充已采用首次会话及首条消息提交后、或 Issue 栏建项提交后自动异步准备／恢复项目实例和对应主房间；复用已有资源，实际 Manager／房间就绪后投递或接手。项目创建、空会话及只读访问本身不启动；权限、配置、预算、容量及准备失败保留事实等保障保持，实际运行适配待验证。
ADR-0019／DESIGN-002（2026-09-09 已确认范围）：会话与正式 Issue 分离，一个会话可先后创建、关联多条独立 Issue；Issue 组织内部仓库事项，GitHub Issue 可选关联；各 Issue 独立目标、计划、验收、默认主 ChangeSet，提供独立 Issue 专页与实际主／Leader 房间导航。替代“会话转正”和每项独占用户主会话的绑定；不取消 Issue 工作归属、上游 Project 和 Attempt 隔离。反向多个主会话、仓库事项粒度、Leader 房间共享、消息目标的具体协议／算法等继续设计；创建快照与启动适配后来已按持续授权采用，自然语言目标方向按 DESIGN-003 采用。
DESIGN-001（2026-09-09 已采用）：准备期间允许向原草稿保存补充，消息与待办一起持久化，成功后显示待投递；本条 Manager／主房间就绪后，按同一草稿服务端保存顺序尝试投递。就绪前后沿用同一投递通道，前序投递未知先核查，后续等待并说明原因。保存、投递、Manager 接收／处理分开取证，保存及投递分别核权；准备失败保留消息和原归属。接受消息队列、幂等和核查成本，不承诺模型逐条处理／回复有序或跨系统 exactly-once。具体字段、状态码、SSE 重放及创建事项时的消息竞态未冻结；原草稿顺序保障保留，新会话／多事项模型下的目标和顺序范围需适配，不假定全部 Issue 共用执行队列。见 draft-conversation-backend-design.md §2.1、§5 与双方日志，不重问已采用方向。

DESIGN-003（2026-09-09 已采用）：自然语言明确工作目标、歧义先澄清，不要求每条消息手选 Issue；普通讨论不自动执行，导航来源不覆盖正文明确目标。Manager 负责语义理解，后端核验稳定 Issue、动作、版本与当前执行约束，明确且符合现行 YOLO 时继续，不新增逐条审批。结构校验不能保证模型绝不误判，需保存来源、目标展示、澄清与更正，恢复和晚到结果不随页面改归属。具体算法、工具协议、自动默认目标、批量命令与补救流程未冻结。见专项稿 §3.2 与双方日志。

DESIGN-004：历史 3／3 收口。先确认页面入口、默认新会话与 P1，后据持续授权采用受控 Manager MCP／页面共用创建及查询、可选同项目有权已有会话、完整本地原子范围、可信来源与输入快照、新会话初始取 Issue 标题后独立改名、项目保留期幂等占位、P2 自动接续与 P3 Issue SSE。REST 创建基线 201 首次／200 重放，未知查原键。精确 MCP transport／上下文／schema 和其他未写协议继续设计，不等待再次审批。

【最新入口与页面／接口输入】

长期多仓项目
    +-- 会话持续讨论，可无 Issue
    |     -> 首条消息提交后异步准备，实际就绪后投递
    |     -> 明确创建意图经 Manager 受控 MCP 建立独立 Issue
    +-- Issue 栏创建：默认新会话，可选同项目有权已有会话
          -> 同事务登记 Issue／主 CS／必要会话／来源与快照／幂等／待办／事件
          -> 返回稳定业务身份，后台自动准备或恢复
          -> 实际就绪后 Manager 接手，后续按有效计划和当前约束执行
    +-- Issue 专页：目标／范围／计划／验收／主 CS／实际房间导航
          -> Issue SSE 失效通知 + REST 快照

业务会话存在不证明 runtime Ready；来源导航不授权工作，创建记录不伪造用户消息或 Manager 回复。一会话多项不共享执行计划／上游 Project；选择失效的已有会话返回错误，不静默替换。项目创建仍只保存资料和核查工作。
创建契约：docs/current/issue-page-create-api-contract.md，status accepted，页面维护的字段与错误基线。当前七个接口如下：
- GET /api/projects/{projectId}/issue-creation-options
- GET /api/projects/{projectId}/issue-creation-conversations
- POST /api/projects/{projectId}/issues
- GET /api/projects/{projectId}/issue-creations/{creationId}
- GET /api/issues/{issueId}
- GET /api/issues/{issueId}/rooms
- GET /api/issues/{issueId}/events（SSE）
创建只等待本地短事务，不等待外部运行；201 首次、200 同输入重放，同键不同输入冲突。重放核当前读权而不重用新建条件拒绝历史成功；查询404不证明旧请求不会继续提交，不换键盲重建。删除正文后的幂等占位先核权限，返回410 CREATION_RESULT_REMOVED，不再进入正文比较或新建分支。
conversation 省略等价 mode=new；existing 必须给同项目有权会话 id，new 不允许 id。规范化输入包含关联选择。候选分页默认50最大100；请求体256 KiB，标题200／描述20,000个Unicode标量，最多100个仓库、100条验收，每条最多2,000个Unicode标量。确切条件和错误看原契约，不在后端另写冲突版本。
Manager 工具已采用 repomesh_create_issue、repomesh_get_issue_creation 及共用业务字段和限制；项目／发起人／Manager／会话由可信上下文绑定。source.kind=manager_mcp，输出只有issue／rooms链接，操作用查询工具核查，不返回只查询页面操作的links.operation。工具名称和业务职责已定，完整可执行JSON Schema、来源引用／操作承接及transport还未写完。
来源快照只保存允许字段的确切提交、规范化摘要及版本、可信操作者和真实页面／消息来源；不保存密钥、认证凭据或隐藏推理。操作键项目保留期不复用，删除正文保留最小无正文占位，不让旧请求复活。
Issue SSE 只失效通知，REST 返回当前事实；业务或本地运行观察变化与事件同事务持久化，提交后发布。建立连接／游标失效需重取快照，重连按订阅范围游标补查，订阅／发送／查询都核当前权限。项目列表、会话消息、完整状态聚合等尚未写出的协议继续设计；不能用 Issue SSE 代替会话投递。
这些是 RepoMesh 产品接口设计，不是 AgentTeams 原生端点。数据库约束、权限集成和真实 SSE 尚未验收，具体采用范围及允许字段以当前契约为准。
当前原型：
C:/Users/18092/.codex/visualizations/2026/09/09/01a08562-2a37-7371-8a6e-050e47013223/repomesh-conversations-issues.html
上一版内存演示尚未补入可选已有会话及页面建项后自动准备，后续进度以页面日志为准；旧版保留，原型存在不等于真实后端完成。

【关键业务约束】

1. 每多仓项目独立 AgentTeams 实例，一个长期 Manager，每仓长期 Team／Leader；人统一与 Manager 对话，Leader 负责被委派仓库。用户主会话可关联多条 Issue，但各项工作目标、计划、执行和验收隔离；实际多事项上下文与 room/session 映射仍待验证，不把共享聊天当作共享执行状态。
2. Issue 是内部整体正式事项，组织内部仓库事项，可选关联 GitHub Issue；首期不默认外部创建、评论、同步或关闭。每正式 Issue 默认一个主 ChangeSet，与 Issue 和待办原子登记，可暂时没有候选、组合和 PR。会话本身不转正，新增仓库事项不默认各建或嵌套 ChangeSet。
3. Worker 单活跃任务，Attempt 独立副本。团队 Worker 并发默认 1，不含 Manager／Leader／验证负责人，仍受全局容量、项目额度及当前有效权限限制；按 Skill 能力请求，RepoMesh 实际分配及启动。
4. 当前统一 YOLO，无人工审批等待。Manager 解释缺失业务规则并记录来源，不覆盖明确要求、不冒充人已确认、不放宽权限、预算或验收。受限返回真实未完成结果。后续审批规则已在 ADR-0003 §1.7—1.11 确认，但能力后续开发，不重问，也不恢复此前未采用的默认审批建议。
5. Plan Version、许可、生效分开；自然语言经 Manager 调整编排，直接改图后续做。完整受影响上游 Project 停止新派工、在途收尾并核对 submitted 后才能应用及读回，冲突／拒绝／失败留历史。
6. 顺序、并行、汇合、有界 Loop；默认修复 3 轮、诊断 2 轮、环境恢复 2 次，预算或时限先到先停，换计划不清零；连续两轮同一失败且无新增有效证据则停止。
7. 验证小组默认关，开有固定负责人，关由 Manager 组织。正式验证者与本轮业务作者分离；每验证 Attempt 独立环境，固定 commit 组合、配置与数据。Leader 提供运行说明和 mock，验证侧不向业务仓推修复。结果五类及必检证据门槛按现行文档，新组合重新判断证据适用性。
8. GitHub App 固定工作权限集，运行收窄且不放大发起人有效 Git 权限。候选经 Leader 内部初审接受且有范围／自测后创建 draft PR；满足有效验证及必需检查后推荐审查，人转正式、人按仓规则合并。权限撤销不能借收尾继续无权操作。
9. Candidate 正式提交即入 ChangeSet，未接受也留历史。Manager 从接受候选选择固定组合，未改动仓库固定基线，不静默替换在途组合。执行、验证、交付分别汇总；摘要带来源，当前 ChangeSet 优先，历史按当前权限可见，不默认跨项目。
10. CS1—CS14：约定交付物、验收与必做事项满足，必需 PR 人工合并且最终实际组合证据有效才完成；部署另记。部分合并保留成功部分并按实际版本推进，不自动撤销。已合并撤销由人发起关联恢复事项，Team 出撤销 PR、人合并；首期部署和数据恢复由有权限的人执行并记录。
11. ChangeSet 是稳定归属主记录、关联明细与摘要视图。记录修订、Plan Version、候选和组合版本分开，字段／表／schema 未冻结。独立逻辑操作幂等，同键不同输入拒绝；目标版本校验，同目标冲突串行，超时／重启先核查，保留事实、判断、更正及证据。
12. J1—J3：先选择实际有权参与的仓库；待接受邀请不等于授权，账号关联不等于 App 授权。项目不选分支，Manager 按正式需求定分支与固定基线；模型密钥在设置配置，项目引用配置。新仓库须用户明确加入，不自动扩展在途 Issue；权限部分失效按影响范围受限，无关获准工作可继续，历史读取也按当前权限，不承诺恢复授权就自动重启已结束事项。
13. H1—H15 Skill 工程方向虽同意，整个模块按 ADR-0009 待定占位，不开发管理、评估发布、分发加载、版本隔离或页面，不创建空代码、接口、开关。不能把成员工作区分发等同于按 Issue 固定版本成功加载。

【未完成事项与下一步】

先处理双方日志未结通信，再按持续授权推进具体设计，直接报告结果。COMM-001 第 1 轮，DESIGN-001 第 1／3、DESIGN-002 第 3／3、DESIGN-003 第 1／3、DESIGN-004 第 3／3 均保持，不能借授权落实或压缩清零。完整原文与最后收发以日志为准。

用户决定证据：
- 2026-09-09T11:18:59.124Z：内部仓库事项可关联 GitHub Issue；11:25:55.800Z：同意一会话多项、Issue 专页及主／Leader 房间导航。
- 2026-09-09T12:30:56.538Z：“是的，我建议给manager可以额外写一个创建issue的mcp让其可以调用，你觉得如何”。当时“是的”采用自然语言目标，后半句提出工具评估。
- 2026-09-09T12:37:30.362Z 要求 Issue 栏创建；12:42:29.811Z“默认新建会话”仅确认当时默认值。
- 2026-09-09T13:03:30.896Z“确定”对应原子保存单项问题；当时未整体批准启动、SSE、具体快照。
- 2026-09-09T13:12:51.158Z：“之后的待决定都批准，按照建议来。”此后按 design-delegation.md 采用完整创建方案并继续设计，不倒填为此前单项批准，不重问 P1—P4。

已完成当前决定落实：ADR-0016／0018 定点补充，后端专项稿 §2／§4／§5／§6 与本 prompt 同步；页面创建契约 v1、双入口机制和持续授权清单均已作为 adopted 设计基线落盘。双方同步已经完成，不再等待旧回执，详见下方“本次交接结果”。新授权不编码、部署或新建任务。

后续需要完成：
- MCP 确切 schema、可信身份与消息依据绑定、传输接入；双入口统一命令已采用。
- 会话消息保存／投递／澄清／更正、稳定操作目标与晚到结果；消息顺序保障在新对象下的具体范围。
- 一个 Issue 的反向主会话关系，内部仓库事项每仓数量与生命周期，Leader 房间共享及角色上下文。
- 真实就绪、并发准备复用、失败恢复、配置门槛、无 Issue 工具与预算归属；P2 触发已采用。
- 创建候选分页已见创建契约；继续设计项目／Issue／会话列表的完整分页、会话与项目快照／事件、权限变化和跨快照一致性。Issue SSE 已采用，默认事件保留 24 小时、15 秒心跳；实际运行待验证。
- 项目创建 §1.1—1.2 的完整契约、外部核查未知处理，以及后端模块／数据模型／数据库约束。

这些是继续设计或待验证的工作，不是等待用户逐项批准。遵守历史明确否决／暂缓；出现与其冲突才报告具体冲突，不能借授权将未验证能力写成已实现。

【本次交接结果与通信状态】

截至本文件更新时：后端 DESIGN-004-BACKEND-CP12 已记录持续授权合并事实回执投递；页面 DESIGN-004-PAGE-CP15 已明确实际收到、保存并核对，双方创建基线一致，无明确冲突，无待回复设计消息。不能把旧日志中“等待回执”当成当前状态，也不需要再发收件确认。若接手时有更晚记录，以最新原文为准。
最后收到的直接消息是页面交接通知：页面已更新 NEXT-SESSION-PROMPT.md、HANDOFF.md、README.md，新页面任务将核实身份并通知新的回复目标；该通知仅记录、无需回应。最后发送仍是后端持续授权合并事实回执，无需补发确认。双方当前均在准备下一任务交接，没有创建新任务；接手后先核实页面任务是否已经换人。本次没有新产品决定或新的讨论轮次。
后端本轮成果为 ADR-0016、ADR-0018、draft-conversation-backend-design.md、NEXT-BACKEND-SESSION-PROMPT.md 及后端日志。既有决定保留原 date 与确认来源，只加后续补充；没有新增 ADR-0020。将来需要新增编号先核对当前目录，不能预留或硬编码下一编号。
结果核验：此前五份后端 Markdown 链接、行尾空白和冲突标记检查通过，41项主阅读清单路径均存在；页面报告九份文档155个本地链接、六份JSON示例检查通过。后者不是后端独立运行的检查。没有真实API、MCP、数据库事务、权限、队列、SSE或上游运行验收。上一版原型未补可选已有会话和自动准备。

【原设计任务开始后的执行顺序（历史，本轮不执行）】

本段的旧身份核实／通信和 MCP、数据库实施建议不构成本轮授权；当前新阶段先按页首与 HANDOFF 读取实际工程状态，再按新任务范围接续。

1. 按上述入口及41项主清单完整读取原文，确认本地路径、最新授权、先后替代关系和实际协作者身份；长日志分段读完。补写接手检查点，保留原轮次、原话、最后收发和未完成项。
2. 先用简短中文说明当前理解、已采用创建基线与尚未实现边界，然后继续设计；无需询问“是否继续”，不重新讨论 P1—P4。
3. 第一项具体工作：补齐 Manager 创建／查询 MCP 协议及可信逻辑操作来源。以“用户明确要求把订单导出建项，响应丢失后恢复”和“一条消息明确创建两项独立工作”为场景，定义业务输入／输出、工具业务错误、消息／澄清来源引用、可信身份绑定、一个意图的持久操作身份、重试与当前权限核验；不能以传输调用号、随机换键或会话唯一键代替。
4. 该项是已采用能力的技术细化，不重开 DESIGN-004 的产品选择。先与页面对齐当前文件和分工，沿历史登记其细化进展，不通过另起编号清零旧主题轮次；对齐只涉及本次具体协议，不重复做第四轮产品评价。按持续授权采用推荐方案，直接报告结果和代价。
5. 先确定文档归属再落笔：后端可补专项稿或独立协议 Markdown，页面机制及 REST 契约由页面维护；不要同时编辑同一稿。产出可核查的JSON Schema草案、可信上下文与业务命令职责、逻辑操作／来源关系、异常恢复表和未来验收场景，标明是设计文件而非已部署MCP服务。
6. 本项完成后推进相应模块和数据库约束，再接会话消息、仓库事项、运行准备与恢复；若页面已有更新或更紧迫的独立需求，先读其原文再协调顺序。每轮更新专项稿、接手说明及自己的通信日志，保留采用理由与验证缺口。

【已排除、已替代与明确未采用】

- “首次发送即正式 Issue”“会话整体转正”“每条正式 Issue 独占用户主会话”均非现行模型；会话可无 Issue，也可关联多项。是否保留会话内事项草案继续设计。
- “首条正式 Issue 后才准备实例”原由 ADR-0018 替代；新模型首次消息与页面创建触发已在 ADR-0018 持续授权补充中采用。项目创建即启动仍不采用，不重批旧决定。
- 单仓直达 Leader、每条 Issue 复制团队／实例、不同项目共享一个管理域，不是当前方案。
- 默认人工审批、逐 Worker／逐修复轮人工批准、明确建立事项之外再加隐含开工门禁，当前不采用。
- 第一版 A／B／C 视觉与入口被否定；三栏布局、项目默认私有／邀请成员建议没有采用。最新深色会话式风格与项目分组方向已明确，不恢复旧原型业务规则。
- 不自动扩仓、不静默替换组合、不自动合并或回滚、不借验证修改业务仓、不承诺仅暂停一个分支。
- 不把 Go 动态插件、第三方 Graph 框架、QwenPaw 正式运行组合、所有新接口字段或页面示例当作已选定。独立部署 Graph 服务不是首期采用方式。

【上游证据与结果边界】

2026-09-09 重新核对 AgentTeams 最新 main 为 eeaab64391ccaec9118e84977f538aefd40720d6（09-05 提交），最新 release 为 v1.2.3；本次 Graph／Loop 基线用确定 main 提交，不使用浮动 latest 部署。除静态调研外已有四镜像构建、双实例实际运行、模型往返与 Worker 自有权限证据，尚无 RepoMesh 全链路验收；接手先读[验证执行记录](../../../../../validation/agentteams-2026-09-09/reports/validation-status.md)及[现行专题](../../../../current/graph-loop-design.md)，特别核对实际通道、资源限额、shared 写路径与产物语义。Controller REST 提供资源生命周期及部分工作流能力；taskflow 委派／提交等主要是本地 stdio MCP，不能虚构 REST 任务启动端点。
上游 Project 身份涉及实例、team、project_id，task_id 还需避免跨 Issue／轮次／Attempt 碰撞。replan 遇到 in-progress／submitted 拒绝，paused 直接 replan 的 409 已经真实服务验证；pause 不停止在途。受控 resume 后 replan 或 paused replan 补丁尚未选定，completed Project 不能假定可直接重开。原生 DAG／Loop 与 Dashboard HITL 不证明 RepoMesh 全部编排和审批已实现。

原要求子项与新增原生证据集中于[完成度审计](../../../../../validation/agentteams-2026-09-09/reports/completion-audit.md)：合法DAG候选推进与实际执行去重分开；不同通知故障窗口的恢复结果分开；删除同名重建后旧SA在缓存窗口仍被接受，之后401，旧Matrix身份与存储身份另行核验；当前checkpoint代理200正文为HTML。双Worker各76项官方回归有资源测量，但没有业务调度／配额验收。本轮只新增验证与交接事实，没有新增或实现业务REST／SSE接口。

最新[普通回复／thread实测](../../../../../validation/agentteams-2026-09-09/reports/runtime-reply-thread-live.md)已到已部署handler和真实模型层：入站关系保存，出站只编辑自身新建的主时间线占位，不保留原父消息／thread；session仍按房间。需在消息目标／结果回传适配中处理，不能拿Matrix持久性测试替代。跨Team同名Project和原生查询耗时已补测，见完成度审计；其正向结果仍不证明业务身份映射或采集到页面的完整链路。
同一个 QwenPaw runtime 的 Matrix 房间影响 session_id；不同 Agent 不共享全局隐藏上下文。房间缓冲有内存与长度限制，不能作可靠历史；保留 room 有利于上下文连续是静态设计推断，不能承诺完整恢复。读取 history 不证明有实时订阅；创建受理和 Ready 不同，Controller 内存 ready 观察重启可能丢失。不能依赖未接通的 remoteSkills 创建／更新字段。

已有成果：架构 v1、会话／独立 Issue 后端专项稿、ADR-0010—0019。ADR-0019 替代旧会话绑定，ADR-0016／0018 新补充承接持续授权的创建事务和启动时序；专项稿 §2／§4／§5／§6 已同步创建采用清单。后端整体设计仍有未完成部分，标继续设计／待验证，不再以逐项批准阻塞；当前不新占 ADR 编号。
后端尚未实现；页面仅本地内存模拟原型，页面走查不是后端权限、幂等、持久化、订阅或上游契约验收。此前已检查文档链接及阅读路径，无缺失；修改后继续做相关一致性检查，不为文档整理运行无关代码测试。
工作区存在前序文档整理及其他会话改动，保留它们，不清理、回退或提交 Git。不要把文件出现或 ADR accepted 当成部署完成。

【本会话预期产出】

按持续授权完善后端专项稿及架构设计，必要时为真正独立取舍记录 ADR，优先定点补充已有决定并保留时间及确认来源。当前后端负责相关 ADR、专项稿、本 prompt 和自己的日志；页面维护其契约、专题、授权清单、通信约定、CONTEXT、页面 prompt／HANDOFF／索引、页面日志与原型，避免并发覆盖。
每轮报告主题与原轮次、采用方案、理由和代价、实际改动、未完成／待验证项及接口影响，保存通信原文和结果。按推荐直接推进，不再逐项重复审批；业务 YOLO、权限与仅设计范围不变，不新建任务、不编码或部署。

```
