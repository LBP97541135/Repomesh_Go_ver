# 本地观测、Jev rubric 与 DeepSeek 分析实施记录

日期：2026-09-20。基线 `86f63e53ed67f231c1abfbe03793d30114e93bf3`；分支 `codex/main-worktree-20260920-120900`，工作副本 `/home/xubohan/projects/Repomesh_Go_ver-main-20260920-120900`。修改未提交；未触碰其他分支或共享业务数据库。最终文件摘要见 `change-manifest.json`。

## 范围与结果

依据[增量 Spec v1.1](../../current/observation-platform-expansion-spec.md)完成本地工作台切片：调用计量和覆盖缺口、可信 Trace 绑定、Jev typed rubric、默认关闭的本地自动评分、DeepSeek 聚类归因与 AI 初标、冻结样本及版本化人工标签。发现链新增可选非阻塞本地计量；审核决定新增审核单证据版本前置条件。页面、档案、调度和标注均在本机；只有用户配置的 Jev／DeepSeek 模型 API 出站。

**G0／G1 为部分完成，G2／G3 未运行；本报告不表示整份 Spec 全部验收。** 28 项逐条结果见 [acceptance-matrix.json](acceptance-matrix.json)。PARTIAL 表示有实现或局部证据但不满足整行，不等于 PASS。原生 DSH、真实流式 TTFT、完整候选生命周期与真实任务对照仍待后续。

按用户要求，三个 GPT-5.6 Sol 子 agent 仅调研源码、协议和文档，未编辑、运行测试或使用密钥。主要代理负责实现及运行；这些调研不冒充独立人员端到端验收。

## 主要行为

- `repomesh-evidence/3` 使用 Jev `jev-1.13.0`；四个维度各自保留 Score、legend、概率及证据充分性 Choice。代码负责归一化与阈值，缺证／低置信度为 unknown，固定 HTTP 产物的工具效率为 not_applicable，绝不覆盖确定性验收。
- DeepSeek `deepseek-flash` 只做受限归组、类别和证据说明，初标未确认、不提供 rubric 数值分。小于 2 条的簇由代码归入离群项；未知、缺失或重复成员拒绝。
- 新模型调用先持久保存付费尝试和固定输入；HTTP 重放复用结果，发送后未知或结果落盘失败不自动重付费。每日次数限制为本实例本地策略，不能代表供应商金额封顶或跨实例 exactly-once。
- `model_calls` 是中立业务事实，经有界队列落盘；TTFT 无来源则未知，不用总耗时替代。当前发现调用只精确到 Project／Issue，不能冒充 Task／Attempt 或原生 turn。
- 样本与选样原因分别保存；原始证据、AI 标签、人标签和评分分开。审核单证据版本在锁内比较；尚未实现候选 SHA 改变自动更新全链证据版本。
- 输入投影脱敏后才发往模型，私有原始档案保留。只读挂载不改写来源。管理入口只面向本机私有档案，不是多租户服务。

## 检查与失败记录

| 检查 | 实际结果 |
| --- | --- |
| 隔离 PostgreSQL 下 `go test -json -p 1 ./...` | 最终 **697 PASS、0 FAIL、3 SKIP**。跳过 Jev 显式实时测试、RootFileOwner、ProjectBrowserServer；Jev 另行执行，见下节。 |
| `go build ./...`、`go vet ./...` | 通过。 |
| observepipe／observeui／observe 命令 race | 通过。 |
| `frontend` 锁文件安装、build、lint | 通过；保留原有 bundle 提示和两个 hook 警告。修复基线 FocusPanel 未使用解构变量引发的 TS6133。 |
| `web` 锁文件安装、typecheck、35 个测试、build | 通过。 |
| 浏览器 | 桌面／窄屏导航、3 类固定 HTTP 产物、CSV、配置密钥为空、新增页面及样本表单通过；0 页面错误，0 浏览器外站请求。服务端模型请求单独记录。 |
| 本地文档链接／空白 | 新增文档链接及 diff 空白检查通过；原 Spec 的 5 个 AgentTeams 源码链接因本 worktree 未克隆上游而不可达，未伪造或移植其他工作区内容。 |
| API 文档检查 | 基线与当前均有 **52 项既有失败**，本次新增失败为 0；不标整体通过。 |

前次全量及一次定点复试出现未改动 `internal/models/TestPostgresTwentyWaySameSave` 的 `RESULT_UNCONFIRMED`。随后基线与当前各 10 次均通过，最终全量通过；无法确定间歇原因，不删除失败日志，也不声称已修复该既有并发问题。初始测试库 trust 认证使错误密码测试失败，改为本轮私有实例 scram 后通过；初始迁移全局 role 竞争、浏览器缺本机库及脚本路由问题均属验证环境／脚本修正，保留过程记录。

采集 caller 微基准预热后 30 对、阈值检查通过且无记录丢失；没有完整逐次性能／内存实验，OBS18 仅 PARTIAL。不能用它推导实际模型或真实业务性能收益。

## 真实模型调用与 TypeSafe skill 验收

已按 TypeSafe skill 阅读实时 API、Score／Confidence 与 composite-scoring 文档，拆成独立 typed judgments；不要求 Jev 生成解释、执行测试或替代代码规则。使用只读找到的 skill：`/home/xubohan/projects/Repomesh_Go_ver-main-20260920-115016/internal/typesafe/assets/typesafe-ai/SKILL.md`，来源 skill 仓库锁定 `65a39f393687675ce170e6094757de20370365b9`。

最后一轮固定产物集真实调用：3 次 Jev、1 次 DeepSeek 归因、1 次失败聚类及修正后 1 次显式新聚类，共 6 次。原始聚类返回单成员簇，旧实现拒绝；新增确定性小簇归离群项处理后再用新尝试验收，旧失败保留。最终 4 个样本形成 1 个双成员组和 2 个离群项，重放没有新增付费请求。此处的 6 次不是整个调试阶段累计调用数，整个工作台以 `paid_attempts` 档案为准。

| 固定产物 | 确定性业务验收 | 实际 Jev 辅助结论 |
| --- | --- | --- |
| 旧契约 baseline | fail | fail；正确识别契约／金额错误。 |
| 修正 candidate | pass | unknown；产物正确性通过，契约一致性证据充分性未采纳。 |
| 装配版本不符 | unknown | unknown；缺必要证据不补分。 |

不调整阈值把 candidate 强行改成 pass。领域阈值及误判仍需真人标注校准。

最终另外向 Jev 发送实际源码片段、测试结果与真实模型记录，对 8 个独立声明作 `supported / contradicted / insufficient` 核验，固定置信度门槛 0.8。**6 项实现声明获支持，原生 DSH 已全部接通的反例被否定；7/8 达门槛。真实任务收益声明返回 insufficient、置信度 0.36，未采纳。** 因测试要求所有题达门槛，该次显式验收测试退出 1；保留为未全通过，不降阈值、不重抽模型结果。该声明没有真实实验依据，不能据此宣称收益。完整概率摘要见 [jev-acceptance-summary.json](jev-acceptance-summary.json)。

这次验收只评阅提供的实施证据。Jev 没有亲自执行测试，模型信心不等于系统正确率。浏览器中的 `browser-fixture-reviewer` 是明确标记的自动表单夹具，不是真人的质量标签；E4 仍未运行。

## 预览与证据位置

独立预览：<http://127.0.0.1:19091>，默认在线评分关闭。旧 18090 实例没有替换。启动／API／配置详见[工作台说明](../../current/local-observation-workbench.md)。

私有证据根：`/home/xubohan/.local/state/repomesh-observe-jev-20260920-120900`；`archive/` 保存实际不可变事件、评分、付费预约、样本和标签。两个密钥只在私有配置文件中，不提交仓库、不回读到页面。

关键记录：

- `checks/delivery-summary.json`、`go-test-delivery.log`、`go-build-delivery.log`、`go-vet-delivery.log`、`go-race-delivery.log`。
- `checks/go-test-final.jsonl` 与 `baseline-models-concurrency-10.log`／`current-models-concurrency-10.log` 保留前次失败及复试。
- `checks/live-v3/` 保存冻结真实模型样本、旧失败聚类、新尝试及摘要；更早调试结果仍在 checks 根目录。
- `checks/jev-implementation-evidence.json`、`jev-implementation-result.json`、`jev-implementation-acceptance.log` 保存最终 Jev 输入与完整原始响应，未包含密钥。
- `checks/browser-final/` 保存截图、CSV、导航及样本表单结果；`checks/api-doc-check.json` 保存 API 基线差异。

本轮隔离 PostgreSQL 只供验证，交付时停止；预览服务继续运行。未执行原生 Agent、真实跨仓任务、真实人审效率对照或云观测操作。后续按[实验计划](../../plan/observation-platform-validation-and-experiments.md)分别推进运行采集与真实任务，不能将本轮受控 HTTP 产品当成 Agent 成功案例。
