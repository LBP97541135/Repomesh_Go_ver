# TypeSafe / Jev 测试与代码审查实施记录

源码起点：`801f9ea8f96d6a3f4ac36562eb8be312e4eaf07f`。
工作树：`codex/main-worktree-20260920-115016`。状态：LOCAL_VERIFIED（本工作树、隔离数据库、合成外部请求）；未部署到已有业务实例。

## 实施范围

- [Spec](../../current/typesafe-verification-spec.md)：按项目一个开关同时启用测试团队和仓库负责人的 Jev 辅助代码审查。
- 设置保存/替换/清除 Key；复用信封加密，读取不回显，配置修订防覆盖，真实合成连接检查有项目冷却。
- 固定官方 TypeSafe Skill，源码提交与文件摘要见 [source.json](../../../internal/typesafe/source.json)。工作树 `.agents/skills/typesafe-ai` 引用同一包；运行安装遇到不同内容不覆盖，已有完全相同的官方包可直接使用。
- 测试/审查运行使用短期、限额、绑定项目与配置修订的调用凭证；真实 Key 留在 Web。结果、输入来源、模型和概率独立记账，不修改审批或合并许可。
- 开启后新任务追加独立 `review_agent`，用 `code-review` 指令检查候选副本；审查失败也不改写任务经理门状态或消耗开发重试额度。
- 设置及测试/审核面板可见；修正相关集成证据六段引用解析、测试退出码矛盾和 FocusPanel 的项目 prop 接线。

## 验证环境

使用本轮独立 PostgreSQL 17 集群及由 testdb 创建/销毁的隔离数据库。没有迁移、重启其他工作树的业务数据库或服务。Go/npm 使用本工作树；浏览器依赖只在临时目录提取，未修改系统安装。

真实调用只发送合成材料。Codex CLI 验证要求实际 shell 读取官方 SKILL.md、调用产品 helper，再从产品 Web broker 与数据库读取真实 Jev 结果，检查 supported / contradicted / insufficient 三种判断。API Key 不传给 Codex。浏览器脚本挂载真实 React 设置/结果组件，把请求送往生产 Go 路由的隔离实例；它不是完整业务部署验收。

## 最终结果

- `go build ./...`、带独立 PG 的 `GOMAXPROCS=2 go test -p 1 ./...`、`go vet ./...` 全部通过；原始输出见 [build](go-build.txt)、[test](go-test.txt)、[vet](go-vet.txt)。降低测试并发后，既有 web 套件在完整基线快照和当前源码均通过，见 [baseline](web-go-baseline.txt)、[current](web-go-current.txt)。前面的失败仍保留在下方，不宣称这些历史用例完全无间歇问题。
- [最终真实报告](live-codex.json)：`passed=true`。Codex CLI `0.153.4` 在测试及代码审查两种用途下均实际读取官方 Skill、调用产品 helper，Web 以保存的 Key 调用真实 `jev-1.13.0` 并落库。每种用途一次请求、852 个输入 token，三个断言分别为 supported / contradicted / insufficient；此结果仅证明合成样本，不代表一般代码审查准确率。
- 同一轮 Chromium 验证 7 项操作通过：保存 Key 不回显、真实测试/审查结果展示、统一开关关闭/开启、替换 Key、刷新、清除并关闭、跨项目拒绝。无页面运行错误；[截图](settings-and-evaluations.png)为真实组件与隔离产品 API 的验收页。
- 已核对新增文档链接、上游包摘要和变更内容中的凭据；交付文件不含用户提供的 API Key。临时 Key 文件、独立 PG 集群及其连接凭据已清理；浏览器和 Vite 已退出，见 [cleanup](cleanup.txt)。

## 执行过程与保留的失败

- 官方 Skill、HTTP 协议、额度、权限、并发重放、关闭/过期/撤销、密钥存储和错误状态已有针对性测试；具体覆盖保存/替换/清除、不同项目拒绝、修订冲突、有效/过期/关闭的凭证、有限调用次数、幂等、错误结果及 Skill 文件归属。
- web 前端：类型检查、35 项测试与构建通过。
- frontend 控制台：构建及 lint 通过，保留原有动态导入/包体积与两处 React effect 依赖警告。
- 新功能相关包的 race 检查通过；`go vet ./...` 通过。
- [第一次真实验证](attempt-01.md)：样本预期有误，保留失败。
- [第二次真实验证](attempt-02.md)：测试/审查两条 Codex→Jev 用例通过；浏览器缺少库，整体失败。
- [第三次报告](live-codex-attempt-03.json)：测试用途通过，审查用途未取得已完成结果；当次诊断保存不足，不能据此归因于供应商或产品。后续脚本保存失败响应及受保护 CLI 日志。
- 全量 Go 首轮遇到测试集群 trust 认证不符合错误密码用例、旧模型 HTTP 并发保存用例失败；已改本轮集群为 SCRAM，未改产品认证行为。下一轮数据库检查通过，旧 Issue 候选发现用例曾间歇失败；随后在限定并发的基线与当前源码核对中通过，最终全量通过。未修改这些旧用例的业务断言。
- [第四次真实验证](attempt-04.md)：两条 Codex 用例通过；浏览器 harness 误拦截 Vite 模块，修复请求路径后完成最终通过。
- 历史 API 文档检查器仍硬编码 `/api/v1` 与旧目标表清单：[基线](api-doc-baseline.txt)和[当前](api-doc-current.txt)均为同样 52 项失败，已用完整 `git archive HEAD` 快照对比，新增失败为 0。未改写旧检查器为虚假全绿。

## 使用与限制

部署需应用迁移 0058、配套更新三个入口，并配置执行器 `REPOMESH_TYPESAFE_BROKER_URL`；步骤见[根 README](../../../README.md#可选jev-辅助测试与代码审查)。本次没有部署到已有业务实例。

当前能力是辅助判断：原始代码/日志文本及提交声明由 Agent 提供；Jev 的结构化输出不等于独立执行事实，不保证发现所有缺陷。真实 Codex 验证使用合成仓库材料，没有执行真实 GitHub 交付。既有跨仓集成使用 main 而未绑定候选组合的限制继续保留。

重现非外部检查：

```bash
go build ./...
go test ./...
go vet ./...
# 设置指向专用测试集群的 REPOMESH_TEST_DATABASE_URL 后，再执行数据库测试。
go test -race -p 1 ./internal/typesafe ./cmd/repomesh-coordinator ./cmd/repomesh-host-executor
npm --prefix web run typecheck
npm --prefix web test
npm --prefix web run build
npm --prefix frontend run build
npm --prefix frontend run lint
```

真实验证是 opt-in 的 `internal/web/TestTypeSafeLiveCodexSkill`，需要 `REPOMESH_TYPESAFE_LIVE_KEY_FILE`（受保护的 Key 文件路径）、`REPOMESH_TYPESAFE_HELPER_BIN`（本次构建的执行器绝对路径）和专用测试数据库。设置 `REPOMESH_TYPESAFE_LIVE_REPORT` 可保存不含凭据的 JSON 证据。浏览器验证另外传 `REPOMESH_TYPESAFE_BROWSER_SCRIPT`、`REPOMESH_TYPESAFE_WORKTREE`、Playwright 的模块路径及可用 Chromium；脚本为 [verify-typesafe-ui.cjs](../../../scripts/verify-typesafe-ui.cjs)。不要把真实 Key 作为命令参数或提交到仓库。
