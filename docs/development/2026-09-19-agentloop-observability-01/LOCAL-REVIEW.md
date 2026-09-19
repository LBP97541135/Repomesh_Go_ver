# 本地平台独立复核

复核时间：2026-09-19 21:36 UTC。复核者未参与本地平台脚本实现；本次仅新增本报告，未修改产品代码、安装脚本、Collector 配置或上游源码。

结论：**本地平台限定范围验收通过，未发现需修复的问题。**此结论覆盖官方来源、专用进程生命周期、HTTP 入口及未配置状态，不代表 AgentLoop 云端评估、RepoMesh 完整 Trace 或 DeepSeekHarness 已接通。相关部署说明见 [LOCAL-RUNBOOK](LOCAL-RUNBOOK.md) 与 [PLATFORM-READINESS](PLATFORM-READINESS.md)。

## 复核范围与版本

审阅两个启停脚本、离线保护测试、Collector 配置及上述两份文档。脚本与配置的 SHA-256 如下，便于区分后续改动：

| 文件 | SHA-256 |
| --- | --- |
| `scripts/observe-local.sh` | `2de84a269cbe14d7c0e5858bf7f4c70ded7fc6a940acaf02030bd0600510050d` |
| `scripts/agentloop-local.sh` | `c073c3b2c5a5db6bdc96e0c7feb44547a387f58604642a4a1d40af8c3bcbadfd` |
| `scripts/test-observe-local.py` | `e9f1cc8cee7c2b1764aae7c266521c7676cc78413a1622c9f597b1b199bc751d` |
| `configs/otel-collector.local.yaml` | `0bc68e5afa7a6fb3fd574e6ed23a8b285600dd42b5af9b5aa26c1e5634630ba0` |

独立请求 GitHub 官方 Release API，并将其资产 `digest` 与本机已下载归档重新计算的摘要比较：Collector Contrib `0.161.0` 为 `778c689efa681ff6e4722ce9f66b9b7f57c3ba009ab2e2b43dc2e0315862c731`；uv `0.12.17` 为 `fa82fd8dde8e8eefdecada6aa0889666556cfceb690d06e0c3bca49eb3070a63`，均一致。来源分别为 [Collector 官方发布](https://github.com/open-telemetry/opentelemetry-collector-releases/releases/tag/v0.161.0) 与 [uv 官方发布](https://github.com/astral-sh/uv/releases/tag/0.12.17)。

Launcher 的本机 Git origin 为 `https://github.com/aliyun/agentloop_experiment_manager.git`，HEAD 为 `be1dbf6629016fdff74a696ed22afeb207e74422`，工作树无修改；[官方提交 API](https://api.github.com/repos/aliyun/agentloop_experiment_manager/commits/be1dbf6629016fdff74a696ed22afeb207e74422) 可查询同一提交。此次复核复用了已安装依赖，未声称再次完成全新网络安装。

## 实际检查

| 检查 | 结果与依据 |
| --- | --- |
| Shell 语法 | `bash -n scripts/observe-local.sh scripts/agentloop-local.sh` 通过。 |
| 生命周期离线保护 | `python3 scripts/test-observe-local.py`，4 个测试通过；覆盖空目录、未安装、无关进程、共享或符号链接状态目录。 |
| 正式实例状态 | Collector `14318` 与 Launcher `18090` 均 `running`；复核前后 PID 分别保持 `72081`、`77177`，未重启它们。 |
| Launcher 运行依赖 | `bash scripts/agentloop-local.sh doctor` 输出 `Runtime check: OK`。 |
| 页面与静态资源 | `/`、页面引用的 JS、CSS 均 HTTP 200，资源 Content-Type 正确。 |
| Launcher 健康检查 | `/api/v1/health` 为 HTTP 200，`gatewayMode=agentloop`、`schedulerEnabled=false`。 |
| 未配置云连接 | 正确 Settings 路径 `/api/v1/settings/agentloop` 为 HTTP 404、`CONNECTION_NOT_CONFIGURED`；`/api/v1/plans` 为 HTTP 503、同错误码。未将普通路由 404 当作配置证据。 |
| 隔离实例端口保护 | 对两个脚本分别使用临时 `0700` 状态目录、占用中的随机端口，启动均拒绝且端口占用者仍存活。 |
| 隔离实例启停 | 分别实际启动两种服务：重复 start 保持 PID；stop 后 status 为 `not_running`；同端口立即重新 start 成功且 PID 变化。 |
| 重启后数据 | Collector 新旧启动使用不同证据目录，旧 Trace 文件保留；Launcher SQLite 与原 Master Key 保留。只比较密钥是否一致，未输出其内容或摘要。 |
| 文件与输出边界 | 正式状态根目录为 `0700`，SQLite、锁和 Master Key 为 `0600`；脚本不输出环境秘密或密钥正文；Collector 仅配置回环接收及本地文件导出。 |

隔离生命周期检查复制已校验的 Collector 二进制、引用同一已核对 Launcher 源码和依赖，但使用各自独立的数据目录与临时端口。检查结束后，所有临时进程通过对应脚本优雅停止；正式观测进程与 RepoMesh 业务进程未被操作。进程控制还经代码审阅确认：按 UID、启动时间、执行文件与命令行匹配，停止时使用 pidfd，未使用进程名批量终止。

## 保留的限制

官方源码要求云端 `OFFLINE` Plan、Dataset、Evaluator 及 Region、AgentSpace、AK/SK；官方[实验操作指南](https://help.aliyun.com/zh/agentloop/experimental-procedure-guide)也明确离线实验仍需本地访问 AgentLoop 服务。本次 Launcher 使用真实 `agentloop` Gateway，未启用 `fake`，未设置云连接或发起实验。文档对此描述准确。

未复测标准 SDK Span 的业务内容、Go 导出器、数据库快照或折扣评分；这些由本批其他验收记录覆盖。未执行 Windows 浏览器跨 WSL 访问或浏览器交互测试。上述通过项证明当前本地基础设施可用，不证明云端可查询、官方评估器判分、DSH 计量或业务交付改善。
