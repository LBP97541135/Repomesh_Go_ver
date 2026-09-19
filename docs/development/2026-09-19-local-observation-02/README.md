# 全本地观测工作台实施与验证

日期：2026-09-19。用户明确要求全本地观测和评测，并允许 DeepSeek API 模型评审。部署决定见 [ADR 0024](../../adr/0024-local-observation-workbench.md)，使用方式见[工作台说明](../../current/local-observation-workbench.md)。本次是本地实施与行为验证，不宣称经过另一位实现者的独立复核。

## 改动与实际部署

- 新增 `internal/observeui/`，由 `repomesh-observe serve` 提供本地页面、证据查询、Trial／CSV、规则验收、OTLP 接收和可选 DeepSeek 评审。HTML、CSS、JavaScript 嵌入 Go 二进制，无 CDN。
- 复用原 `observepipe` 归档与验收器，新增受限的 Span／评审记录类型；业务模块、共享数据库和 DSH 调度不变。
- 新增 `scripts/observe-workbench.sh` 和本地 OTLP 配置。停止此前任务启动的官方云实验 Launcher，新工作台运行在 `127.0.0.1:18090`；原 Launcher 的源码和数据保留。
- 状态目录：`$HOME/.local/state/repomesh-observe-local`。旧的 `acceptance-20260919T214746` 和 `db-acceptance/20260919T214023.434147599` 以只读来源挂载，新结果保存到新状态目录的 `archive/`。
- DeepSeek Key 通过本地设置接口保存到 0600 私有配置，未写入仓库。测试读取模型列表，确认 `deepseek-flash` 可用；执行一次真实契约评审，结果及 3368 token 用量保存在本地。

## 验证结果

| 检查 | 实际结果 |
| --- | --- |
| Go 全量构建、vet | `go build ./...`、`go vet ./...` 通过。 |
| Go 全量测试，配置隔离 PostgreSQL | 587 通过、0 失败、2 跳过；跳过 `TestProjectBrowserServer`（专门浏览器入口）和 `TestRootFileOwner`（权限条件）。 |
| 相关包 race | `go test -race ./internal/observeui ./internal/observepipe ./cmd/repomesh-observe` 通过。 |
| 根工程要求的前端检查 | `npm --prefix web ci`、typecheck、test、build 全部通过，35 项测试通过；新工作台页面另由真实浏览器验证。 |
| HTTP／归档行为 | 三种真实本地固定产物分别为 fail／pass／unknown；SDK 导出 27 个 Span；重复上报不新增；重启后 Trial／Span 可查询。 |
| 异常与隔离 | 冲突 Span 批次拒绝、损坏证据来源报错、跨站写入拒绝、非法 Host 拒绝、任意文件路径拒绝；AI 失败记录 unknown，原规则结果不变。 |
| 浏览器 | 实际点击运行三组合、检查 8 个必需项、展开 HTTP 响应、证据下钻、搜索、CSV 下载、设置与移动端显示；0 浏览器错误，0 外部页面请求。 |
| 本机实际重启 | 当前档案的 3 个新 Trial、27 个落盘 Span、1 条真实 AI 评审保持；另有 2 个只读来源。 |
| 进程管理 | 停止→重建→启动成功；伪造旧 PID 标记不停止其他进程；端口占用时拒绝启动，不驱逐其他服务。 |
| 真实 DeepSeek | `/models` 成功；一次 `deepseek-flash` 评审完成并判 fail，引用旧契约消费与订单持久化金额证据；确定性验收仍为原来的 fail。 |
| 秘密与静态检查 | 页面设置不回读 Key，模型输入不包含 Key，新代码无该凭据；JavaScript 语法检查、文档链接与兼容 CRLF 的 diff 检查通过。 |

浏览器测试在运行实例中新建 3 个 Trial，与原档案的 3 个历史 Trial 分开。总视图有 56 个事件（54 个固定产物验收事件＋2 个真实隔离库发现历史事件），不是 56 次 Agent 运行。

详细运行证据位于本机状态目录 `checks/`：`go-test.jsonl`、`deepseek-acceptance.json`、`browser/browser-summary.json`、`browser/restart-summary.json` 和页面截图。上述文件不含模型 Key。官方 Collector 的旧证据和首轮报告不改写。

浏览器复验命令（会新增三个固定 Trial）：

```bash
PLAYWRIGHT_MODULE=/absolute/path/to/playwright/index.mjs \
REPOMESH_WORKBENCH_TEST_URL=http://127.0.0.1:18090 \
REPOMESH_WORKBENCH_TEST_OUTPUT=/absolute/private/browser-artifacts \
node scripts/test-observe-workbench.mjs
```

本机 Chromium 使用之前准备好的私有动态库目录，通过 `LD_LIBRARY_PATH` 注入；不新增系统安装。

## 剩余边界

全本地指观测、档案、查询及规则验收。用户允许的 DeepSeek 模型评审会把选中的报告发送给该模型服务，只有显式评审操作发起此请求。没有云 AgentSpace／Dataset／Evaluator 前置要求。

完整 DSH turn／模型／工具采集、多仓真实 Agent 产物评测、共享业务实例迁移、后台采集计划和任意自定义用例编辑仍未交付；页面明确标明 DSH 未接入。不能把固定折扣验收或一次模型判断写成 Agent 能力提升结论。

## 后续：RepoMesh 前端同步

按用户补充要求，观测页直接转到本地工作台，旧 Trace 深链仍可进入事件视图，不再读取云地址或旧双入口偏好。设置增加「模型与 API」三类用途入口；供应商表单、AgentTeams 平台向导及观测工作台分别明确配置归属。仓库分析与项目执行仍沿用模型供应商和项目固定配置，不新增用途绑定协议。

已构建并确认当前 8443 Web 提供新前端资源；本地工作台配套重启。HTTPS 控制台到 HTTP 回环工作台的顶层页面导航允许通过，跨站 API 读写继续被拒绝，相关边界测试通过。再次执行 Go build／vet、全量数据库测试（587 通过、2 条件跳过）、相关包 race、`frontend` build／lint 及 `web` typecheck／35 项测试／build，均通过。

浏览器实际验证三个用途入口、供应商字段标签、观测直达、旧 Trace 深链及独立观测模型设置；未向产品发送写请求或再调用模型。产品身份和只读响应为浏览器夹具，工作台及跳转为真实本机服务，不算真实 OAuth 或供应商保存验收。记录与截图在本机 `checks/frontend/`（`summary.json`、`model-usage.png`、`provider-usage.png`、`evaluation-settings.png`）。
