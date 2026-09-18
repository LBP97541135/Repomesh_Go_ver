# B06+B07 物理合表方案：14 → 7

分析者 Astra。2026-09-15。只写本临时方案，未编辑仓库，未运行数据库、服务或业务测试。按 unslop 写作要求整理。本轮可以确定下述物理承载；P1、来源、读取范围、回执与 SSE 的已采用语义不变。P9、S06及原候选 API 不因本轮合表自动采用。

## 结论与证据

保留七表：`repomesh_issues.conversations`、`issues`、`changesets`、`repository_scopes`、`issue_events`、`project_issue_counters`、`issue_room_links`。前六归 B06，最后一张归 B07。

`backend-first-batch-persistence.md` §1 已明确允许逻辑记录合并物理表，前提是保留外键、唯一性、比较和保留保障。同节 CreationSource 与操作/Issue 一一对应；§7.1规定本批每 Issue 一个初始 issue_continue。本方案把这些1:1事实放进 Issue 行。操作与 Issue 的身份仍分别生成，查询入口仍按原操作作用域访问。初始待办保留稳定 work_id 与自己的状态，不把 Issue 状态当待办状态。

ChangeSet 保持独立表。`changeset-design.md` 已采用 CS1—CS14，主 ChangeSet 承载候选、组合、PR 与历史，Issue/主CS各有独立身份；目前1:1创建不足以证明可合并其完整生命周期。没有为达到数字把它塞进 Issue。

B07不再扩现有 `repomesh_projects.cursors`。B03/B04的字段、约束和读取行为全部保持；B07使用签名不透明游标。该实现选项已在原 b07 待决第3项列明。游标载荷受完整性保护并绑定主体、项目、列表种类、规范过滤、排序版本、内容/集合版本、扫描位置、limit、过期时间；消息游标额外绑定会话与高水位，具体归B09。游标不是授权。

## 14张旧提案映射

| 原物理表 | 新承载 | 不可丢失的事实 |
| --- | --- | --- |
| conversations | conversations | 独立会话、标题派生来源、内容范围修订 |
| issues | issues | Issue稳定身份、number、主会话、主CS、正文/清理 |
| creation_operations | issues的creation字段组 | 独立操作ID、原复合作用域唯一、原比较器/输入、稳定回执、removed占位 |
| changesets | changesets | 主CS独立ID及同Issue归属 |
| issue_repository_scope | repository_scopes的Issue行且is_work=true | 非空、明确工作集合、同项目仓库FK |
| issue_content_scope | repository_scopes的全部Issue行 | 内容集合只增不减，包含工作集合 |
| conversation_content_scope | repository_scopes的Conversation行 | 整段内容核权与范围版本 |
| page_sources | issues的page_source字段组 | source_id、issue_page来源、操作/会话/Issue一致；无假message |
| conversation_cards | issues的card字段组 | card_id、同source及主会话；敏感副本可清理、关系保留 |
| continuation_work | issues的continuation字段组 | work_id、cause操作、issue_continue、blocked→cancelled责任 |
| issue_event_streams | issues的event字段组 | generation、last_sequence、清理边界、同流锁 |
| issue_events | issue_events | 逐事件主键、与业务事实同提交、可重放 |
| project_issue_counters | project_issue_counters | 项目锁下单调分配，禁止MAX+1 |
| issue_room_links | issue_room_links | main/leader互斥、最新观察及B09/B10完整复合FK |

无视图伪装剩余物理表，无通用entity_id/operation/job/event垃圾表；不把任何字段移进已有36张表。

## 1. conversations

基础列：`id text PK`，`project_id text NOT NULL`，`title text NULL`，`title_redacted_at timestamptz NULL`，`content_scope_revision text NOT NULL`，`removed_at timestamptz NULL`。保留`UNIQUE(project_id,id)`和到现有项目的FK，删除RESTRICT。

页面派生来源列：`created_by_page_operation_id text NULL`，`title_origin_operation_id text NULL`。两个`(project_id, operation_id)`都用可延迟复合FK引用`issues(project_id,creation_operation_id)`；原created_by_operation_id改为前者，名称明确仅指页面来源。必要新会话设创建来源；复制Issue标题时也设标题来源。已存在会话不改创建来源或标题。独立改名清除title_origin，不清created_by。标题与redacted时间二选一的原约束保留；`title_origin_operation_id IS NOT NULL`时必须存在title，且来源Issue的main_conversation_id必须为本会话。后者用延迟约束触发器检查，避免同项目另一Issue误标标题来源。

B09普通会话可以独立创建，两个页面来源列均可为空。B09拥有独立创建幂等字段组和分支完整性，不能要求普通会话先有Issue。B06只要求其新建分支的created_by_page_operation_id非空并与本操作一致，不为整个表施加非空Issue依赖。

与B09已对齐的独立创建字段组：`standalone_creation_id text`、`standalone_creation_actor text`、`standalone_creation_schema_version integer`、`standalone_creation_exact_input bytea`、`standalone_creation_canonical_input bytea`、`standalone_creation_receipt jsonb`、`standalone_creation_committed_at timestamptz`、`standalone_creation_removed_at timestamptz`、`standalone_first_message_id text`，均物理可空但由完整分支CHECK约束。部分唯一`(project_id,standalone_creation_actor,standalone_creation_id) WHERE standalone_creation_id IS NOT NULL`，entry固定为conversation_create；该索引覆盖removed，绝不按活状态过滤。XOR：页面created_by非空且standalone整组为空；或页面created_by为空、standalone id/actor/schema/committed_at非空。后者活记录exact/canonical/receipt齐全，清理占位三项全空而键、actor、时间和first_message_id保留。actor引用稳定account。

独立创建回执只存B09 §2.7定义的封闭版本化形状，冻结当次title/revision/messageId/sequence，不能用当前title重建原结果。firstMessage省略时first_message_id及回执sequence均null，不造消息/工作；有首消息则同事务引用B09 messages的同项目同会话复合键，并延迟检查回执身份/序号。submission_id按完整创建作用域稳定派生，不能只用裸creationId。字段具体来源与首消息完整性由B09负责；JSON不承载开放状态机或任意子记录列表。

B09 `conversation_message_counters`的两列落本表：`last_message_sequence bigint NOT NULL DEFAULT 0 CHECK(last_message_sequence>=0)`、`message_sequence_updated_at timestamptz NULL`。它们在B06创建时保持0/null；B09所有真实消息写路径锁本会话行、分配序号并持锁到提交，分页水位读此值。页面来源卡片不分配消息序号。B09自己的创建输入/原回执字段由B09方案列明，不能用B06 Issue创建回执代替。

## 2. issues

### 身份和业务字段

`id text PK`、`project_id text NOT NULL`、`number bigint NOT NULL CHECK(number>0)`、`main_conversation_id text NOT NULL`、`main_changeset_id text NOT NULL`、`title/description/criteria`按原契约类型与长度保存但允许清理后NULL、`revision text NOT NULL`、`content_scope_revision text NOT NULL`、`work_scope_revision text NOT NULL`、`created_at timestamptz NOT NULL`、`removed_at timestamptz NULL`。

保留`UNIQUE(project_id,id)`、`UNIQUE(project_id,number)`、`UNIQUE(project_id,id,main_conversation_id)`。主会话用`(project_id,main_conversation_id)` FK到conversations；主CS用`(project_id,id,main_changeset_id)`可延迟FK到changesets(project_id,issue_id,id)。身份、所属项目、首次主会话、主CS及创建时间不可更新。业务正文编辑若未来采用，必须提升revision；本批只创建与清理。

P9仍候选。若另行采用，其`initial_configuration_revision`放在本行；同项目FK与历史删除RESTRICT、owner/profile/secret一致性等按原候选落实。操作结果和待办不再复制第二个pin，通过本行取得同一事实，回执仍不暴露pin。当前物理合表决定不把P9的NOT NULL要求写成已采用义务；P9收口前仍不得落库真实可接续Issue，原开工条件不撤销。

### 创建操作字段组

`creation_operation_id text NOT NULL UNIQUE`、`creation_actor text NOT NULL` FK到稳定account、`creation_entry text NOT NULL CHECK(creation_entry='issue_page')`、`creation_id text NOT NULL`、`creation_schema_version integer NOT NULL CHECK(creation_schema_version=1)`、`creation_exact_input bytea NULL`、`creation_canonical_input bytea NULL`、`creation_digest bytea NULL`、`creation_receipt jsonb NULL`。

核心唯一键`UNIQUE(project_id,creation_actor,creation_entry,creation_id)`覆盖活行和removed行，不能建仅活跃部分唯一。另设`UNIQUE(project_id,creation_operation_id)`与`UNIQUE(project_id,creation_operation_id,id)`供来源/后续引用。schemaVersion保留，即使removed也不改。Issue ID仍不是creationId或operationId。没有独立running/failed操作行；先生成全部ID后在同一事务插入本行，循环FK延迟检查，未完成事务不对外可见。

结构化结果直接来自本行的id/number/main_changeset_id/main_conversation_id/project_id/created_at，稳定回执在创建时由这些值构造。回执schema按既有HTTP形状，`receipt`的JSON形状与身份/时间用检查函数或延迟触发器核对。正文清理不允许保留exact/canonical/digest/receipt。`CHECK`分支必须完整：非removed要求正文、原输入、回执非空；removed要求这些敏感字段均为空，身份与操作作用域仍非空。摘要只是比较加速器，最终比较canonical bytes。

此字段组只实现当前页面建项来源；未来可信Manager创建需要自己的已确定entry/来源约束后显式扩展，不能把任意entry放进本次CHECK，也不虚构其消息来源。

### 页面来源和卡片字段组

保留独立稳定`page_source_id text NOT NULL UNIQUE`和`conversation_card_id text NOT NULL UNIQUE`，`page_source_kind text NOT NULL CHECK(...='issue_page')`。来源actor、操作、Issue、project、conversation均直接引用本行已有事实，不重复存一套可串线ID。card的source关系由同一行提供；延迟完整性检查要求二者同时存在。既有候选允许保存的敏感source/card摘要可用独立可空正文列，按已有字段定义命名；不得新增无来源的消息正文、message_id或投递。

查卡片按`main_conversation_id`索引读取该会话关联的Issue行，并在完整会话范围核权后投影。一个会话可以命中多条Issue。清理保留source/card ID和关系，清空敏感副本；不能因卡片不再是独立行而丢失来源身份。

### 初始待办字段组

`continuation_work_id text NOT NULL UNIQUE`、`continuation_kind text NOT NULL CHECK(...='issue_continue')`、`continuation_state text NOT NULL`、`continuation_reason text NOT NULL`、`continuation_cancelled_at timestamptz NULL`。

cause恒为本行creation_operation_id、target恒为本行id、project/actor也取本行。需要SQL FK的跨批消费者引用`UNIQUE(project_id,id,continuation_work_id)`，不只记一个裸work_id。等价唯一性`UNIQUE(creation_operation_id,continuation_kind,id)`可明确声明，尽管本行1:1已排除了重复。创建只能`blocked/INTEGRATION_NOT_AVAILABLE/null`；本批唯一更新为`cancelled/CONTENT_REMOVED/非空时间`，禁止反向。removed必须cancelled；blocked不得存在外发事实。本批不存在externalOperationId/phase/leaseOwner字段或记录，所以不会误把已外发工作当可取消。

如保留§3逻辑DurableWork字段的物理位置，应作为本Issue的同一初始责任字段组，未来消费者依单独采用协议增加租约与核查字段；不能以本次合表授权运行消费者，也不能泛化为所有任务的共用列。后续B09消息处理、B10执行各自持有其责任记录，不往该字段组追加第2条任务。创建与清理后可扫描同一work_id恢复原责任，内存唤醒不是事实。

### SSE流字段组

`event_generation text NOT NULL`、`event_last_sequence bigint NOT NULL CHECK(>=0)`、`event_pruned_through bigint NOT NULL DEFAULT 0 CHECK(>=0 AND <=event_last_sequence)`。创建事务写generation和序号1，event_pruned_through为0。

同Issue事件写入先锁本Issue行，更新last_sequence并插事件，持锁到commit；任何写路径均如此。事件字段不与Issue业务revision混用，更新counter不伪造业务编辑。event_pruned_through是当前generation已清理连续前缀的末序号，供空事件表或全窗口被删时仍能精确判断重放缺口。

## 3. changesets

保留`id text PK`、`project_id text NOT NULL`、`issue_id text NOT NULL`、`kind text NOT NULL CHECK(kind='main')`及原创建时间。`UNIQUE(project_id,issue_id,id)`供Issue主CS FK；`UNIQUE(project_id,issue_id)`保证本批恰一个主CS；`(project_id,issue_id)`可延迟FK到issues(project_id,id)。相互循环在提交时检查，删除RESTRICT。

主CS独立ID和后续历史承载保留。将来支持非main需显式迁移改为main部分唯一，当前不提前扩枚举。

## 4. repository_scopes

单行表示明确Issue或Conversation对一个仓库的内容范围归属；Issue行上的is_work表示该仓同时属于工作集合。

列：`scope_id text PK`、`project_id text NOT NULL`、`repository_id text NOT NULL`、`issue_id text NULL`、`conversation_id text NULL`、`is_work boolean NOT NULL DEFAULT false`、`introduced_by_page_operation_id text NULL`，以及首次引入时间/依据的原逻辑字段。`work_issue_id text GENERATED ALWAYS AS (CASE WHEN is_work THEN issue_id ELSE NULL END) STORED`。

完整分支CHECK：`(issue_id IS NOT NULL AND conversation_id IS NULL) OR (issue_id IS NULL AND conversation_id IS NOT NULL AND is_work=false)`。再明确`CHECK(NOT is_work OR issue_id IS NOT NULL)`。两类owner都有实体FK：`(project_id,issue_id)`到issues；`(project_id,conversation_id)`到conversations；`(project_id,repository_id)`到旧project_repositories。都RESTRICT。页面引入时`(project_id,introduced_by_page_operation_id)` FK到issues的operation唯一键；未来真实消息引入依据由B09定义，不能强制所有conversation scope都来源于Issue。

`UNIQUE(issue_id,repository_id)`、`UNIQUE(conversation_id,repository_id)`防各自重复；NULL分支由CHECK排他。`UNIQUE(work_issue_id,repository_id)`及`UNIQUE(project_id,work_issue_id,repository_id)`是工作范围专用FK目标。这是普通唯一约束，不能用部分唯一索引充当FK目标。B07/B10通过该目标约束选中的是is_work=true的Issue行，不能把仅历史内容仓当工作仓。

原issue_repository_scope(issue_id,repository_id)的下游FK改为repository_scopes(work_issue_id,repository_id)，本地引用列issue_id/repository_id不必更名。原issue_content_scope按`issue_id IS NOT NULL`读全部；conversation_content_scope按conversation_id读全部。每个Issue提交时至少1个is_work=true行；工作集合天然是内容集合子集。延迟约束检查每个Issue内容仓都存在于它主会话的content集合中。scope新增和owner范围revision提升同事务，任何内容行删除、owner/repository身份改写都拒绝。本批工作标志创建后不可改，后续范围缩减若另行采用只改is_work且不能删内容行，已有执行FK/责任存在时仍RESTRICT。不得让旧scope物理只增不减表述误解成可以任意扩大当前工作集合。

只锁scope子行不足以避免并发范围写偏差。所有范围扩展先锁project、再按会话/Issue固定对象序锁owner，检查superset与修订；创建/清理/消息引入沿同一规则。后续Issue内容扩大必须同步扩主会话范围，不得先发布新内容再补权限集合。

## 5. issue_events

`issue_id text NOT NULL` FK到issues，`generation text NOT NULL`、`sequence bigint NOT NULL CHECK(sequence>0)`、`kind text NOT NULL CHECK(kind='snapshot_invalidated')`、`created_at timestamptz NOT NULL`，`PK(issue_id,generation,sequence)`。删除Issue RESTRICT，事件保留清理显式执行。不对event_generation建立到Issue当前generation的FK，否则换代会强迫删除历史事件。

写入触发器/仓储协议检查持有Issue锁、写入generation等于当前流代次、sequence来自该事务分配。创建完整性只在首次创建提交时要求第1个事件存在，不能在每次Issue更新时要求永远存在创建事件，否则24小时保留清理会被误拒绝。

默认24小时来自已采用创建契约。清理锁Issue后只删除当前代次的连续前缀并原子推进event_pruned_through；旧代次记录也可按保留规则清理。Last-Event-ID仍是不透明保护的(issue,generation,sequence)。错Issue、篡改、旧generation、sequence<pruned_through或sequence>last_sequence都resync_required。sequence=pruned_through可从下一序号重放。若未来换代，必须锁Issue、明确生成新generation并初始化计数/边界，不靠进程重启自动换代。

重放在有界一致快照中读取generation、边界、高水位及事件；清理前取得完整快照的重放可以完成，清理后建立快照的按新边界判定。发布器只扫描已提交事件，发送不删行、不记浏览器已读；重启扫描主库恢复。线上仍发issue.changed，changed保守投影为[issue,rooms]；不复制正文、房间凭据或授权状态到事件。连接期间重核权限，失权停止发送。B07详情/rooms读取仍分开，不宣称跨请求同一快照。

## 6. project_issue_counters

`project_id text PK` FK到旧projects、`next_number bigint NOT NULL CHECK(>0)`。创建先取得既有项目行锁，再在新表初始化/锁计数行，以UPDATE RETURNING分配。项目内number唯一，回滚/迁移允许间隙。不改旧projects、不使用MAX+1、全局序列或新通用counter表。

## 7. issue_room_links

原列全部保留：room_link_id主键，project/issue/kind/instance_id/upstream_room_ref，conversation_id/repository_id分支，created_at/retired_at，availability/reason_code/observed_at/expires_at/observation_revision/runtime_generation/observer。

完整分支CHECK不变：main要求conversation_id非空且repository_id空，leader反之。`(project_id,issue_id,conversation_id)` FK到issues(project_id,id,main_conversation_id)。leader的`(issue_id,repository_id)` FK改指repository_scopes(work_issue_id,repository_id)，并保留(project_id,issue_id) FK到Issue。可使用三列工作范围FK进一步直接固定project。

保留有效main部分唯一`(issue_id) WHERE kind='main' AND retired_at IS NULL`，`UNIQUE(issue_id,kind,instance_id,upstream_room_ref)`，B09所需`UNIQUE(project_id,issue_id,conversation_id,room_link_id)`与B10所需`UNIQUE(project_id,issue_id,repository_id,room_link_id)`。以上复合键都保留，不能让调用方只凭room_link_id串线。

runtime_generation是不透明身份，不能按字典序比较新旧。普通观察写入要求当前generation精确相等、revision严格递增且未退役；可信运行代次切换单独锁Issue/关联并CAS旧generation，原子登记新generation与对应观察，旧代次其后全拒绝。探测失败登记更晚revision的unknown而保留关联；过期读作unknown/OBSERVATION_STALE；退役行禁止再覆盖。room观察和其失效事件同本地事务写，先锁Issue再锁关联，避免与事件计数逆序。运行代次与B09 Manager逻辑epoch分别核查。

无Issue普通会话不写本表。B09 manager session以自己的instance/transport/state绑定运行，关联Issue后才可用上面的main复合FK。Leader条目还须存在B10首次真实委派登记的RepositoryIssue；创建选仓不造仓库事项或Leader。房间行不重复存repositoryIssueId，B07按Issue+repo读B10身份。

## 统一事务、清理、重放

保留§7.2共同owner边界：交互写先锁binding/session/account，再project和所需connection；持有后段锁后不回拿account。原独立operation行锁现在就是目标Issue行锁。为免会话/Issue逆序，已存在业务对象统一按conversation先、Issue后且同类ID排序取锁；由原操作键预查出会话ID后锁conversation再锁Issue，并重读作用域/归属防陈旧定位。不存在的操作靠owner/project锁与最终操作唯一约束串行，不把SELECT不存在行当锁。目录/profile/policy/secret锁仍遵循现行逐路径规则，B03/B04不改。

新创建仍在事务外获取准确完整范围观察。短READ COMMITTED写事务内锁主体/project、重查原键，命中优先走旧结果；无命中才核新creationContextRevision/会话范围/固定政策。项目counter分配编号；生成全部ID，写conversations必要分支、Issue整行、scope、changeset、创建事件，提交前延迟核所有最终关系。无独立受理占位提交。2秒/5秒、S06的60秒与能力门槛仍按原候选状态标记。

原结果按操作唯一键从Issue读最小身份和存量Issue/Conversation完整范围，事务外核权，短事务按上述锁序重比本地身份和范围修订后投影。removed有权410，先于正文比较；当前无权隐藏404；完整结果按所存schema比较返回原200或409。不读新输入范围替代旧范围，不重新检查App写能力/预算/新建条件。COMMIT未知503，404仍不证明旧请求不会提交。

清理沿同一锁序锁主会话和Issue；清空Issue正文、exact/canonical/digest/receipt、source/card敏感副本，取消初始未外发待办，写removed_at。title_origin等于被清理操作时才清会话派生title并写redacted_at；独立改名标题保留。保留Issue/operation/source/card/work/CS/会话身份和全部scope。所有清理在一个事务提交；无物理删除、无键复用、无反向复活。若未来消费者扩展了外发事实，本维护入口必须先判定适用性，不能用cancelled推断外部停止。

延迟约束检查最终行，覆盖Issue与CS双向归属、必要新会话创建来源、scope包含、source/card存在、创建receipt一致、初始blocked待办和序号1事件。创建完整性只在创建聚合首次提交要求初始状态，后续清理用removed分支，后续事件保留不再要求最早事件。identity更新、scope删除与removed复活用立即保护拒绝。

## 跨批和同步位置

- B09：消息计数进conversations，所有writer锁该行；独立会话创建字段组由B09限定，页面来源FK可空。manager_sessions的四列main房间FK保持；普通会话不要求Issue。读取范围改读repository_scopes。B09的消息、澄清、投递含义不由本方案补造。
- B10：repository_issues、plan_revisions、round_plan_pointers及其他工作范围FK统一改到repository_scopes(work_issue_id,repository_id)，原issue_id/repo列形状不变。B07房间五类身份/归属复合键保持。Issue、CS与RepositoryIssue互不替代。
- B11：Issue/CS引用仍独立，通过B10工作范围/真实仓库事项继续校验，不从scope行推导已委派或已执行。
- `docs/api-database/b06.md`：重写数据表、触发器、锁序与计数，物理七表清单需与本方案一致，P9候选框保留。
- `docs/api-database/b07.md`：scope与事件计数目标替换、signed cursor取代旧表扩列、room复合FK指向调整、清理边界规则及索引表同步。
- `docs/api-database/b09.md`、`b10.md`、`b11.md`：同步上述直接FK与counter引用，不保留幽灵旧表名当现行物理依赖。
- `docs/current/backend-first-batch-persistence.md` §1/1.1/1.2/2.2/3/4/7：保留已采用逻辑记录与行为，增加本轮物理映射/替代说明；将“DurableWork表”说明为域责任记录可内嵌拥有者，本批initial work在Issue行，不暗示另有未计数共享job表。§7逐路径锁序明确operation与Issue合行后的对象顺序，避免原文字让人先锁Issue再锁conversation。
- `docs/current/issue-configuration-binding-design.md` §8：只同步候选P9引用的新物理位置，仍proposed，不能写成已采用。
- `docs/current/backend-message-clarification-design.md`：由B09作者同步消息counter物理映射，逻辑定义保留。
- 新增本轮物理设计采用/变更说明，并从`docs/current/README.md`和HANDOFF链接；说明只确定布局、未实施、P9等仍待采用。
- `docs/development/2026-09-13-b04-b06-design-01/migration-design.md`是历史证据，不覆写旧结论。新说明精确指明其B06表/锁序部分被替代，当前文档不再把旧附件作为唯一现行物理权威。
- API-database README/foundations/表清单/渲染HTML同步本轮数字；不把旧逻辑名称、JSON字段或视图重复计成物理表。

## 实施时必须证明的拒绝与恢复

直接SQL尝试跨项目scope、同时两个owner、conversation行is_work=true、Leader引用仅内容仓、主room错会话、B09绑定Leader、B10错Issue同仓、Issue错主CS，全部提交失败。相同创建键并发只剩一行Issue和原三业务ID；removed旧键任意正文仍410且不可新建。创建每写点失败无半聚合；清理后scope/身份与work cause仍可查。SSE先提交小号才可见大号、清理与重放无半个窗口、全事件清空仍能判断旧游标缺口。普通会话零Issue可落库，页面卡片不分配消息序号。上述均为未来验证要求，本轮仅做文档/源码阅读，没有声称运行通过。
