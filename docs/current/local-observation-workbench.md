# 全本地观测与评测工作台

采用范围见 [ADR 0024](../adr/0024-local-observation-workbench.md)。当前默认入口是 RepoMesh 自带工作台，不再要求配置 AgentLoop 云空间。用户已明确：观测与评测在本机，模型评审允许使用 DeepSeek API。

## 启动与使用

从仓库根目录执行，要求已有根 Go module 的开发工具和依赖；运行中的工作台不需要 npm、Python 后端、Docker 或云端账户。管理脚本使用 Linux Python 3；也可直接运行 Go 二进制。

```bash
# 若之前启动过同端口的官方云实验 Launcher，先停止它。
bash scripts/agentloop-local.sh stop
bash scripts/observe-workbench.sh install
bash scripts/observe-workbench.sh start
bash scripts/observe-workbench.sh status
```

RepoMesh 控制台侧栏「观测」可直接进入工作台；「设置 → 模型与 API」区分仓库分析、AgentTeams 与观测评估，并提供观测模型的独立配置入口。

打开 <http://127.0.0.1:18090/>。`/settings` 也会进入新的本地设置页。

- **概览**：事件、Span、验收与 AI 评审数量，以及明确的接入状态。
- **Trace 与事件**：按名称、项目、Issue、Trial 或 Trace ID 搜索；查看原始证据、因果前序和 OTLP Span 父子关系。业务事实是时点事件，不冒充完整耗时轨迹。
- **评测与验收**：运行三个固定折扣组合或其中一个，查看检查期望／实际值、请求响应与固定版本。验收器实际启动回环 HTTP 服务，随后关闭。
- **数据集**：按档案下载含证据、rubric 与结果的 CSV，下载不会上传。
- **设置**：本机档案位置、OTLP 地址、可选 DeepSeek 模型和密钥；不需要 Region、AgentSpace 或阿里云 AccessKey。

本地固定用例和已有归档查询无需任何模型密钥。AI 契约评审由用户在具体 Trial 上触发，发送该条报告与固定 rubric；不会发送密钥以外的配置、任意业务库或整个证据目录。API Key 仅保存在私有档案根的 `model.json`，文件权限 0600，目录 0700；设置接口不回读密钥，错误不回显供应商响应正文。

默认模型为 `deepseek-flash`，设置中可填写账号支持的 DeepSeek 模型；「测试连接」读取 `/models`，不会产生生成请求。评审使用固定的 `contract-handoff/1` 规则、JSON 输出和 2048 输出 token 上限。输入、响应、用量、模型及每次尝试分别保存；无效输出、缺少确定判定引用、HTTP 失败或超时保留 `unknown`，不覆盖本地确定性验收。接口依据 [DeepSeek 官方 Chat Completions 文档](https://api-docs.deepseek.com/api/create-chat-completion/)。

## 存储、重启和旧档案

默认状态目录为 `$HOME/.local/state/repomesh-observe-local`，`archive/` 存证据和结果，`bin/` 存二进制。重启不清理数据。

```bash
bash scripts/observe-workbench.sh stop
bash scripts/observe-workbench.sh start
# 更新源码后：stop → install → start
```

`REPOMESH_WORKBENCH_STATE` 可指定另一个专用私有目录，`REPOMESH_WORKBENCH_PORT` 可改端口；后续管理使用同一目录。脚本核对 UID、执行文件、命令和进程启动时间，使用 pidfd 停止自己的进程，拒绝占用中的端口，不清理其他服务。

可在状态目录建立 `read-archives.json`，内容为已有私有档案的绝对路径数组。重启后这些档案以只读来源展示，新验收和 AI 评审只写当前 `archive/`。服务不会从浏览器接受任意文件路径。

也可直接启动：

```bash
go run ./cmd/repomesh-observe serve \
  --archive /absolute/private/active-archive \
  --read-archive /absolute/private/previous-archive \
  --addr 127.0.0.1:18090
```

当前机器已挂载上一轮三个固定产物的验收档案及隔离数据库的两个真实发现历史事件；保留原始记录，不改写旧验收结论。

## OTLP 与业务历史

工作台自身提供 OTLP HTTP 接收，不依赖独立 Collector。仅接受回环访问、protobuf 或 OTLP JSON；每次请求上限 16 MiB／10000 Span，当前要求未压缩请求。同 Trace／Span 身份同内容幂等，变内容拒绝；落盘错误不确认成功。

```bash
export REPOMESH_LOCAL_OTLP_TRACES_ENDPOINT=http://127.0.0.1:18090/v1/traces
go run ./cmd/repomesh-observe export \
  --archive /absolute/private/active-archive \
  --config configs/observe-local.example.json
```

页面执行验收会直接生成可查看的事件、报告及证据；OTLP Span 视图展示通过接收接口送达的记录，导出是显式操作。独立 Collector 可继续用于其他本机来源，但全本地路径不使用其云端转发配置。

业务历史仍通过 `collect --source-id ... --project ... --issue ...` 精确采集，参照[原工具说明](agentloop-observation-development.md)。须先在对应授权环境升级迁移 0045；工作台不迁移或修改共享业务数据库。

## 本地 HTTP 契约

这是独立管理工具的接口，不增加产品 Web／MCP 协议。

| 接口 | 行为 |
| --- | --- |
| `GET /api/health` | 服务身份与进程存活，不宣称业务或 DSH 就绪。 |
| `GET /api/catalog` | 校验档案后返回来源、事件、Trial、Span、评审；损坏来源单独报错且不计入正常统计。 |
| `GET /api/evidence?archive=…&ref=…` | 仅取配置来源内有效摘要引用的 JSON。 |
| `GET /api/dataset.csv?archive=…` | 下载来源内已完成归档的 Trial CSV。 |
| `POST /api/trials` | `variant`: baseline、candidate、assembly-mismatch 或 suite。 |
| `POST /v1/traces` | 标准 OTLP HTTP 接收，成功后持久保存。 |
| `GET /api/settings` | 返回本地配置与凭据是否已配置，不返回密钥。 |
| `PUT /api/model` | 保存 `model`、`api_key`；已有配置的空密钥表示保留。 |
| `POST /api/model/test` | 验证 DeepSeek 凭据及模型可见性。 |
| `POST /api/judge` | 选择 `archive`、`trial_id`，保存独立 AI 评审尝试。 |

除 OTLP 接收外，写接口必须带 `X-RepoMesh-Local: 1` 和 JSON 请求。外部 Host／Origin 和跨站 API 访问被拒绝；允许从 HTTPS 控制台顶层导航到工作台首页或设置页，不提供 CORS 或远端监听入口。OTLP SDK 无需额外凭据或管理头。

## 验证与限制

Go 行为测试覆盖本地三态验收、真实 SDK 导出、重启读回、幂等与冲突、损坏档案、跨站写入、文件越界、私有密钥和模型失败。`scripts/test-observe-workbench.mjs` 使用 Playwright 对明确指定的本地实例做浏览器验证，会新增三个 Trial；运行方法及当前结果见[全本地实施记录](../development/2026-09-19-local-observation-02/README.md)。

当前是单用户、按需运行的本地管理工具，按档案读取，非大规模多租户平台；不含后台采集计划、任意用例编辑器、完整团队调度或 DSH turn／模型／工具接入。固定折扣产物用于验证验收流程，不能计为 Agent 交付能力。AI 评审调用外部模型服务；只有确定性评测与本地观测无需外网。上述范围在页面保留明确状态。
