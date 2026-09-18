---
status: proposed
design_revision: RM-MODEL-API-r1
review_revision: R01-2026-09-12-candidate
implementation: not-started
---

# 首批模型设置：保存、测试与专用应用字段草案

页面独占新增浏览器字段唯一草案；补旧模型UI r3未定协议，独立RM-MODEL-API r1，不改变[现行项目PATCH](first-batch-browser-api-contract.md)或已采用界面范围。全部为本次六项完整待审包中的候选。内部实现责任只在[后端模型稿](backend-model-operations-draft.md)维护；若该稿使用内部状态，不自动成为HTTP枚举。

2026-09-12候选修订：依据[设计审查R01](../reviews/2026-09-12-design-readiness/README.md#r01key-保存的永久未知状态)，补充原saveId原子终结协议，替代本稿原先“Key保存未知只能查询”的限制。新增端点、closed_without_save终态及页面动作仍待采用，不改写历史r1技术核对轮次，也不表示已实现。

## M01 通用约束与对象

当前主体只管理自己的供应商，不共享、不编辑平台默认。每次查询、预览、提交及结果读取重新核权；未知503 AUTHORIZATION_UNCONFIRMED，不提供旧名称／Key／原正文。JSON、CSRF／Origin、256KiB和error结构沿现行首批契约。ID/revision为不透明非空字符串最长128；时间UTC RFC3339，任何示例不是实际资源。

Provider为稳定供应商身份；revision为当前完整连接／模型快照版本；model行id稳定，外部modelId可修改；每行一个稳定modelProfileId，应用时固定providerRevision与secretVersionId。删除／停用模型或供应商不在首批，新快照不能丢弃原模型行，保留历史快照供已有项目。SecretVersion仅受控只写，状态不得透露原值或尾号。普通保存不暗改项目版本，旧测试结果保留在旧快照。

列表GET统一limit默认50、1..100，cursor按主体／查询／排序绑定，稳定ID升序；q最多200，字面子串，不搜秘密；空items可有nextCursor。400 INVALID_CURSOR／409 CURSOR_EXPIRED沿现行，过期首屏刷新，不拼旧页；无total。

## M02 接口目录

| 方法／路径 | 用途 |
| --- | --- |
| GET /api/model-providers?q=&cursor=&limit= | 当前owner供应商摘要列表 |
| GET /api/model-providers/{providerId} | 当前完整快照 |
| GET /api/model-providers/{providerId}/versions/{providerRevision} | 当前有权读取的指定不可变快照，字段见§5.1 |
| POST /api/model-provider-saves | 原子创建／更新完整快照与Key意图 |
| GET /api/model-provider-saves/{saveId} | 查询原保存回执 |
| POST /api/model-provider-saves/{saveId}/close | 原子终结原保存；已提交则返回原结果，未提交则阻止迟到保存 |
| POST /api/model-test-previews | 得到固定单模型、出站及费用政策的测试确认 |
| POST /api/model-tests | 登记一次测试及持久外发责任 |
| GET /api/model-tests/{testId} | 查询该次测试状态／结果 |
| POST /api/projects/{projectId}/model-application-previews | 项目模型替换预览 |
| POST /api/projects/{projectId}/model-applications | 比较预览并只换模型 |
| GET /api/projects/{projectId}/model-applications/{applicationId} | 原应用回执 |

预览POST仅建立短期授权比较上下文，无模型调用、费用预留或配置修改，不作为业务工作启动。所有links.*均为API链接；浏览器恢复页另用`/settings/model-saves/{id}`、`/settings/model-tests/{id}`、`/projects/{projectId}/model-applications/{id}`。

## 3. 供应商读取与完整保存

列表200：`{items:[{id,name,revision,modelCount}],nextCursor}`；modelCount为当前可读模型行数。详情200：

```json
{
  "id":"provider_1","name":"开发网关","revision":"pv_7",
  "baseUrl":"https://gateway.example.invalid/v1","apiFormat":"openai_chat_completions",
  "secret":{"configured":true,"versionId":"sv_3","availability":"available"},
  "models":[{"id":"modelrow_1","modelProfileId":"mp_1","modelId":"deepseek-chat","displayName":"对话模型","contextWindow":256000,"maxOutputTokens":128000,"reasoning":true,"vision":false}],
  "updatedAt":"2026-09-11T12:00:00Z"
}
```

secret.configured为是否绑定版本；未配时versionId=null，availability=missing；其余available/unavailable/unknown表示受控存储／解密资格观察，不证明远端接受Key。详情不内嵌动态测试结果，测试只从原testId查询；当前会话可留最近测试索引，不能将旧providerRevision结果挂到新版本。

保存POST带Idempotency-Key UUID saveId：

```json
{
  "providerId":"provider_1","expectedRevision":"pv_7",
  "name":"开发网关","baseUrl":"https://gateway.example.invalid/v1","apiFormat":"openai_chat_completions",
  "secret":{"mode":"keep"},
  "models":[{"id":"modelrow_1","modelId":"deepseek-chat","displayName":"对话模型","contextWindow":256000,"maxOutputTokens":128000,"reasoning":true,"vision":false}]
}
```

新建providerId=null且expectedRevision=null，模型行id=null；更新二者非空、id必须属于该供应商，不能移用他人行或省去已保存行。本候选每供应商1..50行，modelId非空1..200且同供应商唯一；displayName可null／空则显示modelId，最长200；name非空最多100；baseUrl最多2048，仅https，无userinfo／fragment／query，去尾斜杠不变更其余路径语义；不能包含最终`/chat/completions`路径。地址保存通过格式校验不表示允许出站。contextWindow和maxOutputTokens为1..2147483647整数，maxOutputTokens≤contextWindow；reasoning／vision布尔，不从模型名字推断。

secret必填：`{mode:"keep"}`或`{mode:"replace",value:"<只写值>"}`，第一种禁止value，第二种非空1..8192 UTF-8字节，原样使用、不trim、不回显。创建只允许replace；更新keep固定原版本，replace生成新版本，即使明文相同也不与旧值公开比较。不允许null／空值代表删除；真实密码输入只在本次表单内存，发送后立即清输入框，未知也不重新索要Key重发。

同actor、provider_save、saveId唯一。先查已提交／确定拒绝／关闭／清理占位，再做新修订校验；可比较的保存输入同键不同正文409 IDEMPOTENCY_CONFLICT，关闭／清理按下文先处理。秘密参与受保护的请求比较摘要，具体HMAC与加密责任在后端稿；禁止保存可离线猜测Key的裸哈希。有效新写短事务保存完整快照、secretVersion及模型profile映射、原结果。密钥处理失败整笔回滚，无半个配置。

R01补充：上述槽位还允许无原始输入的closed_without_save终态。合法保存POST命中它时返回409 MODEL_SAVE_CLOSED，先于原输入比较、秘密解密和新修订校验，不能把该请求变成一次新保存。通用认证、请求体／信封格式检查仍可先行；任一前置错误也不解除已终结槽位。

201新供应商／200更新及重放，SaveResult：

```json
{"saveId":"a2394178-23a4-43ba-a886-2755c1f9ad16","outcome":"committed","providerId":"provider_1","providerRevision":"pv_8","secretVersionId":"sv_4","committedAt":"2026-09-11T12:01:00Z","links":{"provider":"/api/model-providers/provider_1","operation":"/api/model-provider-saves/a2394178-23a4-43ba-a886-2755c1f9ad16"}}
```

原结果200可为上形或确定拒绝形`{saveId,outcome:"rejected",error:{code,message,fieldErrors,requestId},decidedAt}`。拒绝只在服务器已取得该键唯一责任、确认未发生保存并持久终结时返回；400解析／无资格等前置错误不承诺有拒绝记录，网络不明仍查原键。查询未见记录404 MODEL_SAVE_NOT_FOUND保持未知；清理410 MODEL_SAVE_RESULT_REMOVED且键不复用。409 PROVIDER_REVISION_CONFLICT为明确终结拒绝后，回读并重新确认新操作。当前provider回读可已是更晚版本，原回执不改。

### 3.1 R01：终结原保存，解除未提交操作的未知状态

页面在保存结果未知时提供“查询原结果”“终结原保存”及稍后返回的入口。终结须由用户明确触发；确认文案说明：“已经保存会返回原结果；尚未保存会阻止这次请求以后保存。它不会撤销已保存配置。”关闭弹窗、超时和GET 404均不会自动调用终结。

`POST /api/model-provider-saves/{saveId}/close`，路径为原UUID，`Idempotency-Key`必须等于同一个saveId，正文严格为`{}`。不新建closeId，也不接受providerId、原修订、配置正文、秘密、摘要或“用户确认未保存”字段。路径和头不一致返回400 INVALID_IDEMPOTENCY_KEY，正文不符合形状按通用校验拒绝。

该动作以当前认证actor在`(actor, provider_save, saveId)`作用域内关闭自己的槽位，不能指定另一个actor。写请求核Origin／CSRF及当前模型配置操作资格；原操作已有结果时仍核其当前结果读权和已绑定目标owner资格。未绑定目标的空槽位允许由该actor终结，不要求先有Provider或重新输入Key，也不依赖模型出站、Key解密成功或当前Provider修订。结果不可披露时隐藏404 RESOURCE_NOT_FOUND；当前主体无动作资格403 MODEL_SAVE_CLOSE_NOT_ALLOWED；会话无效401；资格未知503 AUTHORIZATION_UNCONFIRMED。以上拒绝均不证明原操作未提交。

关闭与保存竞争同一个持久唯一槽位，不建立独立的“关闭成功但旧保存仍可提交”记录：

| 槽位的最终事实 | 关闭响应及原GET | 页面后续 |
| --- | --- | --- |
| committed | 200返回原SaveResult，不改providerRevision、secretVersionId和committedAt | 显示这次已保存，另读当前配置；不撤销、不自动重新保存。 |
| rejected | 200返回原确定拒绝回执，不改decidedAt | 可回草稿，由用户修正、重新输入必要Key并明确发起新saveId。 |
| 无已提交结果，关闭取得槽位并提交 | 200返回closed_without_save；GET随后同形 | 显示“原操作已终结，未保存”，可回草稿；新保存必须用新saveId并重新确认。 |
| 另一事务持槽，锁等待或提交结果无法确认 | 503 RESULT_UNCONFIRMED | 仍未知，查询原saveId或重试同一close；不得换键保存。 |
| 结果已清理 | 有权410 MODEL_SAVE_RESULT_REMOVED | 显示原记录已清理，不能称未保存、复用原键或自动重建供应商。当前配置须另查。 |

新终态SaveResult示例：

```json
{"saveId":"a2394178-23a4-43ba-a886-2755c1f9ad16","outcome":"closed_without_save","closedAt":"2026-09-12T12:01:00Z","links":{"operation":"/api/model-provider-saves/a2394178-23a4-43ba-a886-2755c1f9ad16"}}
```

此形状不含providerId、输入摘要、Key或secretVersionId；保存请求可能从未到达，所以不能伪造它们。closedAt是关闭事务记录的固定UTC时间，不证明原请求曾被服务器接收。关闭操作不新增模型请求、SecretVersion、供应商或项目修改。

GET仍不会终结空槽位，404 MODEL_SAVE_NOT_FOUND始终只表示本次查询未见结果。close本身不会因同主体空槽位返回该业务404；它须提交closed_without_save，或明确返回认证／授权／限流／校验／结果不确定错误。关闭响应丢失后查询仍404时，可继续查询或重试同一个close，不能把404改判为未保存。两次close必须返回同一终态；已closed的合法迟到保存一律409 MODEL_SAVE_CLOSED，旧输入相同与否都不重新执行。close不与原保存正文做幂等输入比较，因为它是同一槽位的互斥终结动作。

关闭结果与查询结果按actor／saveId／请求代次归属。切账号不关闭前一个主体的操作；重新登录同一actor后可核原结果。页面核对恢复索引中的原actor，未知时先显示当前账号和操作定位供核对，不能把另一actor作用域内空槽的关闭解释为原主体请求已终结。相关页面与多标签在未知或关闭请求未确认时暂停该次草稿的新保存；跨标签提示仅辅助同步，服务端安全性来自唯一槽位，不能靠浏览器锁证明旧请求已停。不同saveId仍是不同显式操作，不按Key或供应商名称合并；更新同一供应商另受expectedRevision保护。

## 4. 单模型测试：预览与费用确认

测试消费已保存的固定连接／模型／密钥版本；不能测试未保存草稿，不暗中探测。服务端发送固定短文本，不接受任意prompt、工具、图片或流式输出。候选输入消息固定“Reply with OK.”，max completion tokens=min(16,保存模型maxOutputTokens)，真实适配须能正确映射限制，单请求超时30秒，预算／出站策略待用户批准。

POST /api/model-test-previews正文`{providerId,providerRevision,modelRowId}`。200：

```json
{"previewId":"tp_1","expiresAt":"2026-09-11T12:06:00Z","providerId":"provider_1","providerRevision":"pv_8","modelRowId":"modelrow_1","modelId":"deepseek-chat","secretVersionId":"sv_4","policyRevision":"testpolicy_1","cost":{"basis":"request_count","units":1,"mayCharge":true,"currency":null,"maxEstimatedCharge":null},"requestLimit":{"maxOutputTokens":16,"timeoutSeconds":30},"canSubmit":true,"reasonCodes":[]}
```

首批推荐有限请求次数预算，每次测试1单位，明确可能收费；currency/maxEstimatedCharge均null，不是0或免费。金额预算是需要另审可信价格版本和计量的替代，不能同时把次数方案当金额上限。previewId随机、5分钟、绑定actor／固定版本／政策，预览不预留额度。

预览在同一次当前主体观察中读取未核清测试、额度窗口和处理器登记。三项结果互不替代：queued、running和recovery.open的unknown计入Submit现有候选阻断规则；新测试会命中该规则时，canSubmit=false且reasonCodes含TEST_ALREADY_OUTSTANDING。额度不足或不可用仍单独给QUOTA_UNAVAILABLE。其他reasonCodes为EGRESS_NOT_ALLOWED／SECRET_UNAVAILABLE／PROFILE_UNAVAILABLE／TEST_HANDLER_UNAVAILABLE／LIMIT_UNSUPPORTED。没有匹配处理器登记时给TEST_HANDLER_UNAVAILABLE。处理器登记读取失败时整次返回503 TEST_HANDLER_UNCONFIRMED，不建立Preview记录，不用RESULT_UNCONFIRMED冒充只读观察失败。没有额度、处理器或正确输出限制映射时不能确认。无价格不是次数方案的单独阻止原因。本文现有Submit候选是每actor最多1笔，因此命中时只有一笔阻断记录；该数值仍是未采用的S05候选，改变它时须同时修订Preview和Submit形状。

只有TEST_ALREADY_OUTSTANDING分支可在预览顶层增加`existingTestId`和`links:{operation}`，且二者同时出现。服务端只返回当前actor仍有权读取的阻断记录testId和`/api/model-tests/{testId}`；其他主体、其他原因和不可读原结果均不得返回这些字段。该分支示例为`{"canSubmit":false,"reasonCodes":["TEST_ALREADY_OUTSTANDING"],"existingTestId":"03c4ae03-fcb4-4972-a0c3-7741e3b37947","links":{"operation":"/api/model-tests/03c4ae03-fcb4-4972-a0c3-7741e3b37947"}}`，其余预览字段仍按上方完整形状返回。

POST /api/model-tests带Idempotency-Key UUID testId，正文`{previewId,confirmPotentialCharge:true}`；false拒绝422。服务端先按当前主体读取原testId并用原schema比较确切输入。原键与原输入匹配时返回原回执，优先于preview期限、处理器、未核清测试、额度和其他新建可用性检查；同键异输入仍409 IDEMPOTENCY_CONFLICT。没有原操作时才重查当前actor／版本／策略、处理器、未核清测试和额度。版本或策略变化409 TEST_PREVIEW_CHANGED，过期409 TEST_PREVIEW_EXPIRED，不静默重取并提交。命中现有候选未核项阻断条件时返回409 TEST_ALREADY_OUTSTANDING。若当前actor仍可读阻断记录，错误在通用字段外增加`details:{existingTestId,links:{operation}}`；不可读或属于其他actor时不返回details。该提交竞争定位与Preview顶层定位并存，Preview观察不能消除两者之间的竞态。预算不足409 TEST_BUDGET_UNAVAILABLE。处理器缺失409 TEST_HANDLER_UNAVAILABLE；处理器登记读取失败503 TEST_HANDLER_UNCONFIRMED，不登记测试、不预留额度、不消费预览。其他未知仍用对应503，无免费绕过。202只表示登记测试及持久责任，绝不表示测试通过；同键登记重放200/202取当前状态，但原任务身份与acceptedAt固定。

TestResult（登记响应和GET同形）：

```json
{"testId":"03c4ae03-fcb4-4972-a0c3-7741e3b37947","providerId":"provider_1","providerRevision":"pv_8","modelRowId":"modelrow_1","state":"queued","resultRevision":"tr_1","acceptedAt":"2026-09-11T12:02:00Z","observedAt":null,"result":null,"budget":{"unit":"request","amount":1,"status":"reserved"},"charge":{"status":"not_sent","currency":null,"amount":null},"recovery":{"state":"open","canStartNewTest":false},"links":{"operation":"/api/model-tests/03c4ae03-fcb4-4972-a0c3-7741e3b37947"}}
```

state=queued/running/passed/failed/unknown/rejected。queued持久登记但尚无实际调用开始观察；running有实际调用开始依据；passed只该快照的一次成功探测；failed有明确供应商拒绝或已确认响应无效依据；unknown网络／进程断点无法判断外部结果；rejected确定未发出（政策或权限失效）。queued等待候选10分钟，证明尚未进入可能发送且旧发送权失效才能rejected/NOT_STARTED；上游5xx／限流等已取得明确响应的通用失败用PROVIDER_REQUEST_FAILED，网络响应未确认仍unknown，不混为确定失败。result非null时`{code,summary,latencyMs,usage:{inputTokens,outputTokens}|null}`，code=OK／NOT_STARTED／PROVIDER_REQUEST_FAILED／PROVIDER_ACCESS_REJECTED／MODEL_UNAVAILABLE／PROVIDER_RESPONSE_INVALID／REQUEST_TIMEOUT_UNCONFIRMED／POLICY_CHANGED／ACCESS_REVOKED；没有任意供应商响应正文。不能把所有失败归Key错。usage只来自可核实响应，缺失为null。

budget.status=reserved/consumed/released，表示1单位本地次数，可能发送前由预留转已消费；charge.status=not_sent/possible/unknown且currency/amount均null，不声称金额结算。确定未发才能释放；错误或unknown不自动退款／再发。每actor最多1笔未核清测试仍是未采用的S05候选。无论最终上限为何，预览都用canSubmit=false和TEST_ALREADY_OUTSTANDING镜像当次Submit的阻断结论；新Submit仍在事务内重查并以409拒绝，不能把预览观察当预留或锁。

recovery.state=open/closed_without_result/not_needed；passed/failed/rejected为not_needed，queued/running/未关闭unknown为open，运维关闭unknown为closed_without_result；后者只允许受控运维证明旧发送者已失去调用能力并审计关闭，历史state仍unknown，预算保守已消费，不伪造成通过／失败。canStartNewTest在queued/running/仍open的unknown为false，其余为true仅表示原操作不再阻塞，新测试仍须新预览及收费确认和全部当前门槛。本批无关闭未知的浏览器接口，前端不能自行忽略未知。

查询404 MODEL_TEST_NOT_FOUND保持未知，410 MODEL_TEST_RESULT_REMOVED不发新探测。5xx／断网先查原testId；轮询2→5→10→30秒，隐藏暂停，单请求合并；以actor／目标／请求代次丢弃晚到状态，resultRevision只作等值比较，不按不透明文本比较大小，回读权失效立即清信息。测试结果变更服务器单调修订；一次客户端查询在途，其后回读覆盖，不并发应用两个结果。

## 5. 仅更换模型：预览与原子应用

POST /projects/{projectId}/model-application-previews正文`{providerId,providerRevision,modelRowId}`，当前project owner、有配置资格，绑定具体候选快照。不发送Key，不生成新配置版本。200：

```json
{"previewId":"ap_1","expiresAt":"2026-09-11T12:08:00Z","projectId":"prj_1","expectedProjectRevision":"prjrev_7","before":{"configurationRevision":"cfg_4","model":{"reference":{"mode":"inherit"},"profileId":"mp_old","snapshotId":"pv_old"},"execution":{"reference":{"mode":"reference","id":"exec_1"},"effectiveVersionId":"ev_7"}},"candidate":{"modelProfileId":"mp_1","providerId":"provider_1","providerRevision":"pv_8","modelRowId":"modelrow_1","modelId":"deepseek-chat","secretVersionId":"sv_4"},"canApply":true,"reasonCodes":[]}
```

before.model允许null表示原未配置；execution.effectiveVersionId可null，无法取得原有效版本时canApply=false／EXECUTION_SNAPSHOT_UNCONFIRMED。原execution已知版本当前不可用时允许保存受限配置（canApply可true并reasonCodes含EXECUTION_CURRENTLY_UNAVAILABLE），不把保留理解为恢复可执行资格。candidate当前不可读404；已知可引用但暂不可用允许受限保存，并提示必要条件仍不足。预览返回的显示名／BaseURL／参数由客户端读取已固定provider详情（不得用最新替代该revision；如下§5.1补只读版本查询），不重复第二套Provider字段。

2026-09-12 项目契约检查发现，本节尚未定义原模型的完整响应分支：before.model=null 只表示未配置，§5.1要求的受限标记没有机器形状；原模型也未提供读取固定历史快照所需的 providerId 定位。此项记为[检查 C06](../reviews/2026-09-12-project-contracts/README.md#c06-应用预览缺少原模型的完整形状)。须在本唯一候选中补齐可读、未配置与受限的区分及历史定位，权限未知仍沿M01的503规则，再验收替换前模型预览；不能以null代替受限、查询最新版本代替原快照，或由前端遍历供应商猜测来源。当前示例保留为未补齐的候选，不代表所有分支已定义。

### 5.1 固定快照补充读取

`GET /api/model-providers/{providerId}/versions/{providerRevision}`，同§3 Provider形状，revision固定；按当前owner及历史快照可读性核权，404不可披露／410已清理，secret仍不回显。连同§3.1的原保存终结，本稿共12个新增接口；当前应用预览依赖本只读接口展示完整候选参数。原模型来源不可披露时仅给不含名称的受限标记，不能据此禁止有权的配置修复。

应用POST带Idempotency-Key UUID applicationId，正文仅`{previewId}`。服务端首先当前身份及原操作查询，再检查预览actor／目标／5分钟期限、候选仍为本次固定快照且未被禁止引用、projectRevision及原configurationRevision一致，原execution引用和有效版本与预览完全一致。候选供应商当前版本变化也409 MODEL_APPLICATION_PREVIEW_CHANGED，重新预览后由用户再确认，不默默选旧/新。若命中原已提交则重放优先于预览过期。

同一previewId只许单次消费，consumedBy与接受测试／提交应用同事务；同操作重放优先，不同testId或applicationId重用预览409 PREVIEW_ALREADY_CONSUMED，可返回本人原操作定位。

事务仅将modelProfile显式reference到候选稳定profile并固定候选providerRevision/secretVersion，execution原reference及有效版本原样复制；有实际模型引用／固定版本变化时生成新configurationRevision、projectRevision、creationContextRevision与回执；若这些引用和版本完全相同则no-op成功，保留原修订，仍保存本操作回执及预览消费。绝不调用现行完整configuration PATCH重解析execution；平台默认后来更新不影响这次保留。既有项目其他资料和Issue范围不变，不启动运行。

200 ApplicationResult：`{applicationId,outcome:"committed",projectId,projectRevision,configurationRevision,providerRevision,modelProfileId,retainedExecutionVersionId,committedAt,links:{project,operation}}`；retainedExecutionVersionId须与预览相同。确定拒绝回执同SaveResult拒绝形，主键字段用applicationId。未知查询404 MODEL_APPLICATION_NOT_FOUND；清理410 MODEL_APPLICATION_RESULT_REMOVED，不复活；确定比较冲突409 MODEL_APPLICATION_PREVIEW_CHANGED、PROJECT_REVISION_CONFLICT或EXECUTION_SNAPSHOT_UNCONFIRMED，整笔不写。重放回执不代表当前项目仍该版本，需另GET project。

## 6. 三种原操作恢复与保留

保存／测试作用域分别actor+provider_save+saveId、actor+model_test+testId；应用actor+project+model_apply+applicationId。键UUID，各自不混；相同保存／测试／应用键不同输入（包含预览ID或秘密意图）409，先原操作后新版本校验。保存终结沿§3.1竞争原槽位，不属于使用原键另存一份正文；已closed的保存先返回MODEL_SAVE_CLOSED，不做秘密比较。

浏览器sessionStorage只保存允许的非秘密业务快照、原键及资源ID，**Key原文既不进sessionStorage也不进localStorage**；localStorage仅actor隔离的operationKind/id/可选projectId/时间。保存发出后清Key内存，不重收Key重放原保存；未知期间可查询原saveId或明确终结原保存，尚未取得终态不得换键重发。只有closed_without_save或rejected确定回执才直接允许回草稿、重新输入必要Key并明确建立新操作；committed转原成功结果和当前配置读取。保存前生成并提供浏览器恢复链接；存储不可用仍可复制链接。浏览器原秘密输入丢失不妨碍查询或终结。

模型保存／应用原回执在对应主体／项目有效历史期保留；可清正文但保留不可重用键占位。测试结果保留30天，之后仅费用责任、所绑定快照身份和不可复用键占位；unknown的费用责任不得因30天清理消失。无权404优先于410；账号／项目ID永不复用。用户业务删除／数据保留策略另有变更时不得恢复旧键。

closed_without_save没有原输入或秘密比较材料，保留固定回执和原唯一槽位至该主体业务历史期结束，不按查询次数或短期unknown时限删除。后续清理只能保留不可复用removed占位并给有权410；账户最终清理须先封闭该actor写入口且身份永不复用，不能删掉槽位后让迟到原请求重新保存。410允许用户查看当前可读配置并重新做业务选择，但不提供“这次未保存”的证明或自动恢复原草稿。

## 7. UI与验收覆盖

Key三项见[候选页面](model-key-save-design.md)；测试新原型在主预览model-test；应用沿[F04已采用三项](model-project-apply-design.md)。此次不请求共享／默认编辑／删除／禁用／完整费用平台。

待验收：同键含Key不同正文冲突、密钥失败全回滚、预览过期重放优先、旧execution默认变但精确保留、测试登记后崩溃／外发后丢响应不重复收费、额度／处理器未知不可确认、次数预算不冒充金额上限、无权原结果不披露、原Key丢失仍可查询／终结、原回执与最新配置分开、模型快照切换不移植旧测试。R01还须覆盖请求未到服务器、持槽前503、关闭与迟到保存竞争、双close、关闭回执丢失、已提交后关闭、无权／错主体、关闭后清理及旧保存重放。只设计，不执行任何真实模型请求。

## 8. B04/B05 本轮字段收敛候选

状态 `RM-MODEL-API-r2-candidate / PROPOSED_NOT_ADOPTED`，2026-09-13。本节只替代明确列出的旧候选歧义；12端点目录和 B04/B05 划分保持，未新增产品实现或整体采用。

B04 的 ModelView.displayName 始终输出字符串。输入 null/空字符串规范化为同一空显示名意图，展示时取 modelId；本轮仍要求输入模型对象带 displayName，省略不额外成为合法输入。所有 error.fieldErrors 必返数组，持久 rejected.error.requestId 固定为作出决定的原请求 ID，不能回读时重写。POST 的确定拒绝用对应标准错误 HTTP 响应，GET/close 返回200的 rejected 联合；客户端不能仅凭409认定终结，先查原回执。根/DB暂时故障不持久伪造 rejected。

保存比较机制由[内部 §7](backend-model-operations-draft.md#7-b04-保存事务收敛候选2026-09-13)单 vault 推荐替代 §3 的具体 HMAC 指称；不改变 Key 禁裸哈希与精确比较目标。信封解析先于槽检查；合法 JSON 的业务字段验证不能覆盖有权 removed/closed 终态。新输入仍严格拒绝未知字段。

### 8.1 C06 原模型的完整分支

定点替代 §5 示例的 `before.model` 和原“尚未补齐 C06”描述。其余 before/candidate/提交字段保持。before.model 不再使用裸 null，而是以下精确联合：

| 分支 | 完整字段 |
| --- | --- |
| 未配置 | `{status:"unconfigured"}`。没有绑定模型版本，不能用于表示失权。 |
| 可读 | `{status:"readable",reference:ProfileChoice,modelProfileId:string,modelProfileVersion:string,providerId:string,providerRevision:string,modelRowId:string,links:{snapshot:string}}`。ProfileChoice 沿首批 §5；snapshot 是既有指定版本 GET 的 API 路径。modelProfileVersion 与 providerRevision 必须对应同一不可变映射。 |
| 受限 | `{status:"restricted",reasonCodes:["ORIGINAL_MODEL_UNAVAILABLE"]}`。不返回 reference、profile/provider/row/secret ID、名称、地址或版本链接。 |

权限未知仍整次503 AUTHORIZATION_UNCONFIRMED。可读旧快照缺失/材料已清理且不可再展示时用 restricted，不用最新模型替代，也不遍历供应商猜定位。模型已经禁用但历史仍可读时可给 readable，其当前秘密可用性在指定版本详情表示；不把历史可读解释为可调用。

返回 readable 后，前端只 GET links.snapshot 对应固定 providerId/providerRevision，并选 modelRowId；核响应身份、版本和行归属。迟到旧请求不得覆盖当前预览。原模型受限不单独禁止有权配置修复；execution 的固定版本与当前应用比较仍完整保存于服务端 Preview，不因公开原模型裁剪而丢失。

### 8.2 测试与恢复补充

§4 示例 timeoutSeconds=30 为候选示例，实际 preview.requestLimit.timeoutSeconds 取固定 timeLimitPolicy 版本值；不得预览30秒、实际执行另一值。output 上限仍 min(16,已保存 maxOutputTokens)，无法正确映射时 LIMIT_UNSUPPORTED。TestResult 的 budget 是本地次数，charge.status 在可能发送且无金额依据时为 unknown，currency/amount始终null。收到明确成功或错误仍不能把 charge 改成免费或已结算。

§5 的预览 POST 完整路径以 M02 为准，即 `/api/projects/{projectId}/model-application-previews`。testId 原接受回执的 acceptedAt 与固定模型身份不变，resultRevision 只作等值比较；操作状态可前进，但不把观察写回 ProviderRevision。B04/B05 的登录恢复 Destination 使用已有认证稿的 provider_save/model_test/model_apply 种类，服务端只允许固定同源恢复路径，先核当前主体与目标；首次查询404仍可返回原操作恢复页供核查，不触发提交或close。

C05 在[首批契约 §10](first-batch-browser-api-contract.md#10-c05-固定预算时限摘要候选)维护。本节仅补 C06 和模型字段，不再复制项目预算摘要。C05/C06 均为设计已补、待采用、待实现、待真实验收。


本候选 B04/B05 的 apiFormat 首批仅 `openai_chat_completions`，不从已有原型格式选项推导 Responses/其他协议已经可消费。保存与读取校验此明确枚举；扩展格式需要新的协议映射和本地验证。
