# B04—B06 设计后交接

日期：2026-09-13。本文件交接已经完成的设计及下一任务入口；配套 [Prompt](NEXT-TASK-PROMPT.md) 可以直接复制。当前新推荐为 `RECOMMENDATIONS_NOT_ADOPTED`，产品实现为 `IMPLEMENTATION_NOT_STARTED`。用户随后授权整理交接并同步当前工作区全部项目改动，排除秘密及本机配置；这项 Git 授权不等于采用设计或授权实施。

2026-09-13 稍后用户授权“推进B04”。D01—D04 已写入 [采用记录](../../current/b04-model-sources-adoption.md)。B04 实现已开始；B05／B06 与 D05—D08 仍未采用，不自动开工。

## 接手状态

| 范围 | 实际状态 | 下一步边界 |
| --- | --- | --- |
| B00／B01 | VERIFIED | 保留既有结论 |
| B02 | IN_PROGRESS，外部 PAUSED_BY_USER | 不恢复外部账号实验；历史 FAIL、NOT_RUN、RESTORE_IN_PROGRESS 保留，未来跨账号项 DEFERRED_BY_USER |
| B03 | INTEGRATED_LOCAL_VERIFIED | 主目录已有实现与最终独立复核；不是整批业务或真实 GitHub VERIFIED |
| B04 | DESIGN_ADOPTED；IMPLEMENTED；INTEGRATED_LOCAL_VERIFIED。收口见 [2026-09-13-b04-closeout-01](../2026-09-13-b04-closeout-01/README.md)，非整批 VERIFIED | 进入 B05 前另作 D05／D06／C05／C06 采用 |
| B05／B06 | 设计完成、实施 TODO | 分别依赖 D05／D06，以及 P9／S06 与完整配置来源；不因 B04 开工而自动开工 |
| B09 | 必要数据兼容问题的部分静态审查 | 完整 G1／G2 未完成，实验 E01—E05 均 NOT_RUN |

`businessReady=false`；Web `/readyz` 仍为 503，host-executor 入口仍未实现。没有模型请求、Issue 产品实现或 AgentTeams 运行接入。主目录 `/home/xubohan/projects/Repomesh_Go_ver` 是实现基线，不再次整体合并 `/home/xubohan/projects/Repomesh_B03`。

## 先读哪些材料

先读根 AGENTS.md、README.md、[当前索引](../../current/README.md)、[当前交接](../../current/HANDOFF.md)、[施工计划](../../plan/IMPLEMENTATION-PLAN.md)。然后完整阅读本轮 [设计入口](../2026-09-13-b04-b06-design-01/README.md)、[决定表](../2026-09-13-b04-b06-design-01/DECISIONS.md)、B04／B05／B06、Go／TS 声明、迁移设计、验收映射、兼容审查、源码核对与独立复核。对应唯一专题与章节见设计入口，不另造一套 HTTP Schema。

B03 以 [主目录集成记录](../2026-09-12-b03-integration-01/README.md)及其 [最终独立复核](../2026-09-12-b03-integration-01/FINAL-INDEPENDENT-REVIEW.md)为准。09-12 的 B04 Prompt 和另一 worktree 的 A／B／final 候选仅为历史来源；09-13 设计已经完成比较和综合，不必重做同一轮选型，也不能把文件名 final 当作采用证据。

## 已完成的设计与待采用事项

| 决定 | 推荐内容 | 实施时不能丢失的约束 |
| --- | --- | --- |
| D01 | `projects` 继续拥有配置目录和项目固定配置；`models`／`sources` 使用有限事务接口 | 不复制配置解析器、不引入通用配置平台或循环依赖 |
| D02 | 一个独立目的的加密完整输入 vault，用于精确比较 | 不保存裸 Key 哈希；业务 Key 停用不破坏原输入比较，vault 不可作为模型凭据 |
| D03 | `Prepare` 在业务锁外预扣包装额度，`InsertPrepared` 加入调用方事务 | 预扣不退款；秘密、模型、目录与原回执原子提交；保留认证 API |
| D04 | B04 schema 1 登记每项单 owner 的执行元数据；新默认固定 `pinned_version` | 旧 null 保留 B03 语义；B05 schema 2 补全预算／时限／出站必须新增版本；导入 CLI 仅使用数据库部署身份 |
| D05 | 次数预算和一次发送许可 | 仅明确确认 COMMIT 的原进程获得外发能力；读库不能重建发送许可，未知不能自动重发；金额未知不能写成零 |
| D06 | 专用模型应用完整复制旧 execution 绑定；C05 固定摘要与 C06 原模型分型 | 不复用完整 PATCH resolver；无变化仍保存原回执并消费预览；受限模型不泄露身份 |
| D07／P9 | Issue 非空、不可变、同项目复合 FK 固定到 ProjectConfigRevision | 原操作内部结果与 Issue 同版本；数据库同时约束 owner、秘密与参数来源；旧坏数据拒绝迁移，不批量补造 |
| D08 | Issue、必要会话、主 ChangeSet、来源卡片、内容范围、原回执、blocked work 和事件同一短事务 | 创建无持久 rejected；先重放再检查新建门槛；清理保留 provenance、配置 pin 与墓碑 |
| S06 | 新建仓库至少 Metadata read／Contents write／Pull requests write；用户及 App 观察最终提交前 60 秒内 | 这是候选新门槛；原结果仍按完整内容读权恢复，不声称 GitHub 与本地数据库原子一致 |

D05 数值也是待采用项：预览 5 分钟、输出不超过已保存上限且至多 16 token、20 次／UTC 日、每 actor 最多 1 笔未核责任、请求 30 秒；处理器登记租约 60 秒／续期 20 秒，首批仅 `openai_chat_completions`。租约过期只影响新测试资格，不能证明旧发送者能力已撤销。若调整这些决定，须同时修订依赖它们的声明、C05／C06、B06 门槛和验收项。

B04 限六个模型保存／读取／原操作终结端点，不发送模型请求。B05 承担其余六个模型端点。B06 限 options、已有会话候选、POST 创建、GET 原操作四端点；详情、rooms、SSE 留给 B07。B06 不接 Manager／MCP／消息消费者，不创建 RepositoryIssue；可选分析尚不可解析时不能静默丢弃输入 ID。

## 按可验收单元接续

1. B04：U04.1 秘密事务与认证回归 → U04.2 Provider／目录／save／close → U04.3 执行导入 → U04.4 六端点与浏览器恢复。D01—D04 及 B04 精确协议采用后才进入实现。
2. B05：U05.1 schema 2／政策／预览 → U05.2 测试登记与受控单次传输 → U05.3 专用应用 → U05.4 C05／C06 与前端恢复。真实付费模型另行授权，先用任务拥有的本地 TLS fixture。
3. B06：U06.1 数据库约束和 DB／CB 直写验证 → U06.2 options／会话／完整 App 观察 → U06.3 创建／查询／清理 → U06.4 四端点与冻结输入恢复。完整固定模型链、执行政策、P9／S06 都是依赖，约束必须先于首个真实 Issue。

规划、架构、核心类型和函数声明沿用用户指定的 GPT-6 Astra／xhigh；以后获准实施时，按已声明接口由 GPT-5.6 Sol 填写函数体，非主要实现者独立复核。当前交接和同步任务没有开启任何实现单元。

## 验证事实与复现限制

[设计 checks.json](../2026-09-13-b04-b06-design-01/checks.json)记录 2026-09-13 08:45 UTC 的实际检查：17 份原文档归档哈希一致；134 个新增本地链接／锚点无错误；Go parser 检查 433 个声明、31 个 package 段；TS strict noEmit 使用当前真实 api.ts 的 Result 类型，通过；任务增量无空白问题。461 个基线非秘密文件中 17 份允许文档变化，444 个范围外文件未变。独立设计复核 PASS，无开放 P0／P1／P2。

这些是声明和文档检查，不是产品编译、数据库、浏览器、并发、模型或运行验收；后者在本轮设计中全部 NOT_RUN。B03 历史最终证据另有 PostgreSQL 73 通过／0 跳过、前端 28／28、浏览器 25＋1、配套发布和独立复核。同步时的工程检查单独记录在 [同步检查](SYNC.md)，不改写上述旧结果。

设计包的 baseline.json、initial-status.txt、original-documents.zip、changes.diff 和 output-manifest.json 均保留原字节。设计 checker 比较的是设计开始时的脏工作区和原 HEAD；交接导航变更及后续提交会使旧保护门槛不再满足。不要为了让旧 checker 变绿而重写其历史基线；新任务应建立自己的基线。

AgentTeams 源码审查锁定 `517caff9280242a00a4d4c06365352b9e41659c6`，旧 09-07 调研锁定 `eeaab64391ccaec9118e84977f538aefd40720d6`。已读源码与历史 CFG05 只支持共享配置／活动模型能力，不能证明每 Issue 配置／秘密隔离、完整参数消费或重启恢复。本轮未 fetch、构建或运行上游。

## Git 与资源保护

同步前基线为 `main`／`ad9a49b2fe3b5fe743a65c838ded6cb022e79363`，当时 B02／B03 源码与历史资料尚未提交；它们不属于 B04—B06 的产品实现。用户本次明确授权一并同步，具体纳入、排除及检查见 [SYNC.md](SYNC.md)。同步后从实际 Git HEAD 和状态接手，不把旧文档的“未提交／不得推送”复述成当前 Git 状态或永久禁令。

保留 `.codex/config.toml` 本机修改、认证配置、包装根、token、Key、数据库与浏览器状态；不读取秘密正文或扫描进程环境。B02 专用数据库、55432、r3 服务及旧发布包都不用于新测试，不按历史 PID 操作，不清理共享资源。今后测试只能使用任务新建且拥有的隔离资源；不运行旧 verify-record.py，不覆盖失败轮次或已有发布标签。GitHub 同步不恢复真实账号、模型、部署或上游实验授权。
