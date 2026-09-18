# Graph／Loop：复用上游 DAG 与 RepoMesh 协调

> **物理承载替代说明（2026-09-15 更新）：** 本文的逻辑契约与候选状态不变。物理表承载自 2026-09-15 起以 [Go 版数据库重构方案](../RepoMesh_Go版数据库重构方案.html) 的 44 张目标表为准，对应接口见 [API 设计](api-design.md)；此前的 B05-B11 物理合表已归档到 [历史目录](../archive/2026-09-15-api-database-catalog/README.md)。

更新：2026-09-10，补充新版上游验证事实。本文是 Graph／Loop 的现行专题。用户已采用“按最新版本复用 AgentTeams 原生 DAG，不重写全部能力”，并要求清理、更新相关文档。职责调整记录在 [ADR-0014](../adr/0014-in-process-graph-plugin.md)／[ADR-0015](../adr/0015-round-scoped-upstream-dags.md) 的 09-09 补充；[ADR-0007](../adr/0007-graph-loop-plugin.md) 的流程与循环规则继续有效。

**已采用的是复用方向和职责边界；具体工具桥接、状态字段、补丁及恢复协议仍需实现和验证。** 当前已有[基础工程骨架](development-scaffold.md)，尚无 Graph／Loop 或业务实现。本专题记录设计决定；09-09 另有从锁定源码构建的双实例隔离验证，不能把上游实验写成 RepoMesh 产品已部署或业务契约已通过。

## 1. 最新版本基线与证据

**09-10 跟进基线为 `517caff9280242a00a4d4c06365352b9e41659c6`**。这次是相对下述 09-09 基线的增量验证，不替换旧版实验原文，也不宣称新版 AT-01—12 全套已通过。[Controller 差异及官方测试](../../validation/agentteams-2026-09-10/reports/controller-delta.md)记录三包 252 个顶层测试＋59 个子测试，即 311 个通过事件；[真实 HTTP／身份／CLI](../../validation/agentteams-2026-09-10/reports/controller-api-live.md)记录 51 项主观察和 31 项真实 SA TeamLeader 范围检查。后者身份来自 Controller 生成的 Worker SA 与实际 Team membership，不是普通 Worker 自有权限保证。

**09-10 双在线补测：** [c/d 双实例读取矩阵](../../validation/agentteams-2026-09-10/reports/controller-twin-read-live.md)完成 **313 个断言、260 次 Controller HTTP**；两实例各有两个同名 Team，使用 Admin 与真实 SA TeamLeader 验证新增读取路径。**30 次错实例请求均为 401，每次来源前后请求均为 200；24 个夹具原字节哈希前后不变**。这是停止／未托管引用和元数据夹具的读取契约验证，不是业务验收。原 51／31 项保留独立历史计数；本次在线、TLS 与来源身份有效的对照补充旧时钟异常观察，不覆盖或改写其原始证据。

**09-10 存储与模型补证：** [真实S3 trace](../../validation/agentteams-2026-09-10/reports/mermaid-storage-read-live.md)补齐20项检查：JSON前后正对照读取TaskMeta，Mermaid true/false有限窗口内目标TaskMeta的HEAD／GET均0。[新版Manager实际推理](../../validation/agentteams-2026-09-10/reports/manager-model-smoke-new.md)随后通过一次Matrix消息返回精确标记，原生会话及用量确认1次模型调用、未观察到工具调用；Console503和后置读探针错误保留。以上均不代替RepoMesh业务验收。

新增 Task inspection 读取 TaskMeta 时返回原始 status，缺失／破损／归属不符时回退图节点的规范化状态；history 只是读取已有字段，trace 只是 project/task 过滤提示，都不能替代业务审计、运行身份或产物验证。workflow 缺省仍返回 JSON，显式 `format=json` 返回 **400**。有效 task ID `end` 的 Mermaid API 虽返回 200，其原字节在[真实浏览器解析](../../validation/agentteams-2026-09-10/reports/mermaid-browser-live.md)失败；实际 CLI 详情即使传 `--team` 仍可因同名 Project 返回 **409**。页面与 Adapter 不能据此默认可渲染或已消歧。逐函数未变的 DAG、Project 写入／取消、认证缓存等只能复用 09-09 已注明范围的证据，不能改写成新版全套实测。

Manager配置清空的CFG01—05也已完成到真实CR、存储投影与QwenPaw运行进程内存模型，见[Manager实测](../../validation/agentteams-2026-09-10/reports/manager-config-live.md)。时钟回退中断后的证书恢复单独记录；最终默认模型chat，CFG读回本身不代表模型推理，后续基本推理单独见上述专项。

以下表格保留 **09-09 当时的查询与构建基线**：

2026-09-09 通过官方 GitHub API 重新查询 `commits/main`、`releases/latest` 与 `commits/v1.2.3`：

| 对象 | 核查结果 | 本文如何使用 |
| --- | --- | --- |
| 09-09 查询的源码 `main` | [`eeaab64391ccaec9118e84977f538aefd40720d6`](https://github.com/agentscope-ai/AgentTeams/commit/eeaab64391ccaec9118e84977f538aefd40720d6)，提交时间 2026-09-05 07:20:50 UTC | 09-09 接入设计及实测的确定源码基线，与 09-07 调研相同；09-10 增量见上方。 |
| 最新正式 release | [`v1.2.3`](https://github.com/agentscope-ai/AgentTeams/releases/tag/v1.2.3)，发布时间 2026-08-22 00:13:25 UTC；对应提交 `223ddc2b8073e4c8b93bcbb15e1d717f196c04d9` | 区分最新 release 与更新的 main，不将两者当作同一构建。 |
| 实际运行构建 | 四个官方 Dockerfile 镜像及双实例实际运行已核对，见[部署记录](../../validation/agentteams-2026-09-09/reports/deployment-run.md) | 使用本次记录的镜像 ID，不指定浮动 `latest`。后续升级重新核查契约。 |

原生能力引入时间也已核对：TeamHarness 核心 [PR #939](https://github.com/agentscope-ai/AgentTeams/pull/939) 于 2026-06-21 合入，其源码已含 `plan_dag`、`ready_nodes`、`accept_task_result` 和 Loop；Controller 的图查询、生命周期写入分别在 2026-08-14 的 [PR #1169](https://github.com/agentscope-ai/AgentTeams/pull/1169) 与 2026-08-15 的 [PR #1172](https://github.com/agentscope-ai/AgentTeams/pull/1172) 合入。以上日期为 UTC，09-07 是调研日期。

不同证据分别使用：

- [09-07 API／CLI 调研](../agentteams-survey-2026-09-07/agentteams-api-cli-survey-2026-09-07.md)与锁定源码证明接口及实现机制。
- [TeamHarness 组件实验](../../validation/agentteams-2026-09-09/reports/control-component-report.md)使用真实工具实现，但 Matrix 与 mc 传输为 fake；它复现了入口旁路、身份冲突与结果语义等，不证明部署 Worker 的实际副作用。
- [Controller 组件实验](../../validation/agentteams-2026-09-09/reports/controller-components.md)覆盖 replan 状态限制及状态摘要语义。
- [真实 Controller HTTP＋MinIO 记录](../../validation/agentteams-2026-09-09/evidence/controller-live-results.json)中 LIVE-C03 确认 `pause → replan → resume → replan` 返回 `200 → 409 → 200 → 200` 并读回；LIVE-C04 确认显式管理员 mc 陈旧写可覆盖 REST pause。后者未验证部署 Worker 凭据路径，整份记录不包含 runtime／模型或 RepoMesh 业务验收。

同日后续[实际 Worker 自有凭据实验](../../validation/agentteams-2026-09-09/reports/worker-auth-live.md)已补上实际部署身份：REST 拒绝其 Project 更新，但自身存储凭据可写 global shared，已安装 TeamHarness 接收 `role=leader` 后实际取消夹具 Task。另有[产物与副作用证据](../../validation/agentteams-2026-09-09/reports/worker-side-effects-live.md)：缺失附件仍可被接受，取消后接受结果可使 Project 节点 completed 而 Task cancelled。接入不能只依赖外层 `ok`、REST 鉴权或单一状态字段。

持续更新的[逐项验证状态](../../validation/agentteams-2026-09-09/reports/validation-status.md)区分组件、真实上游与未实现的 RepoMesh 适配。核查时读取对应专项范围；实验复现原生缺口不构成完整业务验收，也不改写历史实验原文。

## 2. 能力复用与职责

| 能力 | 上游复用 | RepoMesh 保留的职责 |
| --- | --- | --- |
| 仓内当前轮次 DAG | TeamHarness `projectflow plan_dag` 的任务结构、依赖与环校验 | 形成有明确业务归属的当轮安排；校验范围、输入、验收覆盖及跨仓关系；不另造通用 DAG 语言或复制仓内调度算法。 |
| 仓内候选就绪节点 | `ready_nodes` 的依赖计算 | 核查观察的新鲜度、跨仓前驱、结果采纳、计划生效与占用；原生 ready 只是候选资格。 |
| 任务委派、接收、提交 | TeamHarness 本地 stdio MCP `taskflow` | 受控桥接、可信身份、权限、预算、容量和 Attempt 隔离；不虚构 Controller 任务启动 REST 接口。 |
| 结果接收与上游状态推进 | `accept_task_result` 等原生记录能力 | 先核对归属、产物和业务采纳，再受控映射；上游 completed 不自动表示候选接受、验证通过或 Issue 完成。 |
| 图查询与计划调整 | Controller workflow／history／artifact，以及适用的 replan 等操作 | 记录本地业务依据，处理停派工、收敛、逐目标读回、部分应用和恢复；不把查询摘要当完整状态机。 |
| 跨仓依赖、固定组合及独立验证 | 各上游 Project 提供本委派范围的执行事实 | RepoMesh 负责全局关系、组合选择、证据适用性和实际放行。 |
| 修复／诊断／环境恢复 Loop | 每轮继续使用上游有限 DAG | RepoMesh 判断后续轮次、累计消耗及停止；首期不调用原生 Loop 自主推进整段业务循环。 |

原生 Loop 存在这一事实保留。按 ADR-0015，首期选用的是“上游 DAG＋RepoMesh 有界循环”，不是因误认为上游没有 Loop 才另做一套。

## 3. Graph 模块、执行 Adapter 与派工

ADR-0014 的进程内模块及配套发布方式不变；“Graph 插件”现在明确指 RepoMesh 的跨仓协调与循环策略模块，不承担完整仓内 DAG 引擎。模块接收后台提供的有效计划、已采纳结果、固定组合、循环进展以及带来源与修订信息的上游 DAG 观察，返回需要等待、可请求执行或应停止的判断。

后台通过上游执行 Adapter 读取原生就绪节点、发布安排、接收结果和委派工作。Graph 模块不持有 Controller／GitHub／Docker 凭据，不直接调用远端、创建 Worker 或占用资源。它的本地业务策略可以确定性计算；这不要求把上游 Python DAG 代码移植为 Go。具体 Interface、错误格式、观察修订及兼容机制继续细化。

```text
Manager／Leader 提出当前有效范围内的工作安排
        |
后台经 Adapter 写入／读回各仓当前轮次 DAG
        |
上游 ready_nodes → 带来源的候选就绪节点
        |
RepoMesh Graph：跨仓前驱、结果适用性、循环与停止判断
        |
后台：核验计划和权限，原子预留 Attempt／Worker／容量
        |
事务外准备环境 → 启动前重验 → 受控 taskflow／执行入口
        |
结果核查与采纳 → 持久记录和后续待办 → 下一次协调
```

上游 `ready_nodes` 会返回依赖已 completed 的 `planned` **或 `assigned`** 节点，因此同一节点再次出现不能被当成新的派工请求。执行 Adapter 核查当前 Attempt 和稳定操作身份；上游查询失败或观察陈旧时先补查，不静默启用一套本地仓内 DAG 调度替代上游。

React Flow 展示 RepoMesh 汇总的业务节点、依赖、版本与阻塞原因，可展开原生任务和 Attempt；不直接把压缩的 workflow 状态作为正式业务状态，不在浏览器中另算派工。直接编辑图仍延后，完整图查询／推送契约另行细化。

## 4. 跨仓依赖与结果采纳

上游 Project 按 Issue 的仓库委派范围隔离，引用包含 `instance/team/project_id`；不同 Issue 共享 Team 并发额度。业务会话可关联多 Issue，不作为图或计划的唯一身份。验证工作的上游 Team／Project 归属继续设计，不默认启用常驻验证小组。

跨仓边保存在 RepoMesh。若后端节点依赖前端产物，即使上游仓内图将后端节点列为 ready，前端产物未满足业务条件时也不能委派或启动。不能删除跨仓边后直接放行；也不通过伪造上游“完成任务”绕过依赖。按受控映射发布当前工作并在实际执行入口检查外部前驱，具体批次物化方式需和 replan 约束一起验证。

结果至少区分执行事实、候选接受、验证结论与 Issue 交付。`submitted`、`completed` 和工具 `ok` 各自只说明相应上游事实；验证通过仍要求当前固定组合下的必检项具备有效通过证据。接受结果的工具写入也必须受控，不能在业务采纳前将成功状态传播给依赖节点。为了满足 replan 条件处理 submitted 时，按真实结果记录收尾，不把失败或待补证改成通过。

例如 frontend F1 与 backend B1 经初审接受后，Manager 选定 C1，独立 Worker 验证。发现后端业务缺陷后，下一轮只修后端得到 B2；Manager 选定 C2＝F1＋B2，再判断哪些证据仍可复用、哪些必须重验。F1、B1、C1 的结果与历史保留。旧 Attempt 的迟到结果归档，未经重新核对不能解除当前依赖。

## 5. 轮次、版本与停止条件

| 记录概念 | 用途与变化条件 |
| --- | --- |
| Plan Version | 不可变业务安排，包含范围、依赖、验收及预算／循环上限；范围或约束改变时按 ADR-0003 形成变更。 |
| 循环进展／轮次 | 记录同一获准工作目的的累计推进；范围和上限内进入下一轮不创建新业务 Plan Version、Issue 或 Manager。 |
| 上游任务安排修订 | 记录向哪些 Project 发布了哪些有限任务、应用和读回情况；技术改图与业务计划变更分开。 |
| Task／Attempt | 工作与一次执行分开；新 Attempt 不覆盖旧执行，结果保留其实际输入和轮次。 |
| 上游绑定及操作身份 | 映射到明确的 instance、team、project、task 和本地 Attempt；同一逻辑操作重试复用身份，新执行不复用旧产物路径。 |

表中是记录职责，不冻结新表、字段或独立 IssueRun 产品实体。上游 `shared/tasks/{task_id}` 未按 Project 分目录，标识必须避免跨 Issue／轮次／Attempt 碰撞；不能每张图都从 `T1` 开始，也不能每次传输重试都生成新身份。编码格式继续细化。

默认每个修复 Loop 3 轮、诊断补证 2 轮、环境恢复 2 次；落实到具体计划，预算或时限先到先停。连续两轮同一失败且无新增有效证据时停止，Manager 记录未完成原因；换计划或重命名节点不清零累计消耗。YOLO 在现有授权内继续，不增加逐轮人工审批，也不自行提高总体预算、权限或放宽明确验收条件。

实现时分别记录请求重试、Attempt 重试与业务修复轮次。轮次登记和额度预留应能去重；准备失败、取消、真正启动及未知结果怎样计数和释放，需要明确协议。不能通过技术性重试绕过上限。“同一失败／新增有效证据”由协调或复核角色给出可追溯判断，系统执行停止规则；不以日志字符串相等代替业务判断。

## 6. 换图、恢复和真实执行约束

2026-09-12 审查明确：本节的写入责任和两种换图接法仍未选定，不能作为已经收口的执行接口直接实现。应先交付[执行接入门槛 G3—G5](execution-integration-gates.md)要求的写方／凭据清单、生命周期协议、submitted 收尾及逐目标恢复规则；下文的业务顺序继续有效，未因此采用某项上游补丁。

业务要求仍是对完整受影响上游 Project 停新派工、等在途收尾、核对 submitted、应用并读回。当前基线 REST replan 只接受 active 的 DAG（或尚未设置 plan_type），且拒绝存在 in_progress／submitted 的情况；paused、completed 和原生 loop 不能直接套用该操作。不要在仍可能追加获准轮次时过早 complete 上游 Project；Project 跨轮复用与最终关闭时机须在映射协议中明确。

建议落实的应用流程如下，具体状态字段和恢复算法尚未冻结：

1. 记录目标修订及受影响 Project 清单，持久关闭这些目标的新派工入口。上游 pause 可配合收敛，但它不代替 RepoMesh 的实际执行约束。
2. 核查在途与 submitted，按真实状态收尾；停派工不等于终止进程。
3. 通过经过验证的受控接法更新各目标：可在 RepoMesh 门禁持续关闭时 resume 后 replan，或以窄补丁支持保持 paused 的受控 replan。两条路径尚未最终选定，本次复用决定不等于采用某项补丁。
4. 逐目标权威读回，核对任务、依赖、身份及预期修订／内容。只有全部必要目标确认一致，才记录本次安排已落实；若涉及新 Plan Version，此时才记录其生效。
5. 再次核验计划、权限、预算和占用，恢复允许的派工。前端成功、后端失败／未知时保留各自事实，受影响范围持续关闭，不盲目恢复已被改写的旧安排。

同一逻辑操作重试沿用操作身份与输入；409、超时或重启先读回核查。业务结果与后续待办同事务保存；Attempt、Worker 占用与容量短事务统一预留，环境准备在事务外，启动前再次核验，按 ADR-0016／0017 执行。旧进程未停止或未失去写能力时不能仅凭超时开替补，也不能将取消元数据当作资源已释放。

需要封闭实际旁路：原生 delegate、改图、结果接受，以及可达的文件／存储写入都必须遵守适用约束。REST ETag 与 MCP 普通覆盖不是统一并发保证；先确定正式 Project／Task 元数据写入责任或所有写方共同遵守的版本条件，再验证。复用原生 DAG 不代表开放不受控的原生工具；仅限制页面按钮或 ready_nodes 输出不足以形成保证。

## 7. 落地次序与验证重点

1. 固定上述源码及实际构建，定义窄执行 Adapter 的建图／读图／就绪查询／结果接收／委派契约；先用单仓、单轮最小 DAG 验证原生复用和受控路径，不先开发通用 Go DAG 引擎。
2. 建立本地计划、映射、操作、结果与待办记录，打通去重、原子预留、启动前核验、响应丢失核查和重启恢复。
3. 扩展双仓并行／依赖／汇合、固定组合与独立验证，证明仓内 ready 不绕过跨仓与业务前置条件。
4. 完成有界 Loop、整 Project 收敛、多目标应用与下一轮恢复，再接 React Flow 的业务状态展示。

验证沿用 [AT-01—12](../plan/agentteams-validation-plan.md)，补充聚焦：

| 场景 | 必须观察到的结果 |
| --- | --- |
| 原生 DAG 的顺序、并行、汇合与依赖／环校验 | 通过真实受控 Adapter 使用上游能力；RepoMesh 不靠另一个仓内引擎使测试通过。 |
| assigned 节点重复出现在 ready 查询、队列重复投递 | 不重复创建有效 Attempt 或启动。 |
| 上游 ready，但跨仓前驱或业务接受未满足 | 实际工具及执行入口均不放行。 |
| 原生 completed 无证据、submitted 未复核 | 不宣布对应业务依赖满足或验证通过。 |
| pause／resume／replan 期间直接 delegate 或陈旧上传 | 不启动旧工作、不静默覆盖有效安排。 |
| 两仓只写成功一仓、响应丢失或后台重启 | 保留部分事实，逐目标核查后恢复，不提前生效或重复启动。 |
| 跨 Issue 同 Team、下一轮、新 Attempt 与旧结果迟到 | 身份不碰撞，历史保留，旧结果不直接解锁当前图。 |
| 连续相同失败、预算先到、换计划与进程重启 | 按累计规则停止，轮次与消耗不清零。 |

当前仍需完成受控适配、真实 Agent 执行、跨目标恢复和 RepoMesh 全链路验收。Skill 工程、后续审批能力及直接编辑图继续延后；本专题不启动这些模块，也不改变 Git 人工合并职责。
