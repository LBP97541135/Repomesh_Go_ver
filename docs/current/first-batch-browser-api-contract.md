---
status: accepted
date: 2026-09-10
design_revision: RM-API-01-r3
implementation: not-started
documentation_updated: 2026-09-12
---

# 首批项目、配置引用与列表：浏览器接口契约 v1

2026-09-12 B02 采用补充。用户已回复“确认，继续”，采用范围以 [B02 采用记录](b02-authentication-adoption.md)为准。本文对应认证及必要秘密子集的“待采用”描述保留原提案历史，已由该记录替代；模型、预算、运行和其他未列明部分仍待采用。实现与验收进度见 [B02 记录](../development/2026-09-12-batch-02/README.md)。

本稿为 RM-API-01 r3（第 3／3 轮双方具体方案已确认）的唯一浏览器字段来源，由页面维护。r3 在 r2 上只补项目动作403错误码、非法幂等头和项目操作浏览器恢复路由；对象关系采用 RM-B01-04 r2。内部存储／事务由后端在[首批持久化协议](backend-first-batch-persistence.md)维护，不能复制第二套 HTTP Schema。创建 Issue 的七个接口仍只以[创建契约 v1](issue-page-create-api-contract.md)为准。技术确认不代表用户已接受具体页面布局；页面须展示原型讨论。

当前只完成本稿列明范围的设计。没有实际账号接入、数据库、业务路由或接口验证。登录跳转、完整仓库发现、全局模型 Key 管理、会话详情／消息／运行协议不因本稿存在而完成。原型与实现范围见[页面交接](HANDOFF-PAGE-API-DESIGN.md)。

## 采用范围与后续候选

2026-09-12 文档核对保留 RM-API-01 r3 的字段、路径和采用范围，澄清无实际变化的配置保存规则，并补齐后续设计入口。已采用的 F01—F04 具体 UI 范围以页面交接为准；新增候选不因本稿 accepted 而采用。本次发现与未补字段见[项目契约检查](../reviews/2026-09-12-project-contracts/README.md)。

| 衔接范围 | 唯一来源与当前边界 |
| --- | --- |
| 登录、回调、重连、注销和仓库发现批次 | [认证候选](authentication-browser-api-draft.md)已定义新增端点、Destination、尝试回执及发现策略，仍待采用；本稿 session、仓库条目和 coverage 字段保持。 |
| 模型保存、测试和仅换模型 | [模型浏览器候选](model-settings-browser-api-draft.md)维护新增协议；其专用应用精确保留 execution，不能用本稿完整 configuration PATCH 替代。 |
| 秘密、默认配置、执行与预算来源 | [配置来源候选](backend-first-batch-sources-draft.md)维护内部及部署方案；不能把导入结构直接当作浏览器响应。项目预算与时限的只读摘要尚缺，见 §5。 |
| 页面失败恢复与已有会话入口 | [恢复候选](first-batch-recovery-design.md)细化页面状态并提出只读会话页头；已有消息读取沿[消息契约](conversation-message-clarification-api-contract.md)，首批不因此开放发送。 |
| Issue 固定配置 | [P9 配置绑定候选](issue-configuration-binding-design.md)补 Issue 到 ProjectConfigRevision 的持久引用；接收供以后执行的真实 Issue／待办前须确定并落实，不改变本稿项目 JSON。 |


## 1. 共用规则和接口目录

认证来自服务端可信会话，不接受客户端 userId／owner／role 赋权。Cookie 同源写操作执行 Origin 与 CSRF 校验；查询和后续 SSE 同样核当前权限。JSON 响应 `Cache-Control: no-store`；不把凭据放入 URL、操作索引或日志。统一错误结构引用创建契约 `error={code,message,fieldErrors,requestId}`，requestId 不等于业务操作身份。模型测试提交的409 TEST_ALREADY_OUTSTANDING可按[模型字段稿§4](model-settings-browser-api-draft.md#4-单模型测试预览与费用确认)增加受限`error.details={existingTestId,links:{operation}}`；它只定位当前actor仍可读的阻断记录，不放入其他错误或项目接口。

UTF-8 JSON 请求体最大 256 KiB。格式错误、重复 JSON 属性名、非法 Unicode 编码返回 `400 INVALID_JSON`，合法 JSON 的未知字段／非法长度／重复集合成员返回 `422 VALIDATION_FAILED`；请求体过大 `413 REQUEST_TOO_LARGE`。文本长度按 Unicode 标量计数，保留实际提交内容，不根据相似标题归并。

| 方法与路径 | 调用者／用途 | 成功形状来源 |
| --- | --- | --- |
| `GET /api/session` | 当前登录会话 | §2；无有效会话401。 |
| `GET /api/repositories` | 登录用户，发现可参与仓库 | §3。 |
| `GET /api/projects` | 登录用户，可读项目外壳列表 | §4。 |
| `GET /api/projects/{projectId}` | 当前有项目资格的用户，首批owner | §4，最新项目快照。 |
| `GET /api/projects/{projectId}/repositories` | owner，查看已保存范围的可披露部分 | §3。 |
| `GET /api/configuration-profiles` | 登录用户，选择可引用配置 | §5。 |
| `POST /api/projects` | 登录用户，保存新项目 | §6，201首次／200重放。 |
| `GET /api/project-creations/{projectCreationId}` | 原操作者，核查原项目创建 | §6，200已提交。 |
| `PATCH /api/projects/{projectId}` | owner，编辑资料／引用或明确加仓 | §7，200首次／重放。 |
| `GET /api/projects/{projectId}/updates/{updateId}` | 原操作者，核查原项目更新 | §7，200已提交。 |
| `GET /api/projects/{projectId}/issues` | 有项目访问资格且对象内容范围可读 | §8。 |
| `GET /api/projects/{projectId}/conversations` | 同上，独立业务会话列表 | §8。 |

没有本稿之外的通用写权限。业务查询不创建项目／会话／Issue、不触发准备；项目保存和配置更新也不启动运行。首批没有项目／会话列表 SSE，不扩充 Issue SSE 冒充这些协议。

## 2. 登录会话读取

`GET /api/session` 返回：

```json
{
  "user": {"id": "usr_1", "displayName": "示例用户"},
  "githubConnection": {"status": "connected", "observedAt": "2026-09-10T00:00:00Z"},
  "csrfToken": "session-bound-example"
}
```

`user.id` 为稳定本地主体，displayName 仅展示。githubConnection.status 为 `connected / missing / unknown`，observedAt 为 UTC 时间或 null；connected 不保证任何仓库的当前资格或 App 能力。csrfToken 是当前会话的防伪值，不能持久化到 URL／操作恢复索引，也不替代身份 Cookie。

没有有效登录返回 `401 AUTHENTICATION_REQUIRED`，页面清身份敏感缓存和订阅并进入登录流程。真实登录／回调／注销／账号重连已有[认证字段候选](authentication-browser-api-draft.md)，仍待采用与实现，不能根据这个 session 示例自行拼接 OAuth 地址或索要扩大授权。

## 3. 仓库候选与保存范围

### 3.1 共用能力观察

本稿 `Capability` 形状如下，实际外部资格不是永久授权：

```json
{"status":"allowed","reasonCodes":[],"observedAt":"2026-09-10T00:00:00Z"}
```

status 为 `allowed / denied / unknown`；reasonCodes 为允许披露的原因字符串数组；observedAt 为观察 UTC 时间，没有依据为 null。userParticipation 表示用户参与／读取资格，appCapability 表示首批工作必要 App 能力观察；缺少 App 权限不等于用户无权看仓库。具体执行动作仍单独核验。

仓库身份与名称仅在 userParticipation=allowed 且观察有效时返回。denied 或 unknown 都不能借缓存返回旧名称或身份；不能把 unknown 标成 denied。已有用户读权、但 App 检查失败时可返回该仓库及 appCapability=unknown。

### 3.2 账号仓库候选

`GET /api/repositories?q=order&cursor=...&limit=50`：

```json
{
  "items": [
    {
      "id": "repo_orders",
      "displayName": "example/orders",
      "userParticipation": {"status":"allowed","reasonCodes":[],"observedAt":"2026-09-10T00:00:00Z"},
      "appCapability": {"status":"denied","reasonCodes":["APP_PERMISSION_MISSING"],"observedAt":"2026-09-10T00:00:00Z"}
    }
  ],
  "nextCursor": null,
  "coverage": {"status":"partial","reasonCodes":["APP_INSTALLATION_SCOPE"],"observedAt":"2026-09-10T00:00:00Z"}
}
```

coverage.status 为 `complete / partial / unknown`，描述发现覆盖范围，不是某个仓库权限。仅依 App 安装范围发现时必须标记 partial／APP_INSTALLATION_SCOPE，不能声称已经取得全部可参与仓库。完整账号发现接入仍需解决，见后端专项的认证事实；本稿没有降低既有 J1 目标。

nextCursor=null 只说明当前发现结果遍历结束；只有 coverage=complete 且末页才可表达当前范围已全部加载。partial／unknown 时允许用户明确选择已核实的子集保存，不提供“全部参与仓库”全选承诺。没有可提供的可靠结果而外部核查失败时返回 503，不以空数组冒充完整成功。页面翻页只改变可见列表，已选 ID 集合不会自动改变。

### 3.3 已保存项目仓库

`GET /api/projects/{projectId}/repositories?cursor=...&limit=50` 返回 `items`（同 §3.2 仓库条目）、`nextCursor`、`projectRevision`、`restrictedRepositoryCount`。这里没有第二个可写 repositoryIds 替换集合。

restrictedRepositoryCount 是 owner 本地已保存范围中本次未披露的数量，包含确认无权和核查未知；不能显示为确定失权数量。隐藏仓库 ID／名称不返回。范围仍完整保留，不能用 items 重新构造项目范围提交。

游标额外绑定 projectRevision；范围／资料修订变化导致旧页无效时返回 `409 PROJECT_CONTEXT_CHANGED`，从第一页重新读取；不据此删掉手动选择。每页重新核权，不承诺外部权限跨页一致。

## 4. 项目列表与详情

`GET /api/projects?q=订单&cursor=...&limit=50` 返回 `{"items":[{"id":"prj_orders","name":"订单系统","projectRevision":"prjrev_1","createdAt":"2026-09-10T00:00:00Z"}],"nextCursor":null}`。首批仅返回当前 owner 可读项目外壳，不把同仓协作者加入项目。

`GET /api/projects/{projectId}` 返回以下全部字段：

| 字段 | 类型／含义 |
| --- | --- |
| id、name、purpose | 项目稳定 ID、名称、用途。 |
| projectRevision | 不透明字符串；覆盖本地资料、明确范围和配置引用，等值比较，不作数字排序。 |
| createdAt | 项目创建的 UTC 时间。 |
| configuration | §5 的完整只读配置快照。 |
| actions | `{canEdit:boolean,canCreateIssue:boolean}`。canEdit 仅当前配置资格；canCreateIssue 表示通用门槛满足且存在可用范围，不保证任意仓库组合。 |
| creationReadiness | `{status:"ready"或"restricted"或"unknown",reasonCodes:string[],observedAt:UTC或null}`，仅管理面创建条件。 |

`creationReadiness.ready` 不表示 AgentTeams、模型调用、主房间或 Worker Ready。缺少已知必要配置显示 restricted，必要条件无法确认显示 unknown；部分仓库受限但存在可用选择时不能整体禁止创建，最终条件以 Issue 创建 options 和实际提交校验为准。项目外壳可读不表示所有 Issue／会话历史可读。

## 5. 配置引用与固定版本

配置写入只允许 modelProfile、executionProfile 两项引用；两者均为 `{"mode":"inherit"}` 或 `{"mode":"reference","id":"profile_id"}`。inherit 禁止 id，reference 必须 id；不接受 null、Key、provider 地址、预算数值或任意运行命令。项目采用已有配置，不把模型密钥复制进项目接口。

读取完整 configuration：

```json
{
  "modelProfile": {"mode":"inherit"},
  "executionProfile": {"mode":"reference","id":"exec_default"},
  "effective": {
    "configurationRevision":"cfgrev_1",
    "modelProfileId":null,
    "executionProfileId":"exec_default",
    "workerConcurrency":1,
    "budgetPolicyId":"budget_default",
    "timeLimitPolicyId":"time_default",
    "verificationGroupEnabled":false
  },
  "checks":{"status":"denied","reasonCodes":["MODEL_CONFIG_MISSING"],"observedAt":"2026-09-10T00:00:00Z"}
}
```

effective.configurationRevision 为不透明字符串，其余 effective 引用字段可为 null；并发 number／验证开关 boolean 在不能解析时也为 null。null 不等于无限预算、0额度、关闭校验或可执行。Worker 既有默认并发为1，但未能解析的实际配置不冒填默认值。checks 按 Capability 表达引用可读／启用、密钥版本存在及必要参数；检查通过不证明真实模型调用或运行接入成功。

`GET /api/configuration-profiles?kind=model&cursor=...&limit=50`，kind 必填为 `model / execution`，返回 `{"items":[{"id":"profile_1","name":"项目可用配置","availability":{"status":"allowed","reasonCodes":[],"observedAt":"2026-09-10T00:00:00Z"}}],"nextCursor":null,"defaultProfileId":"profile_1"}`。defaultProfileId 可以为 null；只返回当前用户可引用配置，某个已知配置当前不可用仍可作为待修复引用保存，不等于允许执行。

有效 profile 与密钥版本由 configurationRevision 固定。后台发现撤销、禁用或固定版本不可用，只更新检查观察及相关创建上下文修订，不静默切换 profile／密钥版本。在本稿项目更新接口中，只有显式 PATCH 提供 configuration（即使仍选同一个 inherit／reference）才重新解析两项引用。配置选择、固定引用版本或有效值实际变化时，生成对应 configurationRevision 并更新 projectRevision／creationContextRevision；完全无变化时保留原修订，仍保存本次成功操作回执。只改名称、用途或加仓不顺带更新模型；平台默认变化只可提示更新，原固定版本仍有效时可继续，失效则保持受限。这与 §7 及[持久化 §2.4](backend-first-batch-persistence.md)的无变更保存规则一致。

配置引用版本固定的收益是可追溯费用／权限依据；代价是默认变化需要显式保存配置才能采用。保存不是新的业务开工审批，也不触发实例重启。Key 保存、替换与原操作安全终结见[模型浏览器候选](model-settings-browser-api-draft.md)，尚未采用或实现；删除等未覆盖操作不从该候选推导。

本节响应尚不足以展示[项目配置 J1](project-configuration-design.md)要求的实际预算和时限。budgetPolicyId／timeLimitPolicyId 只标识策略，配置候选列表也未返回其数值、单位或固定版本内容。须补对应 configurationRevision 的只读摘要契约，并区分固定配置与当前额度／资格观察；内部来源清单和模型测试预览不能代替它。此项记为[检查 C05](../reviews/2026-09-12-project-contracts/README.md#c05-项目预算和时限缺少只读响应)，当前不新增已采用字段、不填入候选默认数值，也不要求先实现完整 F06 编辑页。

## 6. 保存项目与原操作恢复

```http
POST /api/projects
Content-Type: application/json
Idempotency-Key: b3e58aa5-7dc4-4c95-97bc-4bead37d13bc
```

```json
{
  "name":"订单系统",
  "purpose":"协调订单服务与界面的持续维护。",
  "repositoryIds":["repo_orders"],
  "configuration":{
    "modelProfile":{"mode":"inherit"},
    "executionProfile":{"mode":"inherit"}
  }
}
```

name 必填、非空白、最多200；purpose 必填、非空白、最多20000；repositoryIds 非空、去重，最多100个稳定身份。首批项目总范围上限100，作为本轮可调整的明确规模参数。configuration 可省略，等价两个 inherit；提供时必须包含完整两项引用。用户／owner、权限、分支、startRuntime 等字段一律不接受。

新建时完整所选用户读权必须有效核实；任何未知返回 `503 AUTHORIZATION_UNCONFIRMED`，没有部分落项目；明确无权按可披露403或隐藏404。App／模型／环境缺失或未知可以保存完整待配置项目。配置引用不存在或不可引用404；引用存在但暂不可用可受限保存。名称相同不合并项目。

认证后，先核原操作，再走新建校验。唯一作用域为 `(actor, project_create, projectCreationId)`，UUID 键即 projectCreationId。集合排序用于比较，重复先拒绝；配置省略按两项 inherit 规范化；文本保留原样。项目、owner、范围、配置引用／固定版本、输入快照和操作结果同事务；不产出会话、Issue 或运行准备待办。

首次成功201，同键同输入重放200：

```json
{
  "projectCreationId":"b3e58aa5-7dc4-4c95-97bc-4bead37d13bc",
  "status":"committed",
  "projectId":"prj_orders",
  "projectRevision":"prjrev_1",
  "createdAt":"2026-09-10T00:00:00Z",
  "links":{
    "project":"/api/projects/prj_orders",
    "operation":"/api/project-creations/b3e58aa5-7dc4-4c95-97bc-4bead37d13bc"
  }
}
```

`GET /api/project-creations/{projectCreationId}` 返回同形200。回执 projectRevision 和 createdAt 固定为原提交值；后续配置变化不改重放内容，最新状态另 GET project。查询只接受原主体作用域，按当前项目访问资格返回，不以旧回执绕过失权。

同键不同输入409 IDEMPOTENCY_CONFLICT；暂未有可见提交404 PROJECT_CREATION_NOT_FOUND；正文已清理410 PROJECT_CREATION_RESULT_REMOVED。404不是“原请求永不提交”，不得换键自动再建；5xx、断网、丢响应先查询。键在账号保留期内不复用，项目正文删除保留最小占位并禁止旧键复活，旧项目 ID 不复用。账号／项目删除及运维清理仍须保持此保障，不因缓存清理让旧操作成为新操作。

浏览器恢复页为 `/project-creations/{projectCreationId}`，只查询本节操作API；创建前不要求尚不存在的 projectId。sessionStorage 按 `(actor, operationKind, key, 可选projectId)` 保留原键与确切非秘密输入，localStorage 只保存主体隔离的最小索引；输入丢失只查询，不按项目名称猜测恢复。未发请求可以取消，已发请求取消等待不回滚。存储不可用时发出前提供由上述浏览器路由生成的可复制恢复链接；响应 links.operation 始终是API地址，两者不能混称。注销／失权清敏感数据规则仍沿创建契约 §5.1。

## 7. 更新资料、引用与明确加仓

```http
PATCH /api/projects/prj_orders
Content-Type: application/json
Idempotency-Key: 30658f09-049a-4e8e-b44e-c7d0dfe78f5b
```

```json
{
  "expectedProjectRevision":"prjrev_1",
  "name":"订单系统维护",
  "repositoryIdsToAdd":["repo_web"]
}
```

expectedProjectRevision 必填；name、purpose、repositoryIdsToAdd、configuration 可选，至少一项明确更新字段。名称／用途限制同创建；提供的添加集合必须去重，最多100项且最终总范围不超100，超限422。既有 ID 可出现在添加集合中作为集合幂等，输入比较仍按原提交集合排序保存；不提供完整仓库替换或删除字段。

configuration 提供时是完整两引用并显式重新解析固定版本；未提供保持原值，不接受 null。元数据／配置修复不要求对原失权仓库再证明读权；真正新增范围必须核实有效用户读权，任何添加失败整次更新不提交。已保存范围完整保留，不能从返回 items 的缺席推断删除。

唯一作用域为 `(actor, project, project_update, updateId)`，UUID 键即 updateId。预期修订属于规范化输入，字段省略表示保留，不能与显式配置更新混为一谈。命中原已提交操作时先按当前权限重放；新写旧 projectRevision 返回 `409 PROJECT_REVISION_CONFLICT`，保留用户草稿并显示重新读取的版本，不能自动覆盖。无实际变化可以成功，不强制增加 projectRevision；若重新解析配置确有变化则必须升修订。

200首次／重放：

```json
{
  "updateId":"30658f09-049a-4e8e-b44e-c7d0dfe78f5b",
  "status":"committed",
  "projectId":"prj_orders",
  "projectRevision":"prjrev_2",
  "updatedAt":"2026-09-10T00:01:00Z",
  "links":{
    "project":"/api/projects/prj_orders",
    "operation":"/api/projects/prj_orders/updates/30658f09-049a-4e8e-b44e-c7d0dfe78f5b"
  }
}
```

`GET /api/projects/{projectId}/updates/{updateId}` 返回同形200；回执 projectRevision／updatedAt 始终是该次提交的不可变结果，不回填后续新状态。原键不同输入409；暂未可见404 PROJECT_UPDATE_NOT_FOUND 保持未知；已清理410 PROJECT_UPDATE_RESULT_REMOVED，不比较新正文或重执行。项目保留期内操作身份不复用。

资料、明确加仓、配置／修订、输入快照及回执同事务；提交失败不落一半。更新不触发准备，不自动改变在途 Issue 的范围、计划和固定组合。恢复纪律与 §6 相同，重放优先于新修订校验，权限始终当前有效。

浏览器恢复页为 `/projects/{projectId}/updates/{updateId}`，只查询本节操作API。本地恢复记录按 `(actor, operationKind=project_update, key, projectId)` 隔离，与项目创建及Issue创建分开；确切输入丢失只查询。复制给用户的是浏览器恢复链接，不是 response.links.operation API地址。

## 8. Issue／会话列表与统一分页

`GET /api/projects/{projectId}/issues?q=归档&repositoryId=repo_orders&cursor=...&limit=50`：

```json
{
  "items":[{
    "id":"iss_103","number":103,"title":"增加订单归档",
    "repositoryIds":["repo_orders"],"mainChangeSetId":"cs_103",
    "source":{"kind":"issue_page","conversationId":"conv_24"},
    "createdAt":"2026-09-10T00:02:00Z","revision":"issrev_1"
  }],
  "nextCursor":null
}
```

字段含义复用原创建契约；source.kind 沿 issue_page／manager_mcp 真实来源，不能把尚未接入 Manager 的示例当实测。正文不在列表中返回。首批不引入完整执行生命周期枚举或状态筛选；业务记录已建立与 rooms 实际观察分别展示，不能把“已创建”放进执行状态替代“执行中／完成”。

`GET /api/projects/{projectId}/conversations?q=订单&cursor=...&limit=50` 返回 `{"items":[{"id":"conv_24","title":"订单讨论","createdAt":"2026-09-10T00:02:00Z","revision":"convrev_1"}],"nextCursor":null}`。只定义业务会话列表，不定义会话正文、消息发送或订阅。

两类列表仅返回完整内容范围可读的对象，不回传隐藏名称或受限对象计数。已确认 denied 可以按权限排除；核查候选内容范围时遇 unknown 返回 `503 AUTHORIZATION_UNCONFIRMED`，不能省略后返回看似完整成功。来源 conversationId 只是一条身份引用，不保证该会话当前可读；进入仍重新核权。repositoryId 过滤跨项目／当前不可读404，核查未知503，不能用于探测隐藏范围。

| 分页／搜索参数 | 统一语义 |
| --- | --- |
| limit | 默认50，允许1..100整数；非法422。 |
| cursor | 不透明且绑定主体、资源范围、查询、筛选、排序；不是授权。 |
| q | 在目录中允许 q 的端点使用，最多200个 Unicode 标量，字面大小写不敏感子串匹配名称／标题；空值等价不筛选，不解析 SQL 通配符／正则，不搜消息正文。 |
| 排序 | 首批固定稳定 ID 升序，不支持其他排序；列表无总数承诺。 |
| nextCursor | 最后一段扫描位置；null 才表示当前遍历末尾。允许 items=[] 且 nextCursor 非空，不得提前结束。 |

游标非法／参数不匹配400 INVALID_CURSOR，过期409 CURSOR_EXPIRED；显式从第一页刷新，不能默默重置游标并拼接旧页。项目仓库列表另有 §3 的 projectRevision 冲突；创建候选原契约的 creationContextRevision 规则不被本节覆盖。每页重新核当前权限，跨页不持有数据库快照，新增较早 ID 或查询字段变化需要刷新。

列表首次进入、用户明确刷新、写成功后、窗口重新获得焦点时重新查询；并发刷新合并且应用页面请求代次规则。失败显示读取失败／允许保留的快照可能过期，不宣称实时；权限拒绝清相应敏感缓存。Issue 详情按其原 SSE 恢复，不把列表刷新产生的网络行为当命令。

## 9. 错误、恢复及验收边界

| 状态／代码 | 页面行为 |
| --- | --- |
| 401 AUTHENTICATION_REQUIRED | 恢复登录前清身份敏感缓存／订阅；只由原主体核查原操作。 |
| 403 PROJECT_CREATE_NOT_ALLOWED | 当前登录主体无新建项目资格；不创建，也不改用其他身份绕过。 |
| 403 PROJECT_UPDATE_NOT_ALLOWED | 项目可见但当前无编辑资格；保留仍可读取的外壳，不泄露受限内容。 |
| 404 RESOURCE_NOT_FOUND | 项目或敏感仓库范围不可披露；不从错误暴露隐藏身份。 |
| 400 INVALID_IDEMPOTENCY_KEY | 缺失或非法幂等头；不把格式拒绝当成功，不改Issue原有错误码。 |
| 409 IDEMPOTENCY_CONFLICT | 保留原操作，不改绑输入；新工作需显式独立操作。 |
| 409 PROJECT_REVISION_CONFLICT／PROJECT_CONTEXT_CHANGED | 回读当前修订，保留有权输入，明确核对再提交／重新分页。 |
| 400 INVALID_CURSOR／409 CURSOR_EXPIRED | 从第一页重新查询，不混合新旧页，不改手选集合。 |
| 404 PROJECT_CREATION_NOT_FOUND／PROJECT_UPDATE_NOT_FOUND | 当前没有可见提交，保持未知；不等于可换键。 |
| 410 PROJECT_CREATION_RESULT_REMOVED／PROJECT_UPDATE_RESULT_REMOVED | 原操作结果已清理，不返回正文，不重新执行。 |
| 429 RATE_LIMITED | 按实际 Retry-After 退避，不增加逻辑操作数。 |
| 503 AUTHORIZATION_UNCONFIRMED／RESULT_UNCONFIRMED | 分别展示权限核查未知／提交结果未知；写操作均先核查原键，不声称失败或已创建。 |

上述项目403业务码不改变Issue原 `CREATE_NOT_ALLOWED`。不能通过错误或 fieldErrors 回显受限仓库名、秘密或请求正文。没有运行中／失败的公开项目操作状态，也不采用202冒充本地保存成功。

实施后至少验证：无变更配置保存保留修订并保存原操作回执；项目同键并发与丢响应；更新旧修订与同键重放优先；正文清理后旧键不复活；受限原仓库不影响配置修复；明确添加失败整次回滚；默认配置变化不暗换版本；候选部分覆盖与空页游标；列表核权未知返回503；跨主体游标／过滤不能泄露；失权和晚到响应不能覆盖新页面。后端故障矩阵和页面 UI-01—12 分别见各自专题。

本稿文档／JSON 检查不等于接口实测。首批项目／列表字段已收口；完整认证、可参与仓库覆盖、模型 Key 设置、会话详情／消息／房间与运行恢复仍是明确设计依赖，P01/B01、P05 和整个产品设计不能据本稿一并标为完成。

## 10. C05 固定预算时限摘要候选

状态 `C05-r1 / PROPOSED_NOT_ADOPTED`，2026-09-13。本节补 §5 的字段缺口，原 RM-API-01 r3 已采用字段不变。本节按 S05 的有限 request 预算推荐设计；若选择金额方案，本节 budget 分支须重新设计，不把次数改名为金额。实现与验收均未开始。

推荐在 `GET /api/projects/{projectId}` 的 configuration 内新增 `fixedSummary` 与 `quotaObservation`，不新增策略查询端点。二者与原 `effective.configurationRevision` 指向同一配置；固定摘要从不可变记录读取，额度独立核当前窗口。只读 GET 不创建窗口、消费额度或重解析默认。

`fixedSummary` 必含 configurationRevision、executionVersionId（string或null）、budget、timeLimits。两个政策字段各自使用以下分支：

| 字段／分支 | 完整形状 |
| --- | --- |
| budget，已完整固定 | `{status:"available",policyId:string,policyVersion:string,scope:"project_model_runtime",unit:"request",period:"utc_day",limit:integer,maxUnresolved:integer}` |
| budget，未解析或缺历史材料 | `{status:"unresolved",reasonCodes:string[]}`，无策略 ID 或数值 |
| timeLimits，已完整固定 | `{status:"available",policyId:string,policyVersion:string,modelRequestTimeoutSeconds:integer,workerAttemptLimitSeconds:integer}` |
| timeLimits，未解析或缺历史材料 | `{status:"unresolved",reasonCodes:string[]}`，无数值 |

available 表示原固定参数可读取，不表示当前已启用/额度充足/进程停止。政策已禁用但历史材料仍可读时仍展示原固定值，由 configuration.checks 表达当前限制。unresolved 不能填0、无限或推荐默认。原配置未解析 execution 时 executionVersionId=null；有可信版本但材料不完整时保留其确切版本和 unresolved 分支。

`quotaObservation` 只针对上述固定运行政策，完整分支为：

- `{status:"known",configurationRevision,policyId,policyVersion,unit:"request",windowStart,windowEnd,effectiveLimit,reserved,consumed,remaining,observedAt}`。UTC 时间；非负整数，remaining=max(0,effectiveLimit-reserved-consumed)。effectiveLimit 是原政策限额与该窗口保守下调限额的较小值，可能小于 fixedSummary.budget.limit。
- `{status:"unknown",configurationRevision,reasonCodes,observedAt}`。observedAt 为本次核查 UTC 时间或null；没有可靠账本／读取失败不得伪造0。
- `{status:"not_configured",configurationRevision,reasonCodes}`。原固定配置没有可解析运行预算；无额度数值。

本地项目 owner 可读上述本项目摘要，不要求所有原仓恢复读权。策略来源身份不可披露时不返回相关政策名或数值；当前 owner 资格无法确认仍整次503 AUTHORIZATION_UNCONFIRMED。本批 owner 私有执行引用的政策没有额外共享 ACL；若以后增加策略 ACL，需要新增受限分支，不用 unresolved 隐藏已知无权。普通账本故障可返回 quotaObservation.unknown，但项目主体/配置快照无法可靠读取时仍返回503，不拼旧快照。

响应中的固定摘要与 effective 必须来自同一配置版本；额度观察允许稍后变化，不承诺跨网络最新或已为 Issue 预留。默认改变后旧项目仍显示旧版本数值，模型专用应用保持原 execution 数值。契约设计覆盖[原检查 C05](../reviews/2026-09-12-project-contracts/README.md#c05-项目预算和时限缺少只读响应)，采用与真实验收尚待后续完成。

C05的project_model_runtime窗口不代替actor_model_test窗口，也不表示模型测试可提交。模型测试预览在[模型字段稿§4](model-settings-browser-api-draft.md#4-单模型测试预览与费用确认)同次观察当前actor的outstanding和测试额度。已有未核清测试与额度观察是两个独立条件；浏览器不得从本节remaining推导测试canSubmit。
