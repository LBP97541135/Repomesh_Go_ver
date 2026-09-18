# 分批施工计划

更新：2026-09-13。本表维护 B00—B11 的实施状态、依赖和验收入口。当前接手条件见[交接](../current/HANDOFF.md)，计划分类见[计划导航](README.md)。

B00、B01 保持 VERIFIED；B02 本地已实现，整批 IN_PROGRESS，外部验收 PAUSED_BY_USER。B03、B04 为 INTEGRATED_LOCAL_VERIFIED，均不等于整批业务 VERIFIED。B04 只结束 D01—D04、U04.1—U04.4 授权范围；B05、B06 的后续候选仍待采用。`businessReady=false`。

只有相应验收证据齐全的组可以标为 VERIFIED。设计完成、明确采用、代码实现与运行验收分别记录；本表不自动授权下一批工作。

## 批次表

| 批次 | 状态 | 交付范围 | 前置条件 | 必须通过的验收 |
| --- | --- | --- | --- | --- |
| B00 | VERIFIED | 源码和工具基线、批次表、验证入口与工作区隔离 | 保留现有未提交修改 | 工程构建、测试与静态检查；现有 Web 探针；现有三入口行为；记录版本和结果 |
| B01 | VERIFIED | PostgreSQL 连接、显式迁移与版本核查、独立临时数据库验证 | B00；完成实现方案比较 | 真实数据库连接；重复和并发迁移；故障回滚；迁移历史不匹配拒绝；超时；二进制启动与退出；readyz 仍为 503 |
| B02 | IN_PROGRESS；外部账号验收 PAUSED_BY_USER，本地验收见[B02 结果](../development/2026-09-12-batch-02/README.md) | GitHub 登录、会话、固定返回目的地、Origin 与 CSRF、仓库发现；含认证秘密基础及 coordinator 持久发现续扫 | B01 已通过；A1 至 A7 及认证所需 S01、S02、S06 子集已明确采用。App 材料、固定 HTTPS 和服务已配置 | 既有 LIVE-09/10 PASS 保留；second-account-02 的 FAIL/NOT_RUN 与恢复中状态保留；未来真实验证只用 A，跨账号项 DEFERRED_BY_USER |
| B03 | INTEGRATED_LOCAL_VERIFIED；整批业务未 VERIFIED | 待配置项目创建、读取与列表、资料编辑、明确增仓、固定配置引用与原操作恢复，已实现 | 已授权并完成主目录集成；Astra 规划与接口、Sol 实现 | PG 73/0、前端 28/28、浏览器 25+1、Go/race、HTTP、实际进程恢复和 r1 配套发布通过；最终独立复核无开放 P0/P1/P2，businessReady=false |
| B04 | DESIGN_ADOPTED；IMPLEMENTED；INTEGRATED_LOCAL_VERIFIED；授权范围已结束，非整批 VERIFIED | 模型供应商保存／close、不可变版本、部署 schema 1 导入、六个 HTTP 端点与 Key 恢复。不发送模型请求 | 用户授权推进 B04；采用记录见 [b04-model-sources-adoption.md](../current/b04-model-sources-adoption.md) | [验收报告](../development/2026-09-13-b04-acceptance-01/README.md)：验收基线 `621592d`，Go 260 通过、0 失败、2 跳过、前端 32／32、S01—S12、U04.2／U04.4 夹具。真实模型与 GitHub 未跑，businessReady=false |
| B05 | IMPLEMENTED（本地，feat/astra-b05，已并 main）；非整批 VERIFIED | 模型单次测试、专用应用、预算与时限只读摘要 | B04；C05/C06设计已补 | 测试未知不重发；快照隔离；只换模型保留 execution；无变化保存不造修订；受限原模型预览不泄露。验收见 [B05 ACCEPTANCE](../development/2026-09-16-b05-model-tests-applications/ACCEPTANCE.md) |
| B06 | IMPLEMENTED（本地，已并 main）；非整批 VERIFIED | 手动 Issue 原子创建、必要会话、主 ChangeSet、来源、范围与持久待办（迁移 0017/0018） | B05；采用并落实 P9 固定配置绑定 | DB01 至 DB08 和 CB01 至 CB05；任一写入失败全回滚；创建与换配置并发绑定完整版本；不伪造运行接入。验收见 [B06 ACCEPTANCE](../development/2026-09-16-b06-issue-creation/ACCEPTANCE.md) |
| B07 | IMPLEMENTED（本地，已并 main）；SSE 未实现 | Issue 列表、最小详情、rooms 分读（REST 面）；SSE 与前端恢复页待续 | B06；收口详情和恢复候选 | 当前完整内容范围核权；分页隔离；401 和失权清缓存；旧响应隔离；SSE 失效通知与快照重读（SSE/前端部分 DEFERRED）。验收见 [B07 ACCEPTANCE](../development/2026-09-16-b07-issue-queries/ACCEPTANCE.md) |
| B08 | ACCEPTANCE_DESIGNED（矩阵已交付）；实际执行待办 | 首批管理闭环整体验收与配套发布验证 | B02 至 B07 | 登录、项目、配置、建项、查看、编辑全流程；故障和恢复矩阵；完整工程检查及打包；记录未接入运行状态。矩阵见 [B08 ACCEPTANCE-MATRIX](../development/2026-09-16-b08-acceptance-matrix/ACCEPTANCE-MATRIX.md) |
| B09 | PARTIALLY IMPLEMENTED（本地半边，已并 main）；传输适配器/MCP 未实现，非整批 VERIFIED | G1/G2：消息持久化、澄清记录、受控 decide 动作、按 Issue 固定配置消费闭包（迁移 0019） | B08；完成对应执行协议 | 固定配置实际消费；可信消息与工具关联；外部投递、回复及重启恢复独立取证。验收见 [B09 ACCEPTANCE](../development/2026-09-16-b09-manager-session/ACCEPTANCE.md) |
| B10 | PARTIALLY IMPLEMENTED（本地，已并 main）；container 拆除授权/进程协议冻结待续，非整批 VERIFIED | G3/G4：执行预留账本、核验停止、受限 host-executor（迁移 0020） | B09；全部写入路径与生命周期确定 | 派工、提交、停止、写权限撤销、回收；REST、MCP、shared 旁路验证。验收见 [B10 ACCEPTANCE](../development/2026-09-16-b10-restricted-execution/ACCEPTANCE.md) |
| B11 | TODO | G5，两轮执行与跨仓恢复 | B10；合法换图和部分应用协议确定 | 两轮换图；双仓失败；部分应用恢复；证据、验证和交付一致 |

C05项目预算/时限摘要和C06原模型受限/历史定位已在本轮候选中补齐设计，尚待采用及真实验收。详见[契约审查](../reviews/2026-09-12-project-contracts/README.md)。它们进入 B05 的前置项，不阻塞 B01。历史文档的后端 B01 至 B08 编号与本表的施工批次编号不同。

## Astra 前置设计待办与范围

2026-09-13，用户要求将提前设计分工列入必读和计划，并限制各项工作范围。[Astra 前置设计分工](ASTRA-DESIGN-PREPARATION.md)为必读，逐项规定允许内容、排除项、文件／操作边界和交付终点。下列为设计待办，不替代上方实施批次及验收状态；B04—B06实际交付与采用范围见[本轮设计](../development/2026-09-13-b04-b06-design-01/README.md)及[复核](../development/2026-09-13-b04-b06-design-01/REVIEW.md)，其余设计未推进。

| 完成 | 设计任务 | 主要范围与依赖 |
| --- | --- | --- |
| [x] | B04，设计完成，D01—D04 已采用并完成本地集成验收 | [设计](../development/2026-09-13-b04-b06-design-01/B04.md)／[复核PASS](../development/2026-09-13-b04-b06-design-01/REVIEW.md)；已交付来源、不可变版本、秘密保存与安全终结。实现范围见[B04 采用记录](../current/b04-model-sources-adoption.md)，不包含模型测试、应用或 Issue。 |
| [x] | B05，Astra候选设计完成；Sol待采用后实现 | [设计](../development/2026-09-13-b04-b06-design-01/B05.md)／[复核PASS](../development/2026-09-13-b04-b06-design-01/REVIEW.md)； 单次模型测试、专用应用及 C05／C06 只读契约；B04 类型与版本确定后定稿，不做预算编辑。 |
| [x] | B06，Astra候选设计完成；Sol待采用后实现 | [设计](../development/2026-09-13-b04-b06-design-01/B06.md)／[复核PASS](../development/2026-09-13-b04-b06-design-01/REVIEW.md)； 手动 Issue 原子创建、P9、共享本地用例与待办关系；结合 B04／B05 定稿，不接运行消费者。 |
| [x] | B07，REST 读取面已实现（SSE/前端恢复 DEFERRED） | [B07 ACCEPTANCE](../development/2026-09-16-b07-issue-queries/ACCEPTANCE.md)；Issue 列表/最小详情/rooms，数据类型依赖 B06，不开放消息发送或完整执行面板。 |
| [x] | B08，验收矩阵设计已交付；实际执行待办 | [B08 ACCEPTANCE-MATRIX](../development/2026-09-16-b08-acceptance-matrix/ACCEPTANCE-MATRIX.md)；管理闭环故障／恢复／发布验收矩阵；设计完成不勾选实际 PASS。 |
| [ ] | B09-G1，Astra 提前兼容审查 | 本轮四问题静态部分覆盖见[兼容审查](../development/2026-09-13-b04-b06-design-01/COMPATIBILITY.md)；实际消费/重启/撤销实验NOT_RUN，完整G1/G2未完成。 |
| [x] | B09，本地半边已实现（传输/MCP 未实现） | [B09 ACCEPTANCE](../development/2026-09-16-b09-manager-session/ACCEPTANCE.md)；消息持久化、澄清状态机记录、decide 动作、按 Issue 固定配置消费闭包（0019）。 |
| [x] | B10，本地账本与受限执行已实现（container 拆除/协议冻结待续） | [B10 ACCEPTANCE](../development/2026-09-16-b10-restricted-execution/ACCEPTANCE.md)；执行预留账本、核验停止、host-executor 有限动作（0020）。 |
| [ ] | B11 G5，Astra 设计，Sol 后续实现 | 可先推演两轮／双仓失败；基于 B10 写方与生命周期定稿换图、Graph 边界和部分应用恢复，不重写 DAG。**已有输入（2026-09-15，经小陈授权登记）：[B11-REPLAN-PROTOCOL.md](B11-REPLAN-PROTOCOL.md)（异常重规划协议与全量快照修订设计）与 `feat/replan-mainline` 分支（任务轴占位状态机 + 迁移 0009：`public.tasks`/`public.task_assignments`，未合入 main）。B11 设计与实现须以该分支与协议文档为基定稿，禁止另起第二套任务表。** |

B04 的已授权范围已结束；后续按明确授权推进 B05、B06 采用和 B07 设计。G1 兼容审查应提前参与 B04—B06，完整 B09／B10 可先研究，B11 接口定稿等待 B10 协议。无需等待前置批次全部编码完成才能设计，但实际实施顺序和外部验收门槛保持。独立复核由非主要实现者承担。

每项设计勾选完成时链接其实际交付和复核记录，另列采用／候选／待验证范围；不能将勾选设计完成改写为产品 VERIFIED。纯设计默认仅编辑声明的文档和设计附件，不修改产品代码、迁移或运行配置，不启动服务或执行真实外部调用。历史 B04—B06 设计组合已交付，B04 后续授权实现也已结束；其他批次按新任务授权推进，通常单项边界见[单项任务范围](ASTRA-DESIGN-PREPARATION.md#单项任务的工作范围)。

## 后续工作与历史记录

B05 实施前须明确采用 D05、D06、C05、C06，并核对已实现 B04 的接口与版本语义。B06 依赖 B05，并须采用和落实 P9 配置绑定。每项任务按实际范围确定文件编辑归属和验证方法，不沿用旧轮次的协作名单。

B02 外部验收保持暂停，未来真实账号验证只使用主账号 A。LIVE-09、LIVE-10 的既有 PASS 保留；second-account-02 的 LIVE-05 历史 FAIL、LIVE-08 NOT_RUN、RESTORE_IN_PROGRESS 仍可从[暂停交接](../development/2026-09-12-b026-second-account-02/PAUSE.md)追溯。未来跨账号项为 DEFERRED_BY_USER，不自动恢复旧账号流程。

B00、B01 的执行步骤、B02 开工核对及 B03 临时文件归属已移至[整理前计划](../archive/2026-09-13-plan-organization/IMPLEMENTATION-PLAN.md)。各轮 `PLAN.md` 和证据继续留在原 `docs/development/` 目录，当前操作以本表与新任务授权为准。

## B00 与 B01 已完成记录

| 项目 | 状态 | 验收证据 |
| --- | --- | --- |
| 保存工作区基线和工具版本 | VERIFIED | [工作区基线](../development/2026-09-12-batch-01/baseline.json)和[工程检查](../development/2026-09-12-batch-01/b00-checks.json) |
| 六项基础检查与当前运行探针 | VERIFIED | [工程检查](../development/2026-09-12-batch-01/b00-checks.json)和[运行探针](../development/2026-09-12-batch-01/b00-runtime.json) |
| 数据库实现方案比较及接口草图 | VERIFIED | [实现方案](../development/2026-09-12-batch-01/design.md) |
| PostgreSQL 连接与迁移实现 | VERIFIED | [实现与范围](../development/2026-09-12-batch-01/README.md) |
| 真实数据库重复、并发、回滚及历史不匹配验证 | VERIFIED | [12 组 PostgreSQL 实测](../development/2026-09-12-batch-01/b01-verification/checks.json) |
| Web 二进制命令和兼容启动验证 | VERIFIED | [命令、重启和 HTTP 验证](../development/2026-09-12-batch-01/b01-verification/checks.json) |
| 独立代码与记录复核 | VERIFIED | [代码和脚本复核、证据补全](../development/2026-09-12-batch-01/review.md) |
| 更新 README、HANDOFF 和本表 | VERIFIED | [结构检查](../development/2026-09-12-batch-01/structural-checks.json)和[打包结果](../development/2026-09-12-batch-01/release-verification.json) |

## 每组验证规则

代码检查覆盖实际改动。影响入口、前后端或发布链路时执行根 README 列出的完整工程检查。数据库能力必须在独立 PostgreSQL 中实测，跳过集成测试不等于该组通过。UI 组须实际操作页面，外部集成组区分测试替身与真实服务证据。每组保存命令、版本、退出码和结果，失败修复后重跑受影响检查。未实现能力保持真实未接入状态。

B00／B01 历史记录见[原 decision log](../development/2026-09-12-batch-01/decisions.tsv)，B02 另见[本批 decision log](../development/2026-09-12-batch-02/decisions.tsv)。后续组各自建立记录目录，再在本表挂接证据。
