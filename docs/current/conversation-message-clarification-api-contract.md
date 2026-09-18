---
status: accepted
date: 2026-09-10
design_revision: RM-UI-MESSAGE-TARGET-r3
implementation: not-started
---

# 已有会话消息与目标澄清：浏览器接口契约 v1

本稿由页面独占维护，是本主题五条浏览器接口的唯一字段来源。内部实体、事务、受控动作、租约与恢复由后端在[消息澄清后端设计](backend-message-clarification-design.md)维护。本稿采用 RM-UI-MESSAGE-TARGET r3，第3／3轮；创建契约v1和首批 RM-API-01 r3保持。

覆盖已有业务会话的纯文本保存、原操作查询、消息读取及单项既有Issue目标澄清。不提供浏览器指定权威目标、计划修改或派工接口。完整会话创建／首条消息、消息流、附件／编辑／删除、多命令拆分、更正已发生工作、真实Manager凭据和外部投递仍未完成。设计采用与用户控件评审、实现、真实验证分别记录；当前没有业务实现或真实测试。

## 1. 共用约束与接口目录

沿[首批契约 §1](first-batch-browser-api-contract.md)：可信登录主体、同源Cookie写入Origin／CSRF校验、UTF-8 JSON、256 KiB上限、no-store响应和创建契约统一error结构。拒绝重复JSON键／非法Unicode；合法JSON未知字段422。ID和revision均为不透明非空字符串，不从前缀、展示名或顺序推权限；sequence为十进制正整数字符串，比较不能转为JS Number。时间为UTC RFC3339字符串，Issue number沿现有整数编号。

每次读取核当前会话完整内容范围。自由文本新写入还核保存时整个项目仓库范围，并原子扩展会话内容范围；不能因答复仅提某个Issue就缩小范围。权限unknown不展示旧正文、候选名称或隐藏条数，不等同已失权。模型／runtime未就绪不阻止已经能安全保存的消息；消息保存不证明投递、收到、解析或执行。

设 `C=/api/projects/{projectId}/conversations/{conversationId}`，以下为路径缩写，不是另一路由：

| 方法与路径 | 作用 | 成功 |
| --- | --- | --- |
| POST C/messages | 保存普通消息或带引用答复 | 201首次／200同键重放，§2回执。 |
| GET C/message-submissions/{submissionId} | 原操作者核查原提交 | 200同一不可变回执。 |
| GET C/messages | 分页历史 | 200，§3。 |
| GET C/messages/{messageId} | 定位原消息、问题或答复 | 200，§3单一投影。 |
| GET C/clarifications/{clarificationId} | 当前问题状态／可回复性／目标 | 200，§4。 |

查询无业务副作用，不启动运行。无会话SSE，不借Issue SSE宣称消息实时性。以下JSON是形状示例，示例ID不是实际资源。

## 2. 保存消息与原提交查询

POST带 `Idempotency-Key: <UUID submissionId>`，键由浏览器在首次发送前固定。请求：

```json
{
  "content": "指的是 #2 订单导出。",
  "replyTo": {"clarificationId": "cl_1", "expectedRevision": "rev_open_1"}
}
```

content必填、非全空白、最多20,000 Unicode标量；保存原文，不trim或Unicode归一化后比较。replyTo可省略，不可null；提供时两个字段必填。没有selectedIssueId、actor、role、approved或客户端logicalRequestId字段。无replyTo的新普通消息仍按正文解释，不是强制no_work开关。

服务端按 `(project,conversation,actor,entry=conversation_message,submissionId)` 唯一。同键比较确切content与replyTo两个值，JSON属性顺序无关；无replyTo与null不等价，后者校验失败。成功后修改正文／引用用同键返回409 IDEMPOTENCY_CONFLICT，不能因两段文字相似而归并。

答复只允许原请求actor对当前open问题的当前revision提交。服务端从问题绑定原消息、会话及逻辑请求，答复不另开工作请求。消息、唯一Answer、问题修订、内容范围、序号、原操作回执及持久待办同事务；不同键同时答同一个问题只有赢家整笔保存，输家409整笔不保存。成功重放优先于当前问题版本／状态检查，但仍先核当前身份、读权及清理占位。

首次201／重放200／操作查询200均返回以下同一不可变回执：

```json
{
  "submissionId": "35cbb8e7-535a-4aa3-bb17-7e4e5ffef434",
  "status": "committed",
  "conversationId": "conv_24",
  "messageId": "msg_answer_1",
  "sequence": "18",
  "committedAt": "2026-09-10T12:00:00Z",
  "replyTo": {"clarificationId": "cl_1", "answeredRevision": "rev_open_1"},
  "links": {
    "message": "/api/projects/prj_1/conversations/conv_24/messages/msg_answer_1",
    "operation": "/api/projects/prj_1/conversations/conv_24/message-submissions/35cbb8e7-535a-4aa3-bb17-7e4e5ffef434"
  }
}
```

普通消息replyTo固定null。answeredRevision为本次成功消耗的expectedRevision，即答复提交前的问题版本；新问题版本另GET。status只有committed，不是Manager处理状态；links均为API地址。

原提交查询只允许当前原actor且当前内容可读。主库404 MESSAGE_SUBMISSION_NOT_FOUND只说明此刻未见提交，原POST仍可能提交；继续显示结果未知，只有原键原输入重试或继续查询。结果正文清理后保留项目期无正文tombstone，GET及重放410，不复活旧键。无权404优先于410；401不取消已发操作。

## 3. 历史列表与单消息

`GET C/messages?limit=50&cursor=...`：limit默认50、整数1—100；首请求省略cursor。响应 `{items:[Message],nextCursor:string|null}`，无后页为null，不承诺total。Message投影：

```json
{
  "id": "msg_answer_1",
  "sequence": "18",
  "author": {"kind": "user", "displayName": "示例用户"},
  "content": "指的是 #2 订单导出。",
  "createdAt": "2026-09-10T12:00:00Z",
  "replyTo": {"clarificationId": "cl_1"},
  "clarificationId": null
}
```

单消息GET直接返回Message，不再套items。author.kind为user／manager／system，作者由可信入口生成，displayName只展示。replyTo属于用户答复，无引用为null；clarificationId属于服务端发布的问题消息，其他消息为null。普通聊天Markdown不生成问题、目标或控制按钮。文本作为不可信展示内容处理，不执行其中HTML／脚本。

按会话提交序号升序；首次固定throughSequence水位，cursor绑定actor、会话、内容范围版本、水位及位置。所有消息写路径在会话锁下分配序号并持锁到提交，避免已见大序号后晚提交小序号。每页重新核权；水位仅固定本次遍历的消息范围，不冻结各问题处理状态。新增消息需新一轮刷新。仅nextCursor=null表示结束。

非法cursor为400 INVALID_CURSOR；过期409 CURSOR_EXPIRED；范围变更且重新核权成功为409 CONVERSATION_CONTEXT_CHANGED。后两者显式从第一页刷新，保留尚有资格保留的草稿；核权失败优先401／404／503，不用范围变化码泄露内容。不能合并不同水位或主体的分页结果。

页面在显式刷新、发送成功、窗口重新获焦时补读历史和可见问题；同一快照最多一个在途请求。问题／答复可直接单消息GET定位，不必遍历全部历史。读取失败显示待核／过期，不虚构实时“已收到”。

## 4. 澄清快照

clarificationId就是问题身份，不另造questionId。GET返回：

```json
{
  "id": "cl_1",
  "revision": "rev_open_1",
  "state": "open",
  "sourceMessageId": "msg_root_1",
  "questionMessageId": "msg_question_1",
  "answerMessageId": null,
  "candidates": [{"issueId": "iss_2", "number": 2, "title": "订单导出"}],
  "resolution": null,
  "supersededBy": null,
  "actions": {"canReply": true},
  "reasonCodes": []
}
```

候选最多20条、issueId去重，可为空，表示建议而非全部目标；读取时核当前关联及读权，不凭隐藏候选覆盖用户改写。答复可以明确其他当前可读关联Issue；与根请求冲突或仍不明确时继续澄清。

| state | 事实与页面行为 | canReply／reasonCodes |
| --- | --- | --- |
| open | 问题等待答复；原actor可引用当前revision。 | 原actor true／[]；其他可读者false／[NOT_ORIGINAL_REQUESTER]。 |
| answer_saved | 唯一答案已保存，answerMessageId非null；等待处理，不显示已确定目标。 | false／[ANSWER_SAVED]。 |
| resolved | 答复已完成目标解释，answerMessageId与resolution非null。 | false／[RESOLVED]。 |
| superseded | 老问题被替代，supersededBy非null；保留已有答案或null，显式进入后继。 | false／[SUPERSEDED]。 |
| invalidated | 业务来源已失效，保留已有答案或null；不重新开启。 | false／[INVALIDATED]。 |

reasonCodes为string[]，本批仅上表值；状态原因优先于非原actor。权限未知整笔503。supersededBy仅superseded非null；resolution仅resolved非null，形状 `{id:string,outcome:"issue_target"|"no_work",issueId:string|null}`：issue_target必须有效ID，no_work必须null，只表示本条无需工作，不取消旧任务。

尚无问题的明确单项请求不被强迫走澄清；其受控处理与本接口的澄清快照分开。无浏览器决定目标接口；后端受控动作只保存目标解释与待核验事实，不直接改计划／派工。原消息、问题、答案链不被新问题覆盖。源失效与核权未知是不同事实。

## 5. 错误与页面恢复

错误形状复用[创建契约](issue-page-create-api-contract.md)，字段错误不泄露隐藏ID／名称。以下均沿当前身份及内容核权优先规则：

| HTTP／code | 页面处理 |
| --- | --- |
| 400 INVALID_JSON／INVALID_IDEMPOTENCY_KEY／INVALID_CURSOR | 修正格式或刷新游标，不把坏请求当成功。 |
| 401 AUTHENTICATION_REQUIRED | 清主体敏感缓存和订阅；同主体登录后核原操作。 |
| 403 MESSAGE_SEND_NOT_ALLOWED | 可读但不可发／不是原答复者；不自动去引用绕过。 |
| 404 RESOURCE_NOT_FOUND | 不存在、跨项目或隐藏；清受影响敏感缓存，不泄露存在性。 |
| 404 MESSAGE_SUBMISSION_NOT_FOUND | 仅原操作GET；结果仍未知，不自动换键。 |
| 409 IDEMPOTENCY_CONFLICT | 保留原操作和编辑内容，不能覆盖原提交。 |
| 409 CLARIFICATION_REVISION_CONFLICT | 当前问题非open或版本陈旧；本次新消息整笔未存，保留有权的输入并GET问题。 |
| 409 CURSOR_EXPIRED／CONVERSATION_CONTEXT_CHANGED | 重新核权后显式从第一页刷新，不混旧分页。 |
| 410 MESSAGE_SUBMISSION_RESULT_REMOVED | 原结果已清理，禁止复活旧键。 |
| 413 REQUEST_TOO_LARGE／422 VALIDATION_FAILED | 指示格式或字段限制，已发未知操作仍单独核查。 |
| 429 RATE_LIMITED | 遵守Retry-After；不增加操作身份。 |
| 503 AUTHORIZATION_UNCONFIRMED | 权限待核，不展示旧敏感缓存，不报确定无权。 |
| 503 RESULT_UNCONFIRMED／连接中断 | 原提交结果未知，按原键查询；不报发送失败后自动新发。 |

浏览器恢复路由为 `/projects/{projectId}/conversations/{conversationId}/message-submissions/{submissionId}`，只核原提交。它是页面地址，不能直接用回执links.operation的API地址替代。

首次发送前sessionStorage按actor／project／conversation／submissionId保存确切content+replyTo及操作身份；localStorage仅主体隔离的最小身份／时间索引。URL和长期索引不含正文、候选名称、凭据。存储不可用时先给可复制恢复链接；确切输入丢失只查，不根据气泡重构发送。用户停止等待／离开页面不是撤销已发消息。

切主体、会话或问题建立请求代次，晚到结果保留原操作归属，不能填入新输入框或新右栏。409不自动更新expectedRevision、移除replyTo或换新键；用户在获知当前状态后再明确答当前问题。权限unknown暂停敏感展示；注销／401／明确失权按原约定清理敏感正文及派生缓存。

## 6. 设计验证与实施依赖

页面验收待执行：双窗口答复一胜一409；成功重放不重复气泡；改写候选不沿旧选择；superseded旧问题不可复活；网络未知／404后晚提交仍查原键；410不能再建；非原actor／跨项目／失权／权限未知不泄露；分页水位及内容范围变化不漏混消息；切上下文晚到结果不串目标。后端MC系列用例见其唯一内部设计。

本轮仅核对文档字段、链接、示例及双方一致性。原型已有内存演示不覆盖这些真实验收；新增恢复／冲突／草稿保护的具体控件须另展示给用户讨论。完整B05、Manager适配和后续副作用协议未完成前，不接入依赖它们的执行处理器。
