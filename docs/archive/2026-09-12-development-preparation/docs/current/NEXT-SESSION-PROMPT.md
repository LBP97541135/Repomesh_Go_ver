# 页面／接口设计：新会话接手 Prompt

## 当前设计缺口交接入口（2026-09-10）

用户后续要求将正式开发前的缺口分为页面／接口与后端两份 handoff。页面／接口侧当前目标、P01—P07 清单、推荐顺序、职责和开发就绪条件统一见 [HANDOFF-PAGE-API-DESIGN](HANDOFF-PAGE-API-DESIGN.md)，对侧见 [HANDOFF-BACKEND-DESIGN](HANDOFF-BACKEND-DESIGN.md)。先读新 handoff，再按本文件补读原 41 项材料与原型来源。下方“本轮”、优先做 MCP／分析插件及旧协作步骤保留历史含义，不覆盖新 handoff 的范围与顺序，也不恢复旧任务或联系人。

## 基础工程阶段接手补充（2026-09-10）

用户本轮明确授权创建基础代码、工程配置和必要依赖，替代旧交接在本轮范围内的“仅设计、不编写代码”。本轮只建立最小可编译工程；原产品决定、权限和暂缓范围继续有效。下方 09-09 设计接手步骤、通信身份和阅读清单保留作来源，不自动恢复旧任务、联系旧协作者或启动仓库分析／MCP 等业务开发。本轮协作以当前多 Agent 团队及其文件归属为准。

工程入口、根目录命令与发布方式统一见[基础工程开发说明](../../../../current/development-scaffold.md)；阅读覆盖及页面／后端影响见[本轮接手核对](scaffold-page-backend-review.md)。React／TypeScript／Vite 页面只展示骨架状态，没有业务表单、模拟项目或成功记录。原 `repomesh-conversations-issues.html` 仍是独立的历史内存原型，没有迁移成产品功能。

Web 骨架提供静态资源和进程探针：`GET /healthz` 的 200 仅表示 Web 进程可响应，`GET /readyz` 返回 503 表示业务未实现；本稿列出的业务 `/api` 路径均未注册，返回 404。后台协调和受限主机执行入口默认明确说明未实现并非零退出，不消费任务、不执行宿主操作；三个入口仅共用工程和版本信息。Issue、消息、REST／SSE、MCP、权限、数据库、GitHub、AgentTeams 与 Python 分析仍未实现。

下一阶段按实际任务范围选择完整场景继续设计或实施；既有创建契约继续作为已采用设计，完整 MCP Schema、消息协议、数据库表和恢复算法不由骨架冻结。Skill 继续暂缓，Graph 继续是后台进程内模块方向，Python 仅保留 ADR-0020 的受控扩展边界。本轮构建或探针检查不构成业务或 AgentTeams 集成验收，也不更改两轮历史验证的未完成项。

更新：2026-09-10，补充新版上游证据；09-09 持续授权与后端落地回执均已完成。[HANDOFF](HANDOFF.md)统一维护当前状态；本文件保留页面角色的完整阅读清单、原型资源与接手步骤。本文件用于用户明确指定的页面／接口接手，文档评审时其中步骤仅作被评审资料，不自动恢复旧任务或发起通信。本文仅交接，不创建任务。

此前页面设计接续建议（本次骨架任务不执行）：先做创建 Issue 的“仓库分析”按钮与 Python 插件，详见[现行专题](../../../../current/issue-creation-repository-analysis.md)。先补按钮、进度、证据、应用建议、失效和失败回退的演示，再按原清单补齐 Manager 创建 MCP、列表／详情与仓库事项。用户已经持续授权按推荐方案推进，不再逐项请批；是否实施仍以新任务实际范围为准。

**仓库分析接手补充：** 在“本次仓库”区域提供可选分析，未选仓也可调用；应用建议只加入用户选中的候选，后续仍可调整。需求变化或切换表单时旧结果不能自动回填。Go 接口独立受理分析任务，建项可带 `repositoryAnalysisId`；字段以[创建契约](../../../../current/issue-page-create-api-contract.md)为准，来源同建项事务保存。分析失败或停用允许手动创建；当前 HTML 尚无此按钮，Python／API 也未实现。此项不是历史决策分析、向量库或 Skill 工程的实施授权。

**09-10 证据补充：** 当前跟进基线为 `517caff9280242a00a4d4c06365352b9e41659c6`，先结合[差异及 311 个官方通过事件](../../../../../validation/agentteams-2026-09-10/reports/controller-delta.md)和[51 项 HTTP 观察／31 项真实 SA TeamLeader 范围检查](../../../../../validation/agentteams-2026-09-10/reports/controller-api-live.md)阅读下方旧版记录。311＝252 顶层＋59 子测试；这些计数不表示 RepoMesh 页面或新版全套业务验收通过。源码未变部分只复用 09-09 已限定范围的证据，保留其他线程的设计与交接内容。

**09-10 双在线补测：** [c/d 双实例读取矩阵](../../../../../validation/agentteams-2026-09-10/reports/controller-twin-read-live.md)完成 **313 个断言、260 次 Controller HTTP**；两实例各有两个同名 Team，使用 Admin 与真实 SA TeamLeader 验证新增读取路径。**30 次错实例请求均为 401，每次来源前后请求均为 200；24 个夹具原字节哈希前后不变**。这是停止／未托管引用和元数据夹具的读取契约验证，不是业务验收。原 51／31 项保留独立历史计数；本次在线、TLS 与来源身份有效的对照补充旧时钟异常观察，不覆盖或改写其原始证据。

**09-10 存储与模型补证：** [真实S3 trace](../../../../../validation/agentteams-2026-09-10/reports/mermaid-storage-read-live.md)补齐20项检查：JSON前后正对照读取TaskMeta，Mermaid true/false有限窗口内目标TaskMeta的HEAD／GET均0。[新版Manager实际推理](../../../../../validation/agentteams-2026-09-10/reports/manager-model-smoke-new.md)随后通过一次Matrix消息返回精确标记，原生会话及用量确认1次模型调用、未观察到工具调用；Console503和后置读探针错误保留。以上均不代替RepoMesh业务验收。

页面接入须注意：workflow 缺省 JSON 可用，`format=json` 返回 **400**；Task inspection 的 TaskMeta 原始 status 与缺失时的规范化图状态不能混成一个枚举，history/trace 不表示审计已完整或任务已实际执行。`task_id=end` 的 Mermaid API 200 已被[真实浏览器确认解析失败](../../../../../validation/agentteams-2026-09-10/reports/mermaid-browser-live.md)，不能直接呈现为图成功；实际 CLI detail 传 `--team` 仍返回同名歧义 **409**。31 项身份检查来自真实 Team membership 的 TeamLeader，不是普通 Worker 权限证明；CFG01—05已验证到QwenPaw进程内存模型配置并恢复chat，见[Manager实测](../../../../../validation/agentteams-2026-09-10/reports/manager-config-live.md)；未据此宣称模型调用或业务验收通过。本次仅同步上游事实，RepoMesh 业务 REST／SSE 契约无变化。

## 1. 角色、工作区与目标

以下保留原页面设计任务职责；基础工程阶段采用页首范围，不将旧身份和通信步骤作为本轮操作授权。

请接手 RepoMesh 的页面设计和接口设计。你负责页面交互、浏览器到 RepoMesh 的 REST／SSE 契约，并与后端共同补齐创建 Issue 的 MCP 边界；另一位 Agent 负责后端业务实现设计、可信运行接入和架构。用户于 2026-09-09 授权双方直接通信，每次设计对齐，用户最新授权“之后的待决定都批准，按照建议来。”；推荐方案对齐后直接推进并报告，见[持续授权清单](../../../../current/design-delegation.md)，不再逐项等待批准。先完整读取 [通信约定](design-communication.md) 及双方通信日志，恢复主题、轮次和待决状态；同一主题最多三轮，每次自动压缩按约定保存并恢复通信信息。

实际工作区：D:\Project4work\Repomesh_Go_ver
不要把历史消息的转义写法误读为 D:\Project4work\Repomesh\_Go\_ver。

目标：在已采用业务及架构规则上，继续完善中文页面交互与 REST／SSE 契约，每个业务场景同时说明页面、状态、权限、异常和接口，形成可评审的本地原型与 Markdown，供后端侧细化。当前不是生产实现任务，也不要求先讨论完全部架构才开始页面。

## 2. 当前进度与接手位置

最新接续：用户持续授权后续待决定按建议推进。[创建接口契约 v1](../../../../current/issue-page-create-api-contract.md)与[双入口创建机制](../../../../current/manager-create-issue-tool-design.md)已采用为设计基线，必须完整阅读原文，不以本段替代。包含具体创建／核查 REST、可选会话、原子快照、幂等、错误、实际房间与 Issue SSE；尚未生产实现。

当前创建方案：Issue 栏默认新建业务会话，也可明确选同项目有权已有会话；Manager 的受控 MCP 与页面复用同一业务创建命令。必要新会话、来源关联／真实记录、输入快照、幂等结果、Issue／主CS／持久待办同一本地短事务；新会话初始名取 Issue 标题，之后独立改名。首次保存会话及首条消息，或页面建项提交后，异步准备／恢复项目实例与关联主房间；项目创建和空会话不启动。已有资源复用，外部准备在事务外，实际就绪／接收／执行分开取证。

DESIGN-001 准备期保存补充、按服务端顺序尝试投递已采用，前序未知先核查；DESIGN-003 自然语言明确目标、歧义澄清已采用。二者原第 1／3 轮保持。DESIGN-002 会话与 Issue 分离、一会话多项、内部仓库事项、独立计划／验收／主 ChangeSet 及 Issue 专页实际主／Leader 房间导航已采用；DESIGN-002／004 历史均 3／3，不更名清零或重开第四轮。

当前原型支持独立 Issue 列表／详情、默认新会话创建及真实来源记录；尚未补入可选已有会话和页面提交后自动准备，未模拟语义识别或真实 MCP／REST／SSE。已采用设计与原型完成度分别记录。

最近一次对齐已完成：后端已更新 ADR-0016／0018、后端专项稿、后端 prompt 和日志，页面已核对并将完整回执保存至页面日志 CP15。双方无明确冲突，无待回复设计问题；不能把旧 CP14 的“等回执”恢复成当前状态。

下一步按推荐补齐 Manager 创建工具的完整 schema／可信来源操作承接，再深化 Issue 列表／详情及仓库事项、消息与状态恢复契约；每次与后端对齐并直接报告结果，不再把这些列为逐项待批。运行适配能力仍须验证，不能由设计授权推断上游已支持。

## 3. 协作方式与授权边界

以下为原设计阶段规则。原型授权与生产编码禁令在当时有效，本轮基础代码／配置／依赖按页首新授权执行；旧任务通信、真实业务集成和部署不因本轮授权恢复。

- 全程中文。按持续授权选择推荐方案，记录场景、理由、代价并推进，向用户报告结果，不再逐项请批。用户明确否决／暂缓仍有效；未编制内容和运行证据分别列为继续设计／待验证。
- 页面与接口按同一场景一起讨论：先用户看见什么、能做什么、操作后状态怎样变化，再说明 REST 查询／命令、同步返回与后台完成、SSE 通知、权限及异常。接口包括调用者、输入输出、业务状态、幂等、并发修订与恢复，不直接把表翻译成 CRUD。
- 每次设计通过现有任务直接与后端对齐，按通信约定保存原文和轮次；每轮结束向用户说明设计变化、更新文件准确位置、REST／SSE 影响、关键状态／权限／异常、已采用范围及尚待设计／验证的工作。没有接口变化时明确写“本轮接口无变化”。双方对齐后依持续授权采用；不自动创建或委派新任务。
- 收到后端更新后，先完整阅读相应文件，区分已采用、继续设计与待验证。持续授权采用的创建方案以当前契约为准，原单项确认的范围不倒填；不将整份未完成后端稿或运行能力标成已完成。
- 继续停用 grill-with-docs。当前允许中文讨论、本地 Markdown，以及用户明确要求的可丢弃本地交互原型。原型是原“不编码”要求的局部例外，不授权生产代码、安装、部署、真实服务接入、远端写入、创建真实 GitHub App／Issue／PR／任务，或自动创建 Codex 会话。
- 原型使用内存模拟，不连接真实模型、仓库或业务接口。可临时本地预览并检查，不能启动真实产品服务。演示控件不是实际业务按钮，原型走查不等于后端验收。
- 定义变化才修改 CONTEXT，它只能放术语；独立架构取舍按需 ADR。同步专题、HANDOFF、索引与本 Prompt，避免只追加新说明而保留冲突的旧下一步。
- 工作区存在既有未提交改动，另一位 Agent 可能更新共享文档；2026-09-09 文档评审已确认工作区属于 Git 仓库。接手时重新核对实际状态，不能沿用旧会话的仓库识别失败或声称未执行的检查已通过；不为交接初始化 Git。修改前重读目标段落，保留他人内容，不清理、回滚或覆盖非本轮改动。不要为了交接自动提交 Git。

### 通信身份、分工与恢复

历史通信身份与恢复说明仅供溯源；本轮不执行核实旧任务、通知旧协作者或追加旧通信日志的步骤。

| 角色 | 交接时任务标题 | 任务 ID | 主机 |
| --- | --- | --- | --- |
| 本次页面交接的来源任务 | RepoMesh页面与接口设计_0 | 01a08562-2a37-7371-8a6e-050e47013223 | local |
| 已建立通信的后端任务 | RepoMesh后端架构讨论_1 | 01a08556-74cc-76d1-891a-bbece9ea81ef | local |

上表页面 ID 是来源任务，不是新任务自己的 ID。新任务先核对实际身份及后端现状，在通信约定中更新页面接手身份，并在页面日志追加交接记录，保留旧原文。通过现有任务消息工具联系后端，不创建替代任务；把自己的真实 ID／标题及接手位置通知后端，使后续回执发往新任务。工具不可用时如实说明，不冒称已通信。

页面维护页面专题、创建接口契约、创建工具的产品契约、CONTEXT、页面 prompt、HANDOFF、索引、通信约定、页面日志及原型。后端维护后端专项稿、后端 prompt、架构 ADR 和后端日志；涉及 MCP schema 的新增文件先对齐维护归属，避免两份字段基线。共享文件修改前重读，不改对方正在维护的文件。

COMM-001 已收口；DESIGN-001／003 均 1／3，DESIGN-002／004 均 3／3。补齐尚未写出的协议时，只对齐具体缺口、接口差异及实现约束，不重评已采用的双入口、默认关联或启动取舍，不用新编号或任务清零历史上限。达到三轮后按持续授权记录推荐取舍并报告，不能无限往返。

每条消息发送前保存完整正文，发送后记工具结果，实际收到后保存完整回执。自动压缩无法保证有事前回调，因此通信即时落盘；恢复后的第一项协作操作是完整读取通信约定与双方日志，补记身份、主题、轮次、最后收发、未回复、授权证据及下一步。可预知压缩时先写检查点，不声称安装了自动压缩钩子。本次最后实际压缩恢复为页面 CP13，CP15 是完成检查点，不是新的压缩。

## 4. 完整阅读顺序

先完整阅读本 Prompt。随后依次完整阅读 docs\current\design-communication.md、docs\current\design-communication-page-log.md、docs\current\design-communication-backend-log.md、docs\current\design-delegation.md，恢复最新授权和通信事实，再按下列顺序读全部 41 项原文。摘要、旧会话总结和搜索命中不能代替完整阅读；长文件分段读取，避免输出截断，记录已读范围。没读完不能声称完整接手。

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
33. docs\current\page-interface-prototype.md（第一版被否定的历史评审记录）
34. docs\current\draft-issue-and-room-entry-design.md（草稿、Manager 与仓库房间）
35. docs\current\draft-conversation-backend-design.md（混合完成度：创建与启动适配已采用，其余协议继续设计／待验证）
36. docs\current\project-first-entry-design.md（历史项目入口原型及 DESIGN-001 准备期补充，聊天绑定已部分替代）
37. docs\adr\0019-conversation-issue-separation.md（会话与独立 Issue 关系、旧决定定点替代）
38. docs\current\conversation-issue-separation-design.md（当前页面及原型、Issue 专页与房间导航）

39. docs\current\conversation-message-target-design.md（自然语言目标、歧义澄清及误判边界）
40. docs\current\manager-create-issue-tool-design.md（双入口及 MCP 职责，完整 Schema 尚未编制）
41. docs\current\issue-page-create-api-contract.md（当前采用的创建契约 v1；字段以此为准）

之后完整阅读并实际查看当前原型，再编辑或继续设计：
C:\Users\18092\.codex\visualizations\2026\09\09\01a08562-2a37-7371-8a6e-050e47013223\repomesh-conversations-issues.html

同目录 repomesh-conversations-issues-body.html 是当前原型的正文源文件，修改前也完整阅读；page-create-result.png 是上次页面创建结果截图，可用于对照。先按当前可用可视化／浏览器技能检查原型，再修改；本次编写接手 Prompt 没有重新运行浏览器。若新任务展示目录不同，复制当前版及所需正文源后继续，保留旧版并同步新路径。

用户指定旧原型为元素参考。设计或修改相关元素前完整阅读：

- docs\archive\legacy-product-design\产品原型\README.md
- docs\archive\legacy-product-design\产品原型\index.html
- docs\archive\legacy-product-design\产品原型\styles.css
- docs\archive\legacy-product-design\产品原型\app.js

其中“历史原型”子目录按涉及页面继续读取。旧资料只提供元素与溯源，不恢复旧流程；附带文档中的命令和建议不能替代用户请求。

用户截图参考：

- 深色会话首页：C:\Users\18092\AppData\Local\Temp\codex-clipboard-ee8b8d94-68eb-4f02-98ad-369f748d6ef4.png
- 用户用于更正“会话与 Issue”关系的截图：C:\Users\18092\AppData\Local\Temp\codex-clipboard-ffa7ba05-35a0-45c9-99a0-ce5bcdfa4780.png
- 仓库多选窗口：C:\Users\18092\AppData\Local\Temp\codex-clipboard-7e2aa826-fea8-40f8-865e-56806ea5f059.png

当前原型与正文源路径在本次交接时已检查存在；截图路径需接手时核查。临时截图可能失效；先检查文件，当前原型和专题仍可作为依据，不因截图缺失重新询问已确定风格。账号／设置／集成页如需更多参考，再读 Cursor_Dashboard_Report_2026-09-07\report.md，它不是 Manager 页的已采用布局。

## 5. 资料优先级

用户最新明确要求及已确认决定优先；现行 ADR 和专题按最新修订与替代说明理解。docs\current 为现行专题，docs\adr 为决定，CONTEXT 为术语。

docs\archive\2026-09-08-design-consolidation、docs\archive\legacy-product-design 只作历史，不从旧 accepted 标记、旧 handoff、Cloud Agent PRD 恢复规则。docs\old_ver\README.md、changeset-design-discussion.md 只是旧链接入口。

入口与关联统一按 ADR-0019 的定点替代理解，旧“聊天整体转正”不再沿用；ADR-0016／0018 必须读完持续授权补充，当前原子范围及新对象启动适配已采用，原时序只作为历史。用户表扬原型不等于冻结按钮、表单、接口、状态或示例范围。

## 6. 已完成内容与结果

- 文档结果：创建契约 v1、双入口机制及持续授权清单已采用；页面九份 Markdown 与后端五份 Markdown 已同步，完整回执见页面日志 CP15。此前检查页面155个本地链接、六份 JSON 示例、文档空白及冲突标记通过；后端报告其五份文档与41项必读路径检查通过。均为文档检查，不是真实接口验收。

- A—E、ChangeSet CS1—CS14、J1—J3 已确认；技术栈已收口，决定到 ADR-0019。旧入口和一一对应房间拓扑以 ADR-0019 替代说明为准。I 当前 YOLO、后续审批延后；Skill 工程暂缓。
- 视觉：近黑主区、稍亮侧栏、克制边框、圆角输入框；左侧按项目展开会话，Issue 有独立页面，不重新挑大布局。
- 项目创建：无项目时先搜索／多选仓库，再填名称、用途、查看默认配置；不选分支、不重复填密钥。保存后左侧出现项目且尚无事项；支持复用已有项目、创建第二个项目与分组切换。
- 会话入口：原型演示首条消息后准备、期间允许补充并区分消息状态；新对象下启动触发已按持续授权采用，见创建契约。Manager 可用不代替本条主房间就绪。
- Issue 专页：同一会话可明确建立多项并显示关联卡片；列表／详情显示独立工作和主 ChangeSet，可往返主房间与只读 Leader 房间。计划和房间就绪通过独立模拟事件表达，不由创建成功推断。
- DESIGN-004 原型新增：Issue 栏显式创建默认新会话，提交后进入 Issue 详情，侧栏显示会话与真实业务创建记录。未就绪无房间入口，不模拟创建即启动或派工。已检查未启动项目直接创建、表单失败／提交前取消不新增、重复标题独立建项及 320—1024 宽度无溢出；控制台无错误／警告。网络超时、幂等及新增事务范围未验收。
- 原型版本：当前为 repomesh-conversations-issues.html；同目录 repomesh-conversations-issues-before-page-create.html 保留本次修改前副本；同目录 repomesh-project-first.html 为拆分前历史，旧会话版及 A／B／C 也保留，不误改旧版。
- 此前原型已检查：同会话三条独立 Issue，创建与普通聊天不改旧项；纯讨论和准备期补充不创建 Issue；Issue 到主房间、只读 Leader 房间及返回保留正确目标；未就绪 Leader 无进入按钮。列表、详情、主与 Leader 房间在 1024／736／360／320 像素无横向溢出，当前预览控制台无错误／警告。既有项目向导检查见旧专题，未重复运行全部旧流程。这是内存原型检查，不证明真实权限、持久化、幂等、队列、SSE 或后端正确。
- 固定回复与示例数据均为内存模拟，刷新重置。窗口外可载入同会话两条 Issue 示例，或模拟消息和指定 Issue 的就绪。示例创建表单、最多前两个可用仓库及每仓一个仓库事项不冻结生产算法／基数，导航来源提示不决定消息执行目标。未接真实服务。

## 7. 必须保留的关键约束

1. 每个长期多仓项目独立 AgentTeams 实例，一个长期 Manager，每仓长期 Team／Leader；所有需求包括单仓均由 Manager 统筹，每条需求独立上下文，不按每条需求复制整个团队。
2. 人通过 Manager 沟通。仓库房间本版只读；首期图用于查看，编排调整通过自然语言形成版本记录，直接改图延后。
3. 项目、业务会话、Issue、仓库事项、主 ChangeSet、实际 room 和角色 session 分开。一个会话可关联多项；底层上下文隔离与恢复另行验证，不据共享入口混用工作归属。
4. 项目先保存、讨论不直接派工、消息先持久化、就绪后投递等保障保留；首次会话消息与页面建项提交后的准备触发已采用。聊天不转正，在会话中明确建立独立 Issue；准备实例不授予执行权限。
5. 消息保存、待投递、传输接收、Manager 接收／处理和回复分开。无证据不能显示已接收；准备失败、配置不足、权限失效、容量等待、超时未知分别如实表达，保留原项目、草稿与操作归属。
6. 账号访问、App 能力、项目范围和本次工作范围分开核验。项目保存不等于执行可用；某仓受限不整体禁止仅涉及可用仓库的工作。历史读取遵守当前权限；项目扩仓不自动改变在途 Issue。
7. 当前统一 YOLO，不加计划审批／开工批准。“明确创建 Issue”是业务边界，不是新审批门禁。Manager 不覆盖用户明确要求、不冒充确认、不扩大权限预算或降低验收。
8. Plan Version、许可与生效分开。按完整受影响上游 Project 停新派工，在途及 submitted 收敛后应用、读回；多目标部分应用不宣布整体生效。上游 pause 不停止在途，也不保证只暂停某分支。
9. Worker 单活跃 Attempt、独立副本；Attempt／Worker／容量短事务统一预留，环境在事务外准备，启动前再核验，超时不等于资源可释放。Worker 默认并发 1，不含 Manager／Leader／验证负责人，受全局与项目限制。
10. 执行、验证、交付分别汇总。独立验证者与本轮作者分离，验证绑定固定组合、配置、数据与前提；必检证据满足才通过。提交候选／Leader 接受不等于整体通过，新组合重判证据。
11. 正式 Issue、主 ChangeSet 与后续待办同事务保存；页面创建默认新会话时，必要新业务会话、来源关联、真实创建记录、允许字段快照、幂等结果及相应持久事件也一起保存（P1 及后续持续授权），事务不跨外部长时间等待。逻辑操作幂等，同键不同输入拒绝，校验目标修订，结果未知先核查。每个创建操作的持久约束和竞态算法仍待定，不能设置会话级只准一条 Issue。
12. Agent 只推工作分支、交付 draft PR；验证与检查满足后推荐审查，由人转正式 PR、按仓库规则合并。YOLO 不授权自动合并。完成要满足交付、验收、必需 PR 和最终实际组合证据，部署另记；部分合并不自动撤销。
13. Skill 工程整体暂缓，不创建其页面、接口、空代码或开关。验证小组默认关闭；已确认的循环上限、停止条件、预算累计、恢复及历史规则继续按原文执行。
14. 技术栈已选 React／TypeScript／Vite／React Flow、Go、PostgreSQL 与持久队列、REST＋SSE、对象存储和模块化单体。Web、后台协调、受限主机执行同一 Go 工程配套发布。09-09 用户明确复用最新上游仓内 DAG；进程内 Graph 模块管跨仓前驱、结果适用性与 Loop，后台核验后派工。补读 [Graph／Loop 专题](../../../../current/graph-loop-design.md)及 ADR-0014／0015 补充；React Flow 展示业务汇总及上游／Attempt 明细，不将原生 ready／completed 当作业务许可或验收通过。
15. 浏览器不暴露上游管理凭据，Web／Agent 不直接持有 Docker socket。保留上游协作生命周期，通过 RepoMesh 控制工具与执行，必要时小范围补丁；不靠提示词替代实际约束。
16. 2026-09-09 重新核对 AgentTeams 最新 main 仍为 eeaab64391ccaec9118e84977f538aefd40720d6，最新 release 为 v1.2.3；本次采用确定 main 源码基线。证据已含双实例实际运行、六个模型响应、Worker 自有权限与停机恢复，接手先读[验证执行记录](../../../../../validation/agentteams-2026-09-09/reports/validation-status.md)及 [Graph／Loop 专题](../../../../current/graph-loop-design.md)。页面尤其不能把保存消息等同完整恢复处理、submit 的 ok 等同附件已交付、Project completed 等同业务已验收。这些实验不证明 RepoMesh 受控执行、业务事务或页面 SSE 已实现；原生 DAG／Loop、HITL、历史查询及 session 局部观察也不能扩成完整产品保证。
17. 新增补测和未完成子项见[完成度审计](../../../../../validation/agentteams-2026-09-09/reports/completion-audit.md)。原生DAG就绪不证明实际工作只启动一次；新Worker资源存在不证明旧身份立即撤销；当前checkpoint代理200正文为HTML，不能呈现为恢复成功。双Worker官方回归通过只证明该组件工作量。本轮业务REST／SSE接口无变化。
18. [实际普通回复／thread补测](../../../../../validation/agentteams-2026-09-09/reports/runtime-reply-thread-live.md)中，入站关系保存在Matrix且消息被模型处理，但Worker出站没有保留原父消息／thread关系；两类消息仍共用房间session。页面不能靠原生回复自动确定线程或Issue，可信目标与回复关系适配继续保留为待实现。

## 8. 接口现状：已采用与未完成分开

[创建接口契约 v1](../../../../current/issue-page-create-api-contract.md)为当前字段基线，完整原文优先于以下入口表，未实施：

| 用途 | 已采用接口 |
| --- | --- |
| 创建条件及仓库候选 | GET /api/projects/{projectId}/issue-creation-options |
| 可关联已有会话 | GET /api/projects/{projectId}/issue-creation-conversations |
| 页面创建 | POST /api/projects/{projectId}/issues |
| 原页面创建操作核查 | GET /api/projects/{projectId}/issue-creations/{creationId} |
| Issue 详情 | GET /api/issues/{issueId} |
| 实际主／Leader 房间 | GET /api/issues/{issueId}/rooms |
| Issue 详情失效通知 | GET /api/issues/{issueId}/events（SSE） |

关键含义：201 为首次本地提交、200 为已提交同输入重放；原键不同输入409，删除正文后的占位优先返回410，不复活旧操作；404查询不能证明在途创建不会提交。页面 conversation 默认 new、可明确 existing；规范化包含关联选择，重试不能改绑。创建回执不是运行快照。

SSE 已采用失效通知、持久事件、范围绑定游标、resync_required 与 REST 补查。默认保留24小时／15秒注释心跳是可调整运行参数，不是性能验收。订阅与读取核验当前权限；项目／会话列表事件、消息正文协议、全站认证接入尚未完成。

MCP 已采用 repomesh_create_issue、repomesh_get_issue_creation 的名称和业务职责；完整 inputSchema／outputSchema、可信上下文与逻辑操作承接协议未编制。Manager 来源为 manager_mcp，来源会话由可信上下文绑定；结果仅提供 issue／rooms 链接，不能返回只受理页面操作的 operation URL。不能将模型自报身份、随机传输编号或“用户已同意”当可信授权。

项目创建／列表、会话／消息、仓库事项、执行／验证／交付聚合接口继续设计。旧 draft-issues／formalizations、Issue messages 或 repository-rooms 路径仅作历史参考，不直接恢复为新模型的通信生命周期，也不替代上述现行 rooms 路径。

## 9. 未完成事项

以下按推荐继续细化并对齐，不等待用户逐项批准：

1. Manager 创建／结果查询 MCP 的完整 schema、可信来源与逻辑操作承接协议；现有业务字段和边界见机制稿。
2. Issue 列表／详情、仓库事项粒度与生命周期、Leader 房间拓扑及只读导航；不重开已收口的旧关系讨论。
3. 会话消息保存／投递／澄清、更正和并发恢复，输入修订与消息游标分开。
4. 将已采用的可选已有会话与页面提交后自动准备补入本地原型；实际接口仍需后续实施。
5. 项目和会话列表的分页、搜索、跨客户端通知及权限变化；Issue 级 SSE 已采用，不等于所有列表事件已设计完成。
6. 深化 Manager、任务图、执行／验证／交付、候选证据、PR、部分应用、受限与完成历史。
7. 账号、模型配置、用量与预算、空闲回收及实际运行适配验证。

## 10. 已排除、未采用与延后方案

- 用户已否定：第一版 A／B／C 的整体视觉与入口、首次输入直接建正式 Issue、缺少先建多仓项目的入口。不要重做三种大布局让用户再选。
- ADR-0018 已替代：首条正式 Issue 后才准备实例。项目创建即启动实例也不是现行规则。
- 未采用：固定三栏、默认私有与邀请成员的旧建议，以及旧原型随意采用的刷新／轮询数值。当前创建契约的分页／长度／SSE 参数已按持续授权采用，不能混为仍待批。准备期允许保存补充现已采用，旧禁用原型不再代表当前行为。
- 延后：Skill 工程、后续审批能力、直接编辑图。不因参考旧原型恢复人工范围批准、开工审批、自动回滚或自动合并。
- 禁止混同：项目保存＝实例 Ready、消息保存＝Manager 已接收、计划获准＝生效、单仓成功＝整体交付、原型检查＝后端验收。

## 11. 历史设计接续步骤：完成 Manager 创建 MCP 的具体契约

以下步骤保留为后续设计参考，本轮骨架任务不执行。尤其第 1 项不能作为联系旧协作者的授权；当前下一步与未实现范围以页首补充及 HANDOFF 为准。

完整读取上述原文并查看当前原型后，用简短中文报告接手位置，然后直接推进，不以“是否继续”结束：

1. 核实新页面任务与后端任务身份，保存交接原文并通知后端新的回复目标；无需重新询问是否允许通信。核对其是否有 CP15 之后的新决定或文件变化。
2. 从机制稿及创建契约提取共享业务字段、可信上下文和页面结果要求。补齐 repomesh_create_issue／repomesh_get_issue_creation 的 inputSchema、outputSchema、成功及业务错误示例，区分模型可填字段与适配层必须绑定的信息。
3. 与后端对齐来源消息／澄清引用、逻辑创建操作的可信生成和恢复、并发去重、修订冲突、结果未知、失权及删除占位；明确同一消息可有多个独立工作，不能按整条聊天做唯一创建键。保持 REST、MCP 和业务事务的一致含义；传输方式和身份接入交后端协作细化，不凭空宣称支持某种 transport。
4. 用具体场景说明页面结果：创建卡片、Issue 详情、原会话关联、等待核查、受限及恢复。页面建项由 Manager 接手同一 Issue，不再次调用工具重建；来源记录与真实消息分开。
5. 将完整契约保存为本地设计文档／Schema 示例，先约定文件归属，维护唯一字段基线。检查 JSON／Schema 自洽、示例引用和本地链接；没有真实服务就不声称接口验收通过。
6. 完成该契约后继续 Issue 列表／详情与仓库事项状态设计，再把“可选已有会话、创建后自动准备”补入当前本地原型。保留此前版本并做相应浏览器走查，不提前增加生产实现。

本阶段产出应包括：完整工具 Schema 与示例、可信上下文／来源／幂等恢复说明、页面状态映射、后端对齐结果和实际检查记录。不得仅重复工具名称或职责后宣称契约完成。

每次设计变更后同步对应专题、HANDOFF、索引、本 Prompt 和必要原型；发送与接收全文分别落盘。最终报告采用方案、变更文件、检查结果与剩余工作。压缩恢复继续完整读日志并补检查点，不清零轮次，不恢复逐项批准流程。本文是交接，不创建新任务或启动生产工作。
