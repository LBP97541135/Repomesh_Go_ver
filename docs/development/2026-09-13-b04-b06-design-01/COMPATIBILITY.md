# B04—B06 最小运行兼容审查

状态：STATIC_PARTIAL_COVERAGE。Sol/high定向读取，Astra复核决定架构的类型、reconcile、gateway及脚本。当前本地 AgentTeams HEAD `517caff9280242a00a4d4c06365352b9e41659c6`，main、嵌套工作树干净；未fetch，不称今日远端最新。两份09-07调研锁定 `eeaab64391ccaec9118e84977f538aefd40720d6`。来源和所读文件内容哈希见[source-evidence.json](source-evidence.json)。

## 四项覆盖

| 问题 | 锁定源码/历史事实 | 本轮设计要求与未验证项 |
| --- | --- | --- |
| Issue能否定位完整参数/秘密版本 | [ManagerSpec](../../../third_party/AgentTeams/agentteams-controller/api/v1beta1/types.go)约642行只有model/provider，无Issue/configRevision/secretVersion；[启动脚本](../../../third_party/AgentTeams/manager/scripts/init/start-manager-agent.sh)637—719按名称/env推导参数，已有ID模型只补缺。 | RepoMesh必须保存 Issue→配置修订→modelProfileVersion→完整snapshot/secretVersion，execution亦固定政策。上游不能替代此闭包；实际请求消费仍NOT_RUN。 |
| 普通讨论和正式Issue归属 | [会话专题](../../current/conversation-issue-separation-design.md)允许一会话多个Issue；[消息目标](../../current/conversation-message-target-design.md)可no_work。[TeamHarness](../../../third_party/AgentTeams/plugins/teamharness/mcp/server.py)普通回复禁projectflow，但创建source/requester由调用者提供。 | 普通持久请求与正式Issue分别固定配置；目标未确认不得套“最近Issue”。本轮只保留边界，不定义消息受理/MCP或可信runtime。 |
| 重启读取同版 | [reconcile](../../../third_party/AgentTeams/agentteams-controller/internal/controller/manager_controller.go)134—160读当前Spec；[配置发布](../../../third_party/AgentTeams/agentteams-controller/internal/controller/manager_reconcile_config.go)14—41覆盖当前共享配置。 | 重启读work→Issue→initialConfigurationRevision。重建Manager只收敛当前共享Spec，不能证明旧Issue仍用C1。CB03仅本地持久追溯待验。 |
| 秘密撤销后禁止fallback | [provisioner](../../../third_party/AgentTeams/agentteams-controller/internal/service/provisioner.go)733—743先AuthorizeAIRoutes空provider；[Higress](../../../third_party/AgentTeams/agentteams-controller/internal/gateway/higress.go)181—194空filter授权全部；[Aliyun](../../../third_party/AgentTeams/agentteams-controller/internal/gateway/aigateway.go)180—186空值回退全局。非空provider后来再收窄，清空则保持全路由。 | 上游Manager consumer不是秘密版本隔离边界。新动作查原secret当前可用性，失效blocked；已可能发送只核原操作，无新Key重发。真实撤销/隔离实验NOT_RUN。 |

这些源码足以否定“设置共享Manager当前model/provider即可实现每Issue绑定”的设计推断，尚不足以给出完整运行Adapter或G1/G2方案。本轮未设计这些内容。

## 历史实测的证明范围

[CFG-05报告](../../../validation/agentteams-2026-09-10/reports/manager-config-live.md)在517caff9镜像中证明共享Manager配置切换重建进程并改变内存active model；错误provider保留旧配置，显式清空恢复默认chat。不是并发Issue隔离或完整参数实际消费。主设计本轮重读该报告，未重跑。

[单次模型smoke](../../../validation/agentteams-2026-09-10/reports/manager-model-smoke-new.md)另证明一条默认chat Matrix推理与usage；未观察工具。不是完整参数、provider秘密版本或重启隔离验收。旧Worker凭据撤销实验以eeaab643为基线，只说明不同凭据面撤销速度不同，不能推成模型Key撤销。历史入口见[骨架证据索引](../../current/scaffold-agentteams-evidence.md)。旧FAIL/NOT_RUN/RESTORE_IN_PROGRESS原始记录不改写。

## 后续有界实验，全部 NOT_RUN

| 实验 | 条件及观察 | 启动前提/归属 |
| --- | --- | --- |
| E01 持久闭包 | 自有临时DB建立C1/S1与C2/S2，先后创建A/B；重启只读恢复查询，逐字段核参数及secretVersion；非法空/跨项目/混pin由DB拒绝。 | B04—B06实施后，采用P9；本地CB01—05，不需真实模型。 |
| E02 普通讨论归属 | 同会话no_work/A/B输入，记录正式目标解析前后及各自pin。 | 消息受理/目标解析协议采用并实施后；超本批，不借此标B08完成。 |
| E03 实际消费 | 两个自有记录端点与不同测试凭据，交错A/B、切当前项目配置并重启；观察实际模型、参数、secretVersion映射。 | 未来G1消费者设计与授权；本批不设计Adapter或执行。 |
| E04 撤销无fallback | 受理A后外发前撤销S1，S2/默认仍可用；A blocked且零出站，B继续；已可能发送分支只核原操作。 | 未来运行边界；另授权环境，绝不使用真实秘密做本轮探测。 |
| E05 上游授权机制 | 自有517caff9隔离实例观察P1绑定、清空、重启前后route授权，不做付费推理。 | 独立实验授权，仅验证上游fallback；不等于G1/G2。 |

B09提前审查仅覆盖本轮四问题的数据限制。完整G1/G2、真实Manager往返、MCP、执行Adapter和后续Gate均未完成。
