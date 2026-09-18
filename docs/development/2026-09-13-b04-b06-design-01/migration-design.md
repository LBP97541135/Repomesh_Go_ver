# B04—B06 迁移设计附件

状态：MIGRATION_DESIGN_NOT_ADOPTED_NOT_EXECUTED。这是可评审的表与约束设计，未向产品 migrations 写入 SQL。实际迁移编号在实施时按现存 0001—0004 之后分配，并由 B01 显式迁移/历史校验执行；普通启动不迁移。下述名称是候选物理名，已有表名称以当前 `0004_projects.sql` 为准，尤其是 `repomesh_projects.configuration_revisions`。

## B04

| 表/扩展 | 列、身份与关键约束 |
| --- | --- |
| repomesh_sources.deployment | singleton=true PK；deployment_id UUID UNIQUE NOT NULL，创建后不可改；数据库备份保留。部署角色成员资格来自数据库认证，非manifest。 |
| sources.import_serialization / imports | 单例行预置；imports PK(deployment_id,import_id)，schema_version、canonical bytea、receipt jsonb、committed_at非空。没有可提交pending。旧比较器保留。 |
| sources.environment_templates | PK(id,version)，完整登记字段沿来源§8；所有业务列不可更新；同ID/version异内容不能覆盖。 |
| sources.execution_versions | PK(profile_id,version)，owner、template_id/version、worker_concurrency、verification_group_enabled；复合FK(profile_kind='execution',profile_id,owner)至profiles，FK至profile_versions及模板；本记录为來源的完整元数据。 |
| projects.defaults | 加 nullable pinned_version，FK(kind,profile_id,pinned_version)至profile_versions。旧行保留null；新导入强制非空。只该来源渠道建立 execution 默认，不新增model默认UI。 |
| models.providers | id PK、owner FK account、head_revision NOT NULL、enabled/access_epoch；UNIQUE(id,owner)，head复合FK可延迟。owner和id不可改。 |
| models.provider_revisions | PK(provider_id,revision)，owner、name、base_url、api_format、secret_version_id、secret_owner_kind='model-provider'、secret_owner_id=provider_id、purpose='model-provider-key'、created_at。provider/owner复合FK；完整secret四元组FK；所有事实不可改。 |
| models.model_rows | id PK，provider_id、owner、profile_id；UNIQUE(provider_id,id)、UNIQUE(profile_id)，FK provider/owner及(kind='model',profile_id,owner)；行只增不删。 |
| models.model_snapshots | PK(provider_id,provider_revision,row_id)，外部model_id及完整模型参数；UNIQUE(provider_id,provider_revision,model_id)，FK revision、row。每revision有1..50行且保留该Provider全部已存在行，由最终状态延迟约束检查。 |
| models.profile_links | PK(profile_id,profile_version)，固定kind='model'、provider_id/revision/row_id、owner、secret_version_id。FK profile_versions及完整snapshot；CHECK profile_version=provider_revision。延迟约束核对profile owner、snapshot row的profile、revision秘密和profile_versions秘密四元组全同。 |
| models.save_operations | PK(actor,save_id)，kind=save_input/save_closed；target provider可空；schema_version、canonical_nonsecret、input_vault_version、receipt、outcome、removed_at。target带actor复合FK。vault四元组固定 owner=(provider-save-input,actorUUID:saveUUID)、purpose=operation-input。 |

save_operations 在最终状态只允许三种形态：①save_input、committed/rejected、schema/canonical和完整稳定回执非空；replace须vault，keep无vault；②save_closed、closed_without_save、closedAt和固定回执非空，所有输入/target/vault为空；③removed占位，正文/canonical/receipt/vault关联清空，必要target/owner/key/removedAt保留。已提交 kind/outcome/result 不可改，唯一例外是合法原子清理转removed。不能把普通CHECK写成中间占位必须完整导致两阶段同事务写入失败；最终完整性使用延迟constraint trigger，按PK回读当前行。

秘密层复用现有 versions/availability/root_keys，不新增秘密系统。PreparedSecret 在进入业务事务前已独立预扣且不返还；InsertPrepared同事务写版本和availability，应用结果回滚即二者均无记录。availability的可变状态与不可变secret身份分离；密文合法销毁保留identity。认证 orphan清理白名单不扩到新model目的，模型维护自己清理vault。部署导入无须secrets启动。

保留0004已经存在的configuration_revisions_immutable/profile_versions_immutable触发器，不重复创建同义检查；新增模型事实同样不可变。enabled等资格只在单独可用性行或现有profile行变化。root rewrap只改秘密包装列，不得触发误把合法重包当版本身份改变。旧 configuration fixed JSON 不重写，未能证明完整旧映射时只报告受限。

## B05

| 表/扩展 | 约束 |
| --- | --- |
| projects.request_policy_versions / time_policy_versions | PK(id,version)，来源§9的显式参数CHECK；不可变，不以ID覆盖旧版。scope不混用actor测试与project运行。 |
| sources.egress_policy_versions / test_bindings | egress PK(id,version)不可变，白名单数组规范化无重项；test binding PK(owner)，三个确切政策FK、组合revision。改绑定不改历史preview/test。 |
| sources.execution_versions完整扩展 | v2版本增加预算/时限id/version复合FK及完整标志；v1保持不完整。相同id/version不得UPDATE补全；新版本须模板、政策和project profile_version共同存在。project profile_versions的旧policy_id列与新复合引用由延迟一致性检查对应。 |
| modelbudget.windows | scope_kind只能为actor_model_test或project_model_runtime；actor_scope_id与project_scope_id按kind恰有一个非空并分别FK account/project。scope_id是从该非空FK列生成的stored列，调用方不能独立写。start_utc规范为UTC 00:00，end_utc=start_utc+1 day；limit>0、reserved/consumed>=0，revision；同窗口limit只能减少。PK(scope_kind,scope_id,start_utc)，policy不在主键。 |
| modelbudget.test_reservations | UNIQUE(actor,test_id)并复合FK tests(actor,test_id)，另有window FK、policy FK、amount=1、状态reserved/consumed/released；状态变更与窗口计数同tx。consumed不可转released。跨日仍结算原window。 |
| models.previews | id随机PK，kind=test/apply分型，actor/固定snapshot/完整政策/有效期；apply额外project/version/完整before；不可变输入，consumed_by只能空→一个operation，不可释放再用。保留最小消费墓碑，不随5分钟TTL删去消费事实。 |
| models.tests | PK(actor,test_id)，固定snapshot FK、preview及policy、accepted_at；canonical输入、当前state/result_revision/recovery、removed_at；身份与accepted_at不可改，观察列按状态机更新。preview唯一消费约束。 |
| models.test_handler_leases | instance_id随机不可复用PK、protocol_version、last_seen/lease_until、retired_at；仅coordinator登记/续期，Web只读。过期不是撤销证明。 |
| models.test_dispatch | UNIQUE(actor,test_id)并复合FK tests(actor,test_id)；external_operation_id和send_permit_id唯一、may_have_sent_at、generation/lease、sender_instance_id FK handler租约记录、credential_capability_id/version、capability_revoked_at及revocation_evidence_digest。发送许可事务固定这些身份；may_have_sent只能空→非空，能力撤销只能空→固定事实。关闭后该sender/permit不能再取得凭据。本地撤销记录不冒充提供商远端凭据撤销。 |
| models.test_observations | evidence_id PK，actor/test/external_operation复合FK至同一dispatch、已知观察字段；append-only；不存完整供应商响应/Key。旧generation可追加该逻辑调用证据，只有当前reconciler采纳，不让迟到者直接更新正式状态。 |
| models.actor_outstanding_tests | actor PK，(actor,test_id)复合FK tests(actor,test_id)，并保存recovery-open依据；不对test_id单列全局唯一约束。注册和责任清除锁account后在同tx处理；30天结果清理不能删未核清责任。该形状依赖S05每actor最多1笔的候选数值，本轮五项收口不单独采用该数值；改变上限时须连同Preview、Submit和本表一起重审。 |
| models.unknown_test_closures | UNIQUE(actor,test_id)且PK(deployment_identity,close_key)，复合FK tests(actor,test_id)，并绑定同一dispatch的external_operation_id、send_permit_id、不可复用sender_instance_id和credential_capability_id/version；保存规范化schema、两类带外材料引用及SHA-256、由实际DB session_user取得的受控操作员主体、closed_at和稳定回执。事实列不可更新，不存原始Secret。关闭与dispatch本地能力撤销、对应actor outstanding清除及审计同tx；原key重放，COMMIT未知仍用原key恢复。 |
| models.application_operations | PK(project_id,actor,application_id)，规范化输入/preview消费、committed或rejected稳定结果/removed；项目/配置复合FK及候选snapshot FK；no-op也有回执，retained execution与before及结果配置一致。 |

测试登记事务一次写accepted test、preview消费、reservation、actor outstanding及test_dispatch可扫描责任。预算预留后但尚无许可可证明未发而取消；可能发送许可持久化同时消费1单位，并且只有收到该次COMMIT成功回执的进程获得内存SendPermit。读库不能复原发权。消费/permit约束必须同事务，网络不入事务。平台预算数据只计算此scope下受控本地请求，非供应商全账户额度。

unknown关闭不修改test.state和consumed。延迟Observation保持append-only，不能改写unknown_test_closures或恢复outstanding。证据结构约束只能证明字段完整且绑定原test、permit、sender和credential能力版本；材料事实由受控操作员承担，数据库CHECK不能把自填JSON提升为自动证明。

`modelbudget.windows`采用以下DDL形状。实际迁移须使用现存表的精确约束名，并以PostgreSQL约束用例验证：

```sql
scope_kind text NOT NULL
  CHECK (scope_kind IN ('actor_model_test', 'project_model_runtime')),
actor_scope_id text REFERENCES repomesh_access.accounts(id),
project_scope_id text REFERENCES repomesh_projects.projects(id),
scope_id text GENERATED ALWAYS AS
  (COALESCE(actor_scope_id, project_scope_id)) STORED,
CHECK (
  (scope_kind = 'actor_model_test' AND actor_scope_id IS NOT NULL AND project_scope_id IS NULL)
  OR
  (scope_kind = 'project_model_runtime' AND actor_scope_id IS NULL AND project_scope_id IS NOT NULL)
),
start_utc timestamptz NOT NULL,
end_utc timestamptz NOT NULL,
CHECK (start_utc = date_trunc('day', start_utc AT TIME ZONE 'UTC') AT TIME ZONE 'UTC'),
CHECK (end_utc = start_utc + interval '1 day'),
PRIMARY KEY (scope_kind, scope_id, start_utc)
```

互斥CHECK使scope_id只能等于所选actor或project外键，不能由正文冒领另一身份。UTC日界CHECK把同一scope同一UTC日的start_utc规范为唯一值；复合主键因此拒绝该日第二行。policy切换继续命中同一窗口，不因policyVersion变化建立第二窗口。

B05项目额度窗口的无历史证明由本地受控账本版本/消费者启用事实产生；缺表/读错/已有消费未知不等于空。未来增加运行消费者时必须在同窗口计数原子写用量，不能另造绕过计数路径。只读GET无写窗口；模型专用应用保留 execution，不重置或增加额度。

所有新增owner列必须引用稳定account身份，并由不可变触发器或等价DDL禁止UPDATE改属。该义务覆盖Provider、profile、来源执行版本、政策绑定和测试固定身份。新表的用例核权不替代持久约束。同owner写先锁account；来源导入按owner ID排序锁账户后才锁catalog。

## B06

新增 `repomesh_issues` schema；只保存这批业务聚合，尚不迁入完整计划/消息/运行表。

| 表 | 最小键与完整性 |
| --- | --- |
| conversations | id PK、project_id FK、title可清理为null、title_redacted_at、created_by_operation_id、title_origin_operation_id、content_scope_revision、removed_at；UNIQUE(project_id,id)。两operation引用均同项目复合FK可延迟；必要新会话将两者设本creation，关联已有会话不改。title与redacted标志二选一。独立改名时title_origin_operation_id置null但创建来源保留；已有会话可多Issue，无project当前配置绑定列。 |
| issues | id PK、project_id、number、title/description/criteria、revision、main_conversation_id、main_changeset_id、initial_configuration_revision、creation_operation_id、removed_at；UNIQUE(project_id,number)，UNIQUE(project_id,id)，UNIQUE(project_id,id,initial_configuration_revision)。初始引用、项目、创建操作不可变。 |
| creation_operations | PK(project_id,actor,entry,creation_id)，entry CHECK issue_page；id稳定UNIQUE用于关系；UNIQUE(project_id,id)、UNIQUE(project_id,id,issue_id)；schema/canonical/exact/digest、issue_id/main_cs_id/conversation_id/initial_configuration_revision/created_at及receipt、removed_at。只有完整成功或清理占位可提交。 |
| changesets | id PK、project_id、issue_id、kind CHECK main；UNIQUE(project_id,issue_id,id)、UNIQUE(project_id,issue_id)在本批仅一个main；未来增加子CS须显式迁移放宽到partial unique kind=main。 |
| issue_repository_scope | PK(issue_id,repository_id)，project_id、scope_revision；FK(project_id,issue_id)与FK(project_id,repository_id)至project_repositories；最终非空。 |
| issue_content_scope / conversation_content_scope | 分开两表，各PK(object_id,repository_id)、project_id、introduced_by_operation，两个同project复合FK；只增不减，scope修订同锁内更新。避免object_kind通用FK无法约束的空洞。 |
| page_sources / conversation_cards | source以operation_id唯一，issue/project/conversation复合关系；card以source_id唯一，并引用同一conversation/issue/operation。页面来源无message_id；敏感摘要可清空，关系保留。 |
| continuation_work | work_id PK，project_id、issue_id、cause_operation_id，kind=issue_continue；UNIQUE(cause_operation_id,kind,issue_id)；FK(project_id,cause_operation_id,issue_id)至operation，创建state=blocked、reason=INTEGRATION_NOT_AVAILABLE；本批唯一后续转换blocked→cancelled，reason=CONTENT_REMOVED、cancelled_at非空；保留cause/Issue/pin关联，禁止反向或删除。后续消费者另审其他状态迁移。 |
| issue_event_streams / issue_events | stream PK(issue_id)、generation、last_sequence；events PK(issue_id,generation,sequence)，事件snapshot_invalidated、created_at；创建sequence=1。每Issue计数锁串行，不用全局sequence证明提交顺序。 |
| project_issue_counters | project_id PK/FK，next_number>0；分配事务先锁project，UPDATE RETURNING下一号；允许回滚/未来迁移造成间隙。 |

核心关系约束以如下 DDL 形状审查，省略的是表内普通字段而非条件分支：

```sql
-- 全部引用字段在完整活记录中非空；FK默认RESTRICT，无正文级联删除。
FOREIGN KEY (project_id, initial_configuration_revision)
  REFERENCES repomesh_projects.configuration_revisions(project_id, revision);
-- operation的固定版本必然与该Issue一致。
FOREIGN KEY (project_id, issue_id, initial_configuration_revision)
  REFERENCES repomesh_issues.issues(project_id, id, initial_configuration_revision)
  DEFERRABLE INITIALLY DEFERRED;
-- Issue.main_changeset_id必须是自己的CS，不能只引用全局id。
FOREIGN KEY (project_id, id, main_changeset_id)
  REFERENCES repomesh_issues.changesets(project_id, issue_id, id)
  DEFERRABLE INITIALLY DEFERRED;
-- Issue主会话与operation结果同项目；另有最终一致性核对主会话/CS/operation相同。
FOREIGN KEY (project_id, main_conversation_id)
  REFERENCES repomesh_issues.conversations(project_id, id);
```

为上述循环关系使用 DEFERRABLE INITIALLY DEFERRED。Issue主CS列为NOT NULL，先生成所有ID再插入，不能先提交空引用。operation与Issue互相引用，延迟检查最后一个状态；客户端看不到中间占位。

必须具名实现以下 constraint triggers，并在实施中直接用破坏性SQL证明拒绝，不能只跑handler成功：

1. `creation_aggregate_complete`：按operation PK读取最终行；非removed要求非空规范输入/receipt/结果、schema支持、唯一Issue/主CS/主会话/来源/card/非空scope、一个初始blocked work及至少创建事件存在（removed操作允许该work变cancelled）；各结果身份与pin一致。不要求事件永久保留：创建事务检查事件，之后合法保留期清理不触发旧操作错误。未提交operation占位不能漏检。
2. `issue_origin_consistent`：Issue和operation双方插入/身份变更都排队检查相等、同project/actor归属，防只插孤立Issue绕开上一个trigger。removed也保留pin及必要关系；清理只删正文。
3. `scope_complete`：Issue插入与work scope插入/删除均排队，最终至少一仓；issue content为工作范围超集，关联conversation content为该Issue content超集。所有scope写路径先锁同project再对象。内容行删除拒绝，不把工作缩减变成历史内容权限缩减。
4. `immutable_configuration_fact`、`immutable_provider_fact`、`immutable_creation_identity`：禁止事实UPDATE/DELETE；显式白名单只允许正文清理及操作→removed，绝不允许removed→活。允许合法secret rewrap/availability更新不改变其身份。
5. `creation_receipt_consistent`：结构化结果列是权威，receipt由同一result构造且最终校验JSON关键身份/时间/配置投影一致；清理时删除receipt，不以残留JSON绕过正文清理。相同规则适用于model操作稳定回执。

以上最终约束查的是当前行，不使用触发时尚未填全的 NEW 旧快照。SQL一致性需要以真实PostgreSQL证明；本次仅设计检查，尚未执行。新Issue无nullable历史例外。若未来引入别的来源/消费者，须增量设计新的受信身份及约束，不能把本批entry CHECK直接去掉后接受客户端字符串。


## 独立复核补强

`configuration_owner_and_binding_consistent` 在configuration_revisions插入时排队，按最终行核两个固定profile的owner均等于project.owner；model/execution固定JSON中的secret四元组必须等于确切profile_version元数据，物化参数必须等于所绑定不可变来源。用profile ID/version的复合FK只证明版本存在，不能替代此owner/值一致性检查。profile和project的owner身份也不可更新；若以后引入共享ACL须另行替代此owner-only约束。本批运行消费者不得跳过这条持久归属事实。

对已有B03记录只按其原固定schema和原profile版本验证（包括旧model-policy后由execution非null覆盖的物化规则），不跟随当前head/default；原合法未解析/不完整记录继续受限。新v2完整配置必须有execution政策闭包。迁移发现既有归属/秘密/物化值不一致时整体拒绝，不批量重绑或猜修历史，记录不含正文的定位供另行处理。实施须故意用Alice项目/Bob profile、篡改fixed.secret、篡改workerConcurrency的SQL证明拒绝，P9同项目FK不能单独满足该验收。

正文清理还核会话标题来源：当title_origin_operation_id等于被清理创建操作时，才清title并置title_redacted_at；单纯created_by_operation_id相同不足以清掉之后独立改名的标题。关联的已有会话标题不动，保留必要会话/卡片/scope身份；候选列表将已redacted标题投影为无正文固定标签。显式改名必须同事务移除派生标记，防后来错误删除独立内容。本批不新增改名路由，但储存关系可判定清理责任。
