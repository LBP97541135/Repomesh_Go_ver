# 本地观测环境运行手册

日期：2026-09-19。范围：Linux x86_64／WSL2 上的独立观测环境。依据 [实施计划](../../plan/agentloop-observability-implementation.md)；能力边界与官方来源见 [平台就绪说明](PLATFORM-READINESS.md)。本环境不重启 RepoMesh 业务进程，不替换 AgentTeams，不修改已有业务库。

## 1. 已启动的两个服务

| 服务 | 地址 | 实际用途 |
| --- | --- | --- |
| 官方 OpenTelemetry Collector Contrib 0.161.0 | `http://127.0.0.1:14318/v1/traces` | 接收 OTLP HTTP Trace，保存到本机 JSONL。没有云端 exporter。 |
| 阿里云官方 AgentLoop Local Experiment Launcher 0.1.0 | [本地工作台](http://127.0.0.1:18090) | 本地 Settings、连接、计划入口和运行历史；`gatewayMode=agentloop`，定时调度关闭。 |

两者职责独立。Collector 的 HTTP 200 与文件落盘，不等于 AgentLoop 已可查询；Launcher 页面和健康检查通过，不等于已取得云端计划或完成评估。当前没有设置 Region、AgentSpace、AK/SK，没有创建云端实验。

本机状态根目录为 `/home/xubohan/.local/state/repomesh-observe-20260919`，权限 `0700`。程序、下载、缓存、Python、上游源码、SQLite 与日志都在该目录；未使用 sudo、Docker 或全局 Python／Node 包安装。脚本只操作记录中匹配 UID、启动时间、可执行文件和命令行的进程，停止时使用 Linux pidfd 防止 PID 重用；不会按名称批量杀进程。

## 2. 安装与启停

前置：Linux x86_64、Bash、Python 3.10+（Linux pidfd）、Git、Node.js 22 与 npm、可访问 GitHub／PyPI／npm 的网络。以下命令从仓库根目录执行。

```bash
# 首次安装；后续 start 不会重新下载或自动升级。
bash scripts/observe-local.sh install
bash scripts/agentloop-local.sh install

# 检查官方 Launcher 的运行依赖与前端资源。
bash scripts/agentloop-local.sh doctor

# 启动；重复执行复用当前进程。
bash scripts/observe-local.sh start
bash scripts/agentloop-local.sh start

# JSON 输出包含 PID、实际端点、日志与 Trace 文件路径。
bash scripts/observe-local.sh status
bash scripts/agentloop-local.sh status

# 优雅停止当前专用实例，保留数据与证据。
bash scripts/agentloop-local.sh stop
bash scripts/observe-local.sh stop
```

如需独立目录或端口，在同一终端设置，后续安装、启停与查询保持相同目录。目录需当前用户拥有且权限 `0700`；端口被占用时拒绝启动，不停止占用者。Windows 浏览器通常可通过 WSL localhost 转发访问；此处验证的是 WSL 内的 HTTP，不把跨系统浏览器访问视为已测。

```bash
export REPOMESH_OBSERVE_STATE="$HOME/.local/state/repomesh-observe-another-run"
export REPOMESH_OTLP_PORT=14319
export REPOMESH_AGENTLOOP_PORT=18091
```

这两个安装脚本锁定已验证版本，不跟随 `latest`。升级应先停止本专用实例，在新目录验证新版本与旧证据回放，再明确修改锁定信息；脚本发现上游工作区偏离锁定提交时会拒绝，保留已有内容。不要在运行期间覆盖二进制或复用不明来源的数据目录。

## 3. Trace 接收与证据位置

[Collector 配置](../../../configs/otel-collector.local.yaml) 只监听回环 HTTP，启用内存限额和文件 exporter，关闭自监控指标监听。OTLP SDK 可使用下列精确 Trace 端点；如果 SDK 配置项要求基础地址，使用不带 `/v1/traces` 的地址，避免重复拼接路径。

```bash
export OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=http://127.0.0.1:14318/v1/traces
export OTEL_EXPORTER_OTLP_TRACES_PROTOCOL=http/protobuf
```

这些变量供标准 SDK 使用；RepoMesh 独立工具应按自己的配置接口显式设置端点。不要假定设置 shell 变量会自动改变已经运行的 Web／coordinator。

每次 Collector 启动新建 `collector-runs/<UTC启动标识>/`，其中有配置快照、`collector.log`、`traces.jsonl`，不会截断前一次记录。文件 exporter 是上游 alpha 组件，JSONL 用于本地协议验收；长期事实仍以 RepoMesh 独立事件／证据档案为准，不能让业务历史依赖该文件格式或内存中的 Collector 队列。SDK 的上报成功还需在文件中核对 traceId／spanId。

可用下面的只读命令查看当前文件的 Span 摘要，不打印原始模型或工具内容：

```bash
python3 - <<'PY'
import json, os
from pathlib import Path
state = Path(os.environ.get('REPOMESH_OBSERVE_STATE', str(Path.home() / '.local/state/repomesh-observe-20260919')))
record = json.loads((state / 'collector-process.json').read_text())
for line in Path(record['trace_file']).read_text().splitlines():
    for resource in json.loads(line).get('resourceSpans', []):
        for scope in resource.get('scopeSpans', []):
            for span in scope.get('spans', []):
                print(span.get('traceId'), span.get('spanId'), span.get('name'))
PY
```

安装来源与校验值分别保存在 `collector-install.json`、`agentloop-install.json`。Launcher 的 SQLite、加密 Secret Store 和自动创建的 `master.key` 位于 `agentloop-data/`；密钥不是云凭据，不应加入仓库或输出到报告。`agentloop-runs/<UTC启动标识>/launcher.log` 保存各次启动日志。归档或清理本地数据前先停止对应服务；本轮没有自动清理策略或守护重启服务。

## 4. 本轮实际验证

2026-09-19 21:21 UTC，本机完成：

| 检查 | 实际结果 |
| --- | --- |
| 官方 Collector 归档校验、版本与配置校验 | PASS，0.161.0，校验值见下方就绪说明。 |
| 标准 Python OTLP HTTP protobuf exporter → 真实 Collector → JSONL | PASS，1 个已知 Span；Trace `c0d3a88737f1ccf714d6697acf3a3435`，Span `08cfea4ea705b071`。仅验证传输。 |
| Launcher 官方锁定依赖安装、前端 TypeScript 检查与生产构建、doctor | PASS。 |
| Launcher HTML、JS、CSS | PASS，三个资源均 HTTP 200。 |
| Launcher `/api/v1/health` | PASS，`status=ok`、`gatewayMode=agentloop`、`schedulerEnabled=false`。 |
| Launcher Settings／Plans 未配置时的状态 | 如实阻止：Settings HTTP 404、Plans HTTP 503，均 `CONNECTION_NOT_CONFIGURED`。 |
| Launcher 重复 start、优雅 stop、立即重启 | PASS；SQLite 与本地 Master Key 保留。首次验证发现端口 TIME_WAIT 的预检查误报，已通过 `SO_REUSEADDR` 修复并重测。 |
| 空状态、未安装、错误 PID／无关进程、共享或符号链接目录 | `python3 scripts/test-observe-local.py`，4 项通过。该测试不代替真实平台验收。 |
| AgentLoop 云计划／数据集／评估器、逐题结果回读 | NOT_RUN：未配置目标云空间及凭据。 |
| 原生 DeepSeekHarness 执行与计量 | NOT_RUN：本地平台启动不代表 DSH 业务接通。 |

本机机器可读结果位于状态根目录 `local-platform-acceptance.json`。RepoMesh Go exporter、数据库采集及折扣评估的验收由本批主记录单独汇总；上述一个传输 Span 不证明业务改进。

## 5. 后续正式 AgentLoop 接入

在本地工作台 Settings 配置用户自己的地域、AgentSpace 和受控凭据，并使用平台提供的真实接入点。云端需要已有的 `OFFLINE` Plan、Dataset、Evaluator 及正确变量映射。随后建立真实本地 Agent 连接，运行受控用例，并回读同一 recordId 的逐题结果、Trace 关联和评分状态。

本轮没有执行该步骤，没有用 `fake` 模式填充成功记录。若后续需要跨平台格式转换，应以真实返回与官方接口核对，不能把 RepoMesh 的规范化导入格式当作 AgentLoop 原生导出格式。
