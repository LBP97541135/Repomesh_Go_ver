# 本地观测任务地图实施记录

日期：2026-09-20  
基线：`b5c0cf5ba4726830829460a15df9d0f6bc839737`  
工作分支：`codex/main-worktree-20260920-120900`

## 结果

本轮把独立观测工作台的默认入口改成任务地图。地图以一次冻结 Trial 或一次 OTLP Trace 为选择单位，通过三种视图回答“发生了什么、时间花在哪里、结论从何而来”：

1. 任务故事线：Trial 依次显示受测对象、确定性验收、事件、Jev 和样本复核；Trace 按真实 `parent_span_id` 排列 Span。
2. 性能泳道：Trace 使用同一真实时间轴；Trial 只显示分别观测到的验收／Jev 耗时，并明确它们不是一条共享运行时 Trace。
3. 证据链：把公开任务、固定组合、实际 HTTP、确定性检查、Jev 和复核样本串成索引，不把缺失项补成成功。

点击故事步骤或证据节点继续复用原详情窗。完整 Trial 仍可查看八项检查、实际请求响应、组合、Jev 概率／usage 和证据 JSON；完整 Trace 仍可查看所有 Span、属性、事件和 Links。主图对超大 Trace 最多展示前 80 个 Span并明确总数，原详情保留全部。左侧保留数据概览、Trace 与事件、评测与验收、数据集、指标与计量、本地聚类与归因、Rubric 评分、样本与复核、设置九个原页面。

实现只组合已有 `/api/catalog`、`/api/trace`、`/api/calls`、`/api/evaluations` 和 `/api/samples` 数据，不新增后端事实、评分规则、数据库迁移或模型调用。任务地图静态资源仍由回环工作台的既有 CSP 和同源边界提供。

## 验证

在独立 19091 预览实例完成以下检查；没有替换共享业务服务，也没有发起 Jev／DeepSeek 推理：

- `node --check`：`task-map.js`、`app.js`、`extended.js` 和浏览器脚本通过。
- `go test ./internal/observeui ./cmd/repomesh-observe -count=1`：通过；随后 `go test ./... -count=1` 全仓通过。
- `go build ./...`、`go vet ./...` 通过；`go test -race ./internal/observeui -count=1` 通过。
- 浏览器：任务地图为默认入口；九个原页面均可进入；固定 Trial 的故事线、性能与证据视图及八项检查／证据 JSON 下钻通过。
- 浏览器向真实 `/v1/traces` 写入一条明确标注的两层协议夹具；任务地图按父子关系展示两个 Span，并从完整 Trace 读到属性、`first-token` 事件和 Link。该夹具只验证协议与界面，不计为真实 Agent 执行。
- 1440 像素桌面和 390 像素移动端均无水平溢出；浏览器页面错误和外部请求均为 0。
- 浏览器汇总位于私有检查目录的 `task-map-browser-delivery/browser-summary.json`；截图包含故事线、证据链、完整验收详情、设置和移动端。

- 文档链接、差异空白和密钥内容扫描通过；没有把本机 Jev／DeepSeek Key 写入源码、报告或浏览器产物。

## 保留边界

- 当前预览已有真实档案结构和受控固定产物，但没有原生 DSH Trace；不能据此宣称真实跨仓任务、流式 TTFT 或人工效率改善。
- 协议夹具包含真实 OTLP 父子 Span、事件和 Link 的解析与持久化路径，但来源明确为 `browser-fixture`。
- Trial 与 Jev 的耗时来自各自记录，缺少共享 Trace 时不能判断两者先后、重叠或占总任务比例。
- 任务地图是读取与导航层。评分、标注、数据集版本和证据完整性仍由原接口及档案规则决定。
