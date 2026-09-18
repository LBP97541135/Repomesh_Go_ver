# B09 六表方案独立交叉复核

2026-09-15，Astra。复核者不是B09主要设计者。只读分析，未修改仓库或执行数据库。对象为`astra-b09.md`最终六表稿。六表数量和合并方向可以保留，以下问题需在落盘文档中修正。行号以复核时临时稿为准。

## R09-01 / P1：跨域范围版本与配置版本类型错误

原位置：§2第1条“修订、序号和代次用bigint”；§8共同列148行`content_scope_revision bigint`；178行“根据content_scope_revision重新读取scope”。

证据：B06新conversations与issues的content_scope_revision均为text，repository_scopes只有owner、repo、来源，scope revision保存在owner行。现有0004/0005迁移中configuration revision、profile version、provider revision均为text。外部消息契约§1/§5.3把ID/revision视为不透明字符串。

最小修复：executions.content_scope_revision改text，并明确是当次会话范围观察修订。若出站同时使用Issue范围，增加`issue_content_scope_revision text NULL`，与issue_id成组；不把一个revision混用为两个owner的版本。按`project_id+conversation_id`读取Conversation owner与其repository_scopes；有Issue再按`project_id+issue_id`读Issue owner及scope，比较各自版本，变化后重核完整集合。保留原出站版本快照，不能为了让旧调用通过而原地替换已发送的依据。revision本身不是FK或可追溯到旧scope快照的钥匙。

共同类型说明改为“ID、不透明范围/配置/profile/provider版本用text；内部request/question单调计数可bigint，API编码保持不透明字符串；sequence/epoch/claim/observation计数用bigint”。不得用JS Number解析公开大序号或重写原expectedRevision回执。

## R09-02 / P1：模型调用列不足以形成旧表真实复合外键

原位置：§8 model_call分支164—168行，声称model/secret/profile/config引用真实键但缺provider_id、execution_profile_id及固定kind。

证据：`0004_projects.sql` profile_versions PK(kind,profile_id,version)，configuration_revisions PK(project_id,revision)；`0005_models.sql` model_snapshots PK(provider_id,provider_revision,row_id)，provider_revisions PK(provider_id,revision)，profile_links PK(profile_id,profile_version)。旧版本文本不是脱离owner的独立全局键。

最小修复仅加新executions列与约束，不改旧36表：

- `provider_id text`，FK(provider_id,provider_revision,row_id) → repomesh_models.model_snapshots同名PK。保留provider/profile/row/secret闭包一致性检查，不能各自存在即视为来自同一模型。
- `model_profile_kind`为固定model的受约束列或生成列，FK(kind,model_profile_id,model_profile_version) → profile_versions(kind,profile_id,version)。另可FK(model_profile_id,model_profile_version) → profile_links(profile_id,profile_version)，延迟检查所选link的provider/revision/row/owner/secret等于本行。
- `execution_profile_id text NULL`与execution_profile_version全空或全非空；固定execution kind与两列FK → profile_versions真实PK。普通讨论是否要求execution profile依既有候选，不在本轮强制新增业务门槛。
- FK(project_id,configuration_revision) → configuration_revisions(project_id,revision)。Issue模式的pin匹配可用延迟约束读取issues，或由B06在P9另行采用时新增到新Issue表的UNIQUE(project_id,id,initial_configuration_revision)供三列FK；P9未采用状态保留。
- secret_version_id至少FK → secrets.versions(version_id)。若持有四元组，则用现有已存在的(version_id,owner_kind,owner_id,purpose)唯一目标；profile_links/provider revision/四元组及project owner的一致性用新表上的延迟检查，不添加旧表键。所有有用途的敏感引用只能指模型provider用途的版本，不能指认证秘密。

## R09-03 / P1：根输入的通用CHECK误拒已采用的未答问题替换

原位置：§4授权86行要求root_message的expected_question_revision为空；§6约束122行要求root_message“此前没有问题历史”，未限定action。

证据：现行`backend-message-clarification-design.md` §4明定open→superseded可替换未答问题；§6的root_message“无任何问题历史”限制属于decide_message_target的直接判断分支。§2.3/§5.2要求工具授权绑定实际问题版本，并没有禁止用根消息作为未答问题替换的输入。

最小修复改为按action和request/question状态列出合法组合：

- 初次propose：root source、无accepted answer、无当前问题，expected_question_revision空。
- decide直判：root source、request pending且从未有问题、accepted answer及expected question空。
- replace当前open问题：输入来源仍root_message，accepted_answer_id空，`step_question_id`和expected_question_revision非空，必须匹配同请求current_question且state=open；原问题superseded+新问题open+请求revision提升同提交。
- replace当前answer_saved问题或decide答复：clarification_answer、accepted_answer_id为原问题ID，当前问题/唯一答案和版本全部匹配。

不要为替换open问题伪造Answer，也不要移除decide直判的禁止绕过规则。延迟触发器按具体动作核分支，不把以上跨行规则写成无条件单行CHECK。

## R09-04 / P1：清空当前授权会丢失稳定控制步骤的依据

原位置：§4 80—88行把slot单列保留，但branch/source/expected versions/allowed_actions归在可整组清空的authorization列中。文稿同时要求续领沿原slot与原依据。

证据：现行消息内部设计§2.3规定同一决策步骤恢复沿用槽位、互斥动作集固定；§8重启第3项要求新领取者沿原commandSlot及输入依据，不可由租约换代重新定义操作。

最小修复在同一processing行拆两组，不增表：

1. 稳定step组：`current_command_slot_id`、`step_input_kind`、`step_source_message_id`、`step_source_revision`、`step_question_id NULL`、`step_accepted_answer_id NULL`、`step_expected_request_revision`、`step_expected_question_revision NULL`、`step_allowed_actions`。slot分配时原子完整写入；续领只读这些值，不能改branch/source/actions。明确的上下文失效或已完成步骤才换下一slot；原成功由control row保留。
2. 可替换credential组：`authorization_id`、`authorized_session_id`、`authenticated_service_ref`、`authorization_generation`、`authorization_expires_at`、`authorization_revoked_at`。代次变化时可清空这组，稳定step保留。当前授权ID加UNIQUE，签发使用新ID，期限不超过本次租约。CHECK分别允许“有完整step但无credential”和“有完整step及完整credential”，不让整组全空规则逼着擦掉step。

控制与model_call入账复制当次step+session+claim证据，不FK到可替换authorization_id；它们不是可重放授权令牌。根入口与答复入口各有独立step/credential，两个不同logicalRequest在同会话可以各自获得授权；不得改成会话单槽。

## R09-05 / P1：复合FK目标清单尚缺，不能用一般性承诺替代

原位置：§3 49—54、§4 72/84、§5 102—104、§6 114—124。目前写“同域FK”但部分被引用组合未明确存在。尤其根请求actor复合FK缺包含actor的唯一目标；control/execution的processing_entry必须证明归属本logicalRequest，答复入口自己的logical_request_id为空，直接四列FK会错误拒绝合法答复。

最小修复补明确键及FK表：

- conversations(project_id,id)已有新表唯一；所有消息/processing/session/execution的project/conversation均引用它。
- messages增加UNIQUE(project_id,conversation_id,id,actor)；clarifications(project,conversation,answer_message_id,request_actor) FK到此键。非用户actor为空，配合非空request_actor及消息author/entry CHECK排除非用户答案。保留answer消息回指本问题、revision/time相等的延迟检查。
- processing增加UNIQUE(project_id,conversation_id,entry_id)、UNIQUE(project_id,conversation_id,logical_request_id,request_actor)。clarifications的同名请求/actor四列引用后者。target_logical_request_id用project/conversation复合FK到根request唯一目标。
- processing增加生成列`effective_logical_request_id = COALESCE(logical_request_id,target_logical_request_id)`及UNIQUE(project_id,conversation_id,entry_id,effective_logical_request_id)，或等效精确延迟检查。control/execution的project/conversation/processing_entry_id/logical_request_id引用该键，允许答复入口指向旧根request且拒绝串到另一请求。
- processing.message_id、step_source_message_id及control/execution的source message全部带project/conversation FK到messages已有三列唯一。authorized_session_id及control/execution.manager_session_id带project/conversation FK到manager_sessions三列唯一。
- current_question_id、step_question_id、accepted_answer_id、superseded_by按project/conversation/logicalRequest引用clarifications四列唯一，接受答案还须非空answer槽与准确source message匹配。仅FK到clarification PK不能证明同请求。
- root resolution_id引用control_operations(project,conversation,logical_request_id,resolution_id)显式普通UNIQUE；该FK与resolved状态一致性延迟检查。不要拿只有resolution不为空的部分唯一索引做FK目标。

B07 room的四列目标已实际存在，B09此处正确；普通会话room与issue均空不被该FK阻断。

## R09-06 / P2：初始会话回执清理须随首消息/派生标题清理联动

原位置：§8清理178行只列delivery/call；§10 206—210行定义standalone_creation_exact_input/receipt，却未把单条首消息或其派生标题删除与这组副本联动。

证据：现行消息内部§5.3要求清所有可恢复正文副本；B09独立创建的exact/canonical可含firstMessage，receipt含首次title。B06会话清理保护title来源，原键永不复活。

最小修复：清理`standalone_first_message_id`指向的消息时，在相同conversation锁内清空该会话的standalone exact/canonical/receipt，并置standalone_creation_removed_at，保留创建键/actor/时间/首消息身份。若标题由该首消息派生，应有可判定来源规则并一并清理仍派生的标题；独立改名后的标题不误删。独立创建回执中的旧标题也属于副本。一般后续消息清理不能误清与它无关的会话创建操作。无需新表或新删除API。

## 已核对可保留

六表物理数量不必增加。当前授权按processing入口存储，允许多请求并存，方向正确。message/submission合行仍能保留原复合作用域和removed键；answeredRevision保存提交前版本不随问题当前状态变动；Answer槽与resolution唯一键覆盖清理后，均符合已采用规则。事件计数与消息计数分离，B06页面卡片不占消息序号。session换代不改变旧execution的已外发目标，未知继续核查。锁序conversation→Issue→processing→question与B06一致，不存在因为本次counter内嵌新加的逆序。

以上为静态约束复核，不是SQL拒绝测试或真实模型/传输验收。文档作者修复后应逐项在最终b09.md确认，尤其不要只改临时稿。

## 实际落稿回读结果

已读取实际`docs/api-database/b09.md`修订后§3.1—3.8。R09-01—06均已落实：范围text且分owner复核、旧配置真实复合键、replace open合法分支、step/credential分组、effective请求归属及唯一目标、首消息/独立创建回执清理联动。

回读发现并已让作者修复的4个落稿错误也已实见修正：effective_request初始可NULL；凭据前5项非空但revoked_at可空；step问题/答案FK使用effective request，避免答复logical_request_id空值豁免；target request自FK及其三列普通唯一存在。

尚有小型静态补齐：authorization_id应明确UNIQUE允许NULL，避免同一当前凭据重复在两个入口；首段36张“业务表”应写36张“已有表”，其中含1系统表。已告root转达。未新增运行验证结论。
