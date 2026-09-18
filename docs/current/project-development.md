# B03 项目管理开发说明

当前采用与实现状态见[当前交接](HANDOFF.md)和[施工计划](../plan/IMPLEMENTATION-PLAN.md)。B04 的 D01—D04 已采用并完成本地集成验收，见[B04 采用记录](b04-model-sources-adoption.md)；B05、B06 的后续范围仍待采用。原[B04—B06 设计交付](../development/2026-09-13-b04-b06-design-01/README.md)保留设计时的候选及复核范围。

用户已解除 B03 等待 B02 VERIFIED 的旧门槛，项目管理代码已合入主目录，当前为 `INTEGRATED_LOCAL_VERIFIED`，整批未 `VERIFIED`。历史 worktree `/home/xubohan/projects/Repomesh_B03` 为 `LOCAL_VERIFIED`；其证据保留在 `/home/xubohan/projects/Repomesh_B03/docs/development/2026-09-12-b03-01/`。B02 外部验收已按用户决定暂停，未来真实账号验证只使用主账号 A。本文描述已实现机制，当前状态见[交接](HANDOFF.md)，本轮文件集成见[集成证据](../development/2026-09-12-b03-integration-01/README.md)。

## 使用范围

认证后从首页进入“项目”。创建页先明确选择可读仓库，再填写名称、用途和配置引用；缺少模型或环境配置仍可保存。项目详情提供设置入口，资料、已有范围及配置按采用的页面顺序展示。增加仓库是明确增量，受限已有仓库只显示数量，不能用当前可见列表替换完整范围。

配置只有显式勾选本次更新时才重新绑定。省略配置保留原固定版本；默认或 secret 版本随后变化不会暗换已保存项目。B03 批次只实现内部固定版本消费与候选读取。后续 B04 已提供模型供应商保存和部署来源导入，采用范围见本文顶部链接；新库不会自动生成 profile/default。测试 fixture 不能替代真实模型调用或运行准备验收。项目的 `canCreateIssue` 保持 false。

创建和更新的未知结果保留原操作键与原输入，沿创建恢复页或更新恢复页查询。404 只表示当前无可见提交；成功回执的 revision 与时间固定，当前项目另读。输入丢失只允许查询；存储不可用时发送前展示可复制的浏览器恢复链接。注销、401、身份变化及失权清理敏感输入，晚到响应不能写回旧页面或存储。

HTTP 字段、错误码、分页及浏览器恢复路径的唯一规范仍是[首批浏览器契约](first-batch-browser-api-contract.md)。本文不另建 Schema，也不采用后续 Issue、模型设置或运行候选。

## 源码与持久化

- `internal/projects` 拥有项目事务、规范化输入、固定配置、不可变回执及分页。
- `internal/access/project_access.go` 提供 opaque principal 和仓库观察；token 留在 access，最终事务重核 binding/session/account 与必要的 credential revision/epoch 和观察时效。
- `internal/web/projects.go` 适配项目 HTTP，Web 入口组装服务。认证与项目共享当前会话规则，项目模块不复制认证表访问逻辑。
- `web/src` 的项目页面沿用认证清理机制，恢复记录按 actor、operation kind、key 和可选 projectId 隔离。

`0004_projects.sql` 在 `repomesh_projects` schema 保存 owner、项目、长期仓库定位与完整范围、固定配置版本、两类独立操作表及游标。项目、范围、固定配置、确切输入/规范化字节与回执同事务提交。catalog 单例锁使默认缺省与双引用绑定也有确定事务边界。配置版本不可变，原操作清理保留最小归属及 tombstone，旧键不能复活。

当前项目资格采用 owner-only 范围，没有成员或 grant 管理。新增仓库逐一真实核读权，denied/unknown 阻止整次新增；资料编辑或配置修复不要求恢复旧范围的全部读权。App 能力不足不会阻止保存待配置项目。

## 运行与验证

沿既有认证配置启动 Web 与 coordinator，先用新二进制显式迁移独立开发库至版本4；启动本身不自动迁移。B03 开发验证不迁移 B02 专用数据库，不替换其服务或浏览器。缺少认证配置时项目 API 返回503，`/readyz` 仍不报告完整业务就绪。

```bash
go build ./...
go test ./...
go vet ./...
npm --prefix web ci
npm --prefix web test
npm --prefix web run typecheck
npm --prefix web run build
pwsh -NoProfile -File scripts/verify-batch.ps1 -Batch B03 -PostgresBin /usr/lib/postgresql/17/bin
```

普通Go测试未设置测试库时会跳过PostgreSQL用例。B03批次脚本创建自己的SCRAM实例，拒绝把跳过计为通过，保存新证据并清理自身资源。BACKEND_LOCAL_VERIFIED仅表示脚本覆盖的后端范围。主目录最终集成验证另由下列后端、前端、浏览器、发布与独立复核共同支撑，不能把单一脚本结果当整批业务 VERIFIED。

主目录[最终独立复核](../development/2026-09-12-b03-integration-01/FINAL-INDEPENDENT-REVIEW.md)确认配置分页活动 Rows、sending/unknown 输入锁定和子请求明确失权清理共两项 P1、一项 P2 均闭环，无开放 P0/P1/P2。[最终后端](../development/2026-09-12-b03-integration-01/backend-verification-20260913-062605/checks.json)为真实 PostgreSQL 73 通过、0 跳过，完整 Go build/test/vet/race、HTTP与实际进程恢复通过；[前端](../development/2026-09-12-b03-integration-01/frontend-verification-20260913-062547/test.txt)28/28，[普通浏览器](../development/2026-09-12-b03-integration-01/browser-normal-20260913-062417/browser-result.json)25项及[空配置目录](../development/2026-09-12-b03-integration-01/browser-empty-20260913-062531/browser-result.json)1项通过。[最终发布验收](../development/2026-09-12-b03-integration-01/release-validation-20260913-062934/summary.json)验证新包 `dist/repomesh-0.3.0-b03-integrated-20260912-r1/` 的11项哈希、迁移与包内实际进程恢复；businessReady=false，不是外部 GitHub、部署、跨平台验收或整批业务 VERIFIED。

历史 worktree 的最终后端批次位于 `/home/xubohan/projects/Repomesh_B03/docs/development/2026-09-12-b03-01/backend/verification-20260912-193556/checks.json`，记录 72 个 PostgreSQL 测试通过、0 跳过，完整 Go build/test/vet、race、真实 HTTP 和两个实际 Web 进程恢复通过。browser-18 普通目录 23 个事件、browser-17 空目录 1 个事件均通过，前端独立复核位于 `/home/xubohan/projects/Repomesh_B03/docs/development/2026-09-12-b03-01/frontend/final-independent-review.md`。npm ci/typecheck、28 项测试/build 和配套发布验证完成；旧包位于 `/home/xubohan/projects/Repomesh_B03/dist/repomesh-0.3.0-b03-worktree-20260912-r1/`。这些旧结果不替代主目录集成后的新验证。

历史 worktree 最终独立复核位于 `/home/xubohan/projects/Repomesh_B03/docs/development/2026-09-12-b03-01/FINAL-INDEPENDENT-REVIEW.md`，无开放 P0/P1/P2。11 项包内哈希、包内迁移和实际双进程恢复只证明旧 worktree 的本地配套一致性，`businessReady=false`。旧失败和各次运行目录保留。

浏览器测试入口是仅存在于 `_test.go` 的 `TestProjectBrowserServer`，运行真实 PostgreSQL、session 和产品 handler，GitHub 使用测试 provider。其测试控制只在该测试进程可用，生产没有故障开关。每轮使用独立库、manifest、冻结资源及新浏览器上下文；白名单证据区分真实HTTP与客户端模拟错误渲染。该结果不能代替 B02 的真实 GitHub App 验收。

规划、架构与接口由 GPT-6 Astra 完成，实现由 gpt-5.6-sol 执行，未主要实现的模型复核。证据中的两轮私有 helper 事后声明偏差保留，不声称全程满足前置声明流程。B02.6 外部验收与 B03 主目录验证按各自真实结果判定。

09-12 历史检查点：当时 B04 为 `DESIGN_PREPARED_NOT_ADOPTED`，只准备[交接](../development/2026-09-12-b04-handoff-01/HANDOFF.md)和[下一会话 Prompt](../archive/2026-09-13-plan-organization/NEXT-TASK-B04-PROMPT.md)。该准备阶段已由 B04 采用与验收替代，当前以本文顶部链接为准。
