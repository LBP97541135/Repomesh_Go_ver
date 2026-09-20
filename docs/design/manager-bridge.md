# Manager 双向桥 · 设计方案

- 分支：`feat/manager-bridge`（worktree `Repomesh_Go_ver_managerbridge`）
- 日期：2026-09-20
- 状态：定稿待实现（① 已在分支上）
- 裁定人：小陈

## 1. 目标

任务执行途中，人在与 Manager 的聊天室（工作台主会话）里发消息，与 AgentTeams 的
Manager 真实对话：更迭需求、调整计划、追问进度、给反馈建议——有来有回、有充足
信息交互。不造新会话环：Manager 本来就是活的 LLM agent，缺的只是把人接进它的
房间这一座桥。

## 2. 现状与依据

- 人在工作台发消息的通道现成：`POST /conversations/{id}/messages`
  （幂等 submission → 落会话流）；前端 `submitMessage` 在用。
- AgentTeams Manager 是**实例级**协调 agent（一个实例一个，不是每队一个），
  有 model/skills/MCP/status.room。
- 每支**队**有自己的 `TeamRoomID`（Team CR status，`GetTeam` 已封装）——按 team
  隔离，是人的消息的默认收件人（天然不串 issue）。
- `SendMessage`（Matrix PUT，txnID 幂等，支持 masquerade 指定发言身份）与
  `RoomMessages`（读房间近期消息）均已封装于 `internal/agentteams/matrix.go`。
- 计划换代机制现成：plans 全量快照替换 + revisions 历史 + 协调器按新版本重排。

## 3. 架构

```
人（工作台聊天室）
   │  POST /conversations/{id}/messages（既有）
   ▼
RepoMesh 会话流（唯一真相源）──────────────┐
   │ 正向桥（协调器）                        │ 反向桥（协调器轮询）
   │ masquerade 成该用户                     │ event_id 去重 + 过滤桥身份
   ▼                                        │
AgentTeams 队房（TeamRoomID）◀──────────────┘
   │ Manager（真实 agent）在房间里读到人的话
   ▼
按其 skills/MCP 行动：回答 / 调整 AgentTeams 侧 workflow / …
   │
   ▼
④ 回写：桥检测 workflow 变化（Workflow() 读面 diff）
   → 走既有 plans 全量快照替换通道 → 新版本 + revision
   （换代历史 / 决策链 / 审计自动继承）
```

## 4. 对抗性审查结论（已定稿的五个坑）

| # | 坑 | 裁定 |
|---|---|---|
| ① | 双写不原子（RepoMesh 流 + Matrix 房间无事务） | 以 RepoMesh 会话流为**唯一真相**；Matrix 发送成功才标"已送达"，失败就地标重试提示，不假装送达 |
| ② | 回声循环（桥发出去又被拉回来） | 反向桥按 Matrix event_id 去重入库 + 过滤桥自己的身份；Controller 重启丢 sync 游标也靠这张去重表兜底 |
| ③ | Manager 实例级，多 issue 串台 | 人的消息默认发**队房**（TeamRoomID，按 team 隔离）；Manager 房间仅无队兜底；队房内多 issue 并行再加 issue 标签前缀二道保险 |
| ④ | 两个计划真相源（AgentTeams workflow vs RepoMesh plans） | **完整方案（用户拍板）**：桥把 Manager 侧计划变更回写成 RepoMesh plans 换代——workflow diff → 全量快照替换 → revision |
| ⑤ | 桥的身份与宿主 | 桥放协调器（遵守 A3/A4 退避）；Matrix 凭据走服务身份 + masquerade 成发言的用户，Manager 才知道是谁在说话 |

## 5. 时序（用户问的"分发后才接入？"）

**是——现状约束就是物化（建队）后才有房间可桥**（建队时机已由
`20c63ba1` 收敛为"逐仓确认接入"，不动）。物化前的消息照旧落 RepoMesh 流；
桥启动时把**未同步的人消息**补发给 Manager 当上下文——规划期说的调整意向不丢。

## 6. 实施顺序（每步独立可验）

1. **① 适配器补 `GetManager`**（`/api/v1/managers/{name}` status 拿房间号；
   `internal/agentteams/managers.go`，无队兜底收件人）。
2. **② 正向桥**：会话流新消息 → masquerade 成该用户 → 发队房；txnID 幂等；
   Matrix 确认才标"已送达"；未同步消息在桥启动时补发（见 §5）。
3. **③ 反向桥**：协调器轮询队房（event_id 去重 + 过滤桥身份）→ 写回会话流；
   前端聊天室零改动即可显示 Manager 回话。
4. **④ 回写**：`Workflow()` 读面 diff 出计划变更 → plans 全量换代 + revision。
5. 验收：真 issue 走一轮"人说话 → Manager 回 → 计划变 → revision 落"。

## 7. 明确跳过的（ponytail）

- 不造新会话环/新状态机：Manager 的智能全是 AgentTeams 的，桥只搬消息。
- 不提早建队：撞队友 `20c63ba1` 的收敛，不干。
- 不做"语义归类"前置：自由对话，动作由 Manager 自己决定（④ 负责把结果落回）。
- 高危动作工具（驳回任务/暂停项目）本版不接，跑通低危再说。

## 8. 待线上验证的两个假设

- Manager 是否响应**队房**里人的话（vs 只在实例房间）——上线一测便知；
  不响应则降级为"发 Manager 房间 + issue 标签前缀"。
- `ManagerView.Room` 的 json 字段名（`room`）按调研 status 表先写，
  首次真实负载核对。

## 9. 开工侦察记录（②③④ 实现前的现场事实，2026-09-20）

- 消息提交链：`internal/messages/submit.go`（幂等 submission →
  `repomesh_messages.message_submissions`，含 canonical/receipt/message_id）；
  会话卡片落 `repomesh_issues.conversation_cards`（0017，source_id 唯一键，
  FK 到 page_sources.operation_id）。**正向桥的"已送达"标记加在
  message_submissions 或新桥表，不动 conversation_cards**（它是渲染投影）。
- 桥宿主：`cmd/repomesh-coordinator/`（roomnotice.go 已有"往房间发通知"的
  先例可抄：AgentTeams client 的构造、重试、A3/A4 退避节奏都在那）。
- issue→team→房间：issue 详情带 `agentteams_team_name`（contract.ts:208）；
  `GetTeam(name).TeamRoomID` 拿队房（workers.go:141）。Manager 兜底房：
  `GetManager(name).Room`（本分支 ① 新增，managers.go）。
- masquerade：`matrix.go:44 MasqueradeAs`（?user_id=，appservice 代指定身份），
  由 `Service.ActAs` 注入（matrix.go:239）——部署配置需补一个"桥以谁的身份发言"。
- 反向读：`RoomMessages`（GET /sync + room filter，直打 homeserver，
  Controller REST 无按 roomID 读消息）——去重键 = Matrix event_id，新桥表。
- 回写 ④ 的换代通道：`internal/plans`（全量快照替换 + revisions，
  `pipeline.go:39` 注释确认"执行中换代会生成 v2"）。

### ② 的最小落地形状（下一步直接照此写）

1. 迁移 00XX：`repomesh_messages.bridge_deliveries`（submission_id PK、
   matrix_event_id 唯一、方向 human→room / room→human、状态 pending/sent/
   received、txnID）。同一张表兼做 ② 的送达标记与 ③ 的回声去重。
2. 协调器加一个循环（抄 roomnotice 的退避）：扫 pending 的 human 消息 →
   `GetTeam` 拿队房 → `SendMessage(masquerade=人)` → 成功置 sent+event_id。
   物化前无队 → 留 pending（桥启动时自然补发，即 §5）。
3. ③ 循环：轮询队房 → event_id 不在表里且 sender≠桥 → 写回会话流
   （走 messages 包的既有落卡路径，标注来源 manager）+ 置 received。
