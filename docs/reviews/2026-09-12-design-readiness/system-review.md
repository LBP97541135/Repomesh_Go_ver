# 系统、执行与架构设计复核

审查日期：2026-09-12。审查者：多 agent 审查中的 system_architecture 分支。根仓库 HEAD 为 `4516805a276d65eb79490a12783654f3d7b1c677`；审查包含工作树中的现有未提交文档。只新增本报告，没有修改设计、原型或产品代码。

## 判断

已经具备拆分开发切片的架构基础，但还不能把整套设计交给多个实现者直接并行完成运行系统。项目、会话、Issue、主 ChangeSet 的关系，本地事务与外部动作的边界，以及 Web／后台协调／受限主机执行的职责有一致依据。仓内 DAG 复用与 RepoMesh 跨仓控制也有明确分工。

最早的业务记录、查询和浏览器切片不需要等待整个 Graph、交付或 Skill 设计完成，其可实施性还须合并 API 分支对首批契约的审查。进入真实 Manager、运行准备、Worker 派工与循环前，必须先补齐配置绑定、可信执行协议、资源生命周期和换图恢复四项。现有文档对大部分执行缺口已经坦诚说明；本报告将这些列为开发前必须收口的边界，不把它们冒充新发现的产品漏洞。

没有发现 ADR 核心方向之间新的、已无法通过明确后续替代关系解释的根本冲突。未把旧 Draft Issue、每 Issue 一个用户主房间、旧 Graph 职责、三进程之外的 Python 扩展或 Skill 暂缓视为互相推翻。

## 发现和开发前条件

### SYS-01 · P1 · Issue 固定配置的绑定时点和可解析引用缺失

类型为跨模块承诺尚未落到持久关系的设计缺口。API 分支也发现了此项，本分支独立核对其架构侧。

- 承诺侧：`docs/current/model-connection-settings-design.md:70` 要求应用模型不切换其他项目和在途 Issue；候选内部协议 `docs/current/backend-model-operations-draft.md:43`、`:148` 明确旧 Issue 继续原 profile／secret，应用后既有 Issue 配置不变。
- 记录侧：`docs/current/backend-first-batch-persistence.md:20` 定义不可变 ProjectConfigRevision，但 `:22` 的 Issue、`:27` 的 CreationOperation 和 `:67` 至 `:71` 的创建事务没有规定把 Issue 绑定哪一个配置版本。`:154` 的 creationContextRevision 是创建条件修订，不是历史 Issue 配置版本。
- 架构侧：`docs/adr/0018-provision-instance-after-first-draft.md:69` 要求准备时核验当前配置；`docs/current/architecture-design-v1.md:385` 只说明创建核验和后续计划，没有解决配置选择时点。全文没有提供可补足此关系的绑定机制。

触发例：项目使用 cfg1 创建 Issue A，A 尚在排队；用户把项目应用为 cfg2，再创建 Issue B。后台处理 A 时若读取项目当前配置，会使用 cfg2；若只保存创建条件令牌而无法解析旧 cfg1，也不能证明保留旧配置。一个长期 Manager／同一会话承接两项时，还需要区分无 Issue 讨论、A 的协调和 B 的协调实际使用哪个模型。

影响：在途配置不变的界面承诺、费用依据、运行参数及恢复可追溯性无法得到一致实现。当前模型内部／HTTP 新稿仍待采用，此项不宣称实现已违反契约。

建议：在启用运行前确定配置固定发生于创建、首版计划还是首次执行；持久保存可解析的不可变引用，并明确无 Issue 会话的配置策略。当前权限／密钥可用性继续实时核验，不能把固定版本解释为永久有效授权。配置变更与创建交错、进程重启和共享 Manager 处理不同配置 Issue 应纳入同一验收场景。

阻塞：模型应用接运行、B06 运行准备、B07 派工；不阻塞先做不产生运行副作用的创建／查询。

### SYS-02 · P1 · 受控派工协议尚未确定谁有权写正式上游状态

类型为已知执行缺口的开发就绪阻塞，非新发现的 ADR 冲突。

- 目标：`docs/adr/0011-agentteams-controlled-integration.md:15` 至 `:19` 要求工具、凭据、共享存储和实际动作同时受控；`docs/current/architecture-design-v1.md:194` 提出启动绑定 Issue／Task／Attempt／计划／Worker／占用代次。
- 未定边界：`docs/current/graph-loop-design.md:122` 仍要求先确定正式 Project／Task 元数据的写入责任或所有写方共同遵守的版本条件；`docs/current/architecture-design-v1.md:172` 至 `:180` 只给出 REST、Matrix、本地 stdio MCP 三类接入建议，尚无可信协议。
- 上游依据：`docs/agentteams-survey-2026-09-07/agentteams-api-cli-survey-2026-09-07.md:301`、`:312`、`:313` 明确建图、接受结果与委派在本地 stdio MCP，并非 Controller 的远程任务启动 API。
- 现有实测：`validation/agentteams-2026-09-09/reports/worker-auth-live.md:24` 至 `:27` 是同一真实 Worker 的 REST 拒绝和 shared 写成功，`:35` 至 `:42` 是传入 role 改变原生工具角色判断；`validation/agentteams-2026-09-09/reports/controller-mcp-race-live.md:39` 至 `:46` 是 MCP 拉取后覆盖已写入 REST pause 的实际交错。

触发例：RepoMesh 已关闭新派工、登记 V2 待应用，但运行 Agent 仍持有正式 shared 元数据写权；原生 plan_dag 的后续普通上传可把状态恢复成 active。只在 REST Adapter 或 Graph ready 输出处核权，不足以控制真实写入和委派。

影响：计划许可、生效、旧代次拒绝、单 Worker 单任务和跨仓前驱的数据库检查可能被实际执行路径绕过。它们是整套运行正确性的基础，不能等 UI 接完后才决定。

建议：先提交一张逐动作、逐身份的控制表，明确 plan_dag、delegate、ack、submit、accept、cancel、原生文件同步、Git 和模型出口分别由谁执行、使用哪种凭据、在哪检查当前计划与占用代次。确定正式控制元数据的唯一写入职责或共同条件写协议，划定最小上游补丁，再以合法派工和拒绝路径共同证明闭环。须保留必要的心跳、Matrix 通信和产物提交能力。

阻塞：真实 Manager 工具接入、受控仓内 DAG、B07 调度和执行。纯本地事务、只读上游状态采集及 Adapter 契约夹具可先开发。

### SYS-03 · P1 · 长期 runtime 与 Attempt 环境尚无唯一生命周期映射

类型为已知资源设计缺口的开发就绪阻塞，非单凭“未实现”判失败。

- 目标：`docs/adr/0001-agentteams-issue-concurrency-and-isolation.md:245` 至 `:251` 要求每 Attempt 独立 clone、分支、构建目录、实际资源限制和受限宿主控制；`docs/adr/0017-atomic-attempt-resource-reservation.md:25` 至 `:29` 要求先预留、准备后再核验、未知不释放。
- 未定边界：`docs/current/architecture-design-v1.md:83` 规定 Controller 继续管理 Agent runtime，主机执行管理分配给它的环境，同一容器只允许一个生命周期管理者，但具体资源明细仍待核查。`:290` 仍未规定执行端协议和停止核查。
- 已有实测：`validation/agentteams-2026-09-09/reports/worker-runtime-live.md:11` 请求 0.5 CPU／512Mi，`:32` 至 `:41` 实际 Docker／cgroup 未落实这些限制；`:45` 的实际 Worker 没有挂入业务仓独立 clone，工作区由配置／对象存储初始化。这是旧版指定环境的实测，不等于所有部署均如此。
- 后续版本边界：`validation/agentteams-2026-09-10/reports/controller-delta.md:35` 说明相关生命周期与资源投影实现未变，只支持保留旧事实，不代表本次已重跑新版全套隔离实验。

触发例：复用同一 Worker 身份执行 Attempt 2，同时 Attempt 1 的执行或写入结果未知。若主机执行仅更新数据库占用，而 Controller 仍持有相同 runtime 的自动恢复权，资源释放、重新创建和旧写入撤销无法靠一个抽象 stop 动作保持一致。反过来，让两个进程同时管理容器会违反已采用的单一管理者原则。

影响：两个实现者可能分别实现相互竞争的启动／停止路径；界面并发额度与实际 CPU／内存／进程资源不一致。仅增加资源表或调用 Worker PUT 不能满足隔离目标。

建议：先确定一份资源所有者清单，包含 Controller runtime、Attempt clone、任务进程或任务容器、验证服务、网络、数据卷和模型调用。逐项规定唯一管理者、稳定外部引用、启动许可核验点、停止证明、可写能力撤销及释放条件，并明确 Controller reconcile 不会复活已终止 Attempt 的措施。验证小组、Manager／Leader 基础开销以及 Worker 内部临时 agent 都应有明确资源／模型用量归属。

阻塞：host-executor、Worker 复用和替补、正式独立验证、全局容量保证。受控 Python 分析可独立实现自身作业生命周期，不必等完整 AgentTeams 任务映射。

### SYS-04 · P1 · 多轮 DAG 的换图和失败结果收尾仍不足以直接实现

类型为已知状态机缺口的开发就绪阻塞。

- 已采用行为：`docs/adr/0015-round-scoped-upstream-dags.md:15` 至 `:19` 要求在获准范围内追加轮次，不新增业务 Plan Version；下一轮必要时经过受影响 Project 收敛、核对 submitted、replan 和读回。
- 未定边界：`docs/current/graph-loop-design.md:110` 至 `:118` 同时保留门禁持续关闭时 resume→replan，以及允许 paused replan 的窄补丁两条方案；尚未选定 Project 跨轮复用及最终关闭时机。
- 状态证据：`validation/agentteams-2026-09-09/reports/controller-mcp-race-live.md:69` 至 `:76` 显示 in_progress／submitted 无法 replan，accept 后 Project 仍 active 才可进入下一 DAG，而 complete 后不能 replan。该实验接受的是元数据夹具，不证明真实候选接受或正式验证通过。
- 失败语义：`docs/current/graph-loop-design.md:88` 要求失败／待补证不能为满足 replan 条件冒充通过；`docs/current/verification-node-design.md:135` 至 `:143` 又保留五类业务结论，因此不能把单一上游 completed 直接当作所有业务结果的映射。

触发例：第一轮独立验证已 submit，结果是业务缺陷，需要第二轮修复。实现者必须决定如何真实收尾 submitted、保留缺陷、让上游处于可改图状态，并防止在 resume 窗口新派工。只写“等 submitted 收敛后 replan”，没有给出可恢复的合法路径。

影响：按原生 API 直译会在第二轮返回 409；错误地 accept／complete 或恢复旧安排又可能解除不应解除的依赖，或使之后轮次无法继续。跨仓只成功应用一个目标时问题更明显。

建议：冻结一条最小换图状态迁移及恢复协议，逐类映射执行事实、业务采纳和失败结论；规定 Project 何时最终 complete、每轮任务身份、累计计数和部分应用恢复责任。先证明单仓两轮真实受控闭环，再扩两仓部分应用和回执丢失。无需为此重写完整 Go DAG 引擎。

阻塞：有界 Loop、重规划、多目标应用和最终 Graph 状态展示。单仓单轮 Adapter 与纯只读图可先做。

### SYS-05 · P2 · 验证专题需要补上首期 Skill 暂缓时的适用条款

类型为首期适用范围未标清，未发现足以认定严格矛盾的非空 Schema。

- `docs/current/verification-node-design.md:22` 要求记录使用的 Skill 版本；`:88` 和 `:111` 将其列入最小证据和节点输入的判定依据。
- `docs/adr/0009-skill-engineering-deferred.md:11` 明确不开发分发加载与版本隔离，`:51` 不把未实现能力作为其他模块前提；`docs/current/architecture-design-v1.md:409` 已提出记录实际可取得的工作说明及来源。

触发例：首期验证器根据最小证据表设计必填字段时，尚不存在按 Issue 受控加载并核验的 Skill 版本。当前文字没有在验证专题当地说明“未使用 Skill”与“使用但版本无法核验”的区别，容易造成无条件硬阻塞或填写不能证明实际加载的版本。

建议：直接在验证输入和最小证据表注明首期记录实际工作说明、脚本／内容版本及来源；有实际 Skill 使用时记录并核对它，未使用不伪填。不能核验必要执行依据时仍应保留缺口。此项不要求恢复 Skill 工程。

阻塞：正式验证记录 Schema 和验收器实现前应补齐；不阻塞创建、配置、列表和本地事务。

## 分切片开发判断

| 开发切片 | 系统侧判断 | 开始前的具体条件 |
| --- | --- | --- |
| 项目／会话／Issue／主 ChangeSet 的本地记录及查询 | 架构可支持开工 | 合并 API 审查的首批权限、事务和失败契约；真实运行状态保持未接入事实。 |
| 首批页面、列表、创建与恢复交互 | 不被完整执行架构阻塞 | 只消费已采用契约；候选认证和模型协议的采用状态由总审查核对。 |
| Python 仓库分析 | 有独立边界，可单独收口实现 | 固定 Python 输入输出 Schema、只读快照、作业幂等、资源上限与停止核查；不依赖 Manager 或 AgentTeams Ready。 |
| Manager 真实讨论／创建工具 | 尚不具备直接并行接入标准 | 可信消息与工具身份、配置选择、会话／runtime 映射和投递恢复闭环。 |
| Worker 派工／独立 Attempt | 尚不具备标准 | SYS-02、SYS-03 的可信执行、生命周期与资源约束。 |
| Graph 两轮以上、跨仓汇合 | 尚不具备标准 | SYS-04 的换图状态机和恢复；本地业务采纳与原生 ready 不能混同。 |
| 独立验证与 PR 交付 | 产品语义充分，执行协议尚未收口 | 固定 commit／配置／数据证据、独立作者核验、Git 受控入口、PR head／合并事实和最终实际组合映射。 |
| Skill、人工审批、拖拽改图 | 按既有决定后置 | 不为首批开发补建空接口或假加载状态。 |

## 已核对且不应误报的设计关系

1. ADR-0019 只替代会话与 Issue 的入口绑定，不撤销各 Issue 的独立计划／执行／ChangeSet；ADR-0016、0018 有针对新入口的后续补充。
2. ADR-0014、0015 的 09-09 补充明确上游仓内 DAG 与本地跨仓／Loop 分工，原生 Loop 存在不表示首期授权它自主推进。
3. 三类 Go 进程与 ADR-0020 的 Python 分析是明确的局部扩展；Graph 仍在 Go 后台进程内。
4. YOLO 的受限结果与人工审批分开；人转正式 PR／合并不被解释为 Agent 自动合并权限。
5. 默认 Worker 数量为 1 是并发上限，不是固定只有一个身份，独立验证不必因此让作者自证。
6. Candidate 收录、初审接受、固定组合验证、可审查、PR 合并、Issue 完成相互区分。CS10、11 对不安全合并顺序和部分合并已有规则，没有发现与 E1 提前开 draft PR 相冲突。
7. 09-07／09-09 的 `eeaab64391ccaec9118e84977f538aefd40720d6` 与 09-10 的 `517caff9280242a00a4d4c06365352b9e41659c6` 分开使用。后者新增读取和模型增量证据不覆盖旧执行缺口，不以 main／latest 的文字作为浮动部署锁。

## 完整覆盖清单

下表 34 份主责文件全部逐段阅读；大文件分段读取，输出截断部分另行补读。导航、引用追溯和跨模块定点复核不冒充另一个分支的完整覆盖。

| 文件 | 行数 | 主要核对内容 |
| --- | ---: | --- |
| docs/adr/README.md | 48 | 采用范围、后续替代和证据解释 |
| docs/adr/0001-agentteams-issue-concurrency-and-isolation.md | 479 | 团队／Issue／实例隔离、并发、生命周期、验收 |
| docs/adr/0002-github-app-authorization-and-draft-pr-delivery.md | 55 | 权限交集、部分失权、受控 Git 和人工合并 |
| docs/adr/0003-plan-change-authorization-and-activation.md | 255 | 许可／生效、模式、在途收敛、多目标应用 |
| docs/adr/0004-acceptance-rule-clarification.md | 32 | 缺失规则解释、依据、验收边界 |
| docs/adr/0005-optional-project-verification-group.md | 94 | 独立验证、默认关闭、停用收尾 |
| docs/adr/0006-manager-entry-modes-and-skill-driven-execution.md | 122 | 统一 Manager、YOLO、工具与 Skill 职责 |
| docs/adr/0007-graph-loop-plugin.md | 68 | 循环上限、停止、迟到结果、复用边界 |
| docs/adr/0008-changeset-attribution-and-history.md | 99 | 稳定归属、候选、组合、交付历史 |
| docs/adr/0009-skill-engineering-deferred.md | 73 | 暂缓范围和保留保障 |
| docs/adr/0010-technology-stack-and-modular-monolith.md | 41 | 技术栈、模块化单体与 Python 特例 |
| docs/adr/0011-agentteams-controlled-integration.md | 43 | 实际动作控制和补丁边界 |
| docs/adr/0012-issue-scoped-upstream-projects.md | 61 | 上游复合身份、多目标应用、房间非等同 |
| docs/adr/0013-web-coordinator-host-executor-processes.md | 57 | 三进程、权限边界、生命周期唯一管理 |
| docs/adr/0014-in-process-graph-plugin.md | 57 | 进程内计算、远端 Adapter 和职责收窄 |
| docs/adr/0015-round-scoped-upstream-dags.md | 59 | 获准轮次、业务版本与上游安排修订 |
| docs/adr/0016-transactional-background-work.md | 102 | 本地同事务、外部未知、来源、事件与幂等 |
| docs/adr/0017-atomic-attempt-resource-reservation.md | 43 | 统一预留、启动再核验、未知不释放 |
| docs/adr/0018-provision-instance-after-first-draft.md | 90 | 历史／当前启动时点、无 Issue 消耗 |
| docs/adr/0019-conversation-issue-separation.md | 64 | 会话／事项分离及精确替代 |
| docs/adr/0020-python-repository-analysis-plugin.md | 14 | 独立 Python 作业和受控生命周期 |
| docs/current/architecture-design-v1.md | 431 | 模块所有权、三类状态、控制／资源／恢复／交付 |
| docs/current/technology-selection.md | 39 | 技术选型及已落地骨架的区别 |
| docs/current/graph-loop-design.md | 144 | 原生复用、结果采纳、换图、轮次和证据 |
| docs/current/team-execution-policy.md | 33 | 调度、复用、角色故障和回收 |
| docs/current/verification-node-design.md | 177 | 五类结论、输入、必检证据、复核和覆盖 |
| docs/current/integration-environment-design.md | 49 | 固定环境、mock 数据、版本和清理 |
| docs/current/draft-pr-review-design.md | 29 | PR 产生、更新、审查和人工合并 |
| docs/current/changeset-design.md | 57 | CS1—14、合并过渡、部分成功、恢复 |
| docs/current/changeset-structure.md | 115 | 身份／版本／记录关系和逻辑示例边界 |
| docs/current/changeset-design-discussion.md | 9 | 历史链接入口，不恢复旧提案 |
| docs/current/skill-engineering-design.md | 92 | 暂缓能力、上游固定版本和实际加载边界 |
| docs/current/issue-creation-repository-analysis.md | 99 | 固定快照、来源、幂等、输入变化、分析恢复 |
| docs/current/agentteams-validation-plan.md | 151 | AT-01—12、旧版静态与新版限定证据 |

另外完整阅读了根 AGENTS.md、README.md、CONTEXT.md、docs/current/HANDOFF.md、docs/current/README.md。定点复核了首批持久化、项目配置、模型设置与内部候选协议，以及 API／CLI 调研第 1—4、7—8 节。使用的旧／新版本实验报告分别在上文逐项标出。本分支没有完整阅读调研目录三份文件，完整覆盖由独立的上游复核分支负责。

## 检查限制

- 本次是设计审查，没有运行产品构建、Go／前端测试、旧验证脚本、真实 Controller、Docker、Matrix、模型、GitHub 写操作或负载测试。
- 原型完整覆盖和 API／数据契约全面检查由其他分支承担，本报告不能独立替代总审查。
- 上游行为取自现有锁定版本调研和实验记录；本次没有新做运行复现，也没有把旧版运行观察冒充新版全套结果。
- 对局部明确替代的历史条款按适用范围解释；没有根据 accepted／proposed 标签或文件日期简单覆盖整篇。
- “可以开始某切片”表示系统设计层面具备分拆依据，不表示用户已经授权本次审查任务启动业务开发。
