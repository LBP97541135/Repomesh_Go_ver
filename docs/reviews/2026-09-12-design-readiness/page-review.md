# 页面语义与原型开发就绪度复核

审查日期：2026-09-12。审查分支：page_semantics。审查对象是本次工作区文件，未修改设计、原型或产品代码；本报告是唯一新增文件。ADR、完整 API 和后端专题由其他审查分支负责，本报告只对实际读取及交叉核对的范围作结论。

## 结论

页面已经具备清楚的核心实体和首批范围，可以据已采用部分安排开发：项目先保存、会话与独立 Issue 分离、手动创建、列表与最小概览、项目配置编辑。这不等于六包页面的真实闭环已经设计就绪。当前待审的认证、模型保存／测试／应用和配置来源决定尚未成为采用基线；其中模型保存的未知恢复存在一个确定的收敛缺口，必须在实现该路径前修正。当前串联原型还有一个同一固定模型快照显示两种测试观察的交互矛盾，需要在原型交接前修正。

完整产品尚不具备整体按图实现的条件。Manager 真实消息、可信目标、运行房间、计划与执行／验证／交付汇总等不属于本次收拢的最小页面范围，不能从原型中已有按钮和历史示意推导其协议已完成。本判断不要求先设计完整产品才能做首批。

采用范围以具体章节和替代说明为准：[完整评审包第 112 行](../../current/first-batch-complete-review.md)保留 F01—F04 的具体 UI 采用范围，同时明确新增 D/T/A/P/S/R 待用户；[页面交接第 3 行](../../archive/2026-09-12-development-preparation/docs/current/HANDOFF-PAGE-API-DESIGN.md)覆盖下方旧阶段记录。本文不将旧记录中的“未成稿”“0／3”单独列作冲突，也不将原型采用当作产品已实现。

## PAGE-01 · P1 · 含 Key 保存的请求未到达服务器时，原操作永久无法收敛

**类别：当前待审页面规则与 API／持久操作设计组合产生的闭环缺口。阻塞范围：首批自助模型配置保存及依赖它的新用户闭环；不阻塞纯读取和已采用的其他独立开发单元。**

触发条件：新建供应商或替换 Key，浏览器已调用发送并清除 Key；请求在服务器取得唯一操作槽位之前丢失，或者取得槽位前数据库暂时不可用。网络之后恢复。服务器从未持久化该 `saveId`，浏览器只能反复查询一个不存在的操作。

证据两侧：

- 页面要求：[model-key-save-design.md:15](../../current/model-key-save-design.md)规定未知时只能查询／稍后核查，未知期间相关配置不能再改；第 25、27 行要求有确定未提交依据才能修正并发起新操作。
- 浏览器协议：[model-settings-browser-api-draft.md:65](../../current/model-settings-browser-api-draft.md)、[第 131 行](../../current/model-settings-browser-api-draft.md)规定保存发出后清除 Key 内存，只查询原 `saveId`，即使用户记得 Key 也不能换键重发。[第 75 行](../../current/model-settings-browser-api-draft.md)规定查询 404 一直保持未知，只有取得唯一责任并持久终结的拒绝记录才能证明未保存。
- 后端：[backend-model-operations-draft.md:20](../../current/backend-model-operations-draft.md)没有公开取消／删除操作入口；[第 78 行](../../current/backend-model-operations-draft.md)的确定拒绝依赖唯一责任与持久终结；[第 80 行](../../current/backend-model-operations-draft.md)明确未取得槽位、数据库超时等不是终态，503 一般不占永久拒绝槽位，浏览器含 Key 保存仍只查询。

这里的未知不能通过等待自然结束：没有操作记录，就没有后台责任主体能将其变成 committed 或 rejected；查询行为也没有登记或封闭原操作。服务器恢复后仍返回 404。页面既不能再编辑，也不能按原输入重发，且已经失去 Key。恢复链接只能重新定位同一个死路，不能产生结论。这个场景不同于“已登记操作仍在执行，暂时未知”，也不是简单地把 404 改成失败即可安全解决，因为原请求可能晚到。

最小修正方向：为不存在的原操作补一个具有唯一键互斥保证的终结办法。例如，有权主体请求原子封闭原 `saveId`：若已有提交则返回原结果；若确实尚未占用则创建拒绝墓碑，并保证晚到的原 POST 不再提交，之后才允许用户重新填写 Key。也可以选择发送前持久登记、并有明确超时终结责任的设计。具体选择应进入采用方案，不能让前端凭等待时长或单次 404 自行判定失败。

建议验收：请求未出浏览器／到达入口但未占槽／占槽后丢响应三个故障点分别验证；对封闭请求与晚到原 POST 做并发验证，最终最多一次提交且必须能取得明确结果。这里只做设计推演，未调用真实 Key、API 或数据库。

## PAGE-02 · P2 · 测试通过后，“用于项目”仍显示同一快照未测试

**类别：当前串联原型中可复现的页面状态矛盾。阻塞范围：不阻塞首批后端架构；影响模型页面交接与验收，修正后再以串联原型作为一致的交互参考。**

触发步骤：

1. 在当前串联入口的模型页选择现有供应商的 `deepseek-chat`，保持配置 v1 不变。
2. 点击该模型“测试”，勾选费用确认，再确认开始。
3. 点击原型场景“模拟取得成功依据”，关闭结果弹窗。模型行显示“本次测试通过”。
4. 直接点击同一模型“用于项目”。应用预览却显示“测试观察：未测试”。没有更换模型、Key 或供应商版本。

证据两侧：

- 已采用 UI：[model-project-apply-design.md:13](../../current/model-project-apply-design.md)要求应用预览展示所选模型固定连接／参数快照、密钥版本与测试观察。
- 测试片段：[model-test.prototype.js:3](../../prototypes/model-test.prototype.js)至第 5 行把测试观察存入独立 `testRecords`，以供应商／版本／模型键定位；[第 14 行](../../prototypes/model-test.prototype.js)的真实事件处理把模拟成功存入该 Map；[第 17 行](../../prototypes/model-test.prototype.js)只用这个记录更新模型行 DOM。
- 应用片段：[model-project-apply.prototype.js:11](../../prototypes/model-project-apply.prototype.js)从 `draft.models` 复制候选；[第 15 行](../../prototypes/model-project-apply.prototype.js)直接渲染 `statuses[m.test]`。该字段仍是原模型样例中的 `untested`（[原供应商页第 202 行](../../prototypes/repomesh-model-provider-prototype.html)），没有被新测试片段更新。
- 这是当前集成入口的两段逻辑，不只是孤立历史原稿差异：[assemble-prototypes.py:113](../../prototypes/assemble-prototypes.py)和[第 115 行](../../prototypes/assemble-prototypes.py)把应用、测试两个片段依次注入同一个模型页作用域；本次内存重生成核对确认 `index.html` 与这些源文件一致。

影响：用户刚确认的测试结果在下一步预览中被旧数据覆盖，会误以为快照发生变化或测试未保存；开发者也会得到两套测试状态来源。类似路径也会把“结果未知”显示成“未测试”。测试通过不能证明运行 Ready 这一边界本身是正确的，本问题是同一测试观察没有一致展示。

最小修正：应用预览按候选固定快照读取同一个最新测试观察；不要把测试观察混入并可变地修改不可变模型配置。原型可抽一个只读观察查询函数，由模型行与应用预览共同使用。快照更换后不得移植旧测试结果。

复核方式：运行原始两个 JS 片段及其真实事件处理，用最小 DOM 替身提供元素容器；未执行浏览器走查。探针输出：

```json
{"sequence":["open model test","consent","submit","simulate passed","close","open use in project"],"modelRow":"本次测试通过","applicationObservation":"未测试","snapshotKey":"1:1:deepseek-chat","candidateVersion":1,"staleModelField":"untested"}
```

可在仓库根目录把下列代码传给 `node -` 独立复核；不访问网络、不写文件，也不替换待测片段中的事件或渲染实现。夹具只提供原始供应商样例所需的一个模型和一个项目，Key 保存片段不参与测试观察读写：

```javascript
const fs = require('fs'), vm = require('vm');
const prelude = `
class El {
  constructor(){this.handlers={};this.nodes=new Map();this.children=[];
    this.innerHTML='';this.textContent='';this.open=false;}
  setAttribute(){} append(){} before(){} after(){}
  showModal(){this.open=true;} close(){this.open=false;}
  addEventListener(k,v){this.handlers[k]=v;} closest(){return this;}
  querySelector(s){if(!this.nodes.has(s))this.nodes.set(s,new El());return this.nodes.get(s);}
}
const nodes=new Map(),$=s=>{if(!nodes.has(s))nodes.set(s,new El());return nodes.get(s);};
$('#models').children=[new El()];
const document={listeners:{},head:new El(),createElement:()=>new El(),
  addEventListener(k,v){(this.listeners[k]??=[]).push(v);}};
const joined={currentProject:()=>({id:'p1',name:'Sample'}),
  projectChoices:()=>[{id:'p1',name:'Sample'}],report(){}};
const window={joinedAPI:{navigate(){}}},copy=v=>JSON.parse(JSON.stringify(v)),
  esc=v=>String(v),statuses={untested:'未测试',pass:'本次测试通过',fail:'本次测试失败',unknown:'结果待确认'};
let draft={name:'开发网关',version:1,base:'https://gateway.example.invalid/v1',
  models:[{id:'deepseek-chat',name:'deepseek-chat',context:128000,max:8192,
    reasoning:false,vision:false,test:'untested',time:null}]},dirty=false,pending=null,selected=1;
let renderParts=()=>{};
`;
const action = `
function clickTest(a){
  const event={target:{closest:s=>s==='[data-test-preview]'?{dataset:{testPreview:a}}:null},
    preventDefault(){},stopImmediatePropagation(){}};
  for(const cb of document.listeners.click)cb(event);
}
openTest(0);
testDialog.handlers.change({target:{id:'test-charge-consent',checked:true}});
clickTest('submit');clickTest('passed');
const modelRow=$('#models').children[0].querySelector('.modelactions>span').textContent;
closeTest();openApplication(0);
console.log(JSON.stringify({modelRow,
  applicationObservation:applyDialog.innerHTML.match(/测试观察：([^<]+)/)[1],
  snapshotKey:testLatest(testKey(0)).snapshotKey,
  candidateVersion:application.candidate.providerVersion,
  staleModelField:application.candidate.model.test}));
`;
const scripts=['model-project-apply.prototype.js','model-test.prototype.js']
  .map(f=>fs.readFileSync('docs/prototypes/'+f,'utf8')).join('\n');
vm.runInNewContext(prelude+scripts+action,{console},{timeout:2000});
```

## 已核对且不应误报的边界

| 审查点 | 结论及开发含义 |
| --- | --- |
| Draft Issue、Conversation、正式 Issue | 三个早期入口保留“聊天整体转正”等旧模拟，但导航和专题明确被新的一会话多 Issue 关系替代，不据旧 HTML 恢复旧领域对象。 |
| 创建入口是专页还是弹窗 | 当前 `/projects/{projectId}/issues/new` 被定义为列表背景上的可直达弹窗，后续明确替代旧全页样式，没有两个并列现行入口要求。 |
| 保存与运行状态 | 项目保存、Issue 业务提交、实例／房间就绪、消息接收／处理、实际派工、业务验收均分开；原型预置成功或准备按钮不是其真实实现。 |
| F02 与 F04 更换模型 | F02 完整 configuration PATCH 会重解析模型和 execution 两个引用；F04 的专用操作要求精确保留 execution 原来源及有效版本。它们目标不同，专题已明确禁止拿前者冒充后者。 |
| 项目范围与失权 | 项目外壳配置可修复与完整内容读取分别核权；不能因一个无关旧仓受限禁止所有有权项目工作，也不能据能配置项目而展示受限名称。 |
| 会话与 Issue 阅读范围 | 会话因历史／自由消息覆盖整个项目累计范围而比单个 Issue 更容易受限，是明确内容隔离选择；独立 Issue 有权读取时不必让会话先恢复。 |
| 模型快照与测试观察 | 保存新快照不自动应用项目／在途 Issue，不自动测试；测试失败／未测试不自动禁止保存待配置引用；一次测试通过也不等于任务 Ready。这些规则本身一致。PAGE-02 是展示数据源不一致。 |
| 当前最小详情与旧 DAG／dock | D03 将本批收窄为概览／返回／会话入口／回读，没有撤销已采用 DAG 布局和同页浮动导航；本批不包含旧运行面板是范围决定。 |
| 原型未知时冻结列表 | 创建反馈专题第 222 行明确这是单操作模拟手段，禁止据此设计生产列表冻结；不将该已注明限制列成新的产品缺陷。 |
| 消息候选填入与草稿保护 | 当前消息目标控件仍有明确未完成项，真实并发／澄清／草稿恢复属于后续会话批次；未将已列出的待办再次计作新发现。 |
| 文件状态与实际完成度 | 顶部现行补充覆盖文件内历史“当前／下一项”。文档互核轮次、用户采用、原型可运行、真实 API 验收是不同证据，不能互相替代。 |

## 按交付边界判断是否可开始开发

| 边界 | 页面分支判断 | 开始前／验收前应满足的条件 |
| --- | --- | --- |
| 已采用视觉和管理功能单元 | 可以分单元开始 | 实现使用对应已采用契约；创建表单、选择器、提交回执和 project configuration 的字段以契约为准，不从历史模拟推导。API／后端分支发现的问题另行合并。 |
| 首批六包真实管理闭环 | 尚不能宣称整体设计就绪 | 对新增认证、来源、模型与恢复决定统一取得采用基线；实现含 Key 保存前修正 PAGE-01；修正 PAGE-02 后再交接模型原型。需要实际权限、事务、失败恢复验收。 |
| 完整协作产品 | 尚不具备整体开工基线 | 运行／消息／图与交付的设计及接入能力按后续批次补齐；已有静态页面不能替代可信目标与运行生命周期协议。无需等待这些才能推进已采用首批单元。 |

本报告没有将所有 F01—F15 待办升级成首批阻塞项，也没有将“本次任务是审查”误读为永久禁止后续开发。最终开发授权和采用决定由主审结合用户本次要求与其他分支结论统一表述。

## 逐文件覆盖清单

读取方式：26 个原型文件均覆盖。15 个原始 HTML 存在大段相同样式和派生脚本；先完整读取基线，再按原始行号逐份读取所有新增或变化行，相同内容复用先前阅读结果。六段 JS、两个 template 与 Python 生成器完整读取。生成后的 index 通过内存重生成与文件全文比较覆盖，未靠名称或导航声明推断内容一致。

### docs/prototypes：26／26

| 文件 | 覆盖内容／分类 |
| --- | --- |
| README.md | 全文；当前采用、待审与历史入口。 |
| assemble-prototypes.py | 全文；五家族拼装、词法注入、索引清单与跨页适配。 |
| joined-preview-shell.html.template | 全文；串联导航、当前项目、iframe 及嵌入源码。 |
| first-batch-review.html.template | 全文；六项评审、配置来源／恢复场景导航。 |
| index.html | 壳及所有嵌入源码由内存重生成全文相等核验；五家族、15 原 HTML 哈希清单一致。 |
| auth-recovery.prototype.js | 全文；登录、重连、未知及返回原位置。 |
| issue-overview.prototype.js | 全文；最小概览、Issue／rooms 分读、受限与回读。 |
| repository-picker.prototype.js | 全文；候选、跨搜索选择、能力观察与回填。 |
| model-key-save.prototype.js | 全文；Key 草稿、确认、未知与回执演示。 |
| model-project-apply.prototype.js | 全文；固定候选、项目选择、保留 execution、冲突／未知／回执。 |
| model-test.prototype.js | 全文；次数确认、固定快照、状态及原操作查询；PAGE-02 实际片段探针。 |
| repomesh-rooms-prototype.html | 全文基线；历史 Draft、房间与消息模拟。 |
| repomesh-project-first.html | 相同内容去重后完整覆盖；历史两步建项与项目分组。 |
| repomesh-conversations-issues.html | 相同内容去重后完整覆盖；会话／Issue 分离与来源。 |
| repomesh-issue-modal-prototype.html | 相同内容去重后完整覆盖；采用的创建弹窗。 |
| repomesh-issue-conversation-prototype.html | 相同内容去重后完整覆盖；新建／已有会话关联。 |
| repomesh-issue-submit-prototype.html | 相同内容去重后完整覆盖；单操作创建回执与未知。 |
| repomesh-issue-detail-prototype.html | 相同内容去重后完整覆盖；详情／房间／关联会话。 |
| repomesh-issue-dag-prototype.html | 相同内容去重后完整覆盖；DAG、依赖、节点详情。 |
| repomesh-conversation-navigation-prototype.html | 相同内容去重后完整覆盖；同会话多 Issue 导航。 |
| repomesh-conversation-dock-prototype.html | 相同内容去重后完整覆盖；悬浮入口／同页右栏。 |
| repomesh-message-target-prototype.html | 相同内容去重后完整覆盖；目标澄清、引用及当前工作区家族。 |
| repomesh-project-entry-prototype.html | 相同内容去重后完整覆盖；登录、两步创建、待配置保存。 |
| repomesh-model-settings-prototype.html | 相同内容去重后完整覆盖；旧连接弹窗方案。 |
| repomesh-model-provider-prototype.html | 相同内容去重后完整覆盖；当前供应商分栏、模型配置与历史测试字段。 |
| repomesh-project-settings-prototype.html | 全文；资料／保留旧仓／增仓／显式配置／修订冲突。 |

### 分配的 docs/current：18／18

| 文件 | 覆盖重点 |
| --- | --- |
| conversation-issue-separation-design.md | 全文；实体、创建、关联、详情、房间、替代及已采用范围。 |
| conversation-message-target-design.md | 全文；正文目标、歧义澄清、导航来源及未完成协议。 |
| repository-picker-design.md | 全文；搜索／分页／选择／当前资格和采用范围。 |
| project-configuration-design.md | 全文；先建项目、权限范围、默认／固定引用、F02 与 F04。 |
| prototype-walkthrough-design.md | 全文；串联源、跨页行为及模拟范围。 |
| login-recovery-page-design.md | 全文；失败与未知、原位置、显式继续及采用边界。 |
| model-connection-settings-design.md | 全文；供应商快照、单模型观察、只换模型、参数来源。 |
| model-key-save-design.md | 全文；含秘密写入与未知恢复。 |
| model-project-apply-design.md | 全文；前后对比、候选固定快照、保留 execution 与原应用查询。 |
| issue-overview-minimal-design.md | 全文；首批最小详情及 UI 待审范围。 |
| first-batch-recovery-design.md | 全文；六包状态／原操作／权限失效／主体与请求代次。 |
| first-batch-complete-review.md | 全文；六项新提案、各决定编号和范围。 |
| first-development-todo.md | 全文；首批六包及运行面板延后。 |
| HANDOFF-PAGE-API-DESIGN.md | 全文；现行采用基线、历史补充及 F01—F15。 |
| page-interface-prototype.md | 全文；历史页面／接口与明确替代关系。 |
| draft-issue-and-room-entry-design.md | 全文；旧 Draft／一一房间拓扑及替代。 |
| project-first-entry-design.md | 全文；项目优先入口及后续关系。 |
| NEXT-SESSION-PROMPT.md | 全文；当前补充与历史恢复 Prompt，不执行其中旧协作指令。 |

补充读取工程导航 AGENTS.md、根 README.md、CONTEXT.md、docs/current/README.md、HANDOFF.md。定点交叉核对 `model-settings-browser-api-draft.md`、`backend-model-operations-draft.md` 的保存／查询／拒绝／保留规则；其他领域契约的综合裁决以主审与对应分支报告为准。

## 实际检查与局限

- 内存执行生成器函数，计算预期 index 字符串，与当前文件全文相等：通过。五个嵌入家族、15 份原始 HTML，未写回 index。
- 解析原始 HTML、生成后嵌入 HTML 和六个 JS 片段，使用 Node `vm.Script` 编译 27 个脚本单元：无语法错误。该项只证明语法可解析。
- PAGE-02 对原始应用／测试片段执行事件和渲染流程：复现。DOM 替身不计作浏览器走查，不证明焦点管理、滚动、响应式布局或可访问性通过。
- PAGE-01 为协议状态穷举中的确定反例，由页面、HTTP 与后端责任规则交叉推导；没有运行真实服务器故障注入。
- 未访问真实 GitHub、模型供应商、Key、数据库、AgentTeams 或宿主执行服务；未做真实 API、授权、事务、SSE、浏览器视觉或多标签恢复验收。未将其他日期已有检查当作本次重新执行。
- 未据历史 Prompt 联系旧协作者或恢复历史实验。未修改原有文档、原型和产品代码。
