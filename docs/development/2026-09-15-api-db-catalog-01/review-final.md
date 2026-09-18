# B09-B11 最终有界复核（逐项 PASS 与 R1/R2 闭合）

复核方式：只读 Markdown、源码与迁移。没有改仓库，没有改 HTML，没有启动服务、数据库、浏览器或容器，没有运行产品测试。
范围：`/tmp/repomesh-api-db-20260915/review/recheck-execution.md` 的 P1 至 P5 与续报条目，重点为最新 `docs/api-database/b09.md`、`b10.md`、`b11.md`，并按需核对 `b06.md`、`b07.md`。

复核快照：

| 文件 | mtime | md5 |
| --- | --- | --- |
| b06.md | 2026-09-15 01:37:36 | 8eb8097a13876b5440be84a241a3c15f |
| b07.md | 2026-09-15 01:43:43 | 8b0f0633a7527b7f3383d03b5650574f |
| b09.md | 2026-09-15 01:42:25 | 249a37358acd45734b65e220305d8c89 |
| b10.md | 2026-09-15 01:48:39 | 2aa0f88cc5b5d90e505ad60f337bdc99 |
| b11.md | 2026-09-15 01:40:06 | ae2c58001f2c741670a81f45fa3d1f5c |

结论：P1 至 P5 全部 PASS；S1 至 S10 的修复点全部 PASS；R1 与 R2 已由父修复，本轮补验通过，没有剩余残留。生成器检查本次未复核，父已说明 HTML 尚未生成且工具在另一任务修正。

| 项目 | 结果 | 依据 |
| --- | --- | --- |
| P1 每仓修订加 round_plan_pointers | PASS | `b10.md:275-288` |
| P2 manager_sessions 可空房间与 Issue 归属 | PASS | `b09.md:381-392`、`b07.md:195-209` |
| P3 同 Issue 同仓复合链 | PASS | `b10.md:253`、`:273`、`:297`、`:308`、`:320`、`:336`、`:342` |
| P4 去掉重复绑定与过宽唯一键 | PASS | `b11.md:223`、`:236` |
| P5 只保存最终裁决 | PASS | `b11.md:163`、`:306`、`:310` |
| S1 当前代次标记 | PASS | `b09.md:387`、`:392` |
| S2 repository_issues 生命周期 | PASS | `b10.md:240`、`:255`、`b11.md:85` |
| S3 Attempt 终态与替补门禁 | PASS | `b10.md:326`、`:336`、`:338`、`:340`、`:551` |
| S4 target_applications 状态转移 | PASS | `b11.md:236-240` |
| S5 message_deliveries unknown | PASS | `b09.md:126`、`:433`、`:438` |
| S6 round_transitions 合法转换 | PASS | `b11.md:202-214` |
| S7 房间分支检查 | PASS | `b07.md:207` |
| S8 plan_revisions 仓库措辞 | PASS | `b10.md:267`、`b11.md:103` |
| S9 dispatch_operations 写入者 | PASS | `b10.md:378`、`:384` |
| S10 B10 房间列与复合外键 | PASS | `b10.md:321-322`、`:342`、`b07.md:209` |
| index.html 生成检查 | 本次未复核 | 父已说明 HTML 尚未生成、工具在另一任务修正；01:43 的旧检查曾通过 |

## 一、外键可构造性逐条核对

本轮没有运行数据库，下面按组合键的列与顺序核对，全部有对应的唯一或主键目标。

| 外键 | 被引用键 | 结果 |
| --- | --- | --- |
| plan_revisions `(round_id, issue_id)` 引用 rounds | rounds `UNIQUE(id, issue_id)`（`b10.md:234`） | 顺序一致 |
| round_plan_pointers `(round_id, issue_id)` 引用 rounds | 同上 | 顺序一致 |
| plan_revisions `(issue_id, repository_id)` 引用 issue_repository_scope | B06 `PK(issue_id, repository_id)`（`b06.md:311`） | 顺序一致 |
| round_plan_pointers `(issue_id, repository_id)` 引用 issue_repository_scope | 同上 | 顺序一致 |
| round_plan_pointers `(round_id, repository_id, current_plan_revision_id)` 引用 plan_revisions | plan_revisions `UNIQUE(round_id, repository_id, id)`（`b10.md:273`） | 顺序一致 |
| execution_tasks `(plan_revision_id, issue_id, repository_id)` 引用 plan_revisions | plan_revisions `UNIQUE(id, issue_id, repository_id)`（`b10.md:273`） | 顺序一致 |
| attempts `(task_id, issue_id, repository_id)` 引用 execution_tasks | execution_tasks `UNIQUE(id, issue_id, repository_id)`（`b10.md:308`） | 顺序一致 |
| attempts `(repository_issue_id, issue_id, repository_id)` 引用 repository_issues | repository_issues `UNIQUE(id, issue_id, repository_id)`（`b10.md:253`） | 顺序一致 |
| repository_issues `(issue_id, repository_id)` 引用 issue_repository_scope | B06 `PK(issue_id, repository_id)` | 顺序一致 |
| attempts `(project_id, issue_id)` 引用 issues | B06 `UNIQUE(project_id, id)`（`b06.md:275`） | 顺序一致，`room_link_id` 为空时仍执行 |
| issue_room_links `(project_id, issue_id)` 引用 issues | B06 `UNIQUE(project_id, id)`（`b06.md:275`） | 顺序一致 |
| issue_room_links `(project_id, issue_id, conversation_id)` 引用 issues | B06 追加 `UNIQUE(project_id, id, main_conversation_id)`（`b06.md:277`） | 顺序一致 |
| manager_sessions `(project_id, issue_id, conversation_id, room_link_id)` 引用 issue_room_links | `b07.md:209` 第一条唯一键 | 顺序一致 |
| attempts `(project_id, issue_id, repository_id, room_link_id)` 引用 issue_room_links | `b07.md:209` 第二条唯一键 | 顺序一致 |

## 二、P1 指针集合 CAS

`round_plan_pointers` 主键 `(round_id, repository_id)`（`b10.md:288`）保证一轮一仓一行；指针外键含 `round_id` 与 `repository_id`（`b10.md:284`），指向另一轮或另一仓的修订会被拒绝；`issue_id` 通过 `rounds(id, issue_id)` 与 `issue_repository_scope(issue_id, repository_id)` 固定，修订本身也受同一轮次与范围约束（`b10.md:264-267`），因此指针与修订不会落在不同 Issue。插入顺序 `rounds → plan_revisions → round_plan_pointers`，全部普通立即外键（`b10.md:236`），无环。CAS 写法为锁 `rounds` 行后按完整指针集合与各行 `pointer_revision` 比较（`b10.md:288`、`:489`），B11 的 Evaluate 与 Close 明确读取完整集合（`b11.md:66`、`:85`）。移出某仓时先确认无在途工作属于事务检查，不是可声明的单行约束，作为实现条件保留即可。

## 三、scope 空值绕过核对

B09 主房间引用：`room_link_id` 可空，`CHECK (room_link_id IS NULL OR issue_id IS NOT NULL)`（`b09.md:392`）阻止只填房间不填 Issue；`conversation_id` 与 `project_id` 非空，房间非空时四列全非空，复合外键必然执行。B07 的 main 行 `conversation_id` 非空、leader 行该列为 NULL（`b07.md:207`），所以 B09 的引用不可能匹配 Leader 行。

B10 Leader 房间引用：`room_link_id` 可空；非空时 `project_id`、`issue_id`、`repository_id` 均非空，复合外键必然执行。B07 的 main 行 `repository_id` 为 NULL，因此 B10 的引用只能匹配 Leader 行。

R1 已修：`attempts` 增加独立的 `(project_id, issue_id)` 复合外键引用 B06 `issues(project_id, id)`（`b10.md:321`、`:342`）。两列都非空，`room_link_id` 为空时该外键仍执行；房间外键只负责 Leader 房间归属，项目归属不再依赖它。

## 四、状态与替补门禁

`attempts` 用部分唯一索引 `UNIQUE(task_id) WHERE state IN ('prepared','starting','running','submitted','stopping','unknown')` 阻止同一任务并行存在两个未收敛 Attempt，`stopped`、`reclaimed`、`failed` 明确定义为终态（`b10.md:336`）。R2 已修：`replacement_allowed` 有完整 CHECK，终态回非终态由限定旧状态与 `claim_generation` 的条件 UPDATE 拒绝，`stopped`、`failed` 只允许在回收后进入 `reclaimed`（`b10.md:338`）。Delegate 先锁 `rounds`，再对该任务行 `SELECT ... FOR UPDATE`，持锁重读全部历史 Attempt 并检查许可后才插入新行；许可更新也先锁任务行，没有反向锁序，任务行始终存在，不靠“尚无 Attempt 行”做并发保护（`b10.md:340`）。G4-14 覆盖并发与拒绝用例（`b10.md:551`）。

## 五、R1 与 R2 修复确认（最终 PASS）

### R1 已修｜attempts.project_id 的独立项目外键

位置：`docs/api-database/b10.md:321`、`:342`。

`project_id` 现在与 `issue_id` 组成独立复合外键，引用 B06 的 `issues(project_id, id)`。两列都非空，所以 `room_link_id` 为空时外键仍然执行，项目归属不再依赖可空房间引用；B06 的 `UNIQUE(project_id, id)` 已存在（`b06.md:275`）。G4-12 增加“room_link_id 为空时把 project_id 改为另一个项目仍被独立外键拒绝”的用例（`b10.md:549`）。

### R2 已修｜完整 CHECK、条件状态 UPDATE 与持锁替补检查

位置：`docs/api-database/b10.md:338`、`:340`、`:551`。

1. `replacement_allowed` 的 CHECK 同时覆盖终态与证据：不允许为 true，除非状态属于 `stopped`、`reclaimed`、`failed`，且从未启动，或停止证据与写能力撤销证据都非空（`b10.md:338`）。
2. 状态更新使用限定旧状态与 `claim_generation` 的条件 UPDATE；终态不能回到 `prepared`、`starting`、`running`、`submitted`、`stopping` 或 `unknown`，`stopped`、`failed` 只允许在回收完成后进入 `reclaimed`，`reclaimed` 不再转移（`b10.md:338`）。
3. Delegate 在同一短事务中先锁 `rounds` 行核对完整计划指针，再以 `SELECT ... FOR UPDATE` 锁定 `execution_tasks` 任务行；持锁读取该任务全部历史 Attempt，只有全部 `replacement_allowed = true` 才能插入新行并占用资源。许可更新也先锁任务行，禁止反向锁序；任务行始终存在，首次执行也有可锁对象（`b10.md:340`）。
4. G4-14 覆盖“无证据置 true 被 CHECK 拒绝、终态回 running 被条件更新拒绝、两个并发 Delegate 只有一个登记”的用例（`b10.md:551`）。

两处残留均已闭合，没有新的替代残留。

## 六、其余条目逐项说明

P4：`target_applications` 已删除 `upstream_binding`，唯一键为 `(plan_revision_id)`，目标与仓库由修订决定（`b11.md:223`、`:236`）。

P5：`Decision` 返回枚举保留 `needs_recheck`，它只作为返回与等待状态，不写 `result_admissions`；持久表只允许最终 `admit_for_current_round` 与 `reject_stale`（`b11.md:163`、`:306`、`:310`）。

S1：`manager_sessions` 增加 `superseded_at`，部分唯一索引 `(project_id, conversation_id) WHERE superseded_at IS NULL`，并规定换上下文新建代次（`b09.md:387`、`:392`、`:499`）。

S2：`repository_issues` 的写入者改为首次登记与 Issue 终结或明确停用，普通轮次收尾不改 state；B11 的 CloseRound 也明确不改该表（`b10.md:240`、`:255`，`b11.md:85`）。

S4：`target_applications` 的 `state` 与 `recovery_state` 各有合法转换表，恢复读回一致时由 `RecoverTarget` 同时写 `recovered` 与 `readback_ok`（`b11.md:236-240`）。

S5：`state` 保持已证实的推进值或进入 `reconcile_required`，探测失败只写 `last_observed_state = unknown`，该列已列出允许值（`b09.md:126`、`:433`、`:438`）。

S6：`round_transitions` 增加合法转换表，并要求两个状态值属于 `rounds.state`、写入事务核对当前状态（`b11.md:202-214`）。

S7：`issue_room_links` 的分支检查是完整互斥 CHECK（`b07.md:207`）。

S8：计划修订改述为绑定本 Issue 固定范围内的一个仓库，首次真实派工时 Attempt 才绑定 `repository_issues`（`b10.md:267`、`b11.md:103`）。

S9：`dispatch_operations` 按 kind 列出写入者：`Delegate`、`RequestStop`、结果接收用例（`b10.md:378`、`:384`）。

S10：`attempts` 增加可空 `room_link_id` 与四列复合外键，指向 B07 Leader 唯一键（`b10.md:321-322`、`:342`）。

index.html：本轮按父要求未运行生成器。01:43 的旧 `render.py --check` 曾通过，但 01:48 的 b10 修改后 HTML 尚未生成，工具由另一任务修正。

## 七、未核验与限制

本报告只做文档与源码核对，没有运行产品 Go、前端、构建、发布、数据库或真实上游测试。生成器检查本轮未运行，按父要求留给另一任务；HTML 状态以后续生成为准。

b00 至 b05、b08 与 foundations 没有逐条重读；本报告只按 P1 至 P5 与续报条目所需的引用核对。

报告中的行号对应开头快照表的文件 md5。作者若继续改动这些文件，需要按新版本重新定位。
