// F05 key/save proposal. Only a boolean demo placeholder; never accepts a real key.
const keyDialog=document.createElement('dialog');keyDialog.id='key-save-dialog';keyDialog.setAttribute('aria-labelledby','key-save-title');$('#repomesh-rooms-prototype').append(keyDialog);
let keyDraft=false,keyDemo=false,keyFlow=null,keyCount=0;
const keyBtn=(label,action,primary=false,disabled=false)=>`<button type="button" data-key-save="${action}" class="${primary?'primary':'outline'}" ${disabled?'disabled':''}>${label}</button>`;
const keyOutstanding=()=>keyFlow&&['pending','unknown','close-confirm','closing'].includes(keyFlow.stage);
function keyOpen(){
 if(keyOutstanding()){keyRender();keyDialog.showModal();return;}
 if(pending||hasUnknown()){notice('请先核查当前未确认的操作。');return;}
 keyDemo=false;keyFlow={stage:'edit'};keyRender();keyDialog.showModal();
}
function keyClose(){keyDialog.close();renderParts();joined.report('models','models',null);}
function keyRender(){
 const f=keyFlow;if(!f)return;let title='',body='',actions='';
 if(f.stage==='edit'){
  title=draft.key?'替换 API Key':'设置 API Key';
  body=`<div class="key-current"><span>供应商</span><strong>${esc(draft.name||'新供应商')}</strong><span>当前密钥</span><strong>${draft.key?'已配置 · 原文不回显':'尚未配置'}</strong></div><p class="minor">${draft.key?'保留原密钥，无需重新填写。只有明确替换才会加入修改。':'填写密钥后，与供应商连接及模型配置一起保存。'}</p><label class="field">新的 API Key<input aria-label="新的 API Key 演示占位" readonly value="${keyDemo?'••••••••（演示占位）':''}" placeholder="仅演示占位，不输入真实密钥"><small>此原型不接收真实密钥。</small></label>${keyBtn('填入演示占位','demo')}<p class="key-info">加入草稿还未保存，也不会更新已有项目或在途 Issue。</p>`;
  actions=keyBtn('取消','close')+keyBtn('加入配置草稿','draft',true,!keyDemo);
 }else{
  const p=f.snapshot;
  title=({summary:'确认保存配置',pending:'正在保存配置',unknown:'保存结果待确认','close-confirm':'终结原保存',closing:'正在终结原保存',receipt:'配置已保存',rejected:'本次未保存',closed_without_save:'原保存已终结，未保存'})[f.stage];
  body=`<div class="key-current"><span>供应商</span><strong>${esc(p.name)}</strong><span>Base URL</span><strong>${esc(p.base)}</strong><span>API Key</span><strong>${f.replacing?(f.setting?'本次明确设置':'本次明确替换'):'保留原密钥'}</strong><span>模型配置</span><strong>${p.models.length} 个模型 · 完整连接快照</strong></div>`;
  if(f.saveId)body+=`<p class="minor">原保存 ID：${esc(f.saveId)}（演示）</p>`;
  if(f.stage==='summary')body+='<p class="key-info">保存后形成新的配置快照；已有项目和在途 Issue 保持原版本。</p><p class="minor">保存不会测试模型或用于项目，这两步需要单独操作。</p>';
  if(f.stage==='pending')body+='<p class="key-info" role="status">正在等待本次保存结果，可以关闭弹窗后继续核查。</p>';
  if(f.stage==='unknown')body+=`<p class="key-info" role="status">${f.closeAttempted?'终结请求尚无确定回执，原保存结果仍待确认。请查询原保存，或重试终结同一原操作；不能开始新保存。':'尚未确认本次是否保存。可以查询原操作，或明确终结原保存；暂不重复提交配置。'}</p>${f.queried?'<p class="minor">本次样例查询仍未取得结果，继续保持待确认。</p>':''}<p class="minor">关闭弹窗后，供应商页面保留“核查原保存”入口。关闭弹窗不终结原操作。</p>`;
  if(f.stage==='close-confirm')body+='<p class="key-info">终结原保存 · 新增待审操作<br>如果原请求已经保存，返回原保存结果，不撤销配置。如果尚未保存，服务端持久关闭这个原保存 ID，阻止晚到请求再保存。</p><p class="minor">此操作不重新发送 API Key。只有取得明确未保存的终态回执后，才能重新填写并发起新保存。</p>';
  if(f.stage==='closing')body+='<p class="key-info" role="status">正在等待同一原保存的终结回执。尚未证明未保存，不能发起新保存。关闭弹窗只停止等待。</p>';
  if(f.stage==='receipt')body+=`<p class="key-success" role="status">本次保存已确认 · 示例配置 v${f.savedVersion}</p><p class="minor">密钥原文不回显；新快照未测试，已有项目保持原版本。</p>`;
  if(f.stage==='rejected')body+='<p class="key-info" role="status">已取得持久拒绝回执，本次没有保存；同一原请求不能后来提交。原配置仍保留。</p>';
  if(f.stage==='closed_without_save')body+='<p class="key-success" role="status">原操作已终结，未保存。原请求晚到也不能保存；已有配置不会被撤销。</p>';
  if(['rejected','closed_without_save'].includes(f.stage))body+=`<p class="minor">${f.replacing?'原密钥输入已清除，请重新填写 API Key 演示占位，再检查草稿并明确发起新操作。':'可返回检查保留原密钥的草稿，再明确发起新操作。'}</p>`;
  actions=f.stage==='summary'?keyBtn('返回修改','close')+keyBtn('确认保存','submit',true):f.stage==='unknown'?keyBtn('稍后核查','close')+keyBtn(f.closeAttempted?'重试终结原保存':'终结原保存',f.closeAttempted?'close-save':'request-close')+keyBtn('查询原保存结果','query',true):f.stage==='close-confirm'?keyBtn('继续核查，暂不终结','keep-querying')+keyBtn('确认终结原保存','close-save',true):['pending','closing'].includes(f.stage)?keyBtn('关闭，稍后核查','close'):['rejected','closed_without_save'].includes(f.stage)&&f.replacing?keyBtn('返回模型设置','close')+keyBtn('重新填写 API Key','reenter',true):keyBtn('返回模型设置','close',true);
 }
 const controls=f.stage==='pending'?keyBtn('模拟结果未知','unknown')+keyBtn('模拟保存成功','found')+keyBtn('模拟持久拒绝','rejected'):f.stage==='closing'?keyBtn('模拟原保存先提交','found')+keyBtn('模拟关闭成功，未保存','closed_without_save')+keyBtn('模拟原操作已拒绝','rejected')+keyBtn('模拟终结响应丢失','unknown'):f.stage==='unknown'?keyBtn('模拟查到原保存成功','found')+keyBtn('模拟查到持久拒绝','rejected')+(f.closeAttempted?keyBtn('模拟查到关闭且未保存','closed_without_save'):''):'';
 keyDialog.innerHTML=`<div class="modalhead"><h2 id="key-save-title">${title}</h2><button type="button" data-key-save="close" aria-label="关闭密钥保存弹窗">×</button></div><div class="modalbody"><p class="key-prototype">F05 · 页面候选 · 仅演示占位，无密钥或接口请求</p>${body}${controls?`<details class="key-tools" open><summary>原型结果演示</summary><div>${controls}</div></details>`:''}</div><div class="modalfoot">${actions}</div>`;
 joined.report('models','key-save','F05 · 密钥保存与原结果查询待评审');
}
keyDialog.addEventListener('click',e=>{
 const a=e.target.closest('[data-key-save]')?.dataset.keySave;if(!a)return;
 if(a==='close'){keyClose();return;}
 if(a==='demo'){keyDemo=true;keyRender();return;}
 if(a==='draft'){if(!keyDemo)return;keyDraft=true;draft.key=true;keyFlow=null;keyDialog.close();changed();notice('新的密钥占位已加入草稿，请统一保存配置。');joined.report('models','models',null);return;}
 const f=keyFlow;
 if(a==='reenter'&&['rejected','closed_without_save'].includes(f.stage)){keyOpen();return;}
 if(a==='submit'&&f.stage==='summary'){f.stage='pending';f.number=++keyCount;f.saveId='00000000-0000-4000-8000-'+String(f.number).padStart(12,'0');pending=copy(f.snapshot);keyDraft=false;keyDemo=false;draft.key=Boolean(providers.find(p=>p.id===selected)?.key);renderParts();}
 if(a==='unknown'&&['pending','closing'].includes(f.stage)){f.stage='unknown';f.queried=false;}
 if(a==='query'&&f.stage==='unknown')f.queried=true;
 if(a==='request-close'&&f.stage==='unknown')f.stage='close-confirm';
 if(a==='keep-querying'&&f.stage==='close-confirm')f.stage='unknown';
 if(a==='close-save'&&(f.stage==='close-confirm'||f.stage==='unknown'&&f.closeAttempted)){f.stage='closing';f.closeAttempted=true;f.queried=false;}
 if(a==='found'&&keyOutstanding()){const candidate=copy(f.snapshot);commit(candidate);keyDraft=false;f.savedVersion=candidate.version;f.stage='receipt';renderParts();}
 if((a==='rejected'&&keyOutstanding())||(a==='closed_without_save'&&f.closeAttempted&&['closing','unknown'].includes(f.stage))){pending=null;keyDraft=false;keyDemo=false;draft.key=Boolean(providers.find(p=>p.id===selected)?.key);f.stage=a;renderParts();notice('已取得明确未保存的原操作回执，请检查草稿后再确认新操作。');}
 keyRender();
});
keyDialog.addEventListener('cancel',e=>{e.preventDefault();keyClose();});
const keyParts=renderParts;renderParts=function(){keyParts();$('#replace-key').textContent=draft.key?'替换 API Key':'设置 API Key';$('#provider-key').value=keyDraft?'新密钥待保存 · 尚未确认':draft.key?'已配置 · 原文不回显':'尚未配置';$('#format').disabled=Boolean(pending);if(keyOutstanding()){$('#dirty-state').textContent=keyFlow.stage==='pending'?'本次保存中':'本次保存待确认';notice(keyFlow.stage==='pending'?'正在等待本次保存结果，请勿重复提交。':'本次保存结果未知，请核查原操作。');$('#save').disabled=true;$('#query-save').hidden=false;$('#save-hint').textContent=keyFlow.stage==='pending'?'正在保存，请核查原操作':'保存结果待确认，请核查原操作';}};
$('#replace-key').onclick=keyOpen;
$('#provider-form').onsubmit=e=>{e.preventDefault();if(!dirty||pending)return;if(!draft.name.trim()||!draft.key||!draft.models.length){notice('请填写供应商名称、密钥占位并添加至少一个模型。');return;}keyFlow={stage:'summary',snapshot:copy(draft),replacing:keyDraft,setting:keyDraft&&!providers.find(p=>p.id===selected)?.key};keyRender();keyDialog.showModal();};
const keyOldDiscard=$('#discard').onclick;$('#discard').onclick=()=>{if(pending)return;keyDraft=false;keyFlow=null;keyOldDiscard();};
$('#query-save').onclick=()=>{if(keyOutstanding()){keyRender();keyDialog.showModal();}};
const keyNavigation=window.joinedAPI.navigate;window.joinedAPI.navigate=function(route){keyNavigation(route);if(route==='key-save')keyOpen();};
// The old standalone save-result switch remains in its source; this joined flow has explicit result controls.
$('#save-result').closest('label').hidden=true;
const keyStyle=document.createElement('style');keyStyle.textContent='#repomesh-rooms-prototype #key-save-dialog{width:min(560px,calc(100vw - 24px))}#repomesh-rooms-prototype .key-current{display:grid;grid-template-columns:90px minmax(0,1fr);gap:12px;font-size:13px;border:1px solid #3b3b3b;border-radius:9px;padding:16px;margin-bottom:18px}#repomesh-rooms-prototype .key-current span{color:#999}#repomesh-rooms-prototype .key-current strong{font-weight:400;overflow-wrap:anywhere}#repomesh-rooms-prototype .key-info{background:#2b271e;border:1px solid #514735;border-radius:8px;color:#cebd99;font-size:12px;padding:13px;line-height:1.8}#repomesh-rooms-prototype .key-success{color:#acc9af;border:1px solid #405844;border-radius:8px;padding:13px;font-size:13px}#repomesh-rooms-prototype .key-prototype{font-size:11px;color:#af9f7c;margin:0 0 18px}#repomesh-rooms-prototype .key-tools{font-size:11px;color:#aaa}#repomesh-rooms-prototype .key-tools>div{padding:10px;display:flex;flex-wrap:wrap;gap:7px}#repomesh-rooms-prototype .key-tools button{font-size:11px}';document.head.append(keyStyle);renderParts();
