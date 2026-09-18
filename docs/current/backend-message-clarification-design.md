---
status: accepted
date: 2026-09-10
design_revision: RM-UI-MESSAGE-TARGET-r3
owner: backend
---

> **物理承载替代说明（2026-09-15 更新）：** 本文的逻辑契约与候选状态不变。物理表承载自 2026-09-15 起以 [Go 版数据库重构方案](../RepoMesh_Go版数据库重构方案.html) 的 44 张目标表为准，对应接口见 [API 设计](api-design.md)；此前的 B05-B11 物理合表已归档到 [历史目录](../archive/2026-09-15-api-database-catalog/README.md)。

# 会话消息与最小目标澄清链路

本稿对应 RM-UI-MESSAGE-TARGET r3，第3／3轮，双方已确认r2方案及r3五项定点修订，技术语义收口且最终落盘已互核；accepted仅指该局部设计采用。用户要求“但是具体实现得和后端聊一下”；本稿只做文档，不实施消息接口、数据库或Manager工具，也不将这句话视为新控件采用。页面唯一字段源§1—6与本文一致，无遗漏实质分歧，不因此宣称实现或运行通过。

后端独占本文，负责内部身份、状态事实、约束、事务、可信工具上下文和恢复。页面独占[消息目标专题](conversation-message-target-design.md)及[消息澄清浏览器契约](conversation-message-clarification-api-contract.md)；本文的逻辑字段不是第二套REST Schema。提案及修订原文见[当前通信日志](../archive/2026-09-12-development-preparation/docs/current/design-communication-backend-2026-09-10.md)，最终字段以页面r3为准。已有[创建契约v1](issue-page-create-api-contract.md)、[首批浏览器契约r3](first-batch-browser-api-contract.md)保持。

## 1. 范围和实施门槛

本批覆盖已有业务会话中的纯文本消息保存、单条既有Issue请求的直接目标判断与目标澄清、原提交核查及只读状态查询。答复通过同一个普通消息提交用例，额外携带问题引用及预期修订；不建立候选按钮专用执行接口，不接受权威selectedIssueId，不解析聊天Markdown、编号按钮、系统提示文本作为控制命令。

它不完成新建空会话／首条消息的完整接入、消息附件／编辑／删除、流式正文、会话SSE、跨多Issue批量命令、已执行工作的更正或补救、完整创建MCP、实际Matrix投递和运行会话恢复、计划执行协议。本批为B05的一段内部设计，不标B05—B08完成。

普通消息已经明确目标时不强行提问；同一目标判断动作按可信上下文分为root_message与clarification_answer两条路径，见§6。有开放问题不能使用root_message绕过。两者均不执行工作；后续工作接续与完整Manager理解流程仍不在本稿中完成。

链路分为三个可检验边界：

1. 本地保存：消息、答复关联、问题修订和持久待办同事务；提交成功只证明已保存。
2. 受控理解：实际Manager在受限上下文中提出问题或提交目标判断，经后端核验后记录事实；普通聊天回复不改变控制状态。
3. 后续工作：目标判断只形成可审计的处理依据。计划变更／创建／派工仍经各自业务命令核验，缺处理器时保持未接入，不假装已启动。

选择单问题单有效答复，便于恢复且不强迫每条消息预选Issue。代价是并发第二份答复不能自动合并，须显示冲突并保留用户输入；自然语言理解仍可能误判，结构化身份不消除该风险。

## 2. 可信身份与逻辑工作请求

### 2.1 用户消息入口

actorUserId来自认证中间件，projectId从当前可访问会话查得。客户端不能提交作者、角色、Manager身份或自称已获准。用户提交的UUID操作键仅用于本次消息保存幂等；messageId由服务端生成，消息作者、会话和原文保存后不可改写。

操作作用域为(projectId, conversationId, actorUserId, entry=conversation_message, submissionId)，与Issue创建、项目操作及Manager控制操作分开。第一次发送前冻结同键确切输入，包括可选replyTo和修订；不能通过换修订、去掉引用或换会话绕过未知结果。

replyTo内的clarificationId就是本文问题身份；不另造questionId。原消息、原操作者、逻辑请求、会话均由该身份查库得到，不能信任客户端复制的关联。本批只允许原工作请求操作者答复；owner首批下通常是同一人，仍显式检查，不提前开放代答。

### 2.2 逻辑请求的服务端分配

可信消息入口为每个已保存普通消息登记一个稳定的处理入口，唯一(messageId, handler=interpret_message)。这不是Issue创建操作，也不保证消息包含工作。队列重试复用入口，不能每次模型调用生成一个新请求。

本批支持的单项请求由可信协调器在该入口首次处理时分配logicalRequestId，保存根sourceMessageId及sourceRevision、actor、project、conversation；唯一(processingEntryId, sourceUnit=whole_message_single_request)。该槽位只负责本批单项目标解释。普通讨论可结束为非工作；已明确含多个独立命令的消息标需要拆分，不能把全段强行选到一个Issue，不能因此执行多个Issue。

这不定义“一消息永远只能一项工作”。完整B05须持久化多来源句段清单，并由可信服务端给每个来源单元分配身份；模型提出的分段仍须校验原文位置、原消息修订及清单CAS。未完成该协议前，本槽位不得被多个创建工具当作各自的新建依据。多项输入可以保存和讨论，但本批受控确定目标动作须拒绝跨多Issue结果，保留继续澄清／拆分的责任。

澄清答复带有replyTo时，只加入原logicalRequestId的依据链，不另开独立工作槽位。明确移除引用后发送属于新的普通消息提交，不能同时消耗旧问题；移除引用不证明文本就是闲聊，仍按普通消息解释，也不能自动改绑、撤销或重放旧工作。

### 2.3 Manager受控上下文

只有可信运行适配器认证为本项目Manager且绑定实际instance／session／conversation的调用可提出问题或提交判断。模型不自报role、actor或任意conversationId。后端为每个处理步骤签发短期、不可转移的调用授权引用，服务端记录：

| 内部绑定 | 用途 |
| --- | --- |
| authenticatedService / managerSessionEpoch | 标识真实服务调用及当前Manager会话代次；旧session重连不能恢复旧写权限。 |
| project / conversation / actor / logicalRequest | 固定业务作用域；从服务端持久源取得。 |
| sourceMessage / inputKind / acceptedAnswer / sourceVersions | 固定本次模型实际可读输入；inputKind由服务端据请求状态绑定，root_message不伪造答案，clarification_answer固定唯一有效答复。 |
| processingWorkId / claimGeneration / leaseUntil | 限定当前处理领取者；代次及租约由数据库时钟判断。 |
| expectedRequestRevision / expectedQuestionRevision | 防旧问题、旧答复的判断覆盖新状态。 |
| commandSlotId / allowedActions / expiresAt | 固定一次控制决策槽位，限制动作种类，承接稳定操作身份。 |

授权引用作为传输凭据由适配器附加，不能放进公开消息、浏览器URL或要求模型填写。后端从数据库重建其约束，不能只验签而不查撤销／代次。传输认证／凭据实际签发方式仍依B01/B06接入；未完成时工具默认不可用，而非接受测试role字符串上线。

commandSlotId由协调器在领取业务步骤时持久分配；同一决策步骤恢复沿用同一槽位，换领取代次不换逻辑操作。槽位允许的互斥动作集固定；一个槽位只能提交一个规范化输入和结果。旧领取者无权再写新结果，但已提交控制结果可在当前可信作用域核权后查询，无须重新执行判断。

## 3. 最小逻辑记录和唯一约束

| 记录 | 必要内容 | 约束 |
| --- | --- | --- |
| ConversationMessage | messageId、project/conversation、authorKind、actor或可信服务来源、正文、提交时间、committedSequence、replyTo引用 | 消息原文不可变；作者由入口绑定。会话内序号唯一，不按客户端时间排序。 |
| MessageSubmission | 上述作用域、输入版本／规范化输入／摘要、messageId、当次回执、removed标志 | 唯一作用域；只有完整提交或无正文tombstone可见。回执不可随当前问题状态改写。 |
| MessageProcessingEntry | messageId、处理种类、处理责任、传输观察引用 | 每真实消息一条对应处理入口；来源卡片不伪装用户消息；答复处理不重复分配新工作请求。 |
| LogicalWorkRequest | 根来源、可信actor和会话、sourceUnit、revision、解释状态、currentQuestionId、resolutionId | 单项解释槽位唯一；原来源不可重绑；同请求最多一个当前问题和一个最终目标解释。 |
| Clarification | clarificationId、logicalRequestId、questionMessageId、原来源、revision、state、候选身份／公开问题、supersededBy、invalidationReason | ID由后端生成，永久不复用；同请求同一时刻至多一个open或answer_saved问题。 |
| ClarificationAnswer | clarificationId、answerMessageId、answeredRevision、提交操作、actor | clarificationId唯一、answerMessageId唯一；仅一个成功接收答复，不等于已解决。 |
| TargetResolution | resolutionId、logicalRequestId、inputKind、依据消息／可选问题／可选答复版本、outcome、可选issueId、requestRevision、controlOperationId | logicalRequestId唯一最终解释；有目标时固定单一同项目关联Issue，不可原地换目标；直接分支没有虚构问题或答案。 |
| ControlOperation | commandSlotId、动作、schema/canonical版本、输入、不可变结果、removed | commandSlotId唯一；不同动作／不同输入不能覆盖同槽位成功。 |
| DurableWork / Claim | causeId、kind、targetId、权限/上下文引用、状态、代次、租约 | 沿首批持久待办约束，唯一(causeId, kind, targetId)。 |
| DeliveryObservation | messageId、稳定外部操作、实际room/session归属、发送/接收证据 | 观察与本地保存、解释结果分别记录；外部适配未接入不得填成功。 |

所有跨对象外键带projectId/conversationId等同域约束。问题和原消息引用必须落在同一逻辑请求／会话，答复必须来自同一操作者。本批问题卡片是一条可信服务发布的会话消息，发布与Clarification同事务；用户／Manager任意Markdown不能伪造questionId及开放状态。

最终数据库约束至少包括：ClarificationAnswer的clarificationId唯一、TargetResolution的logicalRequestId唯一、Clarification按logicalRequestId在state为open或answer_saved时的部分唯一索引，以及问题／答案／源消息的复合归属外键。currentQuestionId与开放问题的一致性在同一持锁事务维护；不能只靠进程内先查询判断唯一。请求resolved后不再接受新问题，后继链不可回指自己或旧祖先。

LogicalWorkRequest内部解释状态为pending、awaiting_clarification、answer_pending、resolved、needs_decomposition、invalidated。它不是Issue生命周期。resolved的outcome为issue_target或no_work；no_work表示本次解释为普通讨论，不等于取消原先已发生工作。needs_decomposition不授权跨多Issue执行，保留明确原因和后续责任。

## 4. 问题状态与双答复

| 当前事实 | 允许转移 | 同事务效果 |
| --- | --- | --- |
| 请求pending，尚无任何问题 | 直接判断→请求resolved | 固定root证据，生成TargetResolution、公开处理记录及对应待核验责任；不创建问题或答案，不改计划。 |
| 尚无问题，单项请求pending | 提问→open | 后端生成问题ID、公开问题消息及候选引用，关联原请求，增加请求修订。 |
| open | 用户答复→answer_saved | 保存答复、Answer唯一关联、问题和请求升修订、登记按序投递／解释责任。 |
| open | 替换问题→superseded | 老问题保留，新ID问题进入open，supersededBy连接；取消尚未发送的旧待办，已可能发送者继续核查。 |
| answer_saved | 明确目标或非工作→resolved | 保存不可变TargetResolution和公开处理记录；issue_target另登记后续核验待办，不直接改计划。 |
| answer_saved | 答复含糊／矛盾→superseded | 同请求提出新问题、新ID，保留原问题及答复；不重开旧问题，不退回open复用旧revision。 |
| open / answer_saved | 业务源被撤销、删除或不再可用→invalidated | 留非敏感失效原因，撤当前问题指针，阻止旧控制结果和新答复消耗。 |
| resolved / superseded / invalidated | 无原地重新开放 | 同键历史结果可核查；纠正另留记录／新逻辑操作，不能改写旧执行事实。 |

权限未知是本次读取／处理无法确认，放入待办blocked或retry_wait并注明原因，不直接把问题判成业务失效。权限已知撤销先阻止当前读写／投递，不能泄露问题；访问恢复也不复活已被业务明确invalidated的问题。候选被删除／不再关联但原请求仍可讨论时，经当前上下文重核后替换提问；不会自动选择剩余唯一候选。

两个不同操作键同时答同一open问题：按同一请求／问题行锁串行，首个有效事务保存答复并升修订；另一个409问题修订冲突，整笔消息不保存、不入队，客户端保留文本。即使两个正文相同也不按文本合并。只有同操作键同输入重放返回同一成功；同键不同正文409幂等冲突。

用户收到冲突后读取当前问题：可能已答复、被替代或失效。页面不得自动更新expectedRevision后重发，也不得自动删除replyTo把原输入当普通消息发送。用户明确另发补充必须使用新消息操作；不能借“补充”覆盖既有答复。已保存答复尚未处理时的并发更正属于后续更正协议，本批只保留追加讨论能力。

## 5. 事务和幂等恢复

### 5.1 消息提交事务

复用[首批持久化协议](backend-first-batch-persistence.md)的主库、短事务、外部核权观察、精确输入比较和唯一提交纪律。本批纯文本建议最多20,000 Unicode标量，正文非空白；体上限、JSON严格解析和错误外形沿首批规则，由页面契约最终定稿。正文不trim、不按Unicode归一化或换行归一化后判同；有无replyTo、问题ID和预期修订全部纳入比较。

1. 认证和请求安全检查，取得可信actor；初步验证信封可解析。从主库查原作用域操作，当前可读的既有成功优先于问题当前修订／状态，删除占位优先410。有效相同输入200原回执，异输入409；重放不扩大内容范围、增加消息、追加投递或重解目标。
2. 新操作验证会话、原问题／请求和actor。事务外核完整会话内容范围、本次消息保存将引入的项目全仓范围及必要目标范围；权限未知503，不保存半条答复。模型／runtime尚未Ready不阻止有资格的本地文本保存。
3. 固定锁序：actor本地访问版本→project→conversation→logicalRequest→clarification→操作槽位／其他结果；有多个同类对象时按ID排序。所有问题替换、接收、解决、失效和正文清理路径遵守同序。先取得输入范围的有效观察，不持锁调用GitHub或模型。
4. 锁后再查成功操作，重新验证本地accessEpoch、范围版本／项目范围、外部观察仍有效。新replyTo再比问题revision、state=open、原操作者及currentQuestionId。相同逻辑问题已被回答／替换／失效统一409修订冲突；不暴露另一个用户内容。无资格隐藏404、可披露写拒绝403。
5. 插入唯一MessageSubmission占位；创建messageId，在会话锁下分配提交序号；保存正文、来源、规范化输入／回执，扩展内容范围。replyTo还保存唯一Answer并把问题改answer_saved、请求改answer_pending，各升修订。
6. 同事务保存MessageProcessingEntry和按序投递责任。答复不另投“按钮命令”，不直接创建TargetResolution。提交后才返回201；通知仅唤醒，丢失通知由扫描恢复。
7. 已知事务回滚可原键原输入重试；COMMIT回执丢失、唯一竞争超时、主库不可读等返回503 RESULT_UNCONFIRMED或连接未知，按原submissionId核查。操作GET返回404仍不证明旧POST不会提交。

新操作的权限核验覆盖保存时完整项目范围，沿现行自由消息保守策略；一条答复因此可能收紧会话整体读权。不能因为答案只提#2，就仅核#2而绕过旧会话材料或未标注自由文本的范围。授权观察与外部权限变化不是分布式原子提交，后续投递／处理还须再核。

### 5.2 控制动作事务

服务认证→操作原结果核查→当前调用授权／代次核验→外部权限预核→相同固定锁序→重查原结果→检查请求／问题／答复版本→一次性提交控制结果、问题变更、公开服务消息及相应待办。公开消息内容与候选也先登记并核内容范围。

内部控制占位与结果必须同事务，不向模型暴露“已接收即可能生效”的独立成功。已有成功返回原结果，不根据最新问题状态重做判断；旧代次可经独立查询工具核原结果，但无提交过的槽位只能由当前有效授权提交。槽位身份与租约代次分离，不能靠换代次制造另一目标。

所有回写核claimGeneration、owner及数据库时钟下未过期租约，且核ManagerSessionEpoch、request/question revision和acceptedAnswer身份。任意不符只返回内部STALE_PROCESSING_CONTEXT，不追加公开“已解决”记录。租约到期不等于旧进程终止；这里只保证旧调用无法写入RepoMesh正式控制事实，不声称外部系统也具备围栏能力。

### 5.3 回执与当前状态分离

消息成功回执只冻结submissionId、messageId、conversationId、提交时间／序号和当次答复关联，不携带会变化的“当前问题已解决”状态。answeredRevision明确保存此次成功消耗的expectedRevision，即提交前问题版本，不是answer_saved后的新版本。无replyTo请求省略该字段，回执固定null；有replyTo按原输入冻结关联。操作GET始终返回相同提交事实；当前问题和消息处理观察另GET。当前问题从answer_saved走到resolved后重放POST仍返回原消息，不再次消费问题。ID／修订均为不透明字符串，sequence为十进制正整数字符串，时间为UTC；不得用JavaScript Number比较大序号。

操作键与控制槽位在项目保留期内不复用。正文删除清理确切输入、摘要、派生公开文本和可还原正文的副本，保留无正文身份／权限范围和removed占位，当前有权者GET／重放410。对象已不可披露时404优先于410；注销后401不替用户取消已提交消息。

本批不新增浏览器删除接口。删除／失效事务须提升请求修订、撤销未生效控制上下文，阻止未来本地执行接续；已可能发生外部动作的核查责任保留，不把数据库清理当作外部取消成功。

## 6. Manager动作和正文改写

以下是后端内部受控动作名建议，不是已注册MCP工具，不复用创建工具权限：

| 动作 | 模型允许提供的业务参数 | 后端约束及结果 |
| --- | --- | --- |
| propose_clarification | 公开问题文本、当前上下文内候选Issue引用、可核对的来源句段引用 | source/request/actor由授权绑定；仅当前单项请求、无其他当前问题时生成ID并原子发布问题。候选仅为辅助，可空，不证明候选集穷尽。 |
| decide_message_target | outcome=issue_target或no_work、单一issueId（前者必需）、可见用户消息／答复的精确证据引用 | 服务端绑定root_message或clarification_answer上下文；按下文分支核版本、问题、来源、可读关联Issue。成功持久TargetResolution及处理记录，不能包含计划patch、预算授权或派工指令。 |
| replace_clarification | 新公开问题文本、可读候选引用、reason=ambiguous或conflicting或context_changed | 当前open／answer_saved问题须CAS；新ID与老superseded关系同事务，不修改老用户文本。 |
| get_message_control_result | 可信适配器承接的原commandSlot引用 | 只核查原动作结果，当前读权；404表示尚无结果，不能授权换槽位重做。 |

最小工具参数类型：问题文本非空且不超过2,000 Unicode标量；候选去重最多20个，省略等价空集合、按集合比较，不承诺全部候选；issueId只接受稳定服务端ID。证据最多20个，引用实际上下文中不可变消息ID／修订及Unicode标量半开区间[start,end)，要求0≤start<end≤正文长度，摘录由服务端截取而非相信模型重写。root_message至少引用根消息，clarification_answer至少引用当前acceptedAnswer，原请求和问题依据由服务端自动附加。禁止未知字段、重复JSON键、跨作用域引用、模型自报author、分支或授权值。

双分支的服务端准入条件如下，均在§5.2相同事务及请求行锁下核验：

- root_message：请求是pending、尚无问题历史、currentQuestionId和resolutionId均为空，授权绑定root原消息及修订。允许明确的单项既有Issue目标或no_work；含糊则提问，不创建伪答案。
- clarification_answer：当前问题是answer_saved且属于该请求，授权内acceptedAnswer等于该问题唯一Answer；原消息／问题／答案身份和版本全部匹配。含糊／冲突则替换问题，不走direct分支。
- 请求存在open问题、已resolved／invalidated／needs_decomposition，或问题链已变化，不能接受root_message判断。分支是授权属性，不是浏览器或模型可自由选择的参数。
- 同一pending请求的提问与直接判断竞争，先提交者升requestRevision并固定结果；后者旧授权失效。即使错误地签发了不同槽位，request行CAS和resolution唯一约束仍阻止两个决定并存；按当前事实重新签发合法步骤，不覆盖首个决定。

直接分支不提供浏览器改目标入口，也不伪造clarification资源；其公开处理记录的完整工作投影仍随B05补齐，本批五个端点不因此被称作完整工作结果查询。

issue_target必需单一issueId，no_work禁止issueId；工具原结果查询不接受重新指定的目标。控制输入比较保留文本／证据顺序和明确版本，候选按集合排序但重复先拒绝，省略候选与空集合等价；各动作的比较器版本随槽位结果保存，不能升级后改变旧键相等判定。相同槽位更换动作也属输入冲突。

来源句段只证明“这段文字真实存在并供本次处理读取”，不能机械证明其足以推出目标。服务端可拒绝空证据、越界、候选／正文显式身份不一致等可检测矛盾；无可靠语义结论时必须继续澄清，不能使用置信度阈值、最近页面或候选按钮隐藏值兜底。如何识别所有自然语言矛盾仍需真实模型评估，不能冒充确定性服务端规则已解决理解问题。

候选文本被用户改写时，以保存后的实际正文和原引用链为依据重新解释；没有“原样字符串才合法”的生产规则。预置原型的原样匹配只用于模拟。答复明确指向另一条当前可读且关联的Issue，可以提交该稳定目标，即使不在旧候选集合中；若它与原请求含义冲突，先继续澄清，不能机械地让旧候选或最新一句覆盖全部上下文。

若没有明确工作意图，no_work结束本条目标解释，不能取消其他工作。若仍含糊或包含多个Issue命令，replace_clarification／需要拆分流程保留旧依据，本批不提交跨多Issue目标。目标issue_target提交后不可换绑；后续用户纠正只能走新的更正／补救协议，未完成前不能自动撤回或重试另一Issue。

## 7. 查询与浏览器契约要求

页面负责最终路径和字段。[消息澄清浏览器契约](conversation-message-clarification-api-contract.md)维护提交／原提交查询／消息分页／单消息读取／问题读取五个入口，全部限同一个已有会话；原建议及r3修订保存在本主题回执原文。没有现成普通消息业务API可直接扩字段，实现时应共用新消息保存用例，而非误以为已有路由已实现。

- 保存响应为已提交消息事实；当前问题读取提供state、revision、原消息／答复引用、可读候选、已解决目标和被替代后继，配套reply资格，不混入运行Ready。无目标用null，不以右栏选择补齐。
- 澄清快照的answer_saved／resolved必须有answerMessageId，superseded／invalidated保留曾有答案或null；resolution仅resolved时非null，outcome=no_work的issueId必须null。此约束限问题读取，不为直接判断制造假问题。
- reasonCodes只解释不可回复，按页面r3使用NOT_ORIGINAL_REQUESTER、ANSWER_SAVED、RESOLVED、SUPERSEDED、INVALIDATED；状态原因优先于非原actor，有回复资格为空数组，未知整笔503。可读但不是原actor试答为403 MESSAGE_SEND_NOT_ALLOWED，不冒充不存在。
- 候选和目标按当前完整内容范围、会话关联核权；当前快照若包含无法确认权限的材料返回503，不提供缓存身份或把未知改成空候选。已撤权对象不可披露404，原会话仍可读时可给无敏感内容的失效原因；不得返回受限Issue计数／名称。
- 消息分页按会话committedSequence升序；序号在会话行锁下分配并持锁到提交，所有消息写路径共同遵守。第一页固定throughSequence高水位，游标绑定actor／会话／权限范围版本／高水位／位置，不使用全局自增最大值承诺提交顺序。范围变化须重新核权并重新开始分页，分页不是跨页历史快照。
- 当前消息正文不可变，但处理观察随读取更新，分页高水位只界定消息范围，不冻结处理状态。新消息从新一轮刷新获取，首批不声明会话实时订阅；来源问题／答复可由单消息GET定位，不要求遍历全部历史。
- 消息GET与列表使用同一消息投影，无后页nextCursor为null。分页上下文变化仅在重新确认当前读权之后返回409 CONVERSATION_CONTEXT_CHANGED，页面显式从首开始；401／隐藏404／权限未知503优先，不能用范围变化码探测隐藏内容。
- 401清主体缓存和订阅、重新登录同主体后只核原操作；404清受影响敏感内容但不把提交判作失败；409保留草稿并补查问题；410不复用键；503保留未知／待核，不自动新建操作。
- 浏览器发送前sessionStorage按actor／project／conversation／submissionId保存确切输入，localStorage仅最小操作索引；正文、候选名、凭据不进URL或长期索引。空间不可用先提供可复制的原操作恢复入口。页面须定义对应浏览器恢复路由，不能拿API链接当用户恢复页；输入丢失只查原操作，不重构正文。
- 切会话／主体／问题建立请求代次，旧结果只能留在原操作记录，不能回填新输入框或新右栏。问题已被替代时，原问题仍可查看其历史，但新答复只能经显式当前问题引用提交。

移除replyTo之后的普通讨论仍经过自然语言解释，不是权限或执行绕过入口。候选点击时已有未发送草稿怎样保留属于页面交互；不得为填候选静默发送或消费原草稿。具体交互仍须先展示用户讨论。

## 8. 重启、投递与后续工作

消息提交与投递责任同事务。协调器扫描待办，运行未就绪时只保留已保存／待接入事实；按现行明确提交后自动准备方向接续，不能以模型不可用拒绝已经可安全保存的答复。

同会话向Manager投递采用服务端提交序号，不允许答复走旁路插队。前序投递可能发生但结果未知时先核其稳定外部操作，后续消息等待该传输结果；等待某条请求语义澄清则不占住整个会话投递队列，后续独立讨论／其他Issue工作可继续。这两种等待必须分别记录。

发送前登记稳定外部操作及“可能发送”，不把claimGeneration当外部消息ID。上游没有可靠幂等／查询证据时停在reconciling，不自动换ID重发；接收证据、模型实际收到的输入与授权引用绑定仍需B06适配。传输成功不等于Manager收到，普通“收到”聊天不作为控制回执。

重启时：

1. 消息或问题事务未提交：数据库回滚，无半条消息／问题；原键重试遵守当前修订。
2. 提交已完成、唤醒丢失：扫描原workId，不新增消息或问题。
3. 模型处理后尚未持久控制结果：新领取者使用原commandSlot与输入依据；旧领取者写入因代次失效被拒绝。若模型提交已成功但回执丢失，先查原ControlOperation。
4. 已resolved但后续待办未运行：扫描TargetResolution产生的唯一待办；不重复确定目标。处理器缺失则blocked/INTEGRATION_NOT_AVAILABLE，不写计划成功。
5. 解释时权限未知或配置不可用：保留answer_saved及原来源，待核条件恢复再用原请求核验；不重新开放问题让用户重复回答，也不静默换模型版本。
6. 旧问题被替代／失效：旧任务失去控制写权限；旧外部调用若可能已发仍核查原操作，只保存必要审计，不发布过期“目标已确定”。

后续计划／执行命令必须引用resolutionId、logicalRequestId和依据版本，核当前actor、目标、状态／范围／计划、预算、容量及相关修订；resolution不是授权令牌。对应业务操作身份由后端分配并在各自命令内最终去重，不把消息幂等成功当外部exactly-once。更正或源失效与计划受理竞争的CAS／外部补救须由完整B05/B07完成，在此之前不得启用实际副作用处理器。

## 9. 错误分类与原操作核查

以下为双方r3确认的错误语义映射，最终字段以页面唯一浏览器契约为准；控制工具以类型化业务错误承接，不把HTTP状态当MCP传输状态。

| HTTP／内部类别 | 业务码 | 语义和恢复 |
| --- | --- | --- |
| 401 | AUTHENTICATION_REQUIRED | 当前会话无效；不取消已有提交，同主体恢复后查原操作。 |
| 403 | MESSAGE_SEND_NOT_ALLOWED | 会话可披露，但无发言／答复动作资格；不泄露受限原消息或Issue。 |
| 404 | RESOURCE_NOT_FOUND | 资源不存在、跨项目或权限隐藏；不能拿隐藏结果判断另一操作未提交。 |
| 原提交GET 404 | MESSAGE_SUBMISSION_NOT_FOUND | 当前尚无可见提交，可能有在途POST；继续原键核查。 |
| 409 | IDEMPOTENCY_CONFLICT | 同键输入不同，保留原操作；不覆盖正文／replyTo。 |
| 409 | CLARIFICATION_REVISION_CONFLICT | 新答复的问题已答复／替换／失效或修订不符；整笔不保存，保留输入后补查。 |
| 410 | MESSAGE_SUBMISSION_RESULT_REMOVED | 同域操作占位仍在，正文结果已清理；不可复用旧键。 |
| 400 / 422 | INVALID_JSON / INVALID_IDEMPOTENCY_KEY / VALIDATION_FAILED | 信封解析／键格式／允许字段或值不合法；合法结构解析后，历史重放不受新问题状态影响。 |
| 413 / 429 | REQUEST_TOO_LARGE / RATE_LIMITED | 请求体超限／频率受限，沿通用限制和重试提示，不换键盲发。 |
| 409分页 | CONVERSATION_CONTEXT_CHANGED | 当前读权确认后发现范围上下文变化，显式从首刷新；无权／未知先按401/404/503。 |
| 503 | AUTHORIZATION_UNCONFIRMED | 当前内容读权未知；不保存、不披露未知材料。 |
| 503 | RESULT_UNCONFIRMED | 提交或主库核查不确定；查原提交，不新建操作。 |
| 控制调用拒绝 | STALE_PROCESSING_CONTEXT / SOURCE_CONTEXT_MISMATCH / TARGET_NOT_RESOLVABLE | 代次／依据／目标不满足，不保存控制成功；当前有效处理器重读或继续澄清。 |

错误响应不得附带未核权的当前问题正文、候选名或另一用户的答复。问题不存在／不可读先隐藏404，只有可读同域问题才给修订冲突。原操作已提交且仍可读时，即使问题后来被替代，也优先200原消息回执；清理占位410优先输入比较。429及重试提示沿首批通用规则。

## 10. 待执行验收

所有用例均为设计，尚未执行，不用HTML脚本替代事务、真实权限或上游故障测试。

| 编号 | 触发与预期证据 |
| --- | --- |
| MC01 | 同键同输入并发提交：只有一个message／Answer／投递责任，赢家201、重放200，原回执相同。 |
| MC02 | 同键改正文或改replyTo：409幂等冲突，不覆盖原记录，不换问题。 |
| MC03 | 不同键双答同一revision：仅一条答复提交；另一整笔409且客户端保留原输入，无孤立消息。 |
| MC04 | 提问／替换与答复竞争：锁及revision给出一个串行结果；晚到答复不消费后继问题。 |
| MC05 | 答复事务在消息、Answer、队列任一写入后崩溃：无部分提交；提交后丢响应则原GET能核同一消息。 |
| MC06 | 原GET先404、旧POST稍后提交：不换键，最终仍一条消息；404不产生失败终态。 |
| MC07 | 问题已resolved或superseded后同键重放：200原提交；不同键旧revision答复409，不再解目标。 |
| MC08 | 普通Markdown伪造问题卡、角色或“已解决”：只保存用户文本，不能创建Clarification／Resolution。 |
| MC09 | 候选#2被改写成#1或自由文本：按真实保存正文重新解释；不沿隐藏selectedIssueId定目标；无法明确则新问题保留旧链。 |
| MC10 | 右栏查看#1，正文明确#2：受控结果属#2且来源版本固定；导航变化不产生工作命令。 |
| MC11 | 并发旧Manager session／租约过期领取者回写：STALE_PROCESSING_CONTEXT，无过期公开处理记录或重复后续待办。 |
| MC12 | 控制结果提交后响应丢失：原槽位查询回同一question／resolution；不同输入不能占用已成功槽位。 |
| MC13 | 问题候选失权／核权未知：不泄露缓存名称；未知503或内部待核，已知拒绝不让旧答案授权执行。 |
| MC14 | 新项目仓库加入与自由答复保存竞争：会话范围按保存提交版本扩展，不能旧范围核权后发布新范围正文。 |
| MC15 | 解析含糊或多Issue命令：不提交单一猜测目标；保留需要澄清／拆分的状态及责任，不取消其他Issue工作。 |
| MC16 | 前序投递未知：答复不能旁路超越；纯语义等待则不阻塞后续已保存消息投递。 |
| MC17 | 进程重启／丢通知／模型离线：恢复原work和输入，answer_saved不退回open，不重复让用户答复。 |
| MC18 | 清理与同键重放竞争：按锁串行，只见完整原结果或410；不可从摘要／派生记录恢复已删正文。 |
| MC19 | 会话分页与并发提交：不越过未提交较早序号；高水位外新消息需刷新，源／答案可单独定位。 |
| MC20 | resolved后的后续处理器未接入：唯一待办blocked并有原因，不显示计划变更／派工成功；任何外部副作用测试等待B05/B06/B07收口。 |
| MC21 | 删除replyTo明确另发普通消息：不消费旧问题、不覆盖旧Answer；新文本仍需解释，不能用移除引用绕过执行核验。 |
| MC22 | 操作者／会话／问题／答案引用跨域，或证据区间越界：隐藏拒绝／类型化来源错误，无可利用的跨域引用或控制写入。 |
| MC23 | 明确pending请求直接判断与提问并发：只有一个请求修订成功；direct证据含root、无伪问题／答案，提问先提交后旧direct拒绝，两者都不改计划。 |
| MC24 | open问题尝试direct绕过、答案版判断引用非当前Answer：均拒绝；answeredRevision回执始终为提交前版本，当前问题新修订通过独立GET取得。 |

验收需要真实PostgreSQL并发故障注入、认证／内容范围接入、受控Manager模拟器及最终上游传输证据。完整源句段拆分、普通消息全协议、实际服务凭据、运行映射和执行更正仍是依赖。本文落盘完成只能证明方案可审查，不能证明实现或运行通过。
