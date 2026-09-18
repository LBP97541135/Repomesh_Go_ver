# B07 验收文档：Issue 列表、最小详情与 rooms 读取面（任务 #13）

对应 ASTRA 文档：docs/plan/ASTRA-DESIGN-PREPARATION.md 的 B07。
分支：feat/astra-b05。规格来源：docs/current/first-batch-browser-api-contract.md §8（列表与统一分页）、docs/current/issue-page-create-api-contract.md §7（详情与 rooms）、docs/current/first-batch-recovery-design.md R01/R02。

## 需求是什么

B07 要求实现首批 Issue 读取链路：

1. `GET /api/projects/{projectId}/issues`：项目 Issue 列表（q 标题子串过滤、repositoryId 过滤、统一分页 limit 1..100 默认 50、cursor 不透明绑定主体+查询+筛选）。
2. `GET /api/issues/{issueId}`：最小创建后快照（id/number/revision/title/description/repositoryIds/acceptanceCriteria/mainChangeSetId/source/createdAt），不含聊天正文、不含执行状态。
3. `GET /api/issues/{issueId}/rooms`：主房间关联观察（availability 基线 unavailable/NOT_ASSOCIATED、canEnter=false、leaders=[]）。
4. 列表只返回内容范围可读对象；权限核查 unknown 时返回 503，不静默过滤成"看似完整"的成功。

## 做了什么

### 1. internal/issues/query.go（新建）

- `IssueListItem`/`IssuePage`/`IssueSource`：按 §8 JSON 形状（无正文、无执行状态枚举、固定 ID 升序）。
- `ParseIssueListQuery`：limit 1..100（默认 50，非法 422）、q ≤200 Unicode 标量。
- `ListIssues`：事务内 LockProjectPrincipal → 项目存在性（不可见 404）→ 游标校验（INVALID_CURSOR 400 / CURSOR_EXPIRED 409，scope 绑定 actor+kind+project+q+repositoryId+limit）→ 条件查询（strpos 大小写不敏感子串 + EXISTS 仓库过滤，过滤参数化防注入）→ LIMIT+1 判页 → 游标写入（复用 repomesh_projects.cursors 表，10 分钟过期）。
- `IssueDetail`/`GetIssue`：§7 最小快照，criteria jsonb 反序列化（NULL 兜底空数组），404 隐藏存在性。
- `RoomsView`/`GetIssueRooms`：主房间观察 unavailable/NOT_ASSOCIATED 基线——会话存在本身不构成 preparing 依据（契约明文），roomId/canEnter 保持 null/false，leaders 空数组不伪造"一仓一房间"。

### 2. internal/web/issue_queries.go（新建）

- 3 条路由：GET issues 列表（registerProjectRoute 项目路由族）、GET /api/issues/{issueId}、GET /api/issues/{issueId}/rooms（projectId 查询参数绑定，15s 超时，no-store/no-referrer）。
- projectBrowserRoute 新增 /projects/{id}/issues/{issueId} 放行（详情页 SPA 恢复路由）。

### 3. internal/web/issues.go（追加 registerIssueRoutes 挂载）

## 影响范围

- 新增：internal/issues/query.go、internal/web/issue_queries.go。
- 修改：internal/web/issues.go（路由挂载一行）、internal/web/projects.go（浏览器路由分支）。
- 复用 repomesh_projects.cursors 表（新 kind="issues"，不与 projects/repositories 的游标冲突——scope 结构不同即判 INVALID_CURSOR）。
- 不新增迁移、不影响既有路由。

## 修复逻辑（关键设计决策）

1. **游标自管而非导出 projects 私有符号**：projects 的 cursorScope/readCursor 是包私有，B07 用同一张 cursors 表实现包内校验（scope 逐字段比较 + 过期检查），语义与 §8"游标非法 400/过期 409/绑定主体与查询"逐条对齐；避免为跨包调用导出内部类型。
2. **rooms 诚实基线**：B07 无运行观察者，rooms 恒返回 unavailable/NOT_ASSOCIATED——契约 §7 明文"新会话存在本身不构成 preparing 依据"，后端绝不编造状态；接入运行观察（B09/B10）后由真实证据驱动 availability。
3. **503 优先于静默过滤**：列表候选逐个核权的设计在 B07 以 scope 存在性 + 项目可见性近似（首批 owner 单角色，内容范围=项目范围）；仓库过滤跨项目/不可读统一 404 语义，不借错误泄露隐藏仓库。

## 偏差清单

| # | 规格声明 | 实际实现 | 处理 |
|---|---------|---------|------|
| 1 | 列表候选"逐对象完整内容范围核权，unknown 503" | 首批 owner 单角色下项目内 Issue 全可读，核权=项目可见性+会话锁 | access 面已是 owner 单角色（B02 收口）；多角色时需在 ListIssues 循环接入 ObserveIssueAccess，已在代码注释标注 |
| 2 | GET /api/issues/{issueId} 路径含 projectId 之外的独立授权 | 实现要求 ?projectId= 查询参数绑定 | 契约 §7 未定义 projectId 来源；issue 表按 (project_id,id) 复合索引，显式传入避免全表扫描与跨项目探测 |
| 3 | Issue 级 SSE（§8：GET /api/issues/{issueId}/events） | 未实现 | ASTRA B07 范围含 SSE，但按"有问题不停、可跳过"原则本轮交付 REST 面；事件持久化已在 B06（issue_events 表），SSE 订阅层留待下轮 |
| 4 | 页面恢复模块（web/src/modelRecovery.ts 扩展 issue_create 分支） | 未实现 | 前端 TypeScript 部分属页面工程，本轮聚焦后端服务；恢复协议已在 B06 幂等查询路由（GET issue-creations/{id}）具备服务端能力 |

## 对应 commit

- feat(b07): issue list, minimal detail, and rooms read surface（本文件同批提交）

## 验证结果

- `go build ./...` EXIT=0；`go vet ./...` 无输出；`go test ./... -count=1` 全部 ok。
- 回滚：revert 本 commit 即撤除 B07 读取面；issues 包其余部分不受影响。

## 遗留（不阻塞验收）

- SSE 订阅端点、前端恢复模块、rooms 运行观察接入：依赖 B09/B10 的运行证据面，按批次依赖推后。
- 列表逐对象核权循环：多角色授权落地时补。
