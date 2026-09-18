# B10 与 B11 合表裁决

本方案将原 20 张提案表压到 11 张。仅设计后续物理结构，不改动已有 36 张表，不声称执行能力已实现。已核对 B10、B11、B06 ChangeSet/范围、B07 房间字段，以及 ADR 0013/0015/0017、Graph 专题第 2—5 节、ChangeSet CS1—CS14 和结构说明、执行与验证专题相关章节。没有运行上游、数据库或容器。写作按 unslop 技能自查。

最终表为 `rounds`、`repository_issues`、`plan_revisions`、`execution_tasks`、`attempts`、`resource_reservations`、`execution_operations`、`execution_records`、`cross_repo_edges`、`result_admissions`、`change_set_versions`，均可放在 `repomesh_execution` schema。最后一表由 ChangeSet/交付用例持有，B10 首次单轮候选即需要此基础表，B11 复用；schema 位置不把业务采纳权交给执行 Adapter。

合并有两条关键约束。第一，计划不可变指的是计划身份与 payload 不可改，应用进度和当前指针可以与它分列存储。第二，候选与组合必须有真实业务身份，不能把原来的两张执行映射删掉后，声称 B06 已有实际不存在的候选/组合表。本方案用 `change_set_versions` 的 candidate/combination 两个明确子型保存真实业务版本。组合成员是一次选定的有界不可变快照，数据库触发器负责完整引用约束；执行观察与映射不再复制业务身份。

## 原 20 表的完整映射

| 原表 | 新位置 | 保留的事实 |
| --- | --- | --- |
| rounds | rounds | Issue 内轮次身份、状态、停止/收尾摘要、整轮计划集合版本 |
| repository_issues | repository_issues | 首次实际委派登记的同 Issue 同仓稳定事项 |
| plan_revisions | plan_revisions 的不可变列组 | 轮次、仓库、上游绑定、任务集合、写入者、内容摘要 |
| round_plan_pointers | plan_revisions 的 is_current/pointer_revision 列组；rounds.plan_set_revision | 每轮每仓当前修订，完整多仓当前集合与 CAS |
| execution_tasks | execution_tasks | 计划内本地任务、仓内 DAG 映射；任务不等于 Attempt |
| attempts | attempts | 领取、启动、停止、撤销、回收及替补许可 |
| worker_leases | resource_reservations，resource_kind=worker | Worker 排他占用、代次与释放证据 |
| resource_reservations | resource_reservations，其余资源子型 | 容量、容器、目录、网络、卷的逐项占用 |
| dispatch_operations | execution_operations，target=upstream | delegate/cancel/accept_result 的稳定操作回执 |
| host_operations | execution_operations，target=host | 七种有限宿主动作的稳定操作回执 |
| attempt_observations | execution_records，record_kind=observation | 原始状态、映射版本、观察来源和代次 |
| attempt_artifacts | execution_records，record_kind=artifact/retention | 不可变产物根及追加保留状态；Candidate 独立引用该根 |
| write_capability_revocations | execution_records，record_kind=write_revocation | 每种写能力的请求、未知和证实撤销记录 |
| round_transitions | execution_records，record_kind=round_transition；rounds 保存摘要 | 完整追加状态转换历史和收尾依据 |
| target_applications | plan_revisions 的 application/recovery 列组 | 每修订一份独立应用与恢复进度；不是整批成功位 |
| cross_repo_edges | cross_repo_edges | 跨仓依赖、逐边状态与证据 |
| round_candidate_refs | change_set_versions 的 candidate 子型、execution_records 初审，以及来源 Attempt 的轮次 | 正式候选身份/来源/初审历史直接可查，不再复制不透明 candidate_ref |
| round_combinations | change_set_versions 的 combination 子型与有界成员快照；rounds 当前组合引用 | 真正组合身份、每仓固定版本、候选/基线、选择者与依据 |
| result_admissions | result_admissions | 唯一最终 admit/reject，needs_recheck 不落最终裁决 |
| stop_decisions | execution_records，record_kind=stop_decision；rounds 保存摘要 | 累计停止决定及 B05 账本依据，不创建第二套消费账本 |

相减为 20 − 1 指针表 − 1 Worker 表 − 1 操作表 − 2 证据表 − 1 转换表 − 1 应用表 − 1 候选映射表 − 1 停止表 = 11。组合映射表升级成真实组合关系，仍只算一张，没有另藏业务候选表、组合成员表、历史表或通用队列表。视图不计物理表，但不能作为被引用的 FK 目标。

## 共同字段与数据库约束规则

以下 ID 用 text，时间用 timestamptz，代次/修订用 bigint，状态用 text+完整 CHECK，业务 ID 永不复用。每表有必要的 created_at，所有复合 FK 的被引用列都有普通 UNIQUE。被子型检查要求的关联列必须显式 IS NOT NULL；不能让 MATCH SIMPLE 的 NULL 豁免绕过归属。FK 默认 RESTRICT，不级联删除历史。

JSONB 容纳已定义形状的证据正文、受控位置引用、原始观察及本方案明确选择的不可变组合成员快照，必须有 schema_version、大小限制和验证器。Issue、仓库、Task、Attempt、Candidate、Combination、操作身份、代次、权限目标和状态均为有型列。组合成员中的仓库/Candidate/基线引用与固定 SHA 由第 11 节的专用约束触发器展开核验，不假称 JSON 元素已有声明式 FK。跨行数量、状态机、不可变列、追加链和证据有效性由具体表触发器及受控事务维护，应用角色不获随意绕过触发器的直接写权限。这不是通用 entity_id/payload 框架。

## 1. rounds

保留原字段 `id,project_id,issue_id,round_index,purpose,state,created_at,closed_at,closed_reason`。新增 `plan_set_revision bigint NOT NULL DEFAULT 0`、`state_revision bigint NOT NULL DEFAULT 0`、`outcome`、`last_transition_record_id`、`last_stop_record_id`、`stop_reason`、`stop_decided_at`、`current_combination_id`、`combination_selection_revision bigint NOT NULL DEFAULT 0`。round_index>=1；purpose 与 state 枚举沿用原 B10；本表不加 repository_id 或单条 current_plan_revision_id。

PK(id)，UNIQUE(issue_id,round_index)，UNIQUE(id,issue_id)，UNIQUE(id,project_id,issue_id)。FK(project_id,issue_id)→B06 Issue 归属键。当前组合以 `(current_combination_id,issue_id,固定combination子型)` 引用第 11 表的组合版本；空表示尚未选定。最近转换/停止字段以 `(record_id,id,固定record_kind)` 引用第 8 表，并 CHECK 对应种类。插入时摘要引用为空，先有轮次，再插追加记录，最后更新摘要；全部是立即外键，不需要延迟约束。

状态摘要和对应历史记录同事务提交。历史在 execution_records，不把全部历史塞进 rounds JSON。state_revision 随每次合法转换递增，记录保存转换前后的 revision，UNIQUE(round_id,transition_revision) 防重复。停止可以发生多次，保存每次不同原因与依据；rounds 只存最新有效摘要，不清除先前决定。轮次图沿用原 B11：planned→active/cancelled；active→draining/applying/recovering/closed/cancelled；draining→applying/recovering/closed/cancelled；applying→recovering/closed/cancelled；recovering→applying/closed/cancelled；closed/cancelled 无后继。通过收尾必须有当前组合下完整有效证据。

索引 `(issue_id,round_index DESC)`、部分索引 `(state,created_at) WHERE state NOT IN ('closed','cancelled')`。Graph/Close/Delegate 锁此行后读取所有 is_current 修订；既核 plan_set_revision，也核完整 `(repository_id,plan_revision_id,pointer_revision)` 集合。跨仓新增/移除不能绕过集合校验。

## 2. repository_issues

原表完整保留。字段 `id,project_id,issue_id,repository_id,state,created_at,closed_at,closed_reason`。PK(id)，UNIQUE(issue_id,repository_id) 覆盖所有历史；UNIQUE(id,issue_id,repository_id)。FK(project_id,issue_id)→B06 issues；FK(issue_id,repository_id)→B06 固定仓库范围的有型唯一键。

state 只含 active/closed，时间/原因与状态相容。Delegate 在首次实际登记 Attempt 的原子事务内创建；建 Issue、选仓、PlanRound 和 B07 读取均不能创建。后续轮次和重试复用原 id，普通轮次收尾不关闭。停用/重新启用复用原 id；Issue 完成后的新增工作使用关联新 Issue。索引 `(issue_id,state,repository_id)`。关闭不证明上游、房间或资源已经关闭。

## 3. plan_revisions

不可变列保留 `id,round_id,issue_id,revision_index,repository_id,upstream_binding,task_set_digest,created_by,created_at`，补 `plan_schema_version,canonical_plan bytea,validation_repository_ids text[] NOT NULL DEFAULT '{}'`，防止只有摘要没有可恢复原输入。若原契约把图正文固定在任务行，可保留相同规范化全文作为发布输入，不另造图权威。绑定明确 instance/team/project 与命名空间，外部引用不含秘密。validation_repository_ids 是该修订明确要求验证覆盖的固定仓库集合，普通无验证安排可以为空；触发器拒绝重复项并逐项核对同 Issue 工作范围，组合创建所引用的验证修订必须非空。它与规范化计划内容相同且不可变，不从当前改图仓库集合临时猜测。

可变指针列是 `is_current boolean NOT NULL DEFAULT false`、`pointer_revision bigint`、`pointer_changed_at`。is_current 为 true 时后两列非空且 revision>0；撤销 current 后保留最后版本。PK(id)，原 UNIQUE(round_id,revision_index)、UNIQUE(round_id,repository_id,id)、UNIQUE(id,issue_id,repository_id) 保留；补 UNIQUE(id,round_id,issue_id,repository_id)。部分 UNIQUE(round_id,repository_id) WHERE is_current 强制每轮每仓至多一个当前修订。索引 `(round_id,repository_id,pointer_revision) WHERE is_current` 是完整指针视图的索引。

切换时锁 rounds，核完整旧集合，先把原修订 is_current=false，再设新修订 true，写下一版本并递增 rounds.plan_set_revision，同一短事务提交。pointer_revision 使用此次全局 plan_set_revision+1，避免仓库移除后重新加入时回到 1。无读者看到中间空指针。当前集合为空只允许 planned 或已经关闭/取消的无执行轮次；转入可执行状态时由轮次触发器/事务检查必要仓库完整性。移出仓库须先确认该范围无在途/未收敛 Attempt。旧修订的身份、payload 和历史状态仍保留。

应用列完整吸收 target_applications：`application_id text UNIQUE`、`application_path`、`application_state`、`external_operation_id text UNIQUE`、`application_schema_version`、`application_canonical_input bytea`、`application_may_have_sent_at`、`base_revision,applied_revision,application_observed_at,application_evidence`、`recovery_state,resume_condition,recovery_observed_at,recovery_evidence,application_revision bigint`。应用尚未建立时 application_id/external_operation_id/input 全空且 state=not_requested；建立后必填并不可改，状态进入原 pending/publishing/applied/readback_ok/readback_mismatch/failed/unknown 枚举。not_requested 只表达 B10 计划建好但还没创建外部应用操作，不冒充成功。

应用与恢复各有独立状态机，保留原 B11 转移与恢复条件。readback_mismatch/unknown/open 必须有恢复说明；恢复确认时在同次更新设置 recovered+readback_ok。放行采用显式完成集合 `application_state='readback_ok' AND recovery_state IN ('none','recovered')`，避免原文“不是open或unknown”意外包含 recovering。只读回一仓不能恢复整批派工。

不可变 trigger 拒绝修改 id/round/Issue/仓库/索引/绑定/图正文/摘要/创建者/时间，以及已经建立的应用身份和规范化输入；允许指针和应用状态列按专用 CAS 更新。应用进度的更新绝不能改变 DAG。应用观察如需完整历史，写 execution_records 的 plan_observation 子型，引用具体 plan_revision_id，与当前摘要同事务推进。索引 `(application_state,recovery_state,application_observed_at)` 用于未完成核查；它不承担后台共享队列。

FK(round_id,issue_id)→rounds；FK(issue_id,repository_id)→B06 固定仓库范围。无需物理 round_plan_pointers，可提供同名只读 VIEW 暴露原查询形状供文档迁移；不能一部分用旧表、一部分用新列。

## 4. execution_tasks

保留原所有字段，显式增加 `round_id`，将 `attempt_id` 命名为 `current_attempt_id`。PK(id)，UNIQUE(plan_revision_id,local_key)，UNIQUE(plan_revision_id,upstream_task_id)，UNIQUE(id,issue_id,repository_id)，UNIQUE(id,round_id,issue_id,repository_id)。FK(plan_revision_id,round_id,issue_id,repository_id)→plan_revisions 同列唯一键。依赖只保存本修订的 local_key[]，验证引用存在、同修订和有限 DAG 合法性仍复用上游；不实现新的 Go DAG。

current_attempt_id 为空可创建任务；Attempt 建好后，用 `(current_attempt_id,id,issue_id,repository_id)` FK→attempts(id,task_id,issue_id,repository_id) 更新。新建顺序 task→attempt→task 指针，无延迟 FK。条件状态转移按任务行锁与当前代次，不能将上游 completed 直接更新为 accepted。索引 `(round_id,plan_revision_id,state)`。上游任务编码含项目/Issue/轮次/任务，实际 Attempt 外部执行 ID 另含 Attempt，避免共享目录和通知在替补时复用。

## 5. attempts

完整保留原列与状态、替补 CHECK；补 round_id、实际执行命名空间 `upstream_attempt_task_ref`、不可变 `capability_manifest` 的有版本有限清单及 `capability_manifest_digest`。后者记录实际授予的 Git/对象存储/控制面写能力及其能力版本，不把没授予的能力要求成伪造撤销证据。若某种能力通过配置确定永不授予，记录明确的未授予事实。

PK(id)，UNIQUE(task_id,attempt_no)，UNIQUE(id,task_id,issue_id,repository_id)，UNIQUE(id,round_id,issue_id,repository_id)，UNIQUE(id,task_id,round_id,issue_id,repository_id)。原部分 UNIQUE(task_id) WHERE state IN ('prepared','starting','running','submitted','stopping','unknown') 保留。FK(task_id,round_id,issue_id,repository_id)→tasks；FK(repository_issue_id,issue_id,repository_id)→repository_issues；FK(project_id,issue_id)→B06 issues；可空房间 FK(project_id,issue_id,repository_id,room_link_id)→B07 原四列唯一键。因 repository_id 非空，B07 CHECK 保证它只匹配 leader 房间，空 room 不豁免 project/Issue 归属。

replacement_allowed 默认 false，CHECK 原式保留：只有 stopped/reclaimed/failed 且从未可能启动，或同时有实际停止和全量写能力撤销证据，才可 true。时间摘要由同 Attempt/能力版本的 execution_records 推进，应用不能直接凭时间填充。终态不得返回活跃态；stopped/failed→reclaimed 需逐项回收证据。may_have_started_at 只能空→非空。claim_generation 是核查领取代次，不能把一次旧启动重新变成可启动的新 Attempt。

新 Attempt 必须锁任务后读取全部历史，全为 replacement_allowed 才能登记。不因前一 Attempt 在部分唯一索引之外就忽略旧 false 行。所有修改许可的路径同样先锁任务，绝不 Attempt→任务反向取锁。索引 `(state,lease_until)`、`(repository_issue_id,created_at)`、`(task_id,attempt_no DESC)`。

## 6. resource_reservations

字段 `id,attempt_id,resource_kind,resource_key,state,generation,reserved_at,released_at,owner_manager`。kind 为 worker/capacity_slot/clone_dir/container/network/volume；state 为 reserved/active/released/release_unconfirmed/leaked。worker 的 key 是经过实例/团队作用域消歧的 Worker 稳定引用，不能只用显示名。capacity_slot 的 key 含 global/project/team 作用域和固定槽位编号；一次 Attempt 可以同时预留三类约束所需槽位。owner_manager 只允许 controller/host_executor 等已明确生命周期管理者，不能同一资源两头管理。

PK(id)，FK(attempt_id)→attempts；UNIQUE(attempt_id,resource_kind,resource_key,generation)。部分 UNIQUE(resource_kind,resource_key) WHERE state IN ('reserved','active','release_unconfirmed','leaked')。将 leaked 也保留在排他集合里，修复原方案把泄漏排除后可能重用同资源的缺口。部分 UNIQUE(attempt_id) WHERE resource_kind='worker' AND state<>'released' 确保同 Attempt 不挂两份当前 Worker；触发器检查 worker key 与 attempts.worker_ref 一致。每个 Attempt 的首次登记必须同事务含一条 Worker 和所有适用容量 reservation，受控写入口/提交约束验证，不允许单独提交半套预留。

没有持久资源行时不能用 SELECT FOR UPDATE 锁不存在的行。分配前按规范化 `(resource_kind,resource_key)` 排序获取事务级 advisory lock；冲突哈希只产生多余串行，不允许碰撞放行。容量使用有限槽位集合，选择到的槽位仍由唯一索引兜底。容量上限的固定来源引用随规范化派工输入保存；减额期间已有占用继续计入，不释放超额旧任务。索引 `(attempt_id,state,resource_kind)`、`(state,reserved_at) WHERE state IN ('release_unconfirmed','leaked')`。

所有保留记录不因租约超时自动 released。已释放事实保留历史，新占用创建新 id 和更高 generation。回收只能释放有证据的具体资源；从 leaked 回到 released 必须补实际回收观察。全部完成才更新 Attempt.reclaimed_at。

## 7. execution_operations

只涵盖去往执行上游/受限 host 的动作，不接管消息、模型测试、Issue 创建、轮次决定或全系统 DurableWork。字段 `id,attempt_id,target,upstream_kind,host_action,schema_version,canonical_input bytea,request_digest,external_operation_id,state,may_have_sent_at,observed_at,evidence,claim_generation,created_at`。PK(id)，FK(attempt_id)→attempts，UNIQUE(external_operation_id)。external_operation_id 从 prepared 即非空、由服务端分配；外部传输重试不换号。规范化输入必须能重比，不能只留不可复核的 hash。

完整子型 CHECK：target=upstream 时 upstream_kind IN(delegate,cancel,accept_result) 且 host_action IS NULL；target=host 时 host_action IN(PrepareAttempt,StartAttempt,ObserveAttempt,RequestStop,RevokeWriteCapability,ReclaimAttempt,VerifyStopped) 且 upstream_kind IS NULL。另保存不透明目标命名空间/协议版本于固定规范化输入，不从可变当前计划反查重试目标。一个逻辑动作的操作 PK 全期固定，schema_version 不进入唯一键。

统一状态枚举 prepared/may_have_sent/observed/reconcile_required/failed_before_send。原 host sent 对应 may_have_sent，host unknown 对应 reconcile_required。这两组是相同传输阶段，不是业务执行结果；observed 可以观察到取消失败或资源仍活，并不表示操作业务成功。每子型 outcome、证据仍按具体动作解释；allowed transition trigger 禁止 may_have_sent 后退回 prepared/failed_before_send，may_have_sent_at 只能空→非空。发送前提交标记，响应丢失按旧身份读回，不能重建新启动操作。重复观察追加 execution_records，最新证据摘要可更新。

UNIQUE(attempt_id) WHERE target='upstream' AND upstream_kind='delegate' 防止同 Attempt 创建第二个委派身份；UNIQUE(attempt_id) WHERE target='host' AND host_action='StartAttempt' 防止第二次启动身份。其他动作允许不同业务请求有不同稳定 id；重试仍重用原 id。索引 `(state,observed_at)` 和 `(attempt_id,target,created_at)`。

accept_result 的上游回执只说明上游是否接收。它必须引用已经通过本地准入的结果操作输入，不能替代 result_admissions，不能直接解锁跨仓边。

## 8. execution_records

这是有型的执行事实与决定日志，服务 Attempt、计划应用和轮次三个明确聚合；不是任意 entity 的总事件仓。所有记录只增不改；正文清理追加 retention 记录并清理外部附件，保留身份、摘要和引用。公共列为 `id,record_kind,issue_id,round_id,repository_id,attempt_id,plan_revision_id,operation_id,recorded_at,observed_at,actor,schema_version,payload_digest,evidence`。FK(round_id,issue_id)→rounds 始终非空；Attempt 子型必须 attempt_id/repository_id 非空，并 FK(attempt_id,round_id,issue_id,repository_id)→attempts；plan_observation 必须 plan_revision_id/repository_id 非空，并以同作用域 FK→plans；round_transition/stop_decision/combination_selection 子型强制 attempt_id/plan_revision_id/repository_id 为空；baseline_observation 单独要求 repository_id 非空且 Attempt/plan 为空，以同 Issue 仓库 FK 保证范围。operation_id 非空时 FK(operation_id,attempt_id)→operations 的补充唯一键，以免挂另一个 Attempt 的操作。不同子型无关字段强制 NULL。

| record_kind | 专用有型列及检查 |
| --- | --- |
| observation | source、raw_status、normalized_status、mapping_version、binding_revision、claim_generation；source 枚举保持原四类；映射版本必填 |
| artifact | artifact_kind、controlled_ref、content_digest、fixed_sha、changeset_id、submitted_by；根记录本身是稳定 artifact_id，Candidate 由第 11 表的独立业务版本引用该根；代码产物 fixed_sha 非空，非代码产物保留固定产物版本/摘要，不伪造 SHA |
| retention | artifact_id、artifact_root_kind='artifact'、retention_state、reason；状态 observed/verified/archived/missing/deleted，复合 FK 要求同 Issue/同仓/同 Attempt 的产物根 |
| candidate_review | candidate_id、candidate_version_kind='candidate'、review_revision、review_decision、review_scope、review_basis_ref、reason；decision accepted/rejected/needs_recheck/diagnostic_only，正式可选只认最新有效 accepted，任何旧初审/未接受原因都保留 |
| write_revocation | capability_kind、capability_version、revocation_request_id、revocation_state、requested_at；state requested/observed/unknown，observed 需证据和 observed_at；capability_kind 为 git/object_store/controller，必须对应 Attempt 当时实际能力清单 |
| baseline_observation | fixed_sha、target_branch、repository_source_ref、binding_revision；repository_id 必填，FK(issue_id,repository_id)→B06 工作范围；attempt_id/plan_revision_id 均 NULL，可以在未改仓无任务时登记受控固定基线证据 |
| plan_observation | application_id、application_state、recovery_state、binding_revision、原外部操作身份；通过 plan_revision_id 锁定目标，保存每次应用/恢复观察，不重复存可写绑定 |
| round_transition | from_state、to_state、transition_revision、outcome、reason、evidence_refs；作用域是该轮，序号和前态由 rounds 锁确定，outcome 枚举沿用 B11 |
| stop_decision | stop_reason、consumption_refs、detail；stop_reason 原五类，consumption_refs 是 B05 账本窗口/版本的有型引用结构，不能内嵌另一份可写消耗权威 |
| combination_selection | combination_id、selection_revision、selection_purpose、selection_basis_ref；同 Issue 组合 FK，记录轮次选择历史，purpose formal/diagnostic；formal 需每个候选在选择时已被有效接受 |

补充普通唯一键 `(id,record_kind)`、`(id,round_id,record_kind)`、`(id,issue_id,repository_id,record_kind,artifact_kind)`、`(id,attempt_id,issue_id,repository_id,record_kind)` 供强制子型 FK。candidate_review 必填 changeset_id/source Attempt，round_id 保持该候选来源轮次，另用 review_scope/review_basis_ref 标明本次适用计划与范围；用固定 candidate 判别列向第 11 表形成同 Issue/同仓/同 ChangeSet 的复合 FK，不能引用 combination 或另一仓候选；其 Attempt 归属由事务和候选源 Attempt 一并核对。Candidate 正式提交由第 11 表分配稳定业务 ID，普通观察到文件仅创建 artifact，不自动创建正式 Candidate。

`UNIQUE(candidate_id,review_revision) WHERE record_kind='candidate_review'`；`UNIQUE(round_id,transition_revision) WHERE record_kind='round_transition'`；`UNIQUE(round_id,selection_revision) WHERE record_kind='combination_selection'`。查询索引 `(attempt_id,recorded_at,id)`、`(round_id,record_kind,recorded_at,id)`、`(candidate_id,review_revision DESC) WHERE record_kind='candidate_review'`、`(artifact_id,recorded_at DESC,id) WHERE record_kind='retention'`、`(attempt_id,capability_kind,capability_version,observed_at DESC) WHERE record_kind='write_revocation'`。root 行 append-only，回收更新行为改成追加 retention；最近状态用视图/window query，不新增物理当前表。

本表比原三类观察合并多保存轮次/计划的依据，是同执行过程的有型历史。每种记录只能由对应受控用例写入。正式结果准入仍单独在 result_admissions，因为它有严格全期唯一最终裁决语义，不能与普通观察混为一类。

## 9. cross_repo_edges

保留 `id,issue_id,round_id,from_task_id,to_task_id,condition,state,evidence_ref,updated_at`，补 `from_repository_id,to_repository_id` 与可验证 `evidence_record_id/evidence_kind`、`edge_revision`。PK(id)，UNIQUE(round_id,from_task_id,to_task_id)。两端分别用 `(task_id,round_id,issue_id,repository_id)` FK→tasks。CHECK from_task_id<>to_task_id 且 from_repository_id<>to_repository_id，排除跨 Issue、跨轮和假跨仓关系。

condition 原 artifact_accepted/verification_evidence/business_acceptance，state waiting/satisfied/invalidated。satisfied 必须有可定位的有效证据；证据可以是 Candidate review/验证产物/正式裁决，使用具体 FK 列与条件子型检查，不用裸 evidence_ref 声称已被 FK 保护。若保留 evidence_ref 给外部证据，另有 source/version/digest 且受控核查通过后才能满足。索引 `(round_id,to_task_id,state)`、`(from_task_id,state)`。

边更改锁 round 后逐边 CAS。失效保留先前满足证据的 execution_records 观察/相关决定；不删除边来绕过前驱。仓内 ready 只是输入，边没有 satisfied 时不放行。新计划修订的任务身份与旧任务不同，旧边不能自动挂到新任务。

## 10. result_admissions

保留 `id,result_ref,round_id,task_id,attempt_id,attempt_generation,decision,reason_codes,decided_at`，补 `issue_id,repository_id,source_round_id,result_namespace,observed_record_id,evaluated_plan_set_revision`。round_id 是本次裁决所针对的当前轮次；source_round_id 是结果实际来自的轮次，两者必须分开，否则不能同时用真实 FK 和保存旧轮次 reject_stale。

PK(id)，UNIQUE(result_namespace,result_ref,attempt_generation)，命名空间含上游实例/团队/Project，不同上游相同短 result_ref 不碰撞。FK(round_id,issue_id)→rounds；FK(attempt_id,task_id,source_round_id,issue_id,repository_id)→attempts；observed_record_id 以同 Attempt 复合 FK→execution_records。attempt_generation 从观察事实固定，旧 generation 结果可被 reject，不能要求它等于 Attempt 当前 claim_generation 才允许保留历史。

decision CHECK 仅 admit_for_current_round/reject_stale。admit 时要求 source_round_id=round_id，且事务内锁 round、核完整计划集合和当前 Attempt、代次与有效证据。reject_stale 可以来自另一轮，但须属于相同 Issue 的真实来源；外部归属不明先保持隔离观察，不伪造一条本地 Attempt FK。needs_recheck 不插入本表，保留原执行待核查责任。

只增不改；重复同结果重放读原裁决，异裁决拒绝。索引 `(round_id,decision,decided_at)`、`(attempt_id,decided_at)`。外部 accept 不能反向创建 admit，本地 admit 也不等于通过全部业务验收。最终准入之后，具体 Candidate 初审、验证复核和跨仓条件仍各自判断。

## 11. change_set_versions

该表只有 candidate 与 combination 两个明确业务子型，记录不可变版本。Candidate 是某次正式提交的固定产物，Combination 是 Manager 一次选定的固定仓库版本集合。它们同属 ChangeSet 的版本事实，共用稳定 ID、归属、创建人、时间和规范化版本摘要。它不是任意版本对象注册表，不加入 plan、PR、消息、预算等其他 kind。B10 单仓首次正式提交已经需要 Candidate，本表归 B10 基础，B11 复用并消费 combination。

公共字段 `id,version_kind,project_id,issue_id,changeset_id,origin_round_id,created_by,created_at,schema_version,canonical_digest,source_operation_id,canonical_input bytea`。PK(id)，UNIQUE(source_operation_id) 绑定同逻辑提交输入；UNIQUE(id,issue_id,changeset_id,version_kind)，UNIQUE(id,issue_id,version_kind)，UNIQUE(id,issue_id,changeset_id,repository_id,version_kind)。FK(project_id,issue_id,changeset_id)→B06 主 ChangeSet 归属键，FK(origin_round_id,issue_id)→rounds。所有 payload、身份和来源列创建后不可更新；判断/更正另追加 execution_records，不能回写旧版本。重复 source_operation_id 比较 schema/canonical_input，同键异输入冲突；比较器版本不进入唯一键。

candidate 专用列是 `repository_id,source_task_id,source_attempt_id,source_artifact_id,artifact_record_kind='artifact',artifact_version_kind,fixed_sha,artifact_digest,submitted_by,submitted_at`；组合相关列全部 NULL。repository_id/Task/Attempt/artifact 根非空。FK(source_attempt_id,source_task_id,origin_round_id,issue_id,repository_id)→attempts 五列唯一键；FK(source_artifact_id,source_attempt_id,issue_id,repository_id,artifact_record_kind)→execution_records 产物根唯一键；FK(issue_id,repository_id)→B06 固定工作范围。artifact_version_kind=git_commit 时 fixed_sha 非空且 artifact_digest 为 NULL，完整 SHA 格式与所在仓库 hash 算法相符；非代码产物用 artifact_version_kind=content_digest，artifact_digest 非空且 fixed_sha=NULL。触发器核版本与产物根完全一致，不从浮动分支/PR 名字代入。正式提交未接受仍建 Candidate，最新初审状态从第 8 表 candidate_review 读，不在此表改状态。

combination 专用列是 `selection_purpose,selection_basis_ref,validation_plan_revision_id,members jsonb,member_count,member_digest`；所有 candidate 来源专用列为 NULL。selection_purpose=formal/diagnostic。validation_plan_revision_id 非空并用 `(validation_plan_revision_id,origin_round_id,issue_id)` FK→plan_revisions 的补充唯一键，表示初次组合选定的有效工作/验收上下文，不把后来复用组合强行改到新轮。该计划的不可变 validation_repository_ids 列须明确本次验证覆盖仓库集合；组合快照必须恰好覆盖这一集合，包括未改动的参与仓库，不能仅比较本轮实际改图仓库。

成员采用 schemaVersion=1 的不可变对象 `{schemaVersion:1,members:[...]}`，每项只允许以下两种确切结构：

- 候选成员 `{repositoryId,sourceKind:"candidate",candidateId,fixedSha,acceptedReviewId}`。formal 组合的 acceptedReviewId 必填；diagnostic 可为 NULL，明确记录未获接受的用途。代码候选 fixedSha 必须等于 candidate.fixed_sha。
- 基线成员 `{repositoryId,sourceKind:"baseline",baselineObservationId,fixedSha}`。基线观察是 execution_records 的 baseline_observation 子型，必须记录同 Issue/同仓、实际观察到的固定 SHA、来源仓库/目标分支、观察版本和证据摘要；基线观察可在尚无 Attempt 时登记，不能为未改仓伪造任务。baselineObservationId 不是浮动分支名。

本批固定 SHA 组合只用于涉及代码的验证；非代码 Candidate 保留完整业务身份与固定摘要，非代码检查直接绑定该产物，不为了套用 Git 组合伪造 SHA。扩大 combination 成员到非代码版本时须显式扩 schemaVersion 与对应校验，而非默认接收未知成员类型。该边界不删除非代码交付能力。

快照的有界上限取部署配置固定的 `max_combination_members` 和 `max_combination_snapshot_bytes`，必须为正，写入记录保存适用的限制版本；数组还不得大于 Issue 已登记的仓库数。不能因达到上限截断成员集合，超限拒绝组合创建并返回真实原因。上限数值实施时随实际部署确定，本次不虚构任意产品仓库上限。member_count 和 member_digest 由触发器从按 repositoryId 排序的规范化完整成员计算，客户端无权提交不同摘要。对象缺键、多键、未知 schema、重复 repositoryId、空列表、非法 SHA、同一成员混用 candidate/baseline 引用全部拒绝。

### 组合完整性触发器与并发

`validate_change_set_combination` 是 BEFORE INSERT 专用数据库触发器，不只是应用层 JSON 校验。它在完整行插入前按下列步骤展开和验证，任一步失败则整条 INSERT 和创建事务回滚，不存在可对外引用的半套组合：

1. 锁所属 Issue 的稳定权威行；该锁与 B06 工作范围/归属写入采用同一顺序。锁 origin_round，核有效计划版本及完整 current 集合，读取 validation_plan_revision.validation_repository_ids 的固定验证仓库集合。用 jsonb_to_recordset 展开成员后比较精确集合；每个成员必须能 join 到同 Issue 的固定工作仓库范围。Issue/CS/round 与不可变计划均由实际 FK 锁定。
2. 按 candidateId 排序锁定所有候选版本行 `FOR UPDATE`；按 baselineObservationId 排序锁定所有基线观察根 `FOR KEY SHARE`。候选必须 version_kind=candidate、同 project/Issue/ChangeSet/仓库、固定 SHA 与快照完全相等。基线观察必须同 Issue/仓库且实际 fixed_sha 完全相等。错类型、缺失或跨域引用拒绝。
3. 候选初审写入口同样先锁 Issue→round→candidate，不能不锁候选就追加接受/撤回判断。formal 组合创建时，对每个候选读取当前适用计划/范围内最大 review_revision，必须是 accepted，acceptedReviewId 必须恰好是该记录，且记录的 Candidate/Scope/Plan 匹配。diagnostic 可以引用未接受候选，但 selection_purpose 不可冒充 formal。快照保留当时采用的 review ID，后来初审变化不改历史组合。
4. 核源 artifact 正文/版本当前可用于验证，记录缺失则拒绝声称已备妥。计算规范化 member_count/digest 后一次插入完整 immutable 版本。当前组合指针与 selection 记录再以同 Issue 的 combination 子型普通复合 FK 引用它。

候选/组合版本表禁止 UPDATE payload 和 DELETE，execution_records 根也禁止 DELETE，B06 工作范围和 Issue/CS 归属已有不可变/保留规则，因此 JSON 中引用的对象不会在触发器验证后被修改成另一归属或删除。这是明确的保留约束，不是认为 JSON 元素自带 FK。业务清理只删外部正文并追加 retention；历史版本/源身份/固定摘要保留。必要项目彻底删除必须走单独受控生命周期协议，本次不开放任意 SQL 删除来绕过引用检查。

选择现有组合或启动验证时再次锁 Issue→round→candidate 顺序读取最新初审和证据可用性。历史组合成员不变，但 candidate 新的拒绝/失效初审会阻止新的正式启动；既有在途验证保留实际输入和启动时依据，结果采纳重新判断适用性。组合读取不因某成员后来失效而改写当时接受记录。正式选择追加 execution_records.combination_selection 并更新 rounds 当前组合/selection revision，复用组合不写该版本行。

同一 Candidate 可进入多个组合，同一组合可跨轮复验，一轮可先后选择 C1/C2。原 UNIQUE(round_id,repository_id) 限制移除，唯一性在一个不可变快照内部是 repositoryId。新 SHA 必须新 Combination ID；补充证据可以继续引用原 Combination。初次选择上下文 origin_round_id 保留，后续选择历史在 execution_records。

### 查询与索引

普通索引 `(changeset_id,version_kind,created_at DESC,id)`、`(source_attempt_id) WHERE version_kind='candidate'`、`(issue_id,repository_id,created_at DESC) WHERE version_kind='candidate'`。组合成员查询用 `jsonb_to_recordset(members->'members')` 的只读 VIEW 投影 `(combination_id,issue_id,changeset_id,repository_id,source_kind,candidate_id,baseline_observation_id,fixed_sha,accepted_review_id)`。按 Candidate 找所有组合使用 members 的 jsonb_path_ops GIN 包含查询，读取后仍精确展开匹配；按固定 SHA/仓库关联历史同样展开，必要时增加明确表达式索引，不增加隐藏物理映射表或物化成员表。

查询 Candidate 的所有来源轮次直接 join source_attempt；查询一轮正式候选先从该轮 source Attempt/当前组合成员定位 Candidate，再取适用 candidate_review。未接受、未进入组合的 Candidate 仍可独立查询其提交和初审原因，不因没有组合而消失。B07 投影读真实 Candidate/Combination ID，无需 round_candidate_refs/round_combinations 两张映射。

## 事务、锁序与重放

统一顺序是：需要的 Issue/预算约束行按已有域内顺序 → rounds 按 ID → 当前 plans 按 repository_id/id → tasks 按 ID → attempts → change_set_versions 候选按 ID → 资源 advisory locks 按 kind/key → reservation/operation/record 具体行。B05 预算锁顺序必须和 B05 共同固定，禁止一个入口资源→预算而另一个预算→资源。只读 Graph Evaluate 可用一致快照读取 plan_set_revision 与完整集合，但真正 Delegate 必须在锁 round 的写事务再验；读时的 allow_dispatch 不是不可撤销授权。

原子派工：核可信 B09 调用上下文/Issue 固定配置/计划/权限/预算/时限/轮次/所有跨仓前驱；锁任务，核全历史替补许可；创建或复用 repository_issue；登记 Attempt、Worker 和各类容量、固定派工操作及持久后续责任；同事务提交。所有外部环境准备在事务外。实际 delegate/start 前再次核计划集合版本、权限、预算、Worker/reservation 代次、操作身份，先持久 may_have_sent，再调用有限 Adapter/host 动作。

准备失败可释放有证明尚未使用或已经实际释放的资源；启动不明不能因为租约过期释放。停止请求、进程实际停止、全量实际写能力撤销、各类资源回收依次有证据。Attempt 替补许可只在锁任务后核全部必要证据，容器 stopped 不能代替 Git 凭据撤销，撤销 Git 不能代替对象存储或控制面撤销。replacement_allowed 不隐含 Worker/capacity 已释放，资源排他索引独立阻止复用。

换计划：停新派工、收敛全部受影响目标的 in-flight/submitted、按原应用操作身份执行受控上游更新并逐目标读回。不可变图列与可变应用列同表不扩大更新权。失败/unknown 保留原 binding/操作 ID/条件和成功目标事实；重启扫描全部 current plans，不只读一仓。应用全部 readback_ok 且 recovery none/recovered 后才能恢复相应门禁。上游具体换图传输与窄补丁的既有未采用决定不由合表替代，设计文档需继续准确标注其未运行状态。

结果：原始 observation 先追加；证据不足等待；最终 admission 仅追加一次；候选正式提交生成真实 Candidate 版本；初审追加、组合选择固定成员、验证记录绑定组合及实际上下文；最后才能更新相应跨仓条件与轮次结论。旧轮次/旧 Attempt/旧 generation 观察保留但不推进当前状态。任何会改变“当前性”的指针/选择/采纳都通过同轮锁和整集合 revision CAS，不靠对象名字或时间近似匹配。

进程重启重建责任时读取 rounds+全部 current plans+tasks/attempts+reservations+operations+最新有型记录，旧操作按旧 ID 核查。租约更新只转移本地核查责任；新业务执行必须新 Attempt、新外部执行路径，且旧 Attempt 先满足替补条件。代码验证完成或上游 completed 不能直接关闭 Issue 或把 PR 标为合并。所有归属与决定历史保留，外部附件清理追加可用性状态。

## B06、B07 和其他批次必须提供的键

B06 合表后的物理设计须给出下列真实可引用关系，不能只说视图兼容：Issue 的 `(project_id,issue_id)`；主 ChangeSet 的 `(project_id,issue_id,changeset_id)`；固定工作仓库范围的 `(issue_id,repository_id)`。如果主 ChangeSet 内聚到 issues，则在 issues 给 `(project_id,id,main_changeset_id)` 普通 UNIQUE，本方案直接 FK 到该键。如果范围和内容范围合成子型行，必须给能限定 `work_scope` 的判别列及普通复合唯一键，所有上述 FK 带常量子型，不能仅引用“也可能是内容范围”的裸二元键。

B07 必须保留 leader 房间的 `(project_id,issue_id,repository_id,room_link_id)` 普通 UNIQUE 和 main/leader 互斥 CHECK；若房间内聚到别的权威表，需提供等价的真实有型 FK 目标，不能降成不透明文本。B07 Leader 投影只按 Issue/仓库读真实 repository_issues，不在选仓、建计划或读取时造工作事项。

B05 保持预算/时限权威，执行容量不等于模型消费账本；B09 保持可信调用上下文和授权权威。DurableWork 沿用已采用机制，本次不新增共享任务表。B11 最终候选/组合身份由这里落地后，B06/B07/ChangeSet 查询直接读取这些真实业务对象，不额外新建同名候选/组合权威。

## 需要同步的文档与验证断言

同步 `docs/api-database/b10.md` 的表节、PlanRound/Delegate/Observe/Reclaim 签名说明、锁序和删并表；同步 `b11.md` 的逐目标应用字段、完整集合读取、轮次/停止历史、Candidate/Combination 真实身份和验收场景；同步 B06/B07 的真实 FK 目标及投影查询；同步 foundations、目录计数和渲染手册。现行专题只追加本次合表采用说明并改准确的物理指向，不把过去候选或上游运行结论改成已验收。

`graph-loop-design.md` 需要说明当前计划是多仓 current rows 集合、plan_set_revision 与完整 CAS；`changeset-structure.md` 需要说明 Candidate/Combination 版本的真实稳定身份与受约束成员快照及执行映射不再重复；`backend-first-batch-persistence.md` 保留 RepositoryIssue 首次委派语义并链接新物理方案；`execution-integration-gates.md` 补证据记录与替补/旁路/多仓恢复断言；ADR 0015/0017 如补充只解释结构实现，不替换其已经采用的职责边界。

文档验证必须覆盖：20 个原名各有且仅有本方案去向、11 个物理表没有隐藏新表；跨 Issue 同仓 FK 拒绝；房间空值不能绕过归属；同轮两仓完整集合；一仓切换/另一仓采纳交错 CAS 拒绝；不可变 payload 更新拒绝且 application CAS 允许；Worker/最后槽位竞争仅一方提交；leaked 和 unknown 排他；终态但无撤销证据的旧 Attempt 阻止替补；may_have_sent 丢响应沿旧 ID 读回；逐种撤销及逐资源回收；candidate 未接受历史保留；同 Candidate 进多组合、同轮多次选组合；封存组合成员不可变；needs_recheck 不落最终 admission；旧结果 reject 可保留来源 FK；round closed/cancelled 不复活；停止不清零 B05 消耗。当前阶段只做文档与关系审查，不报告这些运行断言已经通过。
