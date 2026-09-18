---
status: accepted
date: 2026-09-10
---

> **物理承载替代说明（2026-09-15 更新）：** 本文的逻辑契约与候选状态不变。物理表承载自 2026-09-15 起以 [Go 版数据库重构方案](../RepoMesh_Go版数据库重构方案.html) 的 44 张目标表为准，对应接口见 [API 设计](api-design.md)；此前的 B05-B11 物理合表已归档到 [历史目录](../archive/2026-09-15-api-database-catalog/README.md)。

# 首批后端持久化与恢复协议

本文由后端维护，细化 B02—B04；共享业务语义对应 RM-B01-04 r2，双方第2／3轮已ACK，原文见[本轮日志](../archive/2026-09-12-development-preparation/docs/current/design-communication-backend-2026-09-10.md)。accepted仅指本批内部设计采用，不表示B01—B04全部闭合或实现通过。浏览器字段与 HTTP 错误只以[创建契约](issue-page-create-api-contract.md)及页面负责的项目接口为准。本文字段是内部逻辑存储设计，不是迁移文件、Go 类型或另一套 REST Schema。仅覆盖项目保存、手动建项、列表与详情；当前已实现[数据库连接和迁移基础](database-development.md)，尚无本文业务表或运行实现，本文业务验收均待执行。

**2026-09-12 审查补充：** §2.5 是 R04 的推荐修订，状态为待采用；本文原 `accepted` 不包含该新增选择。其余原采用规则保持。真实 Issue 与持久待办首次落库前，需要先确定可追溯的配置绑定，不能等运行消费者接入后补猜历史版本。

## 1. 最小关系和保留约束

首批每个项目由创建者拥有，本地访问资格不从 GitHub 同仓协作关系推导。初期不开放成员邀请或所有权转移。一个用户可有多个同名项目；名字不承担操作去重。外部身份使用 provider + host + stable account/repository ID，改名或转移不制造新仓库；外部身份关系变化仍须重新核权。

| 逻辑记录 | 最少归属／内容 | 必须保持的约束 |
| --- | --- | --- |
| User / ExternalIdentity | userId、provider、host、externalAccountId、本地停用修订 | 外部身份唯一绑定一个本地用户；不凭 email／login 合并或重绑。首批一个用户绑定一个 GitHub 账号。 |
| Project | id、ownerId、名称／用途、projectRevision、创建条件代次、删除标志 | id 永不复用；ownerId 引用稳定用户；配置不含密钥正文；保存本身不产出会话／Issue／运行待办。 |
| Repository | id、host、externalRepositoryId、可变展示信息 | (host, externalRepositoryId) 唯一；同名不是同仓。展示值按当前读权返回。 |
| ProjectRepository | projectId、repositoryId、加入修订、加入时间 | (projectId, repositoryId) 唯一；首批只增不删。项目范围新增不改已存在 Issue 范围。 |
| ProjectConfigRevision | projectId、revision、明确覆盖值／默认引用版本、有效配置引用、创建者 | 配置修订追加且不可变；profile／secret版本只在显式configuration提交时重新固定，后台只更新失效观察，不能跟随新默认。密钥只留secretVersionId。 |
| Conversation | id、projectId、title、业务修订、删除标志 | 项目内独立业务身份；初始标题可取创建 Issue 标题，之后不联动；不存伪造用户消息。 |
| Issue | id、projectId、number、primaryConversationId、mainChangeSetId、正文、revision、删除标志 | (projectId, number) 唯一；(projectId, primaryConversationId) 必须引用同项目会话；首批每 Issue 恰一个主会话且不可改绑。 |
| IssueRepositoryScope | issueId、projectId、repositoryId、范围修订 | 创建时非空、明确、无重复，均属于 ProjectRepository；后续范围改变属于计划规则，不由更新项目触发。 |
| RepositoryIssue | id、issueId、projectId、repositoryId、登记／停用信息 | (issueId, repositoryId) 全历史唯一，不用“仅活跃唯一”允许终止后再建一个；属于本 Issue 的工作范围。建项时可以零条，真实委派才登记。 |
| ChangeSet | id、issueId、projectId、创建时间 | 每 Issue 恰有一个主 ChangeSet 引用；ChangeSet 必须属于该 Issue。其他后续 ChangeSet 不在首批创建。 |
| ContentRepositoryScope | objectKind、objectId、repositoryId、首次引入依据 | 每对象每仓唯一，只增不减；Issue 包括当前工作和历史材料，会话包括卡片、消息、工具结果引入的仓库。 |
| CreationOperation | 作用域、creationId、schemaVersion、canonicalInput、digest、确切输入、结果身份、committedAt、removedAt | 作用域见 §2；仅成功提交或无正文删除占位可对外观察；输入和结果在同事务中完整。 |
| CreationSource | operationId、sourceKind、actorId、conversationId、真实来源引用、可选分析来源 | 与操作和 Issue 一一对应的创建来源，不冒充聊天；MCP 的真实 message 引用由 B05 定义后才能启用。 |
| DurableWork / Event | 见 §3／§4 | 与业务事实同事务，不以进程内通知代替持久责任。 |

表中的逻辑记录可在实现时合并物理表，但外键、唯一性、比较和保留保障不得丢失。跨项目引用使用带 projectId 的复合外键／等效约束，不能只在 HTTP handler 检查一次。外键删除默认 RESTRICT；正文清理使用显式业务清理事务，不对来源、主 CS、幂等结果级联硬删。

Issue 与主 ChangeSet 存在双向归属。建议主引用为非空，使用事务提交时检查的可延迟复合外键；ChangeSet 的 (issueId, id) 唯一，Issue 的 (id, mainChangeSetId) 指向该组合，使“主 CS 属于另一 Issue”不能提交。具体 DDL 随迁移复核。项目内编号在项目锁下更新事务计数器；允许编号不连续，不能用最大值加一或标题计算。

RepositoryIssue 只是稳定业务委派归属，不含上游 Project／Task／Attempt 的同一性假设。同仓多个任务、修复轮次和再次委派沿用原 repositoryIssueId 并追加历史；停用不证明工作已停止。该记录的登记／停用不直接生成执行成功、资源释放或 Leader 房间。完整执行状态由 B07 细化，Issue 完成后新增工作另建关联 Issue。

### 1.1 内容范围与关联提交

页面新建 Issue 时，Issue 内容范围初始化为明确仓库集合；创建来源卡片与会话内容范围扩展在同一事务完成。选已有会话须既有整体内容读权和关联资格；创建后该会话范围为原集合与本次 Issue 范围的并集。卡片没有聊天投递记录。

读取 Issue 正文、标题、来源、CS 摘要和创建回执前核验完整内容范围；会话标题／正文／派生摘要按会话完整范围核权。页面可得到无敏感正文的受限提示，但不能通过关联列表、搜索计数或操作回执恢复已无权材料。项目配置外壳按本地 owner 权限继续可读，受限仓展示只使用允许披露的占位，不回显旧仓库名。

无可靠逐消息标注的自由消息，按保存时整个项目仓库范围登记；受控工具／回复新增材料范围先登记并核权，再发布内容。项目后续新增仓库不凭空污染旧会话，只有新内容引入时扩展；后续缩小工作范围不会减少历史内容保护集合。详细消息投递属于 B05，本文不能使未接入聊天变为可用。

### 1.2 删除与清理

首批浏览器不开放项目物理删除、删除仓库或主会话迁移；存储仍必须支持运维清理／未来正文删除不破坏幂等。删除 Issue 正文时，在同事务中清理其确切输入、摘要、来源正文及可重建该内容的派生副本，设置操作 removed 标志，保留作用域、键、必要结果归属和内容权限范围。摘要也删除，不能留低熵正文哈希作为内容副本。

旧键命中 removed 占位先核验当前权限，再返回 410；不再比较新正文，不重建。项目保留期间这些键永不复用。物理清理项目须先关闭写入口并串行锁定项目、取消尚未外发的待办，对未知外部动作保持核查责任；B06/B07 未完成前不允许据此设计“一键强删并释放”。物理删除后旧 projectId 永不重新分配，关联操作不可借新项目恢复。保留审计的身份可脱敏，但不能删除到让旧请求获得新的创建资格。

## 2. 创建事务与并发幂等

### 2.1 操作身份与比较

Issue 页面操作唯一作用域为 (projectId, actorUserId, entry=issue_page, creationId)，creationId 即原 UUID Idempotency-Key；MCP 使用独立 entry 和可信逻辑身份，不能重放页面入口的键。项目创建时尚无 projectId，作用域为 (actorUserId, entry=project_create, creationId)，成功回执再绑定生成的项目；不能以暂时项目名充当作用域。

schemaVersion 与规范化算法版本持久保存。规则严格复用创建契约：对象键顺序无关、repositoryIds 排序但重复先拒绝；验收顺序和所有文本内容保留；省略验收等价 []，省略 conversation 等价 new；修订及可选分析 ID 纳入比较。拒绝重复 JSON 属性名、未知字段和非法 Unicode，避免不同解析器产生两个输入。确切输入指解析后允许字段的实际值，不要求保留 JSON 空白排版。

摘要用于快速定位差异，最终用规范化表示的等值比较防止单靠哈希认定相同；不能由客户端提供摘要，也不随部署升级用新算法改判旧操作。schemaVersion 对应旧比较器在操作保留期内必须可用。已提交重放只做当前结果读权与原输入比较，不按新配置、分析插件或新创建门槛改判历史。

### 2.2 有界提交步骤

采用 PostgreSQL 主库 READ COMMITTED 写事务，显式锁相关稳定行及最终唯一键，普通外部查询不持锁。唯一约束不能被预查询替代；READ COMMITTED 每条语句可以见到新的已提交结果，竞争失败后的回读必须是新语句，不能复用插入语句原快照。这是协议对实现的要求，数据库行为依据见[官方隔离说明](https://www.postgresql.org/docs/current/transaction-iso.html)。

1. 认证、限制请求体大小、核当前本地项目访问资格。从主库查同作用域已提交操作：removed 先按当前读权返回 410；完整结果按其内容范围核权，再比较输入并返回 200 或 409。
2. 尚未提交时校验输入、明确仓库集、会话关联和预期创建修订；事务外获取有时效、带 credentialVersion／accessEpoch／repository IDs 的权限观察。未知为 503，不伪装拒绝或成功。
3. 开启短事务。按固定顺序锁用户访问版本、项目、所选会话／Issue等对象；任何本地停用、项目修订或会话内容范围修改路径必须遵守同一顺序。已有项目所有配置更新与创建共享项目锁。
4. 在锁后再次查同作用域操作；若已提交走原结果路径，避免把重放误报配置冲突。否则核对本地 accessEpoch、项目创建修订、会话资格／内容范围版本、配置引用、分析来源状态及外部观察时效；观察过期则退出事务后重核，不持锁等待远端。
5. 插入事务内操作占位。最终唯一键失败使用 ON CONFLICT DO NOTHING 之类不会留错误事务的路径，再用新语句回读赢家；不得吞掉所有唯一异常。占位与业务结果一起提交，没有独立已受理状态。项目创建无项目锁可用，凭用户锁和最终唯一约束串行同主体操作。
6. 新建必要会话、Issue、范围、主 ChangeSet、创建来源／卡片、内容范围、确切输入／规范化结果、持久待办和事件；填满操作结果。必要写入任何一个失败均回滚。
7. 提交确认后返回 201；连接断开或 COMMIT 回执丢失标为结果未知，从主库查询原键。没有提交证明时不返回 201，也不盲目重放外部操作。

建议内部锁等待预算 2 秒、单事务总预算 5 秒，作为可调初始上限，不承诺容量；外部预核另设超时。超限、序列化冲突／死锁重试耗尽或提交不确定，对页面按现行 503 RESULT_UNCONFIRMED 核查。任何内部重试仍复用相同键和确切输入，不能重造逻辑操作。已明确回滚可重试事务；不确定是否提交则先查操作。

配置锁不消除外部权限检查后的竞态。观察中明确记录检查时间，后续每个敏感动作再次核验；不能把 PostgreSQL 提交称为 GitHub 授权与业务原子提交。

### 2.3 并发推演

| 交错 | 唯一提交结果及客户端观察 |
| --- | --- |
| A、B 同键同输入均预查为空 | 一个获得最终唯一身份并提交全套；另一个新语句读赢家返回200。等待超限先503，随后原键查询。 |
| A、B 同键不同输入均预查为空 | 首个成功提交绑定输入；另一个409；若首个实际回滚，另一请求可成为唯一成功者，不能声称失败输入永久预留了键。 |
| A事务内插入占位后崩溃 | 未提交记录回滚，不出现可查询running；B原键重试可完整提交。 |
| A提交成功但响应丢失，B先查404 | 404只表达该次查询尚无可见结果；B不换键。A后来提交时唯一约束仍只允许一套。 |
| 操作成功后配置变更／来源分析清理 | 当前结果读权成立时仍200原回执，不走新创建条件。 |
| 正文删除与同键重放并发 | 在相同对象／操作锁下串行；清理提交前可按当时读权读到旧结果，清理提交后只410；不返回半清理正文。 |
| 会话内容范围扩展与读取／关联并发 | 快照内按同一范围版本核权；发出响应前核本地权限／范围版本，变化则重新核验或拒绝，不能按旧小范围读取新正文。 |
| 两个不同键同标题 | 两个独立Issue／操作，不按标题合并。 |


### 2.4 项目创建、更新与配置版本

浏览器字段唯一来源为[首批浏览器契约](first-batch-browser-api-contract.md)（RM-API-01 r3），不复用Issue操作URL或错误名称。三类操作可共享内部存储基础，但entry固定并严格隔离。

| 操作 | 唯一作用域／保留 | 同事务写入／重放 |
| --- | --- | --- |
| project_create | (actorUserId, project_create, projectCreationId)；账号保留期不复用，项目物理删除后仍保留账号下无正文占位 | 项目、owner、完整范围、固定配置引用、输入快照、回执一起提交；201首次／200重放。删除结果使用PROJECT_CREATION_RESULT_REMOVED，不能借新projectId复活旧创建键。 |
| project_update | (projectId, actorUserId, project_update, updateId)；项目保留期不复用 | 资料、增仓、配置、修订、输入／回执一起提交；均200。先查重放再判断expectedProjectRevision，不因后来修订而拒绝旧成功。 |
| issue_page | (projectId, actorUserId, issue_page, creationId)；项目保留期不复用 | 沿用§2.2和创建契约，不与以上两种键互通。 |

配置输入只接受modelProfile和executionProfile引用；省略整个配置等价首次创建两项inherit，更新省略则保持原固定配置，两种操作比较器不得混用。更新的name／purpose保留确切文本，增仓集合排序比较，重复ID先拒绝；已经存在的仓库ID可作为集合幂等输入保留，不重新变成待新增范围。最多100仓，长度和错误由浏览器契约约束。

新建项目先整体核所有所选仓库的用户读权；已知不足或未知不落项目。App能力、模型／环境缺失可以受限保存。project_create回执只返回当次固定结果及项目外壳链接，不带仓库敏感正文；原项目外壳owner资格决定重放可见性，不能要求所有当前仓库读权才能修复配置。

更新先锁user访问版本和project，查同键操作。新操作再比较expectedProjectRevision，读取数据库原范围，只计算明确增量；不把客户端可见列表当全集。只改资料／配置，不要求恢复旧受限仓读权；新增集合任一核查失败整个更新回滚，原集合保持。更新与创建不触发运行准备、扩大在途Issue范围或给旧计划新授权。

显式提交configuration时重新解析profile版本、secretVersion和有效参数；即使仍选择inherit或同一profile，也明确核查是否选定了新版本。仅配置选择、固定引用版本或有效值实际变化时分配新的configurationRevision，并更新相应projectRevision与creationContextRevision；完全无变化时保留原修订，仍保存该操作成功回执。只改名称／用途或加仓不得顺带重解析模型配置。

后台发现固定版本被停用／撤销，只更新检查观察、accessEpoch及相关creationContextRevision，不能切到另一profile、最新secretVersion或新默认。平台默认变化可提示可更新，但只有用户显式PATCH配置才重新绑定。有效旧版本仍可继续；无效版本保持受限。默认配置初始不存在时允许inherit的固定未解析状态，后来新增默认也须明确配置提交才能建立新的有效绑定。

配置读取检查关注引用可见、启用、密钥版本存在及必要参数；不通过checks=allowed宣称模型已往返或环境Ready。首次创建缺引用时为受限记录；不可引用的显式ID隐藏404，不把陌生ID存为正常配置。多个默认／profile并发更新时，在项目配置提交事务内核验相应版本未变／可引用，失效则整体返回契约错误，不写混合版本。

成功回执的projectRevision、configuration选择依据和时间均保持当次提交事实；后续改名后重放不能返回最新revision而假装它是原结果。当前状态用GET重新查询。项目更新查询404同样不证明原请求不会提交，正文清理后占位返回PROJECT_UPDATE_RESULT_REMOVED。浏览器原键/输入保留与未知核查沿用创建恢复保障，具体路由以页面契约为准。

项目创建／更新若未来需要可靠权限核查待办，仍同事务保存有限核查责任；这些待办绝不等价实例准备。首批以显式查询及操作前核查满足管理面要求，不为保存项目生成runtime操作。

### 2.5 Issue 固定配置引用（R04 推荐修订，待采用）

现有 §1 保存了不可变 `ProjectConfigRevision`，但尚未规定 Issue 到该记录的固定引用。推荐为新 Issue 增加不可改写的内部 `initialConfigurationRevision`，以 `(projectId, initialConfigurationRevision)` 引用同项目 `ProjectConfigRevision`；创建操作内部成功结果保存一致版本，Issue 接续待办沿目标 Issue 解析。该字段不是客户端输入，不改变已采用的创建 HTTP 契约或规范化输入，也不补造尚未定稿的 MCP Schema。

按 §2.2 步骤4—6，在同一项目锁内完成条件比较、固定当前配置记录，并与 Issue、操作结果及待办一起提交；先查成功重放，不能因项目后来换配置改写原版本。`creationContextRevision` 继续作为条件比较令牌，不能充当该外键。配置绑定保存的是版本依据，动作前仍核当前权限、固定版本资格及预算；“重核配置”不表示重选项目最新版本。

该选择须在首次保存可供以后接续的真实 Issue／待办之前收口并落实约束。已定 CRUD 代码可准备；尚未设计完成的消费者保持 blocked，不能用覆盖共享 Manager 配置实现每 Issue 绑定。旧记录补绑、密钥失效、普通讨论的范围、并发与恢复验收统一见[Issue 配置绑定候选](issue-configuration-binding-design.md)。本节不将专用模型应用协议、旧 Issue 主动换配置或普通讨论处理协议标为已完成。

## 3. 同事务持久待办

选择数据库内 DurableWork 表作为首批队列事实，不增加必需独立 broker。提交后通知只是唤醒提示；后台必须按 nextEligibleAt 主动扫描，丢失提示不丢任务。首批创建记录待处理的是“接续已建业务”，不是让 Manager 再创建一次。

| 内部字段组 | 含义 |
| --- | --- |
| identity | workId、kind、causeOperationId、projectId、targetKind／targetId；唯一 (causeOperationId, kind, targetId)。 |
| authority | actorUserId、来源引用、所需策略版本／目标版本；凭据仅引用，不存可用密钥。 |
| lifecycle | state、nextEligibleAt、failureCount、lastReason、createdAt／updatedAt。 |
| claim | leaseOwner、leaseUntil、claimGeneration；每次领取代次增加。 |
| external | externalOperationId、phase、外部归属、最后证据引用；第一批未接运行端时为空。 |

state 为内部队列状态 pending、claimed、retry_wait、blocked、reconciling、done、cancelled。它们不进入创建回执 status，不代表 Issue、Manager、Worker 或运行资源状态。blocked 必须有具体原因和重新评估条件，不能默认为需要人工业务审批。cancelled 仅说明该待办停止推进，不等于停止外部进程。

领取用有界批量、明确排序的 FOR UPDATE SKIP LOCKED，在短事务写 leaseOwner／claimGeneration／leaseUntil 后提交。建议租约60秒、每20秒续约，依数据库时钟判断；数值可调。结果回写与续租均须匹配 owner＋generation＋未失效租约，否则旧领取者不得覆盖正式状态。SKIP LOCKED 适用于队列竞争，不用于用户列表读取，依据见[PostgreSQL SELECT](https://www.postgresql.org/docs/current/sql-select.html)。

每次处理先重核actor、本地权限、配置及目标有效性。Issue 固定配置的候选选择与该重核的关系见 §2.5；当前未定义的绑定不能通过读取项目最新值补齐。未接入处理器时写 blocked/INTEGRATION_NOT_AVAILABLE，保持准备责任；不能因为已排队就生成 preparing／ready 观察。安装后通过受控扫描重新评估同一 workId，不制造新建项操作。

### 3.1 外部未知与重启

外部操作在 B06 协议完整前不启用。设计预留如下恢复纪律：

- 调用前先以原业务归属登记稳定 externalOperationId 与“可能发送”阶段，再在事务外调用；重试不得用 claimGeneration 充当新的外部操作身份。
- 租约过期且从未进入可能发送阶段，可重新领取并从本地条件核验开始；已进入该阶段，即使实际可能未发出，也进入 reconciling 查原操作。
- 发现旧执行者失去租约不能推断其网络请求、进程或写凭据已停止。外部接收者不支持稳定去重／代次校验或查原操作时，保持未知，不能用新领取者盲重发来实现“至少一次”。
- 重查证明原操作已完成时登记事实并继续原业务流程；证明未执行且旧发送权已失效才允许原操作重发。无法确认则保留 reconciling 和下次核查，不确认资源释放或开替补。
- 已知瞬时错误采用带抖动的 1、2、4…秒退避，上限60秒；连续8次仍失败转 blocked 并保留重核原因。外部 Retry-After 优先；超时未知不能当作已知失败累计后直接重建。配置／权限恢复触发重新评估，不清零业务预算或创建新Issue。

首批没有运行消费者也能验收待办原子保存与重启可发现，但真实实例复用、消息处理去重、环境回收仍须 B05/B06 独立设计和验证。队列领取权、运行生命周期权和 Worker／资源占用是三件事，后两者不能靠此租约实现。

## 4. 查询、修订与事件

### 4.1 修订责任

| 修订／身份 | 覆盖 | 不覆盖 |
| --- | --- | --- |
| projectRevision | 项目名称、用途、明确范围、配置引用的本地变更 | 外部GitHub实时状态、runtime Ready。 |
| creationContextRevision | 本地范围、有效配置版本、适用创建策略 | 任意历史Issue版本或外部权限永远有效。 |
| accessEpoch／credentialVersion | 本地停用、凭据替换／撤销、已确认权限变化 | 外部变化尚未观测的时间窗。 |
| Issue revision | Issue业务快照 | rooms观察、消息游标、Plan Version。 |
| contentScopeRevision | 内容保护仓库集合 | 本轮工作范围缩减或导航目标。 |
| event cursor | 某Issue事件重放位置 | 业务版本、请求幂等键、权限能力。 |

projectRevision 与 creationContextRevision 对客户端均为不透明等值字符串。非创建相关名称变化可只改变 projectRevision；配置有效值或仓库范围变化必须改变创建上下文。没有业务变更的纯核查不制造新配置修订；外部读权每次仍按观察策略处理。

操作查询、详情和创建后回读首批走主库，不依赖可能滞后的副本。一次详情在有界 READ ONLY REPEATABLE READ 中取一致业务记录、内容范围、当前本地访问依据；外部核权在事务外，核对记录版本未变后组织输出，否则重新读取。页面必须分别读取 Issue 与 rooms，不能宣称两次请求是跨系统同一快照。

### 4.2 列表

首批项目／Issue／会话使用 REST 列表刷新，不增加新的 SSE 字段。按RM-API-01 r3采用默认50、最大100、稳定id升序keyset，q为名称／标题字面大小写不敏感子串，最多200标量，不搜索正文或解释模式／正则。具体形状以[浏览器契约](first-batch-browser-api-contract.md)为准。游标含版本、主体、项目、过滤／排序和最后扫描位置，以服务端签名或保存查询状态保护，不能作为权限凭据；非法400 INVALID_CURSOR，过期409 CURSOR_EXPIRED。repositoryId过滤跨项目／无权隐藏404，未知503。

服务端先按当前访问资格筛选，敏感标题／摘要仅在其完整内容范围可读时返回；Issue／会话列表遇候选范围权限未知，返回503 AUTHORIZATION_UNCONFIRMED，不按无权漏掉后声称完整成功。若需扫描隐藏条目才能凑页，限制内部扫描预算；nextCursor记录最后扫描位置而非最后可见项，允许空页带后续游标，页面不得把items=[]当末页。Issue／会话不返回隐藏对象总数。项目仓库列表的restrictedRepositoryCount仅表示owner已保存范围中本次未披露的总量，包含denied和unknown；不得解释成已确认失权数。候选发现可返回明确coverage=partial／unknown，只有有效allowed条目可披露身份；完整覆盖与本地分页是不同事实。

访问资格核查先于敏感标题匹配和投影；不能让隐藏标题是否匹配q决定200／503等可见结果，形成搜索探测。查询层先确定本地可访问对象及内容范围观察，再在允许部分执行标题过滤；未知按本节规则失败，不通过过滤空结果逃过核权。

跨页不持有数据库快照，不承诺总数或所有页面取自同一时刻；插入更早ID、过滤字段改变和外部权限变化可能需要从首页刷新。创建条件候选已采用“绑定creationContextRevision、变更409”的规则保持，不能被普通列表建议覆盖。

### 4.3 持久事件

Issue SSE 完全复用创建契约：失效通知＋REST补查、默认24小时保留、15秒注释心跳、每次发送／心跳本地核权；本节只补数据库顺序。

每个 Issue 使用单独事件流计数行，在产生业务变更或rooms观察的同一事务中锁该行、增加流序号并插入事件。所有同Issue事件写路径都锁同一计数行至提交，因此不存在“序号较大先提交后游标跨过较小未提交事件”的漏读。不能仅依赖全局自增序列最大值推断提交顺序。多个变化可合并同一失效事件，消费者允许重复。

发布器扫描已提交事件，任何一次网络写入不构成浏览器已读；不全局删除已被某连接发送的事件。重放游标绑定Issue与流代次，保留窗口过期／不能验证连续时发送resync_required。连接先注册再resync；查询期间通知继续使页面dirty。敏感对象失权时停止事件和内容，不用通知字段暗示隐藏房间存在。外部撤权未观察时的风险按B01说明，不能从15秒心跳推导外部撤权发现时限。

## 5. 模块用例和实现边界

| 应用用例 | 负责模块 | 事务边界和主要结果 |
| --- | --- | --- |
| SaveProject | 项目访问＋项目资料＋操作记录 | 完整范围、owner、配置引用、核查观察和回执同提交；App不足可受限保存；不写运行准备待办。 |
| UpdateProject | 项目访问＋配置 | CAS projectRevision；元数据／配置保留旧范围，明确添加集整体校验／提交，首批无删仓。 |
| CreateIssue | 认证适配层→共享创建用例 | 页面/MCP共享§2；MCP未完成不得伪装已接入。 |
| GetCreation | 操作记录＋当前内容读权 | 主库查原作用域；不重跑创建条件，不凭404判定最终失败。 |
| List / GetSnapshot | 查询层＋统一访问规则 | 只组织持久记录；不能写运行成功、触发准备或自动新建会话。 |
| ContinueCreatedIssue | 后台持久工作＋运行适配 | 首批可blocked；B06接通后沿原Issue准备／接手，禁止再CreateIssue。 |
| PublishInvalidation | 事件基础设施＋当前访问 | 提交后投递、重复可恢复；没有资源调度权。 |

这些是同Go工程的模块职责，不要求新建服务／通用工作流引擎。数据库连接和事务由应用用例提供，模块在同一事务中写各自记录；禁止各模块内部自动提交导致半套结果。敏感凭据访问只通过凭据边界，不通过通用查询层或队列payload。

## 6. 实施后的失败与恢复验收矩阵（尚未运行）

| 编号 | 故障注入／动作 | 观察依据和必须结果 | 不得宣称 |
| --- | --- | --- | --- |
| DB01 | 同键同输入20个并发提交 | 最终唯一操作、Issue、主CS和必要新会话各一套；其他响应200或先503后原查询200。 | 仅HTTP全部成功就证明无重复。 |
| DB02 | 同键不同输入并发 | 仅一个规范化输入与结果提交；其他409或未知后核查；正文不被覆盖。 | 先到Web等于先提交。 |
| DB03 | 在每个业务写入阶段中断事务 | 提交前没有半套业务和待办；确认提交后所有必要记录齐全。 | 进程退出等于数据库回滚。 |
| DB04 | 提交后丢弃HTTP回执，并让一次查询先于提交 | 保留原键/输入；允许暂时404；最终查询唯一回执。 | 404后可自动新键再建。 |
| DB05 | 配置CAS和Issue创建交错 | 锁后修订一致；冲突409；已提交重放不按新配置拒绝。 | 外部权限与数据库绝对同步。 |
| DB06 | 关联已有会话时并发扩大内容范围 | 卡片/范围同事务；当前完整范围核权，旧小范围不能读新正文。 | 只过滤关联Issue已保护消息。 |
| DB07 | 部分仓库失权后修复模型引用／追加一仓失败 | 配置修复保留原范围；明确添加失败整体不提交。 | 从可见列表替换项目范围。 |
| DB08 | 正文清理后原键任意输入重放 | 当前有权返回410，无输入比较、无重建；无权不泄露历史。 | 删除哈希/缓存等于可以复用键。 |
| DB09 | 待办提交后后台重启、丢失所有内存通知 | 扫描仍发现同workId；未接入时真实blocked。 | 待办已领取等于Manager接收。 |
| DB10 | 租约过期、旧处理者晚回写 | 代次CAS拒绝旧更新；可能外发者进入核查；不增加外部逻辑操作。 | 租约到期等于进程已停。 |
| DB11 | 两个同Issue事件事务交错，发布器重启 | 范围内流序号按提交可连续核查；无全局序列漏读；过期resync。 | 内存广播一次等于可恢复。 |
| DB12 | 401／失权、SSE重连、切页晚到 | 当前权限阻止读取；页面按代次清敏感缓存；原操作不改绑。 | 前端隐藏按钮完成鉴权。 |
| DB13 | 列表跨页新增／删改、游标篡改／换主体 | 当前权限每页核验，游标不串范围；无隐藏标题／总数泄露。 | 多页属于全局固定快照。 |
| DB14 | 并发读取与正文清理／内容范围扩展 | 本地快照/版本重验；清理提交后开始核验的查询无旧正文。清理前已获准传输的响应可能晚到，记录最后授权核验时点。 | 网络送达与删除事务原子，或已发到用户的历史可以追回。 |
| DB15 | 项目创建响应丢失、项目删除后重放 | 原账号作用域查询唯一结果；删除后保留账号期占位410。 | 没有projectId就无法恢复，或删项目即新键。 |
| DB16 | 配置默认／secret版本变化，同时更新名称和加仓 | 原固定版本不暗换；失效观察受限；仅显式配置提交重新绑定。 | PATCH改名可顺带切模型，或后台默认更新等于用户选择。 |
| DB17 | 项目更新成功后再次修改，再重放旧updateId | 返回旧提交revision／时间；当前快照另GET；无实际变更的操作也有稳定结果。 | 用当前revision冒充旧回执。 |

文档检查只验证引用、语义和表格一致性，不替代这些真实数据库、权限和并发实验。B03已有首批事务实现的主体设计；真实 Issue／待办首次落库前还须收口 §2.5 的配置绑定选择，并执行其候选专题 CB01—CB05。完整系统就绪仍依赖B01认证接入、页面字段基线及B05—B08各自批次，不能从本文推导整个后端设计完成。


## 7. B06 创建事务收敛候选（2026-09-13）

状态 `B06-PERSIST-r1 / PROPOSED_NOT_ADOPTED`。本节细化已采用 §1—4 的原子创建、重放和内容保护；P9 的新增外键、权限观察门槛和具体物理约束仍待采用。声明及约束设计见[本轮 B06](../development/2026-09-13-b04-b06-design-01/B06.md)，唯一 HTTP 字段仍在创建契约。本批没有 rejected 创建操作、独立受理占位、Manager 入口、运行消费者、详情或 SSE 实现。

### 7.1 用例与数据所有权

新增有限 `internal/issues` 包拥有页面创建、原操作查询、创建条件及已有会话候选；它调用 access 当前主体/仓库授权、projects 固定配置读口、modelbudget 门槛读写口，不反向让这些包依赖 issues。issues.Service 是一个创建事务的唯一持有者，handler 只解析边界和映射现行响应。`CreatePage` 与 `GetPageCreation` 共用原操作加载、完整内容核权和稳定回执投影。来源由该受信页面方法固定 `issue_page`，请求不能传 actor/entry；不为尚未确定的 MCP 预铺公共泛型 Source 或 Manager 方法。

Issue 新建身份、项目内展示编号、主 ChangeSet 身份和主会话身份分别生成。编号由已锁项目计数行分配并以 `(projectId, number)` 唯一，允许间隙，不用 MAX+1。会话可关联多个 Issue；每 Issue 此时恰一个主会话和主 ChangeSet。页面创建来源记录和会话卡片关联真实 creation operation，不伪造 Message 或投递。工作仓库非空且全部引用同项目 ProjectRepository；Issue 内容保护集合包含工作仓库，会话保护集合追加原集合与该 Issue 内容集合的并集，不能缩小。每对象内容范围拥有修订；卡片、范围与修订同事务写入。未来新增消息/材料必须沿相同对象锁扩展范围，不能只改变卡片。

本批创建一个 `issue_continue` DurableWork，唯一 `(causeOperationId, kind, targetId)`，状态为 `blocked/INTEGRATION_NOT_AVAILABLE`，重新评估条件为安装并采用相应消费者；externalOperationId、phase、leaseOwner 为空。它只引用 Issue 固定配置，不复制可变配置。创建一个 Issue `snapshot_invalidated` 事件，流序号1及计数行同事务。事件类型沿现行通知契约，保留期24小时；本批不发送、不实现心跳。RepositoryIssue 初始零条，不给未生效计划生成仓内任务。

### 7.2 权限观察与逐路径锁序边界

现有 B03 CheckProjectObservation 只验证用户参与观察和 connection 代次，使用 transaction_timestamp 与60秒窗口；这不足以证明 Issue App 能力和完整历史内容可读。本候选增加 Issue 专用 opaque 观察，仍由 access 产生：actor、credentialVersion/connectionRevision、accessEpoch、准确仓库集合、每仓用户观察时间、所选工作仓库的 App 安装身份/权限版本/观察时间。当前用户须可读提交范围及所关联会话完整内容范围；新创建的工作仓库须有已采用能力政策。推荐 S06 最低 Metadata read、Contents write、Pull requests write，作为待采用的新门槛；不从 B02 登录成功推导此能力。

推荐新创建所有必要观察在最终提交前以数据库 clock_timestamp 检查不超过60秒、不得来自未来；这是延续 B03 数值、补齐覆盖和提交时钟的候选，不代表远端权限原子性。观察过期或本地代次变化，回滚后重新获取，不能持锁访问 GitHub。外部明确不足按当前契约隐藏404或可披露的409；未知503 AUTHORIZATION_UNCONFIRMED。原结果读取只要求当前内容读取权，不要求重新通过 App 写权限、预算和创建条件。

所有owner交互写先锁principal的binding、session和account。持有project、operation、catalog、Provider、额度窗口或secret等后段锁时不得回取account。同owner写由account锁串行，因此各用例保留自己的后续顺序。B03项目写和B06创建继续使用principal→project→operation→catalog/profile，再按各自业务对象、policy/额度窗口和secret顺序取锁；已有conversation/issue按对象类型及ID排序。不存在的operation不靠SELECT FOR UPDATE得锁，由account/project串行及最终唯一约束保障。

B04模型保存独立采用principal→operation slot→catalog→Provider。B05测试采用principal→operation→catalog/profile→Provider→policy/额度窗口→secret；B05应用保留项目路径的principal→project→operation→catalog/profile→Provider。目录作者先按owner account ID排序锁全部owner，再取catalog，禁止catalog持有者回取owner/project/Provider。Provider.owner、profile.owner和来源执行版本owner必须由DDL保持不可变；用例中的owner检查不能替代该约束。来源导入先持import单例，原键未命中后才锁owner accounts和catalog；交互路径不取import单例。

本地秘密撤销以秘密可用性行串行；撤销先提交再异步传播项目观察，因此创建直接检查精确 secretVersion，不能等待 creationContextRevision 更新。模型/执行停用同理核确切版本状态。读取结果先拿最小身份和内容范围修订，不取敏感正文，在事务外核观察；短读事务锁主体/project/connection及目标范围，重比范围版本后投影。会话集合已扩大则重新观察全部集合，绝不凭旧小范围返回新正文。此处授权观察与快照复核也供候选会话查询使用。

### 7.3 新创建与原结果的事务步骤

1. 限制信封/JSON体并认证。主库按 `(projectId, actor, issue_page, creationId)` 预查。存在结果时先以存量 Issue及关联会话完整内容范围核当前读权；removed 有权410先于正文比较；完整结果用所存 schemaVersion 的比较器返回200或409。客户端新传的会话/仓库不得影响旧结果读权。语法上无法解析的请求仍400；不把新的业务有效性校验放在已提交重放之前。
2. 无结果才严格规范化新的允许字段，读取范围/会话版本与确切当前固定配置，事务外取得全范围观察。预核失败仍用短事务再查原操作，让期间提交的赢家优先；无赢家才返回新操作错误，不保存拒绝记录。
3. 开短 READ COMMITTED 事务，设置 lock_timeout=2s 与事务总预算5s（候选初值）。锁主体、项目和 observation 所需 connection，再查/锁原操作。赢家存在但范围与预核不同，释放事务后按赢家完整范围重核，不能用当前提交范围替代。
4. 新操作比较 expectedCreationContextRevision，锁已有会话并比较可关联性与范围修订，锁目录和固定版本/政策。沿当前不可变 ProjectConfigRevision 检查完整参数、确切秘密版本可用性、运行预算政策及当前窗口；必要新窗口只在此写事务中有可核本地无消费历史证明时初始化。额度不足/未知不通过。创建本身不消费或预留额度，不要求 runtime Ready、已生效计划或模型测试成功。
5. 同项目锁下选定唯一 initialConfigurationRevision；插入事务内 operation、必要会话、Issue、工作/内容范围、主 ChangeSet、页面来源和会话卡片、确切输入/回执、blocked 待办、事件，填齐所有关系。尾部再验证观察时限及主体本地时间资格。任何写入或延迟约束失败全回滚。
6. COMMIT 获确认才201；已知同键赢家200。提交回执丢失为503 RESULT_UNCONFIRMED，由浏览器原键查询；查询一次404不能证明不会迟到提交。确认回滚可复用原键输入重试，提交未知先查主库。局部异常不能变成“可能成功”的空201。

P9 的项目锁串行化见配置绑定专题。源版本变化不得改写旧修订；不可用时保存事实仍可读，新创建受限。B06 对操作只保存 committed 或 removed，两者和 B04/B05 的 durable rejected 是刻意不同的已采用入口语义。

### 7.4 清理与恢复责任

清理是 composition 才可获得的维护入口，本批无浏览器删除路由。先锁身份/项目/原操作/Issue/会话，再清除 Issue正文、确切输入、摘要、卡片/来源正文和相关派生副本；取消确定未外发的本批 blocked 待办，保留无正文目标、scope、配置引用与 removedAt。同事务转 tombstone，返回字段不留下可重建正文的副本。来源/关系的必要身份与只增内容权限集合仍保留，旧键永远不能新建。尚不设计物理项目删除或带未知外发动作的强制清理。

跨重启只靠主库原操作、Issue pin 与 blocked 工作重建责任。本批交付保存/查询用例与数据库约束；任何消费者、真实投递及页面详情仍由后续批次另行采用并验证。


### 7.5 复核后的观察来源与输出接口

S06观察由现有固定GitHub适配器新增ObserveAppInstallation保留当前AppCapability已取得但丢弃的installation ID、App ID、suspended及完整permissions。permissionRevision是版本化长度编码的这些非秘密值（permissions按key排序）的SHA-256本地观察指纹，不是GitHub服务器提供的CAS或持续授权。当前AppCapability继续投影原Capability，不改变B02公开语义。access.Provider新增方法的真适配和替身均在U06.2实现，不能只在Issue类型写一个无法取得的字段。

access.OpenRuntime将已导入且PrivateKey回调确切使用的privateRef记录为Issue观察App凭据版本；用户credentialVersion绑定实际access_ref，与connectionRevision/accessEpoch分开。最终事务先核固定模型秘密，再按版本ID顺序核该观察的用户/App秘密当前可用性及权威owner/purpose，最后做时限/主体到期核验。持认证秘密锁的其他路径不得反拿模型秘密锁。外部观察与本地提交之间仍有未观察远端变更的窗口，不能用该指纹声称消除了它。

创建/查询共用issues.CreationReceipt和MarshalJSON；GetPageCreation直接返回同一种receipt，Web统一投影，首次确认提交201、重放/查询200。Location由receipt的固定Issue路径取得。内部pin及确切正文不被编码进创建回执。

本批需主体/范围行锁的原操作读取采用短READ COMMITTED事务复核；只读是业务无写副作用，不将SELECT FOR UPDATE放进PostgreSQL READ ONLY事务。§4.1的通用详情READ ONLY REPEATABLE READ建议不直接套在此路径，B07详情策略仍另行设计。本批不引入原操作查询时的运行准备或新额度窗口。


### 7.6 复核后的归属与派生正文约束

同项目P9外键仍不足以证明配置引用属于该project owner。沿当前owner-only规则，新增延迟configuration_owner_and_binding_consistent检查：两项固定profile owner必须等于project.owner；fixed.secret四元组和物化值必须匹配所引用不可变版本。现有0004的两个immutable trigger保留。旧B03合法受限记录不重写；发现旧错属记录则迁移拒绝，另行明确修复，不能让新Issue通过同项目配置间接读取他人秘密。

必要新会话保存created_by_operation_id，且其复制Issue标题时保存title_origin_operation_id；关联已有会话不改这两个来源。清理Issue派生正文只清仍带该title_origin的标题并置redacted标志，保留关联身份和内容scope。会话以后独立改名须清除标题派生标记，不能因为会话最初由本Issue创建就删除后来独立标题。候选投影使用固定无正文清理标签，既有会话原名不被清理。DB08分别覆盖new、existing和已独立改名的分支。


B06清理的持久待办只允许 `blocked/INTEGRATION_NOT_AVAILABLE → cancelled/CONTENT_REMOVED`，同事务写cancelled_at，保留cause/Issue与其pin可追溯关系；不删除责任身份，不允许回到blocked。它只处理本批尚无外发记录的工作；发现external事实则拒绝本清理入口，留后续协议处理。
