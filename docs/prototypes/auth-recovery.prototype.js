// F01 login recovery proposal: same adopted entry layout, memory-only scenarios.
// No OAuth request, credential storage, session verification or business replay.
let authScreen = null;
let authRemember = 'expired';
let authNote = '';
let authPurpose = 'login';
const authCases = {
  expired: ['登录已过期', '请重新登录后继续。', '重新登录 GitHub', 'start', '已提交的项目与 Issue 不会因此取消。'],
  cancelled: ['这次登录未完成', '你已取消 GitHub 授权，可以稍后再试。', '重新登录 GitHub', 'start', '尚未取得登录资格，不显示原页面的项目内容。'],
  waiting: ['等待 GitHub 授权', '完成授权后，将回到 RepoMesh 确认结果。', '', '', '此处为等待状态样例，没有打开真实 GitHub 授权。'],
  loginUnknown: ['登录结果待确认', '暂时无法确认当前登录状态。', '检查当前登录状态', 'queryLogin', '确认当前账号后再继续；不会自动提交之前的操作。'],
  reconnect: ['重新连接 GitHub', '当前登录仍有效，GitHub 连接需要重新授权。', '重新连接同一账号', 'startReconnect', '请继续使用当前绑定的 GitHub 账号，项目归属保持不变。'],
  reconnectUnknown: ['连接结果待确认', '你仍处于登录状态，但这次重新连接的结果尚未确认。', '查询本次连接结果', 'queryReconnect', '已有连接显示正常，也不能证明这次重新连接成功。'],
  wrongAccount: ['请选择原来的 GitHub 账号', '这次选择的账号与当前绑定账号不同，未更换绑定。', '重新选择账号', 'startReconnect', '原账号与项目归属保持不变。'],
  success: ['当前登录已确认', '继续后会重新读取目标页面的访问资格。', '继续', 'continue', '不会自动重新提交项目、Issue 或其他未确认操作。'],
  reconnectSuccess: ['本次连接已确认', '已取得这次重新连接对应的结果。', '返回工作区', 'return', '登录仍有效不等于仓库权限已核实，返回后仍需重新读取。']
};
const authButton = (label, action, primary=false) => `<button type="button" data-auth-action="${action}" class="${primary?'rm-login-button':''}">${label}</button>`;
const authOldRender = render;
render = function(){
  if(!authScreen){authOldRender();return;}
  authRemember=authScreen;
  const c=authCases[authScreen];
  const isReconnect=['reconnect','reconnectUnknown','wrongAccount','reconnectSuccess'].includes(authScreen)||(authScreen==='waiting'&&authPurpose==='reconnect');
  root.innerHTML=`<main class="rm-login rm-auth-stage"><div class="rm-login-box rm-auth-box"><div class="rm-login-mark">▧</div><div class="rm-auth-kicker">RepoMesh · ${isReconnect?'账号连接':'登录'}</div><h1>${c[0]}</h1><p>${c[1]}</p>${isReconnect?'<div class="rm-auth-account">当前绑定账号 <strong>林悦 · GitHub 示例账号</strong></div>':''}<div class="rm-auth-context"><span>继续前往</span><strong>${isReconnect?'工作区':'原页面'}</strong><small>${isReconnect?'返回后重新核对项目与仓库权限。':'登录后继续访问之前的页面。'}</small></div>${c[2]?authButton(c[2],c[3],true):'<div class="rm-auth-wait" role="status">等待授权结果…</div>'}${authScreen==='waiting'?authButton('返回', 'back'):authScreen==='success'||authScreen==='reconnectSuccess'?'':isReconnect?authButton('稍后处理，返回工作区','return'):authButton('返回登录入口','loginHome')}<p class="rm-auth-foot">${c[4]}</p><div role="status" aria-live="polite" class="rm-auth-note">${esc(authNote)}</div></div></main><footer class="rm-auth-tools" aria-label="登录恢复原型演示"><strong>原型场景 · 三项UI已采用</strong><select aria-label="登录恢复场景">${Object.entries(authCases).map(([key,x])=>`<option value="${key}" ${authScreen===key?'selected':''}>${x[0]}</option>`).join('')}</select>${authScreen==='waiting'?authButton('模拟收到待核结果','demoUnknown'):''}${['loginUnknown','reconnectUnknown'].includes(authScreen)?authButton('模拟取得确认依据','demoConfirmed'):''}<span>仅内存样例 · 刷新重置</span></footer>`;
  joined.report('entry','auth-recovery','F01 · 三项恢复UI已采用 · 无真实认证');
};
root.addEventListener('click', e=>{
  const action=e.target.closest('[data-auth-action]')?.dataset.authAction;
  if(!action)return;
  e.preventDefault();e.stopImmediatePropagation();authNote='';
  if(action==='start'||action==='startReconnect'){authPurpose=action==='start'?'login':'reconnect';authScreen='waiting';}
  if(action==='queryLogin')authNote='样例查询仍未取得当前登录依据，请稍后再次检查。';
  if(action==='queryReconnect')authNote='样例查询仍未确认这次连接，保留待核状态。';
  if(action==='demoUnknown')authScreen=authPurpose==='reconnect'?'reconnectUnknown':'loginUnknown';
  if(action==='demoConfirmed')authScreen=authScreen==='reconnectUnknown'?'reconnectSuccess':'success';
  if(action==='back')authScreen=authPurpose==='reconnect'?'reconnect':'expired';
  if(action==='loginHome'){authScreen=null;state.page='login';state.waiting=false;joined.go('login');return;}
  if(action==='continue'||action==='return'){authScreen=null;joined.go('resume');return;}
  render();
},true);
root.addEventListener('change',e=>{
  if(!e.target.matches('[aria-label="登录恢复场景"]'))return;
  authScreen=e.target.value;authNote='';authPurpose=authScreen.startsWith('reconnect')||authScreen==='wrongAccount'?'reconnect':'login';render();
});
const authOldNavigate=window.joinedAPI.navigate;
window.joinedAPI.navigate=function(route){if(route==='auth-recovery'){authScreen=authRemember;authNote='';render();return;}authScreen=null;authOldNavigate(route);};
const authCss=document.createElement('style');
authCss.textContent=`#repomesh-rooms-prototype .rm-auth-stage{min-height:calc(100dvh - 66px);padding:25px 24px 10px}#repomesh-rooms-prototype .rm-auth-box{width:min(100%,400px)}#repomesh-rooms-prototype .rm-auth-kicker{font-size:12px;color:#aaa;margin-top:16px}#repomesh-rooms-prototype .rm-auth-box h1{font-size:25px;margin:12px 0 10px}#repomesh-rooms-prototype .rm-auth-box>p{margin-bottom:22px}#repomesh-rooms-prototype .rm-auth-context,#repomesh-rooms-prototype .rm-auth-account{border:1px solid #333;border-radius:9px;text-align:left;padding:12px 15px;margin-bottom:20px;font-size:12px;color:#aaa}#repomesh-rooms-prototype .rm-auth-context strong{float:right;color:#ddd;font-weight:400}#repomesh-rooms-prototype .rm-auth-context small{display:block;margin-top:6px;color:#888}#repomesh-rooms-prototype .rm-auth-account strong{display:block;color:#ddd;font-weight:400;margin-top:4px}#repomesh-rooms-prototype .rm-auth-foot{font-size:12px;margin:18px 0 4px;line-height:1.7}#repomesh-rooms-prototype .rm-auth-note{font-size:12px;color:#d2bd93;min-height:20px}#repomesh-rooms-prototype .rm-auth-wait{padding:13px;border:1px solid #474134;border-radius:8px;color:#cbb993}#repomesh-rooms-prototype .rm-auth-tools{display:flex;gap:10px;align-items:center;flex-wrap:wrap;padding:12px 16px;border-top:1px solid #35332d;background:#1d1c19;color:#b8ae96;font-size:11px}#repomesh-rooms-prototype .rm-auth-tools strong{font-weight:400}#repomesh-rooms-prototype .rm-auth-tools select{font-size:12px;max-width:225px;border:1px solid #464238;padding:5px;background:#24221e}#repomesh-rooms-prototype .rm-auth-tools>span{margin-left:auto}#repomesh-rooms-prototype .rm-auth-tools button{font-size:11px}@media(max-width:560px){#repomesh-rooms-prototype .rm-auth-stage{padding:25px 20px;min-height:calc(100dvh - 120px)}}`;
document.head.append(authCss);
