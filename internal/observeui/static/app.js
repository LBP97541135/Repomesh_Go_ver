import {setupExtended,refreshExtended,renderExtended,integrationForm,gradeTable,sampleButton,traceButtons,regradeButton,extendedSnapshot} from "./extended.js";
import {renderTaskMap,setupTaskMap} from "./task-map.js";
const $ = (q) => document.querySelector(q);
const esc = (v) => String(v ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const json = (v) => `<pre>${esc(JSON.stringify(v,null,2))}</pre>`;
const short = (v) => esc(String(v ?? '').slice(0,12));
const labels = {pass:'通过',fail:'失败',unknown:'未知',not_evaluated:'未评估',partial:'部分证据',complete:'完整',error:'错误',baseline:'错误基线',candidate:'修正候选','assembly-mismatch':'装配不匹配'};
const badge = (v,tone=v) => `<span class="badge ${['pass','fail','unknown','complete','partial','error'].includes(tone)?tone:'neutral'}">${esc(labels[v]||v)}</span>`;
const date = v => v ? esc(new Date(v).toLocaleString('zh-CN',{hour12:false})) : '—';
const btn = (action, id='', archive='', text='查看', cls='link') => `<button class="${cls}" data-action="${action}" data-id="${esc(id)}" data-archive="${esc(archive)}">${text}</button>`;
let catalog = {archives:[]}, settings = {}, view = location.pathname==='/settings' && !location.hash?'settings':location.hash.slice(1)||'map', busy=false, filter='', traceKind='events', detailToken=0;
const names = {map:'任务地图',overview:'数据概览',traces:'Trace 与事件',trials:'评测与验收',datasets:'数据集',settings:'设置',metrics:'指标与计量',platform:'本地聚类与归因',evaluations:'Rubric 评分',samples:'样本与复核'};
function all(kind) {return catalog.archives.flatMap(a => (a[kind]||[]).map(x=>({...x,archive:a.id,archive_name:a.name})));}
function notice(message,error=false) {$('#notice').innerHTML=message?`<div class="notice ${error?'error':''}">${esc(message)}</div>`:'';}
async function api(path,body,method='POST') {
  const response = await fetch(path,body===undefined?{}:{method,headers:{'Content-Type':'application/json','X-RepoMesh-Local':'1'},body:JSON.stringify(body)});
  const value = await response.json(); if(!response.ok) throw new Error(value.error||`HTTP ${response.status}`); return value;
}
async function refresh() {
  try {const [c,s]=await Promise.all([api('api/catalog'),api('api/settings')]);catalog=c;settings=s;await refreshExtended(api);render();$('#updated').textContent=`更新于 ${new Date(c.updated_at).toLocaleTimeString('zh-CN')}`;}
  catch(e){notice(`读取失败：${e.message}`,true);}
}
const empty = text => `<div class="empty">${text}</div>`;
function trialTable(rows,compact=false) {
  if(!rows.length) return empty('还没有验收结果。运行一次本地固定用例，即可查看检查和实际 HTTP 证据。');
  return `<div class="table-wrap"><table><thead><tr><th>用例 / 版本</th><th>业务验收</th><th>检查</th><th>完成时间</th>${compact?'':'<th>AI 契约评审</th>'}<th></th></tr></thead><tbody>${rows.map(r=>{
    const report=r.verifier_reports, checks=report.checks||[];
    const grades=all('judgments').filter(j=>j.trial_id===r.trial_id&&j.source_archive_id===r.archive).sort((a,b)=>b.created_at.localeCompare(a.created_at));
    return `<tr><td><b>${esc(labels[r.variant_id]||r.variant_id)}</b><div class="subtle">${esc(r.case_id)} · 固定 HTTP 用例</div><code>${short(r.trial_id)}</code></td><td>${badge(r.verdict)}<div class="subtle">${esc(r.grading_status)}</div></td><td>${checks.filter(c=>c.verdict==='pass').length} / ${report.required_checks.length}<div class="subtle">通过 / 必需检查</div></td><td>${date(report.finished_at)}</td>${compact?'':`<td>${grades.length?badge(grades[0].verdict):'<span class="muted">未运行</span>'}<div class="subtle">${grades.length?esc(grades[0].model):'Jev Rubric'}</div></td>`}<td>${btn('trial',r.trial_id,r.archive)}</td></tr>`;
  }).join('')}</tbody></table></div>`;
}
function render() {
  if(!names[view]) view='overview';
  $('#title').textContent=names[view];document.title=`${names[view]} · RepoMesh 本地观测`;
  document.querySelectorAll('nav a').forEach(a=>a.classList.toggle('active',a.hash===`#${view}`));
  const rows=all('trials').sort((a,b)=>b.verifier_reports.finished_at.localeCompare(a.verifier_reports.finished_at));
  const events=all('events'), spans=all('spans'), grades=all('judgments');
  const counts={pass:0,fail:0,unknown:0};rows.forEach(r=>counts[r.verdict]++);
  const faults=catalog.archives.filter(a=>a.error).map(a=>`<div class="notice error">${esc(a.name)}：${esc(a.error)}。此来源未计入结果。</div>`).join('');
  let html='';
  if(view==='map') {
    html=renderTaskMap(catalog,extendedSnapshot(),{esc,date,badge,empty,json});
  } else if(view==='overview') {
    html=`<section class="hero"><div><h2>观测、证据和验收，都在本机</h2><p>从检查结果追溯到实际请求、固定版本与输入证据。Jev Rubric 评分单独记录，业务验收保留原始结论。</p></div>${btn('run','suite','','运行本地验收','primary')}</section>
    <div class="cards"><div class="card"><label>本地事件</label><strong>${events.length}</strong><small>有来源的业务与验收事实</small></div><div class="card"><label>接收的 Span</label><strong>${spans.length}</strong><small>OTLP 落盘记录</small></div><div class="card"><label>验收运行</label><strong>${rows.length}</strong><small>${counts.pass} 通过 · ${counts.fail} 失败 · ${counts.unknown} 未知</small></div><div class="card"><label>AI 评审</label><strong>${grades.length}</strong><small>${settings.model_configured?esc(settings.model):'尚未配置模型'}</small></div></div>
    <div class="columns"><section class="panel"><h2>验收分布</h2>${['pass','fail','unknown'].map((k,i)=>`<div class="stats-row"><span>${badge(k)}</span><progress class="${['','failure','uncertain'][i]}" max="${Math.max(rows.length,1)}" value="${counts[k]}"></progress><strong>${counts[k]}</strong></div>`).join('')}<p class="help">统计单位为独立 Trial。固定产物结果不代表 Agent 的交付成功率。</p></section><section class="panel"><h2>采集状态</h2><div class="source"><span>本地证据与规则验收</span><span class="badge pass">已启用</span></div><div class="source"><span>OTLP HTTP 接收</span><span class="badge pass">已启用</span></div><div class="source"><span>Rubric 评分</span><span class="badge ${settings.model_configured?'pass':'neutral'}">${settings.model_configured?'已配置':'未配置'}</span></div><div class="source"><span>DSH 模型 / 工具运行链路</span><span class="badge unknown">未接入</span></div></section></div>
    <section class="panel"><div class="panel-head"><h2>最近的验收</h2><a class="link" href="#trials">查看全部 →</a></div>${trialTable(rows.slice(0,6),true)}</section>`;
  } else if(view==='trials') {
    html=`<section class="hero"><div><h2>先验证行为，再解释原因</h2><p>三个折扣组合通过实际本地 HTTP 服务验收：错误基线、修正候选和装配不匹配。缺失证据保持未知。</p></div>${btn('run','suite','','运行三个组合','primary')}</section><div class="toolbar"><label for="variant">单独运行</label><select id="variant"><option value="baseline">错误基线</option><option value="candidate">修正候选</option><option value="assembly-mismatch">装配不匹配</option></select>${btn('runSelected','','','运行','secondary')}</div><section class="panel">${trialTable(rows)}</section><p class="help">真实项目使用命令行 collect 采集业务历史；现有固定用例不启动 Agent，不修改共享业务数据库。</p>`;
  } else if(view==='traces') {
    html=`<div class="toolbar"><button data-action="traceKind" data-id="events" class="${traceKind==='events'?'primary':'secondary'}">业务与验收事件 (${events.length})</button><button data-action="traceKind" data-id="spans" class="${traceKind==='spans'?'primary':'secondary'}">OTLP Trace (${new Set(spans.map(s=>s.trace_id)).size})</button><input type="search" id="filter" value="${esc(filter)}" placeholder="搜索名称、项目、Issue、Trace ID…" aria-label="筛选事件或 Trace"></div><section class="panel" id="trace-list"></section><p class="help">已归档的业务事实为时点事件，不据此推算运行耗时。OTLP Span 展示上游实际提供的起止时间；精确身份之外不做时间近似归因。</p>`;
  } else if(view==='datasets') {
    html=`<section class="hero"><div><h2>本地证据数据集</h2><p>每条记录保留 Trial、固定版本、验收报告和证据引用。CSV 可在本机分析，下载不会上传数据。</p></div></section>${catalog.archives.filter(a=>!a.error).map(a=>`<section class="panel"><div class="panel-head"><div><h2>${esc(a.name)}</h2><p class="help">${a.writable?'当前工作档案':'已有档案 · 只读'} · ${(a.trials||[]).length} 条 Trial · ${(a.events||[]).length} 条事件</p></div><a class="button" href="api/dataset.csv?archive=${a.id}">下载 CSV</a></div>${trialTable((a.trials||[]).map(t=>({...t,archive:a.id})),true)}</section>`).join('')}`;
  } else if(['metrics','platform','evaluations','samples'].includes(view)) {
    html=renderExtended(view);
  } else if(view==='settings') {
    html=`<section class="hero"><div><h2>本地模式已就绪</h2><p>观测证据与确定性验收保存在本机。Rubric 评分使用服务端配置的模型。</p></div><span class="badge pass">本地存储</span></section><section class="panel"><h2>Rubric 评分 · Jev / TypeSafe</h2><p>Jev 返回评分等级、概率和置信度；不会生成解释文本，也不会覆盖业务验收。</p><form id="model-form"><label class="field"><span>评分供应商</span><select name="provider"><option value="typesafe" ${settings.provider==='typesafe'?'selected':''}>TypeSafe · Jev</option></select></label><label class="field"><span>固定评分模型</span><input name="model" list="jev-models" value="${esc(settings.model||'jev-1.13.0')}" required autocomplete="off"></label><label class="field"><span>API Key ${settings.model_configured?'· 已保存，留空保留':''}</span><input type="password" name="api_key" autocomplete="new-password" placeholder="${settings.model_configured?'凭据已配置':'只保存到本机私有配置'}"></label><datalist id="jev-models"></datalist><div class="row"><button type="submit" class="primary">保存评分配置</button>${btn('testModel','','','测试连接','secondary')}</div></form><dl class="kv"><dt>评分 API</dt><dd><code>${esc(settings.model_endpoint)}</code></dd><dt>规则</dt><dd>repomesh-evidence/3 · 独立维度与证据充分性</dd><dt>未知处理</dt><dd>缺证和低置信度保持未知，不补零、不补满分</dd></dl><details><summary>当前冻结 rubric</summary>${json(settings.rubric||{})}</details></section>${integrationForm()}<section class="panel"><h2>本地档案与 OTLP</h2><code>${esc(location.origin)}/v1/traces</code>${(settings.archives||[]).map(a=>`<div class="source"><code>${esc(a.path)}</code><span class="badge">${a.writable?'读写':'只读'}</span></div>`).join('')}<p class="help">原生 DSH 尚未接通，已接收的协议数据不能代替真实任务验收。</p></section>`;
  }
  $('#content').innerHTML=faults+html;
  if(view==='traces') renderTraces();
  if(busy) document.querySelectorAll('button[data-action^="run"],button[data-action="judge"],#model-form button').forEach(b=>b.disabled=true);
}
function renderTraces() {
  const q=filter.toLowerCase();
  if(traceKind==='events') {
    const events=all('events').filter(e=>[e.event_name,e.project_id,e.issue_id,e.trace_id,e.trial_id,e.work_id].some(v=>String(v||'').toLowerCase().includes(q))).reverse();
    $('#trace-list').innerHTML=events.length?`<div class="table-wrap"><table><thead><tr><th>事件</th><th>归属</th><th>结论 / 完整性</th><th>发生时间</th><th></th></tr></thead><tbody>${events.slice(0,300).map(e=>`<tr><td><b>${esc(e.event_name)}</b><div class="subtle">${esc(e.check_id||e.producer)}</div><code>${short(e.trace_id)}</code></td><td><code>${esc(e.issue_id||e.trial_id||e.work_id)}</code><div class="subtle">${esc(e.project_id)}</div></td><td>${badge(e.outcome_verdict)} ${badge(e.collection_status)}</td><td>${date(e.occurred_at)}</td><td>${btn('event',e.event_id,e.archive)}</td></tr>`).join('')}</tbody></table></div><p class="help">匹配 ${events.length} 条，最多展示 300 条。可通过搜索缩小范围。</p>`:empty('暂无匹配事件。通过 collect 命令采集历史，或运行一次本地验收。');
  } else {
    const grouped=new Map();all('spans').forEach(s=>{const k=s.archive+':'+s.trace_id;if(!grouped.has(k))grouped.set(k,[]);grouped.get(k).push(s);});
    const groups=[...grouped.values()].filter(g=>g.some(s=>[s.name,s.trace_id,s.service].some(v=>v.toLowerCase().includes(q)))).reverse();
    $('#trace-list').innerHTML=groups.length?`<div class="table-wrap"><table><thead><tr><th>Trace / 根 Span</th><th>服务</th><th>Span 数</th><th>起始时间</th><th></th></tr></thead><tbody>${groups.slice(0,300).map(g=>{const root=g.find(s=>!s.parent_span_id)||g[0];return `<tr><td><b>${esc(root.name)}</b><div><code>${esc(root.trace_id)}</code></div></td><td>${esc([...new Set(g.map(s=>s.service))].join(', '))}</td><td>${g.length}</td><td>${date(g[0].start)}</td><td>${btn('trace',root.trace_id,root.archive)}</td></tr>`;}).join('')}</tbody></table></div>`:empty('还没有接收 Trace。把本机 OTLP 导出目标配置为设置页中的 /v1/traces 地址。');
  }
}
function show(title,html) {detailToken++;$('#detail-title').textContent=title;$('#detail-body').innerHTML=html;if(!$('#detail').open) $('#detail').showModal();}
function refs(archive,values) {return [...new Set(values||[])].map(ref=>`<p>${btn('evidence',ref,archive,esc(ref))}</p>`).join('');}
function showTrial(id,archive) {
  const row=all('trials').find(t=>t.trial_id===id&&t.archive===archive);if(!row)return;
  const r=row.verifier_reports;
  const grades=all('judgments').filter(j=>j.trial_id===id&&j.source_archive_id===archive).sort((a,b)=>b.created_at.localeCompare(a.created_at));
  show(`验收 · ${labels[r.variant_id]||r.variant_id}`,`<div class="row spaced"><div>${badge(r.verdict)} <code>${esc(id)}</code></div>${btn('judge',id,archive,'Jev Rubric 评分','primary')}${grades.length?regradeButton(id,'',archive,row.evidence_refs[1]):''}${sampleButton(id,archive,row.evidence_refs[1],r.verdict,grades[0])}</div><p class="help">评分会把这条固定证据发送至配置的评分供应商，产生调用；业务验收结果保持独立。</p><dl class="kv"><dt>公开任务</dt><dd>${esc(r.public_task)}</dd><dt>验收器</dt><dd>${esc(r.grader_version)}</dd><dt>开始 / 完成</dt><dd>${date(r.started_at)} → ${date(r.finished_at)}</dd><dt>受测对象</dt><dd>${esc(r.subject_kind)}</dd></dl><h3>逐项检查</h3><div class="table-wrap"><table><thead><tr><th>检查</th><th>结果</th><th>期望</th><th>实际</th></tr></thead><tbody>${r.checks.map(c=>`<tr><td><code>${esc(c.check_id)}</code><div class="subtle">${esc(c.reason_code||'')}</div></td><td>${badge(c.verdict)}</td><td><code>${esc(JSON.stringify(c.expected))}</code></td><td><code>${esc(JSON.stringify(c.actual))}</code></td></tr>`).join('')}</tbody></table></div><div class="divider"></div><h3>AI 评审记录（独立于业务验收）</h3>${grades.length?grades.map(g=>`<article><div class="row">${badge(g.verdict)} ${badge(g.status)} <small>${esc(g.model)} · ${date(g.created_at)}</small></div><p>${esc(g.explanation)}</p>${gradeTable(g)}${g.missing_evidence?.length?`<p class="help">缺失：${esc(g.missing_evidence.join('、'))}</p>`:''}<details><summary>用量和固定输入</summary>${json(g.usage||{})}${refs(g.archive,[g.input_ref,...g.raw_ref?[g.raw_ref]:[]])}</details></article>`).join(''):empty('尚未运行模型评审。')}<div class="divider"></div><h3>证据与版本</h3>${refs(archive,row.evidence_refs)}<details><summary>实际 HTTP 请求和响应 (${r.http_observations.length})</summary>${json(r.http_observations)}</details><details><summary>固定受测组合</summary>${json(r.manifest)}</details><details><summary>本次观察的限制</summary>${json(r.limitations)}</details>`);
  if(busy) $('#detail-body button[data-action="judge"]').disabled=true;
}
function showEvent(id,archive) {
  const e=all('events').find(e=>e.event_id===id&&e.archive===archive);if(!e)return;
  show(e.event_name,`<div class="row">${badge(e.outcome_verdict)} ${badge(e.collection_status)} <span class="badge">时点事实</span></div><dl class="kv"><dt>Trace ID</dt><dd><code>${esc(e.trace_id)}</code></dd><dt>项目 / Issue</dt><dd>${esc(e.project_id||'—')} / ${esc(e.issue_id||'—')}</dd><dt>Trial</dt><dd>${e.trial_id?btn('trial',e.trial_id,e.archive,esc(e.trial_id)):'—'}</dd><dt>缺失说明</dt><dd>${esc(e.missing_reason||'无')}</dd></dl><h3>上下文与证据</h3>${refs(archive,[e.context_manifest_ref,...e.evidence_refs,...e.input_artifact_refs])}<h3>因果前序</h3>${e.caused_by_event_ids.map(id=>`<p>${btn('event',id,archive,short(id))}</p>`).join('')||'<p class="help">未记录前序事件。</p>'}<details><summary>完整事件</summary>${json(e)}</details>`);
}
async function showTrace(id,archive) {
  const detail=await api(`api/trace?archive=${encodeURIComponent(archive)}&id=${encodeURIComponent(id)}`);
  const spans=all('spans').filter(s=>s.trace_id===id&&s.archive===archive);const seen=new Set(),ordered=[];
  function walk(s,depth) {if(seen.has(s.span_id))return;seen.add(s.span_id);ordered.push([s,Math.min(depth,4)]);spans.filter(x=>x.parent_span_id===s.span_id).forEach(x=>walk(x,depth+1));}
  spans.filter(s=>!spans.some(p=>p.span_id===s.parent_span_id)).forEach(s=>walk(s,0));spans.forEach(s=>walk(s,0));
  show(`Trace · ${id}`,`${traceButtons(id,archive,detail.revision,detail.grading_eligible)}<p class="help">${spans.length} 个 Span。仅按上游 parent_span_id 展示父子关系，未收到的父 Span 不补造。</p><div class="timeline">${ordered.map(([s,d])=>`<article data-depth="${d}"><div class="row spaced"><b>${esc(s.name)}</b><small>${s.duration_ms.toFixed(3)} ms</small></div><p class="subtle">${esc(s.service)} · ${esc(s.status)} · ${date(s.start)}</p><code>${esc(s.span_id)}</code><details><summary>属性、事件与关联</summary>${json({resource:s.resource,scope:s.scope,span:s.detail})}</details></article>`).join('')}</div>`);
}
async function perform(button) {
  const {action,id,archive}=button.dataset;
  try {
    if(action==='trial') return showTrial(id,archive);
    if(action==='event') return showEvent(id,archive);
    if(action==='trace') return showTrace(id,archive);
    if(action==='traceKind') {traceKind=id;render();return;}
    if(action==='evidence') {const token=++detailToken;const data=await api(`api/evidence?archive=${encodeURIComponent(archive)}&ref=${encodeURIComponent(id)}`);if(token===detailToken)show('原始证据',json(data));return;}
    if(busy)return;busy=true;button.disabled=true;
    if(action==='run'||action==='runSelected') {
      notice('正在本机运行 HTTP 验收并保存证据…');
      const result=await api('api/trials',{variant:action==='runSelected'?$('#variant').value:id});
      notice(`已完成 ${result.trials.length} 次验收，结果已保存在本机。`);await refresh();
    } else if(action==='testModel') {
      notice('正在检查评分供应商连接与模型列表…');const form=document.querySelector('#model-form');if(form.querySelector('[name=api_key]').value||form.querySelector('[name=model]').value!==settings.model)throw new Error('请先保存模型和 Key，再测试连接。');const result=await api('api/model/test',{});document.querySelector('#jev-models').innerHTML=result.models.map(m=>`<option value="${esc(m)}"></option>`).join('');
      notice(result.ok?(result.message||`连接成功，${settings.model} 可用。`):`凭据有效，但所选模型不在列表中。可用模型：${result.models.join('、')}`,!result.ok);
    } else if(action==='judge') {
      notice('正在按 rubric 评审固定证据…');const result=await api('api/judge',{archive,trial_id:id});
      notice(result.status==='complete'?`AI 评审已保存：${labels[result.verdict]}。业务验收结果保持原值。`:`评审未完成，已保存 unknown：${result.explanation}`,result.status!=='complete');await refresh();showTrial(id,archive);
    }
  } catch(e){notice(e.message,true);} finally{busy=false;button.disabled=false;document.querySelectorAll('button[data-action^="run"]:disabled,button[data-action="judge"]:disabled,#model-form button:disabled').forEach(b=>b.disabled=false);}
}
document.addEventListener('click',e=>{const b=e.target.closest('button[data-action]');if(b)void perform(b);});
document.addEventListener('input',e=>{if(e.target.id==='filter'){filter=e.target.value;renderTraces();}});
document.addEventListener('submit',async e=>{
  if(e.target.id!=='model-form')return;e.preventDefault();if(busy)return;busy=true;
  const values=new FormData(e.target);const key=e.target.querySelector('[name=api_key]');
  try{await api('api/model',{provider:values.get('provider'),model:values.get('model'),api_key:values.get('api_key')},'PUT');key.value='';notice('评分配置已保存到本机私有文件。');await refresh();}
  catch(err){notice(err.message,true);}finally{key.value='';busy=false;document.querySelectorAll('button[data-action^="run"]:disabled,button[data-action="judge"]:disabled,#model-form button:disabled').forEach(b=>b.disabled=false);}
});
$('#close-detail').onclick=()=>{detailToken++;$('#detail').close();};
$('#detail').addEventListener('cancel',()=>detailToken++);
$('#refresh').onclick=()=>void refresh();
window.addEventListener('hashchange',()=>{view=location.hash.slice(1)||'map';filter='';render();});
setupExtended({api,show,notice,refresh,esc,json});
setupTaskMap({render,show,notice,refresh,esc,date,badge,empty,json});
void refresh();
