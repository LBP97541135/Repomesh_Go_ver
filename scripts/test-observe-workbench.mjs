// Exercise an explicitly selected running local instance; creates one protocol
// fixture Trace and three deterministic trials.
// PLAYWRIGHT_MODULE may point to an existing local Playwright installation.
import assert from 'node:assert/strict';
import {randomBytes} from 'node:crypto';
import fs from 'node:fs/promises';
import path from 'node:path';
const {chromium}=await import(process.env.PLAYWRIGHT_MODULE||'playwright');
const base=process.env.REPOMESH_WORKBENCH_TEST_URL||'http://127.0.0.1:18090';
assert(['127.0.0.1','localhost','[::1]'].includes(new URL(base).hostname));
const output=process.env.REPOMESH_WORKBENCH_TEST_OUTPUT;
assert(output,'set REPOMESH_WORKBENCH_TEST_OUTPUT to a private artifacts directory');
await fs.mkdir(output,{recursive:true,mode:0o700});
const browser=await chromium.launch({headless:true});
const page=await browser.newPage({viewport:{width:1440,height:1100}});
const errors=[],external=[];
page.on('pageerror',e=>errors.push(e.message));
page.on('console',message=>{if(message.type()==='error')errors.push(message.text());});
await page.route('**/*',route=>{
  if(!['127.0.0.1','localhost','[::1]'].includes(new URL(route.request().url()).hostname)) {external.push(route.request().url());return route.abort();}
  return route.continue();
});
try {
  const traceID=randomBytes(16).toString('hex'),rootSpanID=randomBytes(8).toString('hex'),childSpanID=randomBytes(8).toString('hex');
  const linkedTraceID=randomBytes(16).toString('hex'),linkedSpanID=randomBytes(8).toString('hex');
  const started=BigInt(Date.now())*1_000_000n;
  const attr=(key,value)=>({key,value:typeof value==='number'?{intValue:String(value)}:{stringValue:value}});
  const otlp={resourceSpans:[{resource:{attributes:[attr('service.name','browser-fixture')]},scopeSpans:[{scope:{name:'repomesh.browser.fixture',version:'1'},spans:[
    {traceId:traceID,spanId:rootSpanID,name:'browser-trace-root',kind:'SPAN_KIND_INTERNAL',startTimeUnixNano:String(started),endTimeUnixNano:String(started+50_000_000n),attributes:[attr('repomesh.task_id','browser-task'),attr('repomesh.fixture','true')],status:{code:'STATUS_CODE_OK'}},
    {traceId:traceID,spanId:childSpanID,parentSpanId:rootSpanID,name:'browser-model-call',kind:'SPAN_KIND_CLIENT',startTimeUnixNano:String(started+5_000_000n),endTimeUnixNano:String(started+35_000_000n),attributes:[attr('gen_ai.span.kind','LLM'),attr('gen_ai.request.model','fixture-model'),attr('gen_ai.usage.input_tokens',0),attr('gen_ai.usage.output_tokens',7)],events:[{timeUnixNano:String(started+12_000_000n),name:'first-token',attributes:[attr('repomesh.event_kind','protocol-fixture')]}],links:[{traceId:linkedTraceID,spanId:linkedSpanID,attributes:[attr('repomesh.link_kind','fixture-parent')]}],status:{code:'STATUS_CODE_OK'}}
  ]}]}]};
  const traceResponse=await page.request.post(`${base}/v1/traces`,{data:otlp,headers:{'Content-Type':'application/json'}});
  assert.equal(traceResponse.status(),200,await traceResponse.text());
  await page.goto(base);await page.getByRole('heading',{name:'任务地图',exact:true}).waitFor();
  await page.getByRole('heading',{name:/从受测版本到复核|完整 Trace 调用链/}).waitFor();
  for(const name of ['数据概览','Trace 与事件','评测与验收','数据集','指标与计量','本地聚类与归因','Rubric 评分','样本与复核','设置']) {
    assert.ok(await page.getByRole('link',{name:new RegExp(name)}).count()>=1,`missing preserved page ${name}`);
  }
  const traceOption=page.locator('#task-map-subject option').filter({hasText:'browser-trace-root'}).first();
  await page.locator('#task-map-subject').selectOption(await traceOption.getAttribute('value'));
  await page.getByRole('heading',{name:'完整 Trace 调用链',exact:true}).waitFor();
  assert.equal(await page.locator('button[data-task-step]').count(),2);
  await page.getByRole('button',{name:/browser-model-call/}).click();
  await page.getByRole('button',{name:'查看完整 Span、属性、事件与 Links',exact:true}).click();
  await page.getByRole('dialog').getByText('2 个 Span。',{exact:false}).waitFor();
  await page.getByText('属性、事件与关联',{exact:true}).last().click();
  assert.match(await page.locator('dialog').innerText(),/first-token/);
  assert.match(await page.locator('dialog').innerText(),/fixture-parent/);
  await page.getByRole('button',{name:'关闭详情'}).click();
  await page.getByRole('button',{name:'性能泳道',exact:true}).click();
  await page.getByRole('heading',{name:'性能泳道',exact:true}).waitFor();
  await page.getByRole('button',{name:'证据链',exact:true}).click();
  await page.getByRole('heading',{name:'Trace 证据链',exact:true}).waitFor();
  const candidate=page.locator('#task-map-subject option').filter({hasText:'修正候选'}).first();
  if(await candidate.count()) await page.locator('#task-map-subject').selectOption(await candidate.getAttribute('value'));
  await page.getByRole('button',{name:/确定性业务验收/}).waitFor();
  await page.screenshot({path:path.join(output,'task-map-story.png'),fullPage:true});
  await page.getByRole('button',{name:/确定性业务验收/}).click();
  await page.getByRole('button',{name:'查看全部检查、请求与组合',exact:true}).click();
  await page.getByRole('dialog').getByRole('heading',{name:'逐项检查'}).waitFor();
  assert.equal(await page.locator('dialog tbody tr').count(),8);
  await page.getByRole('button',{name:'关闭详情'}).click();
  await page.getByRole('button',{name:'性能泳道',exact:true}).click();
  await page.getByRole('heading',{name:'观测到的阶段耗时'}).waitFor();
  assert.match(await page.locator('#content').innerText(),/原生 DSH 尚未接通/);
  await page.getByRole('button',{name:'证据链',exact:true}).click();
  await page.getByRole('heading',{name:'结论如何得到'}).waitFor();
  await page.screenshot({path:path.join(output,'task-map.png'),fullPage:true});
  await page.getByRole('link',{name:/数据概览/}).click();
  await page.getByRole('button',{name:'运行本地验收',exact:true}).waitFor();
  const before=await page.request.get(`${base}/api/catalog`).then(r=>r.json());
  const count=c=>c.archives.reduce((n,a)=>n+(a.trials||[]).length,0);
  await page.getByRole('button',{name:'运行本地验收',exact:true}).click();
  await page.getByRole('status').getByText('已完成 3 次验收，结果已保存在本机。',{exact:true}).waitFor();
  const after=await page.request.get(`${base}/api/catalog`).then(r=>r.json());
  assert.equal(count(after),count(before)+3);
  const active=after.archives.find(a=>a.writable);
  assert.deepEqual([...new Set(active.trials.map(t=>t.verdict))].sort(),['fail','pass','unknown']);
  await page.screenshot({path:path.join(output,'overview.png'),fullPage:true});
  await page.getByRole('link',{name:'✓ 评测与验收'}).click();
  await page.locator('tbody tr').filter({hasText:'修正候选'}).first().getByRole('button',{name:'查看',exact:true}).click();
  await page.getByRole('dialog').getByRole('heading',{name:'逐项检查'}).waitFor();
  assert.equal(await page.locator('dialog tbody tr').count(),8);
  await page.getByText(/^实际 HTTP 请求和响应/).click();
  assert.match(await page.locator('dialog').innerText(),/8000/);
  await page.screenshot({path:path.join(output,'trial-detail.png'),fullPage:true});
  await page.locator('dialog button[data-action="evidence"]').first().click();
  await page.getByRole('heading',{name:'原始证据',exact:true}).waitFor();
  assert.match(await page.locator('#detail-body').innerText(),/schema_version/);
  await page.getByRole('button',{name:'关闭详情'}).click();
  await page.getByRole('link',{name:'⌁ Trace 与事件'}).click();
  await page.getByRole('searchbox').fill(active.events[0].trace_id);
  assert.equal(await page.locator('#trace-list tbody tr').count(),1);
  await page.locator('#trace-list button').first().click();
  await page.getByRole('dialog').getByText('时点事实',{exact:true}).waitFor();
  await page.getByRole('button',{name:'关闭详情'}).click();
  await page.getByRole('link',{name:'▤ 数据集'}).click();
  const [download]=await Promise.all([page.waitForEvent('download'),page.getByRole('link',{name:'下载 CSV',exact:true}).first().click()]);
  await download.saveAs(path.join(output,'dataset.csv'));
  assert.match(await fs.readFile(path.join(output,'dataset.csv'),'utf8'),/repomesh-dataset\/1/);
  await page.goto(`${base}/settings`);
  await page.getByRole('heading',{name:'本地模式已就绪'}).waitFor();
  assert.equal(await page.locator('#model-form input[name=api_key]').inputValue(),'');
  assert.equal(await page.locator('#assistant-form input[name=api_key]').inputValue(),'');
  assert.equal(await page.locator('#model-form input[name=model]').inputValue(),'jev-1.13.0');
  await page.screenshot({path:path.join(output,'settings.png'),fullPage:true});
  await page.getByRole('link',{name:'◷ 指标与计量'}).click();
  await page.getByRole('heading',{name:'模型计量与覆盖'}).waitFor();
  await page.getByRole('link',{name:'↗ 本地聚类与归因'}).click();
  await page.getByRole('heading',{name:'本地聚类与归因',exact:true,level:2}).waitFor();
  assert.match(await page.locator('#content').innerText(),/云空间依赖/);
  await page.getByRole('link',{name:'◈ Rubric 评分'}).click();
  await page.getByRole('heading',{name:'Jev Rubric 与本地评估'}).waitFor();
  await page.getByRole('link',{name:'☷ 样本与复核'}).click();
  await page.getByRole('heading',{name:'样本与标注复核'}).waitFor();
  await page.setViewportSize({width:390,height:844});
  await page.goto(base);await page.getByRole('heading',{name:'任务地图',exact:true}).waitFor();
  await page.locator('#task-map-subject').waitFor();
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true);
  await page.screenshot({path:path.join(output,'mobile.png'),fullPage:true});
  assert.deepEqual(errors,[]);assert.deepEqual(external,[]);
  const summary={passed:true,task_map_default:true,preserved_detail_pages:9,full_trial_drilldown:true,full_trace_drilldown:true,trace_span_count:2,trace_attributes_events_links:true,performance_and_evidence_views:true,trials_before:count(before),trials_after:count(after),new_verdicts:['fail','pass','unknown'],browser_errors:errors,external_browser_requests:external};
  await fs.writeFile(path.join(output,'browser-summary.json'),JSON.stringify(summary,null,2)+'\n',{mode:0o600});
  console.log(JSON.stringify(summary));
} finally {await browser.close();}
