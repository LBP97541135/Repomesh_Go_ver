# 发现链事实保存独立复核

日期：2026-09-19。源码基线：`28c31677e349cd89c5abff3c7cf26e8b960d526a` 加本轮工作区修改。复核对象为本轮业务事实补丁；不据此宣称 AgentLoop 云端、完整 DSH Trace 或多仓交付评测已经通过。

## 结论

复核发现并修复一项新引入的并发死锁。修复后，相关包 19 项测试全部通过、无跳过，其中 10 项使用真实 PostgreSQL 的独立测试库。新增事实表的精确归属、事务回滚、历史不覆盖、相邻去重与晚提交读取在本轮范围内成立。

`Maintenance.Purge` 的完整清除流程存在既有外键失败；本轮复现并区分基线，没有修改该流程。新表的授权级联删除测试通过，不等于完整清除功能通过。

## 文件与审查点

| 文件 | 本轮核对 |
| --- | --- |
| [service.go](../../../internal/discovery/service.go) | 业务保存与事实追加在同一事务；从已保存的 JSONB 行读回快照，避免把未写入的内存字段当作事实；回执账本、`updated_at` 不参与决定去重。 |
| [recall.go](../../../internal/discovery/recall.go)、[steps.go](../../../internal/discovery/steps.go) | 保存实际读到的候选池、画像及扫描身份；排序与公开候选数量规则不变；截断前结果另外保存。 |
| [facts.go](../../../internal/observability/facts.go) | 精确 Project／Issue 查询；跨项目身份拒绝；只读可重复读事务；稳定来源身份；不以最大 ID 当提交水位。 |
| [0043 迁移](../../../internal/database/migrations/0045_observation_facts.sql) | 新表不回填虚构历史；复合外键绑定项目／Issue；`json` 保留摘要对应字节；阻止修改与普通删除；沿用已有清除事务标志。 |
| [发现链测试](../../../internal/discovery/observation_postgres_test.go)、[事实测试](../../../internal/observability/facts_postgres_test.go)、[规划产物测试](../../../internal/discovery/planning_apply_test.go) | 真实测试聚合、输入保留、事务回滚、去重、晚提交、清除兼容与并发计划保存。 |

未修改前端、AgentTeams、共享数据库或正在运行的服务。测试通过现有 `testdb.Open` 创建随机命名的数据库，应用全部迁移，结束后仅删除这些测试库。连接信息从已有本机文件注入环境，未写入报告或打印其值。

## 发现并修复：计划保存的锁升级死锁

严重性：P1；本轮引入，现已修复。

`Plan` 先插入 `public.plans`，外键检查在关联 Issue 行取得 `KEY SHARE` 锁，然后调用 `save`。两笔并发计划事务均完成插入后，本轮新增的 `FOR UPDATE` 要求双方升级到与对方外键锁冲突的锁，PostgreSQL 必须中止其中一笔事务。`AppendDiscoveryFact` 中相同的锁也需要一起调整，不能只改外层。

先加入 [TestPostgresDiscoveryObservationConcurrentPlanSaves](../../../internal/discovery/observation_postgres_test.go)，在真实数据库中固定两笔计划插入均已完成的条件，然后并发保存。修复前结果：

```text
observation save failed for concurrent plan: ERROR: deadlock detected (SQLSTATE 40P01)
--- FAIL: TestPostgresDiscoveryObservationConcurrentPlanSaves
```

经主代理授权，将两处序列化锁改为 `FOR NO KEY UPDATE`。该锁仍对同一 Issue 的事实写入互斥，同时兼容计划／任务外键持有的 `KEY SHARE`。新测试要求两笔事务均提交，并逐一核对两份计划身份都保留在不可变历史中。修复后结果：

```text
--- PASS: TestPostgresDiscoveryObservationConcurrentPlanSaves (0.36s)
```

这项修复没有把历史序列化扩展为整个发现链的并发控制重构；原有读取状态到保存之间的业务并发语义保持原有范围。

## 已有缺陷：完整 Purge 的外键顺序

分别使用两份隔离测试库验证：

1. 已应用 0043，但 `repomesh_observability.facts` 中为 0 行。
2. 同样的空事实条件，另在该测试库删除新增的 `repomesh_observability` schema 后复验。

两组均通过 `testdb.SeedProject`／`SeedIssue` 建立完整业务聚合，调用 `Archive` 后调用 `Maintenance.Purge`，都得到：

```text
SQLSTATE=23503 constraint=issues_main_conversation_fk
```

原因是现有清除代码先删除仍被 Issue 引用的主会话。该错误发生在新增事实表参与删除之前；移除新表后结果相同。此方法仅在测试库操作，原业务迁移与共享数据未改动。

本轮新增测试只在设置 `repomesh.purge_mode='on'` 的事务内核对 Issue 删除会级联删除事实，并随后回滚，避免把部分聚合删除误写成完整清除成功。此结论足以说明新增 FK／触发器没有阻止该授权级联，但完整 Purge 仍保留上述限制。

## 验收执行

以下命令的 `REPOMESH_TEST_DATABASE_URL` 均从本机既有连接文件加载，不在命令输出显示：

```bash
go test ./internal/discovery ./internal/observability -count=1 -v
git diff --check -- internal/discovery/service.go internal/discovery/observation_postgres_test.go internal/observability/facts.go
```

| 验收内容 | 结果与证据 |
| --- | --- |
| 先失败再修复的并发回归 | 新增测试修复前稳定复现 `40P01`；两处锁修复后通过。 |
| 相关包回归 | discovery 17 项、observability 2 项全部通过，无跳过；包耗时分别 3.387s、1.263s。 |
| 同事务保存与回滚 | 事务内部可见事实；提交前独立读取不可见；回滚后业务行和事实均不残留。 |
| 输入历史 | 只返回 1 个候选时仍保留 2 个实际输入与评分；更新扫描并重选后，旧快照字节保持不变；旧幂等请求不触发重选。 |
| 去重 | `A → A → B → A` 保留 3 个事实；仅增加回执不增加事实；重返 A 使用新事件身份。 |
| 字节与归属 | 读取正文与写入字节一致，SHA-256 一致；项目／Issue 与正文身份不匹配被拒绝；不生成旧历史。 |
| 晚提交和重复采集 | 先分配 ID 后延迟提交的事实在下一次精确读取可见；重复读取不改变源事实。 |
| 不可变与授权清除 | 普通 UPDATE／DELETE 拒绝；授权删除及回滚可用；完整 Purge 的已有失败单独保留。 |
| 格式检查 | `git diff --check` 无错误。 |

额外用临时 Go overlay 执行了上述 Purge 对照，不向产品加入“期望既有错误”的回归测试。未运行真实外部模型调用、DSH turn 或共享服务迁移。

## 覆盖限制

- `input_pool`、`rendered_cards`、`all_scored_items` 覆盖 `Service.Candidates` 的同步召回路径。`ApplyPlanningRun(step=2)` 接受 Agent 产物的路径仍只有结果与 producer，不具备实际输入快照；不能把这类记录解释为完整选仓依据。本轮不因此新增将被取消的 CLI 采集。
- 保存的事实是「该事务实际提交了什么发现链状态」。候选理由仍可能来自 Agent 陈述，保存本身不证明理由正确，也不证明契约已被执行者消费。
- 无扫描时可识别空身份／空画像，但未全面采集扫描失败与权限不足的原因；Spec AC05 的全部场景尚不满足。
- 同时保存实际构建、Prompt／Skill 内容版本、完整模型用量、候选组合和 DSH 身份仍是后续覆盖项；本轮不得据此给出完整 Trace 或性能改善结论。
- 本次未单独测量业务补丁的吞吐／存储开销，测试用时不等于接入开销；Spec AC33 的开销测量仍需主批次补充或明确保留为未验收。

合并 main 时因 0043／0044 已由主线采用，此迁移重编号为 0045；上文 0043 与原实测记录仍指首轮隔离测试版本。
