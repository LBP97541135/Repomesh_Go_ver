# B05 验收文档：模型测试与应用服务（任务 #10）

## 需求是什么

ASTRA 设计准备文档（docs/plan/ASTRA-DESIGN-PREPARATION.md）B05 项要求：在 Go 版中实现"模型测试与应用"完整服务层——用户先把某个模型 Provider 版本跑一次真实测试（预算受限、结果落库），确认后再把该模型应用到某个项目上。配套要求：

1. `modelbudget` 记账包（已在任务 #9 完成，commit 2602b43）。
2. 模型测试服务：预览（不落库）→ 提交（幂等、预算预留）→ 查询（状态流转 sent→succeeded/failed/unknown）。
3. 模型应用服务：预览 → 应用（项目锁定、配置替换）→ 查询 → 撤除。
4. unknown 状态的运维出口：`unknown inspect` / `unknown close` 两个子命令，凭证链校验。
5. coordinator 后台进程负责测试分发：注册 handler、心跳、租约丢失自动重注册。
6. web 层 6 条路由 + 浏览器路由放行 + 组合根装配。

规格来源：docs/development/2026-09-13-b04-b06-design-01/b05.go.txt（454 行声明式规格）。

## 做了什么

### 1. internal/models/destination.go（新建）

- `ReadSnapshotTarget(data []byte)`：解析预览/提交请求体里的目标三元组（providerId、providerRevision、modelRowId），256KB 上限、UUID 校验、providerRevision 非空 ≤128。
- `TestService.ResolveDestination`：把测试 ID 解析成浏览器路径 `/settings/model-tests/{testID}`，用于操作确认页跳转。
- `ApplicationService.ResolveDestination`：把应用操作 ID 解析成 `/projects/{projectID}/model-applications/{applicationID}`。

### 2. internal/models/test_service.go（新建）

- `TestService`：预览走只读事务 + SAVEPOINT，提交走幂等（Idempotency-Key 重放返回既有结果）、预算预留（ReserveTest）、发送许可写入 `repomesh_models.tests`。
- `validatePreviewForNewTest`：提交前对锁定预览做二次校验（时间窗、预算、模型可读性）。
- `replayTest` / `registerTest`：幂等重放与新测试登记的内部实现。
- `TestSubmitFailure`：422 VALIDATION_FAILED / 409 TEST_ALREADY_OUTSTANDING（带 TestOutstandingDetails 指路）。

### 3. internal/models/application.go / application_types.go（新建）

- `ApplicationService`：预览（projectOriginalModel 判定 Unconfigured/Readable/Restricted）、应用（LockForConfiguration → ReplaceModel 原子替换项目模型绑定）、查询、撤除（RemoveApplicationResult / RemoveTestResult）。
- `Applied` / `ApplicationRejected` 完整 MarshalJSON（appliedJSON / applicationRejectedJSON wire 结构）。

### 4. internal/models/unknown_maintenance.go（新建）

- `AuthenticateUnknownMaintenance`：校验数据库角色 `repomesh_unknown_maintainer` 成员资格 + 读 `repomesh_sources.deployment` 的 deployment_id 单例，杜绝任意连接跑运维命令。
- `ParseUnknownCloseCommand`：close 凭证 SchemaVersion==1、ActorID/TestID 与命令行参数绑定一致、SenderExit/CapabilityRevocation 的 Ref 必须是 SHA-256。
- `ValidateUnknownClose` / `CloseUnknownTest` / `InspectUnknownTest`：校验后落库关闭，输出 UnknownCloseReceipt。
- `NewUnknownMaintenance`：inspect/close 不经过 web 层，只有 CLI。

### 5. internal/models/transport.go（新建）

- `NewSingleRequestTransport(protocolVersion)`：单次请求发送许可传输器；协议版本空或超 64 字符返回 `TRANSPORT_PROTOCOL_INVALID`。

### 6. internal/models/test_runner.go（新建）

- `TestRunner`：coordinator 侧的分发循环。
  - `RegisterHandler(ctx, protocolVersion)`：INSERT test_handler_leases，30 秒租约。
  - `HeartbeatHandler(ctx)`：按 instance_id 心跳续租；0 行更新返回 409 TEST_HANDLER_LEASE_LOST。
  - `RunOne(ctx)`：认领一条待发测试 → 鉴权 → 发送 → 记录结果；传输失败走对账（reconcile）。

### 7. cmd/repomesh-coordinator/main.go（全文重写）

- 入口分派：`unknown` 子命令走运维 CLI，其余走 worker。
- worker：access 的 RunOne 与模型测试的 RunOne 双轮询（access 无活时才试测试分发），access 15s 超时 / 测试 30s 超时。
- 心跳每 10 秒一次（5s 超时）；409 TEST_HANDLER_LEASE_LOST 自动重新 RegisterHandler。
- 有活 100ms 节奏，无活 1s 节奏；SIGINT/SIGTERM 优雅退出。
- unknown CLI：`unknown <inspect|close> ACTOR_ID TEST_ID [CLOSE_KEY]`，close 从 stdin 读凭证（≤256KB），inspect/close 输出缩进 JSON；30s 超时；参数错退出 2、执行失败退出 1。
- 协议版本常量 `repomesh-model-test-v1`（≤64 字符；改协议需重新注册 handler）。

### 8. internal/web/models.go（全文重写）+ projects.go / types.go 配套

- web 层 6 条路由：
  - POST /api/model-test-previews（无 Idempotency-Key，200）
  - POST /api/model-tests（带 Key，202）
  - GET /api/model-tests/{testId}（200/404/410）
  - POST /api/projects/{projectId}/model-application-previews（无 Key，200）
  - POST /api/projects/{projectId}/model-applications（带 Key，200）
  - GET /api/projects/{projectId}/model-applications/{applicationId}（200/404）
- `readJSONBody` 通用化（415/413/400 统一处理），`readSnapshotBody` 共用 256KB 限制。
- `writeModelError` 处理 *models.Failure 与 *models.TestSubmitFailure（Details 透传到 error.details）。
- projects.go：projectError 增加 Details 字段透传 TestOutstandingDetails；projectBrowserRoute 放行 `/projects/{id}/model-applications/{uuid}`。
- models.go：newRequestID() 生成 32-hex 应用请求 ID（ParseApplicationCommand 要求 1-128 非空）。

### 9. cmd/repomesh-web/main.go（组合根装配）

- `configureModelBudgets`：把 projects 的配额观察面接到 modelbudget。已知配额完整映射 KnownQuotaObservation；未知走 UnknownQuotaObservation；观察器报错覆盖为 UnknownQuotaObservation(["quota_observation_failed"])——绝不报零配额。
- 窗口初始化：CheckEmptyWindowHistory → EnsureProjectWindow，scope = project_model_runtime + UTC 当日，与 ReserveTest 同键。
- 装配链：budgets → configureModelBudgets → testService / applyService → SetModelTestDestinationResolver / SetModelApplyDestinationResolver → web.Models{Service, Tests, Applications}。

### 10. internal/projects 配套（前序已做，此处核对确认）

- LockForConfiguration / ReadFixed / ReplaceModel / InspectFixedForCreation / CheckedFixed / LockedProject（fixed.go）。
- readConfigurationSummary（quota.go:156）——规格里的 buildFixedSummary 对应此实现。
- CatalogWriter.RegisterCompleteExecution / RegisterRequestPolicy / RegisterTimePolicy（catalog_writer.go）。
- sources.ImportV2 全套（import_v2.go / parse_v2.go / types.go，Receipt.MarshalJSON 在 types.go:151）。

## 影响范围

- 新增文件：internal/models/ 下 destination.go、test_service.go、application.go、application_types.go、test_types.go、unknown_maintenance.go、transport.go、test_runner.go；internal/web/models.go 重写。
- 修改文件：cmd/repomesh-coordinator/main.go（重写）、cmd/repomesh-web/main.go（装配）、internal/web/projects.go（Details 透传 + 浏览器路由）、internal/projects/types.go（Failure.Details）。
- 不涉及数据库迁移（B05 复用既有 repomesh_models schema，表已在 0009+ 全量迁移中建好）。
- 不影响既有 projects / access / skills / scan 路由；web.Models 的 Service 为 nil 时全部跳过，向后兼容。

## 修复逻辑（关键设计决策）

1. **双进程职责**：web 进程负责预览/提交/查询（写预算账本），coordinator 进程负责分发（消费账本）。预算存储是每进程内存 + 数据库窗口行，两个进程通过同一张窗口表对账。
2. **心跳先行**：主循环每轮先判断心跳到期（10s），再做工作轮询。租约丢失不是致命错误——自动重注册，协调器自愈。
3. **配额未知 ≠ 零配额**：观察器任何失败都折叠成 Unknown 配额并给出 reasonCodes，前端显示"无法确认"而非"没有配额"。
4. **unknown 出口最小权限**：CLI 直连数据库 + 角色校验 + 凭证与参数绑定（ActorID/TestID 必须一致），防止拿 A 测试的凭证关 B 测试。
5. **规格偏差处理**：规格声明 RegisterHandler/HeartbeatHandler 为单参，实现带 protocolVersion——传输器构造时已校验协议版本，注册时带上才能让 lease 行记录协议，属必要修正。

## 偏差清单（与 b05.go.txt 规格对比）

| # | 规格声明 | 实际实现 | 处理 |
|---|---------|---------|------|
| 1 | `RegisterHandler(ctx)` / `HeartbeatHandler(ctx)` 单参 | 带 protocolVersion 参数（test_runner.go） | 保留实现：lease 行需记录协议版本，规格漏写 |
| 2 | `NewSingleRequestTransport` 返回单值 | 返回 `(*SingleRequestTransport, error)`（transport.go:23） | 按真实签名适配；构造期校验协议版本 |
| 3 | `buildFixedSummary` | `readConfigurationSummary`（quota.go:156，service.go:509 调用） | 功能等价，名称不同 |
| 4 | GET application 404 错误码 | 实际返回 `APPLICATION_NOT_FOUND`（application.go） | 与测试侧 TEST_NOT_FOUND 命名一致，更合理 |

## commit

- 任务 #9（modelbudget 包）：2602b43
- 任务 #10（本验收文档对应的全部代码）：见分支 feat/astra-b05 本次提交（coordinator 重写 + models 服务层 + web 路由 + 组合根装配）。

## 验证结果

- `go build ./...` 全仓通过（EXIT=0）。
- `go vet ./...` 全仓通过（EXIT=0）。
- Windows 本机跳过 postgres_test.go（`//go:build unix` 标签）；相关集成测试需 Unix 环境验证，已在偏差清单外备注。
- 规格核对：b05.go.txt 声明的 projects 锁定/替换 API、modelbudget 全 API、models 测试/应用/unknown maintenance、sources ImportV2、web Models、access destination resolver、projects quota 投影全部确认存在。
