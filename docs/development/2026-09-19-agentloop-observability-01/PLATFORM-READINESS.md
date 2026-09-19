# AgentLoop 本地部署与平台就绪核查

核查时间：2026-09-19。用户选择“本地搭建”，未提供云地域、AgentSpace 或接入配置。本轮已安装并启动阿里云官方本地 Launcher 和真实 OpenTelemetry Collector；云端实验仍未配置。

## 1. 官方能力核对

官方 [实验操作指南](https://help.aliyun.com/zh/agentloop/experimental-procedure-guide) 区分在线实验与用户本地执行的离线实验，后者仍需要本地能访问 AgentLoop 服务。官方 [离线实验平台说明](https://help.aliyun.com/zh/agentloop/use-cases/iv-experiments-back-testing-offline-experimental-platform-and-topic-level-rubric) 明确本地执行平台与云端存储／大盘的分工，并要求地域、AgentSpace 和访问凭据。

阿里云公开了 [AgentLoop Local Experiment Launcher 源码](https://github.com/aliyun/agentloop_experiment_manager/tree/be1dbf6629016fdff74a696ed22afeb207e74422)。该版本提供本地工作台、连接、调度、SQLite 与离线 Runner，通过云端 Plan／Dataset／Evaluator 执行实验；不是脱离云端的完整 AgentLoop 服务端。它的 `fake` 模式只用于 UI 与测试，本轮没有启用。

因此，本次可交付真实本地安装、Trace 接收、本地独立验收与可接入准备；不能据此宣称已创建 AgentLoop 云空间、已完成官方评估器判分，或把自制 HTTP 接收器称为 AgentLoop。未发现该来源提供无需空间与凭据的完整自托管平台；这只是本次核对的公开能力边界，不是对未来产品形态的断言。

## 2. 锁定来源

| 组件 | 本轮锁定值 | 核查与部署方式 |
| --- | --- | --- |
| OpenTelemetry Collector Contrib | `0.161.0`，Linux amd64 | [官方 release](https://github.com/open-telemetry/opentelemetry-collector-releases/releases/tag/v0.161.0) 二进制，归档按发布资产的 SHA-256 校验。 |
| Collector 归档 SHA-256 | `778c689efa681ff6e4722ce9f66b9b7f57c3ba009ab2e2b43dc2e0315862c731` | 脚本内固定；下载文件不同则拒绝安装。 |
| Collector 二进制 SHA-256 | `1e6629a05c0d3084b2bc090307ea16166af4a2d24452785d076736adbcb13be7` | 从已验归档提取，写入安装记录，启动前复查。 |
| AgentLoop Launcher | `0.1.0`，提交 `be1dbf6629016fdff74a696ed22afeb207e74422` | 官方 Git 源码；本机 Linux 使用源码构建，未复用 macOS 发布包。 |
| Launcher Python 依赖 | 官方 `backend/uv.lock` | `uv sync --locked --no-dev`；`agentloop-sdk==1.0.3`、OpenAPI SDK `alibabacloud-agentloop20260520==2.3.1`。 |
| Launcher 前端依赖 | 官方 `frontend/package-lock.json` | `npm ci` 后执行上游 `npm run build`。 |
| Bootstrap uv | `0.12.17`，SHA-256 `fa82fd8dde8e8eefdecada6aa0889666556cfceb690d06e0c3bca49eb3070a63` | [官方 release](https://github.com/astral-sh/uv/releases/tag/0.12.17)，校验后只安装在任务目录。 |
| Python | `3.12.11` | uv 在任务目录安装 managed Python，不更改系统 Python。 |

上游源码、锁文件和静态入口的摘要写入本地 `agentloop-install.json`。Collector 官方 [文件 exporter 说明](https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/v0.161.0/exporter/fileexporter/README.md) 将其标为 alpha；本轮用它验证接收后的落盘事实，不把该格式冻结为 RepoMesh 的长期证据契约。

## 3. 就绪矩阵

| 项目 | 状态 | 依据／剩余条件 |
| --- | --- | --- |
| 官方 Collector 安装与 OTLP HTTP 接收 | READY_LOCAL | 官方二进制版本和摘要已核对，标准 SDK Span 已实际接收并核对 Trace／Span 身份。 |
| 官方 AgentLoop 本地 Launcher | READY_LOCAL | 本地 HTML／静态资源和健康检查通过，真实 `agentloop` Gateway，调度关闭。 |
| 本地云连接配置 | NOT_CONFIGURED | Settings 返回 `CONNECTION_NOT_CONFIGURED`，不猜测地域或空间。 |
| AgentLoop 云端可查询 Trace | NOT_RUN | 需要指定空间、真实 OTLP Endpoint 与凭据，并验证可查询性；本地落盘不能替代。 |
| 官方云 Plan／Dataset／Evaluator | NOT_RUN | 没有目标配置；Plans 明确返回未配置错误。 |
| 本地实验执行器与云端结果闭环 | NOT_RUN | 需要真实本地 Agent 连接与 OFFLINE Plan；Launcher `ACCEPTED`／`HANDED_OFF` 也不等于评估通过。 |
| DSH 身份、Skill 内容与模型／工具计量 | NOT_RUN | 需 RepoMesh／AgentTeams 后续真实执行链路；原生 Launcher 安装不能补出不存在的业务证据。 |

本地启停、数据位置、实际命令与验收结果见 [运行手册](LOCAL-RUNBOOK.md)。这些部署能力可以继续供后续 DSH 采集适配使用，运行时变更不要求把 AgentLoop SDK 加到 RepoMesh 业务包。

## 4. 后续接通时的验收条件

1. 核对真实地域、AgentSpace、OFFLINE Plan、Dataset 和 Evaluator 绑定；凭据保存在工作台加密存储或受控配置，报告只记引用。
2. 把真实业务 Trace 上报到该空间关联的轨迹存储，确认入口关联字段、traceId 和 Spec／DAG／仓库版本没有丢失。
3. 把已知正确、错误、不可判定的用例交给同一官方评估流程，保存权威 recordId 与逐题结果。
4. 回读判分及运行状态，分清“评估器执行成功”“候选通过验收”和“缺证据不可判定”。
5. 将上述结果与本地独立验收核对后再标云端 READY，不以页面加载、本地 mock、空上报或随机 traceId 作为通过证据。
