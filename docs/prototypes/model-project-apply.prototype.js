// F04 r1: isolated application-preview fixtures; never writes F02 configuration.
const applyDialog=$('#project-dialog'),applyFixtures=new Map();
let application=null,applicationCount=0;
const applyButton=(label,act,primary=false,disabled=false)=>`<button type="button" data-apply="${act}" class="${primary?'primary':'outline'}" ${disabled?'disabled':''}>${label}</button>`;
const outstandingApplication=()=>application&&['pending','unknown'].includes(application.stage);
function fixtureFor(id){if(!applyFixtures.has(id))applyFixtures.set(id,{revision:7,execution:7,model:'现用模型 · 示例快照 M2'});return applyFixtures.get(id)}
function takePreview(a){a.before=copy(fixtureFor(a.projectId));a.stage='preview';a.notFound=false}
function openApplication(index){
 if(!outstandingApplication()){
  const m=draft.models[index];if(!m||dirty||pending)return;
  const p=joined.currentProject();application={projectId:p.id,projectName:p.name,candidate:{providerId:selected,provider:draft.name,providerVersion:draft.version,base:draft.base,model:copy(m),keyVersion:'K4（示例）'},stage:'preview',scenario:'success',operation:null};takePreview(application);
 }
 renderApplication();if(!applyDialog.open)applyDialog.showModal();
}
function applyDetails(a){const c=a.candidate,m=c.model,b=a.before,observation=testObservation(testSnapshotKey(c.providerId,c.providerVersion,m.id));return `<div class="apply-grid"><section class="box"><h3>替换后模型</h3><div class="apply-value">${esc(m.name||m.id)}</div><p>模型 ID：${esc(m.id)}<br>供应商：${esc(c.provider)}<br>连接／参数快照：v${c.providerVersion}（样例）<br>密钥版本：${esc(c.keyVersion)}</p><p class="minor">测试观察：${esc(observation.label)}<br>仅对应所选模型的这份样例配置。</p><details><summary>查看连接与模型参数</summary><div class="params">Base URL：${esc(c.base)}<br>上下文：${m.context.toLocaleString()} Token<br>最大输出：${m.max.toLocaleString()} Token<br>推理：${m.reasoning?'支持':'不支持'} · 输入：${m.vision?'文本与图片':'仅文本'}<br>密钥内容不展示。</div></details></section><section class="box"><h3>替换前模型</h3><p><span class="apply-value">${esc(b.model)}</span></p><h3>保持执行配置不变</h3><p>标准执行配置<br>引用：exec_default（示例）<br>有效版本：${a.stage==='missing'?'尚未取得':`E${b.execution}（示例）`}</p><p class="minor">预览比较基线：项目修订 P${b.revision}（示例）<br>保留版本不代表当前执行资格有效。</p></section></div>`}
function renderApplication(){
 const a=application;if(!a)return;const locked=['pending','unknown','receipt','current','conflict'].includes(a.stage),choices=joined.projectChoices();let body='',actions='';
 if(['preview','missing','changed'].includes(a.stage)){
  body=applyDetails(a);
  if(a.stage==='missing')body+='<p class="apply-warning" role="alert">尚未取得执行配置的固定版本，无法核对保留范围。请先重新读取预览。</p>';
  if(a.stage==='changed')body+=`<p class="apply-warning" role="alert">预览条件已变化：项目修订 P${a.before.revision} → P${a.before.revision+1}，执行版本 E${a.before.execution} → E${a.before.execution+1}（均为示例）。本次尚未提交，请重新读取后核对。</p>`;
  actions=applyButton('取消','close')+(a.stage==='preview'?applyButton('确认仅更换模型','confirm',true):applyButton('重新读取预览','reread',true));
 }else if(a.stage==='pending'){
  body='<h3>正在应用模型…</h3><p>等待这次应用的结果。离开弹窗不撤销请求。</p>';actions=applyButton('稍后核查','close');
 }else if(a.stage==='unknown'){
  body=`<h3>应用结果待确认</h3><p class="apply-warning" role="status">${a.notFound?'暂未查到原应用结果，仍不能判断是否保存。':'这次应用可能已经保存，请先核查原操作。'}</p><p>项目：${esc(a.projectName)}<br>模型：${esc(a.candidate.model.id)}<br>原操作：应用演示 ${a.operation.number}</p><p class="minor">查询结果不会自动发起新的应用。</p>`;actions=applyButton('稍后核查','close')+applyButton('查询原应用结果','query',true);
 }else if(a.stage==='conflict'){
  body='<h3>项目配置已变化，本次未保存</h3><p>服务器明确拒绝了旧预览。本次应用内容已保留，重新读取项目后再核对。</p>';actions=applyButton('关闭','close')+applyButton('重新读取并核对','reread',true);
 }else if(a.stage==='receipt'){
  body=`<h3>原应用已保存 · 样例回执</h3><p>项目：${esc(a.projectName)}<br>模型：${esc(a.candidate.model.id)}<br>保留执行版本：E${a.operation.before.execution}（示例）<br>应用演示 ${a.operation.number} · 固定回执</p><p class="minor">回执说明这次保存结果；项目当前配置需另外读取。模型可用性与运行状态尚未验证。</p>`;actions=applyButton('关闭','close')+applyButton('查看项目当前配置','current',true);
 }else{
  const current=fixtureFor(a.projectId);body=`<h3>项目当前配置 · 本次回读样例</h3><p>模型：${esc(current.model)}<br>执行引用：exec_default（示例）<br>执行版本：E${current.execution}（示例）<br>项目修订：P${current.revision}（示例）</p><p class="minor">样例回读没有接入真实项目，也没有修改项目设置页中的配置。</p>`;actions=applyButton('完成','close',true);
 }
 applyDialog.innerHTML=`<div class="modalhead"><h2 id="project-title">用于项目</h2><button data-apply="close" aria-label="关闭模型应用弹窗">×</button></div><div class="modalbody"><p class="apply-prototype">原型样例 · 固定版本与资格为演示数据，专用应用接口尚未接通</p><label class="field">项目<select id="apply-project" ${locked?'disabled':''}>${choices.map(p=>`<option value="${esc(p.id)}" ${p.id===a.projectId?'selected':''}>${esc(p.name)} · 可配置样例</option>`).join('')}</select><small>仅列本次预览中建立的项目，不代表真实资格查询。</small></label>${body}<p class="minor">只更新所选项目模型；其他项目与在途 Issue 不切换。</p><details class="apply-tools"><summary>F04 演示场景</summary><div class="params">${['preview','missing','changed'].includes(a.stage)?`<label>提交结果 <select id="apply-scenario"><option value="success" ${a.scenario==='success'?'selected':''}>模拟保存成功</option><option value="unknown" ${a.scenario==='unknown'?'selected':''}>模拟结果未知</option><option value="conflict" ${a.scenario==='conflict'?'selected':''}>模拟冲突未保存</option></select></label><div class="modelactions">${applyButton('预览信息缺失','missing')}${applyButton('预览条件变化','changed')}</div>`:a.stage==='pending'?applyButton('显示本次模拟返回','resolve'):a.stage==='unknown'?applyButton('模拟查到原应用成功','found'):'本页仅演示状态；真实版本比较、保存与核查未执行。'}</div></details></div><div class="modalfoot">${actions}</div>`;
 joined.report('models','model-apply','F04 · 三项UI已采用 · 专用协议未完成');renderApplicationEntry();
}
function renderApplicationEntry(){let el=$('#application-resume');if(!el){el=document.createElement('div');el.id='application-resume';$('#project').after(el)}el.innerHTML=outstandingApplication()?`<div class="box">${esc(application.projectName)} 有一笔模型应用结果待确认。 ${applyButton('继续核查原应用','resume',true)}</div>`:'';}
function closeApplication(){applyDialog.close();renderApplicationEntry();joined.report('models','models',null)}
function recordApplication(){const a=application,op=a.operation;applyFixtures.set(op.projectId,{revision:op.before.revision+1,execution:op.before.execution,model:op.candidate.model.id+' · 示例快照 v'+op.candidate.providerVersion});a.stage='receipt';}
document.addEventListener('click',e=>{
 const modelButton=e.target.closest('#models [data-action="apply"]');if(modelButton){e.preventDefault();e.stopImmediatePropagation();if(!modelButton.disabled)openApplication(Number(modelButton.dataset.index));return}
 const action=e.target.closest('[data-apply]')?.dataset.apply;if(!action)return;e.preventDefault();e.stopImmediatePropagation();const a=application;if(!a)return;
 if(action==='close'){closeApplication();return}if(action==='resume'){renderApplication();if(!applyDialog.open)applyDialog.showModal();return}
 if(action==='missing')a.stage='missing';if(action==='changed')a.stage='changed';
 if(action==='reread'){if(['changed','conflict'].includes(a.stage)){const f=fixtureFor(a.projectId);f.revision++;f.execution++}takePreview(a);a.operation=null}
 if(action==='confirm'&&a.stage==='preview'){a.operation={number:++applicationCount,projectId:a.projectId,candidate:copy(a.candidate),before:copy(a.before),scenario:a.scenario};a.stage='pending'}
 if(action==='resolve'&&a.stage==='pending'){if(a.operation.scenario==='success')recordApplication();else a.stage=a.operation.scenario}
 if(action==='query'&&a.stage==='unknown')a.notFound=true;
 if(action==='found'&&a.stage==='unknown')recordApplication();if(action==='current')a.stage='current';renderApplication();
},true);
applyDialog.addEventListener('change',e=>{if(e.target.id==='apply-project'){const p=joined.projectChoices().find(p=>p.id===e.target.value);application.projectId=p.id;application.projectName=p.name;takePreview(application);renderApplication()}if(e.target.id==='apply-scenario')application.scenario=e.target.value});
applyDialog.addEventListener('cancel',e=>{e.preventDefault();closeApplication()});
const applicationParts=renderParts;renderParts=function(){applicationParts();renderApplicationEntry()};
const applicationNavigate=window.joinedAPI.navigate;window.joinedAPI.navigate=function(route){applicationNavigate(route);if(route==='model-apply')openApplication(0)};
const applicationStyle=document.createElement('style');applicationStyle.textContent='#repomesh-rooms-prototype #project-dialog{width:min(720px,calc(100vw - 24px))}#repomesh-rooms-prototype .apply-grid{display:grid;grid-template-columns:1fr 1fr;gap:12px}#repomesh-rooms-prototype .apply-grid h3{margin:0 0 10px;color:#ddd;font-size:13px}#repomesh-rooms-prototype .apply-value{color:#e3e3e3;font-size:14px;overflow-wrap:anywhere}#repomesh-rooms-prototype .apply-prototype{font-size:11px;color:#b9aa89;background:#2b261d;border:1px solid #514734;border-radius:7px;padding:8px 10px;margin:0 0 16px}#repomesh-rooms-prototype .apply-warning{padding:12px;color:#d9bf98;background:#2b261e;border:1px solid #554731;border-radius:8px}#repomesh-rooms-prototype .apply-tools{color:#999}#repomesh-rooms-prototype .apply-tools button{font-size:11px}@media(max-width:580px){#repomesh-rooms-prototype .apply-grid{grid-template-columns:1fr}}';document.head.append(applicationStyle);
