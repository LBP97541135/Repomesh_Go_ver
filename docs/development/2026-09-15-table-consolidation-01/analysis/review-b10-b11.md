# B10/B11 十一表方案独立交叉复核

2026-09-15，Astra。非主要设计者，只读复核`astra-b10-b11.md`、旧b10/b11和现行Graph/ChangeSet/ADR 0017。没有改仓库或运行SQL、上游、容器。十一表方向可以保留，以下问题在现有十一表中补列、子型、约束和事务规则即可修复，不改旧36表。

## R10-01 / P1：验证记录缺少真实承载和必需绑定

原位置：§8 execution_records 114—131行子型清单没有verification；§11 168行说非代码检查直接绑定产物；事务203行仅声称“验证记录绑定组合及实际上下文”，没有对应列/键。artifact仅有固定产物、controlled_ref、ChangeSet等，observation只有运行状态，二者都未绑定实际验证组合/环境数据依据。

证据：已采用changeset-design.md CS3/CS4明确固定组合、新验收/环境/数据上下文需新验证记录；CS6/CS9把验证结论与执行完成分开。changeset-structure.md“主记录与明细记录/版本怎样区分”列出Combination、验证Attempt、计划/验收/环境/数据；Graph §4结果语义要求当前固定组合的必检有效证据，不接受completed代替。

最小修复：在execution_records明确增加verification子型，或给artifact_kind=verification_report写同等完整字段/CHECK；不要增加物理表。至少有`combination_id`、固定combination判别列、`verification_attempt_id`或沿用attempt_id、`validation_plan_revision_id`、`evaluated_plan_set_revision`、`acceptance_basis_revision text`、实际环境/数据的封闭版本化引用及摘要、`verification_outcome`、证据正文位置/可用性来源。代码验证必须同Issue/ChangeSet的Combination FK；实际Attempt、round/Issue/repo按现有复合键约束；验证plan绑定本次round/Issue。环境/mock数据可用封闭版本化快照，不能泛化为任意身份payload。Skill工程仍暂缓，不造必须指向不存在Skill表的FK。

非代码验证用明确互斥分支指向该Candidate/产物固定版本，不能为通过组合非空CHECK伪造SHA。业务复核/证据适用性判断要么追加明确verification_review子型并指向原验证记录，要么已有受控判断记录补具体字段；不能覆写原验证结果。Round pass和跨仓verification_evidence只接受这些已定位且仍适用的事实，不能引用任意运行observation。

## R10-02 / P1：跨轮复用组合缺少本次验证上下文及覆盖校验

原位置：§8 combination_selection只列组合、选择版本/purpose/basis_ref（127行）；§11 validation_plan_revision_id固定为origin_round初次计划（161行），183—185行允许跨轮复用但仅重新核初审与可用性。

证据：CS4明确代码版本不变时允许新验证上下文复用组合；CS3同时要求本次范围、前提和有效计划。Graph要求whole-plan CAS。只沿origin计划可把覆盖A/B的组合用于要求A/B/C的新轮，或无法保留新验收的真实来源。

最小修复：combination_selection增加`validation_plan_revision_id`、`evaluated_plan_set_revision`，三列FK(validation_plan_revision_id,round_id,issue_id)到plans实际普通唯一键。字段不要复用公共plan_revision_id而保留“selection必须plan空”的互相矛盾CHECK；可以专用selection_validation_plan_revision_id。本次选择和启动验证锁当前round，核本次完整current计划集合，要求selected验证计划为当前适用版本，并重新比较组合成员仓库精确集合等于该计划validation_repository_ids。不同仓库集合或SHA必须新组合；同集合/版本而验收或环境变化只新增selection/verification上下文，不改旧组合。candidate_review适用性按本次计划/范围核验，不能只拿旧acceptedReviewId作永久许可。

复用的origin_round_id仍保留，不能改成新轮。verification记录引用本次selection/计划和实际执行上下文，从而一份Combination可有多次真实验证，历史证据不被覆盖。

## R10-03 / P1：容量降额下按单槽锁定仍可超额

原位置：§6 92—98行，capacity_slot只含作用域和槽号，分配按选中resource_key上advisory lock，宣称降额时已有占用继续计入，但无聚合作用域串行或全量计数规则。

反例：容量从4降到2，未释放任务仍占slot3/slot4；新算法只在1/2中找空槽，两个新Attempt都能通过槽位唯一索引，实际并发仍4，违反“已有占用计入”。leaked/unknown处于被删除编号时同样出错。

证据：ADR0017要求团队/项目/全局约束在统一预留事务内成立，并且未知不等于释放。方案自身也要求降额旧占用继续计入。

最小修复：capacity_slot新增有型`capacity_scope_kind`、`capacity_scope_id`和`slot_index`，或用严格可逆编码并在同表生成对应字段；非capacity子型这些列空。先按固定顺序取得各capacity scope的事务advisory锁，再读取适用上限和该scope全部未释放reservation数量，包含高于新上限的槽、reserved/active/release_unconfirmed/leaked。仅当全量占用加本次需求不超上限才挑选可用槽，并用现有唯一约束兜底。降额/容量政策切换也遵循同scope锁或明确版本CAS；不清理旧占用、不新增容量表。跨global/project/team一次全部取得或回滚。单个资源key锁继续保护Worker等独立资源。容量scope锁位于既有Issue/预算→round→plan→task→Attempt→Candidate之后、具体resource_key锁之前，同层按规范化scope排序。修改容量若不需要上层对象可从scope层开始，但拿到scope/resource锁后不得回拿上层业务锁；所有分配/释放/配置生效路径使用相同次序。

## R10-04 / P1：跨仓边的证据引用尚不能证明前驱与当前组合

原位置：§9 137—141行只新增evidence_record_id/evidence_kind，却承诺可以引用初审、验证产物或最终裁决。result_admissions是另一张表，只有execution_record FK无法引用它；只核证据id/kind还可以引用同Issue另一任务的accepted记录。

证据：Graph §3/§4要求跨仓前驱、正式结果采纳、组合与证据适用性，ready不能自行放行。旧b11 §3.3定义三类边条件，但本次物理方案必须使其声明的复合保障成立。

最小修复按condition给出有型证据分支：artifact_accepted引用candidate_review，证明候选source_task_id=from_task_id且该初审在当前计划/范围仍accepted；verification_evidence引用R10-01的verification/适用性判断，核验证任务/组合/当前selection及所需检查；business_acceptance引用明确的业务判断或admission列，不能将任意execution_records.id当其他表ID。若仅result admission不足以表达business acceptance，保留该边waiting直到真实业务判断存在，不把admit结果解释成完整验收。

为每个实际被引用组合列补普通UNIQUE，复合FK至少绑定Issue及对应来源；前驱、组合、当前计划等跨行规则使用持round锁的约束触发器验证。state=satisfied要求且只允许对应证据分支完整，其他分支列空。当前计划/组合改变后重核适用性并失效旧满足状态，保留旧证据历史。无需增加物理表。

## R10-05 / P2：外键目标与跨域版本类型需具体落齐

原位置：§3/§7/§8/§11多处称“补充唯一键”；末尾B06键要求仍列主CS可能内聚、scope子型常量两种未选择路径；共同字段“代次/修订bigint”会覆盖上游ETag和旧配置text。

最小修复以已确定B06布局写精确目标，不留两套物理方案：

- 所有工作范围FK(issue_id,repository_id) → `repomesh_issues.repository_scopes(work_issue_id,repository_id)`。work_issue_id由is_work生成，仅工作行非空；不加多态kind常量，不引用全部内容scope裸owner键。
- Issue FK(project_id,issue_id) → issues(project_id,id)；CS FK(project_id,issue_id,changeset_id) → changesets(project_id,issue_id,id)。主CS仍独立表。
- B07 Leader FK(project_id,issue_id,repository_id,room_link_id) → issue_room_links同名普通UNIQUE，已确认存在。
- plan_revisions补普通UNIQUE(id,round_id,issue_id)，用于组合origin验证计划、本次selection验证计划及verification上下文；原四列唯一不能直接充当三列FK目标。
- execution_operations补普通UNIQUE(id,attempt_id)，用于records.operation_id+attempt_id；result_admissions的观察引用必须带record_kind=observation并使用records已列普通复合唯一目标，不能指artifact冒充原始观察。
- 新增verification和edge分支的目标键一起列入完整FK矩阵。
- `base_revision/applied_revision/binding_revision`若来自不透明外部ETag/修订用text；B06 content/config revision及B04 profile/provider版本用text。内部plan_set_revision、pointer_revision、application_revision、review/transition/claim计数可bigint。不要对所有名为revision的列一刀切。

## R10-06 / P2：append-only与敏感正文清理必须有一致的存储边界

原位置：§8公共evidence/json、candidate_review.reason、stop.detail与“所有记录只增不改”；§11“业务清理只删外部正文并追加retention”。如果evidence、raw观察、reason或规范输入已经包含敏感原文，仅删外部附件无法清除这些数据库副本。

证据：首批持久化§1.2和消息内部§5.3要求清所有可恢复正文副本；CS14允许保留归属、版本、历史结论并标附件缺失，并未允许删除请求后保留原文副本。

最小修复二选一并在字段表明确：所有内联evidence/原因/输入只允许封闭的非正文引用与枚举，正文统一受控外部附件；或给可能含正文的列增加可空清理白名单和redacted_at，允许受限清理将正文置空，同时身份、SHA/产物固定摘要、归属及决定值仍不可修改。追加retention保留可用性变化，不能拿追加一条“已删”掩盖旧inline正文仍在。保存用于来源/版本的Git SHA或强产物摘要与创建正文低熵digest区别说明，不必删除真实版本身份。

## R10-07 / P2：组合快照上限的实际保存位置未定义

原位置：§11 170行“部署配置固定max_combination_members/max_combination_snapshot_bytes，写入记录保存适用限制版本”，但公共/combination列没有这些值或版本列。若只写一个不透明config ref，审计无法重建当时检查，也会隐藏新配置表依赖。

最小修复在change_set_versions的combination分支明确加`applied_max_combination_members integer`、`applied_max_combination_snapshot_bytes bigint`、`combination_limit_config_revision text`，三者创建时冻结、均正值/非空；candidate分支空。版本为部署载入时对这两个有效数值和限制schema规范化得到的内容指纹，数值同行保存，不引用新配置表。DB触发器以同行值核member_count与规范化快照字节数，并验证写入值匹配本次可信部署上下文；不能信任客户端任意填超大上限。单次更新配置只影响之后的新组合，历史组合保留当时数值；旧组合新启动仍按当前适用策略核可执行性。无需冻结一个未授权的产品级仓库数上限。

## 已核对可保留与明确限制

十一表可以完整承载原20表的物理映射；Candidate/Combination真实身份由change_set_versions落地，不再依赖不存在的B06候选表。Combination成员采用封闭不可变JSON，专用DB触发器展开核验、引用根禁止删除且owner归属不可改，这一方案可以保留；不能把这些JSON元素宣称成声明式FK。

whole-plan使用round锁、plan_set_revision和完整(repository,plan,pointer revision)集合，指针移除/重加不回到1，方向正确。应用进度允许与不可变计划分列，readback_ok且recovery none/recovered后才放行，避免recovering被误包含。unknown/leaked继续占资源，任务全历史replacement_allowed检查保留，may-have-sent不能新ID重发，均符合原语义。结果reject_stale保留source_round与当前裁决round分别FK，needs_recheck不占最终唯一裁决，也正确。

业务Plan Version与上游plan_revisions仍是两个概念。现行Graph§5定义前者，但本轮原63表提案未给其完整物理模型；应保留这项已有未完成依赖，不能因为本轮有canonical_plan就宣称冻结了业务计划/许可模型，也不能藏一张未计数PlanVersion表。本轮计数是被审查的物理提案集合，不是全部未来业务已经有完整存储。执行门槛/G3—G5及P9未采用状态照旧。

以上为静态裁决，未执行并发、数据库约束或外部恢复测试。

## R10-08 / P1：业务Plan Version是已采用执行前置，建议独立增加一张表

本项替代上文“只保留未完成依赖”的交付建议。根代理补充确认本次要求覆盖后续B04—B11设计完整性，因此不能仅注明缺失后仍把本批描述为可实施的完整执行设计。

证据足以区分延期审批与当前业务计划：ADR0003首部明确当前统一YOLO、审批后续开发，但“计划版本、许可与生效规则继续有效”；§1.1规定不可变业务版本与原依据关系，§1.3循环上限写入实际计划，§1.4规定获准待应用和所有必要目标读回后的生效，§1.6要求RepoMesh保存许可并控制应用/启动。ADR0006现行补充同样要求YOLO自动判断、记录许可和应用。ADR0015“已采用的行为”要求跨轮沿同业务计划、任务安排更新与业务计划分开。Graph§5分别列业务Plan Version、轮次和上游安排修订。

现有浏览器API没有已冻结的PlanVersionID字段；B10 PlanRound/Delegate与B11 Evaluate/OpenNextRound的Go签名也属于候选，尚未显式传该ID。该事实不解除已采用前置条件：这些用例声称核“有效计划”和“既有上限”，必须能从数据库读取原版本和许可/生效事实。旧b11跨批表399行错把业务计划版本权威指向B06，B06实际无该表。本次给Candidate/Combination补真实权威采用同样理由。

建议新增一张`repomesh_execution.business_plan_versions`，由Issue计划用例写，执行层只读取；总新增34，B10/B11变12。不把它加进change_set_versions枚举，也不将上游plan_revisions换名冒充。

最小列组：

- 身份：id PK、project_id、issue_id、version_index、base_plan_version_id可空、created_by、created_at。UNIQUE(issue_id,version_index)、UNIQUE(project_id,issue_id,id)。FK(project_id,issue_id)→B06 issues(project_id,id)，base版本同Issue复合自FK且不允许回指自身/环。
- 不可变内容：plan_schema_version、canonical_plan bytea/封闭版本化plan正文、canonical_digest、明确repository_ids集合、验收依据/范围、适用budget/time policy的确切引用、repair/diagnostic/environment恢复上限。正文结构须限定为业务目标/范围/依赖/验收与上限，不另造仓内DAG权威；repository_ids由触发器逐项核B06工作范围，禁止重复/超范围，不把这个数组当任意entity列表。上限数值沿已采用规则及有效政策，不在本审查擅自新增范围。
- 创建原操作：cause_operation_id/source_operation_id、input_schema_version、canonical_input和稳定提交结果身份，同键异输入拒绝，键永久保留。当前可信服务分配操作键，不能让正文或显示编号充当身份。
- 当前YOLO许可事实：authorization_id、authorization_decision（authorized/rejected）、authorization_actor/service、authorization_basis_version、authorized_or_rejected_at、非敏感reason。基础版本可先持久为尚未判断，正式许可字段全空或完整；一份版本的已经记录许可不回写成另一结论，需重新改变内容/依据时创建显式新版本或未来专门续许可协议，不能本轮顺带设计人工审批。
- 应用/生效最小字段：activation_state（not_requested/pending/applying/unknown/effective/superseded，精确迁移由作者定稿）、activation_revision、effective_at、superseded_at。许可与生效分列，authorized本身不可派工。逐目标外部操作/读回/失败历史继续由已有plan_revisions应用组和execution_records.plan_observation承载，不复制第二套外部状态。可以在新Issue行加current_business_plan_version_id复合FK与指针revision，或此表加is_current及全Issue至多一个有效版本的部分唯一；只选一种权威，旧36不改。

必需下游关系：rounds固定business_plan_version_id，跨轮默认复用原ID；plan_revisions关联同轮同Issue的业务计划，不能另选不匹配版本；verification绑定实际验收所依据的业务版本，selection保存本次上下文；Delegate在锁Issue/业务计划/round后验证获准、生效、范围与上限仍有效。业务版本改变应冻结原内容并建立新版本，只有其全部适用上游目标读回确认后才原子换当前指针。仅下一轮技术安排变化不创建新业务版本。

Go内部提案为PlanRound/OpenNextRound/Delegate增加或由可信服务读出明确BusinessPlanVersionID与expected revision，用实际FK和事务校验，不更改45条浏览器API。第一版获准但未生效时没有旧有效计划，不因已有round就能启动。新版本部分应用unknown时受影响范围继续停止新派工，保存旧版本与成功目标证据。该表若不落入当前设计，B10/B11只能标“缺业务计划持久权威，禁止按本文独立实施真实执行”，不能声称后续设计已完整覆盖。

以上是向根代理提交的建议，尚未通知作者加表；是否将总数从33调整为34由根代理统一决定。
