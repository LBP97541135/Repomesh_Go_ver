# B08 验收设计：首批管理闭环整体验收矩阵

对应 ASTRA 文档 B08（验收设计批次）。分支 feat/astra-b05。
依据：docs/plan/IMPLEMENTATION-PLAN.md B08 行（登录、项目、配置、建项、查看、编辑全流程；故障和恢复矩阵；完整工程检查及打包；记录未接入运行状态）、docs/plan/DEVELOPMENT-START.md（失败验收清单 + 分阶段接入）、docs/current/first-batch-recovery-design.md R01—R03、各批验收文档（B04 收口、B05/B06/B07 ACCEPTANCE.md）。

**性质声明：本稿是 B08 的验收设计交付（矩阵本身），不勾选任何实际 PASS。** 实际执行依赖 B02—B07 全部本地集成，且外部账号验收仍处 PAUSED_BY_USER。未接入的运行能力保持真实未接入状态（/readyz 503、businessReady=false、rooms unavailable 基线）。

## 一、批次覆盖与验收入口

| 批次 | 本批状态（设计时刻） | 验收入口 | 本矩阵覆盖 |
| --- | --- | --- | --- |
| B02 认证 | 本地实现；外部 PAUSED_BY_USER | 2026-09-12-batch-02/README.md | A 组（本地会话面） |
| B03 项目管理 | INTEGRATED_LOCAL_VERIFIED | b03-integration-01/FINAL-INDEPENDENT-REVIEW.md | P 组 |
| B04 模型来源 | INTEGRATED_LOCAL_VERIFIED | b04-closeout-01/README.md | M1 组 |
| B05 测试/应用 | 本地实现（feat/astra-b05） | 2026-09-16-b05-* / ACCEPTANCE.md | M2 组 |
| B06 Issue 创建 | 本地实现（feat/astra-b05） | 2026-09-16-b06-issue-creation/ACCEPTANCE.md | I 组 |
| B07 读取面 | 本地实现（feat/astra-b05） | 2026-09-16-b07-issue-queries/ACCEPTANCE.md | Q 组 |
| B09+ 运行 | NOT_RUN | execution-integration-gates.md | 仅记录"未接入"，不设用例 |

## 二、全流程用例矩阵（登录 → 项目 → 配置 → 建项 → 查看 → 编辑）

每行格式：编号 | 前提 | 动作 | 预期 | 证据类别 | 故障注入点（可选）。

### A 登录与会话（B02 本地子集）

| # | 前提 | 动作 | 预期 | 证据 |
| --- | --- | --- | --- | --- |
| A-1 | 已配置 App 凭证 | 打开 /login 完成回调 | 建立 session cookie + csrfToken；GET /api/session 200 | 浏览器夹具 + HTTP 日志 |
| A-2 | 已登录 | 带正确 Origin+CSRF POST | 通过；缺 Origin → 403 ORIGIN_REJECTED | HTTP 断言 |
| A-3 | 已登录 | 注销后访问受限 API | 401 AUTHENTICATION_REQUIRED；前端清订阅/草稿 | 夹具 + 存储断言 |
| A-4 | — | /readyz | 503（真实未接入，禁止改 Ready） | HTTP 探针 |

### P 项目管理（B03）

| # | 前提 | 动作 | 预期 | 证据 |
| --- | --- | --- | --- | --- |
| P-1 | 登录 | POST /api/projects 带 Idempotency-Key | 201 首次 / 200 重放同键同输入 | HTTP + DB 快照 |
| P-2 | P-1 后 | 同键不同输入 | 409 IDEMPOTENCY_CONFLICT，原操作保留 | HTTP |
| P-3 | 有项目 | PATCH 增仓（明确 additions） | 200；新仓进入可读范围；失败整次回滚 | DB 快照 |
| P-4 | 表单打开 | 仓库范围/配置变化后提交 | 409 PROJECT_CONTEXT_CHANGED / PROJECT_REVISION_CONFLICT；保留输入 | HTTP |
| P-5 | 有项目 | GET 列表/详情 | 骨架→数据；空列表显示创建入口，不显示假数据 | 浏览器夹具 |

### M1 模型来源（B04）

| # | 前提 | 动作 | 预期 | 证据 |
| --- | --- | --- | --- | --- |
| M1-1 | 登录 | 保存 Provider（含 API Key） | 201；Key 进 secrets，不进 sessionStorage/localStorage | 存储断言 |
| M1-2 | 已保存 | close 已提交槽位 | 返回 committed 原结果 | HTTP |
| M1-3 | 已保存 | 迟到保存（终结后） | 409 MODEL_SAVE_CLOSED | HTTP |
| M1-4 | 双账号 | B 读 A 的原保存 | 404 不泄露存在性 | HTTP |

### M2 模型测试与应用（B05）

| # | 前提 | 动作 | 预期 | 证据 |
| --- | --- | --- | --- | --- |
| M2-1 | 模型可读 | 预览 | 200 canSubmit 按额度/占用；unknown≠零配额（UnknownQuotaObservation） | HTTP |
| M2-2 | 可提交 | 提交测试（幂等） | 202；重放同 testId；不二次外发 | HTTP + coordinator 日志 |
| M2-3 | 测试 unknown | 提交前预览 | canSubmit=false + TEST_ALREADY_OUTSTANDING + existingTestId 定位（仅当仍可读） | HTTP |
| M2-4 | 应用到项目 | POST model-applications | 只换模型，execution 原引用保留；配置版本原子替换 | DB 快照 |
| M2-5 | 预算耗尽 | ReserveTest | LIMIT_EXCEEDED；窗口行锁不超 daily_limit | DB 并发 |

### I Issue 创建（B06）

| # | 前提 | 动作 | 预期 | 证据 |
| --- | --- | --- | --- | --- |
| I-1 | 项目有固定配置 | POST issue-creations | 201 Created + Location + 三 ID（issue/cs/conv）+ 回执；无伪造消息 | HTTP + DB 快照 |
| I-2 | I-1 后 | 同键同输入重放 | 200 同 receipt，不新增会话 | HTTP |
| I-3 | I-1 后 | 同键不同输入 | 409 IDEMPOTENCY_CONFLICT | HTTP |
| I-4 | 创建中 kill 进程 | 事务中断 | 无半套业务记录（13 表聚合触发器保证）| DB 一致性查询 |
| I-5 | 项目缺模型配置 | 提交 | 409 CREATION_REQUIREMENTS_UNMET（MODEL_CONFIG_MISSING） | HTTP |
| I-6 | 选已有会话 | 提交 | 409 CONVERSATION_UNAVAILABLE（B06 偏差清单 #1） | HTTP |
| I-7 | 提交后 | GET issue-creations/{id} | 200 原结果；删除后 410 不复活 | HTTP |

### Q 查看（B07）

| # | 前提 | 动作 | 预期 | 证据 |
| --- | --- | --- | --- | --- |
| Q-1 | 有 Issue | GET issues 列表 | §8 形状；无正文/无执行状态；ID 升序 | HTTP |
| Q-2 | 有 Issue | q/repositoryId 过滤 | 子串命中；非法 limit 422 | HTTP |
| Q-3 | 翻页 | cursor 换查询参数 | 400 INVALID_CURSOR；过期 409 CURSOR_EXPIRED | HTTP |
| Q-4 | 有 Issue | GET /api/issues/{id} | §7 最小快照字段齐 | HTTP |
| Q-5 | 有 Issue | GET rooms | unavailable/NOT_ASSOCIATED 基线；canEnter=false；leaders=[]（不伪造 ready/preparing） | HTTP |
| Q-6 | 切换页面 | 晚到响应 | 前端代次丢弃（页面工程，SSE 未接前以代次规则验证） | 夹具 |

### E 编辑与失败恢复（跨批）

| # | 前提 | 动作 | 预期 | 证据 |
| --- | --- | --- | --- | --- |
| E-1 | 各写操作 | 响应丢失后按原 ID 查询 | 原操作身份不变；404 仍未知；有权 410 不复活 | HTTP 序列 |
| E-2 | 会话中 | 401/失权 | 敏感草稿/派生缓存清；localStorage 最小索引保留 | 存储断言 |
| E-3 | 进程重启 | 事务/待办领取之间 kill | 恢复不重复副作用、不丢业务事实、未知不当失败释放责任 | 重启脚本 + DB |
| E-4 | 换账号 | 用 B 恢复 A 操作 | 不认领；原主体重登才可查 | HTTP |

## 三、故障注入点清单

1. **HTTP 层**：丢响应（drop 2xx）、超时（15s 服务端 / 客户端 abort）、413  oversized body、重复 JSON 键 400。
2. **事务层**：COMMIT 前进程 kill（验证聚合触发器回滚）；deferred 触发器违规注入（手工 SQL 改行验证 12 张表联动拒绝）。
3. **锁与并发**：双连接同 Idempotency-Key 并发提交；同项目配置 PATCH 与 Issue 创建并发（验证 initial_configuration_revision 绑定完整版本）。
4. **外部依赖**：GitHub API 断网（观察面 → 503 AUTHORIZATION_UNCONFIRMED，绝不伪写无权）；DB 连接中断（unavailable() 路径）。
5. **进程生命周期**：web/coordinator/host-executor 各自在写入中途 SIGKILL；重启后待办不重复领取（test_handler_leases 租约验证）。

## 四、证据标准与发布条件

**证据类别**（每组保存命令、版本、退出码、结果文件）：
- 工程检查：`go build ./...`、`go vet ./...`、`go test ./... -count=1`、`npm --prefix web ci/typecheck/build`（退出码 + SHA-256）。
- DB 实测：独立 PostgreSQL（migcheck 容器同版 pg17），SQL 快照 + 断言脚本；破坏性触发器用例显式 BEGIN/ROLLBACK。
- HTTP 序列：curl/夹具输出（含错误码、Location、幂等行为），requestId 记录。
- 浏览器夹具：Playwright（本机 playwright-core + Chromium），结果 JSON + 白名单 SHA-256；失败轮保留不改写。
- 发布包：`dist/` 产物 + SHA-256；拒绝覆盖既有发布目录。

**发布条件（B08 实际执行时全部满足才能标 INTEGRATED_LOCAL_VERIFIED）**：
1. 上述矩阵 A/P/M1/M2/I/Q/E 全部用例 PASS（或明确 DEFERRED 并列影响单元）。
2. race 检测通过（`go test -race ./...`）。
3. 浏览器夹具最终轮 PASS，unexpected 白名单为 0。
4. /readyz 仍 503、businessReady=false、rooms unavailable 基线——未接入运行状态如实记录。
5. 独立复核（非实现者）无开放 P0/P1/P2。

**明确不 PASS 的项**（保持真实状态）：真实账号 OAuth（PAUSED_BY_USER）、真实模型调用（B09 G1/G2 未做）、真实执行/派工（B10 G3/G4 未做）、跨仓恢复（B11 G5 未做）。

## 五、与既有批次验收的映射

| DEVELOPMENT-START 失败验收要求 | 本矩阵用例 |
| --- | --- |
| 同键同输入并发一次事实；不同输入拒绝 | P-2、I-2、I-3、注入点 3 |
| 创建与配置应用并发绑定完整修订 | 注入点 3（B06 initial_configuration_revision 校验） |
| 丢响应/晚到/输入丢失原身份不变 | E-1、I-7 |
| Key 保存与终结互斥 | M1-2、M1-3 |
| 失权/换账号/晚到不泄露 | A-3、E-2、E-4、M1-4 |
| 进程重启恢复 | E-3、注入点 2/5 |
| 测试未知不重发 | M2-2、M2-3 |
| 应用保留 execution | M2-4 |
| 不伪造运行就绪 | Q-5、A-4、发布条件 4 |

## 六、执行注意

- 本矩阵执行时逐项记录到 `docs/development/<执行日期>-b08-closeout-01/`，格式沿 b04-closeout-01（工程检查表 + 夹具目录 + 独立复核）。
- 任何用例失败：修复后重跑受影响检查；失败轮次保留证据，不改写为 PASS。
- 不以验收设计名义增加产品功能、重写既有批次、执行真实账号/模型验收或修改部署（ASTRA B08 排除项）。
