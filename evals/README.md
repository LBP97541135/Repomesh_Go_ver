# 观测与评估资产

这些资产配合 [独立观测工具](../cmd/repomesh-observe/main.go)和 [AgentLoop Spec](../docs/current/agentloop-observability-evaluation-spec.md)。本轮实现固定产物的 HTTP 行为验收、Trial 数据集导出与平台结果文件规范化。它们不启动 AgentTeams、DeepSeekHarness 或模型，不证明 Agent 已完成跨仓任务。

## 本地验收

从仓库根目录构建工具。每次调用启动两个独立 localhost HTTP 服务，订单服务实际请求价格服务，将订单写入新临时目录后通过 HTTP 读取。结束时关闭服务、移除临时目录；完整请求、响应、身份、金额与评分保留到指定私有档案目录。

```bash
go build -o /tmp/repomesh-observe ./cmd/repomesh-observe
/tmp/repomesh-observe discount --archive /tmp/repomesh-eval-example --variant baseline
/tmp/repomesh-observe discount --archive /tmp/repomesh-eval-example --variant candidate
/tmp/repomesh-observe discount --archive /tmp/repomesh-eval-example --variant assembly-mismatch
/tmp/repomesh-observe dataset --archive /tmp/repomesh-eval-example --output /tmp/repomesh-eval-example/trials.csv
/tmp/repomesh-observe status --archive /tmp/repomesh-eval-example
```

三个验收命令预期退出码分别是 `1`、`0`、`2`。前者是已知金额错误；最后一个是评测器装配错误导致目标组合不可判定，不是意外命令失败。脚本若启用 `set -e`，应显式捕获这两个预期非零值。CSV 路径不覆盖，重复导出需选新路径。

| 变体 | 价格语义 | 实际订单语义 | 期望判定 |
| --- | --- | --- | --- |
| baseline | 应付比例 | 减免比例 | fail；返回与持久化为 2000 分，期望 8000 分。 |
| candidate | 应付比例 | 应付比例 | pass；返回与持久化均为 8000 分。 |
| assembly-mismatch | 应付比例 | 实际旧版，清单指定新版 | unknown／评分 error；发现身份不符后不将行为归入目标组合。 |

本轮 variant 是预置服务分支，源码修订明确使用 `synthetic:` 标签。artifact digest 来自真正运行的工具二进制，environment 和 Trial 每次生成不同身份；不是伪造的仓库提交。未知结果的分数为 null。HTTP 200、退出 0、自报 `verdict=pass` 及空测试报告都不能代替金额断言。

也可通过 `discount --manifest FILE` 指向明确授权的兼容服务。协议与 [Manifest 样例](discount/manifest.example.json)一致：两个服务提供 `GET /version`；价格提供 `GET /quote?base_cents=10000`；订单提供 `POST /orders` 与 `GET /orders/{order_id}`。身份字段为 `component/revision/artifact_digest/environment_id`。生产接入必须从受信任部署控制面确认 `/version` 与真实运行产物的对应关系，并隔离测试期间的更新；当前实现仅观察配置端点的声明，没有通用部署证明、镜像锁定或数据库直连证明。样例值必须替换，不应当作已运行证据。

## AgentLoop 适配

- [契约交接 Prompt](agentloop/contract-handoff.prompt.md)、[变量映射](agentloop/variable-mapping.json)和 [固定 grader 配置](agentloop/contract-handoff.grader.json)用于配置单一诊断维度。每行 CSV 的 rubric 包含实际规则文本；完整 Trial 验收报告内联，外部证据访问未配置时明确说明。
- [数值映射配置](agentloop/known-outcome.grader.json)、[绑定样例](agentloop/binding.example.json)及 [结果样例](agentloop/result.example.json)只用于本地映射契约演练。样例的 `fixture_only` 标志保存在原始结果中；它不是从 AgentLoop 拉取的真实结果。
- 使用平台结果时，先保留平台分配的 dataset/item 或精确 subject trace 到本地 Trial 的受信任映射，再调用 `import-result --archive DIR --raw FILE --binding FILE --grader FILE`。绑定必须引用档案中的 Trial，不能使用模型自报 ID、相同 session 或时间窗口猜测。
- 平台评估 `status=success` 仅代表评分执行完成。原始结果、平台 task/run/eval ID、被评 Trace 与 Judge 自身 Trace 分开保存；不能用诊断结果覆盖确定性验收。重复同一物理结果幂等，内容变化需另存新结果。

CSV 与 JSON 是本项目的版本化适配格式，不是 AgentLoop 官方上传 API。本轮未创建云端评估任务、没有已认证的数据集导入或云端评分回读。未来接入须核对实际变量输入与结果字段；[官方评估结果字段](https://help.aliyun.com/zh/agentloop/developer-reference/field-descriptions-for-evaluation-results)是当前映射依据。

用例定义见 [discount/case.json](discount/case.json)。固定产品验收不自动评估选仓质量；后续 DSH 接入后，需增加相同起点的真实生成过程、选仓证据、留出任务和负例，再报告能力改善。
