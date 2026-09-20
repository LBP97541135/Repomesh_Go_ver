# TypeSafe / Jev 接入测试验证团队的可行性分析

日期：2026-09-20。源码基线：`801f9ea8f96d6a3f4ac36562eb8be312e4eaf07f`。
范围：仅在本次独立 worktree 阅读与分析；本文是建议方案，不是已实现、已采用或运行验收结论。

## 结论

技术上可行，建议作为测试验证团队的可选语义检查能力接入。已有设置页、Skill 版本与绑定、密钥信封加密、测试派发及证据读面可以复用；仍需实现 TypeSafe 专用调用、项目范围配置、测试任务的能力绑定以及语义检查记录。

建议先交付“保存 Key → 验证连接 → 测试 Agent 加载固定 Skill → 调用 Jev → 工作台显示可追溯判断”的闭环。第一版作为辅助评审，不直接改变自动合并条件。后续是否成为强制门禁，应基于真实样本校准和明确的项目配置。

## 官方能力与用途

- [Agent skill](https://docs.typesafe.ai/agent-skill) 是给编码 Agent 的使用指南，可独立阅读；真正调用 Jev 才需要 TypeSafe API Key。官方示例使用 `TYPESAFE_API_KEY`。开发环境加载该 Skill 不意味着产品中的测试 Agent 已加载。
- [Skill 原文](https://github.com/typesafe-ai/skills/blob/main/skills/typesafe-ai/SKILL.md) 指导 Agent 将语义判断拆为小问题，并由代码组合答案；它本身不执行测试、不提供 RepoMesh 的运行时连接器。
- [HTTP API](https://docs.typesafe.ai/api) 使用 `POST https://api.typesafe.ai/v1/systemone`，Bearer 鉴权，输入为 `state`、`model`、`questions`。Go 可直接实现 HTTP 适配，不必额外引入 Python 服务。
- [Choice](https://docs.typesafe.ai/primitives/choice) 可表达“支持／矛盾／证据不足”等有限结果；Noul 表达是否成立的概率；Score 表达分级程度。Jev 返回结构化判断，报告文字仍由测试 Agent 或界面生成。
- [Models](https://docs.typesafe.ai/models) 当前列出 `jev-1.13.0`，输入为文本。截图需要其他能力转成文本后才能送入；中文场景需专项评估。正式检查建议固定模型版本并记录响应中的实际版本。
- [证据引用检查示例](https://docs.typesafe.ai/cookbooks/citation_check) 先用代码检查引用是否存在，再判断语义是否支持结论，适合借鉴为测试报告的证据核对。
- [Confidence](https://docs.typesafe.ai/confidence) 来自答案概率分布，不能直接当作整个验证流程的正确率。阈值需要本项目样本校准。

适合第一版的问题：某条验收条件是否有相关断言；报告“已验证”的说法是否被日志支持；已提供的上下游契约是否存在语义矛盾。退出码、是否执行、提交是否匹配、文件是否存在等可确定事实由代码检查。证据不全时记录未能判断；模型无法补足未运行的联调。

## 已有代码与缺口

| 环节 | 基线源码事实 | 接入所需改动 |
| --- | --- | --- |
| 设置入口 | [SettingsPage](../../frontend/src/pages/SettingsPage.tsx) 有“模型与 API”“技能”；[ModelUsageSettings](../../frontend/src/components/ModelUsageSettings.tsx) 按用途展示配置 | 新增“测试验证 · TypeSafe / Jev”配置卡，明确作用的项目 |
| 普通供应商 | [input.go](../../internal/models/input.go) 只接受 `openai_chat_completions`；[transport.go](../../internal/models/transport.go) 请求 `/chat/completions` | 使用专用集成配置及 TypeSafe 适配器；不能只把供应商 URL 改成 TypeSafe |
| 密钥 | [secrets](../../internal/secrets/store.go) 已有 owner/purpose 校验、版本、启停与销毁；[crypto](../../internal/secrets/crypto.go) 使用带身份绑定的信封加密 | 复用底层能力，新增准确的用途/归属及配置引用；已有 purpose 白名单不能直接接受任意新用途 |
| Skill 管理 | [presets](../../internal/skills/presets.go)、[service](../../internal/skills/service.go)、[HTTP](../../internal/skills/http.go) 已有种子、版本、绑定、组合与快照接口 | 导入固定上游版本与来源、许可证、内容摘要；保留完整包和所需参考文件，附 RepoMesh 使用说明 |
| 测试团队 | `cross-repo-test-team` profile 存在，额外分配 `cross-repo-test`、`integration-run` | 只为启用的测试任务增加 TypeSafe 能力；不能加入所有 Worker 的默认技能 |
| 单点测试 | [ledger](../../cmd/repomesh-coordinator/ledger.go) 同事务安排开发与测试 run；测试 prompt 可注入 Skill，但同样调用 `ForRole("worker")` | 按实际项目、Agent/团队、任务用途解析一组固定版本的 Skill |
| Skill 查询 | [skillbridge](../../cmd/repomesh-coordinator/skillbridge.go) 显式绑定查询仅按 role，取最近一条，未限制当前项目/空间/Agent；`ForAll` 仅取全局种子 | 接入前修正解析范围，避免别的团队绑定被带入、附加 Skill 替换原测试能力 |
| 集成派发 | [integration](../../cmd/repomesh-coordinator/integration.go) 另有仓内集成、跨仓回归路径，当前 prompt 未调用 Skill bridge | 三类测试都使用一致的测试能力装配入口 |
| 执行环境 | [agent_launch_unix](../../cmd/repomesh-host-executor/agent_launch_unix.go) 用环境变量白名单，未允许 `TYPESAFE_API_KEY` | 新增受任务约束的调用通道；单在宿主 export Key 不会传入 Agent |
| 结果 | [0043](../../internal/database/migrations/0043_test_evidence.sql) 与 [scm_events](../../cmd/repomesh-host-executor/scm_events.go) 保存脚本、命令、退出码、布尔结论、摘要 | 单独记录 Jev 请求/判断及其证据来源，在现有测试记录旁展示 |

文档存在阶段差异：[ADR 0009](../adr/0009-skill-engineering-deferred.md) 和部分交接仍记载 Skill 暂缓，当前源码却已有上述实现。后续开发需补充本次采用范围与现有实现的对应关系，不能据旧状态否定已有代码，也不能据代码存在宣称运行已验收。本 worktree 没有独立 AgentTeams checkout；未读取或改动其他工作树中的上游源码。

## 推荐方案

### 设置与密钥

建议设置页保存项目范围的 TypeSafe 配置。第一版避免隐式共用一个全平台 Key；配置归属及写权限由服务端从当前项目和登录身份确认。

界面提供启用开关、密码输入框、保存/替换/清除、测试连接、模型及最近检查状态。读取接口仅返回是否配置和检查结果，不返回原 Key；保存不代表连接成功，Key 不写入 localStorage、Skill 内容、prompt、命令台账或日志。模型和凭据变更保留配置修订；凭据撤销后禁止新调用。

“测试连接”先检查官方 `/v1/models`；如需证明实际推理可用，再提供明确标注的小型合成内容检查，不能用模型列表可读冒充推理已成功。本轮未调用这些鉴权接口。

### Skill 与执行能力

固定导入官方 `typesafe-ai` 内容，另写 RepoMesh 测试使用说明，规定输入证据、工具入口、预算和失败处理。既有测试 Skill 继续承担跑测试、收证据和归属判断；TypeSafe 作为附加能力。

推荐由服务端保存真实 Key，测试 Agent 通过一个受限工具/命令入口请求语义检查，由 Go 适配器调用 TypeSafe。工具的具体协议需在实施设计中确定，现有 MCP 策略表不证明已有可直接复用的调用通道。身份绑定到当前项目、Issue、Attempt/run 及测试用途，服务端核验后再读取证据和配置；模型结果由服务端落账，不能只信任 Agent 自写的“Jev 通过”。

相比直接注入环境变量，该方案需要更多接线，但更容易限制调用次数、撤销权限及避免 Key 进入测试脚本。临时给测试进程注入 `TYPESAFE_API_KEY` 可作另一种实现方式，不过仓库测试进程也可能继承它；不建议通过全局白名单让全部开发/测试进程共享。

首批问题固定为少量可核对的判断模板。Agent 可提出待核对条件和证据片段，服务端验证证据归属、范围和大小，必要时批量发送独立问题。输入只包含所需的脱敏文本；设超时、请求大小、次数/用量上限。401、参数错误、限流、过载、网络未知结果分别处理，重试有上限。

### 证据与界面

建议增加语义检查子记录，关联测试记录和准确的代码组合。至少保存：项目/Issue/run、输入证据引用与摘要、Skill 版本/内容 hash、问题模板版本、配置修订、请求及实际模型版本、结构化答案/概率、用量、耗时、失败类别。

输入材料或提交变化时重新检查；不把旧判断挂到新提交上。区分未启用、待检查、成功得到判断、证据不足、调用失败。调用成功也可能得到“不支持”的业务判断。原始测试结果与 Jev 意见分别展示，不把 API 故障记成业务测试失败，也不把 API 故障记成验证通过。

第一版不让 Jev 单独修改现有 `passed` 或合并门禁。先积累标注样本，评估漏报、误报和无法判断的比例，再决定是否加入明确的强制验证策略。

## 直接影响接入的既有问题

以下来自当前源码静态核对，尚未运行复现；不是本轮已修复内容。

1. 集成记录引用解析不一致：coordinator 生成 `plan:<id>:kind:<kind>:repo:<owner/name>`，分隔后是 6 段，executor 的 `recordIntegrationEvidence` 却要求 7 段，正常引用会提前返回。完整闭环需先修复并覆盖实际形状。
2. 集成脚本 fetch `origin main` 后使用 `FETCH_HEAD`，未在此路径绑定待验证候选提交组合；跨仓 prompt 也承认只看到当前仓库。第一版可从单点测试接入，但若声称验证跨仓交付，必须先落实准确组合及真实联调证据。
3. `passed` 使用 Agent 声明和 Agent 进程退出码计算，而 JSON 内的测试退出码只作为记录字段；可能出现测试退出码非零但布尔通过的矛盾。新增语义检查前应明确并测试事实优先规则。

Jev 能辅助检查现有证据表达是否合理，但不能修复这些执行与记账缺口。

## 实施顺序与验收

1. 配置闭环：设置页、项目授权、密钥用途与持久化、专用 TypeSafe HTTP 客户端；覆盖保存/替换/删除、权限隔离、返回与日志不含 Key、超时和异常响应。
2. 单点测试闭环：Skill 固定导入及精确组合、受限调用入口、后台结果记录；证明开发 Worker 不获得该能力，启用项目的测试 run 实际使用正确 Skill/模型/证据。
3. 界面闭环：在已有测试记录旁显示语义判断、证据来源和调用状态；覆盖未配置、证据缺失、矛盾、限流、撤销和在途配置变化。
4. 集成与回归：修正上述相关链路问题，绑定候选提交组合，再扩展仓内集成和跨仓测试；中英文代表性样本单独校准。

验证应包含 HTTP 夹具、隔离 PostgreSQL、相关前后端检查；跨前后端契约与进程接线落地时执行仓库要求的完整检查。真实 Jev 冒烟使用配置好的 Key 和合成样本，实际业务资料评估另行明确范围。

本轮仅查阅源码、工程入口和 TypeSafe 在线文档，新增本分析文件并核对本地链接；未修改产品代码、安装 Skill、配置 Key、运行模型调用、启动服务、应用迁移或运行产品测试。
