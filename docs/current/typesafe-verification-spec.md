# TypeSafe / Jev 测试验证接入 Spec

状态：本次用户授权实施的 v1；验收状态见当前交接。日期：2026-09-20。
源码起点：`801f9ea`；前置分析见[可行性分析](../research/2026-09-20-typesafe-verification-feasibility.md)。

## 1. 目标与采用范围

在“设置 → 模型与 API”按当前项目配置 TypeSafe Key。启用后，测试 Agent 能加载固定版本的官方 `typesafe-ai` Skill，通过受运行范围约束的工具调用 Jev；用户可在测试记录中查看判断、来源、模型和失败原因。

v1 采用辅助验证模式：Jev 判断不改写真实测试退出码、`passed` 或合并许可。未启用项目保持原流程；未配置、调用失败和证据不足均明确表达。覆盖任务单点、仓内集成和跨仓回归的工具接入；这不表示既有跨仓执行已绑定候选提交组合。调用方提供的证据文本和提交声明标记为 Agent 提供，不升级为平台已验证的事实。

本次明确恢复 ADR 0009 中第三方 Skill 固定导入、测试用途分配、调用留痕的有限实施范围；其他 Skill 工程规划不随本功能扩大。既有源码已有的 Skill 管理不重新实施。

## 2. 用户行为

1. 在当前项目的 TypeSafe 卡片输入 Key，可保存/替换、启用/停用、清除。统一开关为“启用 Jev 辅助测试与代码审查”，同时控制两种用途。
2. 读取只返回 `configured`、`enabled`、`model`、`revision`、最近连接检查和固定 Skill 版本；不返回 Key。
3. 保存后点击“检查连接”，用保存的 Key 发一次合成内容判断，同时确认鉴权和推理；界面标明会调用外部服务。保存自身不触发付费请求。
4. 测试 Agent 运行时获得官方 Skill 和 RepoMesh 调用说明，逐条核对有证据的验收说法；没有足够材料就报告证据不足。
5. 测试记录展示独立的 Jev 辅助判断，包括 `supported / contradicted / insufficient`、概率与 confidence，以及调用失败/未知状态；不生成虚构解释。

配置按项目保存，当前登录账号须有该项目的配置权限。写入使用现有 Origin、会话和 CSRF 保护以及配置修订检查。切项目后取消旧响应的 UI 更新；Key 只在当前表单内存中存在。

## 3. 组件与数据流

浏览器 → Web 项目配置 API → 现有 secrets 信封加密库。

host-executor 在启动测试 run 时确认项目配置、签发短期随机凭证并只保存其 SHA-256；工具获得凭证和 Web broker 地址。凭证只对应一条 `test_agent` 或 `review_agent` run，用途由运行种类决定，不能由调用方改写。Go helper 从标准输入/文件读取判断请求，调用 Web broker；Web 从 run 推导项目/Issue，检查运行状态、配置修订、过期和额度，解密 Key 后调用固定的 TypeSafe HTTPS 端点。

真实 Key 不进入 executor/Agent 的环境、prompt、argv、Skill、日志或源码。短期凭证只进入对应测试子进程环境，run 结束即撤销。没有通用代理、自定义目标地址或任意模型协议入口。broker 地址由部署配置 `REPOMESH_TYPESAFE_BROKER_URL` 提供，HTTPS 或本机 loopback HTTP；helper 禁止重定向。

官方 Skill 完整固定在源码中，记录 Git 提交与每个文件摘要，构建时嵌入。工作树 `.agents/skills/typesafe-ai` 引用同一目录；测试运行在自己的 checkout 临时安装此包并显式要求使用。已有同名目录内容与固定包完全相同时复用且不接管清理；内容冲突时不覆盖；通过明确的准备失败记录表达。临时 Skill 使用后清理，仅清理本运行安装的目录。普通开发 run 不签发 Jev 凭证。调用环境使用 `REPOMESH_TYPESAFE_GRANT` 传递短期凭证。

## 4. 浏览器及工具契约

| 接口 | 行为 |
| --- | --- |
| `GET /api/projects/{projectId}/typesafe` | 读取脱敏配置，未配置返回默认关闭状态 |
| `POST /api/projects/{projectId}/typesafe` | `expectedRevision`、`enabled`、固定版本 `model`、`secret.mode=keep/replace/clear`；replace 带 value，clear 同时关闭 |
| `POST /api/projects/{projectId}/typesafe/check` | 对 expectedRevision 的保存配置做合成推理，记录成功/错误；不把旧版本结果附到新配置 |
| `POST /api/typesafe/evaluations` | 仅短期 Bearer 凭证；输入 requestId、claims（有限 id/text）、evidence 文本、可选 commit 声明；服务端绑定 run/项目/Issue/用途 |
| `GET /api/projects/{projectId}/typesafe/evaluations?issueId=…` | 当前项目的记录，最多最近 100 条，issue 必须属于项目 |

以上是本功能的新增接口。HTTP 输入严格验证，错误只给稳定类别，不返回供应商原始错误体或原 Key。

工具通过 host-executor 二进制子命令提供，不要求测试镜像安装额外 SDK。每条 claim 使用固定 Choice 模板，选项为 supported、contradicted、insufficient；证据是不可信数据，不能修改题目规则。服务端附任务/运行身份，固定模板和模型，防止 Agent 改用更宽松的题目。API 直接使用 TypeSafe `state/model/questions`，保留其响应的实际模型版本、用量和完整概率。

## 5. 持久化与边界

新增迁移（在当前 0057 之后）：

- 项目配置：修订、开关、模型、secret version 引用、更新时间、最近检查结果；密钥用途 `typesafe-api-key`，owner 为项目。
- 运行授权：run、项目/Issue、配置修订、随机 token 摘要、到期时间、调用计数、Skill 内容摘要、撤销时间；校验关联关系由服务端派生。
- 判断记录：run、requestId、项目/Issue、配置修订、Skill 摘要、模板版本、脱敏输入及 hash、提交声明、状态、返回答案、模型、用量、耗时、错误类别。

同一 run/requestId 幂等：相同输入返回已有状态，不同输入拒绝。先持久化预约再外发；超时/断连保留 unknown，不自动重复计费。进程崩溃留下的过期 pending 在读面呈 unknown；不能伪造失败或成功。

v1 每 run 最多 8 次调用，每次最多 8 条 claim，输入正文上限 32 KiB（服务端额外限制证据和 claim 长度），单次调用 20 秒。调用前重新检查启用、配置修订和 Key 可用性。配置更改使旧授权失效；在途外发可能已完成，结果保留原修订，不冒充新配置结果。凭证最长两小时，run 结束撤销。连接检查也有项目级冷却与并发限制。

429/529、鉴权失败、请求拒绝、无效响应、网络未知分别记录；v1 不自动重试外部推理，调用次数和费用可预期。常见凭据片段在送出/留档前脱敏；该能力不能保证识别所有业务秘密，UI 明示会发送所选证据文本至 TypeSafe。

## 6. 必要的既有链路修正

- 测试 Skill 不再经仅按 role 取一条全库绑定的方式选择；使用明确的测试基础 Skill 加固定 TypeSafe 包，普通开发 Skill 解析不借本任务重写。
- 仓内/跨仓集成调用相同的测试工具准备与清理步骤。
- 修正集成引用六段/七段不一致，并核对 `passed` 与测试退出码矛盾；增加针对性测试。
- 当前集成基于 main 的限制明确保留在证据/文档中。Jev 没有验证到的候选组合不能显示成已验证交付组合。

## 7. 代码审查归属与接入

按用户补充要求，统一开关也启用仓库负责人的新代码辅助审查。现有 `internal/skills/presets.go` 把 `code-review`、`test-review`、`worker-result-evaluation` 分配给 `role=manager`，即 Skill 中的 Repository Leader。代码审查位于 Worker 候选完成后、`ApproveStep/RejectStep` 经理验收门前；项目级总负责人消费结果，跨仓测试团队继续负责验证。

当前只有审查 Skill 与接受/驳回入口，没有独立审查派发。v1 对开关开启后新派发的任务追加一条 `review_agent` 运行，复用同一执行器，按开发→测试→审查顺序执行。审查 run 使用 `code-review` 固定指令和 TypeSafe Skill，在独立克隆中检查开发候选 HEAD，移除远端，不给 SCM 凭据；不编辑开发工作树，不直接调用审批或合并。调用记录标明 `code_review` 和 taskId，显示在对应任务的经理验收区域。

Jev 仅核对有 diff/上下文证据的具体断言，审查 Agent 提供带文件/行号的发现和建议。它不能仅凭模型判断证明任意代码无缺陷。经理仍保留最终决定；辅助审查未完成不偷偷新增强制门禁。停用会拒绝已有授权的新调用；已排队审查仍可完成普通只读审查，不再调用 Jev。存量已派发任务不追溯追加运行。

## 8. 验收条件

| ID | 可观察结果 |
| --- | --- |
| TS01 | 正确项目可保存/替换/清除；并发修订冲突拒绝；其他账号无权读写 |
| TS02 | 明文不进入数据库、GET、错误日志和测试进程；清除后不能新调用 |
| TS03 | 官方 Skill 源码固定，内容 hash 可复核，Codex 实际读取并调用工具 |
| TS04 | 普通开发 run 无凭证；过期、撤销、停止、错误项目/修订及额度超限被拒绝 |
| TS05 | 支持、矛盾、证据不足样本得到完整结构化结果；概率/答案格式有校验 |
| TS06 | 重复 requestId 不重复外发；不同输入复用同 id 被拒绝；超时为 unknown |
| TS07 | 连接检查实际运行合成判断；旧修订检查不能污染新配置 |
| TS08 | UI 能保存配置并显示独立判断及错误，原测试 passed 不被 Jev 改写 |
| TS09 | 单点和集成准备路径有测试，Skill 冲突不覆盖，结束清理只移除自己的文件 |
| TS10 | Go build/test/vet、web 类型/测试/构建、frontend 构建/lint 和隔离 PG 检查通过 |
| TS12 | 统一开关控制测试和审查；新任务追加独立审查 run；用途不可伪造，开发目录与审批状态不被审查改写 |
| TS11 | 用用户授权的真实 Jev Key、合成证据及 Codex CLI 验证实际 Skill→工具→Jev→记录；报告实际结果与限制 |

不启动或升级其他 worktree 的服务和数据库。真实 API 验证仅发送合成材料；测试用临时凭据与服务结束后清理。实施结果单独记录，不把本 Spec 的要求直接写成已通过。
