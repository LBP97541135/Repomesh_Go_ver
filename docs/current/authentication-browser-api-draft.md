---
status: proposed
design_revision: RM-FIRST-ACCESS-r3
implementation: not-started
---

# 首批认证与发现：浏览器字段草案

2026-09-12 B02 采用补充。用户已回复“确认，继续”，采用范围以 [B02 采用记录](b02-authentication-adoption.md)为准。本文对应认证及必要秘密子集的“待采用”描述保留原提案历史，已由该记录替代；模型、预算、运行和其他未列明部分仍待采用。实现与验收进度见 [B02 记录](../development/2026-09-12-batch-02/README.md)。

页面独占的新增认证字段唯一草案。用户要求一次交付六项全部，本稿据此展开候选，不代表认证方向或参数已采用。2026-09-12在r2上修订为RM-FIRST-ACCESS r3候选，补齐已有会话的登录返回、固定路由及后续消息提交恢复的类型边界；本次修订不重置原技术讨论轮次，也不表示新增决定已采用。已采用[首批契约](first-batch-browser-api-contract.md)§2／3／8字段不修改，冲突以现行契约为准并提交修订，不同时维护两套session定义。

## A01 接入与会话候选

首批github.com、单一GitHub App用户授权；仓库发现只承诺当前凭据覆盖，安装外受邀私仓可能缺席，保留完整J1目标。无需为首批新增OAuth App／PAT入口。代价是发现可能partial，用户需要补安装范围而不是看到所有参与仓库。

同源HTTPS、服务端会话、__Host-前缀Secure／HttpOnly／SameSite=Lax Cookie；当前/api/session字段不增加。初始会话绝对12h／空闲30min、授权尝试10min；这些是待用户批准的可调整参数，不是外部撤权SLA。服务端每次业务写入重核本地主体／当前授权；授权观察策略由[来源草案](backend-first-batch-sources-draft.md)唯一维护。

本稿新增JSON遵循现行UTF-8、256KiB、重复键／非法Unicode拒绝、未知字段422、no-store、统一error结构。可信actor来自Cookie，不接受客户端userId。登录发起要求精确Origin＋同源JSON；已登录写入再核X-CSRF-Token（值来自现行session）。回调仅走专门state／PKCE／浏览器绑定验证，不要求业务CSRF。所有本稿错误不含供应商原始响应、code、state、凭据或隐藏资源名。

## A02 接口目录与返回目标

| 接口 | 请求／成功来源 | 权限与副作用 |
| --- | --- | --- |
| POST /api/auth/github/login | §3 Start／StartResult | 未登录同源请求，创建一次浏览器绑定尝试；不做业务写入 |
| POST /api/auth/github/reconnect | §3 Start／StartResult | 当前登录及CSRF，仅同一个外部账号 |
| GET /api/auth/github/callback | §4 | GitHub回调；一次code交换及身份核实 |
| GET /api/auth/attempts/{attemptId} | §5 AttemptResult | 发起浏览器绑定；reconnect另要求原actor或对应已轮换的同actor会话 |
| POST /api/auth/logout | 空对象，204 | 当前会话＋CSRF；失效会话在同源检查后幂等204，无跨主体注销 |
| GET /api/session | 引用现行§2 | 只表示当前身份／连接观察，不能证明某次重连成功 |
| GET /api/repositories | 引用现行§3 | 当前可披露候选，分页／coverage保持现行 |

新增浏览器页面为`/auth/result/{attemptId}`及`/login`，它们不是API地址。前者无code、state、token；attemptId不是授权。登录恢复页只显示“原页面”，登录后才读名称。

Destination为下表严格联合类型。每行列出的字段全部必填且只允许这些字段；`kind`、`operationKind`必须取对应常量，不接受一个所有ID都可选的宽松对象。ID沿相应业务契约，为UUID或系统已有不透明ID，最长128；operationId始终指原操作键，不新建恢复操作身份。

| 首批Destination形状 | 唯一浏览器返回路由 | 返回前及进入时核验 |
| --- | --- | --- |
| `{kind:"home"}` | `/`，固定工作区入口 | 当前session；只显示当前可读项目。 |
| `{kind:"project",projectId}` | `/projects/{projectId}/issues` | 当前项目资格；列表另按每项完整内容范围核权。 |
| `{kind:"issue",projectId,issueId}` | `/issues/{issueId}` | Issue确属projectId且当前完整内容可读。 |
| `{kind:"conversation",projectId,conversationId}` | `/projects/{projectId}/conversations/{conversationId}` | 会话确属projectId且整个会话内容范围当前可读；能读项目外壳不充分。首批只读页头／消息，见[恢复R06](first-batch-recovery-design.md)。 |
| `{kind:"operation",operationKind:"project_create",operationId}` | `/project-creations/{operationId}` | 当前原操作者；不要求尚未创建的projectId。 |
| `{kind:"operation",operationKind:"project_update",projectId,operationId}` | `/projects/{projectId}/updates/{operationId}` | 当前原操作者及原项目操作作用域。 |
| `{kind:"operation",operationKind:"issue_create",projectId,operationId}` | `/projects/{projectId}/issue-creations/{operationId}` | 当前原操作者及原项目操作作用域。 |
| `{kind:"operation",operationKind:"provider_save",operationId}` | `/settings/model-saves/{operationId}` | 当前原actor的provider_save作用域，不接受projectId。 |
| `{kind:"operation",operationKind:"model_test",operationId}` | `/settings/model-tests/{operationId}` | 当前原actor及固定模型测试结果读取资格，不接受projectId。 |
| `{kind:"operation",operationKind:"model_apply",projectId,operationId}` | `/projects/{projectId}/model-applications/{operationId}` | 当前原actor及原项目配置操作读取资格。 |

首批会话页失效后的Start示例为`{"destination":{"kind":"conversation","projectId":"prj_1","conversationId":"conv_24"}}`。它恢复同一只读会话，不降级冒充已回到原会话，也不创建会话或发送消息。消息内容与页头仍独立GET，返回地址不包含消息正文、草稿、候选名称或凭据。

消息发送是后续批次，提前明确其返回类型扩展为`{kind:"operation",operationKind:"conversation_message",projectId,conversationId,operationId}`，列出的五个字段全部必填。operationId对应[已采用消息契约§5](conversation-message-clarification-api-contract.md)的submissionId，唯一浏览器路由为`/projects/{projectId}/conversations/{conversationId}/message-submissions/{operationId}`；查询仍用原`C/message-submissions/{submissionId}`，作用域为`(actor,project,conversation,conversation_message,submissionId)`。必须核原操作者、项目／会话归属及当前整个会话内容读权。首批只读交付不开放该Destination；首批收到它返回422 VALIDATION_FAILED，后续发送批次接通此扩展时才能接受，不静默删掉conversationId或跳到其他原操作。该扩展不新增消息字段、发送端点或派工权限，登录完成后也只核查原提交。

拒绝额外属性（包括returnUrl、path、url、query、fragment）、未知kind／operationKind、缺字段及越界字段。服务端从上表固定路由构造相对站内地址，ID必须作为单独路径参数编码，不能当URL／路径二次解析；点路径段、路径分隔符及其编码变体不得改变目标路由。客户端不能从查询串、localStorage或OAuth回调拼出另一个返回地址。只接受本接口返回的固定站内nextPage，不能用供应商authorizationUrl或业务links.operation替代。

Start只验证Destination的形状，未登录时不探查或回显对象是否存在。生成nextPage时及用户明确继续进入目标页时均重新核当前session、归属和当前读取资格，不能把尝试成功视为原目标永久可读。对象型目标不存在、跨项目、已失权或当前账号不能认领原操作，统一返回固定`/`并只显示“返回工作区”，不携带受限目标ID／名称；资格未知或当前session未确认则nextPage=null，停在结果页重读，不能显示已回到原页面。

是否恢复了原actor，须从服务端可核实的原会话／浏览器绑定恢复上下文或原操作记录判断，不能由Destination、自报actor、URL或本地索引授予。若原身份已明确且与当前actor不同，不接续旧业务，按上文返回工作区；若原身份已无法核实，不声明已认领原操作或自动继承草稿。页面对所有操作型返回先通过当前session显示已确认的当前账号和非秘密操作ID，要求用户明确核对后才继续；无原身份依据时提示“只能核查当前账号下的此操作，不能据此确认其他账号的结果”。nextPage可以是在当前actor已获其自身作用域操作资格后由固定路由表生成的核查地址，不能被解释为原actor已匹配或原结果已取回。这里不新增已认证当前actor显式GET／close自己槽位的“必须证明历史原actor”门槛；本地索引只提示身份，实际权限仍来自当前认证与动作核验。

原操作查询仅“暂未见提交”的专用404不属于无权或业务对象不存在的降级条件：保留当前获准作用域的原操作恢复页并保持未知，不能宣称已取得原结果。Key请求从未到达服务端且旧身份绑定已清理时，用户明确核对当前账号后仍能查询／终结当前actor的同一saveId槽位；这不能证明另一个actor的同ID操作已终结。有权410同样保留原恢复页，展示结果清理及禁止复活；无权隐藏404仍优先于410。换账号不继承原业务草稿／操作身份，登录返回不自动重发任何业务。

## 3. 发起与未知

两个POST均带`Idempotency-Key: <UUID attemptId>`，正文Start：

```json
{"destination":{"kind":"home"}}
```

201首次／200同输入重放StartResult：

```json
{"attemptId":"0a2a5d23-398b-4e46-865b-fc9a1dd15738","authorizationUrl":"https://github.com/login/oauth/authorize?...","expiresAt":"2026-09-11T12:10:00Z","resultPage":"/auth/result/0a2a5d23-398b-4e46-865b-fc9a1dd15738"}
```

URL由服务器从固定App/callback及该尝试state/PKCE生成，不接受客户端改scope/provider。仅顶层导航，禁止fetch跟随OAuth；authorizationUrl不可写入日志、本地恢复索引、分析事件或复制分享。重放仅在尝试未领取code交换且未过期时返回同一URL；已领取返回409 ATTEMPT_IN_PROGRESS并查询原尝试。相同键不同Destination或用途409 IDEMPOTENCY_CONFLICT。

state原值与PKCE verifier须短期加密保存，才能重放同一authorizationUrl；摘要只用于查找和比较。交换完成／过期后销毁可用交换材料，不进日志或恢复索引。BrowserBinding候选绝对7天、不滑动；失效后只生成全新随机绑定，绝不重新激活旧身份。

作用域为浏览器绑定摘要＋用途＋attemptId；reconnect再含原actor和外部账号绑定。服务端首次创建受保护的独立HttpOnly浏览器绑定Cookie；重复start响应未知，浏览器只以原键同正文恢复。若绑定Cookie也未到达，查询403/404后不得推断原尝试不存在；显式从新登录开始，旧尝试过期或旧代次失效，不把认证重做变成业务重放。

候选：同浏览器新尝试使旧未完成尝试失效，多标签页提示“已有更新的登录，请返回当前页面”，不循环抢登录。已登录login返回409 SESSION_ALREADY_ACTIVE，先读session；切换账号必须退出后显式登录，不自动合并项目。新尝试不修改已绑定外部账号。

## 4. 回调与本地事务

只消费GitHub的code、state或error；长度有界，未知外部参数忽略但不记录正文。验证服务端尝试、state摘要、PKCE、浏览器、期限及代次；原子领取唯一交换责任，交换在事务外。重复／过期／旧回调绝不再换code或签Cookie。确认当前外部稳定账号后，短事务再次核代次、账号与尝试，把连接版本及本地会话变更一并保存。reconnect不同账号终结为ACCOUNT_MISMATCH，保留旧绑定。

完成后303至固定resultPage，能可信映射原尝试的失败也到该站内结果页；非法state或无法可信映射尝试的callback统一至固定/login无敏感错误页，不拼接未知resultPage，回调响应no-store／Referrer-Policy:no-referrer，无第三方内容，代理日志去查询串。code交换响应未知，不自动重用原code；同尝试查账号失败可在到期前用已保护token重试只读查询，禁止先绑定猜测账号。

注销事务只撤销请求携带的session及其绑定代次的未完尝试；只有当前BrowserBinding代次仍等于请求session绑定代次时才推进失效。旧session已被取代／撤销时幂等204，不撤销新session或新尝试；保留已提交业务与待办。候选：注销响应不设置覆盖会话的删除Cookie头，旧Cookie由服务端拒绝并按原期失效，避免晚到注销响应清掉新登录Cookie；新登录覆盖同名Cookie。仍可能收到旧登录响应的Cookie，但其已撤销服务端代次不能授权，下一次session读取引导重登。不得承诺浏览器网络响应顺序能保证凭据状态。

## 5. 尝试查询与重连回执

200 AttemptResult：

```json
{"attemptId":"0a2a5d23-398b-4e46-865b-fc9a1dd15738","purpose":"reconnect","state":"confirmed","reasonCode":null,"observedAt":"2026-09-11T12:03:00Z","connection":{"committedRevision":"gc_8","isCurrent":true},"nextPage":"/"}
```

字段：purpose=login/reconnect；state=pending/unknown/confirmed/cancelled/rejected/expired/superseded；reasonCode=null或USER_CANCELLED／ACCOUNT_MISMATCH／BINDING_INVALID／EXCHANGE_UNCONFIRMED／SESSION_NOT_CONFIRMED／ATTEMPT_EXPIRED／NEWER_ATTEMPT；未知外部错误统一EXCHANGE_UNCONFIRMED。observedAt为服务端观察时间或null。connection仅已核原actor的reconnect确认回执返回，否则null；committedRevision是原尝试不可变连接代次，isCurrent是本次查询的当前比较结果。不同则页面“本次已完成，连接后来有更新”，不套用旧结果。nextPage只有当前session有效且资格核验后返回站内地址，否则null，不返回账号／目标名称。

仅尝试绑定一致能查询；未绑定／尝试不可披露404 RESOURCE_NOT_FOUND；reconnect登录丢失401。匿名login查询最多返回上述无账号／目标敏感数据的状态，不授予主体。callback已确认但Cookie未到达时，query可confirmed而session401；页面只能重新登录，不能用回执取回Cookie或原token。

当前有效session=200／githubConnection=connected不证明重连成功；必须查询原attemptId且核其回执。本次unknown不因经过时间变rejected：进程已交换但结果不明时终结处理只能保留未知，用户可显式发起同账号新代次修复，旧迟到结果无法覆盖。查询不做外部交换、刷新或业务写入。

候选保留：完整非秘密尝试状态24h（在绑定仍有效且当前有权时可读的上限），之后410 AUTH_ATTEMPT_RESULT_REMOVED；最小已消费／失效标识至少保留到该binding失效且所有相关尝试／会话最大有效期结束再清理，旧attemptId在同浏览器绑定内禁止重用。绑定Cookie与尝试不使用localStorage；恢复索引可记录attemptId／purpose／时间，无URL／账号或目标内容。匿名绑定丢失不可凭索引认领结果。

## 6. 错误与页面动作

| 返回 | 页面 |
| --- | --- |
| 401 AUTHENTICATION_REQUIRED | 清身份敏感视图；先登录，原业务只查询不重发 |
| 403 ORIGIN_REJECTED／CSRF_REJECTED | 不进入授权；重新读取当前会话／重新发起受控流程 |
| 404 RESOURCE_NOT_FOUND | 通用无法读取结果，不泄露其他浏览器／账号存在性 |
| 409 ATTEMPT_IN_PROGRESS | 查原attempt；不用原code再交换 |
| 409 SESSION_ALREADY_ACTIVE | 确认当前身份，用户明确选择重连或退出 |
| 409 IDEMPOTENCY_CONFLICT | 保留原尝试，不把不同目标绑到原键 |
| 410 AUTH_ATTEMPT_RESULT_REMOVED | 旧结果无法取回，显式重新登录；不重放业务 |
| 422 VALIDATION_FAILED | 修正Destination／输入；不回显非法URL或敏感值 |
| 429 RATE_LIMITED | Retry-After；候选发起每浏览器5次/分钟、每账号10次/分钟，参数另审 |
| 503 RESULT_UNCONFIRMED | 原尝试查询；unknown不是失败 |

GET/session当前身份可读但GitHub核查未知仍按现行200＋unknown，不用401代替外部超时。查询轮询建议2、5、10秒，随后每30秒，页面隐藏暂停；手动查询合并一个在途请求，429按Retry-After。

## 7. 仓库发现衔接候选

现行GET/repositories字段不改。发现批次绑定actor、连接版本、访问代次和查询；后台受控扫描，单次上游工作有界。每页只投影当前核实allowed的仓库身份；无可靠结果503，App能力核查失败可单独unknown。分页空items但nextCursor非空继续，安装范围partial不会因末页变complete。

首批选择策略：无cursor的首页GET复用同actor／连接代次／q的未完批次；完成后下一次首页GET或10分钟过期才建新批。每次扫描最多2个上游页、5秒预算，保存上游续查位置及稳定仓ID；未完工作交coordinator持久有限待办续扫，不依HTTP存活。503后下一次同查询续原批，不把失败当空结果。续扫绑定batchId／claim版本，过期批次和旧claim不得覆盖新批；有cursor只消费其原批。批次最长10分钟，过期用现行409 CURSOR_EXPIRED；重连／访问代次变更旧cursor返回409 CURSOR_EXPIRED（先当前核权），从首屏回读并重新核手选集合。查询参数不匹配400 INVALID_CURSOR。每批按稳定本地仓库ID升序并遵守现行无固定跨页快照承诺；新发现较小ID需刷新。用户搜索只搜已可披露候选，不做全网搜索。全部选中不宣称覆盖整个账号。

安装修复候选：首批展示“联系部署管理员补齐 App 工作授权”，不新增浏览器安装写接口，不把callback installation_id当权利。用户本人有管理资格时可由部署管理员提供受控GitHub入口；产品内安装跳转／回跳流程后续另审。本批如实阻止条件不足的Issue，项目仍可受限保存。

## 8. 审核与待批准范围

A01—A02及§3—7全部为明确候选；UI三项已采用不等于这些HTTP、时间、多标签／Cookie策略采用。测试要覆盖同键并发start、回调重放、token响应丢失、重连旧session误判、错误账号、注销晚到、Cookie丢失、跨浏览器查询、发现partial／空页游标／撤权。r3另外验证会话401→同账号登录→原只读会话、跨项目／失权会话的通用落点、资格未知停留、返回前后权限变化、任意returnUrl／路径逃逸拒绝，以及首批拒绝后续conversation_message扩展。后续发送批次验证该类型携完整原作用域返回原提交，只查询且不自动重发。这里只定义待实现验收，不运行OAuth或真实仓库。
