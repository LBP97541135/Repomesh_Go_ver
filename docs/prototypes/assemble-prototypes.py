"""Build the review-only joined prototype. Existing HTML sources stay unchanged.
Run: python docs/prototypes/assemble-prototypes.py
"""
from pathlib import Path
import base64
import hashlib
import json

ROOT = Path(__file__).resolve().parent
SOURCES = {
    'entry': 'repomesh-project-entry-prototype.html',
    'workspace': 'repomesh-message-target-prototype.html',
    'settings': 'repomesh-project-settings-prototype.html',
    'models': 'repomesh-model-provider-prototype.html',
    'review': 'first-batch-review.html.template',
}

COMMON = r'''
const joined=parent.rmPreview;
document.addEventListener('click',e=>{
 const route=e.target.closest('[data-joined-route]')?.dataset.joinedRoute;
 if(route){e.preventDefault();e.stopImmediatePropagation();joined.go(route)}
},true);
'''

WORKSPACE = r'''
const oldSide=side;
side=function(){return oldSide().replace('<div class="rm-sidebar-bottom">',
 '<div class="rm-sidebar-bottom"><div class="rm-nav"><button data-joined-route="settings">⚙ 项目设置</button><button data-joined-route="models">◈ 模型设置</button></div>')};
const oldProjectPanel=projectPanel;
projectPanel=function(){return oldProjectPanel()+'<div class="rm-details"><button data-joined-route="settings">编辑项目资料与配置 →</button><button data-joined-route="models">管理模型 →</button></div>'};
const exportProject=()=>({id:project().id,name:project().name,purpose:project().description,repos:[...project().repos]});
const oldRender=render;
render=function(){oldRender();const c=current();
 const top=root.querySelector('.rm-top-right');if(top){top.style.display='flex';const settings=document.createElement('button');settings.dataset.joinedRoute='settings';settings.textContent='⚙ 项目设置';settings.style.whiteSpace='nowrap';top.prepend(settings)}
 if(project().id)joined.setProject(exportProject());
 const r=state.panel==='issues'?'issues':state.panel==='create-issue'?'create-issue':state.panel==='issue'?'issue/'+state.issueKey+'/'+state.view:state.panel==='project'?'repositories':state.key?'conversation/'+state.key:'workspace';
 joined.report('workspace',r,c?.clarification&&!state.panel?'消息目标控件 · 待评审':null);
};
mount.addEventListener('click',e=>{if(e.target.closest('[data-action="create-project"]')){e.preventDefault();e.stopImmediatePropagation();joined.go('new-project')}},true);
// Prevent a resolved submission dialog from masking the next new form.
const clearFormOnFresh=e=>{const a=e.target.closest('[data-action]')?.dataset.action;if(a==='new-page-issue'&&!state.submission){state.issueTitle='';state.issueGoal='';state.issueRepos=new Set()}};
mount.addEventListener('click',clearFormOnFresh,true);
window.joinedAPI={
 navigate(route){
  if(route==='resume'){render();return}
  if(route==='sample'){const c=items.find(c=>c.demo);c.messages=c.messages.filter(m=>!m.clarificationPrompt&&!m.clarificationReply&&!m.targetIssueKey);delete c.clarification;c.managerReplied=true;state.replyToQuestion=null;openConversation(c.key);return}
  if(route==='clarification'){resetClarification();return}
  if(route==='issues'){state.panel='issues';state.wizard=false;render();return}
  if(route==='create-issue'){state.panel='create-issue';state.wizard=false;render();return}
  if(route==='repositories'){state.panel='project';render();return}
  if(route.startsWith('conversation/')){openConversation(route.split('/')[1]);return}
  if(route.startsWith('issue/')){const [,key,view]=route.split('/');openIssue(key);state.view=view||'overview';render();return}
  Object.assign(state,{key:null,panel:null,wizard:false,input:'',replyToQuestion:null});render();
 },
 addProject(p){
  const id='project-'+(projects.length+1);projects.push({id,name:p.name,description:p.purpose,repos:p.repos,ready:false});
  p.repos.forEach(name=>{if(!repositories.some(r=>r.name===name))repositories.push({name,app:name!=='partner/billing-api'})});
  state.project=id;state.open.add(id);Object.assign(state,{key:null,panel:null,wizard:false,input:'',replyToQuestion:null});render();
 },
 updateProject(p){const target=projects.find(x=>x.id===p.id);if(!target)return;target.name=p.name;target.description=p.purpose;target.repos=[...p.repos];p.repos.forEach(name=>{if(!repositories.some(r=>r.name===name))repositories.push({name,app:name!=='partner/billing-api'})});render()}
};
// Normal conversation is the starting point; the unadopted clarification is opt-in.
const demo=items.find(c=>c.demo);demo.messages=demo.messages.filter(m=>!m.clarificationPrompt);delete demo.clarification;demo.managerReplied=true;state.replyToQuestion=null;render();
'''

ENTRY = r'''
let joinedSaved=null;
const oldOverview=overview;
overview=function(){return oldOverview()+'<div class="rm-summary"><button data-joined-route="resume" class="rm-primary">进入项目工作区 →</button><button data-joined-route="settings">项目设置</button><button data-joined-route="models">模型设置</button></div>'};
const oldSave=save;
save=function(){oldSave();if(state.saved&&state.saved!==joinedSaved){joinedSaved=state.saved;joined.createProject({name:state.saved.name,purpose:state.saved.purpose,repos:repos.filter(r=>state.saved.ids.includes(r.id)).map(r=>r.name)})}};
root.addEventListener('click',e=>{if(e.target.closest('[data-action="settings"]')){e.preventDefault();e.stopImmediatePropagation();joined.go('models')}},true);
window.joinedAPI={navigate(route){if(route==='new-project'){state.page='create';state.step=1;state.selected=new Set();state.name='';state.purpose='';state.query='';state.unknown=false;state.error='';state.saved=null;joinedSaved=null}else if(route==='login'){state.page='login';state.waiting=false}render()}};
'''

SETTINGS = r'''
let contextId=null;
const contexts=new Map();
const stash=()=>({saved,draft,stage,scenario,readState,oldRestricted,addOpen,addUnknown,selected,changeConfig,op,receipt,error,sidebar,query,conflictLoaded});
const oldCurrentConfig=currentConfig;
currentConfig=function(){const m=joined.modelFor(contextId);return (m?'<div class="notice">模型应用演示：'+esc(m.provider)+' / '+esc(m.model)+'<small>固定快照的实际应用协议待设计；此处仅串联展示。</small></div>':'')+oldCurrentConfig()+'<p><button data-joined-route="models">前往供应商与模型设置 →</button></p>'};
const oldCommit=commit;
commit=function(){oldCommit();joined.updateProject({id:contextId,name:saved.name,purpose:saved.purpose,repos:saved.repoIds.map(id=>allRepos.find(r=>r.id===id)?.name).filter(Boolean)})};
const oldRender=render;
render=function(){oldRender();const side=root.querySelector('.side');if(side){const nav=document.createElement('div');nav.innerHTML='<button data-joined-route="resume">← 返回工作区</button><button data-joined-route="models">◈ 模型设置</button>';side.insertBefore(nav,side.querySelector('.profile'))}root.querySelector('.tools')?.removeAttribute('open')};
document.addEventListener('click',e=>{if(e.target.closest('a[href*="project-entry"]')){e.preventDefault();joined.go('new-project')}},true);
window.joinedAPI={navigate(){const p=joined.currentProject();if(p&&contextId!==p.id){if(contextId)contexts.set(contextId,stash());contextId=p.id;if(contexts.has(contextId)){({saved,draft,stage,scenario,readState,oldRestricted,addOpen,addUnknown,selected,changeConfig,op,receipt,error,sidebar,query,conflictLoaded}=contexts.get(contextId));render();return;}p.repos.forEach(name=>{if(!allRepos.some(r=>r.name===name))allRepos.push({id:'repo-'+allRepos.length,name,appMissing:name==='partner/billing-api'})});saved={...structuredClone(initial),name:p.name,purpose:p.purpose,repoIds:p.repos.map(name=>allRepos.find(r=>r.name===name).id)};draft=structuredClone(saved);stage='edit';selected.clear();changeConfig=false;op=null;receipt=null;error='';readState='ok';oldRestricted=false;addUnknown=false}render()}};
'''

MODELS = r'''
const oldParts=renderParts;
renderParts=function(){oldParts();const p=joined.currentProject();const m=joined.modelFor(p?.id);$('#project').textContent=m?`${p.name} · 应用演示：${m.provider} / ${m.model}。专用应用协议尚未完成，运行状态未验证。`:`${p?.name||'当前项目'}：尚未演示选择模型。`;

};
window.joinedAPI={navigate(){renderParts()}};
const crumb=document.querySelector('.crumb');const back=document.createElement('button');back.textContent='← 返回原页面';back.dataset.joinedRoute='return';crumb.prepend(back);
'''

STYLE = '''<style>html,body{margin:0;background:#131313;color-scheme:dark}#repomesh-rooms-prototype .rm-shell{border-radius:0;border:0;min-height:0;height:calc(100dvh - 54px)}#repomesh-rooms-prototype .rm-demo-load,#repomesh-rooms-prototype .rm-design-notes{display:none}#repomesh-rooms-prototype .rm-main{min-height:0}#repomesh-rooms-prototype .rm-sidebar-bottom .rm-nav{border-top:1px solid #303030;padding-top:10px}.joined-note{font:12px/1.6 'Segoe UI','Microsoft YaHei',sans-serif;color:#bbaa8a;background:#24221d;padding:8px 16px}#repomesh-rooms-prototype .rm-script-control{min-height:38px;box-sizing:border-box}@media(max-width:600px){#repomesh-rooms-prototype .rm-shell{height:calc(100dvh - 86px)}} </style>'''


def build_source(key, path):
    source = path.read_text(encoding='utf-8-sig')
    adapter = {'workspace': WORKSPACE, 'entry': ENTRY, 'settings': SETTINGS, 'models': MODELS, 'review': ''}[key]
    if key == 'workspace':
        adapter += '\n' + (ROOT / 'issue-overview.prototype.js').read_text(encoding='utf-8')
    if key == 'entry':
        adapter += '\n' + (ROOT / 'auth-recovery.prototype.js').read_text(encoding='utf-8')
    if key == 'settings':
        adapter += '\n' + (ROOT / 'repository-picker.prototype.js').read_text(encoding='utf-8')
    if key == 'models':
        adapter += '\n' + (ROOT / 'model-project-apply.prototype.js').read_text(encoding='utf-8')
        adapter += '\n' + (ROOT / 'model-key-save.prototype.js').read_text(encoding='utf-8')
        adapter += '\n' + (ROOT / 'model-test.prototype.js').read_text(encoding='utf-8')
    # All four sources use a single closing IIFE. Inject within its lexical scope.
    pos = source.rfind('})();')
    if pos < 0:
        raise ValueError(f'Missing closing IIFE in {path.name}')
    source = source[:pos] + COMMON + adapter + '\n' + source[pos:]
    source = source.replace('</style>', '</style>' + STYLE, 1)
    return base64.b64encode(source.encode()).decode()


def main():
    encoded = {key: build_source(key, ROOT / name) for key, name in SOURCES.items()}
    manifest = {p.name: hashlib.sha256(p.read_bytes()).hexdigest() for p in ROOT.glob('*.html') if p.name != 'index.html'}
    html = (ROOT / 'joined-preview-shell.html.template').read_text(encoding='utf-8')
    html = html.replace('__EMBEDDED_SOURCES__', json.dumps(encoded)).replace('__SOURCE_HASHES__', json.dumps(manifest))
    (ROOT / 'index.html').write_text(html, encoding='utf-8')
    print(f'Built index.html from {len(encoded)} current source families; indexed {len(manifest)} original HTML files.')


if __name__ == '__main__':
    main()
