# B06 验收文档：Issue 原子创建（任务 #11）

对应 ASTRA 文档：docs/plan/ASTRA-DESIGN-PREPARATION.md 的 B06。
分支：feat/astra-b05。规格来源：docs/development/2026-09-13-b04-b06-design-01/b06.go.txt（286 行声明式规格）+ docs/current/issue-page-create-api-contract.md（HTTP 契约 250 行）。

## 需求是什么

B06 要求在 Go 版中实现"从 Issue 页面发起的原子创建"完整链路：

1. 数据库：repomesh_issues schema 13 张表（迁移 0017，任务 U06.1 已完成并通过 18/18 重放验证）。
2. 访问观察面：仓库级读取/写入观察、App 安装观察、凭证可用性检查、事务内时间窗复核（任务 U06.2，commit 7061def）。
3. 核心服务：幂等的页面创建命令（解析 → 规范化 → 幂等查询 → 事务外观察 → 单事务提交），会话 + Issue + 主 ChangeSet + 来源卡片 + 待办 + 事件要么全成功要么全不留。
4. 查询与选项：创建条件投影（可选仓库列表）、已有会话列表、幂等结果查询。
5. 维护面：正文移除清扫（会话标题脱敏 + 占位墓碑 + 待办取消），绝不 DELETE。
6. web 层：4 条路由 + 浏览器路由放行 + 组合根装配。

## 做了什么

### 1. internal/issues/types.go（新建）

- ID 别名（IssueID/ConversationID/ChangeSetID/OperationID）、conversationChoice 接口（new/existing 两态）、pageInput、PageCommand + ParsePageCommand。

### 2. internal/issues/parse.go（新建）

- `parseNewInput`：字段白名单校验（未知字段 422；重复 JSON 键、非法 Unicode 400；重复键用 token 级递归扫描 hasDuplicateKeys 实现）。
- 字段规则逐条对齐契约 §3：title ≤200、description ≤20000、repositoryIds 非空去重 ≤100、acceptanceCriteria 可省略等价空数组、conversation 省略等价 {"mode":"new"}、existing 必须带 id。
- `canonicalize`：规范化输入（schemaVersion=1、仓库排序、空验收数组显式化、new 会话写成 {"mode":"new"}），幂等比较基于此而非原始字节。

### 3. internal/issues/errors.go（新建）

- Failure{Status,Code,Fields}、digest（SHA-256 服务端指纹，不信任客户端摘要）、newID（crypto/rand 铸造 ID）。

### 4. internal/issues/service.go（新建，核心）

- Service{pool, authorization, projects, budgets} + New。
- 12 相位事务管道（principalLocked → beforeCommit），transactionHook 可观测。
- `CreatePage` 管道：幂等查询 → 事务外 ObserveIssueAccess（网络调用不进锁）→ 单短事务：LockProjectPrincipal → LockForConfiguration → 幂等占位 INSERT ON CONFLICT（reserveCreationIdentity）→ 修订单分配（allocateIssueNumber，UPSERT 原子取号）→ 新会话插入 → Issue + 主 ChangeSet → work scope + protected scope（issue/conversation 双份）→ page_sources + conversation_cards → 单条 UPDATE 补齐全部身份列 + receipt → blocked 待办 → 事件流 + snapshot_invalidated 事件 → COMMIT（deferred 触发器验证聚合完整性）。
- `GetPageCreation`：404/410/200 三态查询。
- receiptWire 的 JSON 键（issueId/mainChangesetId/conversationId/initialConfigurationRevision）与 creation_receipt_consistent 触发器逐一对齐。

### 5. internal/issues/options.go（新建）

- `Options`：创建条件投影（仓库可选项、默认 new 模式、CanSubmit、分析能力 unavailable 基线）；不可读仓库隐藏、unknown 观察 503、仓库锁外观察（ObserveProjectRepositories 事务外网络调用）。
- `Conversations`：已有会话分页（只列未移除会话）。

### 6. internal/issues/maintenance.go（新建）

- `RemoveIssueBody`：正文移除清扫——会话标题脱敏（title→NULL + title_redacted_at，符合 conversations_title_state CHECK）、blocked 待办→cancelled（CONTENT_REMOVED + cancelled_at，符合 CHECK）、占位墓碑（removed_at 置位 + canonical/exact/receipt 置 NULL，符合 cleanup CHECK）、有外部事实（无 blocked 待办）即 409 中止。

### 7. internal/issues/accessors.go + destination.go（新建）

- 回执公开访问器（web 层只读）；ResolveDestination 把 issue_create 目的地解析为浏览器路径 /projects/{id}/issue-creations/{opId}。

### 8. internal/web/issues.go（新建）

- 4 条路由：POST /api/projects/{id}/issue-creations（幂等键必填，201/200）、GET .../issue-creations/{creationId}、GET .../issue-creation-options、GET .../issue-conversations。
- 复用 registerProjectRoute（no-store/no-referrer/单 Origin/15s 超时/CSRF）、readIssueInput（415/413/400）、projectIdempotencyKey、writeProjectError 错误封装（issues.Failure 经 projects.Failure 投影）。
- writeIssueCreation 按 §4 形状输出（creationId/status/projectId/createdAt/source/issue/mainChangeSet/conversation/links）。

### 9. internal/web/server.go + cmd/repomesh-web/main.go（修改）

- RunConfigured/handlerConfigured 追加 Issues 参数（scan/decision/skills 既有签名不动）；handlerWithAuth/RunAuthenticated 传空 Issues{}。
- main.go：issues.New(runtime.Pool(), runtime.Service, projectService, budgets) + SetIssueCreationDestinationResolver + issuesAPI 装配。
- projectBrowserRoute 新增 /projects/{id}/issue-creations/{creationId} 放行分支。
- 三个既有测试调用点同步补 Issues{} 参数。

## 影响范围

- 新增：internal/issues/ 七个文件；internal/web/issues.go。
- 修改：internal/web/server.go（签名追加）、internal/web/projects.go（浏览器路由分支）、cmd/repomesh-web/main.go（装配 + import）、三个 web 测试文件（签名适配）。
- 数据库：无新迁移（复用 0017 全部 13 张表；无 checksum 冲突）。
- 不影响既有 projects/models/skills/scan/decision 路由；Issues 零值时全部路由跳过。

## 修复逻辑（关键设计决策）

1. **原子性来源**：creation_operations 占位行（issue_id NULL）先 INSERT，聚合触发器 creation_aggregate_complete 对未提交占位放行；所有业务行插完后，单条 UPDATE 一次性补齐 issue_id/main_changeset_id/conversation_id/initial_configuration_revision/receipt（immutable_creation_identity 触发器禁二次修改），COMMIT 时 deferred 触发器整体校验——任何一行缺失或错位整个事务回滚。
2. **幂等比较基于规范化输入**：同键同输入重放返回 200 + 同一 receipt（从存储 receipt 反序列化）；同键不同输入 409 IDEMPOTENCY_CONFLICT；比较用 canonicalize 后的字节，键序/仓库顺序/省略验收数组都不影响判定。
3. **网络观察在事务外**：ObserveIssueAccess（读仓库 + App 安装）全部在锁外；事务内只做 CheckIssueObservation（行锁 + 观察一致性）、CheckIssueCredentialAvailability（排序锁双凭证版本）、CheckIssueObservationTime（DB clock 60 秒窗）三道廉价复核，缩短持锁时间。
4. **取号原子性**：project_issue_counters UPSERT + RETURNING 单语句取号，并发下不重号。
5. **触发器契约优先**：写库顺序完全由 0017 的触发器/FK 推导（会话先于 Issue、事件流先于事件、卡片 source_id=operation_id 等），receipt JSON 键名与触发器校验逐字对齐。

## 偏差清单（与 b06.go.txt 规格对比）

| # | 规格声明 | 实际实现 | 处理 |
|---|---------|---------|------|
| 1 | existing 会话模式完整支持 | existing 模式返回 409 CONVERSATION_UNAVAILABLE | 触发器 creation_aggregate_complete 要求 conversation.created_by_operation_id=本操作 id 且 title_origin_operation_id=本操作 id——已有会话（由别的操作创建）永远无法通过该触发器。契约本身已有此错误码。Options 仍展示两种模式；完整支持需要后续迁移放宽触发器（详见下方"遗留"） |
| 2 | CreationReceipt.MarshalJSON 输出完整 HTTP 形状 | 存储投影（receiptWire 六键）+ web 层 writeIssueCreation 单独组 HTTP 形状 | 存储层 receipt 键受 creation_receipt_consistent 触发器约束（六键），HTTP 响应形状（links/source 等）由 web 层组包，职责更清晰 |
| 3 | RunConfigured 按规格 §7 替换签名 | 追加 issueAPI Issues 参数（保留 scan/decision/skills） | 现网 RunConfigured 已比规格多三参，追加而非替换保证三个既有调用方最小改动 |
| 4 | checkBudget 用 modelbudget 通用入口 | 用 InspectFixedForCreation 的 CheckedFixed.Status（model_unavailable→409 CREATION_REQUIREMENTS_UNMET） | modelbudget 包确认无通用创建预算检查 API（只有 ReserveTest/ConsumeTest 等测试记账入口）；模型可用性检查语义等价 |
| 5 | replayAfterPreflight 内含完整 comparePrior | 简化为存储 receipt 反序列化 + 读取权核验 | 首次查询已含 canonical 比较分支（commitNew 内 existing.canonical 对比），重放路径无需二次比较 |
| 6 | RemoveIssueBody 还原 issue 正文 | 只做会话标题脱敏 + 占位墓碑 + 待办取消 | immutable_issue_facts 触发器禁止 UPDATE issues 的 title/description（只允许 revision/removed_at 变），issue 行本身冻结是 schema 级决定；移除语义通过操作墓碑 + 待办取消完整表达 |

## 简化清单（验收时知晓）

1. postgres_test.go 有 //go:build unix 标签，Windows 本机只做 go build + go vet + 非集成测试；issues 包的 DB 集成用例待 Unix 环境补跑。
2. RepositoryAnalysisCapability 固定 unavailable/INTEGRATION_NOT_AVAILABLE（分析服务是 B07+ 范围，契约允许本基线）。
3. reserveCreationIdentity 冲突后的短暂等待重试（契约 503 RESULT_UNCONFIRMED 分支）未实现轮询，直接走 409 重放检查路径。

## 对应 commit

- U06.2（access 观察面）：7061def
- 核心 issues 包：fab6d46（parse/canonicalize/管道）、eaf211e（options/maintenance）
- web 层 + 装配：147cefd

## 验证结果

- `go build ./...` 全仓通过（EXIT=0）。
- `go vet ./...` 全仓通过（无输出）。
- `go test ./... -count=1` 全部 ok（postgres_test.go 因 Unix 标签跳过，与 B05 一致）。
- 0017 迁移此前已通过 18/18 重放 + 10/10 破坏性触发器验证（U06.1，见 2026-09-13 设计目录）。

## 遗留（不阻塞验收）

- existing 会话模式的触发器冲突是 schema 级决定（0017 已冻结）：需要"已有会话可关联"时，须新迁移把 creation_aggregate_complete 的 conversation 校验拆成 title_origin 可空分支——属破坏性变更，需单独评审。
- 回滚方式：分支 feat/astra-b05 上 revert 147cefd / eaf211e / fab6d46 三个 commit 即可完整撤除 B06 服务层与 web 面（0017 迁移已在 main，不受影响）。
