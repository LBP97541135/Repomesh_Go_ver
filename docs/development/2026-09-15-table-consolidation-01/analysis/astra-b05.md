# B05 本轮确定方案：16 → 9 张

2026-09-15，Astra 只读分析交付。供 DeepSeek 修改当前文档。没有修改产品、迁移、历史证据，也没有运行数据库或外部模型。适用范围是 B05 的后续存储方案，六个候选浏览器端点及 C05/C06 的字段、授权、幂等和恢复语义不因合表改变。候选数值仍未采用。

## 1. 确定表清单与边界

本轮冻结以下九个物理名，不再写“实施时任选 schema”。原有36张表的字段、约束、身份和API不变。引用现有表、按原职责插入新配置/执行版本是复用；不向旧表增列、不放入新种类的操作或政策正文。

1. `repomesh_modelbudget.windows`
2. `repomesh_models.previews`
3. `repomesh_models.tests`
4. `repomesh_models.test_observations`
5. `repomesh_models.test_handler_instances`
6. `repomesh_models.application_operations`
7. `repomesh_sources.policy_versions`
8. `repomesh_sources.policy_bindings`
9. `repomesh_sources.policy_imports`

九张而不是八张有源码依据：`0005_models.sql` 中 `repomesh_sources.imports.schema_version` 有 `CHECK (schema_version = 1)`，现有 `execution_versions` 和 `profile_versions` 已不可变。旧稿要求给这两处增加政策引用、让旧 imports 接受 schema2；本轮改为新 `policy_bindings` 保存 execution 政策补充，新 `policy_imports` 保存 schema2 原导入。不可为了达到八张而给旧表塞新职责。Handler 实例拥有独立于测试的启动、续期、退役和不可复用身份，保持独立表。

## 2. 完整旧表映射

| 旧16张提案 | 新物理表 | 保留方式 |
| --- | --- | --- |
| modelbudget.windows | repomesh_modelbudget.windows | 独立共享请求窗口，保留互斥scope FK、UTC日复合PK、计数及只降上限 |
| models.previews | repomesh_models.previews | test/apply两种有界预览，共用过期及单次消费生命周期 |
| models.tests | repomesh_models.tests | 测试聚合根及唯一可扫描待办 |
| modelbudget.test_reservations | tests | window复合FK、amount=1、budget_status列；没有第二条预留行 |
| models.test_dispatch | tests | 领取列、一次send permit、外部操作、sender及本地能力绑定列 |
| models.test_observations | test_observations | 保留1:N不可变证据，独立于当前采纳状态 |
| models.test_handler_leases | test_handler_instances | 改名说明实例登记是历史身份；租约只是其中的观察字段 |
| models.actor_outstanding_tests | tests | is_outstanding生成列及actor条件索引；当前未核计数由account锁内查询，不维护第二个计数权威 |
| models.unknown_test_closures | tests | 0:1完整关闭列组、全局close键唯一索引、不可变关闭事实 |
| models.application_operations | application_operations | 保留独立操作及确定拒绝：一个预览可能被不同application键尝试并产生拒绝，不能把二者误当1:1 |
| budget_policies | policy_versions | policy_kind=budget |
| time_limit_policies | policy_versions | policy_kind=time |
| egress_policies | policy_versions | policy_kind=egress |
| test_bindings | policy_bindings | binding_kind=test，owner唯一当前绑定 |
| request_policy_versions | policy_versions | 取消project侧重复权威，直接引用同一budget行 |
| time_policy_versions | policy_versions | 取消project侧重复权威，直接引用同一time行 |

旧稿的 `execution_versions` v2扩列（不在16张里）改映射到 `policy_bindings` 的 execution 分支。旧 imports 接纳schema2的设想改为第九张 `policy_imports`。九张总数包含这两项所需存储，没有隐藏额外“逻辑表”。B05 的唯一测试工作就是 tests 内的领取/待办字段，不另建 DurableWork 实体表。

## 3. 共同DDL规则

全部新文本身份按原契约有限长度/UUID形状校验。所有列组用明确列、FK、CHECK和写入函数验证；禁止无约束JSON承载关系、预算、许可、关闭证据或政策。规范化输入bytea只做精确重放比较，不能替代结构列及FK。需要保存HTTP回执时允许有schema标签的有限白名单JSON，校验器拒绝未知字段、非法分支及超限正文；事实仍来自结构列。

新表身份、owner、固定快照、原窗口、接受事实、政策版本、已发许可及关闭事实均由新表触发器防改。仅开放各专用用例函数，普通Web/Runner角色没有直接任意UPDATE/DELETE权限。跨表最终一致性用新表上的延迟constraint trigger按PK回读终态；跨事务额度、权限和计数由下文锁序串行。不能给旧36张增加触发器或改原约束以达标。

需要物理FK时直接指向已有唯一键，不假设旧表存在未声明的复合唯一。例如模型快照FK指 `model_snapshots(provider_id,provider_revision,row_id)`；actor等于Provider不可变owner由新表触发器查询既有provider关系验证，不在旧表补索引。账号共同锁沿已收口设计使用现有account行，不在account新增计数、指针或操作字段。

## 4. 九表字段与约束

### 4.1 windows

保留原列：scope_kind、actor_scope_id FK accounts(id)、project_scope_id FK projects(id)、scope_id生成列、start_utc、end_utc、limit、reserved、consumed、revision。PK(scope_kind,scope_id,start_utc)。CHECK显式规定两种scope分别恰有一个非空FK，scope_id生成自对应外键，start_utc为UTC日零点，end_utc恰为24小时后，limit为1..2147483647，reserved/consumed非负。

窗口更新触发器禁止身份/起止变化及提高同窗limit。**不加 reserved+consumed<=limit 的CHECK**：同日下调limit可能低于已消费金额；此时剩余投影为max(0,limit-reserved-consumed)，新登记拒绝，历史计数不被篡改。测试固定原窗口，跨日照原窗口结算。policy版本不在窗口PK。

### 4.2 previews

PK id；kind CHECK test/apply；actor FK accounts；provider_id/provider_revision/model_row_id复合FK snapshot；secret_version_id指既有版本，并验证它等于该provider revision的secret；created_at/expires_at；removed_at。UNIQUE(actor,id,kind)。固定输入、类型、actor、期限不可更新。

测试分支：budget/time/egress各有kind常量及id/version复合FK policy_versions，budget_scope常量actor_model_test；test_binding_revision、max_output_tokens、timeout_seconds明确列。输出限制校验为正且不大于固定模型上限；具体候选上限16不在此次采用。apply专有列全NULL。

应用分支：project_id FK projects，expected_project_revision、before_configuration_revision FK configuration_revisions的实际已有PK，完整before以明确结构列记录：原model和execution各自selection、profile_id/version、default_revision；execution政策id/version来自新policy_bindings；原模型存在性/内部可读材料用显式判别；before完整配置可用已验证且只含现行FixedConfiguration字段的规范化副本辅助逐项比较。公开受限before不反向删掉内部比较。test专有列全NULL。项目owner/配置同项目通过新表约束函数核对。

消费使用 consumed_test_id、consumed_application_id、consumed_at 三列；CHECK按kind最多一个正确分支，时间与消费键同空或同非空；两个消费ID分别有实际FK到tests(actor,test_id)及application_operations(project_id,actor,application_id)，所需复合唯一键建在新表。只有NULL→固定绑定，禁止释放再用。被消费的操作须反向引用本预览，延迟约束检查。过期清理清展示/比较正文并置removed_at，保留身份、原输入比较摘要/必要固定字段和消费墓碑；不硬删后让ID复活。

### 4.3 tests

PK(actor,test_id)，actor FK account；preview_id、preview_kind='test'复合FK previews(actor,id,kind)，UNIQUE(preview_id)；canonical_schema=1、canonical_input（previewId与confirmPotentialCharge的精确规范化字节，有限长度）、confirm_potential_charge CHECK true、accepted_at、accepted_principal固定列。

固定模型snapshot复合FK及secret绑定规则同previews；预算/时限/出站三个确切policy FK；test_binding_revision只记录接受时组合身份，**不FK至可变当前binding revision**。三个政策版本才是历史参数权威。result_revision用服务端bigint>0单调增长，对外仍生成原不透明字符串。

预留列：window_scope_kind生成actor_model_test，window_scope_id生成actor，window_start_utc；复合FK windows。budget_amount CHECK=1；budget_status CHECK reserved/consumed/released。无许可登记为reserved。许可生成时只能reserved→consumed。证明许可生成前未发可reserved→released；consumed永不released。

领取/工作列：work_state CHECK ready/claimed/reconciling/done，next_check_at、claim_generation>=0、claim_owner_instance_id FK handler_instances、claim_lease_until。claimed要求owner/generation/lease齐全；claim代次只能增长。没有permit的queued可被新代次领取；有permit只进核查工作，永不再领发送权。tests条件索引支撑ready与reconciling扫描，不增加工作表。

一次发送列：external_operation_id UNIQUE、send_permit_id UNIQUE、may_have_sent_at、sender_instance_id FK handler_instances、credential_capability_id、credential_capability_version。CHECK六列全空或全部非空，permit出现则budget_status=consumed；一经出现全部身份不可改。UNIQUE(actor,test_id,external_operation_id)供观察复合FK。nonce不持久化可恢复发权；数据库只存不可恢复的permit身份，内存一次许可须依明确COMMIT回执生成。领取owner可变化，但真实sender永远是发许可时的实例。

本地撤销列：capability_revoked_at、revocation_evidence_ref、revocation_evidence_digest、revocation_verified_at，成组同空/非空、摘要为sha256格式、只空→固定；不得把本地撤销等同供应商远端撤销。

当前结果列：state六值、recovery三值、observed_at、result_code白名单、result_summary有长度上限、latency_ms>=0或NULL、usage_input_tokens/usage_output_tokens非负且同空/同时存在、charge_status三值、removed_at。currency与amount无需存为任意数值，API恒null。明确CHECK状态矩阵：queued/running→open；passed/failed/rejected→not_needed；unknown→open或closed_without_result；running/passed/failed/unknown须存在permit；passed/failed/unknown预算consumed；rejected无permit时预算released。queued可以已生成permit但尚未有开始证据，此时预算consumed，不能把may_have_sent伪作running。

is_outstanding存储生成列：state IN(queued,running) OR (state=unknown AND recovery=open)。索引(actor,test_id) WHERE is_outstanding；用账号锁保护COUNT及新登记。不会另写outstanding布尔权威，不会因removed_at或UTC跨日解除。maxUnresolved从明确政策参数取值；本轮不以UNIQUE(actor)擅自采用候选1。现行候选规则仍只能每actor1笔；未来改数值须仍修订Preview/Submit公开形状，当前存储不阻止该修订。

关闭列组：close_deployment_identity FK deployment(deployment_id)、close_key、close_canonical_schema CHECK=1、close_canonical_input、close_operator_principal、close_operator_session_user、closed_at、sender_exit_evidence_ref、sender_exit_evidence_digest、sender_exit_verified_at，以及前述本地撤销材料。close输入包括actor/test、external/permit/sender/capability身份与两类材料，逐项与同一行固定发送列比较。不再重复保存可互相漂移的绑定副本。UNIQUE(close_deployment_identity,close_key)的部分唯一索引 WHERE close_key IS NOT NULL；全部关闭必需列同空或全有；closed_at非空当且仅当state=unknown、recovery=closed_without_result，且budget=consumed和撤销列组完整。所有关闭列从NULL一次写齐后不可改，天然一个test最多一次关闭。

关闭原回执由这些不可变列投影；需要保留wire版本则只存有限schema+白名单回执。accepted、permit、closure三组事实本身包含白名单审计主体/时间/绑定；不额外新增“审计表”。展示正文清理不能删这些事实或canonical close比较材料。

旧稿一处说“已证明未发且旧能力失效才释放”，另一处硬性要求“consumed不可released”。本轮按后者冻结账本：存在permit的请求不退款。通常确定未发拒绝发生在permit之前；若将来采纳permit后确定未发的rejected分支，仍consumed，不能把文案解释为允许已消费倒流。当前unknown关闭协议不需要该新增分支。

### 4.4 test_observations

PK evidence_id；actor/test_id/external_operation_id复合FK tests；reporter_instance_id FK handler_instances（内部核查可用专用报告主体分支）；reported_generation、kind枚举 started/result/transport_uncertain/conflict；started_at/completed_at、protocol_result枚举、result_code白名单、latency_ms、usage两列、evidence_digest及observed_at。按kind作非空CHECK、时间/计数范围CHECK和摘要格式CHECK。不存原响应、Key或任意命令；同evidence_id同内容重放，异内容冲突；append-only。

正式采纳使用tests.result_revision及claim_generation的CAS。证据绑定到原external_operation，不因claim换代变成新调用；采纳者与报告者不是同一权限。关闭后仍允许追加，但不得更改tests正式状态/关闭/预算；removed后证据不得重建展示正文。冲突材料保留且正式状态保持unknown，不能最后写入者覆盖。

### 4.5 test_handler_instances

PK instance_id，随机不可复用；protocol_version、registered_at、last_seen、lease_until、retired_at。CHECK lease_until>=last_seen，last_seen>=registered_at；退役只空→时间；身份/协议版本不可变，协议改变须新实例ID。协议版本在受控transport映射中对应可支持的输出限制，不存任意可执行handler定义。

coordinator启动/续期独立短事务，只改实例行，不锁account；退役或过期不能证明能力已撤销。FK ON DELETE RESTRICT，已被tests/observations引用的实例永留最小墓碑；未引用的失效登记也保留id/注册事实，防同ID复用。所谓有限租约登记是有限活跃集合，非“按TTL删除所有历史身份”。

### 4.6 application_operations

PK(project_id,actor,application_id)，项目/账号FK；canonical_schema、canonical_input（requested_preview_id）；requested_preview_id保存原输入，不强加FK，因为确定拒绝可针对无效预览；accepted_preview_id可空并FK正确actor/project/apply预览。outcome CHECK committed/rejected；result_code；project_revision、configuration_revision、provider_revision、model_profile_id、retained_execution_version_id、committed_at/decided_at、accepted_principal、removed_at及有限wire回执。

committed要求完整结果及accepted_preview_id，结果配置归本项目、保留execution等于预览完整before；rejected要求明确错误且无配置写入结果。后者允许输入预览不存在、过期或已消费，不能要求每条拒绝都有消费FK。no-op仍为committed回执，引用原配置修订且消费预览。结果身份不可改；清理只清正文，保留原键/输入摘要/结果归属及removed墓碑，旧键不能新做操作。

### 4.7 policy_versions：一份版本权威

PK(policy_kind,id,version)，policy_kind CHECK budget/time/egress。每种政策id/version仍有自己的命名空间，不能把相同id跨种类强当同一对象。created_by_import_id与deployment_id复合FK policy_imports；created_at、canonical_digest；全表immutable。

budget列：scope_kind两值、unit='request'、period='utc_day'、request_limit 1..2147483647、max_unresolved正整数、enabled布尔，time/egress专有列全空。

time列：model_request_timeout_seconds 5..120、worker_attempt_limit_seconds 60..86400，budget/egress专有列全空。

egress列：approved_base_urls text[]一维非空、有界输入体总限制、无null/重项，allowed_port=443、allow_private_addresses=false、follow_redirects=false，其他专有列全空。不可变校验函数验证每个规范化HTTPS地址与当前来源§6的禁止地址形状，不发DNS/HTTP；运行准入仍做发送时解析/地址检查。

为预算scope建立UNIQUE(policy_kind,id,version,scope_kind)，绑定/测试用常量budget kind+明确scope复合FK。time/egress用kind常量+id/version FK。全部互斥列CHECK须显式IS NOT NULL/IS NULL，避免SQL CHECK遇NULL无声通过。

projects只从这张表读固定预算与时限，不能新建projects.request_policy_versions/time_policy_versions或在profile里复制政策正文。接口层的RequestPolicy/TimePolicy类型仍可分别存在，不要求表一类对应一个Go类型。

### 4.8 policy_bindings：两个明确且可约束的来源引用分支

列binding_kind CHECK test/execution；owner FK account；execution_profile_id、execution_version；binding_subject_id生成列（test取owner，execution取profile id）；binding_subject_version生成列（test取固定内部标记'current'，execution取execution version）；PK(binding_kind,binding_subject_id,binding_subject_version)。另有revision、deployment_id/last_import_id FK policy_imports、budget_kind生成budget及预算id/version/scope，time_kind生成time及id/version，egress_kind常量及可空egress id/version。

test分支CHECK：execution两列NULL、budget_scope=actor_model_test、egress引用齐全，owner不可改。PK保证owner只一条当前绑定。改绑定只改三个政策引用、revision及last_import，旧测试/预览已持确切版本所以不漂移。相同内容重列不产生新revision，实际组合变化才产生新修订。

execution分支CHECK：execution profile/version齐全、budget_scope=project_model_runtime、egress各列NULL；FK(execution_profile_id,execution_version)→既有execution_versions(profile_id,version)；新表延迟检查owner等于来源执行owner、profile_versions原budget_policy_id/time_limit_policy_id与这里id一致、其parameters_complete与固定原参数一致。本分支immutable，存在即表示v2政策补充完整，不另存能漂移的complete标志。

v2导入新版本时，同事务用既有字段创建原execution_versions及profile_versions，再写本分支。若原execution版本早已存在而无本补充，必须拒绝v1补全，要求新version。仅开放受控v2登记函数，以“本次成功新插入原execution版本”作为可插入补充分支的条件；相同v2已存在则必须完整内容一致才复用。新表触发器验证关系，禁直接DML，不能依赖xmin等实现细节判定“是不是同事务新建”。这只在原表原职责下创建新执行版本，不新增旧列也不变更历史行。

### 4.9 policy_imports：schema2导入原操作

PK(deployment_id,import_id)，deployment FK旧deployment表；schema_version CHECK=2；canonical_schema、canonical_input bytea（规范化schema2全清单，沿现行1MiB限制）、receipt jsonb（严格固定schema2回执）、committed_at、operator_session_user。全行immutable，无pending/rejected持久记录。

回执保持schema1原字段加budgetPolicies/timeLimitPolicies/egressPolicies/testBindings四数组，即使空也保留，没有policies聚合wire键。policy_imports只收schema2；旧imports继续只收schema1，原schema1 canonical和receipt逐字重放。

两个物理表共同具有一个逻辑操作域(deployment_id,import_id)，不能让同key跨schema成功两次。所有受控来源导入入口首先获取**既有import_serialization单例**，再查询两表；任何一表命中先按其原schema比较/重放，同key跨schema确定冲突。新表约束函数也核无旧imports同键；反方向由共同入口覆盖，不给旧imports加触发器。新导入入口必须统一进入此锁内分派，不能保留绕过互斥/跨表检查的第二条CLI写路径。schema1命中仍由原schema1读取/解码路径返回原表原回执；schema2命中由新读取路径返回新表原回执。

`sources result --import-id`不要求用户猜schema。按部署身份在同一只读快照查询两表，唯一命中就原样返回；两表同时出现是完整性故障而非任选一个；均无记录仍不能证明前一事务不会提交。跨schema冲突回读须新语句/新事务。新schema2写入角色没有绕开此入口的原表任意写权限。产品实现时修改共同入口的内部路由，但不改schema1命令参数、JSON字段、原回执或原表DDL。

## 5. 谁写、谁清理

| 表 | 权威写方 | 清理/保留 |
| --- | --- | --- |
| windows | 预算事务函数；测试登记/许可/释放；项目配置/创建门槛的初始化用例 | 只读GET不创建；历史消费/预留责任存在时不删 |
| previews | Web预览用例；提交函数唯一消费 | account前缀维护事务置removed，保留消费墓碑 |
| tests | Web登记；coordinator领取/许可/核查；专用unknown维护关闭 | 仅维护展示正文，身份/许可/预算/关闭/未核责任保留 |
| observations | transport受限报告及核查者append；采纳权另属核查函数 | 本轮不授权硬删；不能因测试30天正文期限丢关闭/冲突证据 |
| handler_instances | coordinator实例启动、续期、退役 | 仅缩减展示元数据，永久保留不可复用身份及被引用事实 |
| application_operations | Web专用应用事务 | account→project→operation，正文removed，结果归属保留 |
| policy_versions | schema2来源导入 | immutable；引用版本不删，无后台“覆盖最新版” |
| policy_bindings | schema2来源导入 | test当前组合可更新；execution分支不可变；不改历史版本 |
| policy_imports | 受控部署schema2导入 | 原输入比较/原回执长期保留 |

## 6. 精确锁序与事务归属

所有同类对象按稳定ID排序。所有owner业务写必须先锁owner account，持后段锁不得回取account。绑定/session只在当前浏览器授权入口取；后台内部收证和关闭不伪造用户session。网络、DNS外部核查、证据文件读取/验真都在事务外完成，事务内重核身份及本地约束；不持业务锁跨网络。

1. **新测试预览**：binding→session→account→catalog/profile→Provider→test policy_binding；读取对应immutable policies、窗口及该actor outstanding、handler有效登记（均不额外锁handler）→secret availability→INSERT preview。handler观察失败整笔不创建preview。readonly观察不是授权。
2. **测试登记**：binding→session→account→tests操作槽→catalog/profile→Provider→preview→test policy_binding/policies→原UTC窗口→在account锁下读tests outstanding→secret availability。操作槽由account串行“无行”检查，最终PK兜底，不持假行到响应外。命中原test先精确重放，不再取新资格的后段锁。成功同事务创建tests/预留、窗口reserved+1、preview消费及接受审计；tests即唯一工作。
3. **Runner领取**：先非锁扫描候选actor/test定位；逐个短事务 account→tests，CAS claim generation/lease，仅无permit queued可获得发送型claim；不存在持tests再取account的FOR UPDATE SKIP LOCKED反序。claim只是领取，不是发权。handler登记不与该事务互等。
4. **发送许可**：account→tests→catalog/profile→Provider→test policy_binding/policies→原BudgetWindow→secret availability。核当前actor启用、旧claim代次、固定secret/出站策略及本地能力；不要求当前Provider head仍为旧head。一次写齐permit/外部操作/sender/能力、窗口reserved-1/consumed+1、tests consumed。确定COMMIT后才交内存一次许可给transport。政策变化需要拒绝时只能在许可前释放，固定历史不重绑新政策。
5. **未发拒绝/释放**：account→tests→原BudgetWindow，核无permit且当前代次；窗口reserved-1，tests released/rejected/not_needed/work done同事务。已permit禁止退款。
6. **只追加观察**：account→tests→INSERT observation；不会读取handler FOR UPDATE。失权actor仍可以由内部收证函数维护责任。**采纳**在相同account→tests前缀内检查claim generation/result_revision CAS；无需改变窗口，预算已consumed。有冲突保持unknown；closed/removed限制分别生效。
7. **unknown关闭**：先验实际DB session_user、部署身份及维护角色；按(close_deployment,close_key)无锁查原回执优先。新操作 account→tests→必要的本地能力撤销核验，不锁或更新handler租约；核两类证据全部绑定，原子写撤销、关闭、recovery、审计（is_outstanding自动false）。预算不变无需锁窗口。若全局close唯一索引冲突且目标actor不同，整笔回滚后新事务查原键比较，禁止持本actor锁再追锁另一个actor。不同key命中已关闭test返回既有关闭回执，不写第二事实。COMMIT未知回原key恢复。
8. **应用预览/提交**：binding→session→account→project→application操作槽（仅提交）→catalog/profile→Provider→preview（提交）→secret availability；读取execution补充及immutable policies不修改。先重放，完整比较before，projects原用例仅替模型。no-op仍回执。新配置/项目窗口初始化按其既有project前缀，窗口在secret前按统一顺序；本次应用保留execution，不重设其预算窗口。
9. **项目额度初始化**：原项目配置路径 binding→session→account→project→operation→catalog/profile→政策引用→本项目UTC窗口→secret availability。只读C05从同一项目配置快照读新补充和policy_versions，额度查询savepoint失败回滚后投影unknown；连接/主体失败仍503。GET不创建窗口。
10. **来源导入schema1/schema2共同前缀**：旧import_serialization单例→双表原键查询→新输入所涉owner accounts按ID→catalog→profile/版本按ID→policy_versions按(kind,id,version)→policy_bindings按PK→schema相应原回执（延迟FK允许回执同事务后插入）。schema1原数据路径照旧；两个schema绝不分别用两个互斥锁。导入不取binding/session/project/Provider；交互路径不取import单例。没有政策查询晚回取account。
11. **清理**：测试/预览 account→目标行；应用 account→project→application行；与采纳使用相同前缀，removed只清正文。handler续期/退役独立实例锁，永不在持该锁时启动业务事务。

## 7. 重放、未知与失败验收（未来执行，本轮未运行）

- 同test键同输入：即使preview过期、handler退役、政策变化也先返回原操作；异preview等异输入409；不会第二次预留。
- 同actor多键并发：account串行COUNT+INSERT，不依赖页面禁按钮；候选上限1只接受一笔；跨日、removed仍阻断open unknown。
- 同preview不同键：唯一消费与反向FK最终检查，最多一笔接受；应用确定拒绝可以持久但不得制造第二消费。
- 许可事务失败：回滚预算和permit，尚无发送；之后可再领取。COMMIT回执丢失：无论查到permit与否都不能据此发；查到保守unknown，查不到按事务恢复，不自动换键。
- 已确认permit后发送前崩溃：最多零次实际请求、预算consumed、unknown；数据库记录不产生可恢复发权。发送后丢响应同理，最多一次本地transport调用。
- handler过期：阻止新预览/提交（无其他合格handler时），不撤销旧sender、不清unknown、不再授permit。
- unknown关闭缺任何一类证据或错绑：无关闭事实、预算consumed、outstanding保留；全部核对成功才写同一tests行完整关闭组。
- 同closeKey并发不同actor/test：唯一索引冲突→回滚→原键比较；异输入冲突，无跨actor倒序加锁。不同closeKey关闭同test最多一个事实。
- 关闭COMMIT未知：原closeKey+原canonical恢复。关闭后迟到证据仅追加同external_operation，不改变unknown/closed/consumed，不重建已removed正文。
- 同窗口换政策或下调limit：命中原PK，历史消费不清零，不违反伪造的“余额必须非负”CHECK；新登记可用额度为0。
- schema1同键原输入：原表原字节回执；schema2同键原输入：新表原字节回执；跨schema同键无论顺序都冲突。同import单例防同时提交双表；结果查询不改旧wire。
- v1 execution相同id/version请求补政策：拒绝；v2新version同事务生成原执行字段及完整政策补充；失败不留半个版本。projects读不到补充就unresolved，不填默认或零。
- 同policy(kind,id,version)同内容可复用，异内容整笔拒绝；项目和测试引用同一版本权威，没有同步两份政策的故障窗口。

## 8. 作者必须同步的位置

1. `docs/api-database/b05.md`：数据表节全面替换为本九张；保留六条HTTP；替换关系链、事务步骤及16张统计；明确schema2新增表来自旧表零改动约束；不要继续写schema名未冻结。
2. `docs/api-database/foundations.md`、`README.md`：共同合表规则、总数、原有36不变、事实/待办/操作不必各一表。汇总本轮9张，不能仍按旧16计。
3. `docs/current/backend-model-operations-draft.md` §4.2、§4.3、§7.2、§8.1/8.3/8.4：逻辑Test/Reservation/Dispatch/Closure保留，物理归tests；actor未核计数改生成谓词+account锁，Handler改新物理名；列明逐路径锁序及同close键跨actor冲突回滚；测试待办不另建表。旧“consumed不可释放”与宽泛“证明未发可释放”的措辞按本方案澄清。
4. `docs/current/backend-first-batch-sources-draft.md` §9：policy_versions唯一事实、policy_bindings两分支、policy_imports、双schema同键及共用旧import单例、结果路由；撤销扩旧execution/旧imports的存储方案。§8保留schema1已采用语义，新增链接指向§9的schema2跨版本操作域兼容说明即可，不改旧schema1字段。
5. `docs/current/first-batch-browser-api-contract.md` C05与 `backend-first-batch-persistence.md` §2.5：政策历史解析来源统一改新policy_versions与execution分支补充；HTTP字段/预算观察三分支不改。
6. `docs/current/model-settings-browser-api-draft.md` §4/后续补充：如点名旧物理outstanding/dispatch表，改逻辑记录或新名；状态/错误/候选数值不改。`issue-configuration-binding-design.md` 若引用request/time政策表或旧执行扩列也同步改读取路径，不因合表变P9采用状态。
7. 当前 `HANDOFF.md`/现行索引：增加本轮存储替代设计入口；明确原设计五项收口语义保留、物理组织被替代，B05未实现/未验收不变。
8. `docs/development/2026-09-13-b04-b06-design-01/migration-design.md`、声明附件和09-14收口报告是历史证据，**不改写**；在新的本轮记录逐项标明B05旧表提案/扩列被本九表方案替代，并从当前文档链接新记录，防历史附件继续被误读为最新DDL。

## 9. 阅读证据与限制

实际核对：`docs/api-database/b05.md` 全部数据表及HTTP/内部边界；`backend-model-operations-draft.md` §4/5/7/8；`backend-first-batch-sources-draft.md` §8/9；`model-settings-browser-api-draft.md` §4相关状态/费用；09-14五项设计收口；09-13 migration-design B05；`internal/database/migrations/0004_projects.sql` 与 `0005_models.sql`相关字段和不可变约束；根README、当前交接/索引及全局指南相关入口。本文件是存储设计分析，不声称DDL已经运行。后续实现必须做真实PG并发及失败注入验收，不能以本轮链接/文档检查替代。
