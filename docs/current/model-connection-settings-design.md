# 模型连接与 API Key 设置

当前状态于2026-09-12核对。供应商分栏、模型参数及 F04 列明的三项 UI 已采用；[F05 Key 页面](model-key-save-design.md)新增 UI、认证及模型机器协议仍待采用。Key 保存／安全终结、测试、专用应用和 modelProfile 映射已形成[模型浏览器候选](model-settings-browser-api-draft.md)及[内部操作候选](backend-model-operations-draft.md)，不再属于完全未编制的协议。当前仍无业务实现，实际运行配置消费见[执行接入门槛](execution-integration-gates.md)。

**当前F04页面记录（三项UI已采用）：** 用户同意F03三项安排后要求继续下一项，新增[模型用于项目预览](model-project-apply-design.md)及串联原型弹窗。仅采用本专题§3已有目标的三项呈现，准确范围见页面CP11；现行PATCH不变，专用协议已有候选，尚未采用或实现。当前文件由继任页面设计师2维护，历史r3采用范围及原作者记录保持。

设计修订：RM-UI-MODEL-SETTINGS r3（第3／3轮，技术与最终落盘已互核，无分歧）。用户在新版原型及模型编辑弹窗展示后回复“ok可以的”，采用当前供应商分栏及模型参数填写方式。r2保存/测试/应用分离及专用应用协议依赖保持。唯一页面编辑者为页面接口设计师1，后端维护其内部设计。仅独立原型与文档，无业务实现或真实连接验证。

当前原型：[供应商与模型设置](../prototypes/repomesh-model-provider-prototype.html)。左侧供应商、右侧配置和多个模型，窄窗改为上方供应商选择；模型参数用小弹窗编辑。旧[连接弹窗原型](../archive/2026-09-12-development-preparation/docs/prototypes/repomesh-model-settings-prototype.html)保留作r2演进记录，不能作为当前布局。用户原话：“类似于这样，但是具体要求填的内容按照agentteams的配置方式”。

## 1. 当前布局与字段（r3）

供应商分组归当前真实账号owner管理，可用于其有配置资格的项目；不开放共享或编辑平台默认。供应商显示名称为RepoMesh元数据，一家可记录多个模型。项目应用必须明确一个模型及其供应商/模型参数/密钥版本对应的固定快照，不把供应商整卡当成一个唯一modelProfile。每模型行到稳定modelProfile的映射已列入[模型候选 M01](model-settings-browser-api-draft.md#m01-通用约束与对象)，仍待采用；多供应商到实际网关的运行路由仍需细化，不能循环覆写同一组全局环境变量来实现。

右侧可修改供应商名称、Base URL和密钥，显示当前已核对的API格式；模型列表显示模型ID/显示名、上下文窗口、最大输出、推理和图片能力，每个模型分别测试、用于项目。添加/编辑模型先进入配置草稿，显式保存才产生新版本。未保存草稿时禁用测试/应用，避免测试旧版本却让用户误以为测的是当前输入。切换供应商前提示先保存或撤销修改。

本次只覆盖锁定源码中的自定义OpenAI兼容接入，不将参考图中的Anthropic Messages下拉、已启用、禁用、删除和Key回显直接照搬。API格式展示OpenAI Chat Completions，并不意味着RepoMesh已实现适配。保存结果、测试结果、当前可执行资格分别表达。

### 1.1 源码基线与填写项映射

源码HEAD已读取并核对为`517caff9280242a00a4d4c06365352b9e41659c6`，来源见[记录](../../third_party/agentteams-source.json)。本轮仅静态阅读，没有运行上游脚本或连接服务。

| 页面填写/显示 | AgentTeams来源字段与含义 | 页面处理 |
| --- | --- | --- |
| 供应商名称 | RepoMesh管理显示名；不是LLM_PROVIDER的自由字符串替代 | 可填写，和实际适配类型分开。 |
| API格式 | `AGENTTEAMS_LLM_PROVIDER=openai-compat`路径 | 当前显示OpenAI Chat Completions；不虚构其他协议选项。 |
| Base URL | `AGENTTEAMS_OPENAI_BASE_URL` | 自定义接入显式填写；兼容接口基础路径，不附`/chat/completions`。 |
| API Key | `AGENTTEAMS_LLM_API_KEY` | 上游供应商密钥；产品只写不回显，原型仅演示占位。 |
| 模型 ID | 选用模型时`AGENTTEAMS_DEFAULT_MODEL`；runtime model记录`id` | 服务实际接受的完整ID，供应商内不能重复。 |
| 显示名称 | runtime模型记录`name`，并非安装器独立环境变量 | 可选，默认ID；保留为页面元数据，未来适配需核对是否保留自定义名称。 |
| 上下文窗口 | `AGENTTEAMS_MODEL_CONTEXT_WINDOW` → `contextWindow` | Token正整数，不能误写成价格或输出上限。 |
| 最大输出 | `AGENTTEAMS_MODEL_MAX_TOKENS` → `maxTokens` | Token正整数，与上下文窗口分开。 |
| 推理能力 | `AGENTTEAMS_MODEL_REASONING` → `reasoning` | 布尔能力声明，不是推理强度，也不代表业务执行开关。 |
| 图片输入 | `AGENTTEAMS_MODEL_VISION` → `input` | false为文本，true为文本+图片；不是任意模态Schema。 |

主要证据：安装器[Request-CustomModelParams / Step-Llm / Test-LlmConnectivity / New-OpenAICompatProvider](../../third_party/AgentTeams/install/agentteams-install.ps1)，控制器[配置读取](../../third_party/AgentTeams/agentteams-controller/internal/config/config.go)、[供应商初始化](../../third_party/AgentTeams/agentteams-controller/internal/initializer/initializer.go)，[已知模型表](../../third_party/AgentTeams/manager/configs/known-models.json)、[OpenClaw模板](../../third_party/AgentTeams/manager/configs/manager-openclaw.json.tmpl)、[启动参数解析](../../third_party/AgentTeams/manager/scripts/init/start-manager-agent.sh)。

三层名称不可混用：安装器选择`openai-compat`，Higress供应商协议为`openai/v1`，OpenClaw运行模板的`api`为`openai-completions`。安装器测试URL是基础地址去末尾斜杠后追加`/chat/completions`；不使用图中`/v1/messages`来冒充本条已核路径。未证明所有runtime拥有相同协议和字段能力。

已知模型预设只说明上述锁定源码记录的值，不是最新厂商规格或账户可用性。原型展示deepseek-chat、deepseek-reasoner、qwen3.6-plus三个预设供显式填入；参数仍可修改。未知模型在本页显式填写能力和Token数值，这是RepoMesh避免静默推断的表单选择：上游Windows安装器本身有150000/128000、reasoning=true、vision=false的交互回车默认，本页不把这些默认伪装成未知模型事实。原型的正整数、同供应商ID不重复和名称长度约束仍非完整机器Schema。

启动脚本约688—714行会保留已有同ID模型记录、仅补缺项；写入四个能力环境变量不保证既有runtime记录随之更新。新增自定义模型的name取模型ID。本页显示名是RepoMesh元数据，runtime参数更新与命名传递须由后续适配明确，不能据表单保存宣称运行生效。

### 1.2 密钥、模型与保存边界

供应商连接或其模型参数保存形成新的完整配置快照；本原型对新快照清除全部模型测试观察。测试按具体模型及保存快照绑定，某一模型通过不证明整家供应商或其他模型通过。原项目继续使用原固定快照；保存不自动更新它。真实设计保留历史版本测试观察；原型只保存当前内存版本，未实现历史查询。

真实产品目标为密钥只写不回显；编辑默认保留，显式替换才生成新密钥版本。原型密钥框只读，“使用/替换演示密钥”仅变更占位状态；不使用localStorage/sessionStorage，不发送地址/密钥/探测请求。供应商原始Key与runtime使用的网关Consumer凭据（例如模板的MANAGER_GATEWAY_KEY）分开，本页不收网关、Matrix或宿主密钥。

`AGENTTEAMS_EMBEDDING_MODEL`为上游独立可选配置，安装器单独请求`/embeddings`测试；本页当前只做主语言模型。RepoMesh的记忆/embedding使用及接入尚未设计，不将它当作主模型必填，也不标为已完成。

## 2. 状态与反馈

新连接和修改后的配置版本显示“未测试”；旧版本测试结果不能移植为新版本结果。本次模型访问拒绝是有预置原因的演示场景，不把全部失败归因于Key错误。未知与失败分开。

| 状态 | 页面呈现与后续 |
| --- | --- |
| 未测试 | 保留已保存连接，可单独发起测试，也可保存待配置项目引用。 |
| 测试通过 | 显示观察时间及仅本次探测通过的范围说明。 |
| 测试失败 | 显示有证据的原因，允许修正或重新测试；已保存连接仍保留。 |
| 测试结果未知 | 查询原测试，不自动发起第二次可能收费的探测。 |
| 保存结果未知 | 核查原保存；不自动重新提交密钥或生成另一写操作。 |

测试方式、可能费用提示、观察有效期、可测试权限和错误枚举已列入[模型字段候选](model-settings-browser-api-draft.md)及[来源候选](backend-first-batch-sources-draft.md)，尚未采用与实现。未测试/失败不等于不能保存引用，也不能据此授权执行；项目必要条件按现行核验处理。保留某个执行旧版本不恢复其已失效的当前可用资格。

原型只用单个内存待保存对象模拟原操作查询；未知恢复示例固定返回同次成功，不证明服务端幂等、跨刷新恢复或失败终态处理。测试时间为浏览器本地演示时间，非服务端证据。

## 3. 项目应用：交互目标及协议依赖

采用的技术目标是：用户在“用于项目”弹窗预览模型连接及不可变版本、原项目模型、执行配置引用及有效版本，明确提交后**仅更换模型，精确保留原execution引用及有效版本**。提交应绑定预览模型版本与项目修订；预览对应条件发生变化时回读并重新明确，不能静默选最新版本。其他项目与在途Issue不随此操作切换。

**2026-09-12 R04 推荐修订，待采用：** “在途 Issue 不随切换”还缺少 Issue 到原配置的持久关联。建议创建事务写入不可变 `initialConfigurationRevision`，引用同项目 `ProjectConfigRevision`；后台重启后沿该引用重核原配置当前是否可用。完整候选及并发、密钥失效、旧记录修复和普通讨论边界见[Issue 配置绑定](issue-configuration-binding-design.md)。这不是新增已采用字段，也不扩大当前 UI 确认范围。真实 Issue 与待办首次保存前应确定该绑定；共享 Manager 的全局配置切换不能充当每 Issue 固定版本的实现。项目应用新配置不会自动修复旧 Issue 的失效 Key 或缺失绑定。

这项原子保证由[专用应用候选 §5](model-settings-browser-api-draft.md#5-仅更换模型预览与原子应用)规定，尚未采用与实现。现行[首批浏览器契约](first-batch-browser-api-contract.md)§5/7仅接受modelProfile的mode/id，没有客户端指定不可变连接版本或预览版本比较；完整configuration提交会重新解析两项引用。即使executionProfile仍是原inherit/同id，也可能得到新的有效版本。因此**不能直接调用现行完整PATCH并宣称满足上述目标**，本稿不更改RM-API-01 r3，不新增虚构REST字段。

r3原型项目仅有“订单系统”一个示例，执行配置“开发容器 · v2”是预置展示值。JavaScript赋值展示未来交互效果，不是版本比较、权限核验或事务实现。供应商、模型行、固定快照与modelProfile的映射，以及应用请求、原子比较、幂等回执和恢复接口已形成上述候选。原模型受限形状与固定来源定位仍有[字段缺口 C06](../reviews/2026-09-12-project-contracts/README.md#c06-应用预览缺少原模型的完整形状)；补齐并采用相关方案前，不能将完整预览标为可验收。

## 4. 未覆盖范围与交付边界

未覆盖真实Key保存/加密/安全传输、配置/密钥版本保留及轮换撤销、用户权限变化、共享、平台默认切换、禁用删除、受控出站、运行凭据传递、测试费用/有效期、应用竞态和跨刷新/多标签恢复。不提供这些未完成行为的假按钮。

用户在被询问模型参数填写方式是否符合预期后回复“ok可以的”，采用本次分栏及模型参数编辑的已展示呈现。新增或未展示的恢复、权限和运行交互仍需分别展示讨论；本次确认不扩大此前详情、交付、消息控件的UI采用范围。

设计目标、原型走查、真实实现与验证分别记录于[页面通信日志](../archive/2026-09-12-development-preparation/docs/current/design-communication-page-2026-09-10.md)。密钥、测试和应用机器协议的当前候选由模型浏览器稿统一维护；后续补齐字段与采用范围，不从本页原型另造协议。
