# AgentLoop 观测与评估首轮实施记录

2026-09-19。依据[开发计划](../../plan/agentloop-observability-implementation.md)与 [Spec v0.2](../../current/agentloop-observability-evaluation-spec.md)，先明确范围再实施。用户补充使用本地部署、当前云配置未知，并授权多 agent 分工开发和独立验收。

源码基线为 `28c3167` 加既有工作区修改。原有插件移除、开发机脚本、README 与环境记录保留。本轮没有提交或推送 Git，没有迁移／重启共享业务实例，没有调用真实模型、创建 PR 或进行 DSH 派工。

## 交付结果

| 层次 | 实际结果 | 不能据此推出 |
| --- | --- | --- |
| 官方 AgentLoop 本地工作台 | 在 `http://127.0.0.1:18090/` 实际运行，`gatewayMode=agentloop`，非 fake；调度关闭 | 已配置云空间、已执行官方云评估。 |
| 官方 Collector | `http://127.0.0.1:14318/v1/traces` 实际接收 OTLP 并保存 JSONL | AgentLoop 云端已索引这些数据。 |
| RepoMesh 来源 | 新迁移 0043；发现保存与历史同事务，精确项目／Issue，保留同步候选输入 | 所有 DSH 输入、Skill、模型和工具轨迹已经采齐。 |
| 独立证据工具 | collect、discount、export、dataset、import-result、status、doctor 可运行 | 已成为当前生产环境常驻采集服务。 |
| 折扣验收 | 正确、错误、错装配三类固定 HTTP 产物，分别 pass／fail／unknown | Agent 已自动修复多仓业务，或成功率已提升。 |
| 平台结果处理 | 变量映射与 rubric、CSV 导出、平台结果解析／绑定及错误路径契约测试 | 已经调用真实 AgentLoop Judge；样例 JSON 明确只是夹具。 |

安装、版本摘要、管理命令和部署边界见 [LOCAL-RUNBOOK](LOCAL-RUNBOOK.md) 与 [PLATFORM-READINESS](PLATFORM-READINESS.md)。日常工具操作见[开发说明](../../current/agentloop-observation-development.md)，用例与评估器文件见[评测资产](../../../evals/README.md)。

## 实际代码范围

- `internal/discovery/service.go` 在既有保存事务中追加已提交状态来源；`recall.go` 和 `steps.go` 保留实际输入池、画像与扫描 fingerprint／时间、截断前评分。没有更改选择阈值、权限范围、派发路径或 CLI runtime。
- `internal/database/migrations/0043_observation_facts.sql` 建立追加历史、精确归属外键和不可变约束；授权 purge 级联遵循既有本地事务标记。`internal/observability/facts.go` 集中写入与只读读取，没有云依赖。
- `cmd/repomesh-observe/`、`internal/observepipe/` 实现本地档案、SDK 生成的持久 Trace 身份、允许字段 OTLP 导出、独立 HTTP 验收及数据／评分处理。
- `scripts/observe-local.sh`、`scripts/agentloop-local.sh` 管理私有目录中的锁定官方软件；核对下载摘要和进程身份，不使用 sudo、全局安装或共享容器清理。
- `configs/` 与 `evals/` 提供无秘密配置、固定用例、rubric 和映射；根 Go 依赖锁定 OTel SDK／HTTP exporter `v1.46.0` 及其依赖，仅独立工具引用。

原前端、Web／coordinator 启动组装与发布脚本未修改。迁移计数及独立工具使用方式同步根 README。已生成的 0043 尚未应用到共享业务数据库，需之后按正式配套部署流程启用新保存路径。

## 端到端实际证据

使用构建后的 `repomesh-observe` 二进制，依次运行三种变体，核对退出码为 1／0／2；每次使用独立本地 HTTP 服务、临时订单数据和 Trial 身份。实际档案：

```text
/home/xubohan/.local/state/repomesh-observe-20260919/acceptance-20260919T214746/
```

| 变体 | 独立验收结果 | 汇总 Trace ID |
| --- | --- | --- |
| baseline | fail；订单金额 2000 分，期望 8000 分；实际保存值也不符 | `9b76be662ffd98af8dcb20aac4b101be` |
| candidate | pass；该用例全部 8 个必检项通过 | `4cd232b83b8e450ed2c6965cce9f803b` |
| assembly-mismatch | unknown；实际订单版本与目标组合不同，业务项不作为目标组合通过证据 | `cc1e99d77195d0ff11015429b1098a4a` |

每次 Trial 的 8 个检查与 1 个汇总形成 9 个时点事实，共 27 条。使用标准 Go OTLP HTTP protobuf exporter 发到真实 Collector，并从其落盘文件逐条比对 **27 个 Trace ID 全部存在**。重复导出为 `exported=0 / already_acknowledged=27`。CSV 有 3 行，身份与上述 Trial 一致，保留 pass／fail／unknown。

本轮机器可读摘要：

```text
/home/xubohan/.local/state/repomesh-observe-20260919/acceptance-summary.json
```

已将不含秘密的关键结果和最终工具二进制摘要保存为仓内 [local-acceptance-summary.json](local-acceptance-summary.json)。早轮档案保留在原目录；上表为完成复核修复后的再次验收。

真实数据库链路另外用 `TestPostgresDiscoveryToArchiveAndLocalOTLP` 验证：测试创建独立数据库和完整项目／Issue 聚合，调用实际 Analysis／Candidates 用例，读取 2 条已提交历史，重复采集新增 0 条，再导出到同一真实 Collector。首次测试误试图更新不可变 Issue 内容被现有约束拒绝，修正测试为直接使用合法创建夹具后通过；没有放宽产品约束。

```text
数据库测试保留档案：
/home/xubohan/.local/state/repomesh-observe-20260919/db-acceptance/20260919T214023.434147599/
```

这些是本地实际数据库／HTTP／OTLP 链路证据，不是云端查询或真实 Agent 执行证据。当前 Span 标记 `instantaneous_fact`，未采集到的运行时间和模型用量没有被补零冒充完整计量。

## 独立复核与修复

本轮先分别开发事实层、验收器与平台资产、本地官方平台，再由非对应实现者独立复核。实际报告：

- [FACTS-REVIEW](FACTS-REVIEW.md)：发现保存的锁与计划外键锁可能死锁，新增并发测试先复现 `40P01`，改用 `FOR NO KEY UPDATE` 后通过。
- [LOCAL-REVIEW](LOCAL-REVIEW.md)：官方版本、摘要、隔离生命周期、端口冲突和进程身份保护通过，现有 14318／18090 未被复核重启。
- [PIPELINE-REVIEW](PIPELINE-REVIEW.md)：发现并修复非 OTLP HTTP 200 误确认、跨 Trial Trace 评分误绑定、CSV 缺证据仍导出及已确认收据跳过输入证据检查；新增相应正反例。
- [EVALUATION-REVIEW](EVALUATION-REVIEW.md)：发现并修复其他字段类型错误抹去已知金额失败、空白证据引用产生确定诊断的问题；逐字段检查与非法数字边界经独立反例复验。

复核中发现 `Maintenance.Purge` 在删除主会话时存在既有外键错误：在零 observation 记录、以及移除本轮 observation schema 的隔离对照中均得到 `issues_main_conversation_fk / 23503`。本轮只验证新增表不阻止既有授权级联，未借此修改无关清除流程，也未宣称完整 Purge 通过。

## 检查结果与剩余边界

最终检查结果：

| 检查 | 结果 |
| --- | --- |
| `go build ./...`、`go vet ./...` | 通过。 |
| `go test -count=1 -timeout 180s -json ./...`，设置 `REPOMESH_TEST_DATABASE_URL` | **582 个通过、0 失败、2 跳过**，计数包含子测试。数据库用例自行创建和清理临时库。 |
| 两项跳过 | `TestProjectBrowserServer` 未启用浏览器服务器夹具；`TestRootFileOwner` 改变文件所有者需要额外权限。没有把它们计为通过。 |
| `go test -race -count=1 -timeout 90s ./internal/observepipe ./cmd/repomesh-observe` | 两包通过。 |
| 实际 PostgreSQL 用例→归档→真实 Collector | 2 条发现历史通过，重复采集不新增；见上方独立保留档案。 |
| 最终 CLI／HTTP／OTLP／CSV | 3 个 Trial 的结果符合预期；27 个 Trace ID 全部在真实 Collector 落盘找到；重复发送为 0；CSV 为 3 行。 |
| `python3 scripts/test-observe-local.py` | 4 项进程／路径保护测试通过。两项脚本 `bash -n` 通过。 |
| JSON／文档／差异 | 评测 JSON、相关本地链接、代码块与新文件空白检查通过；保留既有 CRLF，`git -c core.whitespace=cr-at-eol diff --check` 通过。 |

检查日志保存在：

```text
/home/xubohan/.local/state/repomesh-observe-20260919/checks/
```

前端未修改，没有重建被共享服务直接读取的静态资源。没有重跑历史上游实验或声称全部 Spec AC01—AC33 通过。

当前剩余限制：

- AgentSpace／地域／云凭据、官方 OFFLINE Plan／Dataset／Evaluator 未配置；本地 Launcher 已运行，但正式云端评估未执行。
- DSH 原生运行与多 Issue session／turn 映射、实际 Skill 加载、模型／工具计量待接通。
- 完整选仓输入仅覆盖同步 Candidates 路径；Agent 规划产物路径缺输入池时明确 `selection_input_pool` 缺失。扫描失败／失权等所有原因尚未完整采集。
- 本地工具是按需、单 Issue 全量重扫；持续大规模采集、生产存储保留及吞吐／开销尚未验收。
- 生产部署版本的可信核验、跨仓实际 Git 构建、留出任务和真实人工审查收益均未在本轮完成。

下一步先配置目标 AgentSpace 和真实 DSH 执行身份，再沿已建立的字段契约接入；保留本轮固定产物验收作为工具回归，不能将其升级为 Agent 成绩。
