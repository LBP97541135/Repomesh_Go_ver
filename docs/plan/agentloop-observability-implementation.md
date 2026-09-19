# AgentLoop 观测与评估开发计划

日期：2026-09-19。依据 [Spec v0.2](../current/agentloop-observability-evaluation-spec.md)。本轮授权为「先写开发计划，确定改动范围和验收方式，再开始搭建」，不止交付计划。源码基线 `28c3167`，保留已有未提交修改，不切换或重启共享业务实例。

## 1. 本轮目标与边界

交付一个可以实际执行的初始链路：

```text
真实本地数据库中的发现链历史 → 独立采集 → 版本化事件／证据目录
                                                       ↓
折扣固定产物的正反例 → 独立行为验收 → 结果账本 → OTLP / AgentLoop CSV
                                                       ↓
                                              平台结果导入与核对
```

本轮覆盖 P0、本地 L1 的选仓历史切片，以及 P2 的遥测／数据适配。目标使用 AgentTeams 原生 DeepSeekHarness；不再加 CLI coding agent 采集功能。本轮不实现新的团队调度，不把固定产物验收写成 DSH 能力评测。

真实 AgentLoop 接入需要目标地域、空间及 OTLP 配置。当前会话环境及项目配置未发现这些信息，已向用户询问配置路径。先完成不依赖凭据的开发与实际本地验证；配置取得后继续真实上报，平台接收／可查询与本地接收器验证分别记录。无真实配置时不声称云端已接通。

## 2. 改动范围

| 范围 | 文件／位置 | 原因与约束 |
| --- | --- | --- |
| 发现链持久来源 | `internal/discovery/service.go` | 在已有事务保存处追加不可变观察快照；不改变发现、审批或派工决定，不调用云端。 |
| 选仓输入依据 | `internal/discovery/recall.go`、`steps.go` | 保存实际候选池的画像、扫描提交／时间及未选原因所需材料；不改变评分或范围算法。 |
| 单条新增迁移 | `internal/database/migrations/0045_observation_facts.sql` | 现有发现文档会覆盖，需独立追加历史；字符串业务身份不能强塞现有 UUID 聚合列。迁移编号在落盘前核查，禁止改写既有迁移。 |
| 本地事实写入／读取适配 | `internal/observability/` | 一处处理来源存储与读取，沿用项目权限归属；SDK、数据集和评分不进入业务服务。 |
| 独立工具 | `cmd/repomesh-observe/`、`internal/observepipe/` | 采集、证据持久化、稳定 Trace 身份、OTLP 导出、折扣验收、CSV 与平台结果规范化；可独立执行，不加入 Web／coordinator 的后台循环。 |
| 配置与评估资产 | `configs/agentloop.example.json`、`evals/` | 不含秘密的配置模板、固定案例与 rubric；云端凭据由环境／受控配置注入。 |
| 官方本地平台 | `scripts/observe-local.sh`、`scripts/agentloop-local.sh`、`scripts/test-observe-local.py`、`configs/otel-collector.local.yaml` | 按用户补充「本地搭建」安装锁定官方 Launcher 与 Collector；任务目录、校验摘要、进程身份核对，不配置 fake gateway 或猜测云空间。 |
| 依赖 | 根 `go.mod`、`go.sum` | 只为独立工具锁定标准 OpenTelemetry SDK／OTLP HTTP exporter；业务包不 import 云 SDK。 |
| 测试与记录 | 对应 Go 测试、`docs/development/2026-09-19-agentloop-observability-01/` | 保存实际执行命令、结果与限制；不重写历史实验。 |
| 文档入口 | 根 README、现行索引／HANDOFF、计划索引及 Spec 实施说明 | 更新使用方法与本轮准确完成状态，保留原有修改。 |

不改前端、原有模型配置、CLI 派发路径、AgentTeams 上游源码或共享数据库。只在独立测试库应用新增迁移；新生产者部署需要配套迁移与进程更新，本轮不据开发授权自动重启现有业务。

## 3. 实施顺序

1. 固定配置与字段契约：本地事件、不可变证据、去重、评分三态与导出状态；定义退出码和部署输入。
2. 增加事务内发现快照与扫描来源；验证回滚、重复请求、历史保留及项目／Issue 归属。
3. 开发只读采集与独立证据目录；按来源稳定身份去重，不用递增 ID 水位跳过晚提交事务。采集数据库失败不改变业务。
4. 接入标准 OTLP／HTTP protobuf，保留实际追踪身份及导出收据；配置或上报错误不得丢事件或写成功收据。导出默认只传结构化允许字段，不上传需求全文、命令、令牌或原始 payload。
5. 建立折扣正确、错误、装配不一致的独立 HTTP 行为验收；产物／数据版本与结果关联。使用明确标记的固定服务夹具，不代替真实 Agent 完成任务。
6. 导出一 Trial 一行 AgentLoop 数据集 CSV，附完整关联与验收摘要；实现平台结果回读的状态映射和重复结果识别。提供单维度评估 Prompt 与变量映射。
7. 有配置时执行 AgentLoop 冒烟，并检查真实返回与可查询证据；没有配置则保留明确的 `not_tested` 和待接入项。
8. 完成相关及必要全量检查，更新开发记录、计划状态与 HANDOFF。

## 4. 验收矩阵

| 编号 | 方法 | 必须通过 | 对应 Spec |
| --- | --- | --- | --- |
| T01 | 真实隔离 PostgreSQL | 发现历史在同事务追加；回滚不留事实；重复调用不新增相同事实；旧快照不被新候选覆盖 | AC01、AC05、AC28 |
| T02 | 真实隔离 PostgreSQL | 按精确项目／Issue 采集；重复扫描和晚提交不漏；只读消费者不写业务表；扫描依据可取得 | AC02、AC06、AC08、AC29 |
| T03 | 文件系统与单元测试 | 原子写入、同身份同内容幂等、同身份变内容拒绝、损坏文件可识别；事件与证据摘要匹配 | AC08、AC19、AC32 |
| T04 | 本地真实 OTLP HTTP 协议接收器 | protobuf 可解码、身份和时间匹配、只上传允许字段；失败／部分拒收不能标成功；重试身份不变 | AC07、AC19、AC20、AC27 |
| T05 | 本地独立 HTTP 价格／订单服务 | 正确组合金额符合预期；错误组合报告 8000／2000 差异；错装配不能通过 | AC11—AC15 |
| T06 | CSV／平台结果契约测试 | 中文、换行和 JSON 可回读；评估执行成功不等于业务成功；错误／未知不填零；重复结果不重复记账 | AC16—AC18、AC31 |
| T07 | 工具集成运行 | 一条 collect／验收→本地档案→OTLP→CSV→结果回读链；记录观测范围、实际命令和退出码 | AC01、AC19、AC33 |
| T08 | 真实 AgentLoop | 指定空间中找到上报记录；字段、已知正反例与 unknown 的评估回读可核对 | P2 云端；配置未提供时 NOT_RUN |
| T09 | DSH 原生真实执行 | 完整身份链、实际加载 Skill 和真实模型／工具计量 | 后续 DSH 接入验收，本轮不以夹具标 PASS |

执行受影响包测试与数据库测试、`go build ./...`、`go test ./...`、`go vet ./...`。新增工具和迁移不改浏览器契约，前端不因纯后端独立工具重复构建；如实施改变跨前后端／进程组装／发布链路，则扩为 AGENTS.md 要求的完整检查。

外部平台、本地 HTTP 协议、真实测试数据库和固定产物行为分别出结论。未知、未运行和失败保留，不用测试通过数量替代整批验收。

## 5. 交付与当前状态

- 计划和本地实施：已交付，具体检查见[实施记录](../development/2026-09-19-agentloop-observability-01/README.md)。
- 产品最小补丁：发现保存处追加历史、同步选仓输入保留；隔离测试库已验证，共享业务实例未升级。
- 独立工具：collect／discount／export／dataset／import-result／status／doctor 已实现。本地真实 Collector 及官方 AgentLoop Launcher 已启动。
- 云端接入配置：用户答复「本地搭建，未知」；官方本地 Launcher 使用真实 agentloop 模式，但 AgentSpace、云 Plan／Dataset／Evaluator 尚未配置。
- DSH 原生运行：待后续实际执行接入，不用固定产物夹具代替。
- 当前真实业务部署：本轮未修改，未据源码或本地测试宣称完整业务通过。

实际结果按本地事实层、独立验收器、OTLP 传输、官方平台就绪和未运行的云端／DSH 分层记录。

## 6. 用户变更：全本地工作台（2026-09-19）

用户进一步要求全本地，并允许 DeepSeek API 模型评审。采用 [ADR 0024](../adr/0024-local-observation-workbench.md)，替代首轮计划的默认云 Launcher 入口。实施 `internal/observeui/`、`serve` 子命令、本地静态页面和 `scripts/observe-workbench.sh`；沿用证据与验收器，不改业务状态或共享数据库。原云适配保留为可选项。

验收覆盖：页面实际操作、三态规则结果、证据下钻、CSV、本地 OTLP 落盘／幂等／冲突／重启、进程身份保护，以及可选 DeepSeek 真调用与错误保持 unknown。具体结果见[全本地实施记录](../development/2026-09-19-local-observation-02/README.md)。DSH 接入依旧单独验收。
