let ui;
let selectedKey = '';
let selectedStep = 0;
let selectedView = 'story';

const verdictText = {pass:'通过',fail:'失败',unknown:'未知',complete:'完整',partial:'部分',error:'错误',not_evaluated:'未评估'};
const gradeTone = value => ['pass','complete'].includes(value)?'pass':['fail','error'].includes(value)?'fail':'unknown';
const finiteDate = value => { const n=Date.parse(value||''); return Number.isFinite(n)?n:null; };
const duration = ms => ms==null||!Number.isFinite(ms)?'未知':ms<1000?`${ms.toFixed(ms<10?1:0)} ms`:`${(ms/1000).toFixed(ms<10000?1:0)} 秒`;
const unique = values => [...new Set((values||[]).filter(Boolean))];

function all(catalog,kind){
  return (catalog.archives||[]).flatMap(a=>(a[kind]||[]).map(x=>({...x,archive:a.id,archive_name:a.name})));
}
function attributes(raw){
  try{
    const body=typeof raw==='string'?JSON.parse(raw):raw;
    const out={};
    for(const a of body?.attributes||[]){
      const value=a?.value||{};
      if('stringValue' in value)out[a.key]=value.stringValue;
      else if('intValue' in value)out[a.key]=Number(value.intValue);
      else if('doubleValue' in value)out[a.key]=value.doubleValue;
      else if('boolValue' in value)out[a.key]=value.boolValue;
    }
    return out;
  }catch{return {};}
}
function sampleRows(extended){return (extended.samples?.samples||[]).map(x=>x.sample||x);}
function annotationsFor(extended,samples){
  const ids=new Set(samples.map(s=>s.id));
  return (extended.samples?.annotations||[]).filter(a=>ids.has(a.sample_id));
}
function judgmentRows(catalog,extended){
  const fromCatalog=all(catalog,'judgments');
  const fromAPI=extended.evaluations?.judgments||[];
  const map=new Map();
  for(const row of [...fromCatalog,...fromAPI])map.set(row.id,{...row,archive:row.archive||row.source_archive_id});
  return [...map.values()];
}
function orderedSpans(spans){
  const byParent=new Map(), seen=new Set(), result=[];
  for(const span of spans){const p=span.parent_span_id||'';if(!byParent.has(p))byParent.set(p,[]);byParent.get(p).push(span);}
  for(const rows of byParent.values())rows.sort((a,b)=>(finiteDate(a.start)||0)-(finiteDate(b.start)||0));
  const walk=(span,depth)=>{if(seen.has(span.span_id))return;seen.add(span.span_id);result.push({...span,depth:Math.min(depth,4)});for(const child of byParent.get(span.span_id)||[])walk(child,depth+1);};
  for(const span of spans.filter(s=>!s.parent_span_id||!spans.some(p=>p.span_id===s.parent_span_id)))walk(span,0);
  for(const span of spans)walk(span,0);
  return result;
}

function subjects(catalog,extended){
  const result=[];
  const judgments=judgmentRows(catalog,extended), samples=sampleRows(extended);
  for(const row of all(catalog,'trials')){
    const report=row.verifier_reports||{};
    const relatedJudgments=judgments.filter(j=>j.trial_id===row.trial_id&&(j.archive||j.source_archive_id)===row.archive).sort((a,b)=>(finiteDate(b.created_at)||0)-(finiteDate(a.created_at)||0));
    const relatedSamples=samples.filter(s=>s.trial_id===row.trial_id&&s.source_archive===row.archive);
    const events=all(catalog,'events').filter(e=>e.trial_id===row.trial_id&&e.archive===row.archive).sort((a,b)=>(finiteDate(a.occurred_at)||0)-(finiteDate(b.occurred_at)||0));
    result.push({key:`trial:${row.archive}:${row.trial_id}`,type:'trial',archive:row.archive,id:row.trial_id,title:`${verdictText[row.variant_id]||({baseline:'错误基线',candidate:'修正候选','assembly-mismatch':'装配不匹配'}[row.variant_id])||row.variant_id||'固定验收'} · ${row.case_id||'未命名用例'}`,subtitle:report.subject_kind==='synthetic_fixed_product'?'受控固定产物 · 不代表 Agent 执行':'验收 Trial',at:finiteDate(report.finished_at)||0,row,report,events,judgments:relatedJudgments,samples:relatedSamples,annotations:annotationsFor(extended,relatedSamples)});
  }
  const groups=new Map();
  for(const span of all(catalog,'spans')){const key=`${span.archive}:${span.trace_id}`;if(!groups.has(key))groups.set(key,[]);groups.get(key).push(span);}
  for(const spans of groups.values()){
    const ordered=orderedSpans(spans), root=ordered.find(s=>!s.parent_span_id)||ordered[0];
    const archive=root.archive, trace=root.trace_id;
    const relatedJudgments=judgments.filter(j=>j.trace_id===trace&&(j.archive||j.source_archive_id)===archive).sort((a,b)=>(finiteDate(b.created_at)||0)-(finiteDate(a.created_at)||0));
    const relatedSamples=samples.filter(s=>s.trace_id===trace&&s.source_archive===archive);
    const events=all(catalog,'events').filter(e=>e.trace_id===trace&&e.archive===archive).sort((a,b)=>(finiteDate(a.occurred_at)||0)-(finiteDate(b.occurred_at)||0));
    result.push({key:`trace:${archive}:${trace}`,type:'trace',archive,id:trace,title:root.name||'未命名 Trace',subtitle:`OTLP Trace · ${unique(spans.map(s=>s.service)).join('、')||'来源未标识'}`,at:Math.max(...spans.map(s=>finiteDate(s.end)||finiteDate(s.start)||0)),spans:ordered,events,judgments:relatedJudgments,samples:relatedSamples,annotations:annotationsFor(extended,relatedSamples),calls:(extended.calls?.calls||[]).filter(c=>c.trace_id===trace&&c.archive===archive)});
  }
  return result.sort((a,b)=>b.at-a.at);
}

function trialSteps(subject){
  const {report,events,judgments,samples,annotations,row}=subject;
  const checks=report.checks||[], passed=checks.filter(c=>c.verdict==='pass').length, latest=judgments[0];
  const refs=unique(row.evidence_refs||[]), incomplete=events.filter(e=>e.collection_status!=='complete');
  return [
    {title:'受测对象与版本',state:refs.length?'已冻结':'证据缺失',tone:refs.length?'pass':'unknown',summary:report.public_task||'公开任务未记录',meta:`${refs.length} 个证据引用 · ${Object.keys(report.manifest||{}).length?'组合清单可读':'组合清单缺失'}`,detail:{输入:'公开任务与固定组合',输出:report.subject_kind||'受测对象类型未登记',证据:refs.length?`${refs.length} 个不可变引用`:'没有证据引用'},refs},
    {title:'确定性业务验收',state:verdictText[report.verdict]||verdictText[row.verdict]||row.verdict,tone:gradeTone(report.verdict||row.verdict),summary:`${passed} / ${(report.required_checks||checks).length} 项必需检查通过`,meta:`${(report.http_observations||[]).length} 次实际 HTTP 观察 · 验收器 ${report.grader_version||'未登记'}`,detail:{开始:ui.date(report.started_at),完成:ui.date(report.finished_at),实际检查:`${checks.length} 项`,业务结论:verdictText[report.verdict]||report.verdict||'未知'},action:{name:'trial',text:'查看全部检查、请求与组合'}},
    {title:'业务与验收事件',state:incomplete.length?'部分证据':events.length?'完整':'未采集',tone:incomplete.length?'unknown':events.length?'pass':'unknown',summary:events.length?`${events.length} 条带来源的时点事实`:'没有关联事件',meta:incomplete.length?`${incomplete.length} 条事件声明不完整`:'因果前序和证据引用保留',detail:{事件数:String(events.length),完整事件:String(events.length-incomplete.length),部分事件:String(incomplete.length),说明:'时点事件不用于推算运行耗时'},events},
    {title:'Jev Rubric 辅助评分',state:latest?(verdictText[latest.verdict]||latest.verdict):'未运行',tone:latest?gradeTone(latest.verdict):'unknown',summary:latest?`${latest.model} · ${latest.rubric||'rubric 版本未登记'}`:'尚未对该版本发起评分',meta:latest?`${(latest.dimensions||[]).length} 个维度 · ${duration(latest.duration_ms)}`:'业务验收不受影响',detail:latest?{模型:latest.model,评分状态:verdictText[latest.status]||latest.status,缺失证据:(latest.missing_evidence||[]).join('、')||'无',输入版本:latest.subject_revision}:{说明:'可在完整验收详情中发起 Jev 评分'},action:{name:'trial',text:'查看完整 Rubric、概率与用量'}},
    {title:'样本与人工复核',state:annotations.some(a=>a.actor_type==='human'&&a.confirmed)?'已人工确认':samples.length?'待人工确认':'未回流',tone:annotations.some(a=>a.actor_type==='human'&&a.confirmed)?'pass':'unknown',summary:samples.length?`${samples.length} 个固定版本样本 · ${annotations.length} 条标签`:'尚未保存为复核样本',meta:annotations.some(a=>a.actor_type==='ai')?'AI 初标与人工标签分开保留':'没有 AI 初标',detail:{固定样本:String(samples.length),AI初标:String(annotations.filter(a=>a.actor_type==='ai').length),人工确认:String(annotations.filter(a=>a.actor_type==='human'&&a.confirmed).length),版本继承:'新版本不会沿用旧确认'},href:'#samples'}
  ];
}
function traceSteps(subject){
  return subject.spans.slice(0,80).map(span=>{
    const attrs={...attributes(span.resource),...attributes(span.detail)};
    const kind=attrs['gen_ai.span.kind']||attrs['gen_ai.operation.name']||'SPAN';
    const missing=[];
    if(!attrs['repomesh.task_id'])missing.push('task');
    if(kind==='LLM'&&attrs['gen_ai.usage.input_tokens']==null)missing.push('input tokens');
    if(kind==='LLM'&&attrs['gen_ai.usage.output_tokens']==null)missing.push('output tokens');
    return {title:span.name||kind,state:span.status||'状态未记录',tone:String(span.status||'').includes('ERROR')?'fail':'pass',summary:`${span.service||'来源未知'} · ${kind}`,meta:`${duration(span.duration_ms)} · 层级 ${span.depth}`,depth:span.depth,detail:{服务:span.service||'未知',开始:ui.date(span.start),结束:ui.date(span.end),耗时:duration(span.duration_ms),缺失字段:missing.join('、')||'无'},action:{name:'trace',text:'查看完整 Span、属性、事件与 Links'}};
  });
}
function subjectStats(subject){
  if(subject.type==='trial'){
    const start=finiteDate(subject.report.started_at), end=finiteDate(subject.report.finished_at), latest=subject.judgments[0];
    return [
      ['业务结果',verdictText[subject.report.verdict]||verdictText[subject.row.verdict]||subject.row.verdict,gradeTone(subject.report.verdict||subject.row.verdict)],
      ['验收耗时',start!=null&&end!=null?duration(end-start):'未知',''],
      ['模型评审',String(subject.judgments.length),subject.judgments.length?'':'unknown'],
      ['证据状态',latest?.missing_evidence?.length?`${latest.missing_evidence.length} 项缺口`:subject.events.some(e=>e.collection_status!=='complete')?'部分证据':'可追溯',latest?.missing_evidence?.length?'unknown':'pass']
    ];
  }
  const starts=subject.spans.map(s=>finiteDate(s.start)).filter(x=>x!=null), ends=subject.spans.map(s=>finiteDate(s.end)).filter(x=>x!=null);
  const errors=subject.spans.filter(s=>String(s.status).includes('ERROR')).length;
  const exact=subject.calls.filter(c=>c.binding_status==='exact').length;
  return [
    ['执行状态',errors?`${errors} 个错误`:'已接收',errors?'fail':'pass'],
    ['经过时间',starts.length&&ends.length?duration(Math.max(...ends)-Math.min(...starts)):'未知',''],
    ['Span / 调用',`${subject.spans.length} / ${subject.calls.length}`,''],
    ['可信绑定',subject.calls.length?`${exact} / ${subject.calls.length}`:'未解析',exact===subject.calls.length&&subject.calls.length?'pass':'unknown']
  ];
}

function stepMarkup(steps){
  return `<div class="task-story">${steps.map((step,index)=>`<button type="button" class="task-step depth-${step.depth||0} ${index===selectedStep?'active':''}" data-task-step="${index}"><span class="task-dot ${ui.esc(step.tone)}"></span><span class="task-step-name">${ui.esc(step.title)}</span><span><b>${ui.esc(step.summary)}</b><small>${ui.esc(step.meta)}</small></span><span class="task-step-state">${ui.badge(step.state,step.tone)}</span></button>`).join('')}</div>`;
}
function detailMarkup(subject,step){
  const details=Object.entries(step.detail||{}).map(([k,v])=>`<div class="task-detail-row"><span>${ui.esc(k)}</span><strong>${ui.esc(v)}</strong></div>`).join('');
  const eventLinks=(step.events||[]).slice(0,8).map(e=>`<button class="link" data-action="event" data-id="${ui.esc(e.event_id)}" data-archive="${ui.esc(e.archive)}">${ui.esc(e.event_name)}</button>`).join('');
  const refLinks=(step.refs||[]).slice(0,8).map(ref=>`<button class="link" data-action="evidence" data-id="${ui.esc(ref)}" data-archive="${ui.esc(subject.archive)}">${ui.esc(ref)}</button>`).join('');
  const action=step.action?`<button class="primary task-detail-action" data-action="${step.action.name}" data-id="${ui.esc(subject.id)}" data-archive="${ui.esc(subject.archive)}">${ui.esc(step.action.text)}</button>`:'';
  const href=step.href?`<a class="button task-detail-action" href="${step.href}">进入完整样本与标注</a>`:'';
  return `<aside class="panel task-detail" aria-live="polite"><div>${ui.badge(step.state,step.tone)}</div><h2>${ui.esc(step.title)}</h2><p>${ui.esc(step.summary)}</p><div class="task-detail-list">${details}</div>${eventLinks?`<h3>关联事件</h3><div class="task-detail-links">${eventLinks}</div>`:''}${refLinks?`<h3>原始证据</h3><div class="task-detail-links">${refLinks}</div>`:''}${action}${href}</aside>`;
}
function performanceMarkup(subject){
  if(subject.type==='trace'){
    const starts=subject.spans.map(s=>finiteDate(s.start)).filter(x=>x!=null), ends=subject.spans.map(s=>finiteDate(s.end)).filter(x=>x!=null);
    if(!starts.length||!ends.length)return `<section class="panel">${ui.empty('Trace 没有可用的起止时间，不能绘制性能泳道。')}</section>`;
    const min=Math.min(...starts), max=Math.max(...ends), range=Math.max(1,max-min);
    return `<section class="panel"><div class="panel-head"><div><h2>性能泳道</h2><p class="help">所有 Span 使用同一实际时间轴；重叠表示并行。</p></div>${ui.badge(`${duration(range)} 总经过时间`,'neutral')}</div><div class="task-lanes">${subject.spans.slice(0,80).map(span=>{const start=finiteDate(span.start)||min,end=finiteDate(span.end)||start;const startBin=Math.min(10,Math.max(1,Math.floor((start-min)/range*10)+1));const spanBin=Math.min(11-startBin,Math.max(1,Math.round((end-start)/range*10)));return `<div class="task-lane-label">${ui.esc(span.service||span.name)}</div><div class="task-lane-track"><button class="task-lane-bar start-${startBin} span-${spanBin}" data-action="trace" data-id="${ui.esc(subject.id)}" data-archive="${ui.esc(subject.archive)}">${ui.esc(span.name)}</button></div>`;}).join('')}</div></section>`;
  }
  const start=finiteDate(subject.report.started_at),end=finiteDate(subject.report.finished_at),verify=start!=null&&end!=null?end-start:null;
  const rows=[['确定性验收',verify,'实际本地 HTTP 与规则检查'],...subject.judgments.map(j=>[`Jev · ${j.model}`,j.duration_ms,'独立辅助评分'])].filter(x=>x[1]!=null);
  const max=Math.max(1,...rows.map(x=>x[1]));
  return `<section class="panel"><div class="panel-head"><div><h2>观测到的阶段耗时</h2><p class="help">这些阶段不是同一运行时 Trace，不推定并行关系。Agent／工具轨迹未接入时保持缺失。</p></div>${ui.badge('无虚构时间线','neutral')}</div><div class="task-duration-list">${rows.map(([name,ms,note])=>`<div class="task-duration-label"><b>${ui.esc(name)}</b><small>${ui.esc(note)}</small></div><div class="task-duration-track"><progress max="${max}" value="${ms}" aria-label="${ui.esc(name)}耗时"></progress><strong>${duration(ms)}</strong></div>`).join('')}</div><div class="notice">原生 DSH 尚未接通，因此当前 Trial 没有 Agent、模型和工具的完整性能泳道。</div></section>`;
}
function evidenceMarkup(subject){
  if(subject.type==='trace')return `<section class="panel"><div class="panel-head"><div><h2>Trace 证据链</h2><p class="help">原始 Span 完整保留；下方只显示索引关系。</p></div>${ui.badge(`${subject.spans.length} Span`,'neutral')}</div><div class="task-evidence-chain"><div><b>可信任务绑定</b><span>${ui.esc(subject.calls.some(c=>c.binding_status==='exact')?'存在 exact 绑定':'尚无 exact 绑定')}</span></div><i></i><div><b>原始 Span</b><span>${subject.spans.length} 条</span></div><i></i><div><b>Jev 评分</b><span>${subject.judgments.length} 条</span></div><i></i><div><b>复核样本</b><span>${subject.samples.length} 条</span></div></div><button class="primary" data-action="trace" data-id="${ui.esc(subject.id)}" data-archive="${ui.esc(subject.archive)}">查看完整 Span、属性、事件与 Links</button></section>`;
  const refs=unique(subject.row.evidence_refs||[]), latest=subject.judgments[0];
  const chain=[['公开任务',subject.report.public_task?'已记录':'缺失',subject.report.public_task?'pass':'unknown'],['固定组合',Object.keys(subject.report.manifest||{}).length?'可读取':'缺失',Object.keys(subject.report.manifest||{}).length?'pass':'unknown'],['实际 HTTP',`${(subject.report.http_observations||[]).length} 条`,(subject.report.http_observations||[]).length?'pass':'unknown'],['确定性检查',`${(subject.report.checks||[]).length} 项`,gradeTone(subject.report.verdict)],['Jev Rubric',latest?(verdictText[latest.verdict]||latest.verdict):'未运行',latest?gradeTone(latest.verdict):'unknown'],['复核样本',`${subject.samples.length} 条`,subject.samples.length?'pass':'unknown']];
  return `<section class="panel"><div class="panel-head"><div><h2>结论如何得到</h2><p class="help">每一层都保留原始引用；聚合图不改写业务结论。</p></div>${ui.badge(`${refs.length} 个原始证据引用`,'neutral')}</div><div class="task-evidence-chain">${chain.map((x,i)=>`${i?'<i></i>':''}<div><b>${ui.esc(x[0])}</b><span>${ui.badge(x[1],x[2])}</span></div>`).join('')}</div><div class="task-evidence-refs">${refs.map(ref=>`<button class="link" data-action="evidence" data-id="${ui.esc(ref)}" data-archive="${ui.esc(subject.archive)}">${ui.esc(ref)}</button>`).join('')}</div><button class="primary" data-action="trial" data-id="${ui.esc(subject.id)}" data-archive="${ui.esc(subject.archive)}">查看全部检查、请求、评分与原始 JSON</button></section>`;
}

export function renderTaskMap(catalog,extended,helpers){
  ui=helpers;
  const rows=subjects(catalog,extended);
  if(!rows.length)return `<section class="task-map-hero"><div><span class="eyebrow">TASK MAP</span><h2>从一次任务理解全部观测</h2><p>任务地图会聚合 Trace、验收、评分和样本，原始页面与数据仍完整保留。</p></div></section><section class="panel">${ui.empty('还没有可展示的 Trial 或 OTLP Trace。运行本地验收或接入 Trace 后，这里会生成任务故事线。')}</section>`;
  if(!rows.some(x=>x.key===selectedKey)){selectedKey=rows[0].key;selectedStep=0;}
  const subject=rows.find(x=>x.key===selectedKey)||rows[0];
  const steps=subject.type==='trial'?trialSteps(subject):traceSteps(subject);
  selectedStep=Math.max(0,Math.min(selectedStep,Math.max(0,steps.length-1)));
  const stats=subjectStats(subject);
  return `<section class="task-map-hero"><div><span class="eyebrow">TASK MAP</span><h2>${ui.esc(subject.title)}</h2><p>${ui.esc(subject.subtitle)}。聚合视图只做索引，结论和原始数据保持独立。</p></div><label class="task-picker"><span>选择一次执行</span><select id="task-map-subject">${rows.map(x=>`<option value="${ui.esc(x.key)}" ${x.key===subject.key?'selected':''}>${ui.esc(x.title)} · ${ui.esc(x.subtitle)}</option>`).join('')}</select></label></section><div class="task-summary">${stats.map(([name,value,tone])=>`<div class="card"><label>${ui.esc(name)}</label><strong class="task-stat ${ui.esc(tone)}">${ui.esc(value)}</strong></div>`).join('')}</div><div class="task-view-tabs" role="tablist" aria-label="任务观测方式"><button type="button" data-task-view="story" aria-selected="${selectedView==='story'}">任务故事线</button><button type="button" data-task-view="performance" aria-selected="${selectedView==='performance'}">性能泳道</button><button type="button" data-task-view="evidence" aria-selected="${selectedView==='evidence'}">证据链</button></div>${selectedView==='story'?`<div class="task-story-layout"><section class="panel"><div class="panel-head"><div><h2>${subject.type==='trial'?'从受测版本到复核':'完整 Trace 调用链'}</h2><p class="help">点击任一步查看聚合细节；完整数据从右侧继续下钻。</p></div>${ui.badge(subject.type==='trial'?'Trial':'OTLP Trace','neutral')}</div>${stepMarkup(steps)}${subject.type==='trace'&&subject.spans.length>80?`<p class="help">主图展示前 80 个 Span；完整 Trace 保留全部 ${subject.spans.length} 个。</p>`:''}</section>${detailMarkup(subject,steps[selectedStep])}</div>`:selectedView==='performance'?performanceMarkup(subject):evidenceMarkup(subject)}<section class="task-preserved"><div><b>完整数据仍在</b><span>Trace、事件、验收、数据集、指标、Rubric、聚类归因、样本与设置均保留。</span></div><a href="#traces">Trace 与事件</a><a href="#trials">评测与验收</a><a href="#metrics">指标与计量</a><a href="#evaluations">Rubric 评分</a></section>`;
}

export function setupTaskMap(helpers){
  ui=helpers;
  document.addEventListener('change',event=>{
    if(event.target.id!=='task-map-subject')return;
    selectedKey=event.target.value; selectedStep=0; selectedView='story'; helpers.render();
  });
  document.addEventListener('click',event=>{
    const view=event.target.closest('[data-task-view]');
    if(view){selectedView=view.dataset.taskView;helpers.render();return;}
    const step=event.target.closest('[data-task-step]');
    if(step){selectedStep=Number(step.dataset.taskStep)||0;helpers.render();}
  });
}
