# 工程骨架阶段：AgentTeams 材料覆盖与证据边界

本文件属于本轮基础工程骨架交付。只读取既有材料与证据，没有运行验证脚本、启动外部服务、调用模型、修改 `validation/` 或清理容器、卷。历史协作指令没有被恢复，也没有联系历史协作者。产品决定以当前 ADR 和本轮授权为准；本文不新增业务协议或恢复算法。

## 目录清单与阅读口径

阅读前先列出了指定目录文件。两轮实验目录均包含 `README.md`、`.gitignore`、`reports/`、`evidence/`、`scripts/`、`upstream/`、`runtime/`、`private/`，第二轮另有 `output/`。`upstream/` 是锁定源码副本，`runtime/` 与 `private/` 是实验运行材料，不是产品源码。全树初次列表包含大量第三方文件，随后单独列出报告和脚本清单。本文的逐文件正文阅读记录列于后文，不能将目录枚举解释为逐行审计所有上游源码、依赖或原始事件。

调研目录有三份文件：

- `docs/agentteams-survey-2026-09-07/agentteams-survey.md`：全文已读。
- `docs/agentteams-survey-2026-09-07/agentteams-api-cli-survey-2026-09-07.md`：全文已读。
- `docs/agentteams-survey-2026-09-07/agentteams-survey.html`：完整提取并阅读文本（去掉 style 和标签、解码 HTML 实体；文件无 script 块），覆盖全部 15 节及阅读说明、接口表、流程与来源入口。它是同日调研的可读展示，未发现相对两份 Markdown 改变工程取舍的额外结论；未声称逐字等价，也没有重新渲染或访问其中的上游链接。

先行材料 `docs/README.md`、`docs/current/HANDOFF.md`、`docs/current/README.md`、`docs/adr/README.md`、`CONTEXT.md` 均已读；另全文阅读 [当前 AgentTeams 验收清单](../plan/agentteams-validation-plan.md)。全部 ADR、其他 current 文档、旧分析插件可行性报告由本轮其他成员负责，本文不冒领其覆盖。

报告与 README 的“全文已读”和原始证据、脚本的“定点读取”分别标注。JSON 的定点读取检查了相关对象、状态、断言和执行边界；未逐条阅读无关事件、SVG 或每次 HTTP 响应。下文未列为已读的关联 evidence、scripts、源码和运行件均仅作索引或未读。本轮没有查询最新上游，因此不声称材料反映当前上游 HEAD。

## 对本轮工程的直接约束

1. 沿用 Web、后台协调、受限主机执行三个 Go 进程共用工程与配套发布的既有方向。进程可编译、可启动只说明骨架存在；后台协调不得伪报调度、重放、AgentTeams 连接或业务恢复完成，主机执行入口不得冒充已具备权限门禁和安全执行能力。
2. Graph 留在后台进程内。AgentTeams 的 `ready` 是原生 DAG 候选计算结果，没有资源预留、全局准入、跨仓互斥和 RepoMesh 的业务授权。不要将其升级为独立 Graph 服务，也不要把原生 DAG 直接当成产品调度器。
3. AgentTeams、Issue、计划、调度、权限、GitHub 均不在本轮实现。无需为了“预留”而创建客户端、空接口、虚假数据库仓储、MCP Schema 或消息协议。后续接入应由用例和真实契约推动。
4. Skill 继续暂缓；ADR 0020 的受控 Python 仓库分析只保留扩展边界说明。本轮不复制旧分析代码、不运行插件，也不从验证脚本提取所谓生产执行器。
5. 旧报告中的单仓库 Leader、每 Issue 房间、较早版本启动流程等调研建议须服从后续 ADR。旧验证证明的是特定锁定版本和运行镜像下的有限事实，不是新的产品决策。
6. 工程检查、实验检查和产品验收必须分别记录。`go build/test/vet` 与前端构建通过不能改写旧验证的 blocked 结论，也不能填平取消、授权、幂等、恢复等已知差距。

## 四类事实

| 类别 | 本轮采用的解释 | 不能据此推出 |
| --- | --- | --- |
| 已采用决定 | 当前 ADR/CONTEXT 规定产品对象、受控路径、三进程、后台内 Graph、Skill 暂缓和 ADR 0020 扩展方向 | 早期调研建议自动覆盖后续决定 |
| 锁定提交事实 | 09-07 调研及 09-09 验证基线为 `eeaab64391ccaec9118e84977f538aefd40720d6`；09-10 为 `517caff9280242a00a4d4c06365352b9e41659c6` | 这些 SHA 是今日最新版本；源码未改等于镜像依赖完全相同 |
| 实测结果 | 既有 raw evidence 中真实 HTTP、Matrix、S3、runtime 及受控故障注入所得结果；注明身份、实例、镜像、样本及观察窗口 | 报告中 PASS 就是功能安全、所有租户隔离、端到端产品验收通过 |
| 未验证能力 | RepoMesh 自身权限、状态持久化与恢复、资源准入、Attempt fencing、可靠重放、主机执行控制等尚无本轮实现和验收 | 骨架占位或既有原生端点已经提供这些能力 |

[新基线证据](../../validation/agentteams-2026-09-10/evidence/source-baseline.json)记录了前后 SHA 和 14 个变更文件；[旧构建上下文](../../validation/agentteams-2026-09-09/evidence/build-context.json)记录了旧 SHA 与四份官方 Dockerfile 的散列。本文完整读取了这两份小型证据。依据既有差异报告，变化集中于 Controller 读取、CLI、模型配置；TeamHarness、认证与生命周期关键路径未随这 14 项变化被修复。本文没有自行重算整仓 diff，亦没有把旧镜像运行结果当作新镜像重验结果。

## 两轮验证结论及工程影响

第一轮的 Controller 官方测试与探针、TeamHarness 组件测试、Matrix 组件测试要分开解释。TeamHarness 的 14 项组件测试与 Matrix 的 10+39 项检查包含明确替身传输；实际部署、原生工具和模型运行另有 live 证据。故障复现用例标为 PASS，意味着成功观察了缺口，不意味着缺口已修复。原生通知去重、同一工作区重试和跨对象原子提交也不是同一能力。

第二轮后来补齐的工作不得仍写成未完成：新 Controller 的双在线实例读取矩阵已经有 313 项检查、260 次 HTTP；Mermaid 对实际 S3 的读取追踪完成 20 项检查；新 Manager 经 Matrix 完成一次关联到真实模型 usage 的回复。这些新增证据不会补齐 RepoMesh 产品代码或第一轮发现的写入、授权、取消和恢复缺口。

| 主题 | 已观察结果及适用范围 | 工程影响与原始追踪 |
| --- | --- | --- |
| 真实 Worker 身份 | Worker 自己投射的 SA 对 Project pause/replan 为 403；但同一 Worker 能写 global shared 元数据，在原生 `taskflow` 传 `role=leader` 后取消 Task。runtime 自报 `standalone`。最后远端 Task 读取为 cancelled；Project cancelled 在该条证据中仅是工具返回，不能冒称独立远端核验 | REST 拒绝不等于所有执行通路已授权。全文读 [worker-auth-live.json](../../validation/agentteams-2026-09-09/evidence/worker-auth-live.json) 与 [worker-auth-container.py](../../validation/agentteams-2026-09-09/scripts/worker-auth-container.py)，核对使用真实投射凭据及 packaged `server.call_tool`，没有用管理员替身冒充 Worker |
| MCP 与 REST 状态竞态 | 原生 pull 得到 active 后设置 barrier，REST pause 成功并独立读到 paused；原生随后 push 又写回 active。通知真实发出后令 Project/Task 上传失败，新的工具进程重试返回 ok/synced/reused，Task assigned 但 Project 仍 planned，Matrix 分派消息只有一条 | ETag 的 REST 保护没有覆盖所有原生 MCP 覆写。不能将通知去重当作业务 exactly-once。定点读 [control-race raw evidence](../../validation/agentteams-2026-09-09/evidence/control-race-live-32447df06f.stdout.json) 的 RACE01、RACE03 状态与重试，以及 [注入脚本](../../validation/agentteams-2026-09-09/scripts/control-race-live-container.py) 的真实 `_filesync` 包装、无效 S3 凭据故障、barrier 与两种上传恢复分支 |
| 原生 DAG 与标识 | 17 个阶段/46 项检查中 assigned 仍可出现在 ready 候选，submitted 不解锁依赖，accept 后 Project 节点 completed 而四个 TaskMeta 仍 submitted。同 Task ID 跨 Project 能共享碰撞的 Task 存储路径 | 后续映射需保留实例、Team、Project、Task 与业务 Attempt 的关系，具体编码仍未冻结。Graph 是后台模块，不提前写调度器。定点读 [DAG evidence](../../validation/agentteams-2026-09-09/evidence/dag-native-live-ec283964ba.json) 的阶段、ready_ids、通知复用及最终 Task/Project 状态；定点读 [DAG 脚本](../../validation/agentteams-2026-09-09/scripts/dag-native-live-container.py) 的实际 `server.call_tool`、远端 `mc cat`、delegate/ack/submit/accept 路径及断言。跨 Project 碰撞另追至 [THL-04 原始 evidence](../../validation/agentteams-2026-09-09/evidence/teamharness-live-results.json) 的三次真实工具请求/回执及 Project B 远端 planned 内容，与 [脚本](../../validation/agentteams-2026-09-09/scripts/teamharness-live-container.py) 的共享前缀、collision_retry 和真实 timeline 计数对应 |
| 取消与副作用 | 同一个真实 delegated Task 的模型启动 shell 心跳进程；Task 已 cancelled 后同 PID 心跳由 5 增到 8。观察脚本最后核验 PID/命令行后 SIGTERM，心跳才停止 | cancel 元数据、终止进程、撤销凭据和释放资源必须分别验收。定点读 [model-task-cancel-live.json](../../validation/agentteams-2026-09-09/evidence/model-task-cancel-live.json) 的 delegate、writer、cancel、cleanup 与最终原生元数据；全文读 [验证脚本](../../validation/agentteams-2026-09-09/scripts/model-task-cancel-live.py)，确认观察方没有代替模型创建 writer、未知结果不重发 |
| 资源与执行隔离 | 请求 0.5 CPU/512Mi 仅进入期望值；实际 HostConfig 的 Memory/NanoCpus/CpuQuota 为 0，cgroup v1 quota 为 -1，memory/pids 无实际请求限额；2.5 秒单线程探针接近一核。Worker 端口 HostIp 为空，与 Controller/Manager localhost 映射不同 | 受限主机执行是待实施的边界，不能从进程名、CR 字段、Ready 或构建成功推断资源隔离。定点读 [worker-runtime-live.json](../../validation/agentteams-2026-09-09/evidence/worker-runtime-live.json) 的 HostConfig/cgroup/CPU；全文读 [postready 脚本](../../validation/agentteams-2026-09-09/scripts/worker-runtime-postready.py)，确认是受限时长测量而非压测最大容量 |
| Worker 重建身份 | 容器停止时旧 SA 仍能 Ready=204，同容器启动后 token 相同；删除重建后 CR/SA UID 变化，旧 SA 在认证缓存窗口仍获接受，记录中首次成功后 280.294 秒仍 200、310.32 秒变 401；旧 Matrix token 的 whoami 仍 200，而旧 S3 已拒绝 | 旧 Attempt 的 late ready、凭据撤销与重建身份不能混为一谈。定点读 [重建 evidence](../../validation/agentteams-2026-09-09/evidence/worker-identity-recreate-live.json) 的身份比较、TokenReview、cache_window_http 与最终 Matrix/S3，以及 [stop/start evidence](../../validation/agentteams-2026-09-09/evidence/worker-ready-restart-live.json) 的停止窗口、同容器和同 token。全文读 [重建脚本](../../validation/agentteams-2026-09-09/scripts/worker-identity-recreate-live.py)、[缓存观察脚本](../../validation/agentteams-2026-09-09/scripts/worker-identity-cache-window.py)、[stop/start 脚本](../../validation/agentteams-2026-09-09/scripts/worker-ready-restart-live.py)，确认隔离新 Worker、原凭据快照、不刷新旧 S3 凭据及缓存观察锚点；不由离散采样推断精确失效瞬间 |
| 消息恢复与线程 | 55 条消息已在 Matrix 存储，实际 Worker 只处理编号 46—54 共 9 条；默认 sync 限制 10 且 limited，显式 100 能取回 59 个事件含这 55 条。两条实际普通 reply/thread 请求内容读回准确，回复落在新 timeline 并以 m.replace 更新自身 placeholder，没有延续原 parent/thread；匹配的四条 SQLite history 行使用同一 room session | 业务消息身份、outbox、重放和跨源排序尚需设计与验证；Matrix 持久化不等于 Worker 已消费。定点读 [runtime-recovery audit](../../validation/agentteams-2026-09-09/evidence/runtime-recovery-live-worker-sync-audit.json) 的 limit/limited/event count/worker numbers；检索 [恢复脚本](../../validation/agentteams-2026-09-09/scripts/runtime-recovery-live.py) 的 stop/start、游标与消息段。另定点读 [reply/thread evidence](../../validation/agentteams-2026-09-09/evidence/runtime-reply-thread-live.json) 的两例 inbound/outbound relations、同 session 数据库行、已部署 custom channel 指纹及源码摘录；全文读 [线程脚本](../../validation/agentteams-2026-09-09/scripts/runtime-reply-thread-live.py)，确认每例只发一次、准确读回及只读 SQLite 审计，没有冒充所有运行时或所有线程路径 |
| Checkpoint 假阳性 | QwenPaw 2.0.1 中 Controller proxy 返回 200 且 Content-Type 声称 JSON，实际为 1073 字节 HTML；直接 runtime 同为 HTML，替代 `/api` 路径 404，安装 app 的 checkpoint 路由扫描为空 | 端点存在或 200 不能显示“恢复可用”。定点读 [checkpoint-capability-live.json](../../validation/agentteams-2026-09-09/evidence/checkpoint-capability-live.json) 的响应类型、长度、非 JSON 前缀与扫描结果；检索 [只读检查脚本](../../validation/agentteams-2026-09-09/scripts/checkpoint-capability-live.py) 的 JSON 解析与替代路径，未执行快照/恢复 |
| 双实例新版本读取 | c/d 两实例都在线；同名 Project、双 Team、admin 与实际 SA TeamLeader，313 项检查，260 HTTP；30 次跨实例错 token 401 均有同 token 原实例前后 200（60 个正控），24 个对象原始 hash 不变。Worker 引用是 stopped/unmanaged 元数据 | 这项读取/作用域验证已完成；不覆盖普通 Worker 身份、真实业务创建、执行隔离、调度或模型。定点读 [dual-instance evidence](../../validation/agentteams-2026-09-10/evidence/controller-dual-instance-read-matrix-1cf6fd634d.json) 的 scope、记录分类、正负控制与 hash 检查；定点读 [脚本](../../validation/agentteams-2026-09-10/scripts/controller-dual-instance-read-matrix.py) 的 SA 生成、member role、前后正控、fixture/hash、退出断言 |
| Mermaid 数据读取 | JSON includeTasks 前后正控各有 Project/Task HEAD+GET；Mermaid true/false 两次只看到 Project HEAD+GET，Task 为 0，HTTP 后观察宽限为 1.25 秒，每次观察后仍有 trace 正控 | 是指定 fixture 与窗口的已观测行为，不能写成永远不读 TaskMeta。读取了 [storage evidence](../../validation/agentteams-2026-09-10/evidence/mermaid-storage-read-live-ddb9ad7a3979.json) 全部四个 cases 与部分 trace；定点读 [脚本](../../validation/agentteams-2026-09-10/scripts/mermaid-storage-read-live.py) 的 allowlist、HEAD/GET 映射、监听正控、monotonic 窗口及 hash |
| Mermaid 可渲染性 | Mermaid 11.17.2、18 样本中 14 成功，`end`、`subgraph`、`classDef`、`graph` 四个保留 ID 失败；真实 HTTP 原文 `end` 的独立浏览器输入也为 0 通过/1 失败 | 原生 Mermaid 文本不保证前端可渲染；未来展示 ID 与业务 ID 要分离验证。本轮不用 Mermaid 假装业务 Graph。定点读 [18 例 browser result](../../validation/agentteams-2026-09-10/evidence/mermaid-browser-result.json) 的状态与 parse error，未逐字审计 SVG；全文读 [真实 API 输入](../../validation/agentteams-2026-09-10/evidence/mermaid-api-cases.json) 与 [411 字节正文](../../validation/agentteams-2026-09-10/evidence/controller-delta-live-d33b63db78-reserved-end.mmd)，本轮只读重算 SHA-256 与所载 `026597812b4c8205cf7070ac2c2ef812621344a8c4da1d60b97ca488b090f0cc` 一致；读 [API browser result](../../validation/agentteams-2026-09-10/evidence/mermaid-api-browser-result.json) 的唯一输入与错误。全文读 [浏览器页](../../validation/agentteams-2026-09-10/scripts/mermaid-browser.html)、[函数调用辅助程序](../../validation/agentteams-2026-09-10/scripts/mermaid-renderer-main.go)、[隔离本地服务脚本](../../validation/agentteams-2026-09-10/scripts/serve-mermaid-check.py)，并定点读 [HTTP 捕获脚本](../../validation/agentteams-2026-09-10/scripts/controller-delta-live.py) 的 end fixture、原文保存与散列，确认真实 parse/render、strict 模式、DOM 节点计数和失败记录 |
| 新 Manager 模型调用 | 经 Matrix 的一次真实 nonce 回复精确匹配，4.141 秒；关联原生 turn usage 为 prompt 17,666、completion 57、1 次调用，未观察到工具调用。Console 503 在模型提交前发生；之后历史 chat 查询从 sender 纠正为 room ID，只读确认，没有补发模型请求 | 可说明新 Manager 此路径实际推理成功，不能声称所有 Provider、CLI、runtime 或工具均通过。定点读 [confirmed evidence](../../validation/agentteams-2026-09-10/evidence/control-manager-model-smoke-confirmed.json) 的 status、usage、delta、工具记录；检索 [只读确认脚本](../../validation/agentteams-2026-09-10/scripts/control-manager-model-smoke-confirm.py) 的 room/session、唯一 turn、精确正文与零工具断言 |

身份范围还补读了 [第一轮 Project 消歧 evidence](../../validation/agentteams-2026-09-09/evidence/project-team-disambiguation-f802e49e2a.json) 和 [后续写入 evidence](../../validation/agentteams-2026-09-09/evidence/project-team-disambiguation-f802e49e2a-writes.json) 的歧义/限定读取、404、内容 hash 与精确目标写入断言，并全文读 [初次脚本](../../validation/agentteams-2026-09-09/scripts/project-team-disambiguation-live.py)、[延续脚本](../../validation/agentteams-2026-09-09/scripts/project-team-disambiguation-continue.py)。初次记录的成功写入断言曾失败；后续保留原 evidence、校验原六对象内容 hash，经同字节单段复制后才完成精确目标写入。该元数据夹具使用 admin 和 stopped/unmanaged 引用，不是普通 Worker 授权验证，也没有替代产品身份设计。

以下是全文报告的附加索引，不作为本轮引入目录、接口或实现方案的独立设计依据；本轮未逐个重新阅读其所有原始传输文件，不提高为原始证据复核结论：

- 文件附件：真实六个 `m.file` 有 local/S3/mxc 三方内容核对；缺失或 401 上传仍能出现 submitted/SUCCESS/synced 与 accept，Project completed 与 Task cancelled 可不一致。离开房间后旧已知媒体仍可读取。成功附件链路不代表业务证据完整性、当前权限或审批门禁实现。
- 容量：61 组采样和固定 76 测试的回归、单 Worker 时间不能推导全局承载量、调度准入或强限额；未验最大容量，不提前填写默认可承载项目数。
- 新 Controller 的默认/空 format 是 JSON，显式 `format=json` 与未知值为 400；Task inspection 的原始 TaskMeta 与规范化 graph fallback 不同。CLI detail 的 `--team` 当时未正确透传而得到 409。history 过滤只证明读取行为，trace hints 不等于真实 span，也没有数据库迁移或 append-only 写入验收。
- 模型配置 CFG-01—04 的 60 条 REST/CLI/CR 记录区分 omission/null 保留和显式空串清除；Manager GET 不含 ModelProvider。CFG-05 覆盖实际投射与内存 active model；清除错误 Provider 时 REST 首先完成清除，随后正确 CLI 是重复验证，不能倒写为 CLI 首次修复。
- Provider URL 非空时可呈现 `/v1/v1`，但 route prefix `/v1/` 仍可能匹配，AI Proxy 后续 path 改写未完全验证；不能仅从字符串断言推理必败。源码未变化也不代表重建时非锁定依赖未变化。
- 新 Manager 的 `pip check` exit 1：`copaw-worker` 的 `copaw` 依赖在 `--no-deps` 路径下未安装；`qwenpaw --help`、`agt` 可用且上述单次推理成功。既不能报告依赖全绿，也不能由此直接断言 runtime 一定无法运行。

## 第二轮未完成审计的逐项继承

全文阅读了 [remaining-preconditions.md](../../validation/agentteams-2026-09-10/reports/remaining-preconditions.md)、[resumed-blocked-audit.md](../../validation/agentteams-2026-09-10/reports/resumed-blocked-audit.md)，并完整读取关联 [remaining-preconditions.json](../../validation/agentteams-2026-09-10/evidence/remaining-preconditions.json)、[resumed-blocked-audit.json](../../validation/agentteams-2026-09-10/evidence/resumed-blocked-audit.json)。其原始 `rg` 扫描明确排除 validation/.git，使用 `--no-ignore`，当时没有产品 `.go`/`go.mod`。本轮新授权允许创建这些文件；因此该“当时无 Go 产品工程”的条件会随骨架落地改变，而历史记录保持原貌。

其余产品先决条件不会因工程目录出现自动满足：计划/Issue 状态机、持久化与重启恢复、受控路径与执行、权限门禁、调度准入/互斥、Attempt 身份、取消/重试、写入一致性、资源归还和消息重放依然待实现、待验证。不能把报告列出的未完成项转写为本轮提供的空接口，更不能冻结未定的恢复协议。

审计同时记录旧共享卷仍有其他 GOAI 工作负载消费者。此事实只构成本轮“不清理、不重跑、不干预”的证据；没有执行新的 Docker 状态查询，亦不将历史消费者名称/时间推断为今天仍相同。新阶段不要求清除旧 goal 或恢复实验协作。第二轮“3 次连续 blocked 后封口”的操作记录属于旧任务历史，不能用本轮骨架通过去重写为全部验收完成。

环境报告还记录 stale Docker socket 曾复发；修复过特定 run 目录不能说明根因消除。时钟回拨约七小时导致证书 not-yet-valid，随后只对特定 c 实例证书按原 key 恢复并重载指定进程。本文保留报告时间基准，不重新生成证书、不启动实例、不修正历史时间戳；后续真正做实验时应重做该次独立预检，时长使用 monotonic 量测。

## 完整报告正文阅读清单

第一轮 [README](../../validation/agentteams-2026-09-09/README.md) 与下面 41 份 reports 均全文阅读（包含原始结论与后续审计修正）：

```text
validation/agentteams-2026-09-09/reports/
  artifacts-attempts-live.md
  capacity-live.md
  checkpoint-capability-live.md
  completion-audit.md
  control-component-report.md
  controller-components.md
  controller-live-report.md
  controller-mcp-race-live.md
  coverage-audit.md
  dag-native-live.md
  deployment-preflight.md
  deployment-run.md
  docker-startup-repair.md
  environment-cleanup.md
  evidence-review.md
  findings-and-handoff.md
  manager-enable-preflight.md
  manager-image-build.md
  manager-model-live.md
  matrix-component-report.md
  matrix-live-report.md
  mcp-config-contract-live.md
  mcp-response-loss-live.md
  model-task-cancel-live.md
  native-regression-load.md
  observation-latency-live.md
  project-team-disambiguation-live.md
  runtime-recovery-live.md
  runtime-reply-thread-live.md
  supplement-evidence-review.md
  teamharness-live-report.md
  twin-model-live.md
  twin-runtime-live.md
  validation-status.md
  worker-auth-live.md
  worker-identity-recreate-live.md
  worker-image-build.md
  worker-model-file-live.md
  worker-ready-restart-live.md
  worker-runtime-live.md
  worker-side-effects-live.md
```

第二轮 [README](../../validation/agentteams-2026-09-10/README.md) 与下面 20 份 reports 均全文阅读；包括仍称“计划”的旧文件，以及之后真正完成的 live 报告，优先使用时间/状态更晚、证据更具体的结论：

```text
validation/agentteams-2026-09-10/reports/
  clock-discontinuity.md
  config-cli-delta.md
  controller-api-live.md
  controller-delta.md
  controller-dual-instance-read-matrix-plan.md
  controller-twin-read-live.md
  controller-upgrade-smoke.md
  deployment-and-offline-smoke.md
  deployment-d-readiness.md
  environment-recovery.md
  image-build.md
  manager-config-live.md
  manager-config-preflight.md
  manager-model-smoke-new.md
  mermaid-browser-live.md
  mermaid-storage-read-live.md
  provider-url-analysis.md
  remaining-preconditions.md
  resumed-blocked-audit.md
  validation-status.md
```

脚本与 evidence 读取粒度已经在追踪表逐文件列明；未列入表的关联文件没有被视为本轮逐文件原始证据复核。两个 `upstream/`、镜像构建输出、下载附件、原始完整 Matrix/S3 事件和 private/runtime 运行环境没有在本轮整体审计。此限制不阻碍“只落地最小工程、不实现 AgentTeams 接入”的决策；后续接入阶段应按实际要实现的通路重新选择验收证据与授权范围。

## 下一阶段的交接建议

在业务开发获授权后，从一个真实用例确定 RepoMesh 自身状态与权限的权威来源，再确定 AgentTeams 映射和受控执行契约。用已经复现的真实失败场景设计可观察的验收，明确区分“请求接受、事件持久化、Worker 消费、运行结束、产物可读、业务验收、资源释放”。数据库表、MCP Schema、消息协议和恢复算法在那个阶段再决定。

本轮报告完成意味着材料覆盖和工程约束已经交接；骨架编译检查由主 agent 的本轮交付记录承接，不计入旧实验的 PASS 数，也不等于业务或 AgentTeams 集成验收完成。
