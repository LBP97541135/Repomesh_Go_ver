# 业务计划版本补模独立复核

2026-09-15。非主要设计者，只读复核 `/tmp/repomesh-consolidation-20260915/business-plan-versions.md`，对照 `astra-b10-b11.md`、`review-b10-b11.md`、当前B06新增issues字段以及ADR0003、Graph§5—6。未编辑仓库，未运行数据库或上游。

结论：新增一张business_plan_versions、总34张可以保留。首次候选轮次准备、同版本多许可历史、目标集合冻结、旧计划历史不改绑和removed恢复方向合理。以下三个P1与两个P2需要在现有34张内修复；不新增表，不增人工审批，不选定上游传输协议。

## BP-01 / P1：冻结激活目标被当成所有后续轮次的派工白名单

位置：补模§4第48行冻结目标不可增改，§5第62行要求Delegate核“当前版本目标关系包含该技术安排”；对照Graph§5同业务版本跨轮、技术安排修订独立。

可复现路径：业务版本V1以round R1、plan P1完成首次激活A1；在原范围/上限内OpenNextRound创建R2/P2。P2不在A1已经冻结的目标集合，Delegate必然拒绝。添加P2违反目标冻结；创建新业务版本违反同范围跨轮语义；每轮再做业务激活也不是原规则。

最小确定修复：区分业务版本的生效证据与同版本后续技术安排准入。

- 初次/变更业务版本的confirm_effective仍严格核该activation的冻结全集，不改。
- 同一effective业务版本内的PlanRound/OpenNextRound仍创建新技术修订并走原plan_revisions应用/真实读回。其plan_observation以明确分支保存business_plan_version_id、round绑定修订、完整plan_set_revision与本次技术应用身份；这类观察不强迫填写business_plan_activation_operation_id/target_record_id。
- Delegate核Issue当前effective版本、round当前版本/修订、完整当前技术计划集合、同版本下该计划的真实readback_ok和recovery none/recovered、当前权限及原上限。若该技术安排是在业务版本变更时复用的旧来源版本，另核本次activation的verify_unchanged关系；不能把所有后续新安排都要求存在于首次activation目标中。
- Attempt保存本次业务版本及其生效决定，同时保存实际技术计划/应用依据；生效决定不必列举未来轮次。

新增断言：V1/R1首次生效后，R2/P2在V1上限内应用读回成功可Delegate，且A1目标集合与版本V1不变。

## BP-02 / P1：激活排他只到版本，同Issue两个版本仍可同时改目标

位置：§3第40行“版本当前可只有一项未终结激活责任”，§6只描述事务中的Issue锁；§4允许部分成功/unknown跨事务持续。

可复现路径：同Issue候选V2和V3均获准。V2 begin提交后释放Issue锁，其某目标已应用、另一目标unknown；V3随后begin也通过自己的版本行约束，开始改同一个上游Project。事务中的Issue锁只使登记依次发生，不能维持两笔外部流程的互斥。V3可能覆盖V2的成功目标，旧有效版本V1的Delegate若只核自身有效指针也会漏过别的版本的部分应用。

最小确定修复：在business_plan_versions上增加Issue级激活占有条件。

- 明确activation_work_state枚举及未终结集合，例如pending/applying/blocked/reconciling/unknown；在这个集合建立 `UNIQUE(issue_id) WHERE activation_work_state IN (...)`。表不是旧36张，增加该索引不越界。
- begin在Issue锁下检查所有版本的未终结激活；同一原activation恢复仍通过原键，其他版本begin冲突或保持明确待核，不持第二个目标写权。
- unknown、权限暂不可确认、部分成功都保留占有。只有已经生效完成，或按现有外部恢复协议证明旧流程不再可能继续写的明确终结，才离开占有集合；不能仅把activation标cancelled或清理正文就释放。
- Delegate与技术应用在Issue锁下检查该Issue的激活占有及其冻结受影响目标集合。受影响上游Project持续停新派工，即使Issue的current_business_plan_version_id仍指旧版本。

Issue级排他是本轮有界方案，不需要新的跨目标锁表。它比仅同版本排他更保守，但不会删掉现行并发功能承诺。

新增断言：V2部分成功+unknown时V3不能取得激活占有，旧V1在受影响范围不能Delegate；原V2核查仍使用原operation。

## BP-03 / P1：首次自动许可unknown时没有已经存在的持久责任

位置：§2第21行责任cause固定为激活operation；§3第34行权限unknown“只保留现有责任待核”，第40行仅begin_activation创建work。

可复现路径：首次提案V1已落库，第一次自动许可读取权限/政策失败。此时没有authorized许可，也不能begin_activation，因此文中唯一work尚不存在。进程重启后没有稳定待核operation、领取代次和下一检查时间可恢复。B06当前issue_continue也不能直接补这个洞：其本批合法状态只有blocked/INTEGRATION_NOT_AVAILABLE和清理cancelled，没有本补模定义的许可工作状态与领取协议。

最小确定修复：把business_plan_versions现有work字段组明确为两阶段计划责任，不新增物理表，也不借用不具备状态的B06初始待办。

- 提案事务或首次自动评估事务即登记 `plan_work_id`、`plan_work_stage='permission'`、稳定cause proposal_operation_id、稳定pending_permission_operation_id、原输入/比较schema及现有generation/lease/next_run/blocked字段。permission阶段的active_activation_operation_id必须NULL。
- 明确blocked/unconfirmed可扫描且不写假rejected；重启、租约换代及权限恢复仍核原pending operation，COMMIT未知先查该operation的决定。
- authorized决定提交与进入可准备激活阶段同事务交接；begin才进入stage=activation并固定activation_operation_id和目标。确定拒绝可以终结本次责任，后来明确重新评估使用新逻辑operation，旧决定留存。
- 两分支CHECK允许permission阶段没有activation身份；activation阶段要求完整身份。BP-02的Issue激活排他只约束activation阶段，不把多个尚待分析的候选版本当已取得外部写权。

这只是把“未知保留责任”落到已经计数的新版本行，不要求新增业务API或人工等待。

新增断言：V1首次许可unknown后重启，可扫描原work并继续原pending operation，无activation目标、无外部写、无假拒绝。

## BP-04 / P2：允许新的激活，但单条技术修订只有一次应用身份

位置：§2第21行允许旧激活终结后再有新operation/work；§4第50行复用plan_revisions应用组。原astra-b10-b11 §3规定每plan只有一个application_id/external_operation_id和不可变规范输入，may_have_sent后不得换号。

可复现路径：旧激活曾把技术修订P应用成功；其后另一安排已经改变上游；现在再次激活并以P作为apply目标。P原application_id仍存在，不能把新写重新解释成原请求重试，也不能UPDATE成新application_id。verify_unchanged真实读回会失败，因为上游已改变。

最小确定修复：给目标构造写清唯一规则。

- 原技术修订被复用，且本次真实读回仍一致时，可以verify_unchanged，不建立第二次外部应用。
- 原操作unknown或未核清时只能继续该操作核查，不创建新技术修订绕过责任。
- 旧流程已经按现有协议核清，确实需要再次外部写相同图内容时，创建新的技术plan_revision身份，允许canonical_plan内容相同，生成其自己的新application身份，并在begin前作为本次apply目标固定。业务Plan Version无需改变，旧技术修订与应用历史不动。
- 不能将“图内容相同”当作相同外部逻辑操作，也不能因为新activation就释放未核旧操作。

新增断言：旧P已应用但上游后来变化时，新的合法写用P2技术修订；原P外部操作不被重发或覆盖。

## BP-05 / P2：decision的部分唯一键不能直接当激活操作FK，action还须约束

位置：§3第30/36/40行，§4第46行。当前只列 `UNIQUE(plan_operation_id) WHERE record_kind='business_plan_decision'`，并称后续activation_operation_id引用begin身份；该部分唯一索引不能成为PostgreSQL声明式FK目标。已有 `(id,project_id,issue_id,business_plan_version_id,record_kind)`普通唯一可以约束同子型，却不能区分evaluate_permission、begin_activation、confirm_effective三种不同来源。

最小确定修复，在新的execution_records上增加实际引用用的普通键/明确分支，不改变其他子型的round非空规则。

- 增加普通 `UNIQUE(id,project_id,issue_id,business_plan_version_id,record_kind,plan_action)`。如要用operation ID直接引用，再增加普通 `UNIQUE(plan_operation_id,project_id,issue_id,business_plan_version_id,plan_action)`；其他子型的NULL不妨碍该普通唯一成立。原全期operation去重索引可以保留。
- current_permission_record指同版本action=evaluate_permission；current_activation_record指begin_activation；effective_activation_record指confirm_effective。各本地action常量列/触发器不能都只检查record_kind。
- cause_permission_record明确指同版本evaluate_permission且决定authorized；用进一步普通唯一+判别列或明确约束触发器核authorized，不允许拒绝记录成为生效依据。
- business_plan_target与所有后续activation决定实际FK到同版本的begin记录/operation；不把任意confirm或另一版本的operation作为激活根。confirmed_readback数组仍由已有专用触发器展开核同activation/target，不能宣称数组元素有普通FK。
- 同版本重新许可不会改旧begin的cause_permission。恢复若需新许可，当前动作记录其实际采用的新permission记录，旧cause继续表示启动激活时的历史依据，不能覆盖旧输入/许可历史；当前许可未知则继续blocked。

新增断言：以confirm_effective冒充begin、以rejected评估充许可、跨版本activation、NULL绕过必需分支均被拒；schema可以实际建立所列FK。

## 已核对可以保留

- 首次Issue建立时当前业务版本指针NULL，B10后加反向FK；新版本/新round可先planned准备，Delegate才要求effective，首次准备循环已解开。B06指针/编号三列放新issues不动旧36表。
- 提案操作与版本身份分开；同版本多次evaluate_permission保留不同确定决定；同key重放不以当前许可覆盖原结果。
- 业务版本正文不可变，plan/Attempt独立记录实际来源，不能FK到round可变当前版本指针。这允许新业务版本复用旧技术安排而不改历史。
- begin目标全集和确认采用的观察集合有具体记录；部分成功不能effective，旧成功事实不回滚为“未发生”。上述BP-01只拆后续技术应用门禁，不放松首次/变更激活的全量核验。
- 清理保留原操作墓碑、身份与未知外发责任，有权410优先于输入比较、无权隐藏404；不把append-only当永久保存敏感正文。需按BP-02维持尚未核清的激活占有。
- 预算/时限及真实上游写法尚未采用时保持blocked，不把本补模视为选择上游replan/resume/窄补丁方案；当前YOLO仍无人工审批。

## 验证范围

本轮仅静态设计审查。五项修复不增加表，不改变34张计数，不要求更新旧36张或产品API。最终文档应把这些断言列为未来真实PG/失败恢复验证；当前不得报告运行通过。
