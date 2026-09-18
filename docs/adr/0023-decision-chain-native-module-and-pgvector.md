# ADR-0023: 历史决策链原生模块与 pgvector 选型

日期：2026-09-15 · 状态：已采纳
关联：ADR-0020（Python 插件路线的终止）、`历史决策终版设计-Go蓝图与可开可关-2026-09-15.md`（D9–D14）

## 背景

Python 版的 `decision_chain` 模块随版本退役；Go 版按终局决策（D5/D8）以原生模块重建"历史决策"：记录范围圈定等决策、支持相似召回与语义检索，并由用户在前端随时开关。 teammates 的 44 表数据库重构方案已将 `decision_chain_nodes` / `decision_embeddings` 保留为平台表。

## 决策

1. **Go 原生模块，不做进程外插件**：`internal/decisionchain`，组合根装配（`cmd/repomesh-web`），生产者经 `scan.HTTP.OnScopeDecided` 回调注入，模块间零 import（回调 + 闭包适配）。
2. **第一期生产者 = 范围圈定确认**：契约按五步链（classification/confirmation/integration/task/pr）全量设计，圈定确认落 `step=confirmation` 决策单；其余步骤待需求→仓库分析上线后接入，模型不变。
3. **记录即投影**：不建事件表 + 投影器（单生产者阶段）；统一事件总账落地后可迁移，决策单表形状不变。
4. **向量主能力 = pgvector**：`embedding_vec vector(1024)` + HNSW cosine；同库事务、备份、权限免费，量级（千~万）下专用向量库的成本无用武之地。`embedding` JSON 列同行双写作兜底（扩展/向量异常时应用内存 cosine），再降级为同仓库 Jaccard 结构化召回。语义检索一律按 `model` 过滤，防换模型后向量混库。
5. **开关落库**：`public.feature_settings(feature, enabled, …)`，前端 `PUT /api/settings/decision-chain` 随时切换，即时生效；关 = 不落新决策单 + 决策 API 503，数据保留。写路径永不调 LLM——向量只经 refresh 异步沉淀。
6. **表落在 `public`（全限定访问）**：对齐 44 表方案的平面命名；本仓库连接钉死 `search_path=pg_catalog`，模块 SQL 一律 `public.` 限定，向量类型/算子用 `::public.vector`、`OPERATOR(public.<=>)`。

## 后果

- 迁移 `0008_decision_chain.sql`：`CREATE EXTENSION vector` 为必装主能力，缺失时迁移显式失败；三表全形状 + 补列双兼容写法（全新库 / 44 表库均可落）。
- 语义召回存在新鲜度窗口（落库到 refresh 之间语义不可见）；结构化召回无窗口。
- `decision_embeddings` 与 `decision_chain_nodes` 间无外键（对齐 44 表方案），孤儿向量以 refresh 幂等覆盖。
- 版本化幂等依赖同链 advisory lock 串行（`requirement_key+step`），唯一索引为纯兜底。
