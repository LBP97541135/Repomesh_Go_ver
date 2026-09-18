# 基础工程阶段：页面、接口与后端接手核对

记录：2026-09-10。本记录由页面／接口／后端文档负责人维护，说明本轮阅读覆盖和文档同步；不是主要实现者之外的代码独立复核，也不是业务或 AgentTeams 集成验收。工程结构、启动、配置和构建以[基础工程开发说明](../../../../current/development-scaffold.md)为统一入口，最终状态见 [HANDOFF](HANDOFF.md)。

## 授权与文档归属

用户本轮明确允许基础代码、工程配置和必要依赖，替代旧交接在本轮任务范围内的“仅设计、不编写代码”。保留既有产品决定、权限边界、未定协议和暂缓模块。历史通信指令不构成本轮授权；没有恢复旧任务、联系旧协作者或追加旧通信日志。

本角色仅编辑本记录，以及 `NEXT-SESSION-PROMPT.md`、`NEXT-BACKEND-SESSION-PROMPT.md`、`conversation-issue-separation-design.md`、`issue-page-create-api-contract.md`、`draft-conversation-backend-design.md`。HANDOFF、工程说明和代码由主 Agent 维护；ADR 与验证材料由其他当前团队成员核对。现有未提交文件按工作树读取、定点增补，没有重建或回退到 Git HEAD。

## 逐文件阅读覆盖

以下 24 份材料的正文已覆盖；长文件分段补读截断区域。两轮验证的报告、evidence 和 scripts 由团队中的 AgentTeams 负责人完整负责，本记录不把其工作冒称本角色独立验证。

| 文件 | 本轮用途 |
| --- | --- |
| [docs/README.md](../README.md) | 总入口、设计与实现完成度区分。 |
| [HANDOFF.md](HANDOFF.md) | 当前约束、阶段、证据与后续工作。 |
| [current/README.md](README.md) | 现行专题与历史入口范围。 |
| [ADR 索引](../../../../adr/README.md) | 0010—0020 的定点演进关系。 |
| [CONTEXT.md](../../../../../CONTEXT.md) | Project、Conversation、Issue、仓库事项、room/session 及执行归属。 |
| [ADR-0020](../../../../adr/0020-python-repository-analysis-plugin.md) | 三类 Go 进程之外的受控 Python 扩展。 |
| [NEXT-SESSION-PROMPT.md](NEXT-SESSION-PROMPT.md) | 页面接手、41 项历史清单、原型和通信来源。 |
| [NEXT-BACKEND-SESSION-PROMPT.md](NEXT-BACKEND-SESSION-PROMPT.md) | 后端接手、41 项历史清单、已有决定与缺口。 |
| [conversation-issue-separation-design.md](../../../../current/conversation-issue-separation-design.md) | 独立 Issue 页面与真实房间导航。 |
| [conversation-message-target-design.md](../../../../current/conversation-message-target-design.md) | 明确自然语言目标、澄清和晚到结果归属。 |
| [manager-create-issue-tool-design.md](../../../../current/manager-create-issue-tool-design.md) | 双入口统一命令、可信来源、MCP 未完成范围。 |
| [issue-page-create-api-contract.md](../../../../current/issue-page-create-api-contract.md) | 现行 REST、Issue SSE、幂等和来源设计基线。 |
| [draft-conversation-backend-design.md](../../../../current/draft-conversation-backend-design.md) | 事务、消息保存、运行准备及恢复职责。 |
| [page-interface-prototype.md](page-interface-prototype.md) | 已否定 A／B／C 布局的历史。 |
| [draft-issue-and-room-entry-design.md](draft-issue-and-room-entry-design.md) | 旧草稿绑定已替代，深色视觉及只读 Leader 边界仍有效。 |
| [project-first-entry-design.md](project-first-entry-design.md) | 项目先保存、准备期间补充的历史与保留保障。 |
| [project-configuration-design.md](../../../../current/project-configuration-design.md) | 配置、账号与 App 能力、部分失权。 |
| [issue-creation-repository-analysis.md](../../../../current/issue-creation-repository-analysis.md) | 建项前分析、建议应用、固定输入与来源。 |
| [旧分析插件调查](../../../../research/legacy-analysis-plugin-feasibility-2026-09-10.md) | 旧源码事实、可提取资产、未被整体采用的建议。 |
| [document-review-2026-09-09.md](document-review-2026-09-09.md) | 历史正文与现行决定漂移的修订方式。 |
| [design-delegation.md](../../../../current/design-delegation.md) | 持续设计授权原范围、P1—P4 采用记录。 |
| [design-communication.md](design-communication.md) | 历史协作身份、轮次和原文保存范围。 |
| [design-communication-page-log.md](design-communication-page-log.md) | 阅读时 1—714 行全文；原提案、用户更正、采用与最后交接。 |
| [design-communication-backend-log.md](design-communication-backend-log.md) | 阅读时共 840 行、312 段；162 段与已读页面日志逐段精确文本相同，其余 150 段完整读取，覆盖全部原文内容。 |

日志核对只比较规范化换行后的完整段落，不以摘要代替未读的消息。没有更改日志字节、确认历史或轮次。原型源文件、浏览器截图和旧 Python 库源码不在本角色本轮直接复验范围；本轮新建静态骨架不迁移原型，也不提取 Python 算法。

## 工程影响与完成度表达

| 已采用或已知内容 | 本轮处理 |
| --- | --- |
| 三类 Go 进程来自同一工程、配套发布 | 保留三个 `cmd` 入口和共享版本；进程启动不视为业务连通。 |
| 页面存在独立会话、Issue 与房间关系 | 只展示工程骨架状态；没有产品业务表单、示例项目或伪造成功。 |
| 创建 REST／Issue SSE 已采用 | 业务路由未注册；骨架 404 不代表设计中的权限隐藏或操作查询语义。 |
| 消息保存、投递、处理、就绪与执行各自取证 | `/healthz` 200 仅表示 Web 响应；`/readyz` 503 明示业务未实现。 |
| 后台协调与受限主机执行有不同权限职责 | 默认报未实现并非零退出，不监听执行协议、不消费任务、不接 Docker socket。 |
| Graph 进程内、仓内 DAG 复用上游 | 保留设计，不建 Graph 服务或另写通用 DAG 引擎。 |
| Skill 暂缓 | 不新增目录、接口、开关或页面。 |
| ADR-0020 受控 Python 分析 | 只说明边界，不新增 Python 包、接口或空插件框架。 |

旧项目调查锁定 `9d546e331dc9ca018dac3332b1f2ac1981bc009f`，属于静态来源；其“两个逻辑插件”、历史向量检索及 pgvector 建议没有被整体采用。当前只采用建项前仓库分析，且按钮、Go 持久作业和 Python 包都未实现。旧 `project_id`、进程内分析任务和计划／审批前置条件不能直接带入新长期项目模型。

两份角色 Prompt 在页首、旧角色／通信及执行步骤所在位置标出历史适用范围，保留原阅读清单。页面专题区分新骨架与既有内存原型；接口契约保留字段与业务状态含义；后端专项区分进程骨架与待实现业务。新授权不倒填为旧授权，也不将文档 `accepted` 或任何上游局部成功升级为 RepoMesh 业务完成。

## 未决与后续

完整 MCP input/output Schema、可信来源句段与逻辑操作承接、消息／更正协议、仓库事项粒度和房间拓扑、数据库表及恢复算法仍需继续设计。本轮不为它们预铺接口或抽象，也不因这些缺口阻止最小工程建立。

后续按新的具体任务选择完整场景，结合当前权限、事务与恢复保障逐段实施；涉及 AgentTeams 时先回到两轮验证的未完成项与前提，不用本轮构建通过替代旧验证。实施 Python 分析时应固定快照、限制只读输入和进程资源、明确部分结果与失败，并维持分析失败可手动创建的设计；这些均属于后续实现验收。
