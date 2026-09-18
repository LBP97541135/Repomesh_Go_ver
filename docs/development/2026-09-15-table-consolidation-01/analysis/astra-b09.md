# B09 物理合表建议：13 张降为 6 张

2026-09-15，Astra 只读分析。本文是交给文档作者的设计稿；未修改产品、现行专题或迁移，未运行服务、数据库或真实模型调用。已有 36 张表的字段和约束不变。新布局属于本次物理提案，不能写成已落库、已运行或已采用全部 G1/G2 协议。

## 1. 结论与 13→6 映射

建议新增六张 `repomesh_messaging` 表。合并的依据是同一消息的提交事实、同一处理入口的单项请求与当前授权、同一问题的唯一答案、同一控制结果中的目标解释，以及具有同样外发恢复纪律的两种执行记录。不同业务 ID 仍存在；不能把六张物理表等同于六种逻辑记录。

| 原表 | 新位置 | 独立逻辑身份如何保留 |
| --- | --- | --- |
| message_submissions | conversation_messages 的用户提交字段组 | `(project_id, conversation_id, actor, submission_entry, submission_id)` 唯一；不同于 message id |
| conversation_messages | conversation_messages | `id` 仍是 messageId；序号与正文事实不变 |
| conversation_message_counters | B06 新 conversations 的 `last_message_sequence`、`message_sequence_updated_at` | 仍按 conversation 身份定位计数；不增加 B09 表 |
| message_processing_entries | message_processing_entries | `entry_id` 与 `(message_id, handler)` 保留 |
| logical_work_requests | message_processing_entries 的根入口行字段组 | 独立 `logical_request_id` 唯一；答复入口只引用根请求，不能给答复再分配工作请求 |
| clarifications | clarifications | `id=clarificationId` 保留 |
| clarification_answers | clarifications 的唯一答案字段组 | 原 Answer 的主键本来就是 clarification_id；按问题 ID 定位答案，answer_message_id 另有唯一约束 |
| target_resolutions | control_operations 的 decide_message_target 成功行 | 独立 resolution_id 唯一；logical_request_id 的最终解释部分唯一索引保留 |
| control_operations | control_operations | command_slot_id 仍是独立操作键；同槽位动作／输入冲突规则不变 |
| manager_sessions | manager_sessions | 原代次、当前绑定、Issue 和 room 复合归属全部保留 |
| manager_call_authorizations | message_processing_entries 的当前授权字段组 | 独立 authorization_id 只表示当前有效授权；过期或替换后旧凭据不能重新有效 |
| message_deliveries | manager_executions，kind=message_delivery | 独立 delivery id、message_id 唯一、external_operation_id 不变 |
| manager_calls | manager_executions，kind=model_call | 独立 call id、请求／purpose／attempt_generation 唯一、配置闭包与出站证据不变 |

不能将授权直接压成 manager_sessions 的一份字段。会话可同时包含多个待处理请求，一条请求等待澄清时，其他讨论还应继续。把授权放会话单槽会额外施加语义串行限制；把多份授权塞 JSON 又失去明确键和约束。每个处理入口当前只允许一个受租约保护的步骤，授权放在该行与这项基数一致。

这里没有删除已采用消息内部设计的逻辑记录。原目录计数所称九项中，消息计数只是会话上的字段；八项消息／请求／问题／结果逻辑身份都通过上述不同键定位。原后端 §3 还列有 DurableWork / Claim、DeliveryObservation，这两项也继续由 processing / executions 的领取和观察字段承担，不因物理压缩消失。

## 2. 全表共同规则

- 以下 ID 用 text；修订、序号和代次用 bigint；时间用 timestamptz。复合外键均携带 project_id、conversation_id，需要时携带 logical_request_id、actor。不能仅凭全局 id 唯一省略归属核验。跨表 FK 默认 RESTRICT，正文清理不级联删除身份行。
- 未删除内容、不可变输入和原回执列，与可更新状态／租约／观察列分别规定更新权限。不能因为在同一行就允许一起覆写。
- 必须依靠 CHECK、唯一约束、复合 FK 及明确的延迟约束触发器保证提交后的完整形态。跨行语义不能冒充单行 CHECK。公开提交没有可见半成品。
- JSON 仅用于已有封闭 schema 的候选、非秘密配置快照、证据／用量摘要和不可变 API 回执。ID、类型、当前状态、修订、唯一性、授权边界、控制决定与清理标志全部使用有类型的列。JSON 必须有版本和权威来源。
- removed 后清除确切输入、规范化输入、正文、派生公开文本、候选标签、含原文的结果／证据与所有可还原正文的副本。保留作用域、永久业务键、必要结果归属和权限范围。无权先 404；当前有权查已清理内容再 410；410 不比较新正文，不允许旧键复活。

## 3. conversation_messages

### 字段

保留原消息列 `id, project_id, conversation_id, committed_sequence, author_kind, actor, service_binding_id, content, reply_to_clarification_id, clarification_id, created_at, removed_at`。`content` 改为可空，只有墓碑可空；存活正文非全空白且不超过 20000 Unicode 标量。清理后的空值不能伪装成空消息。

加入用户提交字段 `submission_id text NULL, submission_entry text NULL, submission_schema_version integer NULL, submission_exact_input bytea NULL, submission_canonical_input bytea NULL, submitted_reply_revision bigint NULL`。完整成功回执由这些不可变列、message id、conversation id、committed_sequence、created_at 重建；不另存会变化的问题状态。普通消息的 replyTo 固定 null，答复回执的 answeredRevision 取 `submitted_reply_revision`，永远不是问题的当前修订。比较器版本保留用于原输入判等。

`submission_exact_input` 包括原正文和原引用信封；`submission_canonical_input` 只按已采用比较器处理 JSON 属性顺序等规则，正文不 trim、不做 Unicode 或换行归一化。身份、输入与 message 一次插入提交，不需要独立可见的提交占位。

### 键和约束

1. PK(id)，UNIQUE(project_id, conversation_id, id)，UNIQUE(project_id, conversation_id, committed_sequence)，CHECK committed_sequence > 0。
2. 部分唯一 `(project_id, conversation_id, actor, submission_entry, submission_id) WHERE submission_id IS NOT NULL`。客户端 UUID 是作用域内幂等键，不能增加 submission_id 全局唯一来缩小已采用键空间。
3. 用户行要求 actor、submission_id、submission_entry、submission_schema_version 非空且 service_binding_id 为空。entry 仅 `conversation_message` 或 `conversation_create_first_message`。Manager 行要求 service_binding_id 非空且 actor 为空；system 行由可信服务入口构造。非用户行提交字段全部为空。
4. 存活用户行 exact/canonical/content 非空；用户墓碑 exact/canonical/content 全空，但提交 ID、schema version、actor、entry、序号、时间和必要 reply 身份保留。Manager/system 行仍以同一个 removed_at 清除正文。
5. `reply_to_clarification_id` 与 `submitted_reply_revision` 同时空或同时非空；非空只允许用户消息。`clarification_id` 仅允许可信问题消息，不能由浏览器传入。
6. `(project_id, conversation_id, service_binding_id)` 引用 sessions 同域唯一键。问题与答案复合引用见下一节；循环 FK 设可延迟，在提交时检查完整关系。

### 写入和清理

SubmitMessage 与会话首条消息事务写用户消息，受控服务入口写 Manager/system 消息。处理器不能更新正文、原输入或回执字段；正文清理用受限路径统一置墓碑。已有消息与提交本来一次提交、一对一，不存在独立于 message 而需要提前成功的 submission 生命周期。同一行内可保留原操作身份而清空消息内容，无需两张表。

## 4. message_processing_entries

此表有稳定入口身份和可选的根请求身份；它不是通用工作表。根消息与带 replyTo 的答复都登记入口，答复入口关联旧请求。服务消息如需处理责任，也保留独立入口，但不能因是消息就自动分配用户工作请求。

### 字段与两种定位

所有行保留 `entry_id, project_id, conversation_id, message_id, handler, work_state, claim_generation, lease_owner, lease_until, next_run_at, blocked_reason, created_at, updated_at`。work_state 使用原 `ready/claimed/done/blocked/retry_wait`。handler 首期 interpret_message，不预铺未来多来源框架。

根入口在首次可信处理时写入一次：`logical_request_id text NULL, root_source_message_id text NULL, source_revision bigint NULL, source_unit text NULL, request_actor text NULL, request_state text NULL, request_revision bigint NULL, current_question_id text NULL, resolution_id text NULL, request_created_at timestamptz NULL`。这些列为空时表示入口尚未分配请求；根字段一旦分配，ID、source、actor 与会话不可重绑。request_state 沿用 pending/awaiting_clarification/answer_pending/resolved/needs_decomposition/invalidated。

答复入口增加 `target_logical_request_id text NULL`，根据问题查得，引用根入口的 logical_request_id。它自己的 logical_request_id 必须为空。`reply_to_clarification_id` 从对应 message 获取，不由客户端提交 target request。每个根 request 的处理入口就是所在行 entry_id；逻辑投影不需要再复制 processing_entry_id。

约束要求：根字段全空或基础身份字段全非空；有 logical_request_id 时 source_unit=whole_message_single_request、root_source_message_id=message_id 且 target_logical_request_id 为空。带 replyTo 的用户消息入口 target_logical_request_id 非空，根请求字段为空，归属与 question/message 的原请求一致。此跨行规则用延迟约束触发器验证。UNIQUE(message_id,handler)、UNIQUE(logical_request_id)、UNIQUE(project_id,conversation_id,logical_request_id) 保留。

因此，按 entry_id 找领取／租约；按 logical_request_id 找根请求；按 message_id+handler 找某条答复处理入口；按 target_logical_request_id 找该请求后续答复入口。不会把每个答复伪造为新请求，也不会遗漏答复自身的处理身份。

### 当前控制步骤与授权有限列

同一入口同时只有一个当前领取步骤，添加：

- `current_command_slot_id text NULL`，一项决策步骤的稳定 ID，换领取代次不换槽位。前一步已完成且请求／问题修订已推进，或旧步骤已因明确的上下文变更被失效后，才可为下一合法步骤分配新槽位；领取超时和响应丢失本身不构成换槽位理由。
- `authorization_id text NULL, authorized_session_id text NULL, authenticated_service_ref text NULL, authorization_generation bigint NULL, authorization_expires_at timestamptz NULL, authorization_revoked_at timestamptz NULL`。
- `authorized_input_kind text NULL, authorized_source_message_id text NULL, authorized_source_revision bigint NULL, accepted_answer_id text NULL, expected_request_revision bigint NULL, expected_question_revision bigint NULL, allowed_actions text[] NULL`。

授权的 project/conversation/work/claim/lease 从同行读取，actor/request 从根请求读取，Manager epoch 从 authorized_session_id 引用的不可变 session 代次读取。accepted_answer_id 沿原 Answer 身份，就是 clarification_id，不默默改成 answer_message_id。sourceVersions 的 root/question/answer 所需修订均用已有源引用与上述有类型列重建；需要额外实际输入修订时加明确列，不能改成无约束上下文 JSON。

授权字段全空或完整；input_kind=root_message 时 accepted_answer_id、expected_question_revision 为空且源是根消息；clarification_answer 时 accepted_answer_id、expected_question_revision 非空且源为当前唯一答案。expected_request_revision>0，authorization_generation=claim_generation，expires_at 不晚于本次 lease_until。allowed_actions 必须是本步骤允许的固定动作子集且无重复，不能为空。原结果查询是只读能力，不通过查询产生新的 control row 或控制许可。

每次续领或步骤变化签发新的当前授权 ID，新 ID 不复用；撤销将当前授权置 revoked。领取代次变化时同事务替换或清空旧授权字段组，不能让旧授权继续匹配新领取者。相同决策步骤续领必须沿用 slot、input branch、source/answer 身份与 allowed_actions；上下文版本变化须先使旧步骤失效，再按请求当前事实建立合法新步骤。已过期旧授权可以不作为独立行永久保存，因为正式审计由已提交 control row 和 executions 中的当次上下文证据持有。旧授权 ID 查不到当前授权即失效。不能让 executions 对这个可替换 ID 建 FK；其保存 issued_authorization_id 及实际使用的有类型上下文快照，作为事实证据而不是可再次使用的令牌。

验证不能只验签。服务调用查当前 entry、session 和根请求，核未撤销授权、数据库时间、lease owner/generation、当前 session 代次、request/question revision、答案身份和动作白名单。租约更新不自动延长已签授权过期时间，需要重新签发。

### 工作状态与逻辑状态分离

work_state 表示当前处理责任，request_state 表示解释事实。请求 awaiting_clarification 可没有正在领取的模型工作，不能占住会话传输队列。保存答复使原 request 变 answer_pending，创建答复入口；根请求不会变成第二个入口。权限未知改变 work_state 为 blocked/retry_wait，不把 question 标 invalidated，不把 answer_saved 改回 open。

这次合并只采用当前 whole_message_single_request 基数。未来多来源拆分协议未采用，不为它现在预建表；未来如一入口确需多个独立请求，应新增明确来源单元模型和迁移，不往 JSON 放请求列表。

## 5. clarifications

保留原问题列并补足复合作用域 `id, project_id, conversation_id, logical_request_id, request_actor, source_message_id, question_message_id, revision, state, candidates, superseded_by, invalidation_reason, created_at, updated_at, removed_at`。

答案字段组只有 `answer_message_id text NULL, answered_revision bigint NULL, answered_at timestamptz NULL`。Answer 的 submission_id、actor 从不可变 answer_message 投影取得，避免重复权威；request_actor 与根请求复合 FK 约束其原操作者。增加消息同域含 actor 的可引用唯一键，将 answer_message_id 连同 project/conversation/request_actor 指向用户消息。同事务触发器要求该消息 reply_to_clarification_id=id、submitted_reply_revision=answered_revision、created_at=answered_at。对外 Answer 身份依旧是 clarification_id。

约束：PK(id)；UNIQUE(project_id,conversation_id,id)；UNIQUE(project_id,conversation_id,logical_request_id,id)；`UNIQUE(answer_message_id) WHERE answer_message_id IS NOT NULL`；答案三列全空或全非空。一个问题仅有这一组，原 clarification_id 唯一答复约束由同行槽位直接保证。问题的 source 与 root、question message 的 clarification_id 与本行 id 要作复合归属校验。

`UNIQUE(logical_request_id) WHERE state IN ('open','answer_saved')` 继续存在。open 必须无答案；answer_saved/resolved 必须有答案；superseded/invalidated 可以有或无答案。answered_revision 是提交前 revision，答复保存后 revision 增加；后续替换或解决不得重写答案身份和已消费 revision。

替换只能在原 request 锁下新建新 ID 问题，使原问题 superseded，设置 superseded_by，更新 current_question_id；后继必须同请求，不得自身或回指祖先。terminal 问题不得 reopen。resolved 状态要求同请求唯一 resolution 且 input_kind=clarification_answer、accepted_answer_id=id；直接判断没有虚构的 clarification 或 Answer。

问题正文仍只在 question_message；创建问题的延迟一致性检查要求该消息为可信问题消息且正文不超过2000 Unicode 标量。candidates 只为已有候选 schema，最多20且按 issueId 去重。清理问／答正文不删除问题和答案关联，清除 candidates 中可恢复公开文本和派生内容并置 removed_at。历史答案不会因内容清理解除唯一约束。后续读取先核内容范围和清理状态。

## 6. control_operations

保留 `command_slot_id PK, action, schema_version, canonical_input, result, committed_at, removed_at`；补足 `project_id, conversation_id, logical_request_id, processing_entry_id` 与执行时 `issued_authorization_id, manager_session_id, claim_generation, input_kind, source_message_id, source_revision, accepted_answer_id, expected_request_revision, expected_question_revision` 的有类型上下文。action 首期三个有副作用动作 propose_clarification/decide_message_target/replace_clarification。get_message_control_result 是读取该表的动作，不凭空创建一次“查询成功”占槽记录。原目录 action 枚举出现 get，须解释此物理澄清，不改变已有查询工具行为。

decide 成功行附加目标字段 `resolution_id text, outcome text, issue_id text NULL, resolution_request_revision bigint, resolution_created_at timestamptz`。其中 source/input/acceptedAnswer 复用该控制行不可变上下文，不再复制一份。公开处理消息 ID 使用 `public_message_id text NULL` 并建立同会话 FK，明确与同事务公开消息的关系。

约束包括：

- UNIQUE(resolution_id)，`UNIQUE(logical_request_id) WHERE resolution_id IS NOT NULL`，必要的 `(project_id,conversation_id,logical_request_id,resolution_id)` 可引用唯一键。
- action=decide_message_target 当且仅当 resolution_id/outcome/resolution_request_revision/resolution_created_at 全非空。其他动作这些列全空。outcome=issue_target 要求 issue_id 非空；no_work 要求 issue_id 空。同项目 Issue FK，当前关联和读权在事务中核。
- root_message 必须 source 为原根消息、accepted_answer_id 为空、此前没有问题历史；clarification_answer 必须固定当前唯一答案和对应 revision。跨行条件由持锁校验＋延迟约束覆盖，不写成错误的单行 CHECK。
- 成功行 canonical_input/result/committed_at 完整；墓碑清空输入和含正文结果，保留 command_slot_id、action、schema_version、scope、resolution_id、必要目标归属和 committed_at。resolution 的唯一性部分索引不按 removed_at 过滤，所以清理后不能再插第二个最终解释。
- resolution 指针与根请求 resolved 状态相互一致，使用延迟约束触发器；请求先在锁下 CAS，再提交控制行及公开记录。即使两个错误签发的不同槽位并发，request CAS 和部分唯一约束仍阻止两个终局。

决策步骤的稳定 slot 先保存在 processing row。control_operations 只保存原子提交的成功结果或墓碑；不能提前提交一个“已受理可能生效”的控制占位。重放先读已提交结果并核当前可读范围；同槽位同动作同输入返回原结果，不根据最新问题状态重新判断。旧授权不能写新结果，但可以通过当前可信只读入口查询原槽位。

TargetResolution 是 decide 行上的有类型业务子记录，不从 result JSON 提取权威目标。其 resolution_id 与 command_slot_id 不相等也不混用；下游仍引用 resolution_id＋logical_request_id＋依据版本，并另核执行许可。

最终目标的首项后续核验责任也由这条 decide 行持有有限列：`continuation_kind, continuation_state, continuation_claim_generation, continuation_lease_owner, continuation_lease_until, continuation_next_run_at, continuation_blocked_reason`。仅 outcome=issue_target 时创建完整字段组，kind 固定 verify_issue_target，causeId=resolution_id、targetId=issue_id；resolution 唯一性同时保证 `(causeId,kind,targetId)` 唯一。no_work 与非 decide 行这一组全空。状态沿 ready/claimed/done/blocked/retry_wait，由目标核验扫描器维护；控制结果与 resolution 列仍不可变。下游尚无实现时 blocked/INTEGRATION_NOT_AVAILABLE，不写执行成功。目标核验只确认后续接受条件，真正派工由 B10 域操作去重和核权，不能把这组待办变成自动派工授权。

## 7. manager_sessions

保留原全部字段、复合归属、当前代次部分唯一索引与状态。补充用于被复合 FK 引用的 UNIQUE(project_id,conversation_id,id)。每会话 `(project_id,conversation_id,manager_session_epoch)` 唯一，`(project_id,conversation_id) WHERE superseded_at IS NULL` 唯一。

无 Issue 普通会话 issue_id/room_link_id 均可空，运行绑定由 manager_instance_ref、transport_session_ref、state 持有，不等待 B07 room。room_link_id 非空要求 issue_id 非空，并按 `(project_id,issue_id,conversation_id,room_link_id)` 引用 B07 主房间唯一键。不能引用 Leader room。

Issue 上下文或 room 引用变化必须新代次，旧行置 superseded_at，旧授权失效。不能原地重绑一代 session。旧会话上的已可能外发记录仍保存原 session 并继续核查；会话切换不证明旧外部执行取消。探测 unknown 不删除旧运行绑定，不拿 B07 runtime_generation 替换 manager_session_epoch。

调用授权生命周期短于 session，故没有合在本表；运行权限与调用权限也不能由一个 ready 值代替。

## 8. manager_executions

此表保存两种外发事实，每一行只表示一种事实。不能一条消息只建一行并让该行同时充当 delivery 和 model_call。投递与实际模型调用数量不必一对一，投递成功也不是配置消费证据。

### 共同列

`id text PK, kind text NOT NULL, project_id, conversation_id, content_scope_revision bigint NOT NULL, manager_session_id text NULL, external_operation_id text NULL, claim_generation bigint NOT NULL, lease_owner text NULL, lease_until timestamptz NULL, next_run_at timestamptz NULL, may_have_sent_at timestamptz NULL, observation_revision bigint NOT NULL DEFAULT 0, last_observation_source text NULL, last_observed_state text NULL, last_evidence jsonb NULL, last_observed_at timestamptz NULL, created_at, updated_at, removed_at`。kind 只允许 message_delivery/model_call。

`UNIQUE(external_operation_id) WHERE external_operation_id IS NOT NULL`，外部 ID 由 RepoMesh 生成并带适配作用域，不拿领取代次或上游局部编号充当全局键。UNIQUE(project_id,conversation_id,id,kind) 供跨对象按确切 kind 引用。

行持有出站责任、租约与 latest observation。`may_have_sent_at` 只能由空变非空。状态转移和已外发字段不可变要由受限写路径及约束触发器核验。低 observation_revision、旧 claim generation 的回写不覆盖新事实；核查接管须使用新领取者身份。

### delivery 分支

`message_id text, delivery_state text` 必填，model_call 专有列全空。message_id 在 kind=message_delivery 下唯一。delivery_state 仅 prepared/may_have_sent/accepted_by_transport/observed_at_manager/reconcile_required/failed_before_send。external_operation_id 从登记投递责任起非空，重试不改。

消息保存不能等 runtime ready，所以允许 prepared 且未外发的 delivery 行暂未填 manager_session_id。Ensure 就绪后、发送前在短事务固定该目标；这修正原目录“提交即非空目标”与“先本地保存后准备会话”的物理矛盾。may_have_sent_at 非空或状态已经证实外发时，session 必填且永不改绑。尚未外发的记录重绑也必须在当前代次与配置上下文核验后完成；不能把旧调用恢复写成换会话重试。

从 prepared 经确认发送前失败可到 failed_before_send；确认仍未发送后可以恢复原记录，目标与幂等身份遵守上段规则。发送前先写 may_have_sent；随后只凭证据到 accepted_by_transport 或 observed_at_manager。未知进入 reconcile_required 并保留已证实阶段的时间／证据，探测失败只把 last_observed_state 标 unknown。不得把 unknown 当 failed_before_send。state 与最近观察不是一个值。

### model_call 分支

`logical_request_id, purpose, attempt_generation, configuration_revision NULL, model_profile_id, model_profile_version, provider_revision, row_id, secret_version_id, execution_profile_version NULL, resolved_config, resolved_digest, call_state, usage NULL`。message_id/delivery_state 为空。call_state 仅 prepared/may_have_sent/observed/reconcile_required/failed_before_send/blocked。purpose 为 message_interpretation/manager_turn。

`UNIQUE(logical_request_id,purpose,attempt_generation) WHERE kind='model_call'`。项目/会话/request 同域 FK；model/secret/profile/config identity 引用其真实现有键，不更改 B01-B04 表。session 及完整固定配置在实际出站前必须非空。可用性缺失留 blocked；不能换默认模型或秘密。

Issue 模型调用另有 typed issue_id，非空时 configuration_revision 必须是该 Issue 的 initialConfigurationRevision。普通讨论 issue_id/configuration_revision 可空，但自身 model/secret/version 闭包照样固定。resolved_config 只有非秘密版本和参数，不保存正文／完整 prompt；重启沿原闭包，不查 latest。首次可恢复请求选择配置后，以同请求已有最早固定的闭包为准，后续 purpose/attempt 通过请求锁核对相同固定版本；若跨步骤确需不同 Issue 上下文，必须建立明确新请求，不能原地把旧请求闭包换掉。

附加实际执行时的授权证据列 `issued_authorization_id, processing_entry_id, executed_claim_generation, executed_command_slot_id, executed_input_kind, executed_source_message_id, executed_source_revision, executed_accepted_answer_id, executed_request_revision, executed_question_revision`。这些是对该次调用的不可变来源证明，不是当前授权表替代品；只凭其存在不能重新放行。此分支的 source/answer 约束与授权分支一致。

外部操作进入 may_have_sent 后，目标 session、source/answer、配置闭包、secret identity 全部不可变。逻辑 attempt_generation 递增只能发生在上一尝试有证据未发或已按协议终结且本次策略允许时；前次 may_have_sent 未知只核查原 external_operation_id。不能以“模型重试”为名重新计作新用户请求或逃过预算。

### 费用归属和清理范围

usage 仅 model_call 分支允许非空，使用 call id、project_id、logical_request_id、可选 issue_id 与固定 policy/config 版本定位费用事实；delivery 分支 usage 和全部模型版本字段必须为空。两种分支不会共用一笔模型费用身份。Manager 调用仍须核项目运行预算，不能拿 B05 actor_model_test 额度替代；项目运行预算 scope 未采用或不可用就 blocked，不能补造额度。

发送和核查根据 content_scope_revision 重新读取 B06 repository_scopes。delivery 从 message 的会话范围确定清理对象；model_call 从根请求／实际来源消息／答案及可选 Issue 固定内容范围确定清理对象。行的 project/conversation/issue/source 归属不会因当前会话后来切换 Issue 而改变。删除一条源消息时，清理引用它的投递证据和调用证据中的正文副本，保留 call 的版本身份、用量数值与归属，以及 delivery 的外部身份／接收证据。不能按会话一次硬删两种全部记录，也不能因为 model_call 还需计费就保留 prompt；必须保留的非正文审计与待核责任独立于正文列。所有跨对象范围引用都指向 B06 新 repository_scopes 的相应所有者，不能沿用已取消的三张 scope 表名。

### 为什么能共表

两种行都由 coordinator/Manager 适配层持有外发责任，生命周期都从本地准备到可能已发、证据观察和核查，再保留最小外发证据。共同的不可变外部身份、发送前事实、租约围栏、迟到观察和清理规则相同。分支状态使用不同列与 CHECK，不强行合成一个宽泛 state；各自输入关系与唯一键依旧独立。与 B05 用户模型测试仍不合表，测试额度／主体／协议不同。

## 9. 锁序、事务与恢复

固定总锁序与 B06/B07 对齐为 principal 的 binding/session/account → project 及所需 connection → conversations 按 id 排序 → 必要 Issue 按 id 排序 → message_processing_entries 按 entry_id 排序（包含根请求和此次答复／服务入口）→ clarifications 按 id 排序 → manager_sessions 按 id 排序 → control_operations 按 slot 排序 → manager_executions 按 id 排序。不需要的对象层可跳过；普通消息没有目标 Issue 写锁时直接进入处理入口层。消息/操作最终插入在已持有的会话与请求范围完成。计数并入 conversation 后，不再另锁 counter。

同类 processing 行先收齐根入口和当前入口 IDs，再按 entry_id 排序；不能一个路径先锁答复、另一路先锁根。声明这一映射替代原“logicalRequest 然后其他 processing result”的含糊说法。会话行已经按原协议是所有消息写路径共同锁，移入 counter 没有新造消息级串行瓶颈。

扫描器不能持有 executions 或答复 entry 行锁再倒向 conversation/request。可先无锁选候选 ID，再开启短事务按上述顺序领取并 CAS next_run/state/generation。外部核权观察和模型/传输调用在事务外。准备观察器若只更新一张 session/execution 行无需拿上层锁，但不得在这个事务中继续反向取得业务锁；凡核准控制或切换上下文必须按完整顺序。

消息提交：先核当前身份／可披露性并查原 scoped submission；原成功优先于新问题修订，墓碑优先于输入比较。新请求预核完整内容范围，依固定序锁后重核访问 epoch/range version，再插入合并 message 行、递增会话计数。答复还在 question 同行写唯一答案并使 question=answer_saved、根 request=answer_pending，各升 revision；同事务保存处理入口、delivery 行及必要 continuation。并发失败整笔回滚，不留孤立消息。只提交后返回 201。

控制提交：先核原 slot 成功结果，再核当前授权；按固定序锁后重核 source/question/answer 与 request CAS。一次提交 control row（包括可选 resolution）、新问题/旧问题变化、公开消息及后续责任。slot 的 action/input 一旦成功不可覆盖。最终解释的后续核验责任固定由 §6 的 decide 行 continuation 字段组持有，唯一键语义为 `(resolution_id, verify_issue_target, issue_id)`；目标核验扫描器恢复该行，handler 未接入时 blocked/INTEGRATION_NOT_AVAILABLE，不写执行成功。

传输按同会话 committed_sequence。前序 may-have-sent 未知时，后续 delivery 等待其核查；等待语义澄清不会阻塞同会话后续独立消息。delivery、model_call、control result 的状态分别读取，本地 message receipt 不随其变化。

崩溃后按原 entry/slot/delivery/call 身份扫描恢复。COMMIT 响应丢失查原提交，404 仍可能晚提交；不得换 key。收到模型输出但 control 未提交，重新领取后用原 slot 与依据；旧租约/session 的新写入返回 STALE_PROCESSING_CONTEXT。已提交 control 只查原结果，不重做判断。清理／替换使旧控制许可失效，但已可能外发的记录仍须核查，不声称外部已取消。

清理先取得相同范围锁，撤销当前授权并升业务 revision；清理所有内容副本，保持 message/submission、answer、resolution、slot 和 external ID 唯一性。对外发未知先核查并保留最小审计及待核责任；不能直接删 execution 行丢掉可能外发事实。秘密正文可销毁，仍被引用的版本身份不级联删除。

## 10. B06 的额外衔接

已向 B06 分析者发送 counter 两列并获得同意。B06 conversations 新表增加 `last_message_sequence bigint NOT NULL DEFAULT 0 CHECK >=0`、`message_sequence_updated_at timestamptz`。分配时持有 conversation 锁直到 commit；分页首屏 high-water 读取该值，仍允许序号间隙。

B09 §2.7 独立会话创建不能靠首消息保存幂等，因为空会话也允许创建。建议 B06 conversations 有以下有限可选字段：standalone_creation_id、standalone_creation_actor、standalone_creation_schema_version、standalone_creation_exact_input、standalone_creation_canonical_input、standalone_creation_receipt、standalone_creation_committed_at、standalone_creation_removed_at、standalone_first_message_id。UNIQUE(project_id,standalone_creation_actor,standalone_creation_id) WHERE standalone_creation_id IS NOT NULL，entry 固定 conversation_create。

created_by_page_operation_id 非空且 standalone 组全空，或 page ID 空且 standalone 的 id/actor/schema/committed_at 完整，形成互斥创建来源。未来不同来源采用时再新增明确分支。本批 B06 页面创建新会话进入第一分支，B09 独立创建进入第二分支；既有会话不会改写初始创建归属。存活独立回执 exact/canonical/receipt 全非空，墓碑三项全空但 key/scope/first message 身份保留。

standalone_creation_receipt 是封闭版本的 §2.7 不可变 API 回执，只用来冻结当次 title/revision/messageId/sequence/time，不能放进状态机或子记录列表。清理该回执时清除所有标题正文副本；原会话当前标题不能拿来重建旧回执。首消息 ID 同项目同会话 FK，回执 ID/序号与该消息延迟一致性检查。firstMessage 未给时 ID/sequence 为 null，不造 message/entry/delivery，也不触发 Manager 准备。有首消息则与会话同事务提交，消息 entry=conversation_create_first_message，submission ID 从完整 creation scope 稳定派生，不能仅从裸 UUID 派生。

B07 room link 的既有复合键由该分析者确认保持；B09 sessions 继续原四列 FK。

## 11. 文档作者应增加的明确替代说明

1. `docs/api-database/b09.md` §3 全部物理表、§3.14 原“保留”理由、§4 锁序与恢复、§5 跨批表名引用及相关 API 内部存储名称同步改写。尤其删除原 counter“避免会话锁热点”、Answer“生命周期独立必须拆表”、LogicalWorkRequest“为未来多来源提前独立”的论证，它们不支持当前必须分表。
2. `docs/current/backend-message-clarification-design.md` 顶部和 §3 增加“2026-09-15 物理存储补充”：§2-§9 已采用身份、唯一性、状态和恢复语义不变；§3 是逻辑记录清单，新物理承载以 B09 本次布局为准。逐项列出 MessageSubmission/MessageProcessingEntry/LogicalWorkRequest/ClarificationAnswer/TargetResolution 对应行和键，并指出采用状态仍分开。
3. 同专题 §2.3 说明授权由 processing 当前步骤有类型列持有，不宣称独立授权表已采用；§5.1/§5.2 给出合表锁序与原子提交映射；§8 将原工作、原 slot、原 resolution 的扫描位置映射到新行。
4. `conversation-message-clarification-api-contract.md` 不改五端点和已采用响应 schema；若加说明只需顶部一句“物理存储补充不改变 API／原回执／清理语义”。`conversation-message-target-design.md` 如需维护入口也只加同性质引用。不要把静态设计审查写为真实验证。
5. B06 conversations 追加消息 counter 与独立创建回执组；B09 §2.7 指明该物理承载，空会话无消息也可按原键重放。跨批表总计只计 B06 conversations 一次。
6. `foundations.md`/总目录如有表数、独立记录假设或实体必须逐张建表的句子，同步为物理表与逻辑记录分开计数。不要修改 B01-B04 既有字段和约束。

## 12. 必须列入静态验收的案例

- 同一 scoped submission 重放只得到同一 message；相同 UUID 在不同合法作用域不会因全局 PK 误冲突；同键改 replyTo 409。
- 两个不同 key 并发答同一问题，只在 clarification 的答案槽保存一次，失败事务不留 message 或 delivery；清理答案正文后仍不能重答。
- 根消息与任意多次后继答复分别有 entry，但只找到一个原 logical_request_id；直接判断无 question/answer，答复判断不能跳过唯一 Answer。
- 不同 slot 并发提问与直接判断由 request CAS 阻止；不同 slot 的两个决定被 resolution 部分唯一约束拒绝；removed 结果仍占唯一键。
- 一条请求待澄清时同会话另一请求可获自己的授权；授权换代保留原 slot，旧 ID/代次拒绝新写，当前身份可查询原结果。
- 消息先保存但没有 session 时仍可原子提交 prepared delivery；实际发送前必须固定 ready 目标和授权；may_have_sent 后切 session 被拒绝。
- delivery 与 call 两行分别存在，transport accepted 不能填成 model observed；错误 kind 字段组和状态组合由 CHECK 拒绝。
- 未知外发不生成新 external ID、不换 secret；重启以原 call snapshot 证明 Issue 的固定配置，普通讨论后续独立请求才可选择新配置。
- 删正文清除合并行各副本仍保留 key、question answer、resolution 与外发核查；不同 endpoint 返回原回执或 410，不能局部漏正文。
- 独立空会话原键重放不创建第二会话且不唤醒 Manager；带首消息创建在一个事务中产生一个会话和一个首消息。

以上案例是待实现／待执行的验收设计。本文验证仅为文档与现有已采用约束静态核对。
