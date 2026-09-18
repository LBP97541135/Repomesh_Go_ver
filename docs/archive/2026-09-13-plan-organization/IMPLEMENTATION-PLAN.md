# 分批施工 TODO plan

> 整理前的历史副本，来源为 `3ad89bd`。当前状态见[当前交接](../../current/HANDOFF.md)和[施工计划](../../plan/IMPLEMENTATION-PLAN.md)。旧指令不构成新任务授权。

环境进度：本次接手已直接核实 WSL2 Linux、`/home/xubohan/projects/Repomesh_Go_ver`、Linux 工具路径及桌面项目绑定的 `repomesh-wsl` 主机。此前 SSH、登录恢复和 Windows／Linux 验收保留，见 [SSH 接入记录](../../development/2026-09-12-wsl-ssh/README.md)及 [WSL 环境验收](../../development/2026-09-12-wsl/README.md)。本次证据见 [B02 接手记录](../../development/2026-09-12-b02-preflight/README.md)。
更新于 2026-09-13。用户已确认 B02 最小实施包，范围见 [B02 采用记录](../../current/b02-authentication-adoption.md)。B00、B01 保持 VERIFIED；B02 保持 IN_PROGRESS，本地单元为 LOCAL_VERIFIED，外部账号验收为 PAUSED_BY_USER。LIVE-09、LIVE-10 的既有 PASS 不重跑；second-account-02 的 LIVE-05 历史 FAIL、LIVE-08 NOT_RUN 与 RESTORE_IN_PROGRESS 保留。未来跨账号 LIVE-05 和 LIVE-08-USER-READ 为 DEFERRED_BY_USER，不计为 PASS；今后真实账号验证只使用主账号 A。状态为 TODO、IN_PROGRESS、INTEGRATED_LOCAL_VERIFIED、DESIGN_PREPARED_NOT_ADOPTED、LOCAL_VERIFIED、VERIFIED、PAUSED_BY_USER、DEFERRED_BY_USER 或 BLOCKED。只有相应验收证据齐全的组可以标为 VERIFIED。

B03 已按用户授权完成主目录集成，状态为 `INTEGRATED_LOCAL_VERIFIED`。[最终独立复核](../../development/2026-09-12-b03-integration-01/FINAL-INDEPENDENT-REVIEW.md)确认两项 P1、一项 P2 闭环，无开放 P0/P1/P2；真实 PostgreSQL 73/0、前端 28/28、普通/空目录浏览器 25+1、Go/race、HTTP、实际进程恢复与发布通过。新包为 `dist/repomesh-0.3.0-b03-integrated-20260912-r1/`，businessReady=false；不代表外部 GitHub、部署或整批业务 VERIFIED。历史证据与包保留，新证据见[主目录集成 01](../../development/2026-09-12-b03-integration-01/README.md)。B04 已采用 D01—D04 并实现 U04.1—U04.4，状态为 `INTEGRATED_LOCAL_VERIFIED`，证据见[收口 01](../../development/2026-09-13-b04-closeout-01/README.md)。仍非整批 VERIFIED。B05／B06 设计已复核，实施仍 TODO，不因 B04 收口而自动采用 D05—D08。旧交接/Prompt保留历史，不能自动恢复未授权批次。

## 执行步骤

- [x] Read the Principles section of the poteto-mode skill.
- [x] Phase A: Frame
- [x] Phase B: Design the workflow
- [x] Phase C: Run the loop
- [x] Phase D: Keep the audit trail
- [x] Phase E: Verify and hand back

上述步骤记录 B00、B01 已完成的施工轮次。本次接手核对见[前置记录](../../development/2026-09-12-b02-preflight/PLAN.md)，B02 实施步骤见 [PLAN](../../development/2026-09-12-batch-02/PLAN.md)，继续采用 figure-it-out。每组先记录预期行为，再实现、运行验证、独立复核，最后更新本表。已完成组的证据保留，下一组重新确认依赖。

## 批次表

| 批次 | 状态 | 交付范围 | 前置条件 | 必须通过的验收 |
| --- | --- | --- | --- | --- |
| B00 | VERIFIED | 源码和工具基线、批次表、验证入口与工作区隔离 | 保留现有未提交修改 | 六项工程命令；现有 Web 探针；现有三入口行为；记录版本和结果 |
| B01 | VERIFIED | PostgreSQL 连接、显式迁移与版本核查、独立临时数据库验证 | B00；完成实现方案比较 | 真实数据库连接；重复和并发迁移；故障回滚；迁移历史不匹配拒绝；超时；二进制启动与退出；readyz 仍为 503 |
| B02 | IN_PROGRESS；外部账号验收 PAUSED_BY_USER，本地验收见[B02 结果](../../development/2026-09-12-batch-02/README.md) | GitHub 登录、会话、固定返回目的地、Origin 与 CSRF、仓库发现；含认证秘密基础及 coordinator 持久发现续扫 | B01 已通过；A1 至 A7 及认证所需 S01、S02、S06 子集已明确采用。App 材料、固定 HTTPS 和服务已配置 | 既有 LIVE-09/10 PASS 保留；second-account-02 的 FAIL/NOT_RUN 与恢复中状态保留；未来真实验证只用 A，跨账号项 DEFERRED_BY_USER |
| B03 | INTEGRATED_LOCAL_VERIFIED；整批业务未 VERIFIED | 待配置项目创建、读取与列表、资料编辑、明确增仓、固定配置引用与原操作恢复，已实现 | 已授权并完成主目录集成；Astra 规划与接口、Sol 实现 | PG 73/0、前端 28/28、浏览器 25+1、Go/race、HTTP、实际进程恢复和 r1 配套发布通过；最终独立复核无开放 P0/P1/P2，businessReady=false |
| B04 | DESIGN_ADOPTED；IMPLEMENTED；INTEGRATED_LOCAL_VERIFIED；授权范围已结束，非整批 VERIFIED | 模型供应商保存／close、不可变版本、部署 schema 1 导入、六个 HTTP 端点与 Key 恢复。不发送模型请求 | 用户授权推进 B04；采用记录见 [b04-model-sources-adoption.md](../../current/b04-model-sources-adoption.md) | [验收报告](../../development/2026-09-13-b04-acceptance-01/README.md)：`main` `621592d`，260／0／2、前端 32／32、S01—S12、U04.2／U04.4 夹具。真实模型与 GitHub 未跑，businessReady=false |
| B05 | TODO | 模型单次测试、专用应用、预算与时限只读摘要 | B04；C05/C06设计已补，待采用和实施 | 测试未知不重发；快照隔离；只换模型保留 execution；无变化保存不造修订；受限原模型预览不泄露 |
| B06 | TODO | 手动 Issue 原子创建、必要会话、主 ChangeSet、来源、范围与持久待办 | B05；采用并落实 P9 固定配置绑定 | DB01 至 DB08 和 CB01 至 CB05；任一写入失败全回滚；创建与换配置并发绑定完整版本；不伪造运行接入 |
| B07 | TODO | Issue 列表、最小详情、已有会话只读、rooms 分读、SSE 与恢复页面 | B06；收口详情和恢复候选 | 当前完整内容范围核权；分页隔离；401 和失权清缓存；旧响应隔离；SSE 失效通知与快照重读 |
| B08 | TODO | 首批管理闭环整体验收与配套发布验证 | B02 至 B07 | 登录、项目、配置、建项、查看、编辑全流程；故障和恢复矩阵；完整工程检查及打包；记录未接入运行状态 |
| B09 | TODO | G1 与 G2，真实 Manager 会话往返 | B08；完成对应执行协议 | 固定配置实际消费；可信消息与工具关联；外部投递、回复及重启恢复独立取证 |
| B10 | TODO | G3 与 G4，受限宿主下单仓单轮执行 | B09；全部写入路径与生命周期确定 | 派工、提交、停止、写权限撤销、回收；REST、MCP、shared 旁路验证 |
| B11 | TODO | G5，两轮执行与跨仓恢复 | B10；合法换图和部分应用协议确定 | 两轮换图；双仓失败；部分应用恢复；证据、验证和交付一致 |

C05项目预算/时限摘要和C06原模型受限/历史定位已在本轮候选中补齐设计，尚待采用及真实验收。详见[契约审查](../../reviews/2026-09-12-project-contracts/README.md)。它们进入 B05 的前置项，不阻塞 B01。历史文档的后端 B01 至 B08 编号与本表的施工批次编号不同。

## Astra 前置设计待办与范围

2026-09-13，用户要求将提前设计分工列入必读和计划，并限制各项工作范围。[Astra 前置设计分工](../../plan/ASTRA-DESIGN-PREPARATION.md)为必读，逐项规定允许内容、排除项、文件／操作边界和交付终点。下列为设计待办，不替代上方实施批次及验收状态；B04—B06实际交付与采用范围见[本轮设计](../../development/2026-09-13-b04-b06-design-01/README.md)及[复核](../../development/2026-09-13-b04-b06-design-01/REVIEW.md)，其余设计未推进。

| 完成 | 设计任务 | 主要范围与依赖 |
| --- | --- | --- |
| [x] | B04，Astra候选设计完成；Sol待采用后实现 | [设计](../../development/2026-09-13-b04-b06-design-01/B04.md)／[复核PASS](../../development/2026-09-13-b04-b06-design-01/REVIEW.md)； 优先收口来源／不可变版本／秘密保存与安全终结，形成接口声明；对齐 B03，不包含测试、应用或 Issue。 |
| [x] | B05，Astra候选设计完成；Sol待采用后实现 | [设计](../../development/2026-09-13-b04-b06-design-01/B05.md)／[复核PASS](../../development/2026-09-13-b04-b06-design-01/REVIEW.md)； 单次模型测试、专用应用及 C05／C06 只读契约；B04 类型与版本确定后定稿，不做预算编辑。 |
| [x] | B06，Astra候选设计完成；Sol待采用后实现 | [设计](../../development/2026-09-13-b04-b06-design-01/B06.md)／[复核PASS](../../development/2026-09-13-b04-b06-design-01/REVIEW.md)； 手动 Issue 原子创建、P9、共享本地用例与待办关系；结合 B04／B05 定稿，不接运行消费者。 |
| [ ] | B07，Astra 契约复核，Sol 后续实现 | 最小读取、rooms／SSE 与页面恢复；数据类型依赖 B06，不开放消息发送或完整执行面板。 |
| [ ] | B08，Astra 验收设计，Sol 后续执行 | 管理闭环故障／恢复／发布验收矩阵；可先设计，实际验收仍依赖 B02—B07。 |
| [ ] | B09-G1，Astra 提前兼容审查 | 本轮四问题静态部分覆盖见[兼容审查](../../development/2026-09-13-b04-b06-design-01/COMPATIBILITY.md)；实际消费/重启/撤销实验NOT_RUN，完整G1/G2未完成。 |
| [ ] | B09 G1／G2，Astra 完整设计，Sol 后续实现 | 固定配置消费、首条消息、可信 MCP、投递／回复及恢复；限最小 Manager 往返，不做 Worker 执行。 |
| [ ] | B10 G3／G4，Astra 设计，Sol 后续实现 | 可提前核对上游、比较写入与资源归属方案；与 B09 对齐后定稿单仓单轮执行接口，不做跨仓换图。 |
| [ ] | B11 G5，Astra 设计，Sol 后续实现 | 可先推演两轮／双仓失败；基于 B10 写方与生命周期定稿换图、Graph 边界和部分应用恢复，不重写 DAG。 |

近期设计按 B04、B05／B06、B07 推进；G1 兼容审查应提前参与 B04—B06，完整 B09／B10 可先研究，B11 接口定稿等待 B10 协议。无需等待前置批次全部编码完成才能设计，但实际实施顺序和外部验收门槛保持。独立复核由非主要实现者承担。

每项设计勾选完成时链接其实际交付和复核记录，另列采用／候选／待验证范围；不能将勾选设计完成改写为产品 VERIFIED。纯设计默认仅编辑声明的文档和设计附件，不修改产品代码、迁移或运行配置，不启动服务或执行真实外部调用。当前已授权的B04—B06组合完成后交回，不自动推进B07—B11或实现；通常单项边界见[单项任务范围](../../plan/ASTRA-DESIGN-PREPARATION.md#单项任务的工作范围)。

## 当前吞吐与文件归属

2026-09-13 补充：前置设计按上节新增待办与[Astra 分工指南](../../plan/ASTRA-DESIGN-PREPARATION.md)安排。下列“本轮不继续 B04”等文字记录 B03 集成轮次，不是当前状态。B04 D01—D04 已采用并实现，状态以上方批次表为准。

- Blocking first steps. B00、B01 已有证据；B03 已 INTEGRATED_LOCAL_VERIFIED。B02 外部账号验收暂停，本轮不继续 B04；下一会话先收口并集中采用 B04 设计，再实施。真实 GitHub 外部操作不在当前开发步骤中。
- Independent workstreams. 主代理维护交接及本轮证据；只读代理独立核对源码、采用范围和跨批依赖；另一模型复核实际文档及证据。
- Shared mutable state. 主代理是主目录唯一集成者；秘密、GitHub 和前端代理在各自临时副本编辑。已保存接手前文件哈希及原字节，保留既有配置、脚本和环境记录，不整体暂存或提交。
- Smallest safe decomposition. B02 先解决认证秘密、会话与尝试、GitHub 适配、持久发现续扫及登录页面。每单元验证后推进，模型与运行业务仍留后续批次。

## B02 开工核对

B02.6 接续准备见[计划与吞吐检查点](../../development/2026-09-12-b02-external-preparation/PLAN.md)，外部历史进度见[真实验收 03](../../development/2026-09-12-b026-live-03/README.md)、[自然刷新观察](../../development/2026-09-12-b026-refresh-02/README.md)和[第二账号验收 02 暂停交接](../../development/2026-09-12-b026-second-account-02/PAUSE.md)。B02 整体仍 IN_PROGRESS，外部账号验收为 PAUSED_BY_USER。历史 FAIL、NOT_RUN 与 RESTORE_IN_PROGRESS 不改写；未来跨账号项 DEFERRED_BY_USER，真实账号验证只使用 A。B03 主目录集成与最终独立复核已完成，为 INTEGRATED_LOCAL_VERIFIED。B04 已 INTEGRATED_LOCAL_VERIFIED，仍非整批 VERIFIED。

本次接手已核实 Linux 和桌面 SSH 项目。用户随后确认 [B02 最小实施包](../../current/b02-authentication-adoption.md)，采用门槛已解除，当前按 [B02 计划](../../development/2026-09-12-batch-02/PLAN.md)实施。GitHub App 未配置只影响真实集成验收。原前置记录保留历史判断，不再作为待采用阻塞。

## B00 与 B01 TODO

| 项目 | 状态 | 验收证据 |
| --- | --- | --- |
| 保存工作区基线和工具版本 | VERIFIED | [工作区基线](../../development/2026-09-12-batch-01/baseline.json)和[工程检查](../../development/2026-09-12-batch-01/b00-checks.json) |
| 六项基础检查与当前运行探针 | VERIFIED | [工程检查](../../development/2026-09-12-batch-01/b00-checks.json)和[运行探针](../../development/2026-09-12-batch-01/b00-runtime.json) |
| 数据库实现方案比较及接口草图 | VERIFIED | [实现方案](../../development/2026-09-12-batch-01/design.md) |
| PostgreSQL 连接与迁移实现 | VERIFIED | [实现与范围](../../development/2026-09-12-batch-01/README.md) |
| 真实数据库重复、并发、回滚及历史不匹配验证 | VERIFIED | [12 组 PostgreSQL 实测](../../development/2026-09-12-batch-01/b01-verification/checks.json) |
| Web 二进制命令和兼容启动验证 | VERIFIED | [命令、重启和 HTTP 验证](../../development/2026-09-12-batch-01/b01-verification/checks.json) |
| 独立代码与记录复核 | VERIFIED | [代码和脚本复核、证据补全](../../development/2026-09-12-batch-01/review.md) |
| 更新 README、HANDOFF 和本表 | VERIFIED | [结构检查](../../development/2026-09-12-batch-01/structural-checks.json)和[打包结果](../../development/2026-09-12-batch-01/release-verification.json) |

## 每组验证规则

代码检查覆盖实际改动。影响入口、前后端或发布链路时执行完整六项工程检查。数据库能力必须在独立 PostgreSQL 中实测，跳过集成测试不等于该组通过。UI 组须实际操作页面，外部集成组区分测试替身与真实服务证据。每组保存命令、版本、退出码和结果，失败修复后重跑受影响检查。未实现能力保持真实未接入状态。

B00／B01 历史记录见[原 decision log](../../development/2026-09-12-batch-01/decisions.tsv)，B02 另见[本批 decision log](../../development/2026-09-12-batch-02/decisions.tsv)。后续组各自建立记录目录，再在本表挂接证据。
