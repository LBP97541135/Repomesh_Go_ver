# AgentTeams 独立交叉复核

复核日期：2026-09-12。范围为后端设计与锁定上游的衔接，不替代页面、API、系统主审。已完整阅读 `docs/agentteams-survey-2026-09-07/` 的两份 Markdown 和 HTML 正文，并定点核对架构、Graph、会话、消息澄清、首批持久化和模型操作设计。

结论：上游提供可复用的组件和有限实测依据，尚不足以直接承接 RepoMesh 的完整执行语义。首批管理可以拆分开发，但 Issue 创建前应补齐不可变配置绑定。真实对话还需要配置消费及可信消息适配；完整执行还需要封闭写入旁路、运行代次和实际停止协议。后三项已有文档明确列为未完成，不能当作本次新发现的已实现代码缺陷。

## 版本与证据边界

- 09-07 两份调研及 HTML 固定 `eeaab64391ccaec9118e84977f538aefd40720d6`，当时仅静态阅读。调查中“每 Issue 独占会话”“先准备实例后接收 Issue”“单仓直达 Leader”等是历史设计，不能继续作为产品约束；现行关系由 ADR-0019 等后续决定和当前专题确定。
- 本次只读 `git rev-parse HEAD` 确认 `third_party/AgentTeams` 为 `517caff9280242a00a4d4c06365352b9e41659c6`，与来源记录一致。已读上游根 AGENTS.md，未修改或拉取上游。
- [controller-delta.md](../../../validation/agentteams-2026-09-10/reports/controller-delta.md) 第 5、26、33–35 行说明新版没有改变相关 DAG、Project 写路径、TeamHarness MCP 和认证缓存。旧版实测仍保留旧版及其具体运行路径标签；“源码未变”不等于新版重新完成全部实测。
- 已读新版双在线读取矩阵、升级冒烟、Manager 配置投影及模型 smoke。它们分别证明新增读取契约、限定写入行为、配置加载、一次默认模型推理。没有把通过次数相加为 RepoMesh 验收场景数。
- 本次没有启动服务、重跑历史实验、读取 private 凭据或执行产品测试。新增产物仅本报告。

## 可复用能力与产品责任

| 路径 | 可以复用及已有证据 | RepoMesh 必须补齐 | 主要影响阶段 |
| --- | --- | --- | --- |
| Controller REST | Manager/Team/Worker 资源、生命周期、工作流读取和有限干预；新版 c/d 对新增读取的实际身份及 Team 消歧已有矩阵。 | 固定实例与版本，映射原始状态，写后读回，接受与 Ready 分开。REST 没有通用“按 Issue 启动 coding task”端点。 | 二批准备；执行读取与干预 |
| TeamHarness stdio MCP | `projectflow`、`taskflow`、房间与材料同步已有实现；不是远程 HTTP 任务服务。 | 确定实际工具桥接位置、可信调用身份、稳定操作、所有控制写方的统一约束。 | 完整执行 |
| Matrix | 房间、事件、发送事务与同步；新版 Manager 默认通道有一次真实推理，旧版 custom Worker 的 reply/thread 有实测。 | 业务消息与外部事件、实际模型输入、可信控制槽位、出站结果及运行代次的持久映射。不能从房间成员或引用关系推出业务归属。 | 二批真实对话 |
| MinIO/OSS | 上游 Project/Task、材料、快照；新版单对象条件 PUT 有限定实测。 | RepoMesh 仍保存正式业务事实；明确控制元数据唯一写方或所有写方共同的 CAS，隔离提交材料与已采纳状态。 | 完整执行 |
| Manager 模型配置 | 单 Manager 的 `model`、`modelProvider`、`runtime`；新版配置改变可触发重建并进入新进程内存。 | 不可变 profile/SecretVersion 如何映射到长期共享 Manager 的各次调用；多版本并存及无 Issue 对话的策略。 | 首批保存绑定；二批运行消费 |
| 取消与资源 | 原生取消元数据、生命周期 API 是观察和控制工具。 | 真正中止进程或撤销旧写能力、拒绝旧代次、迟到结果归档、核查后释放容量；不能将 cancelled 或租约到期当作执行停止。 | 完整执行 |

上述接口分层的旧版完整依据在 [API/CLI 调研](../../agentteams-survey-2026-09-07/agentteams-api-cli-survey-2026-09-07.md) 第 303–338、342–410、462–505 行。现行架构 [§6](../../current/architecture-design-v1.md) 第 164–210 行已经正确区分 REST、Matrix 和运行时桥接，没有发现它将原生 MCP 虚构成 REST 的新冲突。

## U1：Issue 对不可变配置的绑定尚未成为创建不变量

性质：新增交叉设计缺口，建议 P1。影响首批 Issue 创建的数据设计；不妨碍独立列表、项目资料等已收口部分先实施。

模型设置的现行技术目标要求“其他项目与在途 Issue 不随应用切换”。模型待审稿进一步明确旧 Issue 固定原 profile/SecretVersion，应用仅新建 ProjectConfigRevision。首批持久化却只为 ProjectConfigRevision 定义不可变版本，Issue 记录及创建事务没有明确保存当次配置引用、采用时点或等效不可变关系。

具体交错：项目配置 C1 下创建 Issue A，A 仍待接入；项目随后应用 C2；后台重启后接手 A。如果接手只读项目当前配置，A 使用 C2；如果准备期或首次执行才绑定，也与“旧 Issue 不自动切换”的宽泛表述不一致。创建请求里的 `expectedCreationContextRevision` 是校验依据，不自动构成到配置版本的可恢复外键。

证据：

- [model-connection-settings-design.md](../../current/model-connection-settings-design.md) 第 70–74 行要求在途 Issue 不切换，且区别完整 PATCH 与专用应用。
- [backend-model-operations-draft.md](../../current/backend-model-operations-draft.md) 第 37、43、144–148 行只有 ModelProfileVersion 和新 ProjectConfigRevision 的规则；该稿内部细节仍待采用。
- [backend-first-batch-persistence.md](../../current/backend-first-batch-persistence.md) 第 20–28 行的关系、66–71 行的创建事务、107–109 行的配置固定规则没有明确 Issue 配置绑定。第 154、160 行也没有规定 creationContextRevision 到旧配置的持久映射。

收口要求：明确 A 在哪一笔事务绑定哪个不可变配置，记录同项目归属和保留约束，并规定停用版本的继续核权及受阻行为。若选择按首次运行绑定，应同步修改“不影响既有 Issue”的范围，不能让实现者自行选择。验收至少覆盖“建 A → 换 C2 → 建 B → 重启领取”，证明 A/B 的配置引用、SecretVersion 和运行责任按所采用规则稳定。

## U2：长期共享 Manager 尚无承接 Issue 固定模型版本的运行方案

性质：运行映射设计缺口，建议 P1。与 U1 分开处理；主要阻塞二批真实对话及随后执行，并不要求首批模型保存就启动 AgentTeams。

架构保留长期 Manager 并允许串行协调多个 Issue；应用模型又承诺不影响在途 Issue。直接 PUT 原生 Manager 只改变该 Manager 的统一配置，不能证明 A/C1 与 B/C2 的后续调用各自保持版本。即使串行执行，也要明确每次调用采用哪一配置、如何读回及怎样恢复，不能从“没有并发”推出配置隔离。

锁定源码与实际证据：

- [architecture-design-v1.md](../../current/architecture-design-v1.md) 第 122–132、194–196 行保留长期共享 Manager，并提出同一协调者可串行调用。
- 上游 [ManagerSpec](../../../third_party/AgentTeams/agentteams-controller/api/v1beta1/types.go) 第 636–649 行按 Manager 定义 model/modelProvider/runtime，没有 Issue 配置身份。
- 上游 [manager_reconcile_container.go](../../../third_party/AgentTeams/agentteams-controller/internal/controller/manager_reconcile_container.go) 第 58–73、274–299 行对包含模型字段的 spec 计算 hash，漂移后删除并重建容器。[deployer.go](../../../third_party/AgentTeams/agentteams-controller/internal/service/deployer.go) 第 1272–1304 行用 Manager spec 生成并写入配置。
- 新版 [manager-config-live.md](../../../validation/agentteams-2026-09-10/reports/manager-config-live.md) 第 44–59 行实际观察到切模型/provider 重建 Manager，global/effective 一并改变；不存在 provider 时 PUT 200 但旧配置仍在，直到后续恢复。
- [backend-model-operations-draft.md](../../current/backend-model-operations-draft.md) 第 145、173 行明确本次应用不重启 Agent，完整运行消费者和 Controller/Manager 映射未启用。这是已知未完成范围，不能报告为现有应用代码必然重启 Manager。

收口要求：先说明固定 profile 适用于 Worker 执行、Manager 对该 Issue 的协调，还是两者；再选择并验证符合长期 Manager 身份约束的运行接法。还要说明无 Issue 讨论，以及尚未解释出目标的普通消息和澄清答复采用哪一配置。当前受控消息上下文表没有配置版本绑定，不能让消息解释后才发现该次模型已用错版本。验收需交错旧新 Issue、切配置、运行重启、SecretVersion 停用，取得每次实际调用的版本证据。

## U3：可信输入、出站归属与消息控制授权仍停在 Adapter 依赖

性质：已知 B05/B06 阻塞的独立确认，建议 P1。阻塞二批“真实 Manager 对话及受控澄清”闭环；首批可保存消息/来源及未接入责任，不得展示处理成功。

[backend-message-clarification-design.md](../../current/backend-message-clarification-design.md) 第 50–65 行要求可信适配器绑定真实 instance/session/conversation、实际输入和调用代次；第 191–208 行将实际投递、输入证据、外部幂等和后续工作留给 B06。逻辑槽位、复合外键及服务端工具参数已有设计，不能据此宣称输入已被该次模型读取，或任意 Matrix 回复属于该控制操作。

旧版 [runtime-reply-thread-live.md](../../../validation/agentteams-2026-09-09/reports/runtime-reply-thread-live.md) 第 5、41–70 行实际证明 custom Worker 的 reply/thread 入站得到回复，但出站是新的主时间线占位及自身 `m.replace`，两者共享 room session。此结论不直接推广到 Manager 的 builtin 通道。新版 [manager-model-smoke-new.md](../../../validation/agentteams-2026-09-10/reports/manager-model-smoke-new.md) 第 13–25、31–35 行证明一次可归属 Matrix 推理，但没有工具调用，也没有 RepoMesh logicalRequest/commandSlot。

收口要求：冻结真实输入的确认方式、普通文本输出与正式控制回写的不同来源证明、异步输出到原 request 的绑定、进程换代及旧输出处理。需验证两项 Issue 交错、澄清被替代后旧回复晚到、断线补拉及换 runtime；不能仅凭一条 `m.in_reply_to` 或模型自报 Issue ID 认定成功。

## U4：控制存储与实际执行缺少统一强制边界，不能靠原生 REST 补齐

性质：已知 B07 及执行验证阻塞的独立确认，建议 P1。影响完整派工、改图、结果采纳与取消，不应扩大成首批管理或只读界面的全部前置。

- 旧版 [worker-auth-live.md](../../../validation/agentteams-2026-09-09/reports/worker-auth-live.md) 第 5、26–41 行证明同一真实 Worker 的 REST 被拒绝，同时 shared 元数据可写，工具可用 `role=leader` 覆盖。当前锁定源码 [server.py](../../../third_party/AgentTeams/plugins/teamharness/mcp/server.py) 第 3628–3632 行仍优先采用调用参数中的 role；这只是工具内角色检查，不是可信主体。
- 旧版 [model-task-cancel-live.md](../../../validation/agentteams-2026-09-09/reports/model-task-cancel-live.md) 第 5、27–42 行证明同一真实委派 Task 已 cancelled 后模型工具进程继续写文件。取消元数据不等于进程停止；assigned 也不证明尚未实际执行。
- 新版 [controller-upgrade-smoke.md](../../../validation/agentteams-2026-09-10/reports/controller-upgrade-smoke.md) 第 14–23 行再次实测 paused replan 409；它只覆盖元数据夹具及单对象条件写，没有重跑模型进程取消。
- [graph-loop-design.md](../../current/graph-loop-design.md) 第 110–122 行已将受控 resume/replan 与 paused 补丁列为未选方案，并要求所有控制写方共同遵守约束。第 126–144 行仍待窄执行 Adapter 和全链路验收。

收口要求：确定正式 Project/Task 元数据写入责任、隔离原生工具及文件写旁路；定义执行授权、旧代次拒绝、停止确认、结果采纳与容量释放的持久状态。单仓单轮验收应先证明重复 ready 不重复派工、取消后旧执行不再写正式结果、重启不能复活旧授权，再扩大跨仓及 Loop。

## 分阶段开工判断

| 阶段 | 本次独立复核的判断 |
| --- | --- |
| 首批项目/Issue 管理 | 上游运行缺口不阻塞全部管理功能。U1 应在 Issue 持久化实现前补齐；认证、配置来源及待审总包是否完成，沿 API 主审判断。不得用可用的上游默认 Manager 配置替代 RepoMesh 的明确配置来源。 |
| 二批准备与真实会话 | 尚未达到闭环开发就绪。U2/U3 必须先形成可实现、可验证的 Adapter 协议；仅保存与查询消息可独立实施，但处理器保持未接入。 |
| 完整执行、Graph/Loop、交付 | 尚未达到完整实施基线。U4 所列强制边界、运行映射、执行身份及恢复协议未收口；可做有界技术验证，不应将产品核心保证留给默认上游行为。 |

本复核没有发现必须推翻“长期团队＋后台协调＋受限主机执行”方向的证据。主要问题是现有产品承诺尚未对应到完整的持久关系和运行机制；先补这些边界，再按阶段实施，比重新设计整套仓内 DAG 更符合当前决定。
