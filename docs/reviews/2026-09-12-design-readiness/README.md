# RepoMesh 设计一致性与开发就绪审查

审查日期：2026-09-12。基线为 `D:/Project4work/Repomesh_Go_ver` 的实际工作树，包含既有未提交和未跟踪设计稿。根仓库 HEAD 为 `4516805a276d65eb79490a12783654f3d7b1c677`；该提交号单独不能标识本次审查内容，逐文件 SHA-256 见 [source-baseline.json](source-baseline.json)。

**结论：可以开始已收口的核心管理切片；首批六包还不能作为无缺口的整体开发基线；真实 Manager 和多仓执行系统尚需收口关键协议。** 主要架构方向一致，无需推翻重做。首批应先解决 Key 保存请求丢失后的永久未知状态，以及供未来接续的 Issue 配置归属记录。运行部分还有模型配置消费、可信消息与工具、资源生命周期和换图恢复等未决边界。

本次完成审查并提出修订建议，不修改原设计的采用状态，也没有开始业务实现。新增方案仍待用户采用是基线事实；这与方案本身是否足够完整分别判断，不能只用“尚未批准”回答开发就绪问题。

## 范围与方法

主审负责决策来源、历史替代、跨层复核和结构检查。三个子 agent 分别审查页面语义、API/持久化、系统架构；用户追加 AgentTeams 对照后，API 子 agent 补读调研，并另建第四个子 agent 独立核对上游与后端。

| 范围 | 文件数 | 审查方式与产物 |
| --- | ---: | --- |
| docs/adr | 21 | 全部决定及演进关系，[系统报告](system-review.md)。 |
| docs/current | 63 | 现行页面、契约、持久化和系统专题按分工审读；历史日志按完整主题索引与关键采用原文追溯。[页面报告](page-review.md)、[API/后端报告](api-backend-review.md)。 |
| docs/prototypes | 26 | 原稿、JS、模板和生成器；共用区块比对与差异审读，核对当前串联行为。[页面报告](page-review.md)。 |
| docs/agentteams-survey-2026-09-07 | 3 | 两份 Markdown 全文及 HTML 完整正文；结合后续版本和现有证据。[独立上游对照](upstream-crosscheck.md)。 |

共 113 个文件。具体阅读方式和归属见 [coverage.md](coverage.md)。没有把历史日志的全文件扫描声称为逐条重审全部对话，也没有把历史证据索引中的实验继承为本次实测。

审查按章节的明确采用范围和替代关系判断，不按文件日期、ADR 编号或整篇 accepted/proposed 标签推断。每项结论区分现行语义问题、候选设计缺陷、原型缺陷和已知的后续开发前置。P1 表示会阻塞其所列开发切片或关键恢复保证，不表示线上已经发生安全事故；P2 表示局部语义、导航或适用范围需要补齐。

## 需要修正的具体问题

| 编号 | 级别与类型 | 具体问题 | 影响范围 |
| --- | --- | --- | --- |
| R01 | P1，候选恢复协议缺陷 | Key 发出即清，未知只能查询；请求未到服务器时一直 404，没有可终结原操作的路径。 | 首批模型配置及管理闭环的失败恢复。 |
| R02 | P2，当前串联原型缺陷 | 同一模型快照测试通过后，模型列表显示通过，但“用于项目”仍显示未测试。 | 模型测试与应用页面的一致性。 |
| R03 | P2，认证导航契约缺口 | Destination 不能表示已有会话和消息提交恢复目标，重登后原页恢复或降级行为未明确。 | 首批只读会话导航；消息发送批次的原操作恢复。 |
| R04 | P1，跨模块配置归属缺口 | 承诺新模型不影响在途 Issue，却未明确 Issue 固定哪版配置、何时绑定及如何恢复。 | 首批可被后续接续的 Issue/待办持久记录，以及后续运行。 |
| R05 | P2，首期适用条款缺失 | 验证记录要求 Skill 版本，但 Skill 工程暂缓；验证专题未就地说明未使用 Skill 时记录什么。 | 正式验证记录与验收器设计；不阻塞管理页。 |

### R01：Key 保存的永久未知状态

依据：[模型浏览器稿](../../current/model-settings-browser-api-draft.md)第 65、75、131 行；[模型内部稿](../../current/backend-model-operations-draft.md)第 20、78、80 行；[总包 D06/P7](../../current/first-batch-complete-review.md)第 36、76 行。主审、页面和 API 子 agent 分别核对后确认同一问题。

用户首次保存供应商和 Key，请求在到达服务器前断网。页面已清掉 Key，服务端没有 ModelOperation。以后 GET 原 saveId 只能返回 404，而页面既不能原输入重试，也不能换键重新保存。已有 rejected 设计只能终结服务端已收到并取得唯一槽位的操作，无法处理从未持久登记的请求。服务器取得槽位之前发生可恢复错误也可能进入这条路径。

建议补受当前主体鉴权、与保存争用同一唯一槽位的“终结原操作”协议。已经提交则返回原结果；尚未提交则原子登记禁止迟到保存的终结占位，之后允许用户明确创建新操作。也可另选受保护暂存和原键重试方案。两者都需要明确设计，等待时间和关闭弹窗不能代替终结证据。

修订验收至少覆盖：请求未到服务器、取得槽位前失败、终结与迟到保存并发、终结回执丢失、已提交后再终结。不能只验证“服务器成功保存但响应丢失”。完整分析见 [AB-01](api-backend-review.md)。

### R02：测试观察在两个页面间不一致

依据：[测试原型](../../prototypes/model-test.prototype.js)第 3—5、14、17 行把观察保存在 testRecords，并更新模型行 DOM；[应用原型](../../prototypes/model-project-apply.prototype.js)第 11、15 行却复制 draft.models 中的旧 test 字段。[F04 已采用说明](../../current/model-project-apply-design.md)第 13 行要求展示该固定快照的测试观察。

页面子 agent 用原片段事件处理器复现“测试确认、模拟通过、用于项目”：模型行是“本次测试通过”，应用弹窗仍是“未测试”。主审另核两个数据读取位置，确认不是单纯缺少真实后端，而是同一内存原型给出互相不一致的观察。

建议两个页面按同一供应商、模型、不可变快照和测试身份查询同一观察源；更新供应商快照后继续隔离旧结果。这里不建议把测试通过新增为模型应用的必要门槛。

### R03：登录返回目标覆盖不足

依据：[认证 Destination](../../current/authentication-browser-api-draft.md)第 33 行只含 home/project/issue/operation，操作类型未含消息提交；[恢复总表](../../current/first-batch-recovery-design.md)第 59—65 行纳入已有会话只读入口；[消息恢复路由](../../current/conversation-message-clarification-api-contract.md)第 149 行包含 projectId、conversationId、submissionId。

需要明确首批从会话重登后返回原会话，还是降级回项目。如果承诺原页恢复，联合类型和固定路由表应能表达会话；消息发送批次再补消息操作目标。目标页仍重核权限，不能改成任意 returnUrl。此问题不阻塞项目/Issue 管理数据层，消息发送本身属于后续批次。

### R04：Issue 与运行配置缺少固定关联

依据：[模型设置](../../current/model-connection-settings-design.md)第 70 行要求在途 Issue 不随应用切换；[内部模型稿](../../current/backend-model-operations-draft.md)第 43、148 行要求保留旧 profile/secret。[首批持久化](../../current/backend-first-batch-persistence.md)第 20 行有 ProjectConfigRevision，第 22、27、70 行的 Issue、创建操作和创建事务没有明确保存相应配置关联。第 154 行的 creationContextRevision 只用于创建条件比较，不能据此推断可还原历史配置。

项目在 C1 下建立 Issue A，随后应用 C2 并建立 B，后台再处理 A。只读当前项目配置会把 A 换成 C2；只保存不透明创建条件令牌又不足以选择 C1。还需明确同一个长期 Manager 处理 A、B，以及尚无 Issue 的讨论时实际使用哪份模型配置。

建议先决定配置绑定单位与时点，并在对应事务保存可解析的不可变引用；另定义旧 Key 失效后如何显式修复在途工作。固定版本与当前权限/秘密可用性仍须分开。首批保留待办供后续版本接续，因此应在首批迁移评审时处理这条关系，避免将来靠时间戳猜测旧配置。具体绑定时点尚未由本次审查代为决定。

CRUD 代码可以并行开发；但在接收供未来版本接续的真实 Issue/待办前，应完成绑定语义和必要持久记录。消费者实现可以后置，关联事实不能等运行接入时再猜测。这是合并 [AB-02](api-backend-review.md)、[SYS-01](system-review.md)和[独立对照 U1](upstream-crosscheck.md)后的首批边界。

### R05：验证记录中的 Skill 适用范围

依据：[验证专题](../../current/verification-node-design.md)第 22、88、111 行与 [ADR-0009](../../adr/0009-skill-engineering-deferred.md)第 11、51 行；[架构](../../current/architecture-design-v1.md)第 409 行已经说明可记录实际工作说明及来源。

现有后续规则能解释 Skill 暂缓，因此不判为根本性架构矛盾。问题是验证专题的最小证据表容易被直接实现为必填 Skill 版本。建议在原表说明：未使用 Skill 时记录实际工作说明、脚本和内容版本；使用了 Skill 才记录并核验其版本。不能为通过校验伪填版本，也无需恢复暂缓模块。详见 [SYS-05](system-review.md)。

## AgentTeams 对照后的执行开发门槛

以下主要是文档已经承认的未完成设计。本次结合上游调研和既有失败证据确认其影响，不把它们重复包装成新漏洞。

| 门槛 | 具体尚缺什么 | 为什么影响实现 | 证据入口 |
| --- | --- | --- | --- |
| 长期 Manager 的配置消费 | 明确旧新 Issue、普通讨论及目标解释前的消息分别用哪份配置，以及如何实际传递和恢复。 | 517caff 的 Manager 配置按整个 Manager 生效，模型/provider 变化会触发容器重建；直接 PUT Manager 不能提供按 Issue 固定版本的保证。 | [新版 CFG-05 实测](../../../validation/agentteams-2026-09-10/reports/manager-config-live.md)、[独立对照 U2](upstream-crosscheck.md)。 |
| 可信消息与工具 | 首条会话消息、runtime/source/actor 绑定、输出与请求关联、完整 Manager MCP schema。 | 房间存储不等于消息已处理，thread 不自动提供 Issue 或澄清归属。 | [消息内部设计](../../current/backend-message-clarification-design.md)、[双入口工具](../../current/manager-create-issue-tool-design.md)、[API 对照](api-backend-review.md)。 |
| 正式上游状态的写入责任 | plan_dag/delegate/submit/accept/cancel 与文件同步分别由谁执行、持何凭据、在哪核代次；选择唯一写方或统一条件写。 | REST 层拒绝不能阻止 shared 存储写入和原生工具旁路，陈旧 MCP 上传可以覆盖 pause。 | [Graph 第 122 行](../../current/graph-loop-design.md)、[SYS-02](system-review.md)。 |
| runtime 与 Attempt 生命周期 | Controller runtime、任务进程/容器、clone、网络/卷各自唯一管理者，停止证据与失去写能力的核验。 | 数据库释放名额不表示旧执行停止，填写 resources 不证明实际 Docker/cgroup 限制生效。 | [架构第 83、194、290 行](../../current/architecture-design-v1.md)、[SYS-03](system-review.md)。 |
| 第二轮及多目标换图 | 选择受控 resume→replan 或 paused replan 补丁，定义 submitted 失败收尾、Project 最终 complete、部分应用恢复。 | 当前上游 paused/in_progress/submitted 等条件限制 replan，不能直接把自然语言 Loop 规则翻译为 API 调用。 | [Graph 第 110—122 行](../../current/graph-loop-design.md)、[SYS-04](system-review.md)。 |

09-07 调研固定 `eeaab64391ccaec9118e84977f538aefd40720d6`，是静态调查。当前来源记录固定 `517caff9280242a00a4d4c06365352b9e41659c6`。后续已增加 Task inspection、Mermaid/history 读取和 Manager modelProvider 清空语义，不能继续沿用旧版本的相关缺口；这些变化也不能证明 TeamHarness 写入控制、消息恢复、取消和资源隔离已修复。[Controller 差异报告](../../../validation/agentteams-2026-09-10/reports/controller-delta.md)及[限定证据索引](../../current/scaffold-agentteams-evidence.md)保留了这个区别。本次没有联网更新上游或重跑这些实验。

## 可以开始哪些开发

| 层次 | 判断 | 开始或集成前的条件 |
| --- | --- | --- |
| 工程基础和模块组织 | 可以，且最小骨架已存在。 | 保持三进程权限边界和按实际用例增加模块。 |
| 项目/Issue 核心持久化、列表与查询 | 可以按已采用契约开始。 | 真实关系、内容范围、三类幂等操作、事件与待办同事务；承接未来执行的 Issue/待办须先满足 R04 的归属记录要求；权限适配不能用模拟成功代替验收。 |
| 已采用页面的静态结构和交互 | 可以开始对应范围。 | 以采用清单和唯一 API 为准；修正 R02，不从历史样例冻结新枚举。 |
| 首批六包的真实管理闭环 | 尚未达到整体无缺口基线。 | 修复 R01，落实 R04 的首批记录要求，明确 R03，采用认证/模型/来源/恢复具体方案；准备真实 App、秘密存储及执行/预算来源。F09 和 Key 新 UI 的采用范围仍待确认。 |
| 真实会话和 Manager 接手 | 尚需先补协议。 | 配置归属、首条消息、可信运行上下文、投递和输出回传闭合。 |
| Worker、跨仓 Graph/Loop、独立验证和交付 | 尚需先补关键运行设计。 | 上述控制写入、生命周期、换图状态机及实际固定组合验收先收口。 |

建议后续按三个可验收单元推进：先修首批恢复缺陷并统一采用范围，形成管理闭环；再做一个真实会话/Manager 的受控往返；最后做单仓单轮、单仓两轮、双仓部分失败的执行闭环。无需先完成全部 F01—F15，也无需为了开工重写 Go DAG 引擎或恢复 Skill 工程。

“可开始”指设计成熟度，不表示本次审查自动变成开发指令。首次消息/执行处理器尚未接入时，第一批仍应保存原契约要求的待办，以真实的未接入状态展示，不能丢弃后续责任或伪造 preparing/ready。

## 检查结果和限制

- 113 个源文件均成功读取并记录 SHA-256；审查过程中未改动这些源文件。
- 结构脚本核对 1,210 个 Markdown 本地文件链接，未发现缺失；29 个 JSON 围栏示例均可解析。这个检查不验证 JSON 业务 schema，也不验证网页锚点或远端 URL 可达性。
- 原型的生成一致性、脚本语法及事件处理复现范围详见页面报告。DOM 替身触发原事件处理不等于浏览器完整走查，更不等于真实 API 验收。
- 历史实测用于判断既有缺口，没有把旧版结果冒称新版重验，也没有把接口读取成功当执行隔离或业务验收。
- 系统子 agent 对主报告作了第二遍只读复核，主审据此收紧 R04 的首批数据接收条件，未将运行消费者后置等同于配置归属也可后补。
- 未运行 Go/npm 构建、数据库故障注入、OAuth、真实 Key/模型请求或 AgentTeams 服务；本次没有产品代码修改。

检查工具和原始输出：[audit_documents.py](audit_documents.py)、[structural-checks.json](structural-checks.json)、[coverage.md](coverage.md)。详细分工报告：[页面](page-review.md)、[API/后端](api-backend-review.md)、[系统](system-review.md)、[上游独立对照](upstream-crosscheck.md)。
