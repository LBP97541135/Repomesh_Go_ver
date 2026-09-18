# 页面设计原型

2026-09-12审查修订：串联模型页的测试行与[用于项目预览](../../../../prototypes/index.html#model-apply)已共用同一固定快照的测试观察，不修改模型配置或增加通过门槛。[Key保存](../../../../prototypes/index.html#key-save)新增待审的“终结原保存”流程，演示原保存先提交、关闭且未保存、终结响应丢失后仍查同一原操作；关闭弹窗不表示终结。字段唯一来源为[模型候选§3.1](../../../../current/model-settings-browser-api-draft.md)，新增UI／协议仍待采用。本次只修本地原型与文档，生成器再生index，未接真实服务。认证返回会话的候选修订见[认证浏览器稿](../../../../current/authentication-browser-api-draft.md)，首批仍不开放消息发送。

**当前入口：[首批六项完整评审](../../../../prototypes/index.html#review)**。本次六组全部一次交付：[总包与决定编号](../../../../current/first-batch-complete-review.md)。新增[单模型测试](../../../../prototypes/index.html#model-test)、[配置就绪摘要](../../../../prototypes/index.html#source-settings)、[异常恢复场景](../../../../prototypes/index.html#recovery-states)，并连接详情、Key、登录、项目/模型设置。原15份HTML保留，五个嵌入家族同一index；所有新增方案待用户，未实现真实接口。下面是此前逐项展示记录，其“未成稿”等状态以总包为准。

**当前：[API Key保存与原结果查询r1](../../../../prototypes/index.html#key-save)**。保留原密钥／明确替换、完整配置确认、未知核查三项待用户。F01登录恢复已获所问三项UI采用，完整协议仍未完成。见[页面说明](../../../../current/model-key-save-design.md)。

**当前：[登录与重连恢复r2](../../../../prototypes/index.html#auth-recovery)**。沿已采用入口视觉，问题优先／重连未知查询或稍后处理／确认后显式继续三项已采用，其余细化仍待审；[页面与API责任说明](../../../../current/login-recovery-page-design.md)。仅内存原型，F09及认证方向未因“继续下一项”自动采用。

**当前：[首批最小Issue详情概览r2](../../../../prototypes/index.html#issue-overview)**。范围已确认，当前只设计页面和API，开发另行进行。沿旧概览主题第二轮，新片段[issue-overview.prototype.js](../../../../prototypes/issue-overview.prototype.js)由生成器嵌入工作区；三项UI待用户采用，见[说明](../../../../current/issue-overview-minimal-design.md)。

此前收拢阶段： 用户已同意F04三项UI，当前转交[首批开发待做表建议](../current/first-development-todo.md)。本轮只整理文档；已有HTML的待审标签尚未刷新，不据标签推翻当前采用记录。

**F04已采用三项页面安排：[模型用于项目](../../../../prototypes/index.html#model-apply)**，模型设置→具体模型“用于项目”。[新增原型片段](../../../../prototypes/model-project-apply.prototype.js)及[页面提案](../../../../current/model-project-apply-design.md)已获三项UI采用，专用协议未完成。

**F03仓库选择器：[已采用三项页面安排](../../../../prototypes/index.html#repository-picker)**，主界面→项目设置→添加仓库。新增[原型片段](../../../../prototypes/repository-picker.prototype.js)嵌入F02，原15份HTML保持；已选摘要／跨搜索保留、账号与App能力分开、回表单统一保存及未知暂停已获用户同意，见[提案与边界](../../../../current/repository-picker-design.md)。

**先看整体：[完整串联原型](../../../../prototypes/index.html)**。登录／建项 → 工作区 → Issue创建与反馈 → 详情／DAG／Leader／交付，另连接项目配置和模型设置。“全貌与原稿”保留全部15份原稿、采用范围及旧版替代关系。整体串联为待评审r1；其中F02三项页面安排已获用户同意，新增要求是主界面可找到项目设置；详见[串联说明](../../../../current/prototype-walkthrough-design.md)。

串联入口由 `python docs/prototypes/assemble-prototypes.py` 从当前原稿与 `joined-preview-shell.html.template` 生成，原稿保持不变。仅内存演示，刷新重置，真实实现与F01—F15未完状态不变。

当前F02讨论稿：[已保存项目配置编辑](../../../../prototypes/repomesh-project-settings-prototype.html)。继任页面沿已采用深色／左侧导航制作，展示资料、保留旧仓／明确增仓、配置引用和冲突／未知恢复。已采用资料→仓库→配置顺序、显式配置开关及摘要后确认，并要求主界面入口；F02继任后端技术核对仍未开展（0／3）；唯一接口仍RM-API-01 r3，完整恢复与F04精确应用未完成，见[专题F02](../../../../current/project-configuration-design.md)。

新页面设计师先读[2026-09-11新版交接](../current/HANDOFF-PAGE-API-DESIGN.md)：当前采用原型、三处待UI反馈和F01—F15未完表单集中列出；下文保留演进记录。

当前设置讨论：[供应商与模型配置](../../../../prototypes/repomesh-model-provider-prototype.html)。按用户参考图改为左供应商＋右配置/模型列表，模型ID和高级参数按AgentTeams 517caff源码核对，支持逐模型编辑、测试及用于项目。r3技术及落盘已互核；用户“ok可以的”采用当前分栏及模型参数填写方式；旧[连接弹窗r2](repomesh-model-settings-prototype.html)保留。字段来源和未实现协议见[专题](../../../../current/model-connection-settings-design.md)。

当前已采用入口：[登录与项目入口](../../../../prototypes/repomesh-project-entry-prototype.html)。沿两步创建；模拟登录后选择当前已发现仓库，填写资料，缺模型可先保存待配置项目。默认从登录开始，?screen=repositories可直接看选仓。用户回复“正确”，已采用两步创建流程及授权提示；模型连接/API Key设置另行展示讨论，不接真实账号或Key。

当前讨论：[消息目标与澄清](../../../../prototypes/repomesh-message-target-prototype.html)。Manager在歧义消息后给出候选，点击填入带问题引用的可编辑答复；发送和明确目标分开演示，右栏查看范围不替代目标。仅脚本样例；新控件待用户评审。

已采用：[悬浮入口与同页右栏](../../../../prototypes/repomesh-conversation-dock-prototype.html)（RM-UI-ROOM-NAV r2）。常驻悬浮入口随窗口宽度收成图标，点击同页展开右侧DAG/Leader内容，主会话保留；支持关闭/临时扩大。r1固定索引与全页跳转原型保留作演进参考。用户已回复“正确的，进行下一项”采用已展示交互。

当前讨论：[会话右侧导航](repomesh-conversation-navigation-prototype.html)。沿既有会话布局，右栏按关联Issue索引任务DAG和各仓Leader房间；同页跳转返回保留未发送草稿。右栏具体宽度/排布待用户评审；仅内存模拟。上轮DAG布局与图下详情已获用户“是的”采用。

本目录保留三个既有页面设计原型，并保存基于它们的当前讨论稿。前三个文件从 Codex 本地展示目录原样复制，原文件保留；HTML 均内嵌样式与脚本，无外部资源依赖，可直接用浏览器打开。

| 原型 | 页面与交互重点 | 设计记录 |
| --- | --- | --- |
| [草稿与房间](repomesh-rooms-prototype.html) | 深色会话式页面、草稿入口和房间导航；旧草稿绑定规则已被后续设计替代。 | [草稿与房间入口](../current/draft-issue-and-room-entry-design.md) |
| [项目优先](repomesh-project-first.html) | 从没有项目的状态开始，先保存项目；会话与 Issue 拆分前的历史版本。 | [项目先创建入口](../current/project-first-entry-design.md) |
| [会话与独立 Issue](../../../../prototypes/repomesh-conversations-issues.html) | 会话关联多项独立 Issue、页面建项、详情与房间导航。 | [会话与独立 Issue 专题](../../../../current/conversation-issue-separation-design.md) |
| [创建 Issue 弹窗（当前讨论稿）](../../../../prototypes/repomesh-issue-modal-prototype.html) | 根据用户决定，从既有列表打开居中弹窗；沿原视觉，当前大小与字段排布已获用户“正确”采用。页面负责人维护。 | [弹窗决定 RM-UI-01 r3](../../../../current/conversation-issue-separation-design.md) |

这些页面使用内存模拟数据，属于设计参考，不代表实际业务、接口或运行能力已实现。现行规则和后续设计进展以对应专题及[创建契约](../../../../current/issue-page-create-api-contract.md)为准。

## 原始文件位置

当前图设计：[只读任务 DAG](../../../../prototypes/repomesh-issue-dag-prototype.html)，按ADR-0007／0015补依赖连线、并行汇合和节点详情；用户已回复“是的”采用DAG布局与图下节点详情。SVG内存示例不代表React Flow工程或真实图协议已实现。

当前页面讨论：[新建 Issue 详情](../../../../prototypes/repomesh-issue-detail-prototype.html)，沿原概览结构，分开涉及仓库、可读业务会话与实时房间入口；当前待用户反馈。其他页签保留历史示意，不代表本轮全详情验收。

当前下一项：[创建结果反馈](../../../../prototypes/repomesh-issue-submit-prototype.html)，展示提交中、结果待确认及成功结果。URL参数scenario=unknown（默认）或scenario=success；为单操作内存模拟，查询取同一结果。用户已回复“确定”采用本次文案／按钮安排，跨刷新与真实服务恢复未实现。

后续控件讨论稿：[创建 Issue：关联会话](../../../../prototypes/repomesh-issue-conversation-prototype.html)，从已采用的弹窗派生；展示默认新建与选择已有会话，用户已回复“可以的”采用控件位置与展开方式。仅内存模拟，原基线文件保留。

- 草稿与房间：`C:/Users/18092/.codex/visualizations/2026/09/09/01a083e6-3395-7581-833d-cec0b19dd9af/repomesh-rooms-prototype.html`
- 项目优先：`C:/Users/18092/.codex/visualizations/2026/09/09/01a08562-2a37-7371-8a6e-050e47013223/repomesh-project-first.html`
- 会话与独立 Issue：`C:/Users/18092/.codex/visualizations/2026/09/09/01a08562-2a37-7371-8a6e-050e47013223/repomesh-conversations-issues.html`

原展示目录中的其他正文源文件、截图及修改前副本未纳入本次复制；历史日志中的原始路径保留用于溯源。

[文档总导航](../README.md) · [现行文档索引](../current/README.md) · [更早的产品原型](../../../legacy-product-design/产品原型/README.md)
