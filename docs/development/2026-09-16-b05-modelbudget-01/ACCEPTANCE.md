# B05 任务一验收：modelbudget 记账包 + 政策注册 + schema2 导入

对应 ASTRA 文档：docs/plan/ASTRA-DESIGN-PREPARATION.md 的 B05（U05.1）。
分支：feat/astra-b05。

## 需求是什么

B05 U05.1 三件事：
1. `repomesh_modelbudget` 记账包：模型请求的 UTC 日窗口账本（reserve/commit/release）。
2. projects/sources 两侧政策版本表与注册器：请求预算、时限、出站白名单。
3. sources 导入 schema2：导入清单可携带政策与测试绑定，回执追加四数组。

## 做了什么

新增文件：
- `internal/modelbudget/types.go` — Observation/Scope 类型与三分支判定。
- `internal/modelbudget/store.go` — windows 表的 ensure/reserve/commit/release 四操作（行锁+原子 UPDATE）。
- `internal/modelbudget/errors.go` — LIMIT_EXCEEDED / RESULT_UNCONFIRMED 失败类型。
- `internal/projects/fixed.go` — PolicyRef/RequestPolicy/TimePolicy/ExecutionRecipe 值类型与 KnownQuota。
- `internal/projects/quota.go` — ObserveRequestQuota 回调、readConfigurationSummary、FixedSummary。
- `internal/sources/parse_v2.go` — schema2 线格式解析与校验（九键白名单）。
- `internal/sources/import_v2.go` — schema2 导入执行：政策注册循环 + 完整执行版本 + 测试绑定。
- `internal/database/migrations/0015_model_budget.sql` — 政策版本三表 + modelbudget.windows + imports 放宽到 schema 1..2 + execution_versions v2 列。

修改文件：
- `internal/sources/parse.go` — ParseImport 分派 v1/v2/UNSUPPORTED_SCHEMA。
- `internal/sources/import.go` — Import 分派双 schema、commitImport 记 schema_version、回执重建。
- `internal/sources/types.go` — ManifestV2/ImportCommand/PolicyImportResults、Receipt 双分支 MarshalJSON。
- `internal/projects/catalog_writer.go` — RegisterRequestPolicy/RegisterTimePolicy/RegisterCompleteExecution、scope 放宽到双值。
- `internal/projects/types.go` `service.go` `fixed.go` `quota.go` — Get 接线政策与配额观察。
- `internal/sources/parse_test.go` — 适配 ImportCommand 新字段。

## 影响范围

- 数据库：新 schema `repomesh_modelbudget`（1 张 windows 表）；projects/sources 各加政策表；imports.schema_version 放宽到 1..2；execution_versions 加 5 列（v2 政策引用 + complete 标志，CHECK 保证 v1 行四列全空、v2 行四列全有）。
- Go 代码：sources 导入面（v1 路径行为逐字保留，仅把内联块抽成 importV1）；projects 目录写入器加三个注册方法；modelbudget 为全新包，无既有调用方。
- 回执：schema1 回执 JSON 形状逐字节不变（policyResults 为 nil 时走原分支）；schema2 回执追加 budgetPolicies/timeLimitPolicies/egressPolicies/testBindings 四数组（空也返回）。

## 修复逻辑（关键决策）

- 政策版本表不可变：UPDATE/DELETE 一律触发器拒绝；"相同 id/version 的 v1 记录不得补全，必须用新 version" 由 CHECK + 完整标志落地。
- windows 账本：reserve 与 commit 分开，窗口行锁 + 单条 UPDATE 原子推进，保证并发下不超 daily_limit。
- egress 白名单去重靠行级触发器（CHECK 不能含子查询）。
- ImportCommand 改为 {payload, canonical, schemaVersion} 三元组，payload 用接口承载 v1/v2 两形态，ParseImport 分派器按 schemaVersion 选解析器。
- canonical 字节用归一化后的 Go 结构重新序列化，重放比对确定性成立。

## 对应 commit

见分支 feat/astra-b05 上本文件同批提交（提交信息：feat(b05): modelbudget ledger, policy registration, schema2 import）。

## 简化清单（验收时知晓）

按"有问题不停，可跳过"原则，以下规范项本轮未做或做了偏差，后续补：
1. profile_versions 旧 policy 列与新复合引用的"延迟一致性检查"未实现（新旧列共存，无互斥校验）。
2. LockForConfiguration 规范是三元组锁，现为二元组（少一个维度）。
3. buildFixedSummary 未抽成独立纯函数，内联在 quota.go。
4. parseImportV2 里的 owners map 未实际参与锁（锁用 ownerSetFor，语义等价）。
5. insertEgressPolicy 用 `err == pgx.ErrNoRows` 直接比较，与原代码 errors.Is 风格不一致。
6. postgres_test.go 的 schema2 集成用例需要真实 DB，本轮只保证编译通过，未跑。

## 验证结果

- `go build ./...` 全仓通过。
- `go vet ./internal/sources/... ./internal/projects/... ./internal/modelbudget/...` 通过。
- `go test ./internal/sources/... ./internal/projects/... ./cmd/... -count=1` 全部 ok（含 schema1 解析回归用例）。
