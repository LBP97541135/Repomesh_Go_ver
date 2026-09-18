# 设计验收映射

状态：本文件仍是设计映射。B04 S01—S12 的运行证据见 [收口 01](../2026-09-13-b04-closeout-01/README.md)，不把该证据回写成本表设计当时的 ALL_RUNTIME_CASES_NOT_RUN。B05／B06 行仍未实施、未跑。不能将行数/静态检查通过代作业务通过。真实外部验收另获授权。

## B04

| ID | 条件/故障注入 | 独立观察与预期 |
| --- | --- | --- |
| S01 | 同actor/key、同输入20并发 | SQL计数仅一Provider revision/完整snapshot组/业务secret/vault/receipt；所有确认响应指向同结果。 |
| S02 | 同key不同Key/输入，业务Key后来撤销 | 独立vault按旧schema精确比较，异输入409；不把秘密撤销当输入相等或重建依据；Key不出日志。 |
| S03 | 每个写入阶段失败，包括secret插入后 | 最终业务、秘密版本、availability、目录和receipt全回滚；root.wrap_count预扣保留，无负退款。 |
| S04 | save先提交、close后；close先提交、迟到save后 | 第一种close原committed；第二种唯一closed，迟到save409且无新secret/provider。并发双close时间相同。 |
| S05 | 包装根切换/退役在Prepare与Insert之间 | exact prepared root重核；不偷偷换root或在持业务锁时reserve；失败无半套，关闭无需解密。 |
| S06 | vault根暂缺、整个服务冷启动根缺 | 运行中比较503保留原槽；正常认证可close空槽；现有冷启动拒绝时需恢复根再原键查询，不声称API存活。 |
| S07 | COMMIT成功但HTTP丢失、一次GET先404 | 主库最终原receipt；原key一直保留；close若取胜也阻止晚保存，不能自动新key。 |
| S08 | 清理原操作、跨actor、旧钥请求重放 | 当前无权隐藏404；有权removed410且无vault/正文/digest；Provider业务secret不因receipt清理销毁。 |
| S09 | 导入schema1正常、重放旧版、同ID/version异内容 | 同key回原receipt；新key重列旧版不倒退head；异内容全回滚；新默认明确可钉旧版。 |
| S10 | 多owner导入与Provider保存并发 | 先import单例→sorted accounts→catalog，不出现catalog回拿owner；账户/默认归属不串写。 |
| S11 | 非部署角色/manifest自报actor/冷启动无App配置 | 角色不符拒绝；manifest未知字段拒绝；有效DB部署身份可用独立CLI导入，不调用auth runtime。 |
| S12 | 未完整execution/旧null默认、新pinned默认 | 固定C1不变；旧null保留原解析语义；v1不能充当B06完整配置。 |

## B05

| ID | 条件/故障注入 | 独立观察与预期 |
| --- | --- | --- |
| T01 | 预览到期/政策变化/不同key复用preview | 新提交409且不预留；原接受操作先重放；消费唯一保留。 |
| T02 | 注册每阶段失败、同actor多key竞争 | test/work/preview/reservation/outstanding全同事务；最多一未核责任，存在原可读test定位。 |
| T03 | 许可commit前、commit回执丢失、确认后发送前崩溃 | 前者可重新处理未许可记录；后两者保守unknown、记录端点零或一次，永不从DB恢复发送许可。 |
| T04 | 发出请求后丢响应/旧lease失效/重启 | 原externalOperation稳定；端点计数不增加，unknown不退款、不自动新请求。 |
| T05 | 真实调用开始、HTTP成功/明确错误/超时 | running只在发送观察后；passed/failed有独立响应证据；超时unknown；费用金额始终null。 |
| T06 | 旧provider head更新、原secret失效、DNS/private/redirect | 已登记test只用原snapshot；不可用拒绝/unknown，不换新Key；受控端点证明无越界请求/重定向/重试。 |
| T07 | UTC换日、降低额度/换政策、unknown跨日 | 原window消费，counter不重置；新日不清actorOutstanding；当前remaining不能凭缺行填0。 |
| T08 | 运维关unknown缺证据/有撤销证据 | 缺准确sender退出或旧发送能力撤销任一材料都保持open；受控操作员两类核查齐全才closed_without_result，state仍unknown、预算consumed。 |
| T09 | 迟到旧generation响应/失权/30日清理 | 只登记相同external op证据；已运维关闭时不改unknown、关闭审计或预算；失权不披露；清理不丢未核责任。 |
| A01 | execution默认C1→C2后应用模型 | SQL fixed JSON中execution选择/版本/参数完全等于原项目；不用当前default。 |
| A02 | 同模型同reference/版本；inherit→同模型reference | 前者no-op保留三修订和稳定回执；后者选择依据变化产生一新配置。 |
| A03 | 应用与项目PATCH/Provider保存交错 | 同owner都先锁account；应用保持project→operation→catalog/profile→Provider，B04保存保持operation→catalog→Provider；CAS冲突全回滚，原操作重放优先。 |
| A04 | 原模型受限/未配置/可读但disabled | C06三分支精确；受限无ID泄露且不阻止修复；可读只取指定snapshot，不取latest。 |
| A05 | execution材料无法确认与已知当前不可用 | 无法确认canApply=false；已知固定历史但不可用可受限保存，仍完全保留execution。 |
| C05 | 固定policy历史、无可信window、政策停用 | GET fixedSummary与effective同revision；未知不填0；historical available不等于可执行；GET不写表。 |
| C06 | 快照迟到/切actor/查询失权 | 前端按actor+目标+代次核对，不移植旧preview/测试；只读受限立即清敏感数据。 |
| T10，待执行 | quota有余量但existing outstanding命中；Preview后竞争到Submit | Preview同次观察给canSubmit=false、TEST_ALREADY_OUTSTANDING和本人可读定位；quota独立；Submit事务重查409并在仍可读时给error.details定位。 |
| T11，待执行 | 无handler登记、handler读取失败、原testId重放 | 缺失为Preview阻止原因和Submit确定拒绝；读取失败503 TEST_HANDLER_UNCONFIRMED且无新记录；原key原输入回执优先于这些新建检查。 |
| T12，待执行 | unknown inspect/close缺材料、同testId跨actor、错绑定、双close、COMMIT回执丢失、晚到结果 | CLI从受控环境取得operator；actor/test/permit/sender/capability绑定不符拒绝；同UUID不同actor隔离；原closeKey收敛同回执；关闭只原子清对应outstanding并留unknown/consumed/审计；晚到只追加Observation。 |
| T13，待执行 | 同owner交互写与多owner来源导入交错；故意UPDATE owner | 所有路径先account且后段不回取；B04保存slot→catalog→Provider，项目路径保留project→operation→catalog/profile；来源sorted owner accounts→catalog；DDL拒绝owner改属。 |
| T14，待执行 | schema1重放、schema2含重复execution集合、同scope同UTC日插第二窗口、伪造scope_id | schema1回执字节不升级；schema2只接受一个executionProfiles；scope互斥FK和派生列拒绝冒领；复合主键拒绝第二窗口。 |

## B06

| 既有编号 | 输入/交错与注入点 | 独立观察与本方案覆盖 |
| --- | --- | --- |
| DB01 | 20个同key同规范输入 | 一operation/Issue/mainCS/必要会话/来源/card/blocked work/创建事件；其余200或503后原查200；U06.1/3。 |
| DB02 | 同key不同规范输入并发 | 仅赢家输入持久，其他409或未知核查；正文不能后到覆盖；保留文本、验收顺序、repo集合规范；U06.3。 |
| DB03 | b06 transactionPhase每写点失败及COMMIT约束失败 | 主库无半套聚合/工作；故意提交孤operation、孤Issue、空scope、错mainCS均拒绝；U06.1/3。 |
| DB04 | 丢HTTP响应、一次查询先于提交 | UI冻结原key/body，404仍未知；最终唯一原三ID与回执；U06.4。 |
| DB05 | 配置CAS和建项交错、再重放 | 锁后条件相同才提交，冲突409；重放不核新条件；对应CB02；U06.3。 |
| DB06 | 关联已有会话时扩大内容scope/撤权 | 卡片与scope原子；旧观察先回滚再核全范围；原结果读取不凭旧小scope披露；U06.2/3。 |
| DB07 | B03部分仓失权后修复模型；明确加仓之一失败 | 原project仓全集保留；应用无需旧仓全读权；添加失败无局部配置/范围变更。必须运行B03回归，不能用Issue测试替代。 |
| DB08 | 清理后原key任意合法正文、并发迟到创建 | 同tx清正文/digest/派生、保留pin/范围/墓碑；new仅清仍派生的标题，existing/独立改名保留；有权410先于比较，无权404；旧key零新Issue；work只转cancelled/CONTENT_REMOVED且保留cause，不能删/反向复活。 |
| CB01 | 成功、每步失败、丢回执 | operation与Issue同一initial config，work经Issue可追；失败全无；原查询同配置。 |
| CB02 | create先拿project锁/应用先拿锁/no-op应用 | 前者Issue钉C1；后者旧context409，刷新新输入才能C2；no-op不换修订。 |
| CB03 | 项目切C2后清进程缓存、重启只读恢复 | SQL work→Issue→C1取得同model/params/secret/version，仍blocked；不调用真实消费者、不证明G1。 |
| CB04 | 空pin、跨project pin、operation错pin、改初始pin | NOT NULL/复合FK/immutable与延迟一致性在DB拒绝；再用Alice项目/Bob profile、篡改fixed.secret/执行参数证明归属/物化一致性拒绝；无nullable新行例外。 |
| CB05 | 缺材料的项目受限保存、SecretVersion撤销、删除被引用配置 | B03保存仍可受限；新Issue失败无聚合；历史配置删除RESTRICT；旧Issue pin保持且接续blocked。 |
| I06 | 用户读权/App能力观察过期、connection epoch变、锁等耗尽 | 最终DB时钟核60秒与代次；回滚后重核；unknown503不冒充denied；不持锁网络。 |
| I07 | supplied repositoryAnalysisId、同名不同key、new/existing会话 | 无分析来源不静默丢ID，新输入404；同名两独立Issue；已有会话不改名/造消息；new才创建必要会话。 |
| I08 | 原结果存量内容scope大于新请求scope、结果已removed | 按存量完整scope核权，removed有权410先正文比较；scope不由新输入驱动。 |

## 跨批交付门槛

实现验证必须按实际函数、事务和存储运行；真实模型请求另行授权。T10—T14是2026-09-14新增的待执行验收，不是实测结果。B02外部暂停不被此矩阵解除。B03全量原回归是U04.1/U05.3/U06.3集成门槛；最终readiness仍区分管理面可创建与运行未实现。历史文档/声明校验结果保留在checks.json，不能填进上述业务用例的“通过”。
