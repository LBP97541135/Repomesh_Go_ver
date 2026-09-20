let ui;
let data={status:{config:{mode:'local',links:{}}},metrics:{groups:{}},calls:{calls:[]},evaluations:{judgments:[],platform_results:[]},samples:{samples:[],annotations:[]},assistant:{},analyses:{analyses:[]},online:{policy:{}},faults:[]};
export function extendedSnapshot(){return data;}
export async function refreshExtended(api){
  const endpoints={status:'/api/status',metrics:'/api/metrics',calls:'/api/calls',evaluations:'/api/evaluations',samples:'/api/samples',assistant:'/api/assistant',analyses:'/api/analyses',online:'/api/evaluation-policy'};
  data.faults=[];
  await Promise.all(Object.entries(endpoints).map(async([key,url])=>{try{data[key]=await api(url);}catch(e){data[key]={};data.faults.push(`${key}: ${e.message}`);}}));
}
const number=v=>v==null?'未知':Number(v).toLocaleString('zh-CN',{maximumFractionDigits:3});
const op=(action,label,values={})=>`<button data-op="${action}" ${Object.entries(values).map(([k,v])=>`data-${k}="${ui.esc(v)}"`).join(' ')}>${ui.esc(label)}</button>`;
export function gradeTable(g){
  if(!g.dimensions?.length)return '';
  const {esc,json}=ui;
  return `<div class="table-wrap"><table><thead><tr><th>Rubric 维度</th><th>辅助判断</th><th>归一化分数</th><th>置信度</th><th>采用情况</th></tr></thead><tbody>${g.dimensions.map(d=>`<tr><td>${esc(d.title)}</td><td>${esc(d.verdict)}</td><td>${number(d.score)}</td><td>${number(d.confidence)}</td><td>${esc(d.reason_code)}${d.missing_fields?.length?`<div>${esc(d.missing_fields.join('、'))}</div>`:''}</td></tr>`).join('')}</tbody></table></div><p class="help">分数为原始 Score ÷（等级数 − 1），仅作辅助诊断。阈值尚需领域校准；确定性验收与交付批准保持独立。</p><details><summary>完整 rubric、原始等级、概率及证据充分性判断</summary>${json({rubric:g.rubric_snapshot,answers:g.typed_answers})}</details>`;
}
export function regradeButton(trial,trace,archive,revision){return op('regrade','Jev 重新评分',{trial,trace,archive,revision});}
export function sampleButton(trial,archive,revision,verdict,judgment){let reason=verdict==='fail'?'verification_failed':'representative_sample';if(judgment?.verdict==='fail')reason=verdict==='pass'?'disagreement':'low_score';else if(judgment?.verdict==='pass')reason='high_score_audit';return op('sample','保存为复核样本',{trial,archive,revision,reason,judgment:judgment?.subject_revision===revision?judgment.id:''});}
export function traceButtons(trace,archive,revision,eligible){return `<div class="row">${eligible?op('judgeTrace','Jev Rubric 评分',{trace,archive,revision}):''}${eligible?regradeButton('',trace,archive,revision):''}${eligible?op('sample','保存 Trace 样本',{trace,archive,revision}):''}</div>`;}

export function integrationForm(){
  const {esc}=ui,c=data.assistant||{};
  return `${onlinePolicyForm()}<section class="panel"><h2>本地分析助手 · DeepSeek</h2><p>聚类、归因和 AI 初标由本机流程调用 DeepSeek；Rubric 数值评分仍由 Jev 完成。档案、数据集和人工标注均保存在本机。</p><form id="assistant-form"><label class="field"><span>DeepSeek 模型</span><input name="model" list="assistant-models" value="${esc(c.model||'deepseek-flash')}" required></label><label class="field"><span>DeepSeek API Key ${c.configured?'· 已保存':''}</span><input name="api_key" type="password" autocomplete="new-password" placeholder="${c.configured?'留空保留现有密钥':'仅写本机私有配置'}"></label><datalist id="assistant-models"></datalist><div class="row"><button class="primary">保存分析配置</button>${op('testAssistant','测试连接 / 读取可用模型')}</div></form><p class="help">无需 AgentSpace、阿里云凭据或云控制台。只有显式启用的模型请求出站。</p></section>`;
}

function onlinePolicyForm(){
  const p=data.online.policy||{};
  return `<section class="panel"><h2>本地在线评估</h2><p>完成且轨迹完整的执行由本机调度 Jev 评分。默认关闭；Judge 自身不会再次触发评估。</p><form id="online-form"><label class="field"><input type="checkbox" name="enabled" ${p.enabled?'checked':''}> 启用本地自动评分（产生 Jev API 调用）</label><label class="field"><span>每日新付费调用上限（Jev + DeepSeek）</span><input name="daily_call_limit" type="number" min="1" max="100" value="${p.daily_call_limit||10}"></label><label class="field"><span>终态后的等待窗口（秒）</span><input name="late_window_seconds" type="number" min="0" max="3600" value="${p.late_window_seconds??5}"></label><label class="field"><input type="checkbox" name="allow_fixtures" ${p.allow_fixtures?'checked':''}> 显式允许受控协议样本（不算真实执行）</label><button class="primary">保存本地调度</button></form><p class="help">付费请求前保存尝试身份。超时或进程中断不自动重复付费，可手动重评。当前预约 ${data.online.paid_attempt_count??(data.online.claims||[]).length} 次。</p></section>`;
}

function analysisView(a){
  const {esc,json}=ui;const result=a.result||{};
  if(a.kind==='clustering')return `<div class="columns">${(result.clusters||[]).map(c=>`<article class="panel"><h3>${esc(c.name)}</h3><p>${c.sample_ids.length} 条样本</p><details><summary>查看成员</summary>${c.sample_ids.map(id=>`<p><code>${esc(id)}</code></p>`).join('')}</details></article>`).join('')}</div><p>离群样本：${(result.outliers||[]).length} 条。小于 2 条的主题由代码归入离群项。</p><details><summary>固定分组与来源</summary>${json(result)}</details>`;
  return `<p><span class="badge">${esc(result.category||'未知')}</span></p><p>${esc(result.summary||'等待分析结果')}</p>${result.missing_evidence?.length?`<p class="help">仍缺材料：${esc(result.missing_evidence.join('；'))}</p>`:''}<details><summary>证据引用</summary>${json(result.evidence_ids||[])}</details>`;
}

export function renderExtended(view){
  const {esc,json}=ui;
  const faults=data.faults.map(e=>`<div class="notice error">${esc(e)}</div>`).join('');
  if(view==='metrics'){
    const groups=data.metrics.groups||{},rows=data.calls.calls||[];
    return faults+`<section class="hero"><div><h2>模型计量与覆盖</h2><p>仅统计已采到的物理调用。缺失用量不补零；未取得独立总数时，整体采集覆盖率保持未知。</p></div></section><section class="panel"><h2>按样本来源、用途与模型汇总</h2><p class="help">团队执行、验证和 Judge 分开；Token 为已知部分的小计。费用：${number(data.metrics.cost)}。</p><div class="table-wrap"><table><thead><tr><th>来源 / 用途 / 模型</th><th>调用</th><th>输入 / 输出 Token</th><th>用量已采</th><th>TTFT 均值 ms</th><th>TTFT 已采</th><th>总耗时均值 ms</th></tr></thead><tbody>${Object.entries(groups).map(([key,g])=>`<tr><td>${esc(key)}</td><td>${g.calls}</td><td>${number(g.known_input_tokens)} / ${number(g.known_output_tokens)}</td><td>${g.measured_usage_calls} / ${g.calls}</td><td>${number(g.mean_ttft_ms)}</td><td>${g.measured_ttft_calls} / ${g.calls}</td><td>${number(g.mean_duration_ms)}</td></tr>`).join('')||'<tr><td colspan="7">尚未收到模型计量。业务时点事件不会作为运行耗时。</td></tr>'}</tbody></table></div>${(data.metrics.errors||[]).map(e=>`<p class="notice error">${esc(e.error)}</p>`).join('')}</section><section class="panel"><h2>物理调用明细</h2><form id="calls-filter" class="toolbar"><input name="q" type="search" placeholder="搜索模型、工具、Issue 或 Trace"><select name="purpose"><option value="">全部用途</option><option value="team">团队执行</option><option value="verifier">验证</option><option value="judge">评估器</option><option value="analysis">DeepSeek 分析</option></select><button>筛选</button></form><div id="calls-table">${callsTable(rows)}</div></section>`;
  }
  if(view==='platform'){
    const status=data.status,rows=data.analyses.analyses||[],samples=data.samples.samples||[];
    return faults+`<section class="hero"><div><h2>本地聚类与归因</h2><p>分析任务和结果保存在本机。Jev 负责评分，DeepSeek 负责主题归组、证据归因和 AI 初标。</p></div><span class="badge pass">全本地观测</span></section><section class="panel"><h2>接入状态</h2><dl class="kv"><dt>本地 OTLP Span</dt><dd>${number(status.otlp_received_spans)}</dd><dt>DeepSeek 分析</dt><dd>${data.assistant.configured?'已配置':'未配置'}</dd><dt>原生 DSH</dt><dd>${esc(status.runtime?.native_dsh||'未接通')}</dd><dt>云空间依赖</dt><dd>无</dd></dl><details><summary>已观测来源与缺口</summary>${json(status.sources||{})}${json(status.errors||[])}${json(status.capture_health||[])}${json(status.paid_attempts||[])}</details></section><section class="panel"><h2>按需求场景聚类</h2><p class="help">至少选择 4 条、最多 30 条固定版本样本，每簇至少 2 条。样本不足时不造图；主题聚类不是失败根因。</p><form id="cluster-form">${samples.map(({sample:s})=>`<label class="field"><input type="checkbox" name="sample" value="${esc(s.id)}"> ${esc(s.trial_id||s.trace_id)} · ${esc(s.reason)}</label>`).join('')||'<p>请先从 Trace 或验收详情保存样本。</p>'}<button class="primary" ${samples.length<4?'disabled':''}>运行本地聚类</button></form></section>${rows.map(a=>`<section class="panel"><h2>${a.kind==='clustering'?'主题聚类':'证据归因'} · ${esc(a.status)}</h2><p class="help">${esc(a.model)} · ${esc(a.created_at)} · ${number(a.duration_ms)} ms</p>${a.error?`<p class="notice error">${esc(a.error)}</p>`:analysisView(a)}<details><summary>固定样本与模型消耗</summary>${json({sample_ids:a.sample_ids,usage:a.usage})}</details></section>`).join('')}`;
  }

  if(view==='evaluations'){
    return faults+`<section class="hero"><div><h2>Jev Rubric 与本地评估</h2><p>评分输入、规则版本、原始概率和每次重评独立保存；分数不会覆盖业务验收。</p></div></section><section class="panel"><h2>Jev 与历史模型评审</h2>${(data.evaluations.judgments||[]).map(g=>`<article class="grade-card"><h3>${esc(g.model)} · ${esc(g.verdict)}</h3><p>${esc(g.explanation)}</p><code>${esc(g.trial_id||g.trace_id)}</code>${gradeTable(g)}<details><summary>输入修订与评估器消耗</summary>${json({revision:g.subject_revision,purpose:g.execution_purpose||'judge',duration_ms:g.duration_ms,usage:g.usage})}</details></article>`).join('')||'<p class="empty">在具体验收或 Trace 详情中发起 Jev 评分。</p>'}</section><section class="panel"><h2>历史结果导入</h2><p class="help">导入是操作员映射的真实导出记录，不代表 API 自动同步或完成来源核验。执行 success 与业务通过不同。</p>${json(data.evaluations.platform_results||[])}<details><summary>历史平台格式结果</summary>${json(data.evaluations.legacy_platform_results||[])}</details><label class="field"><span>导入结果与冻结评估器（repomesh.platform-import/1）</span><input type="file" accept="application/json,.json" data-import="platform"></label></section>`;
  }
  if(view==='samples'){
    return faults+`<section class="hero"><div><h2>样本与标注复核</h2><p>从真实 Trace、低分或高分抽检保存固定版本。AI 初标与人工确认分别留痕，新产物不会继承旧标签。</p></div></section>${(data.samples.samples||[]).map(({sample:s,evidence_available:available})=>{const annotations=(data.samples.annotations||[]).filter(a=>a.sample_id===s.id);return `<section class="panel"><h2>${esc(s.trial_id||s.trace_id)}</h2><p>${esc((data.samples.selections||[]).filter(x=>x.sample_id===s.id).map(x=>x.reason).join('、')||s.reason)} · ${esc(s.subject_kind||'来源类型未登记')} · ${available?'证据可读':'证据不可用'}</p><code>${esc(s.subject_revision)}</code><div class="row"><a class="button" href="/api/samples/export?id=${encodeURIComponent(s.id)}">下载自包含样本</a>${op('attribute','DeepSeek 归因与初标',{sample:s.id,revision:s.subject_revision})}${op('reviewSample','人工确认 / 纠正',{sample:s.id,revision:s.subject_revision})}</div>${annotations.length?annotations.map(a=>`<article class="grade-card"><h3>${esc(a.actor_type)} · ${esc(a.actor_id)} · ${esc(a.verdict)}</h3><p>${esc(a.reason)}</p><p class="help">${a.confirmed?'人工确认':'尚未人工确认'} · ${esc(a.template_version)} · ${esc(a.provenance)}</p><details><summary>标注版本与来源</summary>${json(a)}</details></article>`).join(''):'<p class="help">待标注。可运行 DeepSeek 初标，再由人确认或纠正。</p>'}</section>`;}).join('')||'<section class="panel"><p class="empty">尚无样本。在验收或 Trace 详情选择“保存为复核样本”。</p></section>'}<section class="panel"><h2>回读平台标注</h2><label class="field"><span>标注导出与身份映射（repomesh.annotation-import/1）</span><input type="file" accept="application/json,.json" data-import="annotation"></label><p class="help">导入必须带样本修订、dataset/item、标注者类型、模板版本和原始导出。AI 标签不能标成人工确认。</p></section>`;
  }
  return '';
}

function callsTable(rows){
  const {esc}=ui;
  return `<div class="table-wrap"><table><thead><tr><th>调用</th><th>任务归属</th><th>用途</th><th>耗时 / TTFT ms</th><th>输入 / 输出 Token</th><th>缺失</th></tr></thead><tbody>${rows.map(c=>`<tr><td>${esc(c.name)}<div>${esc(c.model||c.tool)}</div><code>${esc(c.call_id)}</code></td><td>${esc(c.issue_id||'未知')}<div>${esc(c.attempt_id)}</div><span class="badge">${esc(c.binding_status)}</span></td><td>${esc(c.execution_purpose)}</td><td>${number(c.duration_ms)} / ${number(c.ttft_ms)}<div>${esc(c.ttft_reason)}</div></td><td>${number(c.input_tokens)} / ${number(c.output_tokens)}</td><td>${esc((c.missing_fields||[]).join('、'))}</td></tr>`).join('')||'<tr><td colspan="6">暂无匹配调用</td></tr>'}</tbody></table></div>`;
}

export function setupExtended(helpers){
  ui=helpers;
  document.addEventListener('click',async e=>{
    const b=e.target.closest('button[data-op]');if(!b||b.disabled)return;
    b.disabled=true;
    try{
      const {op:action,trial,trace,archive,revision,sample:sampleID}=b.dataset;
      if(action==='testAssistant'){
        const form=document.querySelector('#assistant-form');
        if(form.querySelector('[name=api_key]').value||form.querySelector('[name=model]').value!==data.assistant.model)throw new Error('请先保存模型和 Key，再测试连接。');
        const result=await ui.api('/api/assistant/test',{});document.querySelector('#assistant-models').innerHTML=result.models.map(m=>`<option value="${ui.esc(m)}"></option>`).join('');ui.notice(result.ok?result.message:`连接成功，但所选模型不在列表中：${result.models.join('、')}`,!result.ok);
      }else if(action==='regrade'){
        const storageKey=`repomesh:pending-grade:${archive}:${trial||trace}:${revision}`;
        const attempt=sessionStorage.getItem(storageKey)||crypto.randomUUID();sessionStorage.setItem(storageKey,attempt);
        ui.notice('正在创建新的 Jev 评分尝试…');const result=await ui.api('/api/judge',{archive,trial_id:trial||'',trace_id:trace||'',expected_revision:revision,attempt_key:attempt});sessionStorage.removeItem(storageKey);ui.notice(`新评分已保存：${result.verdict}`);await ui.refresh();ui.show('Jev 评分',`<p>${ui.esc(result.explanation)}</p>${gradeTable(result)}`);
      }else if(action==='attribute'){
        ui.notice('正在用 DeepSeek 分析固定证据…');const a=await ui.api('/api/analyses/attribution',{sample_id:sampleID,expected_revision:revision});ui.notice(a.status==='complete'?'AI 初标已保存，等待人工确认。':a.error,a.status!=='complete');await ui.refresh();
      }else if(action==='reviewSample'){
        const labels=(data.samples.annotations||[]).filter(a=>a.sample_id===sampleID);const previous=labels.find(a=>a.actor_type==='human'&&a.confirmed)||labels[0];
        ui.show('人工复核固定样本',`<p>此决定只适用于该样本版本，不批准代码交付。</p><code>${ui.esc(revision)}</code><form id="sample-review"><input type="hidden" name="sample_id" value="${ui.esc(sampleID)}"><input type="hidden" name="expected_revision" value="${ui.esc(revision)}"><input type="hidden" name="supersedes" value="${ui.esc(previous?.id||'')}"><label class="field"><span>复核人</span><input name="actor" required autocomplete="name"></label><label class="field"><span>质量判断</span><select name="verdict"><option value="unknown">证据不足 / 未知</option><option value="pass">正确</option><option value="fail">错误</option></select></label><label class="field"><span>失败类别</span><select name="category">${['insufficient','none','scope','contract','dependency','tool','implementation','assembly'].map(k=>`<option value="${k}">${k}</option>`).join('')}</select></label><label class="field"><span>证据与修正理由</span><textarea name="reason" rows="4" required></textarea></label><button class="primary">保存人工确认</button></form>`);
      }else if(action==='judgeTrace'){
        ui.notice('正在调用 Jev 对固定 Trace 评分…');const g=await ui.api('/api/judge',{archive,trace_id:trace,expected_revision:revision});ui.notice(`评分记录已保存：${g.verdict}。`);await ui.refresh();ui.show('Jev Rubric 评分',`<p>${ui.esc(g.explanation)}</p>${gradeTable(g)}`);
      }else if(action==='sample'){
        const sample=await ui.api('/api/samples',{archive,trial_id:trial||'',trace_id:trace||'',expected_revision:revision,reason:b.dataset.reason||'selected_trace',judgment_id:b.dataset.judgment||''});ui.notice(`已冻结样本 ${sample.id.slice(0,12)}，可在“样本与复核”查看。`);await ui.refresh();
      }
    }catch(err){ui.notice(err.message,true);}finally{b.disabled=false;}
  });
  document.addEventListener('submit',async e=>{
    if(!['assistant-form','calls-filter','sample-review','cluster-form','online-form'].includes(e.target.id))return;e.preventDefault();const form=new FormData(e.target);
    try{
      if(e.target.id==='online-form'){await ui.api('/api/evaluation-policy',{enabled:form.has('enabled'),allow_fixtures:form.has('allow_fixtures'),daily_call_limit:Number(form.get('daily_call_limit')),late_window_seconds:Number(form.get('late_window_seconds'))},'PUT');ui.notice('本地在线评估策略已保存。');await ui.refresh();}
      else if(e.target.id==='assistant-form'){const key=e.target.querySelector('[name=api_key]');try{await ui.api('/api/assistant',{model:form.get('model'),api_key:form.get('api_key')},'PUT');ui.notice('DeepSeek 分析配置已保存。');await ui.refresh();}finally{key.value='';}}
      else if(e.target.id==='sample-review'){await ui.api('/api/annotations',Object.fromEntries(form));document.querySelector('#detail').close();ui.notice('人工标签已追加保存，原始 AI 初标和旧标签保留。');await ui.refresh();}
      else if(e.target.id==='cluster-form'){ui.notice('正在对固定样本做主题归组…');const result=await ui.api('/api/analyses/clustering',{sample_ids:form.getAll('sample')});ui.notice(result.status==='complete'?'本地聚类已保存。':result.error,result.status!=='complete');await ui.refresh();}
      else{const result=await ui.api('/api/calls?'+new URLSearchParams({q:String(form.get('q')||''),purpose:String(form.get('purpose')||'')}));document.querySelector('#calls-table').innerHTML=callsTable(result.calls||[])+`<p class="help">匹配 ${result.total} 条，本页 ${result.calls.length} 条；可继续缩小筛选。</p>`;}
    }catch(err){ui.notice(err.message,true);}
  });
  document.addEventListener('change',async e=>{
    const kind=e.target.dataset.import;if(!kind)return;const file=e.target.files?.[0];if(!file)return;
    try{if(file.size>1024*1024)throw new Error('导入文件上限为 1 MiB');const value=JSON.parse(await file.text());await ui.api(kind==='platform'?'/api/platform/import':'/api/annotations/import',value);ui.notice('记录已按样本版本导入，原始评分与标注历史保留。');await ui.refresh();}catch(err){ui.notice(err.message,true);}finally{e.target.value='';}
  });
}
