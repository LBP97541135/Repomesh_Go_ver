# B07-B11 复核续报（执行口径与闭合建议）

复核方式：只读 Markdown、源码、迁移与现行契约。没有改仓库，没有启动服务、数据库、浏览器或容器，没有运行产品测试。
复核快照（md5 为本次逐行引用的版本）：

| 文件 | mtime | md5 |
| --- | --- | --- |
| foundations.md | 2026-09-15 01:30:18 | 57ff1551ff83196efef7667297dfa5b0 |
| b07.md | 2026-09-15 01:33:37 | 231677b89c894bca8bbc6df7d2841928 |
| b09.md | 2026-09-15 01:21:21 | 626581854a5b7cc82843c10361050bba |
| b10.md | 2026-09-15 01:28:54 | 4f8a53f9a0a46e3c0363d5a1022b6685 |
| b11.md | 2026-09-15 01:28:20 | a6a02d92179cf8961bd1df79592ca3fd |

根代理已决定：P1 采用方案 B；P3 必须同时约束同 Issue 与同仓，使用 issue_id 加 repository_id 的复合链；P5 只把最终 admit 与 reject 写进 result_admissions，needs_recheck 作为返回状态并沿用现有 DurableWork 与 Attempt 等待核查，不新增通用表。下面按这三个决定给出闭合写法，再列其余确认问题。

## 一、P1 闭合：每仓不可变修订加 round_plan_pointers

现稿问题：`docs/api-database/b10.md:229` 的 `rounds.current_plan_revision_id` 是单指针，`docs/api-database/b10.md:264` 的 `plan_revisions` 每条只绑定一个仓库，`docs/api-database/b11.md:104` 与 `:186` 又要求同一轮多仓各建一条修订。多仓轮次里只有一条修订可被单指针指向，其余仓库没有当前计划可解析。

按方案 B 修改。

1. 删除 `rounds.current_plan_revision_id` 与 `docs/api-database/b10.md:236` 的延迟外键说明。`rounds` 保留 `id`、`project_id`、`issue_id`、`round_index`、`purpose`、`state`、时间列，并补 `UNIQUE(id, issue_id)` 供下游复合外键引用。
2. `plan_revisions` 保留 `round_id` 与 `repository_id`，补 `issue_id`，并补 `UNIQUE(round_id, id, repository_id)`。这样指针可以一次校验轮次、修订与仓库。`docs/api-database/b10.md:270` 现有的 `(round_id, revision_index)` 与 `(round_id, id)` 保留。
3. 新增 `round_plan_pointers`。

```sql
round_plan_pointers(
  round_id text NOT NULL,
  issue_id text NOT NULL,
  repository_id text NOT NULL,
  current_plan_revision_id text NOT NULL,
  pointer_revision bigint NOT NULL DEFAULT 1,
  updated_at timestamptz NOT NULL,
  PRIMARY KEY (round_id, repository_id),
  FOREIGN KEY (round_id, issue_id) REFERENCES rounds(id, issue_id),
  FOREIGN KEY (issue_id, repository_id)
    REFERENCES repomesh_issues.issue_repository_scope(issue_id, repository_id),
  FOREIGN KEY (round_id, current_plan_revision_id, repository_id)
    REFERENCES plan_revisions(round_id, id, repository_id)
)
```

4. 插入顺序改为 `rounds`、`plan_revisions`、`round_plan_pointers`。指针在第三条语句时已指向存在的修订，不需要 `DEFERRABLE`，`docs/api-database/b10.md:514` 的延迟指针用例改成多仓指针用例。指针更新用 `pointer_revision` 做 CAS，只允许换成本轮同一仓库的另一条不可变修订。
5. 接口后果写明：多仓轮次由 `PlanRound` 按仓库各调用一次并复用同一 `RoundID`（或改成一次接收多仓的批量命令）；`docs/api-database/b11.md:49` 的 `EvaluateRound` 改为按 `RoundID` 读取全部指针行，跨仓边仍在轮次级判断；`CloseRound` 检查该轮全部指针行与全部修订下的任务，而不是一条修订。
6. B11 的 `target_applications` 不改引用方式，继续用 `plan_revision_id`。这正是方案 B 比方案 A 改动小的原因；选 A 需要把 `docs/api-database/b11.md:227` 改成引用 `(plan_revision_id, repository_id)` 子关系，并改写 §3.5 与 G5-15。

`docs/api-database/b10.md:229`、`:236`、`:446`，`docs/api-database/b11.md:14`、`:104`、`:186`、`:410`、`:411` 都要同步。

## 二、P3 闭合：同 Issue 同仓的复合外键链

现稿问题：`docs/api-database/b10.md:298` 的 `attempts.repository_issue_id` 与 `docs/api-database/b10.md:311` 的“仓库必须等于任务所属计划修订的仓库”只写在事务里。只加两条 repository_id 外键仍然不够，Issue A 的 repo R 与 Issue B 的 repo R 会串线，因为 `repository_issues(id)` 与 `execution_tasks(id)` 都是单列主键。

按根代理要求改成 Issue 加仓库的复合链：

1. `repository_issues` 保留 `UNIQUE(issue_id, repository_id)`，补 `UNIQUE(id, issue_id, repository_id)` 作为引用目标。
2. `plan_revisions` 补 `issue_id`，补 `UNIQUE(id, issue_id, repository_id)`，并以 `(round_id, issue_id)` 引用 `rounds(id, issue_id)`，以 `(issue_id, repository_id)` 引用 `issue_repository_scope(issue_id, repository_id)`。这样计划阶段也不能引用别的 Issue 的仓库。
3. `execution_tasks` 补 `issue_id` 与 `repository_id`，补 `UNIQUE(id, issue_id, repository_id)`，并以 `(plan_revision_id, issue_id, repository_id)` 引用 `plan_revisions(id, issue_id, repository_id)`。
4. `attempts` 补 `issue_id` 与 `repository_id`，加两条外键：`(task_id, issue_id, repository_id)` 引用 `execution_tasks(id, issue_id, repository_id)`，`(repository_issue_id, issue_id, repository_id)` 引用 `repository_issues(id, issue_id, repository_id)`。

两条外键同时成立时，Attempt 的任务与仓库事项必须落在同一 Issue 的同一仓库，单靠 repository_id 的串线被拒绝。`docs/api-database/b10.md:311` 的散文检查改成约束说明，并在 B10-G4 增加一条“同仓但不同 Issue 必须被拒绝”的破坏性用例。

## 三、P5 闭合：result_admissions 只写最终裁决

现稿问题：`docs/api-database/b11.md:293` 的 decision 含 `needs_recheck`，`docs/api-database/b11.md:297` 的唯一键是 `(result_ref, attempt_generation)` 且表只增不改。证据稍后到达时，同一结果与代次不能再写 admit 或 reject，需要复核的裁决变成终态。

按根代理决定修改：

1. `docs/api-database/b11.md:293` 的持久取值只保留 `admit_for_current_round` 与 `reject_stale`。
2. `needs_recheck` 作为 `DecideResultAdmission` 的返回状态与原因码保留，不插入 `result_admissions`，不解锁依赖也不推进任务。
3. 需要复核时沿用现有 DurableWork 或 Attempt 的等待与再领取机制，不新建表。
4. `(result_ref, attempt_generation)` 唯一键保留，用于最终裁决；证据到达后写入第一条最终裁决，之后的重复或冲突写入被拒绝。

## 四、其余确认问题

### P2 中｜manager_sessions 的可空房间仍缺 Issue 归属

位置：`docs/api-database/b09.md:381`、`:390`，`docs/api-database/b07.md:207`。

房间列可空本身合理，普通会话先于 Issue 存在。但 `manager_sessions` 没有 `issue_id`，无法用 `docs/api-database/b07.md:207` 新增的 `UNIQUE(project_id, issue_id, room_link_id)` 建立复合外键，也无法判断同一会话关联多个 Issue 时指向哪个 Issue 的房间。

最小修改：`manager_sessions` 增加可空 `issue_id`，加检查约束 `(issue_id IS NULL) = (room_link_id IS NULL)`，并用 `(project_id, issue_id, room_link_id)` 引用 B07 的新唯一键。若允许同一会话同时绑定多个 Issue 的房间，则把房间绑定拆成独立关系，不放在会话行上。

### P4 中｜target_applications 的 upstream_binding 重复且唯一键过宽

位置：`docs/api-database/b10.md:265`、`docs/api-database/b11.md:214`、`:227`。

`plan_revisions.upstream_binding` 与 `target_applications.upstream_binding` 是同一事实的两份拷贝；`docs/api-database/b11.md:227` 说每条修订只绑定一个仓库，但唯一键 `(plan_revision_id, upstream_binding)` 允许一条修订对应多个目标。

最小修改：`target_applications` 删除 `upstream_binding`，唯一键改为 `(plan_revision_id)`；需要读回版本时只保留 `base_revision`、`applied_revision` 与证据摘要。若保留该列作为应用时的读回值，就必须明确它不是权威绑定并同样改为 `(plan_revision_id)`。

### S1 中｜manager_sessions 缺“当前代次”标记

位置：`docs/api-database/b09.md:376-390`。

正文写“部分唯一索引保证每个会话至多一个当前活动代次，旧代次保留但失去写权限”，但表里没有 `superseded_at`、`retired_at` 或当前标记，`state` 枚举也没有退役值。部分唯一索引没有可写的谓词，旧代次失去写权限也没有可执行依据。

最小修改：加可空 `superseded_at`，部分唯一索引写成 `(project_id, conversation_id) WHERE superseded_at IS NULL`，写路径要求 `superseded_at IS NULL`；或把历史代次移出本表。

### S2 中｜repository_issues 的停用写入者与跨轮复用冲突

位置：`docs/api-database/b10.md:240`、`:253`，`docs/api-database/b11.md:86`。

`docs/api-database/b10.md:240` 把停用写入者写成“轮次收尾”，`docs/api-database/b10.md:253` 又说后续轮次、任务与再次委派复用同一 id。若每轮收尾都置 closed，下一轮修复无法在同一事项下登记新工作；若不置，停用写入者没有实现位置，B11 的 `CloseRound` 也只更新 `rounds.state` 与 `round_transitions`。

最小修改：停用只发生在 Issue 工作结束（或明确由 Issue 级维护入口触发），轮次收尾不改 `repository_issues.state`；如需在轮次内暂停，用独立的暂停事实表达。

### S3 中｜attempts 的“非终态”集合与替补条件没有定义

位置：`docs/api-database/b10.md:301`、`:311`。

正文用“非终态 Attempt”写部分唯一索引，但没有列出哪些 state 是终态；同一句又要求“写入能力未确认失效前不能开替补”，这个条件还依赖 `stopped_observed_at` 与 `write_capability_revoked_at`，单一 state 索引表达不了。

最小修改：明确终态集合，并给替补条件增加可索引字段（例如 `blocks_replacement` 生成列或显式的 `replacement_allowed` 标记），在 B10-G4 增加“旧 Attempt 终态但写能力未撤销时替补被拒绝”的用例。

### S4 中低｜target_applications 的 state 与 recovery_state 没有转移规则

位置：`docs/api-database/b11.md:208`、`:216`、`:222`、`:355`，`:145`。

完成条件要求 `state=readback_ok` 且 `recovery_state` 不是 `open` 或 `unknown`，但 `RecoverTarget` 只声明“读回核查”，没有写清成功读回后由谁把 state 改成 `readback_ok`。`unknown` 同时出现在两个枚举里，优先级不明。

最小修改：补一张状态转移表，指明 `ApplyPlanRevision`、`RecordPartialApplication`、`RecoverTarget` 各自允许写的 state；让两个枚举职责不重叠（例如 state 只表示应用与读回，recovery_state 只表示恢复），并明确恢复成功如何满足完成条件。

### S5 中低｜message_deliveries 写了枚举外的 unknown

位置：`docs/api-database/b09.md:425`、`:431`、`:436`。

`state` 枚举没有 `unknown`，但 `docs/api-database/b09.md:436` 写“探测失败写 unknown”。`last_observed_state` 字段没有枚举约束。

最小修改：改成“写 `last_observed_state='unknown'`，`state` 保持原推进值”，或把 `unknown` 加进 state 并定义转移；同时给 `last_observed_state` 列出允许值。

### S6 中低｜round_transitions 没有合法转换集

位置：`docs/api-database/b11.md:196-197`、`docs/api-database/b10.md:228`。

`from_state`、`to_state` 只写类型，没有限制取值，也没有说明哪些转换合法，`closed` 之后能否回到 `active` 无法判定。

最小修改：列出合法转换表，约束两个状态值属于 `rounds.state` 的枚举，并在写入事务里校验当前状态与转换前状态一致。

### S7 低｜issue_room_links 的分支检查只有单向

位置：`docs/api-database/b07.md:195-196`、`:207`。

正文说 main 分支 `conversation_id` 非空、leader 分支 `repository_id` 非空，但没有要求 main 的 `repository_id` 为空、leader 的 `conversation_id` 为空。缺少反向检查时，主房间可以同时带仓库，Leader 房间可以同时挂会话。

最小修改：加两条检查约束，`(kind='main') = (conversation_id IS NOT NULL)` 与 `(kind='leader') = (repository_id IS NOT NULL)`（按空值语义写成等价形式），并保留现有的同项目、同 Issue 复合外键。

### S8 中低｜plan_revisions 说“绑定仓库事项”，但计划阶段还没有该行

位置：`docs/api-database/b10.md:247`、`:253`、`:264`，`docs/api-database/b11.md:227`。

`repository_issues` 只在首次真实委派时登记，`plan_revisions` 在计划阶段创建。`docs/api-database/b11.md:227` 写“一条 plan_revisions 只绑定一个仓库事项”，与登记时点不符；此时只有 Issue 固定范围内的仓库，没有事项身份。

最小修改：改成“每条修订绑定本 Issue 固定范围内的一个仓库；首次真实派工时 Attempt 才绑定该仓库的 `repository_issue`”。按第二节的链把 `plan_revisions` 的 Issue 与仓库作用域做成复合外键后，这处只是措辞修正。

### S9 低｜dispatch_operations 的 cancel 与 accept_result 没有写入者

位置：`docs/api-database/b10.md:93`、`:165`、`:351`、`:357`、`:451`，`docs/api-database/b11.md:179`。

表把写入者只写成 `Delegate`，但 `kind` 包含 `cancel` 与 `accept_result`；`RequestStop` 与上游结果接收路径都没有说明写哪一行。

最小修改：在表卡片逐 kind 列写入者（delegate 由 Delegate，cancel 由 RequestStop，accept_result 由结果接收用例），或删除未使用的 kind。

### S10 中｜B10 接收 RoomLinkID 但没有持久列

位置：`docs/api-database/b10.md:137`、`:145`、`:215`，`docs/api-database/b07.md:207`。

`DelegateCommand.RoomLinkID` 被接收并声称“只用于派工或 Leader 房间归属”，但 B10 的 `execution_tasks`、`attempts`、`repository_issues` 都没有 `room_link_id`，也没有使用 B07 新增的 `UNIQUE(project_id, issue_id, room_link_id)`。这个输入无法审计，也无法建立跨批复合外键。

最小修改：在执行侧承载该归属的行（大概率是 `attempts` 或 `repository_issues`）加可空 `(project_id, issue_id, room_link_id)`，用复合外键引用 B07 唯一键，并配检查约束；或者删除该命令字段并明确上游派工不带本地房间归属。

### 工作树状态｜index.html 已陈旧（低）

`python3 docs/api-database/render.py --check` 退出 1，首条错误是 `index.html 已陈旧：与当前 Markdown 或模板不一致`。`docs/api-database/index.html` 的 mtime 为 01:03，晚于它的 b00 至 b11 Markdown 在 01:14 至 01:33 被更新。主要作者完成修改后需要重新运行 `python3 docs/api-database/render.py` 并再跑一次 `--check`。生成检查不证明业务正确性。

## 五、本轮已核验通过

RepositoryIssue 物理提案与已采用逻辑记录一致。`docs/current/backend-first-batch-persistence.md:26` 与 `:37` 要求 `(issueId, repositoryId)` 全历史唯一、建项可零条、真实委派才登记、停用不证明停止；`docs/api-database/b10.md:244-253` 的 `repository_issues` 唯一键、登记时点与停用语义与之一致，`docs/api-database/b07.md:205` 只按 `(issue_id, repository_id)` 读取 id，不加前置外键，归属正确。

房间身份单一权威。`docs/api-database/b07.md:190`、`:207`、`:209` 定义 `room_link_id`、实例加外部房间唯一键及新的项目、Issue、room 三列唯一键；B09 与 B10 只引用 `room_link_id`，没有复制房间可用性、观察版本或运行代次。

B10 与 B11 的前向外键方向正确。`docs/api-database/b10.md:494` 明确 B11 复用 B10 表且不反向成为前置；本次检索 `b10.md` 只在 `docs/api-database/b10.md:344` 与 `:455` 以文字引用 B11 的 `result_admissions`，没有指向 B11 表的模式外键。B11 的 `round_transitions`、`target_applications`、`cross_repo_edges`、`result_admissions`、`stop_decisions` 都引用 B10 的 id。

单仓延迟指针本身可落地，但已被方案 B 取代。`docs/api-database/b10.md:229` 的复合外键有被引用唯一键 `(round_id, id)`（`docs/api-database/b10.md:270`），插入顺序与 `internal/database/migrations/0005_models.sql:97-101` 的实现先例一致，机制没有错误；问题只在多仓语义，方案 B 删除它之后不再需要。

前次复核项已修复：`docs/api-database/b08.md:26` 已写认证七端点并列出 login 与 reconnect；`docs/api-database/b07.md:5`、`docs/api-database/b08.md:5` 已把统一错误外壳与分页指到拥有契约；`docs/api-database/b11.md:381-382` 已把 DurableWork 与原则分开；`docs/api-database/b07.md:223` 不再给 B06 的 `issue_events` 增列。

## 六、未核验与限制

本报告只做文档与源码核对，没有运行产品 Go、前端、构建、发布或数据库测试，也没有运行真实上游。

复核期间 b00、b02、b03、b04、b07 被主要作者继续修改（01:33）。本报告的行号与 md5 对应上表快照；作者修复后需要按新版本重新核对，不能直接把本报告行号套到新文件。

b00 至 b03 的内容不在本任务范围，本轮没有复核。
