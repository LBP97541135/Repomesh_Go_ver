// Contract fixtures verify UI scope and retry behavior; no external provider is called.
// Start frontend Vite first, then set PLAYWRIGHT_MODULE if playwright is not locally installed.
import assert from 'node:assert/strict';
import { mkdir } from 'node:fs/promises';
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const browser = await chromium.launch({ headless: true });
const context = await browser.newContext({ viewport: { width: 1360, height: 1000 } });
const page = await context.newPage();
const errors = [];
page.on('pageerror', error => errors.push(error.message));
const seen = [];
const writes = [];
let actor = 'scope-user';
let failProjects = false;
let created = false;
let joined = [];
let attempts = 0;
let holdAlpha = false;
let alphaRequest;
let notifyAlpha;
const alphaPending = new Promise(resolve => { notifyAlpha = resolve; });
let betaPageRequest;
let notifyBetaPage;
const betaPagePending = new Promise(resolve => { notifyBetaPage = resolve; });
const candidate = (id, name) => ({ id, displayName: name, userParticipation: {status:'allowed'}, appCapability: {status:'allowed'} });
const repos = [candidate('repo-a','org/alpha'), candidate('repo-b','org/beta'), candidate('repo-c','org/gamma')];
await page.route('**/api/**', async route => {
  const request = route.request();
  const url = new URL(request.url());
  const path = url.pathname;
  if (!path.startsWith("/api/")) return route.continue();
  seen.push(`${request.method()} ${path}`);
  const json = body => route.fulfill({status:200, contentType:'application/json', body:JSON.stringify(body)});
  if (path === '/api/session') return json({user:{id:actor,displayName:'Scope tester',githubId:'101'},csrfToken:'fixture'});
  if (path === '/api/setup/status') return json({ready_for_project_creation:true});
  if (path === '/api/projects' && request.method()==='GET') {
    if (failProjects) return route.fulfill({status:503,contentType:'application/json',body:'{"code":"UNAVAILABLE"}'});
    return json(url.searchParams.has('cursor') ? {items:[{id:'project-b',name:'Beta'},...(created ? [{id:'project-c',name:'New Project'}] : [])],nextCursor:null} : {items:[{id:'project-a',name:'Alpha'}],nextCursor:'projects-page-2'});
  }
  if (path === '/api/projects' && request.method()==='POST') {
    const body=request.postDataJSON(); writes.push({path,body,key:request.headers()['idempotency-key']});
    assert.deepEqual(body.repositoryIds,[]);
    created=true;
    return json({projectId:'project-c',projectRevision:'c1'});
  }
  if (path === '/api/repositories') return json(url.searchParams.has('cursor') ? {items:[repos[2]],nextCursor:null} : {items:repos.slice(0,2),nextCursor:'repos-page-2'});
  if (path.endsWith('/repositories')) {
    const project=path.split('/')[3];
    return json({items:project==='project-c' ? repos.filter(r=>joined.includes(r.id)) : project==='project-b' ? [repos[1]] : [repos[0]],nextCursor:null,projectRevision:joined.length?'c2':'c1',restrictedRepositoryCount:0});
  }
  if (path === '/api/projects/project-c' && request.method()==='PATCH') {
    const body=request.postDataJSON();writes.push({path,body,key:request.headers()['idempotency-key']});
    assert.equal(body.expectedProjectRevision,'c1'); joined=body.repositoryIdsToAdd;
    return json({projectId:'project-c',projectRevision:'c2'});
  }
  if (path.endsWith('/issue-creation-options')) {
    const r= url.searchParams.has('cursor') ? repos[2] : repos[0];
    return json({projectId:'project-c',creationContextRevision:'context-c2',canSubmit:true,blockingReasons:[],repositories:[{repositoryId:r.id,displayName:r.displayName,selectable:true,reasons:[]}],nextCursor:url.searchParams.has('cursor')?null:'options-page-2'});
  }
  if (path.endsWith('/issue-creations')) {
    writes.push({path,body:request.postDataJSON(),key:request.headers()['idempotency-key']});
    attempts++;
    if(attempts===1) return route.abort('failed');
    return json({issue:{id:'issue-c'},projectId:'project-c'});
  }
  if (path === '/api/issues/issue-c') return json({id:'issue-c',projectId:'project-c',title:'Precise scope',description:'Precise scope',repositoryIds:['repo-c'],createdAt:'2026-09-19T00:00:00Z',source:{kind:'issue_page',conversationId:'conversation-c'}});
  if (path === '/api/projects/project-a/issues' && holdAlpha) { alphaRequest=route; notifyAlpha(); return; }
  if (path === '/api/projects/project-b/issues') {
    if (url.searchParams.has('cursor')) { betaPageRequest=route; notifyBetaPage(); return; }
    return json({items:[],nextCursor:url.searchParams.get('state')==='closed'?null:'beta-page-2',openCount:0,closedCount:0});
  }
  if (path.endsWith('/issues')) return json({items:[],nextCursor:null,openCount:0,closedCount:0});
  if (path.includes('review-requests') && !path.includes('stream')) return json([]);
  return route.fulfill({status:404,contentType:'application/json',body:'{"detail":"fixture endpoint unavailable"}'});
});
try {
  await page.goto(`${process.env.SCOPE_UI_URL || 'http://127.0.0.1:5297'}/?source=live#/issues/new`);
  await page.getByRole('heading',{name:'项目管理',exact:true}).waitFor();
  await page.getByText('Beta',{exact:true}).waitFor(); // Page two was read.
  assert.equal(seen.some(s=>s.endsWith('/issues')),false,'must not fetch a guessed project');
  await page.getByRole('button',{name:'新建项目',exact:true}).click();
  await page.getByLabel('项目名称',{exact:true}).fill('New Project');
  await page.getByLabel('项目用途',{exact:true}).fill('Scope regression');
  assert.equal(await page.getByRole('checkbox').count(),0,'repository selection comes after saving project');
  await page.getByRole('button',{name:'保存项目，继续选择仓库'}).click();
  await page.getByRole('heading',{name:'New Project · 项目仓库'}).waitFor();
  assert.equal(await page.getByRole('button',{name:'创建 Issue',exact:true}).isDisabled(),true);
  await page.getByRole('button',{name:'接入仓库',exact:true}).click();
  await page.getByLabel('org/alpha',{exact:false}).check();
  await page.getByLabel('org/gamma',{exact:false}).check();
  await page.getByRole('button',{name:'接入所选 2 个仓库'}).click();
  await page.getByRole('button',{name:'创建 Issue',exact:true}).click();
  await page.getByLabel('org/gamma',{exact:false}).waitFor();
  assert.equal(await page.getByRole('checkbox',{checked:true}).count(),0,'no implicit repository selection');
  await page.getByLabel('org/gamma',{exact:false}).check();
  await page.getByPlaceholder('输入需求',{exact:false}).fill('Precise scope');
  await page.getByRole('button',{name:'发送（Ctrl+Enter）',exact:true}).click();
  await page.getByText('提交内容已固定',{exact:false}).waitFor();
  assert.equal(await page.getByLabel('org/alpha',{exact:false}).isDisabled(),true);
  await page.getByRole('button',{name:'发送（Ctrl+Enter）',exact:true}).click();
  await page.waitForURL(url => url.hash === '#/issues/issue-c');
  const issueWrites=writes.filter(w=>w.path.endsWith('/issue-creations'));
  assert.equal(issueWrites.length,2);
  assert.deepEqual(issueWrites[0],issueWrites[1],'retry must preserve key and exact input');
  assert.deepEqual(issueWrites[0].body.repositoryIds,['repo-c']);
  assert.equal(issueWrites[0].body.expectedCreationContextRevision,'context-c2');
  assert.equal(writes.filter(w=>w.path==='/api/projects').length,1,'Issue submission must never create another project');
  await page.locator('aside').getByRole('button',{name:'N New Project',exact:true}).click();
  holdAlpha=true;
  await page.locator('aside').getByRole('button',{name:'Alpha',exact:true}).click();
  await alphaPending;
  await page.locator('aside').getByRole('button',{name:'A Alpha',exact:true}).click();
  await page.locator('aside').getByRole('button',{name:'Beta',exact:true}).click();
  await alphaRequest.fulfill({status:200,contentType:'application/json',body:JSON.stringify({items:[{id:'old-alpha',title:'Late Alpha Issue',repositoryIds:['repo-a'],state:'open'}],nextCursor:null,openCount:1,closedCount:0})});
  await page.waitForTimeout(100);
  assert.equal(await page.getByText('Late Alpha Issue',{exact:true}).count(),0,'late Issue response overwrote new project');
  await page.getByRole('button',{name:'加载更多',exact:true}).click();
  await betaPagePending;
  await page.getByRole('button',{name:/^Closed/}).click();
  await page.getByText('没有已完结的 issue',{exact:true}).waitFor();
  await betaPageRequest.fulfill({status:200,contentType:'application/json',body:JSON.stringify({items:[{id:'old-beta-page',title:'Late Open Page',repositoryIds:['repo-b'],state:'open'}],nextCursor:null,openCount:1,closedCount:0})});
  await page.waitForTimeout(100);
  assert.equal(await page.getByText('Late Open Page',{exact:true}).count(),0,'late pagination response overwrote new filter');
  await page.locator('aside').getByRole('button',{name:'仓库',exact:true}).click();
  await page.getByRole('heading',{name:'Beta · 项目仓库'}).waitFor();
  await page.getByText('org/beta',{exact:true}).waitFor();
  assert.equal(await page.getByText('org/gamma',{exact:true}).count(),0,'previous project repositories leaked');
  assert.equal(seen.some(s=>s.includes('/scan/repositories')),false,'global scan catalog must not enter project workbench');
  const output=process.env.SCOPE_SCREENSHOT_DIR;
  if(output){await mkdir(output,{recursive:true});await page.screenshot({path:`${output}/project-repositories.png`,fullPage:true});}
  actor='another-user';
  await page.reload();
  await page.getByRole('heading',{name:'项目管理',exact:true}).waitFor();
  assert.equal(await page.getByRole('heading',{name:'Beta · 项目仓库'}).count(),0,'account inherited previous active project');
  failProjects=true;
  await page.reload();
  await page.getByRole('alert').filter({hasText:'项目加载失败'}).waitFor();
  assert.equal(await page.getByText('尚无项目，请先创建项目。',{exact:true}).count(),0,'network error disguised as empty project list');
  assert.deepEqual(errors,[]);
  console.log(JSON.stringify({result:'PASS',checks:['explicit project','project pagination','save before repository selection','candidate pagination','explicit joining','Issue option pagination','explicit Issue subset','identical retry','project switching','late response isolation','late pagination isolation','account isolation','network error state'],writes:writes.length}));
} catch (error) { console.error(JSON.stringify({url:page.url(),body:await page.locator("body").innerText(),seen,errors})); throw error; } finally { await browser.close(); }
