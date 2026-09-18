---
status: accepted
date: 2026-09-08
updated: 2026-09-09
---

# ADR-0011：AgentTeams 受控工具与执行接入

RepoMesh 需要实际落实计划许可、有效权限、预算、Worker 单活跃任务和独立 Attempt。采用受控适配方向：保留 AgentTeams 的长期团队、通信和运行生命周期体系，由 RepoMesh 控制工具请求与实际执行入口；必要时维护范围明确的 TeamHarness／runtime 或相关上游补丁，并通过锁版本的契约验证维护兼容性。

本决定承接 [ADR-0010](0010-technology-stack-and-modular-monolith.md) 的技术栈与模块化单体，以及 [ADR-0001](0001-agentteams-issue-concurrency-and-isolation.md)、[ADR-0003](0003-plan-change-authorization-and-activation.md)、[ADR-0006](0006-manager-entry-modes-and-skill-driven-execution.md) 中 RepoMesh 实际控制执行的要求。当前只确认设计方向，未开始编码、安装、部署或远端操作。

## 采用的接入方向

- 继续使用每项目独立 AgentTeams 实例，复用长期 Manager、每仓 Team／Leader 和 Worker 身份。Manager 负责业务判断及整条 Issue 的协调，Leader 负责被委派仓库工作。
- Agent 提出的计划、任务、资源或交付请求进入 RepoMesh 的受控业务入口。RepoMesh 核验身份与归属、当前有效权限、计划许可与生效状态、预算及容量等适用条件，再由受控路径落实动作并核查结果。
- 复用 Controller REST、Matrix 和 TeamHarness 已有能力；缺少受控任务接入时补充运行时工具桥接。必要时可以设计和维护小范围上游补丁，不以完全不改上游作为必须满足的限制。
- 实际动作不能通过直连高权限 Controller、改写正式控制记录、使用宽权限 Git 凭据或宿主控制接口绕过上述规则。仅在提示词或 MCP 工具外增加一层约定，不能代替凭据、存储、网络和执行端的实际约束。
- 适配保留 AgentTeams 必需的心跳、唤醒、通信及协作能力。补丁的目的、对应上游版本、依赖接口与验证结果需要可追溯；升级后重新核查受影响契约，不假定路径名称或 DTO 字段存在就表示能力有效。

这些约束用于落实已有权限与执行规则，不扩大 Agent 的权限。GitHub App 固定工作权限集、运行时收窄、Agent 任务分支与 draft PR、人工转正式 PR 和人工合并继续按 ADR-0002 执行。当前 YOLO 不增加人工执行审批等待；不具备有效能力时返回真实受限或未完成结果。

## 为什么作出这个取舍

锁定提交的 [API／CLI 调研](../agentteams-survey-2026-09-07/agentteams-api-cli-survey-2026-09-07.md) 显示：Controller REST 提供资源管理及 Project 观察／干预，任务委派、接收和提交主要位于运行时内的 TeamHarness stdio MCP；Manager 具有较广的上游管理权限，部分 projectflow 限制依赖工具注入和提示。现有 REST 调用加提示词不足以证明 RepoMesh 的执行约束成立。

因此接受受控适配和必要补丁的维护成本，将校验落实到动作发生的位置。没有采用“保持默认执行路径，只要求 Agent 自觉遵守 RepoMesh 规则”的方案。保留 AgentTeams 的团队和运行基础，避免为这些控制要求重新实现整套协作体系。

例如，支付状态筛选的 backend 修复任务已排队，但其权限在启动前失效：受控执行路径重新核验并拒绝对应操作，Manager 汇总受限原因。Leader 不能通过原生 taskflow 或宽权限 Controller 绕过；仍获准且无依赖的 frontend 工作按既定规则继续。具体检测时效、拦截和结果核查仍需验证，pause 不被当作在途进程已停止的证明。

## 实施影响与待细化内容

- 需要核查真实工具调用、状态写入、凭据和进程启动路径，并验证旁路请求会被拒绝。无法约束的路径不能宣称满足正式执行要求。
- 具体补丁位置、工具协议、身份传递、占用代次、网络及存储授权、单机容器配置和停止机制尚未冻结；不能声称现有 Worker POST／PUT 已暴露完成这些工作的全部字段。
- 本次确认仅针对受控适配深度，不将[架构 v1](../current/architecture-design-v1.md) 整稿一并冻结。后续已分别采用 Issue 上游映射（ADR-0012）、三类进程（ADR-0013）、Graph 进程内运行（ADR-0014）、获准轮次有限 DAG（ADR-0015）及持久化／资源预留方向（ADR-0016／0017）；其余模块、具体协议和恢复算法仍需细化。
- 原接入决定基于 eeaab64391ccaec9118e84977f538aefd40720d6 的静态调研；2026-09-09 重新核对最新 main 仍为该提交。后续已有组件及有限真实 HTTP／MinIO 证据，尚无 RepoMesh 受控执行全链路验收，范围见 [Graph／Loop 专题](../current/graph-loop-design.md)。同日用户采用复用原生 DAG，细化见 ADR-0014／0015 补充；复用不取消本 ADR 的实际路径控制，也不表示已正式接入尚未验证的 coding harness。
- Skill 工程仍按 ADR-0009 整体暂缓，仅保留文档位置；本决定不启动 Skill 管理、发布、分发、加载或版本隔离。审批能力仍后续开发。

## 确认来源

后续进展分别按上文列出的 ADR 确认；这些后继决定不倒填为本次受控接入选择已同时批准全部架构。

用户在架构 v1 方案及最后一个问题“是否接受在保留 AgentTeams 团队体系的前提下，对其工具和执行入口做受控适配，必要时维护小范围上游补丁？”之后回复：“按照你的建议来”。据此采用上述接入方向，继续设计与本地 Markdown 记录。
