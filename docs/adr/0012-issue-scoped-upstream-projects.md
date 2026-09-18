---
status: accepted
date: 2026-09-08
updated: 2026-09-09
---

# ADR-0012：按 Issue 仓库委派范围映射上游 Project

> 关联替代（2026-09-09）：[ADR-0019](0019-conversation-issue-separation.md) 已确认一个用户会话可关联多条独立 Issue，替代本文将“一个主房间／主 ChangeSet／整体计划”并列为一一关系的入口解释。上游 Project 仍按正式 Issue 与仓库委派范围隔离；会话关联多项不合并执行记录。内部仓库事项与上游委派记录的具体映射、Leader 房间共享粒度尚未确定。

RepoMesh 采用“每条 Issue 按仓库委派范围分别建立 AgentTeams／TeamHarness Project，跨仓依赖及整体计划由 RepoMesh 管理”的映射方式。不同 Issue 不共用一个长期上游 Project，以减少一次重规划对其他 Issue 的影响；接受由 RepoMesh 协调多个上游对象及其部分应用结果的成本。

本决定在 [ADR-0011](0011-agentteams-controlled-integration.md) 的受控接入方向上明确执行记录的组织粒度，沿用 ADR-0001 的长期团队和执行归属隔离、ADR-0003 的许可与生效、ADR-0008 的稳定 ChangeSet；用户主会话关联按 ADR-0019。当前只确认设计，未编码、部署或运行验收。

## 已采用的映射

- RepoMesh Project 继续表示用户的长期多仓项目，每项目拥有独立 AgentTeams 实例。AgentTeams Project 表示某个团队范围内的工作流记录；新增这类记录不新建 Manager 或整支 Team。
- 每条 Issue 按参与仓库的委派范围分别建立上游 Project。即使两条 Issue 使用同一仓库和长期 Team，它们的上游工作流记录也分开。
- Manager 始终负责整条 Issue；用户通过 Issue 专页查看本项主 ChangeSet 和整体计划，并导航实际关联的 Manager 主房间与 Leader 房间。一个用户主会话可以关联多条 Issue，各项工作仍分别归属。各仓 Leader 负责本仓委派工作，跨仓依赖和整体进展由 RepoMesh 管理。
- 房间承载协作对话，上游 Project 承载任务与执行状态，二者显式关联。上游对象引用需确定实例、team 和 project_id，并关联所属 Issue 与委派范围，不仅凭标题或同名 ID 猜测归属。
- 验证工作有明确的执行范围和归属，关联固定组合及独立 Attempt。验证小组仍默认关闭；验证记录具体挂到哪个上游 Team／Project 的技术方案另行细化，不因该映射决定启动常驻验证小组。
- 多条 Issue 仍共享所属 Team 的 Worker 并发额度，并受项目额度和全局容量限制。执行记录分开不代表每条 Issue 各自获得一份团队额度，也不证明运行时并行和隔离已经实现。

## 支付状态筛选示例

~~~text
RepoMesh Project：订单系统
  +-- 一个独立 AgentTeams 实例
  +-- 长期 Manager、frontend Team、backend Team
  |
  +-- Issue A：支付状态筛选
  |     +-- 本项主 ChangeSet / 整体计划；关联用户主会话
  |     +-- frontend Team 内的 A 工作流记录
  |     +-- backend Team 内的 A 工作流记录
  |     +-- 固定组合的验证执行记录
  |
  +-- Issue B：订单导出
        +-- 本项主 ChangeSet / 整体计划；主会话可与 A 相同
        +-- backend Team 内的 B 工作流记录

A、B 复用 backend Team，但不合入同一个上游 Project。
跨仓依赖和整体计划由 RepoMesh 管理。
~~~

若 A 的 frontend 候选 F1 已完成，而 backend B1 需要修复，保留 F1 和历史。需要重规划时，处理 A 对应的受影响上游 Project；B 不因为属于同一仓库就必须一起重规划，但仍可能因共享 Worker 或服务器容量而排队。依赖新后端结果的组合验证等待 B2，其他仍获准且无依赖的工作按既定规则继续。

## 暂停与多目标应用

需要 replan 时，仍按受影响的完整上游 Project 停止新派工、等待在途收尾、核对 submitted、应用并读回。pause 不代表在途进程已停止，也不承诺任意分支独立暂停；实际共享资源或接入约束造成更大影响时必须如实处理，不能凭逻辑映射声称完全隔离。

一份整体 Plan Version 可能对应多个上游目标，其写入不具有跨系统原子性。前端安排写入成功、后端失败时，保留各目标的真实结果，受影响范围保持停止新派工；核查和处理失败目标，全部必要目标一致后才宣布新 Plan Version 生效。不能在已被部分改写的范围盲目继续旧计划，也不能把第一次写入成功当作整体生效。未受影响且仍获准的工作按现行规则继续。

## 取舍与待细化内容

锁定提交的 [API／CLI 调研 §3.3](../agentteams-survey-2026-09-07/agentteams-api-cli-survey-2026-09-07.md#33-project-工作流读取与人工干预) 显示，上游 Project 身份包含 team 和 project_id；replan 遇到 in-progress／submitted 拒绝，pause 不停止在途执行。将不同 Issue 放入同一个长期 Project，会让它们在这些操作上互相影响，因此采用按 Issue 与仓库委派范围分开的记录方式。

代价是增加上游记录、映射维护和多目标核查工作。具体标识格式、Plan Version 更新时的复用、Task／Attempt 映射、验证工作的上游归属、采集协议、并发保护和部分应用的恢复算法继续细化；不新增独立 IssueRun 产品实体，不冻结表、字段或接口，也不将架构 v1 的全部建议一并确认。

## 确认来源

用户先要求“先解释我再采用”。在说明两个 Project 的区别、支付状态筛选与订单导出的映射、共享 Worker 额度、后端局部受影响以及多目标部分应用的处理和代价后，用户回复“采用”。据此确认上述映射方向与相关行为。
