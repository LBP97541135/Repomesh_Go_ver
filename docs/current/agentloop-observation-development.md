# 观测与评估工具开发说明

**当前默认入口已切换到[全本地工作台](local-observation-workbench.md)。** 用户明确要求观测与评测本地运行，模型允许 DeepSeek API。下文首轮官方 Launcher 安装流程仅为可选云适配；普通本地使用无需执行。

本页说明已实现的本地切片，设计要求见 [Spec](agentloop-observability-evaluation-spec.md)，施工与验收范围见[开发计划](../plan/agentloop-observability-implementation.md)。本轮记录和真实平台状态见[实施记录](../development/2026-09-19-agentloop-observability-01/README.md)。

## 已实现范围

- 发现链保存时，在同一 PostgreSQL 事务追加不可变状态快照。同步 `Candidates` 路径保存完整输入池、扫描 fingerprint／时间和截断前评分，不因返回 limit 丢掉依据。
- 独立 `repomesh-observe` 读取精确项目／Issue 的已提交历史，保存内容寻址证据和稳定 Trace／Span ID。
- 用标准 OTel SDK 经 OTLP HTTP protobuf 导出允许的结构字段。需求原文、原始快照、命令和凭据不自动上传；接收失败不丢本地证据。
- 实际 HTTP 折扣夹具与独立验收器，分别报告 pass、fail、unknown；输出数据集 CSV，并规范化真实平台导出的评分结果。
- 官方 AgentLoop 本地 Launcher 与官方 OpenTelemetry Collector 可安装、启停和检查。两者用途不同；Launcher 的云连接尚未配置。

这是按需运行的管理工具，不是已部署到共享实例的常驻采集服务。DSH turn／模型／工具链路尚未接通；Agent 规划产物路径没有同步选仓路径的完整输入证据，不能据发现状态快照宣称其输入已采齐。

## 1. 本地环境与启动

普通构建要求沿用根 README 的 Go 版本。两项本地服务的安装脚本目前支持 Linux amd64，使用用户私有状态目录，不需要 sudo 或 Docker。

```bash
bash scripts/observe-local.sh install
bash scripts/observe-local.sh start
bash scripts/agentloop-local.sh install
bash scripts/agentloop-local.sh start

bash scripts/observe-local.sh status
bash scripts/agentloop-local.sh doctor
```

默认 Collector 为 `http://127.0.0.1:14318/v1/traces`，AgentLoop 工作台为 `http://127.0.0.1:18090/`。工作台默认真实 `agentloop` 模式、调度关闭；未配置时显示连接缺失，不启用 fake gateway。安装版本、摘要、端口覆盖、数据保留及停止方式见[本地运行手册](../development/2026-09-19-agentloop-observability-01/LOCAL-RUNBOOK.md)。

```bash
go run ./cmd/repomesh-observe help
```

退出码：0 表示操作成功或该 Trial 通过，1 表示确定的 Trial 验收失败，2 表示配置／执行错误或 Trial 不可判定。自动化调用必须保留 stdout 结果和退出码，不能遇到失败就丢弃结果。

## 2. 收集实际业务历史

新增迁移 `0045_observation_facts.sql` 必须与新版 Web／coordinator 配套。首轮隔离实验曾使用 0043；合入 main 时因主线已占用 0043／0044 而重编号，不能改写已有实验库的迁移历史来绕过校验。本轮只在独立测试库应用；当前共享业务实例未升级，直接采集尚无该迁移的库会明确失败。正常部署仍按根 README 的显式迁移和配套重启执行。

历史表不回填虚构的旧事件；开始使用新版保存路径后才有真实记录。采集工具只读，要求明确项目、Issue 和来源部署身份。建议专用只读数据库连接，授予相关 schema 的 USAGE 和所需表的 SELECT；不把管理员连接交给 Agent。

```bash
# 在当前终端通过受控配置设置此变量，不把连接秘密放在参数或日志中。
export REPOMESH_OBSERVE_DATABASE_URL='<已配套升级环境的只读连接串>'
go run ./cmd/repomesh-observe collect \
  --archive /absolute/private/observation-run \
  --source-id local-dev-01 --project '<project-id>' --issue '<issue-id>'
```

`source-id` 是稳定、非秘密的部署／数据库身份；换库或克隆环境要明确区分。同一作用域再次采集会重新读取历史并按来源去重，能发现晚提交事务；没有用最大 ID 当提交水位。首版全量读取单 Issue 历史，不适合作为无限历史规模的流式服务。

档案目录新建为 0700，已有宽权限或符号链接目录拒绝使用。证据文件内容摘要可复核，事件同身份同内容幂等、变内容拒绝，目录不得多人无约束共写。

## 3. 运行独立折扣验收

```bash
go run ./cmd/repomesh-observe discount --archive /absolute/private/discount-run --variant baseline
go run ./cmd/repomesh-observe discount --archive /absolute/private/discount-run --variant candidate
go run ./cmd/repomesh-observe discount --archive /absolute/private/discount-run --variant assembly-mismatch
```

每次命令创建独立本地价格／订单 HTTP 服务和临时数据目录，实际请求、保存并重读订单，再关闭服务。输入为 10000 分、应付比例 8000 基点；错误组合得 2000 分，正确组合得 8000 分。装配不符先报告 unknown，不运行目标组合的业务检查。

这些是 `synthetic_fixed_product` 验收器用例，不是 DSH 生成代码的结果，没有真实多仓构建、模型调用或 Agent 成功率结论。已有服务可用 `--manifest FILE` 指定，但服务接口与版本字段须满足固定契约；生产版本身份还需可信部署观察佐证，不能只相信候选自报。

`go run` 会额外包装非零退出码；脚本需要精确区分 1／2 时，先 `go build -o <私有路径>/repomesh-observe ./cmd/repomesh-observe` 再调用二进制。

## 4. Trace 导出与 AgentLoop 数据集

配置模板见 [agentloop.example.json](../../configs/agentloop.example.json)。端点必须是完整 traces URL；本地回环允许 HTTP，远端要求 HTTPS，不跟随重定向。不记录 endpoint 或认证头值，因为路径也可能包含秘密。

```bash
export REPOMESH_OTLP_TRACES_ENDPOINT=http://127.0.0.1:14318/v1/traces
go run ./cmd/repomesh-observe export \
  --archive /absolute/private/discount-run --config configs/agentloop.example.json

go run ./cmd/repomesh-observe dataset \
  --archive /absolute/private/discount-run --output /absolute/private/discount-dataset.csv
go run ./cmd/repomesh-observe status --archive /absolute/private/discount-run
```

认证需要时，配置文件只写 headers 环境变量名，实际值由秘密管理注入。头格式使用 `name=percent-encoded-value`，多头以逗号分隔；不能在文档、命令参数或测试报告中粘贴密钥。

导出仅在明确的 OTLP HTTP 200、合法响应类型且无拒收时保存收据；HTML 200、部分拒收、超时和重定向均不会写成功收据。已有收据防止本工具重复上报，仍先核验证据；响应丢失可能导致远端收到相同 ID 两次，不能承诺网络上的 exactly-once。

当前投影为「已提交事实／验收报告已形成」的时点 Span，`duration_kind=instantaneous_fact`，起止相同。真实业务耗时没有被采集时不会用采集／导出耗时冒充。每个检查通过证据引用定位报告中的 `check_id`，汇总 Span 以 Links 关联各检查。

`transport_acknowledged` 只表示指定 Collector／端点已按协议接收。当前本地 Collector 保存 JSONL，不会自动把 Trace 出现在本地 Launcher 的云端视图。云端查询、CSV 导入和 Judge 运行仍需 AgentSpace、数据集及评估器。

## 5. 平台评分回读

将 CSV 导入真实 AgentLoop 后，保存平台 dataset/item 与本地 Trial 的可信绑定；可用 trace 范围时，该 trace 必须已属于这个 Trial。平台导出的原始 JSON 与冻结 grader 配置分别输入：

```bash
go run ./cmd/repomesh-observe import-result \
  --archive /absolute/private/discount-run \
  --raw /absolute/private/platform-result.json \
  --binding /absolute/private/platform-binding.json \
  --grader /absolute/private/grader-config.json
```

原始结果和规范化评分分别保留；同一平台结果重复导入不新增统计身份，变内容拒绝。评估器 `success` 不等于业务通过，缺分／错误保持 unknown 和 null。平台评分作为独立记录，不覆盖已有确定性验收结论。没有配置时不得用手写的成功 JSON 冒充云端结果；本地契约测试样本只证明解析规则。

## 6. 验证与升级

```bash
go test ./internal/observepipe ./cmd/repomesh-observe
go test -race ./internal/observepipe ./cmd/repomesh-observe
# 先在终端设置 REPOMESH_TEST_DATABASE_URL，测试会创建和清理自己的临时库。
go test ./internal/discovery ./internal/observability ./internal/observepipe
python3 scripts/test-observe-local.py
```

需要对真实本地 Collector 做数据库链路验证时，设置 `REPOMESH_OBSERVE_TEST_OTLP_ENDPOINT` 为准确端点，再运行 `TestPostgresDiscoveryToArchiveAndLocalOTLP`。`REPOMESH_OBSERVE_TEST_OUTPUT` 可指定要保留的证据根目录，每次创建新子目录。默认测试不要求启动云平台。

升级分别验证业务来源格式、OTLP 映射及评分规则。来源格式不识别会拒绝猜测转换；旧证据和原始平台结果继续保留。运行记录未覆盖完整 Spec 的项目仍是未验收，不能由本地测试数量推定通过。
