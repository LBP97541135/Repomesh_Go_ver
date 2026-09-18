// F03 r1: review-only picker inside the adopted F02 layout. No real discovery.
allRepos.push({id:'shipping',name:'repomesh/shipping'},{id:'notifications',name:'repomesh/notifications',appMissing:true},{id:'analytics',name:'partner/analytics',appUnknown:true});
const pickerContexts=new Map();
function pickerState(){const key=contextId||'sample';if(!pickerContexts.has(key))pickerContexts.set(key,{limit:2,query:'',error:false});return pickerContexts.get(key)}
const pickButton=(label,action,disabled=false)=>`<button type="button" data-picker="${action}" ${disabled?'disabled':''}>${label}</button>`;
function appObservation(r){return r.appUnknown?'App 工作能力待核查':r.appMissing?'App 工作授权待补齐':'App 工作授权可用'}
picker=function(){
 const s=pickerState(),matches=allRepos.filter(r=>!saved.repoIds.includes(r.id)&&r.name.toLowerCase().includes(s.query.toLowerCase())),visible=matches.slice(0,s.limit);
 return `<div class="panel" id="repository-picker"><div class="section-title"><h2>添加项目仓库</h2><span class="tag">${selected.size} 个待添加</span></div>
 <p class="muted">选择仓库后，一起在项目改动摘要中核对并保存。</p>
 <div class="notice"><strong>当前仅发现部分可参与仓库</strong><small style="display:block">当前发现受 App 安装范围限制；账号可参与的全部仓库仍待完整发现。</small></div>
 <div class="panel" style="margin:12px 0"><h2>本次已选 · ${selected.size}</h2>${!selected.size?'<small>尚未选择。搜索和加载更多不会自动添加仓库。</small>':addUnknown?'<p role="status">所选范围的读取资格待核查，旧名称已隐藏。选择仍保留，核实前不能保存本次增量。</p>':allRepos.filter(r=>selected.has(r.id)).map(r=>`<div class="row"><span>${esc(r.name)}<small>${appObservation(r)}</small></span><button data-picker-remove="${r.id}" aria-label="撤回 ${esc(r.name)}">撤回</button></div>`).join('')}${selected.size?pickButton('撤回全部待添加选择','clear'):''}</div>
 ${addUnknown?`<div class="notice">暂时无法核实账号读取资格，未判定为无权。${pickButton('重新核查（模拟）','recover')}</div>`:s.error?`<div class="notice" role="alert">仓库候选加载失败，不能确定当前结果。${pickButton('重试加载（模拟）','recover')}</div>`:`<label class="field">搜索已发现仓库<input id="picker-search" aria-label="搜索已发现仓库" placeholder="输入组织或仓库名称" value="${esc(s.query)}"></label><div>${visible.map(r=>`<label class="repo-label"><input type="checkbox" data-repo="${r.id}" ${selected.has(r.id)?'checked':''}><span>${esc(r.name)}<small>账号可读取 · ${appObservation(r)}</small></span></label>`).join('')||'<p class="empty">当前已发现范围中没有匹配结果；不代表没有其他可参与仓库。</p>'}</div>${visible.length<matches.length?pickButton('加载更多已发现仓库','more'):'<small>当前发现结果已加载完，发现范围仍不完整。</small>'}`}
 <p class="muted">账号可读取时，App 授权待补齐或待核查的仓库仍可保存到项目；这不代表已能执行工作。受邀账号可能需要仓库管理员协助授权。</p>
 <div class="actions">${pickButton('完成选择，回到项目表单','done')}${pickButton('收起，保留选择','close')}</div>
 <details style="margin-top:18px"><summary class="muted">F03 演示场景 · 三项安排已采用</summary><div class="actions">${pickButton('模拟读取资格未知','unknown')}${pickButton('模拟加载失败','failure')}${pickButton('恢复已核实样例','recover')}</div><small>观察时间：本次预置样例；没有真实账号发现、授权检查或安装跳转。</small></details></div>`;
};
root.addEventListener('input',e=>{if(e.target.id!=='picker-search')return;e.stopImmediatePropagation();const s=pickerState();s.query=e.target.value;s.limit=2;const pos=e.target.selectionStart;render();const el=root.querySelector('#picker-search');el.focus();el.setSelectionRange(pos,pos)},true);
root.addEventListener('click',e=>{
 const action=e.target.closest('[data-picker]')?.dataset.picker,remove=e.target.closest('[data-picker-remove]')?.dataset.pickerRemove;if(!action&&!remove)return;e.preventDefault();e.stopImmediatePropagation();const s=pickerState();
 if(remove)selected.delete(remove);
 if(action==='more')s.limit+=2;
 if(action==='clear'){selected.clear();addUnknown=false}
 if(action==='unknown'){addUnknown=true;s.error=false}
 if(action==='failure'){s.error=true}
 if(action==='recover'){addUnknown=false;s.error=false}
 if(action==='done'||action==='close')addOpen=false;
 render();if(!addOpen)root.querySelector('[data-act="addToggle"]')?.scrollIntoView({block:'center'});
},true);
const pickerRender=render;
render=function(){pickerRender();const opened=addOpen&&stage==='edit';joined.report('settings',opened?'repository-picker':'settings',opened?'F03 · 三项页面安排已采用 · 完整发现与恢复待补':null)};
const pickerNavigate=window.joinedAPI.navigate;
window.joinedAPI.navigate=function(route){pickerNavigate(route);if(route==='repository-picker'&&stage==='edit'){addOpen=true;render();root.querySelector('#repository-picker')?.scrollIntoView({block:'start'})}};
