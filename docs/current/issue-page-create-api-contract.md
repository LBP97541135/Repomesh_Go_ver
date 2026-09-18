---
status: accepted
date: 2026-09-09
---

# Issue 栏创建与默认新会话：接口契约 v1

**基础工程阶段补充（2026-09-10）：** 本契约继续是已采用、未实现的业务设计。新增 Web 骨架只提供静态页及 `GET /healthz`、`GET /readyz` 进程探针；前者 200 仅表示 Web 可响应，后者 503 表示业务未实现。本稿所有 `/api` 业务路径均未注册、返回 404，也没有 MCP 或 SSE 处理器。该 404 是骨架尚无路由，不是下文设计中的权限隐藏、创建不存在或幂等结果；不能用它验收这些业务语义。工程命令及职责见[基础工程开发说明](development-scaffold.md)。本轮不改写下方字段基线，不新增可执行 Schema、数据库表、消息或恢复算法。

用户先批准 P1 原子范围，随后明确“之后的待决定都批准，按照建议来。”本稿按[持续设计授权](design-delegation.md)采用为当前创建接口设计基线；不是接口已实现。原 DESIGN-004 第 3／3 轮保持，未写完的其他模块继续按推荐细化，不再等待逐项批准。

依据：[创建机制稿](manager-create-issue-tool-design.md)、[后端专项稿 §4—§5](draft-conversation-backend-design.md)、[ADR-0016](../adr/0016-transactional-background-work.md)、[ADR-0019](../adr/0019-conversation-issue-separation.md)。J1／J3 的必要配置与按本次仓库范围核权继续有效，旧聊天转正与旧启动图不恢复。下文 URL、字段、枚举、限额及 HTTP 状态分配为本次采用的创建契约。

## 1. 页面行为与契约范围

后续用户采用创建表单中的“仓库分析”按钮及 Python 插件，详见[仓库分析专题](issue-creation-repository-analysis.md)。这是可选的建项前辅助；本稿补充按钮可用性与可选分析来源字段，既有不带分析的请求仍有效。独立分析作业的 202 受理不改变本稿 Issue 创建的 201／200 语义。

用户在当前项目 Issue 栏打开创建表单，填写标题、目标／约束、已明确的验收条件，选择本次仓库。表单默认显示“关联会话：新建会话”，也可明确选择当前项目有权的已有会话。成功后进入 Issue 详情；新建分支在侧栏增加会话，已有分支更新原会话关联；默认主 ChangeSet 属于本 Issue。会话中的记录标明页面来源，不能冒充用户聊天或 Manager 回复。

本稿覆盖一次页面建项及其结果读取。不把浏览器拆成先创建会话、再创建 Issue、再创建 ChangeSet 的三次写操作。Manager MCP 通过受控入口接入同一个业务创建命令；身份及来源不同，事务与结果保障一致。具体工具输入见[机制稿](manager-create-issue-tool-design.md)。

| 页面动作 | 接口 | 成功含义 |
| --- | --- | --- |
| 打开创建表单 | `GET /api/projects/{projectId}/issue-creation-options` | 返回当前创建条件及可选仓库，尚未创建任何对象。 |
| 选择已有会话 | `GET /api/projects/{projectId}/issue-creation-conversations` | 分页返回当前可关联的同项目会话，不创建或启动对象。 |
| 明确提交 | `POST /api/projects/{projectId}/issues` | 本地创建结果已提交；不等待模型、实例或建房。 |
| 超时／丢失响应后核查 | `GET /api/projects/{projectId}/issue-creations/{creationId}` | 查询同一次逻辑操作的已提交结果，避免再建一套。 |
| 打开或刷新详情 | `GET /api/issues/{issueId}` | 当前有权读取的事项摘要，不包含聊天历史。 |
| 展示房间入口 | `GET /api/issues/{issueId}/rooms` | 当前房间关联及访问依据，不能由标题或会话存在推断。 |
| 详情状态通知 | `GET /api/issues/{issueId}/events`（SSE） | 通知重新查询快照；通知本身不是创建或执行命令。 |

当前 Issue 列表和项目会话列表在创建成功后按返回 ID 更新，再重取各自查询结果；其分页、搜索及跨客户端实时更新另议。本稿的 Issue SSE 不代替项目级列表通知，也不创建空会话承载项目事件。

## 2. 创建条件查询

调用者是当前登录用户。先核验项目可见性，再返回允许披露的仓库与限制。示例：

```json
{
  "projectId": "prj_orders",
  "creationContextRevision": "ctx_17",
  "defaultConversationMode": "new",
  "allowedConversationModes": ["new", "existing"],
  "canSubmit": true,
  "blockingReasons": [],
  "repositories": [
    {
      "repositoryId": "repo_service",
      "displayName": "repomesh/order-service",
      "selectable": true,
      "reasons": [],
      "observedAt": "2026-09-09T13:00:00Z"
    },
    {
      "repositoryId": "repo_billing",
      "displayName": "partner/billing-api",
      "selectable": false,
      "reasons": ["APP_PERMISSION_MISSING"],
      "observedAt": "2026-09-09T13:00:00Z"
    }
  ],
  "nextCursor": null,
  "repositoryAnalysis": {"availability": "available", "reasonCodes": []}
}
```

- `creationContextRevision` 是服务端不透明修订，覆盖本地项目仓库范围、配置引用和适用创建策略；不是 SSE 游标，也不是运行就绪版本。具体生成／存储算法不在本稿指定。
- 2026-09-12 审查澄清：该令牌也不是可解析的历史配置引用。[Issue 配置绑定候选](issue-configuration-binding-design.md)建议在同一创建事务另存不可变配置关联；此候选不向本契约请求或成功回执偷偷增加字段，采用状态及并发规则见该专题。服务端不能靠后来读取项目当前配置还原原 Issue 的配置。
- `canSubmit` 表示调用者有创建资格、通用必要配置满足且存在可选范围，不承诺任意仓库组合都能提交。最终校验针对明确提交的集合。
- 返回项目内当前可披露的仓库；`repositoryId` 稳定，名称仅展示。查询不默认全选，也不在提交时吸收新出现的仓库。
- 缺配置、缺 App 能力、当前用户无资格、外部核查未知分别表达。受限仓不拖累仅选可用仓的请求。未读授权内容不经错误或候选列表泄露。
- 管理面配置、预算和适用限制按既有规则核验；不要求 runtime Ready，不预留 Worker，不要求先有生效计划，不要求填写分支或密钥。
- 外部能力只是有时间的观察，不随本地修订成为持续授权。提交及实际执行仍分别核验。条件查询成功不授予执行能力。
- 仓库候选采用 `cursor`／`limit` 分页，默认 50、最大 100；返回 `nextCursor`，末页为 null。稳定按 repositoryId 排序，游标绑定项目、查询范围与创建修订；本地修订变化返回 409 CREATION_CONTEXT_CHANGED 并重新载入，不静默截断。每页重新核权，外部观察不承诺跨页快照一致。
- 已有会话候选使用 `GET /api/projects/{projectId}/issue-creation-conversations?cursor=...&limit=50`，按稳定 id 排序，仅返回可读且可关联的同项目会话，形状为 `{"items":[{"id":"conv_24","title":"订单讨论"}],"nextCursor":null}`。默认／最大分页同上；游标是遍历位置，不是授权，新增较早位置条目需刷新。提交时再次验证所选会话。

创建条件响应补充 `repositoryAnalysis: {"availability":"available","reasonCodes":[]}`；其枚举与独立作业协议见[仓库分析专题 §4](issue-creation-repository-analysis.md#4-浏览器接口补充)。插件不可用不使原本可以手动创建的 `canSubmit` 变为 false。

## 3. 创建请求

```http
POST /api/projects/prj_orders/issues
Content-Type: application/json
Idempotency-Key: 93d25876-e728-4f2e-b680-c7c5b951f163
```

```json
{
  "expectedCreationContextRevision": "ctx_17",
  "conversation": {"mode": "new"},
  "title": "增加订单归档",
  "description": "归档历史订单，保持原有读取权限。",
  "repositoryIds": ["repo_service"],
  "acceptanceCriteria": ["无读取权限的用户无法访问归档订单"]
}
```

| 字段 | 要求与含义 |
| --- | --- |
| 路径 `projectId` | 当前项目的稳定身份；请求体不重复声明另一个项目。 |
| `Idempotency-Key` | 必填 UUID；即本稿 `creationId`。浏览器在首次提交前产生，并与这次确切输入一起保留。不是密码或权限凭据。 |
| `expectedCreationContextRevision` | 必填，来自创建条件查询。检测表单打开后本地条件变化；不代替外部核权。 |
| `title` | 必填、非空白，最多 200 个 Unicode 标量值；只是标题，不承担去重身份。 |
| `description` | 必填、非空白，最多 20,000 个 Unicode 标量值；记录本次明确目标、约束及预期结果。 |
| `repositoryIds` | 非空、不重复的稳定 ID 集合，必须全部属于当前项目并满足本次创建条件；不允许静默删除受限仓库。最多 100 个仓库；整个 UTF-8 请求体最多 256 KiB。 |
| `acceptanceCriteria` | 可省略，等价空数组；每项为非空白字符串。只记录用户明确内容，未填不等于已通过验收或免除后续澄清。最多 100 项，每项最多 2,000 个 Unicode 标量值。 |
| `repositoryAnalysisId` | 可省略；仅在用户应用分析建议时传入同项目、同操作者的分析 UUID。省略表示未关联，不接受 null 或客户端报告正文。最终选仓仍以 `repositoryIds` 为准。 |

新增可选 `conversation` 字段：省略等价 `{"mode":"new"}`；已有会话须为 `{"mode":"existing","id":"conv_24"}`。new 不允许 id，existing 必须 id。选中会话失效、跨项目或不可关联时报 404 RESOURCE_NOT_FOUND 或可披露的 409 CONVERSATION_UNAVAILABLE，不静默切换新会话。

请求不接受顶层 conversationId／conversationMode、用户身份、角色、房间 ID、计划状态、startRuntime、approved、分支或密钥。来源类型由页面路由设为 issue_page，操作者由可信认证上下文提供。额外未知字段返回验证错误。

服务端按解析后的允许字段比较请求：对象键顺序不影响相等；仓库集合排序后比较；省略验收数组规范化为空数组；省略 conversation 规范化为 mode=new，关联方式及已有会话 id 纳入相等比较。文本保留提交内容，不以相似度、标题或会话名判断同一操作。数组中的验收顺序保留，修订值也属于本次输入。规范化后的输入及摘要由服务端生成，不能信任客户端提供的摘要。

创建时新会话标题采用本次 Issue 标题作为初始值，随后独立改名，不建立自动双向同步；已有会话保留原名。接口不允许页面靠名字查找已创建会话。

新操作附带 `repositoryAnalysisId` 时，服务端核验当前报告读取权限、同项目／操作者、成功终态和不可变结果，并比较确切标题、描述、规范化验收条件及创建上下文。报告可为明确标记的部分结果；用户可选择建议之外的有权仓库，也可取消建议仓库。最终仓库集或关联会话不必等于分析结果，分析不能替代本次选仓校验。

来源 ID 纳入规范化比较及确切输入快照，省略与附带 ID 是不同输入；核心同时保存对应输入指纹、固定仓库快照集及插件／结果版本引用。来源关联与 Issue、主 ChangeSet、其他来源、幂等结果和待办同一短事务提交，分析计算不进入创建事务。schemaVersion=1 的本设计基线包含这一可选字段；当前尚无已部署旧 schema 数据。

不可见／跨作用域的来源按 404 隐藏；可披露但尚未完成或与当前输入不符为 409 `ANALYSIS_NOT_APPLICABLE`，已清理为 410 `ANALYSIS_RESULT_REMOVED`。页面允许重新分析，或由用户明确移除来源后手动创建；请求结果未知时先核查原 creationId，再决定新输入，不能静默丢掉来源重试。已经提交的同键同输入重放沿用原结果，只核验当前结果读取权限，不因报告后来过期／清理或插件停用把历史成功改为失败。

## 4. 本地提交与成功响应

**P1 已采用：新业务会话、与 Issue 的来源关联及真实页面创建记录，与 Issue、默认主 ChangeSet 和既有后台待办在同一个本地短事务保存。** 一起成功或一起不生效，成功时完整建立三种业务身份，失败不留半套业务记录。

确认来源：P1 来自用户“确定”；输入快照、幂等结果、已有会话及后续运行等补充来自本次[持续设计授权](design-delegation.md)。沿用 ADR-0016 的事务外调用与未知结果核查保障，不改写两次授权的时间范围。

快照保存确切允许字段、服务端规范化摘要、schemaVersion=1、可信操作者／来源和结果 ID；与创建结果同事务提交。Manager 来源另存可核验的真实消息引用，不复制整条聊天、密钥或隐藏推理。已有会话分支保留会话与旧消息，仅同事务增加来源关联及创建记录。

```text
认证 / 当前项目及结果读取权限核验
  -> 核查同一创建身份的已提交结果
  -> 新操作核验创建权限、当前范围、必要条件、输入及预期修订
  -> 短事务内再次核验相关本地修订、唯一操作身份
       必要的新会话 + Issue + 主 ChangeSet + 来源关联 / 创建记录
       + 既定后续待办
       + 输入快照 / 幂等创建结果 / 持久通知事件
  -> 提交成功后返回 201
  -> 运行观察、接手和执行分别继续取证
```

外部权限核查不占用长事务；事务只验证其适用本地依据及修订，不声称与 GitHub 原子一致。重复提交的唯一约束与竞争处理必须覆盖最终提交，不能只做一次查询后插入。失败不留下成功的一半。数据库锁和表结构交后端实现细化。

命中已删除结果的幂等占位时，先按当前权限返回 410 CREATION_RESULT_REMOVED，不进入新建或正文比较分支；删除后的历史键不能重新获得创建资格。

首次成功返回 `201 Created`，`Location: /api/issues/iss_103`。同键同输入的已提交重试返回 `200 OK` 及同一结果；不再次检查新建条件并拒绝历史成功，但仍检查当前读取权限。响应：

```json
{
  "creationId": "93d25876-e728-4f2e-b680-c7c5b951f163",
  "status": "committed",
  "projectId": "prj_orders",
  "createdAt": "2026-09-09T13:00:03Z",
  "source": {"kind": "issue_page"},
  "issue": {"id": "iss_103", "number": 103},
  "mainChangeSet": {"id": "cs_103"},
  "conversation": {"id": "conv_24"},
  "links": {
    "issue": "/api/issues/iss_103",
    "rooms": "/api/issues/iss_103/rooms",
    "operation": "/api/projects/prj_orders/issue-creations/93d25876-e728-4f2e-b680-c7c5b951f163"
  }
}
```

这是持久创建回执，不是当前运行状态快照。`committed` 只属于创建操作；Issue 全生命周期枚举另定。ID 示例不规定生产格式。操作者详情按历史可见性规则单独返回，不让原始认证凭据进入响应。

前端用返回 ID 进入详情，加入或刷新会话列表，再查询实际状态。默认新会话不自动产生用户首条聊天消息，也不把创建记录塞进消息投递队列冒充用户指令。

本稿不采用异步受理 `202`：创建只等待有界的本地提交，不把持久化未完成称为建项成功；若未来确需预登记受理队列，需另定受理与终态查询契约。数据库繁忙或提交结果无法确定时按 §5 核查，不能推导必然失败。

## 5. 幂等、并发与结果未知

幂等作用域为“项目＋可信操作者＋页面创建入口＋creationId”。用户不能通过更换请求体身份重放别人操作。查询入口只查询当前操作者在该项目的页面创建操作；管理运维读取另定，不自动授权给普通用户。

| 情况 | 后端结果 | 页面后续 |
| --- | --- | --- |
| 同键、同规范化输入已提交 | `200`，原三种业务 ID | 显示原结果，不新增会话。 |
| 同键、不同输入已提交 | `409 IDEMPOTENCY_CONFLICT` | 保留原操作，不把它改绑新内容。 |
| 同键并发、首个提交尚不可见 | 短暂等待唯一提交；无法在有界请求内确认时 `503 RESULT_UNCONFIRMED` | 查原键；不换键，不显示已创建。 |
| 响应丢失、断网、网关错误或中断等待 | 客户端结果未知 | 保留原输入与键，先查询。 |
| 查询找到已提交记录 | `200`，与 §4 同一回执形状 | 继续读取当前详情。 |
| 查询没有当前可见的提交记录 | `404 CREATION_NOT_FOUND` | 不证明旧请求将来不会提交；继续核查或用原键、原输入重试。 |
| 查询失去当前访问权 | 不披露原内容的 `403`／`404` | 显示受限，不以此重建。 |

本稿查询返回已提交结果，不凭空增加 `failed`／`running` 持久状态。可确认的验证失败返回错误，但不声称服务器保存了失败操作记录。所有 `5xx`、网络中断及不能确定来源的错误都按结果未知处理；只有本请求明确验证拒绝且没有其他同键在途请求时，才可认为本次未提交。

`404` 的核查请求应读具备足够一致性的操作记录源；即使当前确实不存在，也不能排除原请求仍在途。等待超过阈值不自动换键。用户若主动提交另一项同名工作，要形成新的明确操作，不是重试去重的替代。

同一页面操作的键在项目保留期内不允许复用。输入及结果随项目历史保存；业务内容被明确删除时，保留不含正文的最小幂等占位（作用域、键、结果已删除标志及必要关联身份），旧请求返回 410 CREATION_RESULT_REMOVED，不重新建项或返回已删除正文。项目物理删除后旧 projectId 永不复用。不得以短缓存过期为由允许重新创建。键与输入丢失后不可根据标题自动认领旧项；应核查可读历史并明确是否另建。

### 5.1 浏览器操作恢复（RM-PAGE-01 r1，2026-09-10）

本节为双方确认的页面持久记录策略，保持 v1 的请求／响应／错误字段不变。首次提交前，在 `sessionStorage` 按可信登录主体、项目、creationId 隔离保存确切允许输入和原键，用于本标签刷新后的原请求恢复；`localStorage` 只存同主体隔离的 projectId、creationId、时间等最小索引，不存标题、正文、仓库名、凭据。它们不是权限依据，不把本地 userId 当后端身份参数。

浏览器恢复路由为 `/projects/{projectId}/issue-creations/{creationId}`，只调用本稿原操作查询，不必提前知道 issueId。若本地存储不可用，发出前提供可复制恢复链接并明确关闭后不能保证自动恢复；创建键已经生成并保持该次请求内稳定。不会因为存储失败在请求后再生成新键。

请求发出后的切页、关闭标签或取消等待只停止页面等待，不构成撤销。回到页面先查询；不能从超时、网络错误、401／403／404、经过足够时间或本地条目缺失推断原操作未发生。原输入仍可恢复时才提供原键原输入重试；只有键时仅核查，不能按标题猜测请求内容。同标题的新工作须由用户明确发起独立操作，不是故障恢复自动分支。

注销、401 或确认失权时，清除对应身份／对象的敏感草稿、正文缓存及订阅；原操作最小索引不在无权页面展示。同一主体恢复登录后仍由后端核查原结果；换账号不能认领原操作。返回受限响应时还清理派生列表、选择器和相关缓存。操作索引只能帮助定位，不能绕过当前权限、tombstone 或项目身份不复用规则。

未发送前可正常修改或取消；发送后确切输入冻结为该逻辑操作的依据。明确验证拒绝且无其他同键在途时，可以修正后明确发起新操作；结果仍未知时先核查，不能静默改修订、仓库、会话或分析来源重试。页面状态和 UI-01—12 验收见[页面专题](conversation-issue-separation-design.md)。

## 6. 统一错误响应与权限

```json
{
  "error": {
    "code": "CREATION_CONTEXT_CHANGED",
    "message": "创建条件已变化，请刷新后核对。",
    "fieldErrors": [],
    "requestId": "req_91"
  }
}
```

`requestId` 标识这次 HTTP 请求，供排查，不能代替持久 `creationId`。字段错误示例为 `{"field":"title","code":"REQUIRED"}`。错误正文和允许披露的限制均按当前权限裁剪，不回显无权仓库名称或密钥。响应使用 `Cache-Control: no-store`。

| HTTP | `error.code` | 处理 |
| --- | --- | --- |
| 400 | `INVALID_JSON` / `INVALID_IDEMPOTENCY_KEY` | 修正请求格式；不可把未发送的错误当创建成功。 |
| 401 | `AUTHENTICATION_REQUIRED` | 恢复登录后仍按原操作核查；登录流程未在本稿实现。 |
| 403 | `CREATE_NOT_ALLOWED` | 项目可见但当前无创建权限；不提示用户换另一身份绕过。 |
| 404 | `RESOURCE_NOT_FOUND` / `CREATION_NOT_FOUND` | 项目／事项不可见时隐藏存在性；创建查询的特殊语义见 §5。 |
| 409 | `CREATION_CONTEXT_CHANGED` | 回读条件并展示变化；不自动替换所选仓库。若修改输入，先确认旧操作结果，再作为明确新操作。 |
| 409 | `IDEMPOTENCY_CONFLICT` | 保留原键所指操作；不能静默替换请求内容。 |
| 409 | `CREATION_REQUIREMENTS_UNMET` | 已知必要配置或本次范围的能力不满足；返回可披露原因，如 `MODEL_CONFIG_MISSING`、`APP_PERMISSION_MISSING`。 |
| 410 | `CREATION_RESULT_REMOVED` | 原创建已发生但结果正文已删除，禁止重建。 |
| 413 | `REQUEST_TOO_LARGE` | 请求体超过 256 KiB，保留本地输入。 |
| 409 | `CONVERSATION_UNAVAILABLE` | 所选会话当前不可关联，不能静默改选。 |
| 409 | `ANALYSIS_NOT_APPLICABLE` | 所附分析未完成或不适用于当前输入；核查原创建结果后重新分析或明确移除来源。 |
| 410 | `ANALYSIS_RESULT_REMOVED` | 新操作引用的分析正文已清理；不影响已提交创建的幂等重放。 |
| 422 | `VALIDATION_FAILED` | 字段缺失、非法长度、重复仓库 ID、未知字段等；保留用户输入。 |
| 429 | `RATE_LIMITED` | 遵循实际返回的 `Retry-After`；不靠重试增加操作数。 |
| 503 | `AUTHORIZATION_UNCONFIRMED` / `RESULT_UNCONFIRMED` | 外部权限无法确认或提交结果不确定；前者不伪写为无权，后者查原操作。统一保守核查同键状态。 |

项目范围外的仓库和不可读仓库统一给不泄露存在性的范围错误，不借 `fieldErrors` 枚举隐藏对象。当前身份、项目可读、创建权限和选中范围在服务端验证；页面的 `canSubmit` 只是展示。执行前仍核验有效计划、权限、预算和资源，不由创建回执授予永久许可。

RM-API-01 r3 的共用解析补充：格式错误、重复JSON属性名及非法Unicode编码均为400 INVALID_JSON；合法JSON的未知字段仍为422 VALIDATION_FAILED。拒绝解析器“最后值胜出”的歧义输入，成功回执、schemaVersion=1及原Issue错误码保持。

认证由 RepoMesh Web 的可信上下文提供；全站 Cookie／令牌接入仍需绑定登录设计，属于后续技术细化，不再等待逐项批准。本稿不让客户端自报 `userId`／`role`。实施前必须绑定既有登录设计；若采用 Cookie，写操作需要相应 CSRF／Origin 校验；SSE 同样鉴权且不把长期令牌放入 URL。

## 7. 详情与房间查询

`GET /api/issues/{issueId}` 最小创建后快照示例：

```json
{
  "id": "iss_103",
  "number": 103,
  "projectId": "prj_orders",
  "revision": "issrev_1",
  "title": "增加订单归档",
  "description": "归档历史订单，保持原有读取权限。",
  "repositoryIds": ["repo_service"],
  "acceptanceCriteria": ["无读取权限的用户无法访问归档订单"],
  "mainChangeSetId": "cs_103",
  "source": {"kind": "issue_page", "conversationId": "conv_24"},
  "createdAt": "2026-09-09T13:00:03Z"
}
```

`revision` 只表示 Issue 业务快照修订，不能代替房间观察、配置修订、消息游标或 Plan Version。本稿不完整定义执行／验证／交付聚合枚举；后续字段须按各自证据扩展，不用创建状态填充“执行中”。`source.conversationId` 是此次创建来源，不宣称反向多主会话能力。

`GET /api/issues/{issueId}/rooms` 无实际主房间时的示例：

```json
{
  "issueId": "iss_103",
  "main": {
    "conversationId": "conv_24",
    "availability": "unavailable",
    "reason": "NOT_ASSOCIATED",
    "roomId": null,
    "canEnter": false,
    "observedAt": "2026-09-09T13:00:03Z"
  },
  "leaders": []
}
```

- `availability` 枚举 `ready / preparing / unavailable / unknown`；只有有实际准备依据才返回 `preparing`，新会话存在本身不构成依据。
- `reason` 包括 `NOT_ASSOCIATED / NOT_READY / ACCESS_RESTRICTED / OBSERVATION_STALE`，正常就绪时为 `null`；允许披露的粒度按读权裁剪。
- `roomId` 为 RepoMesh 的稳定内部关联标识，不是上游带凭据地址；无权时不提供受限房间内容或可利用的访问入口。
- `canEnter=true` 要求当前读权及实际关联、就绪依据全部满足。点击后房间读取仍重新核权；旧快照不能继续授权。
- `leaders` 返回实际允许披露的关联条目，字段为 `repositoryIssueId`、`repositoryId` 以及与主房间相同的运行观察字段；另外固定 `readOnly:true`。没有关联时可为空，不按选中仓库自动伪造“一仓一个事项／房间”。仓库事项粒度仍需独立建模，按持续授权继续设计。
- 业务会话历史可读性与实际房间可进入性分开；侧栏可按既有会话查询显示已保存历史，不能因 room 未就绪就把业务会话删除。

## 8. SSE：通知快照失效，读取仍以查询为准

**P3 已采用：** 详情采用单个 Issue 级订阅，通知 Issue 资料或房间观察变化。路径见 §1，媒体类型 text/event-stream。范围不覆盖项目列表或会话消息正文。

```text
id: evt_cursor_104
event: issue.changed
data: {"issueId":"iss_103","changed":["rooms"]}

```

`changed` 允许值为 `issue`、`rooms`，可以同时出现；事件不宣称 Room Ready，也不带模型内容。收到后重新读取对应快照，权限与状态以查询为准。重复通知允许合并，不承诺 exactly-once。

连接注册完成后服务端先发 `resync_required`（数据仅含 issueId），客户端重新查询两类快照，覆盖先查后订阅间隙。通知事件与其对应的 RepoMesh 事实变更同事务持久保存，提交后发布；外部运行变化先形成有证据的本地观察，再产生通知。查询期间通知按下述在途合并规则补查，不用一次内存广播承诺无遗漏。

重连携带不透明 `Last-Event-ID`；游标只属于当前订阅范围。服务端能重放时继续，游标过期或不可用时发 `resync_required` 并要求重查，不假装已连续补齐。默认事件保留 24 小时、每 15 秒发送注释心跳；它们是可调整运行参数，游标过期始终走完整补查。示例编号不规定排序算法。

前端对每个快照最多保留一个在途查询；查询期间再收到通知则置“需再查询”，结束后再取一次。切换 Issue 后丢弃旧目标的晚到结果，重新连接失败时保留“连接中断／状态可能过期”的可见提示。断连不把 Issue 标为失败，也不自动重新发送创建请求。

订阅建立、重放和后续发送均遵循当前权限；无权时关闭流并停止泄露。客户端发现读取被拒绝时清除对应敏感缓存。未获授权的连接返回普通 HTTP 401／404，不建立成功流；每次发送及心跳前检查本地访问资格，失效关闭连接；外部授权变化取决于核查结果，不承诺 15 秒内必然发现外部撤权。401／404 查询结果清除该对象缓存。

即使 SSE 延后，创建回执及 REST 查询／操作核查仍可独立工作。只在创建前不存在的会话上添加 SSE 不能替代此流程。

## 9. 采用状态及运行衔接

P1—P4、REST、SSE、默认新建／可选已有会话及双入口统一创建均按[持续授权清单](design-delegation.md)采用。原 DESIGN-004 保持 3／3；不再逐项问批准。未写出的其他模块按推荐继续设计，真实运行适配需验证。

页面创建提交成功后，持久待办驱动异步准备／恢复项目实例及此次关联主房间；已有准备工作与就绪资源复用，不按 Issue 复制整套团队。首次持久保存业务会话和首条消息也触发相同准备流程；项目创建、空会话或只读查看本身不启动。运行／建房调用在事务外，实际就绪后向 Manager 交接一次有稳定操作身份的建项工作，按真实结果核查重试。

创建记录是业务事件，不冒充用户消息；Manager 不再创建同一条 Issue。准备受阻保留已提交业务结果与待办，201 不保证运行就绪、接收、计划生效或派工。就绪后的投递／处理分别核验当前权限、配置与预算；Leader 房间随真实委派关联提供，不按创建仓库列表伪造。

## 10. 实施后的验收场景（当前未运行）

本轮骨架的构建、类型检查与进程探针检查不覆盖以下场景；没有执行它们，也不以历史 AgentTeams 的局部观察代替业务验收。

1. 可用项目无会话、无 runtime，提交一次后得到唯一 Issue／CS／新会话；无伪造聊天，运行状态不被写 Ready。
2. 同键同输入串行及并发重试只出现一套记录；同键不同输入冲突；两个明确新操作同标题仍独立。
3. 提交成功但响应丢失，查询原键得到原结果；查不到且旧请求仍在途时不换键，最终不重复。
4. 表单打开后范围或配置变化，返回冲突并保留输入；未选仓失效不阻断只涉及可用仓的请求；外部未知与无权区分。
5. 事务提交前故障无半套业务结果；提交后后台重启仍有待办。这是 P1 已采用的保障，Implementation 仍待实现并验收。
6. 失权重试／查询不泄露缓存结果，不把无权读当未创建；房间就绪观察过期时不提供可靠可进入承诺。
7. 选择已有会话不改旧名、不迁移消息、不改其他 Issue；关联失效不静默新建；重试不能改变关联选项。
8. 页面提交后的异步准备失败保留 Issue／CS／会话，重复待办不重复实例；实际 Ready 前不显示已接手，项目创建不触发准备。
9. SSE 丢失、重复、断线、旧游标及查询竞争均通过快照恢复；切换页面后旧响应不覆盖新 Issue，失权后停止内容泄露。

当前只校验文档结构、JSON 示例与状态／字段的一致性。没有真实接口、数据库、模型或 SSE 实现，以上不是已通过测试。


## 11. B06 分阶段实施说明候选（2026-09-13）

本轮只完成前置设计；既有建项字段和回执仍按 §2—6，不增加 initialConfigurationRevision、entry 或可信运行身份。新增内部事务约束见[持久化 §7](backend-first-batch-persistence.md#7-b06-创建事务收敛候选2026-09-13)，P9 待采用状态见[配置绑定 §8](issue-configuration-binding-design.md#8-b06-p9-约束细化候选2026-09-13)。

B06 后续实施限创建条件、已有会话候选、POST 页面创建和 GET 原创建结果四条路由；§7—8 的 Issue详情、rooms 和 SSE 属 B07 以后。创建回执保留既有链接，但 B06 页面先稳定展示提交回执与三种ID、保存恢复定位，不将未接详情404翻译为创建失败或伪造 ready 页面。四条路由未实现前不改变当前404事实。

仓库分析不在 B06 实施范围。options 诚实返回 repositoryAnalysis.availability=unavailable、reasonCodes=["INTEGRATION_NOT_AVAILABLE"]（此 reason 为本候选新增明确值）。仍识别并规范化已有可选 repositoryAnalysisId，不能把它当未知字段或静默丢弃。新操作附带该ID时，当前无可信分析来源存储可解析，按既有不可见来源404 RESOURCE_NOT_FOUND，不调用分析器、不写入；如权威读取本身未知则503。前端仅手动无来源创建。将来有分析来源数据时，必须完成 §3 的同项目/操作者、输入、成功终态及原子关联规则才能启用；不能沿用“缺模块直接忽略来源”。已提交原操作仍先重放，不重新核来源可用性。
