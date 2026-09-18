# B05–B08 独立语义复核

复核者 Astra。2026-09-15。本人不是 B05/B06/B07 的主要设计者。本轮只读实际文档、对应 Astra 方案与必要现行专题/迁移，没有编辑仓库，没有启动数据库、服务、容器或外部请求，没有派生 agent。

结论：9+6+1 表的合并方向可保留，不需要拆回原表。实际文本仍有下面列出的约束矛盾与遗漏，不能仅凭表数正确判为语义闭合。B05 修复项已直接发送 `/root/deepseek_write_b05` 并通知 root；B06/B07 项已发送 root，由其安排作者修复。以下行号按复核时实际文件，作者修改后需重新定位。

## 复核范围与基线

- `docs/api-database/b05.md`：九表、六个候选 HTTP API、C05、一次发送与运维关闭、逐路径锁序、schema2 导入隔离与双表恢复。
- `docs/api-database/b06.md`：六表、创建/恢复 API、独立会话字段、真实 FK、工作范围生成列、清理、事件边界。
- `docs/api-database/b07.md`：一表、八项已采用读取及页头候选、签名游标、房间代次、SSE。
- `docs/api-database/b08.md`：本轮 diff 全部以及矩阵、证据与发布/迁移检查；没有新增表。
- `/tmp/repomesh-consolidation-20260915/astra-b05.md`、`astra-b06-b07.md`；必要现行 `backend-first-batch-persistence.md`、`model-settings-browser-api-draft.md`、`first-batch-browser-api-contract.md`，B09 §2.7 的实际独立创建字段。
- 真实 `0002_auth_secrets.sql`、`0004_projects.sql`、`0005_models.sql` 的 PK/UNIQUE/FK 与字段；确认 0001–0007 文件清单及本次迁移目录无 diff。B01–B04 文档无本次 diff。

基线 SHA-256：

| 文件 | 摘要 |
| --- | --- |
| b05.md | 10dffb94d47642df25bf47d3e4374c941d9801ee5dd203986c0383f0ad2b2308 |
| b06.md | 4c3d2c5f59c25b469de75efe30935281594a627cad7f38583332d969be3824fb |
| b07.md | d525bd51aaa99880dc14d7c6eae9f8a832c551c15202a12152cd84df1a845fe1 |
| b08.md | 838e6cb9f7c1949870f3f6845ff435ab0f1636e2ca2449253447defa0c3a04b3 |

## R1：首消息 FK 使用不存在的源列

严重性：P1，按文本生成 DDL 会失败。

实际位置：`b06.md:317`；相同表述还有 `b09.md:173`。B06 `conversations` 主键实际是 `id`，见 `b06.md:284`，没有 `conversation_id` 源列。当前却写 `(project_id, conversation_id, standalone_first_message_id)` 引用 messages。

来源：B06 自己的真实字段表与 B09 `conversation_messages(project_id,conversation_id,id)` 引用目标，目标本身存在，错误在源端。

最小修复：明确写 `conversations(project_id,id,standalone_first_message_id) → conversation_messages(project_id,conversation_id,id)`，使用延迟复合 FK，并同步 B09。无需新增同义 conversation_id 列。此项由 root 先提示，本次独立检查确认。

## R2：会话标题约束同时拒绝合法无标题创建与派生清理

严重性：P1，合法创建或清理无法通过约束。

实际位置：`b06.md:287,297–299,508,563`。一处要求 title 与 title_redacted_at 二选一；另一处规定 title_origin 非空时 title 必须存在；清理只清 title 并写 redacted，未解除 title_origin 的非空前提。

可构造两条合法业务路径：

1. B09 `title` 可省略，且 `b09.md:165,175` 明确标题只来自显式输入，不从首消息派生。新无标题会话应是 NULL/NULL，当前二选一拒绝它，不能伪造已清理时间。
2. 页面派生标题有 title_origin。按 `b06.md:563` 清理后是 title=NULL、title_redacted_at 非空、origin仍非空，立即违反 `b06.md:298/508`。

来源：已采用持久化 §7 页面派生清理规则、Astra B06 方案的保留语义、B09 当前独立创建候选。Astra 原方案也遗漏了这两个状态的相容性，不能因作者忠实转写就判正确。

最小修复：统一允许未命名 NULL/NULL、有标题非空/NULL、已清理 NULL/非空，拒绝同时非空。title_origin 非空时允许仍有标题或已清理墓碑，且始终检查来源 Issue 的主会话归属；清理保留来源关系，只有明确独立改名才清标题派生标记。同步触发器清单与清理步骤。

## R3：B05 非空生成判别列与子型全空 CHECK 冲突

严重性：P1，按字面定义 apply/execution 分支不可插入。

实际位置：`b05.md:437,463` 与 `b05.md:687,696`。

previews 将 budget_kind/time_kind/egress_kind/budget_scope 声明为所有行非空生成常量，而 apply 分支要求 test 专有列全空。policy_bindings 将 egress_kind 声明为非空固定 egress，但 execution 分支要求出站各列全空。两处都不能同时成立。

来源：Astra B05 方案要求完整互斥 CHECK，避免 NULL 绕过，并保留 test/apply、test/execution 两种可实现分支。

最小修复：精确枚举空值 CHECK 的列。可保留恒定种类列，只让对应政策 id/version 在不适用分支为 NULL；或者按分支条件生成种类列。必须让使用中的 id/version/必要scope全非空，不能以部分 NULL 通过 MATCH SIMPLE；不要只说“专有列全空”掩盖生成列冲突。已发 B05 作者。

## R4：B05 清理要求与非空/不可变定义不相容

严重性：P1，清理会失败或留下正文。

实际位置：`b05.md:456,465` 的预览 before；`b05.md:621,639` 的应用 canonical_input。

before_fixed_canonical 被标为 apply 必填，且 before 证据不可更新、触发器只允许消费与 removed_at；同一段又要求过期清比较正文。application canonical_input 标必填，清理却明确清空 canonical_input。两个定义都没有完整 live/removed CHECK 与正文清理的白名单。应用还宣称清理后保留输入摘要，但字段表没有 input_digest。

来源：Astra B05 的预览墓碑、应用原操作墓碑与有限清理规则；实际 HTTP 原操作清理后 410 与旧键不可复用。

最小修复：把可清理正文物理列明确设为可空，live 分支必填、removed 分支为空；不可变触发器只允许一次对应清理转换并保留固定身份/政策/消费绑定。明确实际保留摘要的列与格式；若不需要摘要则删掉虚构承诺，removed优先于输入比较。不能为了清理放开修改固定执行输入。已发 B05 作者。

## R5：B05 模型归属与测试预览互指缺少具体数据库约束

严重性：P2，文档未能证明它声称保留的跨行一致性。

实际位置：`b05.md:429–433,479,486–490,463`。actor 只 FK account、snapshot 只 FK model_snapshots，未落实 Astra 要求的 actor=Provider.owner；previews 的秘密四元组只锁同 Provider，没有明确检查它等于该 provider_revision 的 secret。tests 在 :487 有这一秘密检查，但不能替代预览检查。消费最终检查在 :463 只明确 apply，未明确 test 的双向互指及固定值相等。

可构造当前声明的 FK 都合法但业务错误的组合：actor A 的测试引用 actor B 的 Provider；预览用同一 Provider 另一修订的 secret；preview P1 指向 T2，而 T2.preview_id=P2，P2 指向 T1、T1.preview_id=P1。各自存在且唯一，并不足以证明匹配。专用 HTTP 核权可能阻止正常路径，但不能当作已声明的数据库归属约束。

来源：Astra B05 共同 DDL 与 previews/tests 一致性要求。真实 `0005_models.sql:75` 已有 providers UNIQUE(id,owner)，:90–94 已有 provider revision 的固定 secret 引用。

最小修复：在两张新表加 `(provider_id,actor)→providers(id,owner)` FK，或明确等价新表触发器；新表触发器核预览 secret 等于固定 revision secret。把 test 反向检查写明：preview.consumed_test_id 指向的 test 必须反指本 preview，tests 的固定模型/secret/policies 等于消费时预览。触发器查询最终行并覆盖两端写入，旧表无需增列或约束。已发 B05 作者。

## R6：B05 状态矩阵仍放行不可能的预算/责任组合

严重性：P2，单行合并后的 CHECK 尚未闭合。

实际位置：`b05.md:504,551–561`。

当前 CHECK 允许 `(queued,recovery=open,budget_status=released,permit全NULL)`：queued 只限制 recovery，reserved 才要求 permit 空，rejected 才限制 released，因而这条未核任务没有有效预留仍可保存。当前文本还允许 rejected+完整permit+consumed，但 `b05.md:92,561` 说该分支尚未采用。`work_state=done` 也没有明确禁止与 open 未核责任同时存在，唯一扫描根可能停止核查。

来源：Astra B05 以 tests 为唯一可扫描责任根、许可前预留、生成许可即消费且消费不可释放的规定。

最小修复：枚举完整合法组合：queued无许可→reserved；queued有许可→consumed；running/passed/failed/unknown必须许可完整且consumed；当前rejected只能无许可且released。额外把关闭/未发拒绝与done、open未核与非done的关系写明。将来若采用许可后确定未发拒绝，再显式扩矩阵。已发 B05 作者。

## R7：Conversation 丢了业务 revision 与 created_at 的物理承载

严重性：P2，已采用读取字段没有共同权威来源。

实际位置：`b06.md:282–289` 的基础字段表，以及全部 conversations 字段组；消费者 `b07.md:122,130` 要求 createdAt/revision，`b09.md:166,173` 也要求创建时固定两字段。

conversations 只有 content_scope_revision、消息计数和独立创建回执的 committed_at，没有所有会话统一的业务 revision/created_at。页面创建会话与独立会话不能各凭不同操作时间临时猜出共同创建事实，消息序号、内容范围修订更不能冒充业务修订。

来源：`backend-first-batch-persistence.md:25` 的 Conversation 明确包含业务修订；已采用 `first-batch-browser-api-contract.md:274` 返回 createdAt/revision。Astra B06 临时方案也漏了这些列，属于共同设计缺口。

最小修复：在 conversations 增 `revision text NOT NULL` 和不可变 `created_at timestamptz NOT NULL`；明确创建赋值及标题/业务元数据变化时修订推进，与 content_scope_revision/last_message_sequence 分离。创建回执冻结它们当时值，不回写旧回执。无需新增表。

## R8：B07 缺少可信运行代次切换协议

严重性：P2，新 runtime_generation 无可执行的准入规则。

实际位置：`b07.md:235–239,303`。当前只写按 `(revision,generation)` 比较、拒绝旧 generation，并说明 generation 不按字典序比较；没有定义如何确认一个不同 generation 是受信任的新代次而非旧包。

来源：Astra B06/B07 §7 已明确区分普通观察与可信代次切换：普通更新要求 generation 精确相等、revision 严格增加；切换需单独锁 Issue/关联，以旧 generation CAS 登记新 generation 和相应观察。

最小修复：恢复该明确双路径。只靠高 observation_revision 不允许任意 generation 覆盖；切换后旧 generation 全部拒绝。房间观察/失效事件同事务，并沿现行会话→Issue→关联顺序取所需锁。不要新增 generation 历史表。

## R9：UTC 日窗口不应使用依赖会话时区的“1 day”算术

严重性：P2，时区/DST 下窗口可能不是固定 24 小时。

实际位置：`b05.md:407,413`，end_utc 明确写 start_utc+1 day。start/end 均为 timestamptz，而 PostgreSQL calendar day interval 的加法会应用会话 TimeZone 的夏令时规则；UTC日零点并不能使后续calendar-day算术自动变成 UTC。

来源：Astra B05 windows 要求“UTC 日零点、恰 24 小时后”，现行账本窗口口径也为 UTC day。这里是 SQL 语义核查，没有启动数据库实测。

最小修复：约束直接写 `end_utc = start_utc + interval '24 hours'`，或以显式 UTC date 构造下一日端点；不要依赖执行会话的 TimeZone 为 UTC。补非 UTC/DST 时区下的未来数据库验证要求。已发 B05 作者。

## 已核对且无需拆表的部分

- 原操作键未把 schema_version 加入唯一性。测试 `(actor,test_id)`、应用 `(project_id,actor,application_id)`、建项 `(project_id,creation_actor,entry,creation_id)`、独立会话创建原作用域保留，removed仍占键。
- B05 一次许可明确依赖本事务确切 COMMIT 与不可恢复内存许可。外部未知先核查、permit一旦存在即consumed、unknown关闭需发送实例退出及本地能力撤销两类材料。合表没有把租约到期等同撤销，没有退款/重发路径。
- schema2 policy_imports 与旧 schema1 imports 分离；共用已有 import_serialization、两表查原键、同key跨schema冲突、sources result无需猜schema，旧0005 CHECK(schema_version=1)原样保留。共同入口双向串行足以保住操作域，没有要求修改旧表DDL。
- 真实旧键可用：model_snapshots三列PK；configuration_revisions(project_id,revision)；secrets版本四元唯一由0004补充；execution_versions(profile_id,version)；deployment_id唯一。没有发现这几处假想目标键。
- B06 repository_scopes 的 work_issue_id生成列加普通UNIQUE提供真实工作范围目标，B07 leader FK不会误中仅内容行；owner XOR显式排除NULL绕过。Issue/主CS双向同项目复合FK保留。
- B07 main/leader互斥与两组四列普通唯一键保留。无Issue会话不进入room表，B09自身持运行绑定，不伪造Issue；真实RepositoryIssue仍首次委派才登记。
- SSE在Issue行保留generation/high-water/pruned-through，连续前缀清理与一致快照重放齐全。`sequence=pruned_through`允许从下一序号继续，全部历史事件清空仍能区分缺口。创建事件只在首次聚合提交时要求存在，不阻止24小时清理。
- B09首消息清理与standalone输入/回执清理已经同步说明，不再重复报告已修问题。content_scope_revision保留text，不以数值比较。
- B08仅调整表归属和计数，继续区分静态、数据库、浏览器和真实外部证据，没有新增验收表，也没有将本轮静态审查标为运行PASS。B01–B04及0007扫描目录未改。

以上是文档与关系设计结论。运行状态仍是未实现、未验证；修复后需要静态复读，不需要为了本轮纯文档任务启动真实业务环境。
