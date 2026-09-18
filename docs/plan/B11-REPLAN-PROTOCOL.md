# 异常重规划协议（优化版 · Go 版设计）

- 日期：2026-09-15 · 状态：设计定稿，待任务主线编码
- 本文是 Go 版任务主线的编码输入：只含优化后的协议、与动态加仓库的衔接、数据入库边界

## 1. 角色与升级梯子

- **Worker/Runner**：执行中发现"要改的文件不在本仓库"等无法自行解决的情况 → 上报
  `BLOCKED`（附证据：缺失文件/仓库、错误输出）给 TM。Worker 无权判断范围。
- **TM（Team Manager，每仓一个）**：用本仓库全局视野二次过滤——本仓内能解决的自行消化；
  确认超范围 → 上报 Leader（附仓库名单与原因）。
- **Leader**：全组织视野的最终裁决者，触发并主持重规划。

## 2. 七步流程

1. **上报**：Worker 发现异常 → `BLOCKED` → TM。
2. **二次判断**：TM 确认本仓库范围内解决不了 → `BLOCKED` → Leader。
3. **冻结不停摆**：Leader 将计划 v1 标记 `deprecated`（**不取消**，在跑 Task 继续）；
   记录已完成 Task 待迁移；开 **30 秒收集窗**合并并发上报（多 Worker 同时撞墙只触发
   一次重规划；窗口后到达的反馈进入下一轮，不丢弃）。
4. **局部重规划**：反馈确定变更主仓库 → 查**扫描证据图反向依赖**得受影响仓库集合
   （机械计算，不由 LLM 自由发挥）→ 仅对受影响仓库重规划，prompt 钉稳定性约束
   （"其他仓库的 Task 不在本轮范围"）。**前置门**：受影响仓库必须 AutoCard 就绪；
   缺失仓库先走 onboarding（与收集窗并行，不互等）。
5. **版本迁移**：v2 为**完整目标状态快照**；apply 时 diff v1→v2 自动迁移——未变 Task
   保持、修改/重排 Task 旧标 `SUPERSEDED` 新建带新版本、新增创建、删除标 `SUPERSEDED`。
   幂等键重放返回原记录，键含义变更抛冲突；乐观并发（base_version 校验），冲突时
   Leader 重读反馈集合重放。
6. **安全点收尾**：受影响 Runner 在下一个安全点（工具调用完成后）commit 半成品、干净
   退出，**不 kill**；空闲/排队 → 直接 `SUPERSEDED`。未受影响仓库零感知。
7. **执行 v2**：相关 team 按新计划开工。

**机械保障**（不靠自律）：apply 前 v2 batches 过 **DAG 拓扑校验**，不满足依赖即拒；
SUPERSEDED 的 attempt 被投影闸门封死（无法再写入结果）。

**双轴挂钩**：计划换代与决策链**同事务落两笔**——计划版本 +1，决策链落
`status=adjusted` 节点（`upstream_ref` 指向触发本轮的 BLOCKED 节点、
`affected_repositories`=v2 范围、`actor`=Leader）。BLOCKED 上报本身也落决策节点
（`status=blocked`）。梯子每一跳可溯。

**审批闸位（可选）**：preview（v2 diff 视图，持久化）→ 按 supervision 策略决定是否
人工审批 → apply。默认自动，闸位预留。

**状态机**：
```
Task:  queued → running → blocked? → superseded | done
Plan:  active → deprecated(仍推进) → superseded-by-v2
反馈:  blocked → (并入重规划) → adjusted(v2 落地即 resolved)
```

## 3. 与动态加仓库功能的衔接

- 触发特例：反馈 = "缺仓库 X"。X 未注册 → onboarding 并行启动（注册 + 单仓扫描），
  卡片就绪即入受影响集合；X 已注册未圈入 → 直接进集合。
- 人类驱动变体：前端"补充范围"入口——同 requirement 新幂等键 → 决策链 v+1，无需
  LLM 重规划，后端已就绪（零改动）。
- 归档召回：`GET /api/decision-chains?requirementKey=` 展示该需求全史（v1 确认 →
  BLOCKED → v2 修订）；补进来的仓库自动参与相似召回。

## 4. 数据入库现状与待建表

**已就绪（本地库已验证：结构/索引/种子行/写入读回冒烟全通过）**：
- `public.decision_chain_nodes`——决策单（BLOCKED 反馈与范围修订都落这里，不另建反馈表）
- `public.decision_embeddings`——向量（主能力 vector(1024)+HNSW，JSON 兜底双写）
- `public.feature_settings`——前端开关（种子行 `decision_chain.enabled=true`）
- 服务器库上这三张随迁移 0008 自动创建，无需手工建表。

**⚠ 待建表（协议本体的任务轴/计划轴，随任务主线编码落地；开发完成后提醒执行建表迁移）**：

| 表 | 承载 | 设计来源 |
| --- | --- | --- |
| `execution_plans` | 计划轴：版本、batches、DAG、状态（active/deprecated/superseded） | 本文 §2 步骤 3-5 |
| `plan_tasks` | 任务实体：版本、仓库、状态机、SUPERSEDED 迁移 | 本文 §2 步骤 5 |
| `task_assignments` | 占位状态机：generation、attempt 状态、投影闸门 | 本文 §2 步骤 6 |

反馈/BLOCKED/收集窗的记录**不建独立表**——统一落 `decision_chain_nodes`
（`status=blocked`/`adjusted`），一条链讲完整个故事。

## 5. 完整流程说明（端到端走一遍）

1. issue 开工，Leader 产出 **v1 计划**（网关 2 个 Task、SDK 1 个 Task），按仓库组建
   团队（TM + Runner），派单执行。
2. 网关队 Runner 干到一半：要改的文件在 SDK 仓库 → 打 **BLOCKED 报告**（附证据）给 TM。
3. TM 用仓库全局视野过滤：确实出不了本仓库 → 上报 Leader。
4. Leader 标记 v1 `deprecated`（在跑任务继续干），开 30 秒窗口；窗口里又收到 SDK 队的
   反馈，合并成一份反馈清单；每条反馈落决策链（`blocked`）。
5. 反馈定主仓库=网关 → 查依赖图 → 受影响集合={网关, SDK, 公共库}；若缺的仓库未注册，
   并行触发 onboarding（注册+扫描）等卡片就绪。
6. LLM 仅对这三家局部重排（prompt 钉死其他仓库不动），产出 **v2 完整快照**；
   DAG 拓扑校验通过。
7. apply：计划版本 +1，决策链落 `adjusted` 节点（指回触发反馈）；受影响仓库旧 Task 标
   `SUPERSEDED`、新 Task 带新版本创建。
8. 受影响 Runner 在安全点 commit 半成品、干净退出（不 kill）；空闲的直接 SUPERSEDED；
   未波及的仓库无感。
9. 各队按 **v2** 继续执行；事后在历史决策页可查全史：v1 谁确认 → 谁几点撞墙 → 收到
   哪些反馈 → v2 改了什么、谁批的。

**人类变体（无需 LLM）**：用户在需求上点"补充范围"→ 同 requirement 新幂等键重新圈定 →
决策链 v+1——与协议走同一套记录层。
