---
status: accepted
date: 2026-09-07
updated: 2026-09-09
---

# ADR-0001：基于 AgentTeams 的团队组织、Issue 并行与单机隔离

> 局部替代及后续细化（2026-09-09）：[ADR-0019](0019-conversation-issue-separation.md) 已将用户会话与正式 Issue 分离；[ADR-0018 的现行补充](0018-provision-instance-after-first-draft.md#2026-09-09-持续设计授权补充明确提交后自动接续准备) 已采用首次会话消息或页面建项提交后异步准备。本文现行规则与验收据此更新，§7.3 单独保留旧启动时序。项目实例、长期团队、Issue 工作归属及独立 Attempt 保障继续有效；角色执行上下文与 room/session 的具体映射仍待细化和验证。

RepoMesh 使用 AgentScope 团队的 AgentTeams 作为协作基础，采用“一个多仓库项目对应一个 Manager、一个代码仓库对应一个 Team”的长期组织模型。多个 Issue 保留独立计划、任务记录和执行工作区，用户会话可关联多个 Issue；不为每条 Issue 复制 Manager、团队或整套平台。

多项目部署进一步确定为：一个 RepoMesh 统一管理多个多仓库项目，每个独立项目对应一套独立 AgentTeams 实例；多个实例可以位于同一台服务器，实例内复用本项目的 Manager 与仓库 Teams。房间承载协作对话，不代替 Issue 执行归属、项目权限或数据隔离。

用户主界面统一与 Manager 对话，团队和验证按 Skill 指导组织执行，见 [ADR-0006](0006-manager-entry-modes-and-skill-driven-execution.md)。页面或 Manager 创建的正式 Issue 均由项目 Manager 总协调，再按范围委派各仓 Leader；主房间按业务会话关联，不要求每条 Issue 独占一个用户主会话。Skill 工程整体暂缓，尚未实现的加载与版本隔离不作为当前已具备能力。

本记录汇总产品讨论中确认的设计方向。`accepted` 表示产品设计已接受，不表示代码已经实现、部署已经完成或上游能力已经通过运行验证。

## 1. 背景与适用范围

- 用户当前最多使用一台服务器，需要支持多仓协作和多个 issue 同时在途。
- 原讨论希望通过独立聊天隔离每条需求；ADR-0019 后改为会话可关联多项，并以明确的工作归属保持计划与执行隔离。
- 仓库团队应长期复用，不能因每次需求重复建立而丢失职责和知识归属。
- AgentTeams 的资源对象、会话、运行进程和访问权限是不同层次，不能互相替代。
- 历史 PRD、旧版设计及修改前快照通过归档索引查阅，不作为当前实施范围。
- 本期不承诺不可信多租户隔离，不承诺一 Agent 一 VM，也不以增加服务器为前提。

术语定义见 [RepoMesh 领域语言](../../CONTEXT.md)。

## 2. 已接受的决策

| 编号 | 决策 | 原因与边界 |
| --- | --- | --- |
| D01 | 使用 `agentscope-ai/AgentTeams` 作为协作基础 | 复用角色、团队、通信和运行生命周期能力；不平行重造一套团队系统。 |
| D02 | 一个多仓库项目由一个 Manager 负责 | 项目现有 Manager 负责全部 Issue 的总协调和对人沟通；各项计划、决定与执行记录分别归属，复用同一个 Manager 身份，角色上下文映射待验证。 |
| D03 | 一个 Team 长期负责一个仓库 | Leader 负责协调，Worker 执行；Team 不等于共享容器或共享工作目录。 |
| D04 | 用户统一与项目 Manager 沟通 | 会话承载讨论，可明确建立多个独立 Issue，按各项范围委派 Leader，Manager 始终保留总责；首次消息或页面建项提交后按 ADR-0018 异步准备，运行准备不等于业务派工。 |
| D05 | Issue 工作归属隔离，用户主会话可关联多项 | 按 ADR-0019 替代每项独占用户主房间的绑定。每项保留独立计划、执行与验收，复用项目 Manager 身份；实际执行上下文与 room/session 映射待细化验证，不能把共享聊天当作共享执行状态。 |
| D06 | 协调采用短轮事件驱动，执行按容量并行 | 派工后结束本轮协调；结果或阻塞事件触发下一轮，不因等待一个 issue 而阻塞其他 issue。 |
| D07 | 一个 Worker 同时只有一个活跃执行任务 | 空闲检查与占用由后台统一保证，不能靠多个会话各自判断。 |
| D08 | 每个任务尝试使用独立代码副本与结果记录 | 不同 issue、任务和重试不能共享脏工作区。 |
| D09 | 跨仓读与跨仓写分离 | Team 默认只修改本仓；需要修改其他仓库时升级给 Manager。 |
| D10 | 多仓工作区用于受控集成验证 | 检出固定候选组合进行验证，不建立可随意跨仓修改的超级开发成员。 |
| D11 | 一期使用单机容器方案 | 沿用 AgentTeams 单机架构，不以 Kubernetes 或逐 Agent 虚拟机为起点。 |
| D12 | 多业务项目隔离与多 issue 并行分开设计 | 多 issue 复用本项目的 Manager 和 Teams；独立业务项目采用独立实例，不按 issue 复制实例。 |
| D13 | 一个 RepoMesh 管理多个项目，每项目一套 AgentTeams 实例 | 同机部署完整的项目级协作服务和数据边界，不仅新增房间或具名 Manager。 |
| D14 | 初期基础服务也按实例分开 | Controller、Matrix、存储与网关按项目实例配置；接受重复基础开销，避免先开发共享服务的多租户体系。 |
| D15 | 全服务器容量由 RepoMesh 统一约束 | 全局执行上限叠加项目额度；项目登记数量不等于同时启动或执行数量。 |
| D16 | 项目生命周期与实例运行状态分开 | 支持先登记、按需启动与保留数据停机；新项目不是自动立即启动全部成员。 |
| D17 | Issue 导航实际关联的主房间与 Leader 房间 | 单仓与跨仓均由项目 Manager 总协调；主会话可关联多个 Issue，各仓工作仍有明确事项与委派归属。Leader 房间共享粒度、具体 room/session 拓扑待适配；普通 Task/Attempt 不因本次更正默认逐个建房。 |
| D18 | 项目先保存，明确提交后异步准备 | 按 ADR-0018 现行补充，首次会话消息或页面建项提交后准备／恢复项目实例及对应主房间。仅建立项目、空会话或只读访问不触发；先持久保存及登记待办、实际就绪才投递或接手、失败保留归属并核查。 |

## 3. Issue 接入与责任规则

### 3.1 两种创建入口，由 Manager 总协调

会话首次消息由 RepoMesh 核权并持久保存会话、消息及对应待办，按 D18 异步准备；普通讨论无需建立正式 Issue。用户明确要建立工作且目标、范围清楚时，Manager 通过受控创建工具登记正式事项；也可由用户从 Issue 栏表单直接创建。两入口共用业务创建与结果查询逻辑，Issue、主 ChangeSet、来源关联及对应待办等原子范围见 [ADR-0016](0016-transactional-background-work.md) 和[创建接口契约](../current/issue-page-create-api-contract.md)。

页面创建不等待 Manager 或房间 Ready；提交后后台按 ADR-0018 准备，Manager 接手已登记 Issue，不重复创建。Manager 核对本项范围和有效计划，再委派仓库工作。用户在关联主会话查看进度、结果并反馈；目标明确及歧义澄清按[消息目标设计](../current/conversation-message-target-design.md)处理，不从当前页面猜测工作归属。

| Manager 判断的范围 | 委派方式 | 责任 |
| --- | --- | --- |
| 单仓 | 交给对应仓库 Leader，由其拆解和组织本仓工作。 | Manager 始终负责整条 issue，Leader 负责被委派的仓库工作。 |
| 跨仓 | 向涉及的多个 Leader 委派工作，协调依赖及跨仓验证。 | Manager 汇总整体进度、决定和结果，各 Leader 对本仓工作负责。 |
| 尚不明确 | Manager 先调查范围，业务解释按自动化模式处理，再委派。 | 不将来源仓库直接视为完整影响范围。 |
| 执行中发现需要增加仓库 | Leader 向当前 Manager 回报，按授权、模式和计划版本处理。 | 沿用原 Issue 工作归属及实际会话关联，Manager 的总负责身份不发生转移。 |

Manager 完成范围判断后采用短轮委派，具体仓库任务拆解由 Leader 负责，避免重复派工。这里的统一接收不增加人工审批，YOLO 继续按 ADR-0006 自动处理。

### 3.2 总负责人、房间与重试

按 ADR-0019，一个会话可关联多条 Issue；一次创建关联一个来源会话，不据此确认一个 Issue 可关联多个主会话。

- 每条 issue 从接收到结果汇总都由所属项目的 Manager 担任唯一总负责人；单仓执行也不将 issue 主责转交 Leader。
- 复用项目现有 Manager 身份，按业务会话准备、关联或复用主房间；每条 Issue 的计划、执行与验收分别归属。
- 重复消息、创建重试和执行重试保留原逻辑操作及工作归属，不因此另建 Issue 或用户会话。新 Attempt 与真正的新建项分别记录。
- RepoMesh 持久保存会话、Issue 与实际房间的关联。实例准备或建房失败时保留已提交业务事实及原因，沿原操作核查并恢复，避免重复建房。
- 房间存在、邀请成员和模型 session 就绪分别表达。有效消息交给 Manager 后，工作处理须绑定明确事项及版本；多事项角色上下文、实际建房、投递和恢复路径仍待细化与运行验证。
- 所有仓库工作结果汇总回 Manager 主房间，需要用户决定的事项由 Manager 按自动化模式反馈。

### 3.3 Issue 处理流程图

```text
已保存项目内的两个入口：
  会话：保存首条消息及待办 -> 异步准备 -> Manager 讨论 -> 明确建项，受控 MCP
  页面：Issue 栏表单提交 -> 页面创建命令

两入口共用业务创建：
  Issue、主 ChangeSet、来源关联及待办等同事务登记
      |
  复用／恢复实例及来源会话的实际主房间
      |
  Manager 接手明确的 Issue，核验有效计划
      |
      +-- 单仓：委派一个 Leader
      +-- 跨仓：委派多个 Leader
              |
         明确各仓工作归属与实际协作房间
              |
         当前有效计划 + 可核查执行依据
              |
         请求 Worker -> RepoMesh 分配
              |
         独立 Attempt -> 候选与证据
              |
         Agent 初审与正式独立验证 -> Manager 汇总
```

会话可以始终没有 Issue，也可以建立多条独立 Issue。页面创建默认新会话，也可明确选择有权的已有会话；创建后接续准备见 ADR-0018。上述为业务链路，具体 room/session 映射仍待验证，创建成功不等于准备、投递或业务执行成功。

业务解释和计划变更按 ADR-0003、ADR-0004、ADR-0006 的模式处理。修复 Loop 见 ADR-0007，PR 时机见[审查设计](../current/draft-pr-review-design.md)。候选或 PR 不自动表示允许合并或 issue 已完成；ChangeSet 的归属与记录整合见 [ADR-0008](0008-changeset-attribution-and-history.md)，合并及恢复遵守已确认的 [CS9—CS12](../current/changeset-design.md)，具体接口与执行适配仍待验证。

## 4. 团队组织与任务流转

### 4.1 长期组织与短期上下文

- Manager、Team 和 Leader 长期保留。Worker 身份可以复用，执行成员按当前任务和 Skill 动态请求，优先复用合适空闲成员、需要时请求创建；实际分配由 RepoMesh 控制。设置可配置团队 Worker 数量，默认 1，按 A1 的并发上限语义使用；调度、不可用处理及回收见[团队执行策略](../current/team-execution-policy.md)。
- Issue 执行上下文表示角色处理某条 Issue 的目标、计划和结果归属，不是新的角色实例；它与业务会话、实际房间和 runtime session 的映射仍待细化。
- Task 是有明确输入、输出和验收条件的工作单元；Attempt 记录一次执行。
- 确定的任务依赖可以采用 DAG；需要边做边判断的修复过程可采用有停止条件的 Loop。
- RepoMesh 的长期多仓项目，不直接等同于 AgentTeams 内部的每条 `Project` 执行记录；具体映射需要接入适配，不能仅凭同名绑定。

### 4.2 团队组织与流转图

```text
Human
  |
  v
RepoMesh Project P  [Repo A + Repo B + ...]
  |
  +-- Manager M  [one persistent identity]
        |
        +-- Issue context M / I101
        +-- Issue context M / I102
        +-- Issue context M / I103
        |
        +-- Team A  [owns Repo A]
        |     |
        |     +-- Leader LA  [one persistent identity]
        |     |     +-- Issue context LA / I101 / Repo A
        |     |     +-- Issue context LA / I102 / Repo A
        |     |
        |     +-- Worker A1  [reusable identity, allocated on demand]
        |     |     +-- Issue context A1 / I101 / Repo A
        |     |     +-- Task T1 / Attempt 1 -- private clone
        |     |
        |     +-- Worker A2
        |           +-- Issue context A2 / I102 / Repo A
        |           +-- Task T2 / Attempt 1 -- private clone
        |
        +-- Team B  [owns Repo B]
        |     |
        |     +-- Leader LB
        |     |     +-- Issue context LB / I101 / Repo B
        |     |
        |     +-- Worker B1
        |           +-- Issue context B1 / I101 / Repo B
        |           +-- Task T3 / Attempt 1 -- private clone
        |
        +-- Optional project verification group
              +-- Verification Coordinator
              +-- Workers allocated by task
                    +-- I101: Repo A @ SHA-A + Repo B @ SHA-B
                    +-- isolated services / test data / reports
                    +-- no business-repo push or merge permission

Cross-repo flow:
  M/I101 -> LA/I101 -> A1 -> candidate -> LA/I101 -> M/I101
         -> LB/I101 -> B1 -> candidate -> LB/I101 -> M/I101
  M/I101 -> Verification Coordinator -> Worker -> evidence -> M/I101 -> Human
  With group disabled, M organizes independent verification Workers.

Single-repo flow:
  Intake -> Associated Main Conversation -> M/I102 -> LA/I102 -> A2
         -> candidate -> review / verification -> LA/I102 -> M/I102 -> Human
  M owns the issue throughout; LA coordinates the delegated repo work.
```

组织图表示责任与执行归属，图中 M／I101 等记法不冻结 runtime session key 或房间数量。多事项角色上下文仍需验证；不代表所有容器同时常驻或不同上下文必须同时进行模型推理。Worker 在不同时间可参与不同 Issue，但同一时刻只承担一个活跃执行任务。

## 5. 会话与多 Issue 并行

### 5.1 会话边界

用户主会话可以关联多条独立 Issue。Manager 始终负责各 Issue 的总协调；Issue 页面显示本项计划、主 ChangeSet 和执行状态，并导航实际关联的 Manager 主房间及 Leader 房间。Leader 房间目前只读观察，不授予用户直接派工权限。

```text
业务会话 C1 <-> 实际关联的 Manager 主房间
    |
    +-- Issue I101 -> 计划、主 ChangeSet、各仓委派与 Attempt
    |                    +-- 导航实际关联的 Leader 房间
    |
    +-- Issue I102 -> 独立计划、主 ChangeSet、各仓委派与 Attempt
                         +-- 导航实际关联的 Leader 房间

关联会话不合并工作记录；普通讨论可以没有 Issue。
具体 Leader 房间共享粒度及多事项 runtime session 映射待细化。
```

以上为已确认业务关系；主房间按业务会话关联复用，原生资源、消息触发、结果回流和失败恢复仍须验证。不能从一会话多项推导任意多对多关系。

- 一个角色参与多个 issue 时，分别加载各 issue 的目标、计划、决策和进展。
- RepoMesh 负责创建或授权创建房间、邀请成员、设置权限，并持久化 project、issue、repository、agent、room 与 session 的映射。Manager 或 Leader 可以提出建房需求，但不能绕过 RepoMesh 形成无归属的业务房间。
- 房间存在、Agent 被邀请或加入，都不等于其模型 session 已创建。按当前 QwenPaw 路径，Agent 第一次处理该房间内的有效消息时，才按 room_id 查找或创建自己的 session。
- 同一房间中的 Manager、Leader 和 Worker 各自维护本地 session；它们共享房间消息，不共享同一个模型上下文、隐藏推理或未发布的工具结果。
- `@Agent` 用于选择和触发接收者；在新房间第一次有效触发可能导致该 Agent 建立 session，在已有房间再次 `@` 只继续原 session，不产生 Task 或 Attempt 子会话。普通回复或 Matrix thread 也不作为新的 session 边界，除非后续适配显式改变 session key。
- Worker 的协作房间须有明确的仓库工作与 Issue 归属；具体多事项房间映射继续设计。同一 Issue 内的 Task 与 Attempt 由 RepoMesh 任务记录、独立工作副本和执行环境隔离，不为每个 Task 或 Attempt 默认新建房间。
- 只有敏感权限、超大上下文、专项集成验证或其他明确隔离需求，才按策略创建专用 Task / Attempt 房间；该房间仍需登记父 issue、Task、Attempt 和结果回流目标。
- 常设 Team Room 承载团队公共信息，不混放所有 issue 的完整执行上下文。
- 可复用的是经过整理的项目或仓库知识；其他 issue 的临时指令、待批请求和未完成计划不作为共享长期记忆。
- 所有任务通知与结果都显式关联 issue、仓库、任务和尝试，不根据最近聊天猜测归属。
- 新房间需要配套成员权限、消息触发与结果路由；只新建房间或增加一个聊天标签不算完成适配。

### 5.2 事件驱动协调

Manager 或 Leader 完成当前判断与派工后结束本轮协调。任务结果、阻塞、用户输入或其他有效事件到达时，再进入对应 issue 的上下文。

多个 issue 可以同时在途；不同 Worker 可并行执行。即使 Manager 的模型调用短轮串行，也不能让其在等待某个 Worker 时阻塞所有其他 issue。

### 5.3 全局调度约束

1. RepoMesh 后台维护成员占用和执行容量，领取时原子确认；不同协调会话不能各自认为同一成员空闲。
2. 资源不足时进入可解释的队列。初期从少量执行槽位开始，具体数量按服务器配置和构建峰值验证，不在本 ADR 固定数值。
3. 重型集成验证需要多服务资源时，可减少其他构建并发，保护控制服务。
4. 审批模式等待人工回复时，按有效执行映射处理受影响工作并保留记录；YOLO 不建立人工等待。具体分支继续和资源释放规则在 Graph/Loop 与恢复设计中细化。
5. 暂停新派工不停止在途任务。计划变更应用按 ADR-0003 暂停受影响上游 Project 的新派工；ADR-0012 已按 Issue 与仓库委派范围分开上游记录，仍不能承诺同一记录内只暂停某个任务分支。若要求停止进程，需另行确认实际停止结果。
6. 重派后旧尝试迟到的结果保留历史，但不得覆盖当前有效结果或自动推动后续任务。
7. 一项任务的完成仅解除相关依赖，不自动宣布整个 issue 完成。

### 5.4 同仓并行与合入

- 两个 issue 可以分别在本仓独立副本中开发，不因使用同一仓库就强制全程串行。
- 明确存在先后依赖或高风险冲突时，应建立依赖或串行安排，不隐式假定另一 issue 已完成。
- 候选组合、合入顺序和基线变化必须可追踪；无文本冲突也不代表无语义冲突。
- 新基线或组合变化后重新判断并执行必要验证，不能复用失效证据。

## 6. 单服务器执行与隔离

一期采用 AgentTeams 单机容器基础：Manager、Leader 和 Worker 分别运行，控制服务常驻，任务执行按容量启动或回收。模型走外部 API 是当前资源规划假设；若本机还承担模型推理，需重新核算容量。

### 6.1 隔离规则

| 边界 | 设计要求 |
| --- | --- |
| 代码与工作目录 | 每个 Task Attempt 使用独立 clone、分支和构建目录；不挂载整个项目或其他成员的工作区。 |
| 重试与恢复 | 新尝试不默认继承脏工作区；复用已保存候选或检查点时明确来源。 |
| 进程与容量 | 独立容器与显式资源限制；控制磁盘和日志增长，不让单任务耗尽控制服务资源。 |
| 网络 | 禁止任意成员互连，按需放行协作和工具服务；集成服务进入本轮专用网络。 |
| Git 写权限 | 通过受控交付能力验证任务归属，禁止执行成员使用全项目长期高权限凭证。固定 App 权限集、运行时收窄及 draft PR 边界见 [ADR-0002](0002-github-app-authorization-and-draft-pr-delivery.md)。 |
| 产物 | 自己提交区可写，他人的已提交结果只读；已接受证据不能被执行成员覆盖。 |
| 宿主机控制 | Agent 不直接持有宿主 Docker socket；容器创建由可信控制服务校验允许的镜像、挂载、网络和资源。 |
| 长期数据 | 团队身份、配置、经整理知识及历史证据独立保留；临时执行环境可回收。 |

目录命名不是授权，聊天约定不是访问控制，上述约束必须由实际挂载、服务端权限和调度逻辑执行。AgentTeams 默认设置不视为已满足这些要求。

一期优先独立 clone；不把共享 Git 管理数据的 worktree 当作安全边界。后续可优化缓存，但不得把公共可写缓存变成跨任务污染通道。

### 6.2 跨仓验证

仓库 Team 默认只修改本仓，可按授权读取其他仓库的只读快照和契约。启用验证小组时由验证负责人组织专项 Worker，未启用时由 Manager 组织；按固定 commit 组合检出多个仓库，使用隔离测试数据和临时服务。职责和独立性见 ADR-0005，Skill 和资源请求见 ADR-0006。

验证环境可以产生本地构建及测试文件，但没有向业务仓库推送或合并修复的权限。发现问题返回对应 Team 修复，新候选形成新组合后重新验证。

### 6.3 回收与安全等级

- 停止进程、删除工作副本、删除历史证据是不同操作，不应绑定成一次无差别清理。
- 回收临时工作区前确认候选、日志、报告和必要检查点已保存；不能假定尚未同步的数据会自动持久化。
- 同机对象存储不是异机备份，关键状态需要服务器外的备份位置。
- 普通容器共享宿主机内核，不能表述成独占 VM 同级隔离；一期面向可信组织内部协作，不承诺恶意多租户隔离。
- 更强沙箱或 VM 是未来可替换的执行后端，不改变长期团队与 issue 会话模型。

## 7. 多仓库项目的独立实例隔离

### 7.1 已确认的层级

本节已由用户明确确认，不再只是未来部署方向。一个 RepoMesh 可以管理多个独立多仓库项目，每个项目对应一套独立 AgentTeams 实例；实例内部一个 Manager、多支仓库 Team、多个可关联独立 Issue 的用户会话。

| 新增对象 | 创建或分配什么 | 主要边界 |
| --- | --- | --- |
| 独立多仓库项目 | 独立 AgentTeams 实例 | 管理身份、团队、权限、配置与项目数据。 |
| 项目内的仓库 | 本项目的一支 Team | 仓库责任与授权范围。 |
| 项目内的 Issue | 独立计划、主 ChangeSet 和任务记录，关联来源会话 | 工作状态与执行归属，不创建新 Manager；用户会话可关联多项。 |
| 一次任务尝试 | 独立工作副本与执行环境 | 未提交改动、进程及构建状态。 |

“实例”指完整的项目级协作服务与数据，不是一个房间，也不只是 Manager 容器。多个实例可以共用一台服务器，但不因此共享彼此的项目访问权限。

### 7.2 同机多实例部署字符图

```text
One Linux Server
|
+-- RepoMesh                                  [shared platform]
|     +-- Users / Project authorization
|     +-- Project -> Instance mapping
|     +-- Global execution capacity / Queue
|
+-- Project A -> AgentTeams Instance A
|     +-- Embedded Controller A
|     |     +-- Matrix A / Storage A / Gateway A
|     +-- Manager A
|     +-- Team A1 -> Repo frontend
|     +-- Team A2 -> Repo backend
|     +-- Issue rooms / Sessions / Task attempts
|     +-- Network A / Volumes A / Credentials A
|
+-- Project B -> AgentTeams Instance B
      +-- Embedded Controller B
      |     +-- Matrix B / Storage B / Gateway B
      +-- Manager B
      +-- Team B1 -> Repo client
      +-- Team B2 -> Repo service
      +-- Issue rooms / Sessions / Task attempts
      +-- Network B / Volumes B / Credentials B

Authorized request:
  User -> RepoMesh -> Project authorization -> Server-side mapping
       -> Selected instance -> Issue session -> Team / Worker

No default direct access:
  Instance A -X-> Instance B data / credentials / control APIs
```

图中网络阻断和授权是实施目标，需要实际策略与测试保证。初期 Controller、Matrix、存储和网关按实例分开；可复用同一镜像，但不共享可写配置和数据卷。RepoMesh 入口及全服务器容量管理共用，不要求每个项目再部署一个 RepoMesh。

### 7.3 旧版新建项目与实例生命周期（历史时序）

以下编号步骤、图及触发说明是原“首条正式 Issue 后启动”历史时序，只供理解此前取舍，不作为当前规则。现行触发见 ADR-0018：先保存项目；首次会话消息或页面建项提交后异步准备／恢复实例及对应主房间；各自持久化范围见 ADR-0016。业务会话不再整体转正。

1. 用户先保存项目基本信息，RepoMesh 建立稳定项目 ID，项目处于草稿状态；此时不创建 AgentTeams 实例。
2. 用户完成仓库授权，以及首期要求的成员、模型与执行预算配置。必要配置未完成时，不允许正式提交可执行 issue。
3. 配置完成后，项目进入“已配置、实例未创建”的待启动状态；RepoMesh 建立受控的项目到实例映射，但仍不占用本项目的常驻 AgentTeams 资源。
4. 用户正式提交第一条 issue。RepoMesh 先完成来源识别、权限核验、去重和 issue 持久化，将其标记为等待项目准备，再幂等触发本项目实例创建。
5. RepoMesh 创建本项目实例、Manager 和仓库 Teams，并执行服务、身份、权限、消息和存储就绪检查。只有全部必要检查通过，实例才进入 Ready。
6. 实例 Ready 后，为已登记的首条 issue 创建独立主房间，将需求交给 Manager，再进行范围判断、仓库委派和执行；实例准备或建房失败时保留 issue 和已有结果，幂等重试。
7. 后续 issue 复用同一实例。若实例处于保留数据的停止状态，则启动并恢复原实例，不另建一套 Manager 或 Teams。

```text
[Save Project Basics]
        |
        v
[Draft: Required Configuration Incomplete]
        |
        v
[Authorize Repositories + Required Settings]
        |
        v
[Configured: Instance Not Created]
        |
        | Submit first Issue
        v
[Register Issue: Waiting for Project Setup]
        |
        v
[Provision Instance + Manager + Teams]
[Readiness and Permission Checks]
        |
        +-- Failed --> [Setup Failed]
        |                 +-- keep Issue + evidence
        |                 +-- retry same provisioning operation
        |
        v
[Ready: Create Issue Rooms + Route Registered Issue]
        |
        v
[Quiesce Work + Persist State]
        |
        v
[Stopped: Project and Data Retained]
        |
        +---- next Issue ----> [Start Existing Instance + Readiness Checks]

Open Issue page / type draft != submit Issue
Repeated submit / retry      != duplicate Issue or Instance
Stop instance                != delete project or historical evidence
```

这是 RepoMesh 的产品生命周期设计，不是对 AgentTeams 默认安装器自动完成上述流程的承诺。触发点是用户正式提交第一条 issue，不是打开 issue 页面、输入尚未提交的草稿或仅完成仓库授权。前端重复提交、事件重放和人工重试必须复用同一 issue 与同一项目实例准备操作。停机前处理在途任务并确认必要状态已保存；不得以回收资源为由隐式删除项目或产物。

### 7.4 必须隔离的内容

| 内容 | 每项目实例的要求 |
| --- | --- |
| 容器与入口 | 唯一容器名称、网络及宿主机端口；实例地址和服务发现指向正确项目。 |
| 工作与持久数据 | 独立数据卷、Manager 工作目录、成员状态、任务工作区和产物位置。 |
| 消息 | 独立 Matrix 服务及其账号和房间数据；不同服务内的同名角色不视为同一个项目身份。 |
| 凭证 | 独立存储、网关、控制接口凭证；Git 授权只覆盖本项目明确接入的仓库。 |
| 访问路由 | RepoMesh 服务端先验证用户项目权限，再选择实例；客户端不能任意指定实例地址或凭证绕过权限。 |
| 宿主机控制 | 不向项目 Agent 暴露 Docker 控制权；可信控制服务限制可管理容器、挂载、网络和资源的范围。 |

部署时需要参数化名称、目录、端口、服务地址与凭证，并核查单机脚本和后台创建逻辑中的固定配置。仅改一个前缀或重复运行默认安装器，不算完成多实例适配。

实例控制服务仍属于可信管理面。若共用宿主容器引擎，仅分离项目网络和数据卷不足以约束高权限的容器创建接口；必须限制相关 API 的暴露和调用参数。本方案不承诺不同实例之间达到 VM 级或恶意租户级隔离。

### 7.5 全服务器容量与项目额度

- RepoMesh 统一管理全服务器执行上限，各项目再配置各自额度；不能让每个 Manager 都按独占整台服务器计算资源。
- 任务只有同时满足全局容量、项目额度、成员空闲和依赖条件时才能启动。
- 实例数量、处于运行状态的实例数量、活跃 Worker 数量分别统计；额度不足时排队，不能绕过全局限制。
- 容量规划同时计入常驻基础服务、协调 Agent、构建测试和集成服务，不只计算 Worker。
- 允许先登记多个项目、按需启动实例，并对不活动实例保留数据停机。可承载数量待服务器规格与负载验证，不在此承诺固定项目数。

### 7.6 两个项目引用同一远端仓库

项目本地环境隔离，不会复制或隔离远端 Git 仓库。两个项目都获得同一仓库授权时，其远端提交和目标分支仍是共享事实。

- 各实例仍保有各自的团队身份、工作副本和本项目凭证，不能借共享远端仓库读取另一项目的私有会话或产物。
- 任务分支必须能区分项目、issue 和执行工作，避免不同项目重名覆盖。
- 通过受控交付流程协调同一远端仓库的合入、基线更新和必要重新验证；具体机制待实现验证。
- 不能将“本项目环境独立”作为忽略另一项目已合入变更的理由。

### 7.7 取舍与替代方案

初期选择独立实例，接受重复基础服务的内存、维护和启动成本，以避免先开发共享 Matrix、存储、网关及 Controller 的多租户体系。

单实例多 Manager 的资源复用效率可能更高，但当前源码的具名资源支持不足以证明完整隔离。若未来改走该路线，需补齐身份、工作空间、团队归属和权限过滤，并另行记录取代本选择的 ADR；不能在 issue 并行或资源优化中隐式改为共享管理域。

## 8. 上游依据与未验证能力

AgentTeams 源码核查快照：`eeaab64391ccaec9118e84977f538aefd40720d6`（2026-09-05）。以下是本轮只读核查结果，不替代部署和端到端验证，也不保证后续版本保持相同行为。

| 已核查事实 | 设计影响 | 依据 |
| --- | --- | --- |
| Manager 容器可以按资源名称区分，但管理身份并未完整隔离。 | 不以创建多个具名 Manager 作为项目隔离方案。 | [身份与容器命名](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/internal/auth/prefix.go) |
| Manager 创建逻辑仍使用固定 Matrix 用户及 Gateway consumer；Manager 授权为完整访问。 | 独立资源名字不代表独立管理域。 | [身份创建](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/internal/service/provisioner.go#L1345)、[授权](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/internal/auth/authorizer.go#L47) |
| Embedded Manager 启动使用公共工作目录与控制台端口配置；TeamSpec 未提供明确 managerRef。 | 单机多 Manager 还有部署冲突与团队归属问题。 | [启动配置](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/internal/controller/manager_reconcile_container.go#L223)、[资源定义](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/agentteams-controller/api/v1beta1/types.go#L424) |
| QwenPaw 的 Matrix 通道按 room_id 构造 session，并按房间键协调请求；同房不同发送者的消息对同一个 Agent 落入同一房间会话，但不同 Agent 仍各自维护本地会话。 | 多 issue 放同一个 Team Room 不会自动隔离；共同可见消息也不等于多个 Agent 共享同一模型上下文。需建立 issue、角色、房间与会话的显式映射。 | [会话构造](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/plugins/agentteams-matrix-channel/agentteams_matrix/channel.py#L3120)、[房间请求键](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/plugins/agentteams-matrix-channel/agentteams_matrix/channel.py#L425) |
| Manager 已有独立 Project Room、计划和元数据机制。 | 可作为 issue 协调接入基础，但仍需 RepoMesh 的关联和路由适配。 | [项目管理](https://github.com/agentscope-ai/AgentTeams/blob/eeaab64391ccaec9118e84977f538aefd40720d6/manager/agent/skills/project-management/SKILL.md) |

实现前必须验证：

1. 选定运行时能按明确的 Issue 目标正确处理工作；用户会话关联多项时，计划、授权、长期记忆及执行文件不会悄悄串用。实际上下文方案需验证，QwenPaw 的房间键证据不能直接套用到其他运行时或宣称多事项隔离已成立。
2. 会话／页面建项触发后的资源准备、成员授权、真实消息或创建记录接手、Leader 派工和结果回流可完整闭环；仅邀请或加入房间不应被误报成 session 已就绪。
3. 一会话关联多项时，Manager 主房间、Leader 协作房间与各 Issue 执行记录的实际映射不会混用目标，也不破坏 AgentTeams 原生 Team Room 派工或产生重复处理。
4. `@Agent` 只触发目标 Agent 并继续正确的房间 session；未被触发成员的历史缓冲、普通回复和 Matrix thread 不会造成错误新建、错误归属或上下文泄漏。
5. 不同房间模型调用的实际并行行为、限额与失败恢复；不同 session 标识不是并发性能证明。
6. Docker 后端的资源限制、网络策略、挂载、凭证和存储权限真正生效。
7. Worker 重建与工作区清理能保留应保留的数据，又不会恢复不该继承的脏状态。
8. 暂停、停止、重派、超时和迟到结果在多 issue 条件下保持正确归属。
9. 同机创建两个实例时，名称、网络、端口、工作目录、数据卷、凭证及控制请求不串用；项目越权访问被服务端拒绝。
10. 跨实例全局执行容量与项目额度同时生效；停止或恢复一个实例不错误操作另一实例的容器和数据。
11. 同一远端仓库被两个项目接入时，任务分支不会重名覆盖，合入后的基线变化触发必要重新验证。

## 9. 最小验证场景

| 场景 | 预期结果 |
| --- | --- |
| 同一项目的一个会话关联跨仓和明确单仓 Issue | 两项均交同一项目 Manager 总协调，分别保留计划、主 ChangeSet 和执行归属；关联主会话不迫使另建房间，也不合并各项状态。 |
| 创建 issue 房间并邀请 Agent，但尚未发送有效触发消息 | 房间和成员关系已存在，但 UI 不把该 Agent 的 session 标成已创建或就绪。 |
| 仅建立项目、空会话或只读访问 | 业务资料及授权保留，这些动作本身不触发运行实例准备。 |
| 用户首次发送需求 | 保存业务会话、消息和待办后异步准备；实际主房间和 Manager 就绪后投递，普通讨论不必创建正式 Issue。 |
| Issue 栏直接创建 | 原子保存 Issue、主 ChangeSet、来源会话关联及待办等，提交后接续准备；Manager 接手原事项，不重复建项。 |
| 首次消息或页面建项触发准备失败后重试 | 保留业务事实、消息或真实创建记录及失败证据，沿原操作核查恢复，不重复生成事项、会话、实例或 Manager。 |
| 跨仓 Issue 导航实际关联房间 | 入口依据持久关联、当前读权和实际就绪事实；角色消息和结果绑定本项工作，不从标题猜测归属。 |
| 同一 Worker 先后参与两个 issue 的仓库房间 | 两个 issue 的目标和历史不混用；同时只能领取一个活跃任务，另一任务按容量排队。 |
| 普通任务在仓库 issue 房间内派发 | 不额外创建 Task 房间；Task 和 Attempt 仍有独立记录、工作副本、结果与验收证据。 |
| 同一 Leader 同时参与两条 issue | 目标、计划、审批和结果分开；不为每条 issue 新建 Leader 身份。 |
| 两个会话同时申请同一空闲 Worker | 只允许一个成功占用，另一个进入队列；无重复执行。 |
| 同仓两个任务分别修改代码 | 未提交修改、依赖和运行进程不串扰；最终合入前处理基线和冲突。 |
| 审批模式需要用户决定 | Manager 在当前 issue 会话展示问题与影响，决定关联对应事项；Agent 内部复核不逐项请求用户确认。 |
| 计划变更触发暂停 | 按 ADR-0003／0012 的受影响完整上游 Project 范围停止新派工，并显示在途任务；不宣称已支持其中某个任务分支独立暂停。 |
| 重派后旧尝试迟到提交 | 旧结果可追溯，但不能覆盖当前有效结果或解除新计划依赖。 |
| 集成验证后一个仓库候选发生变化 | 形成新组合，不把旧组合的通过结果当作新组合的证明。 |
| 同一 Issue 的工作消息、重复事件或执行重试 | 原操作与事项归属不因页面切换改变；重复事件不重复派工，执行重试记录新 Attempt，Manager 总责保持不变。 |
| 会话主房间创建失败后重试 | 保留已持久化会话及相关事项，核查先前建房结果后恢复原关联，避免重复建房或创建新 Manager。 |
| 同一服务器登记并启动两个独立多仓项目 | 生成两个独立实例及其 Manager、Teams、消息和数据边界，共用 RepoMesh 入口。 |
| 项目 A 的用户或 Agent 试图访问项目 B | 无授权的请求被拒绝，不因篡改实例地址或对象标识而成功。 |
| 项目 A、B 同时申请剩余的一个全局执行槽位 | 最多一个任务获准启动，另一个排队；不各自按本项目容量越过全局上限。 |
| 停止实例 A 后继续使用实例 B | B 不受错误停止、凭证删除或数据清理影响；A 的项目记录和已保存数据保留。 |
| 两项目各自修改同一远端仓库 | 分支与工作副本独立，合入受协调，新基线所需验证不能被跳过。 |

## 10. 取舍与记录

独立项目实例、长期团队及独立 Attempt 保留管理归属和执行隔离，也带来基础服务与工作副本开销。统一 Manager 对人沟通后，RepoMesh 还需保证会话桥接、资源请求、计划许可与实际执行一致。

原有讨论、未采用方案及修改前快照已移至[归档索引](../archive/README.md)。本 ADR 保留 D01–D18；授权见 ADR-0002，计划许可与生效见 ADR-0003，验证职责见 ADR-0005，统一交互和 Skill 动态执行见 ADR-0006。

2026-09-08：用户在审阅“所有新 issue 先进入独立 Manager 主房间、Manager 始终负责整条 issue、Leader 负责被委派仓库工作”的完整规则后回复“是的，采用”。同步 D02、D04、D05、D17、D18 的相关表述、职责、流程和验证场景。被替换的单仓直达及主责转移设计见[历史记录](../archive/2026-09-08-leader-direct-issue-routing.md)。暂停仍沿用 ADR-0003 的首期上游 Project 范围，未执行部署或运行验收。

2026-09-09 文档评审：按 ADR-0019、ADR-0016／0018 的后续补充，将正文和验收同步为会话与事项分离、双入口建项及提交后异步准备；按 ADR-0012 澄清暂停粒度。上述为已有决定的维护，不倒填原确认范围，不新增房间映射或运行保证。
