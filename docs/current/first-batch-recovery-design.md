# 首批页面恢复总表 · 待审

页面独占；用户要求六项全部交付，本稿把首批现有契约落到页面状态、动作及恢复路径。本文不改原三类写操作和列表的JSON／错误／幂等协议。F02保持RM-UI-PROJECT-EDIT r1技术呈现范围；公共恢复在r1上作2026-09-12一致性修订RM-FIRST-RECOVERY r2候选，补认证返回会话和Key原保存安全终结的引用。新增设计仍待采用，旧契约已收口的三轮不重开。

2026-09-14 设计收口固定了前端恢复模块归属和模型测试预览的字段投影。该收口属于本轮五项修订授权，不代表采用全部 B05、B06 候选，也不采用 D05、D06 的候选数值。B05、B06 产品实现和运行验收仍未开始。

## R01 全站呈现规则

先有当前身份和目标读取资格，再显示目标名称及正文；加载时骨架无假数据；确认空列表显示“还没有…”与有权创建入口。初次读取网络失败给重读，不显示空成功；权限未知隐藏旧敏感正文，不把unknown当denied。已确定没有资格用通用“当前无法访问”，不暴露存在性。

每个页面上下文含actor、projectId、objectId、query/filter、requestGeneration。切换任何一项先增加代次、终止旧展示，晚到响应无条件丢弃；AbortController只节省请求，不被当服务器取消。每快照最多一个在途请求，其间失效通知合并一次补读。原请求是否提交只靠原操作核查，不靠取消网络或离开页面。

401清当前身份敏感草稿／派生缓存／选择器／订阅；403/404按错误语义及当前读权清理对应受限内容和派生视图；仅403 PROJECT_UPDATE_NOT_ALLOWED取消写资格时，保留已核可读外壳、禁写，不清仍有权的视图；权限unknown隐藏旧对象内容直至重新核实。保留最小操作定位索引不授予权限，未登录不展示其受限名称。重新登录需同一稳定actor才能查询原操作，换账号不继承草稿。

### R01.1 前端恢复模块归属

B05、B06 扩展现有 `web/src/modelRecovery.ts`。该模块共享操作定位、浏览器存储和读取代次检查，不新增第三套恢复框架。`provider_save`、`model_test`、`model_apply` 和 `issue_create` 各自保留结果解析器、业务 reducer、重试资格和页面状态。共用存储只返回已校验的原定位或非秘密输入，不根据一个通用状态决定重发。

现有 `SaveLocator` 迁移为 `OperationLocator` 的 `provider_save` 分支，并导出 `ProviderSaveLocator` 供保存页面使用。`OperationLocator` 的每个分支都含原 actor、operation kind 和原操作 ID。项目范围操作还含 `projectId`。读取票据同时比较完整 locator、会话 generation 和当前已认证 actor，任一项不同便丢弃响应。

现有 B04 存储键保持兼容。`localStorage` 继续使用 `repomesh.model.save-index.v1`，`sessionStorage` 继续使用 `repomesh.model.save.v1:{actor}:{id}`。后续 operation kind 通过同一存储帮助函数使用各自命名空间，不把正文放入一个跨业务公共值。失去认证、切换 actor 或收到对应受限结果时，客户端清除 `sessionStorage` 中的非秘密输入和当前页面内容，但保留 `localStorage` 中的最小索引。最小索引只含 actor、operation kind、原操作 ID、可选 `projectId` 和记录时间。读取索引必须按当前 actor 过滤并重新向服务端核权。

输入恢复遵守各业务契约。允许精确重试的业务只能复用已解析并冻结的原 body 和原操作 ID。共享模块不能从标题、当前表单、最新 revision 或最小索引重建输入。`provider_save` 发出后已清 Key，replace 保存只能查询或终结原 `saveId`。`model_test` 在可能提交后只查询原 `testId`，不能再次外发。`model_apply` 沿 F04 只核查原应用。`issue_create` 仅在创建契约允许时以原 `creationId` 和确切输入重试。缺少确切输入的操作只查询。

调用者迁移如下：

| 调用者 | 同一批迁移 |
| --- | --- |
| `ModelSavePage.tsx` | 将 `SaveLocator`、旧 `OperationState<T>` 和旧读取参数改为 `ProviderSaveLocator`、保存专用状态及完整 locator；继续使用共用 `beginRead`、`acceptsRead` 和保存 reducer。 |
| `ModelSettingsPage.tsx` | 继续调用共用索引和会话快照写入；`provider_save` 仍走现有 B04 键和恢复路径，不改变 Key 清理时点。 |
| `session.ts` | 用跨批 `clearRecoveryPayloads` 取代 `clearModelRecovery`；仍单独调用 B03 的 `clearAllOperationInputs`。两种清理都不删除最小索引。 |
| `model.test.mjs` | 保留 B04 键、actor 隔离、Key 不落盘和晚到代次测试，再覆盖新增 locator、按 kind 解析、各 reducer 隔离、失权清正文但留索引，以及业务允许时只按原 ID 和原输入重试。旧 `SaveLocator` 在这些调用者同时通过类型检查后删除。 |

`web/src/projectRecovery.ts` 继续拥有 B03 的 `project_create` 和 `project_update`。它保留 `OperationIdentity`、项目操作键规范化、输入解析与冻结、`prepareOperation`、`loadOperation`、项目恢复路由、项目 reducer，以及 `repomesh.project.operation*` 存储键。B05、B06 收口不搬迁这些职责，也不让模型恢复 reducer 解释项目输入。`projectRecovery.ts` 后续可以复用读取票据，但该选择不改变本次边界。

## R02 首批六包页面状态及动作

| 页面 | 加载／空／失败 | 确定受限／未知 | 可恢复动作与终点 |
| --- | --- | --- | --- |
| 登录与建项目 | 初次登录、授权取消、过期按已采用F01三项；选仓无可靠数据503不能空成功 | 仓库名称只有allowed，App不足单独提示，可保存受限项目 | 登录确认后显式继续；选仓→资料两步不改；查询恢复原projectCreationId |
| 主工作区项目／会话／Issue列表 | 骨架；空时显示创建入口；失败显示重新读取 | 会话/Issue列表候选完整内容读权unknown时整页503，不静默过滤成成功 | limit50、只按ID、下一页只依nextCursor；游标失效清旧页再从首屏读，手选不默默清 |
| 创建Issue弹窗 | 基本字段/关联控件/反馈沿已采用 | 条件变化、已有会话失效禁止自动替换；失权清对应内容 | 原creationId恢复；明确未提交后才修正为新操作；404仍未知、410不重建 |
| 最小Issue详情 | 记录保存与运行观察分开；无房间不编造准备中 | Issue本身受限隐藏内容；仅关联会话受限隐藏其名称/入口但保留有权Issue | 业务历史与房间Ready分开；Issue级SSE失效后回读；不加入DAG/交付操作 |
| 项目设置 | 资料→已存仓→配置；加载禁保存、只读摘要 | 原仓失权不删除，可修有权元数据/配置；新增一项未知整次增量保存被阻止 | revision冲突比对当前与草稿再明确提交；未知留updateId查询，不复用列表重构原范围 |
| 模型设置 | 保留原Key、显式替换入草稿；未保存禁测/用 | owner/密钥/地址/次数额度未知分别处理，配置保存不证明可调用 | 保存未知查原结果或显式安全终结原saveId；未取得确定回执不能新保存。测试/应用保持各自恢复限制，不重发收费调用或项目应用 |

仅列表必要分页纳首批，高级筛选／消息全局搜索不扩展。第一批会话以已有业务记录只读查看为主，不把历史Manager样例当真实发送链路完成。

### R02.1 模型测试预览占用与处理器观察

`TestPreview.reasonCodes` 新增 `TEST_ALREADY_OUTSTANDING`。当 `queued`、`running` 或 `recovery.open` 的 `unknown` 测试命中当前有效的提交阻断条件，而且当前 actor 仍可读取该测试时，预览返回 200、`canSubmit=false`，并同时返回顶层 `existingTestId` 与 `links.operation`。两字段只在该分支成对出现。页面打开原操作，不生成新 `testId`。其他 actor 的测试和当前 actor 已失权的测试都不能通过这两个字段披露。该字段不固定未核测试数量或候选默认值。

占用判断与当前额度窗口分开。占用记录读取失败时返回相应通用 503，不能当作没有占用。提交事务再次检查当前有效的未核测试提交阻断条件。预览后发生竞争时，提交仍返回 409 `TEST_ALREADY_OUTSTANDING`，不登记或发送本次新测试。

该 409 使用通用错误封装。当当前 actor 仍可读取阻断测试时，`error.details` 为 `{existingTestId,links:{operation}}`，页面据此打开原操作。失权、其他 actor 或不可披露分支省略 `details`。其他错误码不得返回该形状。Preview 顶层定位和 Submit 错误详情分别覆盖预览时已占用和提交竞争，不互相替代，也不引入新的错误框架。

当前没有满足协议与限制映射的有效处理器登记时，预览返回 200、`canSubmit=false` 和 `TEST_HANDLER_UNAVAILABLE`。读取处理器登记的观察失败时，整次预览返回 503 `TEST_HANDLER_UNCONFIRMED`，不返回可确认的预览正文。`RESULT_UNCONFIRMED` 只表示写入或提交结果未知，不能用于只读预览。以上字段只收口 B05 草图与核心候选的同名投影，不表示处理器、测试页面或真实模型请求已经实现。

## R03 三类已有写入的精确恢复映射

| 写入 | 成功／查询唯一来源 | 浏览器恢复页 | 404／410 |
| --- | --- | --- | --- |
| 项目创建 | [首批§6](first-batch-browser-api-contract.md) POST/projects、GET/project-creations/{id} | /project-creations/{id} | PROJECT_CREATION_NOT_FOUND仍未知；PROJECT_CREATION_RESULT_REMOVED不重建 |
| 项目更新 | [首批§7](first-batch-browser-api-contract.md) PATCH/projects/{projectId}、GET同project/updates/{id} | /projects/{projectId}/updates/{id} | PROJECT_UPDATE_NOT_FOUND仍未知；PROJECT_UPDATE_RESULT_REMOVED不重执行 |
| Issue创建 | [创建§4—5](issue-page-create-api-contract.md)原请求与查询 | /projects/{projectId}/issue-creations/{id} | CREATION_NOT_FOUND仍未知；CREATION_RESULT_REMOVED不重建 |

各自先生成UUID原键、冻结允许输入，sessionStorage按actor/operationKind/项目/key保存确切非秘密输入；localStorage最小索引不含标题、名称、正文和Key。存储不可用发出前展示可复制浏览器恢复链接。未发送可以取消；已发送“关闭/稍后处理”只停止等待。回读后仅原键原输入才可提供原请求重试。Key保存不重收原秘密重放，但可依[模型专用草案](model-settings-browser-api-draft.md)显式终结同一个saveId：committed显示原保存结果，rejected／closed_without_save才允许重新填写Key并明确新操作；终结响应未知仍查原键或重试同一终结，404不证明未保存。模型测试仍按更严只查询策略处理未知，不能借保存终结流程重新外发测试。其他原输入丢失的写入仅查询。

统一未知页主动作“查询原结果”，次动作“稍后处理”；复制链接只生成原操作地址。没有“重新创建”“换键重试”按钮。404后仍显示待核，经过任何时长不变失败。410说明“原结果已清理，不能重新执行”，提供返回列表核查现状，不提供原操作重建。

成功回执显示“本次已保存”，再GET当前资源。若当前修订与原回执不同，显示“记录后来有更新”，原回执不改；不得回填最新版本冒充原结果。

## R04 项目设置F02具体核对

1. 未勾选改配置：PATCH只含expectedProjectRevision及用户实际变更的name/purpose/repositoryIdsToAdd；不得补configuration以“方便同步”。无变更禁保存。
2. 勾选：同时提交完整modelProfile和executionProfile引用，明确提示保存时重解析两项。它不承诺锁定预览或只换模型；精确只换模型使用模型专用应用草案。
3. 已存仓只读；新增选择可跨分页，未知增量保留数量但隐藏ID/名；明确撤回草稿不删除已存范围。新增集合及项目最终范围上限100，重复输入422，超限整笔不写。
4. revision冲突先保留有权草稿，GET当前项目/仓库；按“当前值／我的修改”比较名称、用途、待增量和配置意图。用户选择回读后的修订再确认，形成新updateId；若原操作仍未知则不能进入该新写分支。
5. 403 PROJECT_UPDATE_NOT_ALLOWED显示只读可见外壳，禁写；404通用受限并清相应缓存。原仓权限不足不阻止合法元数据修复；App不足不冒充用户无读取资格。
6. 未知页提供原updateId核查，禁更改原输入、禁追加另次保存；已清理结果不通过“重新保存”复活。主界面设置入口继续保留。

## R05 创建与详情特定状态

options变更不自动把已有会话改成新会话、不默认勾新仓；显示受影响项并回读条件。canSubmit仅通用必要条件，不替代提交时本次范围/配置/额度/App核验。明确错误且无同键在途才允许修正为新工作；未知先查原键。

Issue详情GET与rooms GET独立加载/失败：业务成功、房间未知仍展示已保存记录；preparing必须实际观察，内部待办未接入不直接映射为公开枚举。ready分支仍沿现行合法值，显示“主房间已就绪”但首批不新增执行操作；canEnter只对原有可进入入口有效，点击重新核权。unavailable给实际可披露原因；unknown显示状态待确认；无观察时间不假填当前时间。

Issue SSE只通知issue/rooms失效；连接后resync_required查询，断连标可能过期，重连按原游标或全量回读；403/404清内容并停订阅。列表及会话没有借此新增SSE。

## R06 已有会话只读入口补齐

已有消息查询唯一字段源为[消息契约§3](conversation-message-clarification-api-contract.md)，不另定义消息JSON／分页或发送。本次新增候选`GET /api/projects/{projectId}/conversations/{conversationId}`作为只读页头来源，200：

```json
{"id":"conv_24","projectId":"prj_1","title":"订单讨论","revision":"convrev_1","createdAt":"2026-09-11T12:00:00Z"}
```

字段含义同现行会话列表；需整个会话内容范围当前读权，未知503 AUTHORIZATION_UNCONFIRMED，隐藏404 RESOURCE_NOT_FOUND，401先恢复登录。无roomReady字段、无模型输出/状态聚合/反向多主会话推断。页头失败不显示旧标题或通过已知conversationId授权；消息按原C/messages独立查询，完整内容核权仍由服务端负责。新页头不改变原五端点r3，是补充单一只读接口，技术题RM-FIRST-RECOVERY r1。

浏览器页为`/projects/{projectId}/conversations/{conversationId}`。401后按[认证草案A02](authentication-browser-api-draft.md)提交`{kind:"conversation",projectId,conversationId}`；登录确认后用户显式继续，重新核会话归属和整个内容范围，再分别读取页头／消息。跨项目、已失权或目标不存在使用相同通用返回工作区，不展示原会话标题；资格未知停在登录结果页核查，不自动降级成成功。r2补此返回衔接，首批仍只读。

消息提交恢复属于后续发送批次：Destination扩展为`{kind:"operation",operationKind:"conversation_message",projectId,conversationId,operationId}`，operationId为原submissionId，完整作用域、固定浏览器路由和首批拒绝规则唯一维护于认证草案A02。后续接通时需同一原actor、原项目／会话和当前完整内容读权，进入只核查原提交；认证成功不发送正文、不解除404未知或410禁止重建，不把只读首批扩成消息执行闭环。

## R07 原型展示与逐项验收矩阵

[首批评审入口](../prototypes/index.html#review)可打开六项；[状态演示](../prototypes/index.html#recovery-states)在同一深色框架演示列表、创建、设置、详情的关键状态。模拟控件独立标记，不读取浏览器业务存储或发真实API。

| 用例 | 原型/文档判定 | 实现后必须验证 |
| --- | --- | --- |
| 加载→成功/空/网络失败 | 有不同文案与合适动作 | 非空旧缓存不被当空成功 |
| 失权/权限未知 | 隐藏名称/正文、限制写入 | API、派生缓存与晚到响应都不泄露 |
| 切项目时旧响应晚到 | 场景保留新项目 | 真正代次隔离、不能只Abort |
| 空页有游标/游标过期 | 继续加载/从首屏刷新 | 不丢页、不同查询不混合 |
| 同键不同输入/revision冲突 | 保留原操作/比较后重新确认 | 当前核权、幂等重放优先、无部分提交 |
| 404未见提交/410清理 | 未知/终结不能复活 | 持久占位及在途请求故障注入 |
| 断网/输入丢失/跨主体 | 原查询/同actor恢复 | 实际存储故障、原秘密不落盘 |
| 模型保存/测试/应用 | 结果与运行分开 | 不重复收费、只改模型且execution原样保留 |

以上是全部首批必要恢复设计的审阅覆盖，不声称所有浏览器分支或真实故障已验收。全量上线前仍需实现测试；本次不新增真实服务或重跑历史实验。
