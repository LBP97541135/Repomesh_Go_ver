# B04-B11 综合复核（独立只读）

复核对象：工作树中的 `docs/api-database/{foundations,b04,b05,b06,b07,b08,b09,b10,b11}.md`。
复核方式：只读源码、迁移、现行契约与开发证据。没有启动服务、数据库、浏览器或容器，没有运行产品测试，没有重跑历史实验。
基线：git HEAD `43d8c2a`，工作树带未提交的文档修改，`docs/api-database/` 为新增目录。
范围外：b00-b03（另一作者处理）。本报告不是产品验收结论。
复核期间 `foundations.md` 与 `README.md` 被 B00-B03 作者更新（01:06）；本报告的 foundations 行号按 01:06 版本核对，b04 至 b11 文件在本轮未被改动。

## 最高风险

### R1 B08 把 B02 认证端点写成 6 个，源码注册 7 个（高）

位置：`docs/api-database/b08.md:26`。

原文写“认证六端点”，并把登录与重连合并成一条 `POST /api/auth/github/{purpose}`，漏掉 `POST /api/auth/github/reconnect`。

源依据：

- `internal/web/auth.go:47` 注册 `GET /api/session`。
- `internal/web/auth.go:54-55` 对 `login` 与 `reconnect` 各注册一条 `POST /api/auth/github/...`，共两条。
- `internal/web/auth.go:84` callback，`:102` attempts，`:109` logout，`:120` repositories。

合计 7 条已实现路由。`docs/current/b02-authentication-adoption.md` 采用的认证稿在接口目录 `docs/current/authentication-browser-api-draft.md:25-31` 也逐条列出 7 条，其中 `:26` 是 reconnect。

影响：B08 跨批清单与 B08-A 系列会少覆盖 reconnect，矩阵的覆盖结论不成立。

最小修改：改“认证七端点”，把 login 与 reconnect 分列，并在 B08-A 系列补一条 reconnect 用例。

补充：b07 正文没有重复这个计数错误。`docs/api-database/b07.md:24` 只引用 session 与 repositories 两条注册行。

## 中风险

### R2 b10 的 rounds 与 plan_revisions 互相 NOT NULL 引用，未声明延迟约束（中）

位置：`docs/api-database/b10.md:227`、`:233`、`:242`。

`rounds.current_plan_revision_id` 必填并指向 `plan_revisions`；`plan_revisions.round_id` 必填并引用 `rounds`。两行都要在同一次插入中先行存在，普通立即外键无法落地。b10、b11 两章都没有 DEFERRABLE 声明。

已实现的同类写法见 `internal/database/migrations/0005_models.sql:97-101`：`providers.head_revision` 使用 `DEFERRABLE INITIALLY DEFERRED`，正是为解决同事务先插修订再更新 head。

最小修改：在 b10 写明该外键 `DEFERRABLE INITIALLY DEFERRED` 并给出插入顺序，或把指针改为可空、在事务末尾延迟检查。

### R3 B11 的多仓轮次落不进 B10 的单仓 rounds（中）

位置：`docs/api-database/b11.md:96`（`ScopeRepositories []string`）、`:103`（在 B10 rounds 插入新一轮）、`docs/api-database/b10.md:228`（`repository_id` 必填，“本批恰好一个仓库”）。

B11 还有按仓的 `round_combinations`（`docs/api-database/b11.md:272`）和两仓部分应用场景（`:394`），说明一轮可覆盖多仓。B10 的 rounds 只能保存一个 `repository_id`，repair 轮次的多仓范围没有落点。

最小修改：由 B10 持有轮次仓库范围（例如 `round_repository_scope`，或把 `repository_id` 改为可空并由范围表约束），B11 引用同一事实。若坚持 rounds 只服务单仓，就在 B10 明确写出 B11 另建范围表的归属。

### R4 foundations 被引用为不存在的共享权威（中）

位置：`docs/api-database/b07.md:5`、`docs/api-database/b08.md:5`、`docs/api-database/b11.md:380`。

三处把“统一错误外壳、分页语义或参数、内容范围核权、共享持久待办、事件、操作框架”归给 `foundations.md`。该文件实际只有状态词表、身份归属、操作身份原则、授权与恢复边界、数据生命周期和执行边界。它没有错误码注册表，没有分页语义，也没有通用 operation 或 event 框架。`:64` 反而明确各业务域保留自己的回执，`:74` 只给出“写入与回执同事务、需要后续工作时保存待办”的原则。

实际权威位置：统一错误外壳在 `docs/current/issue-page-create-api-contract.md:205-236`；分页语义在 `docs/current/first-batch-browser-api-contract.md:278-286`；DurableWork 在 `docs/current/backend-first-batch-persistence.md:127`。

最小修改：在 `foundations.md` 增加一节“跨批错误外壳与分页”，或把 b07、b08 的引用改到上述拥有章节；`docs/api-database/b11.md:380` 改成引用 `backend-first-batch-persistence.md` 第 3 节与 foundations 的原则，不写“操作框架”。

顺带核对：`docs/api-database/b07.md:62`、`:68`、`:322` 已把新码 `410 ISSUE_CONTENT_REMOVED` 标为本次提案，没有说成已采用。现行契约里的同类码都是 `*_RESULT_REMOVED`（`docs/current/issue-page-create-api-contract.md:229`、`:233`，`docs/current/first-batch-browser-api-contract.md:303`，`docs/current/conversation-message-clarification-api-contract.md:143`）。命名是否统一随 R4 的登记一起定。

### R5 B06 的已采用端点段落无条件陈述 P9 候选行为（中）

位置：`docs/api-database/b06.md:193`。

该段写“创建先锁项目，读取当前固定配置修订 C1 并提交，则 Issue 固定 C1；应用先提交 C2，则旧创建令牌返回 409”。锁序基线已经收口，但“Issue 固定 C1”依赖 P9。P9 在 `docs/api-database/b06.md:21`、`:496` 与 `docs/current/issue-configuration-binding-design.md:2` 都是未采用候选，`docs/current/backend-first-batch-persistence.md:231` 的 §7 也标为 `PROPOSED_NOT_ADOPTED`。

最小修改：在该句前加“P9 采用后”，或把固定引用语义移到本章 P9 候选小节。

### R6 同一张 issue_events 在 B06 与 B07 有两种定义（中）

位置：`docs/api-database/b06.md:361` 与 `docs/api-database/b07.md:219`。

B06 把事件内容定义为 `snapshot_invalidated` 与 `created_at`；B07 在同一张表上增加 `changed text[] NOT NULL`。B07 自己声明不重定义 B06 的表（`docs/api-database/b07.md:148`、`:152`），但增量没有回到 B06 章。同一个对象出现两个权威。

最小修改：把 `changed` 列写进 B06 的 `issue_events` 定义，或在 B06 登记“B07 扩展该列”。

### R7 leader 房间的 repository_issue_id 没有权威表（中低）

位置：`docs/api-database/b07.md:196`。

该列在 leader 分支必填，含义是“真实的仓内事项”，但 b06、b10、b11 与 `docs/current` 都没有定义可被引用的 repository issue 身份。本次对仓库内 Markdown 文档的检索只在 `docs/api-database/b07.md:196` 出现该名称（index.html 是同一内容的生成副本）。

最小修改：把该列指向已有身份（例如 B10 的 `execution_tasks` 或上游仓内事项映射），或明确写成不透明引用，等该身份所属批次采用后再加外键。

## 低风险与编辑性

### R8 B05 候选数值在正文里读起来像已定规则（低）

位置：`docs/api-database/b05.md:83`（`min(16, ...)`）、`:328`（60 秒租约、20 秒续期）。状态表 `:25` 与未决表 `:607` 已把这些值标成未采用，所以不是状态偷换，但正文单行看不出候选身份。最小修改：在 `:83` 与 `:328` 各加“候选值”。

### R9 B05 的物理 schema 名与产品命名不一致（低，编辑性）

位置：`docs/api-database/b05.md:355`、`:373`、`:388` 使用 `modelbudget.*` 与 `models.*`。B04 已实现 `repomesh_models`（`internal/database/migrations/0005_models.sql:2`、`:69`），后续章用 `repomesh_messaging`（`docs/api-database/b09.md:205`）、`repomesh_execution`（`docs/api-database/b10.md:213`）、`repomesh_delivery`（`docs/api-database/b11.md:185`）。

这些名字来自设计附件 `docs/development/2026-09-13-b04-b06-design-01/migration-design.md:34-43`，是继承的漂移，不是新候选与旧名冲突。最小修改：在 b05 数据表一节说明物理 schema 名是占位，实施时统一。

### R10 B07 的引用措辞（低）

`docs/api-database/b07.md:207` 写 B09、B10 “按该唯一键引用本表”，实际用主键 `room_link_id` 引用（`docs/api-database/b09.md:379`、`docs/api-database/b10.md:143`）。最小修改：改成“通过 room_link_id 引用”。

### R11 B04 两处行号漂移（低）

`docs/api-database/b04.md:36` 引 models.go 第 99 行的 `registerModelRoute`，函数声明在 `internal/web/models.go:98`；`docs/api-database/b04.md:336` 引 parse.go 第 22 行的 `ParseImport`，函数声明在 `internal/sources/parse.go:18`。两处指向的函数都对，只是行号差几行。

## 已核验且未发现问题

B04 的接口与落库事实。六个模型端点与注册顺序对照 `internal/web/models.go:21-96`；未配置认证时模型服务为零值、`/api/*` 404 对照 `internal/web/server.go:96-98`、`:112-114` 与 `cmd/repomesh-web/main.go:59-82`。列表形状、排序、游标绑定与 400、409 语义对照 `internal/models/service.go:16-80`。详情字段与 availability 四值对照 `internal/models/types.go:34-72` 与 `internal/models/storage.go:197-245`。保存输入、close 与幂等对照 `internal/models/input.go:40-105` 与 `internal/models/service.go:187-240`、`:424`、`:463`、`:496`。表、列、复合外键、延迟 head 外键与 replace、keep vault 约束对照 `internal/database/migrations/0005_models.sql:69-160`、`:185-213` 与 `internal/database/migrations/0006_complete_save_vault.sql:1-30`。导入的部署角色、单例锁、owner 排序与退出码对照 `internal/sources/import.go:17`、`:63`、`:183`、`:191` 与 `cmd/repomesh-web/sources.go:100-130`。B04 的状态词与 `docs/current/b04-model-sources-adoption.md` 一致，没有把 `INTEGRATED_LOCAL_VERIFIED` 写成业务 `VERIFIED`。

B05 的 unknown 关闭与参数身份。关闭绑定对照 `docs/development/2026-09-14-b05-b06-design-closeout-01/README.md:10` 与 `docs/api-database/b05.md:336-349`：operator 取自实际 `session_user`，证据逐项绑定 actor、test、external operation、permit、sender instance、credential capability id 与 version，两类材料带 SHA-256，缺一保持 open，关闭后 outstanding 清除并写审计。候选数值与 maxUnresolved 未采用（`docs/api-database/b05.md:25`、`:464`、`:607`）。锁序与已实现 access 前缀一致：`internal/access/project_access.go:67-97` 先锁 binding、session、account，所以 `docs/api-database/b05.md:137`、`:247` 把 account 放在 operation 与 project 之前不矛盾。B05 单次测试与 B09 的 Manager 调用分开：`docs/api-database/b09.md:466` 明确不复用 `actor_model_test` 额度，`:537` 与 `docs/api-database/b10.md:467` 也把 B05 账本只当只读输入。

B06 的采用状态与原操作重放。P1 原子范围在 `docs/current/issue-page-create-api-contract.md:315` 采用；四条路由与字段对照该契约 `:24-27`、`:34-77`、`:109-162`；幂等、404、410 与重放优先对照 `:173-191`、`:220-236`。P9 仍是候选（`docs/api-database/b06.md:21`、`:496`，`docs/current/issue-configuration-binding-design.md:2`），物理表与触发器在 `docs/api-database/b06.md:24`、`:239`、`:384` 标为未采用。

B07 的错误码、游标与房间身份。`410 ISSUE_CONTENT_REMOVED` 不在现行契约错误表里，`docs/api-database/b07.md:62`、`:68`、`:322` 已标为本次提案。游标扩列对照现有表与读写实现：`internal/database/migrations/0004_projects.sql:149-159`、`internal/database/migrations/0005_models.sql:12-16`、`internal/projects/storage.go:452-479`；新增列带默认值，不改变现有九列插入，结论成立。房间身份用 `(issue_id, kind, instance_id, upstream_room_ref)`（`docs/api-database/b07.md:205`），B09 与 B10 通过 `room_link_id` 引用，单一权威成立。

B09 至 B11 的表归属与前向外键。B10 定义 `rounds` 与不可变 `plan_revisions`（`docs/api-database/b10.md:215-250`、`:422-423`），B11 明确删除自己的副本并引用 B10（`docs/api-database/b11.md:14`、`:185`、`:321-326`）。b10 章内没有指向 B11 表的外键，只有“业务采纳由 B11 result_admissions 决定”的文字（`docs/api-database/b10.md:344`、`:455`）。B11 的 `round_transitions`、`target_applications`、`cross_repo_edges`、`result_admissions` 都引用 B10 的 id，方向正确。

冻结的上游事实抽查。replan 前置条件对照 `third_party/AgentTeams/agentteams-controller/internal/server/project_handler.go:2418-2450`；REST 路由对照 `third_party/AgentTeams/agentteams-controller/internal/server/http.go:137-141`；taskflow 与 projectflow 动作枚举对照 `third_party/AgentTeams/plugins/teamharness/mcp/server.py:478`、`:409-414`；shared 任务目录对照 `third_party/AgentTeams/plugins/teamharness/mcp/server.py:1637`；ManagerSpec 没有 Issue、配置修订或秘密版本字段，对照 `third_party/AgentTeams/agentteams-controller/api/v1beta1/types.go:636-669`。这些是源码阅读，不是运行验证。

## 尚未核验或无法核验

b00 至 b03 不在本任务范围，本报告没有复核其 API 数量或状态。

`python3 docs/api-database/render.py --check` 当前退出 1，原因是缺少 b00.md 至 b03.md。这是另一作者的切片，不计入上述发现。index.html 的布局、链接锚点与生成一致性没有重新打开浏览器核对。

B08 验收矩阵中的任何 PASS 都没有执行；本报告只核对文档与源码事实。

B09 至 B11 的上游结论来自锁定源码阅读与既有证据文档，没有重跑 Controller、TeamHarness、容器或真实模型请求。

产品 Go 测试、前端测试、构建与发布检查均未运行。本次只运行了文档生成检查这一条只读命令，失败原因见上。
