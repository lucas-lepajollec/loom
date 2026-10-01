// Barre latérale : Nouveau chat, recherche, projets (dépliables, chats
// imbriqués, glisser-déposer), liste des chats, Réglages en bas.

let NAV = {projects:[], conversations:[], active:'', project_id:'', q:''};
const NAV_OPEN_KEY = 'loom-proj-open';

function navOpenSet(){
  try{ return new Set(JSON.parse(localStorage.getItem(NAV_OPEN_KEY)||'[]')); }
  catch(_){ return new Set(); }
}
function navSaveOpen(set){
  try{ localStorage.setItem(NAV_OPEN_KEY, JSON.stringify([...set])); }catch(_){}
}
function navIsOpen(id){
  const s = navOpenSet();
  if(s.has(id)) return true;
  return NAV.conversations.some(c=>c.project_id===id && c.id===NAV.active);
}
function navToggleOpen(id){
  const s = navOpenSet();
  if(s.has(id)) s.delete(id); else s.add(id);
  navSaveOpen(s);
  renderNav();
}

function navIcon(d){
  return '<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">'+d+'</svg>';
}

function navChatBtn(c){
  const b = document.createElement('button');
  b.type = 'button';
  b.className = 'nav-item nav-chat' + (c.id===NAV.active ? ' active' : '');
  b.title = c.title || 'Nouveau chat';
  b.draggable = true;
  b.dataset.chatId = c.id;
  const name = document.createElement('span');
  name.className = 'nav-label';
  name.textContent = c.title || 'Nouveau chat';
  b.appendChild(name);
  if(c.workspace){b.draggable=false;b.onclick=()=>openWorkspace('discussions',c.id);return b;}
  b.onclick = ()=>{ closeSettings(); restoreHistory(c.id); };
  b.oncontextmenu = (e)=>{ e.preventDefault(); navChatMenu(e, c); };
  b.addEventListener('dragstart', e=>{
    e.dataTransfer.setData('text/plain', c.id);
    e.dataTransfer.setData('text/loom-chat', c.id);
    e.dataTransfer.effectAllowed = 'move';
    b.classList.add('dragging');
  });
  b.addEventListener('dragend', ()=> b.classList.remove('dragging'));
  const more = document.createElement('button');
  more.type = 'button'; more.className = 'nav-more'; more.title = 'Plus';
  more.innerHTML = '···';
  more.onclick = (e)=>{ e.stopPropagation(); navChatMenu(e, c); };
  more.addEventListener('mousedown', e=> e.stopPropagation());
  b.appendChild(more);
  return b;
}

function navDropZone(t){
  if(!t || !t.closest) return null;
  const proj = t.closest('.nav-proj');
  if(proj) return {el:proj, projectId:proj.dataset.projectId||''};
  const chats = t.closest('#nav-chats');
  if(chats) return {el:chats, projectId:''};
  return null;
}
function navDndBound(){
  const root = document.getElementById('sidenav');
  if(!root || root.dataset.dnd==='1') return;
  root.dataset.dnd = '1';
  const clear = ()=> document.querySelectorAll('.drop-on').forEach(x=>x.classList.remove('drop-on'));
  root.addEventListener('dragover', e=>{
    const zone = navDropZone(e.target);
    if(!zone) return;
    e.preventDefault();
    e.dataTransfer.dropEffect = 'move';
    if(!zone.el.classList.contains('drop-on')){
      clear();
      zone.el.classList.add('drop-on');
    }
  });
  root.addEventListener('dragleave', e=>{
    if(root.contains(e.relatedTarget)) return;
    clear();
  });
  root.addEventListener('drop', e=>{
    const zone = navDropZone(e.target);
    clear();
    if(!zone) return;
    e.preventDefault();
    const id = e.dataTransfer.getData('text/loom-chat') || e.dataTransfer.getData('text/plain');
    if(id) assignChatProject(id, zone.projectId);
  });
}

function hideNavMenu(){
  const m = document.getElementById('nav-menu');
  if(m) m.style.display = 'none';
}
function showNavMenu(e, items){
  const m = document.getElementById('nav-menu'); if(!m) return;
  m.innerHTML = '';
  items.forEach(it=>{
    if(it===null){ const hr=document.createElement('hr'); m.appendChild(hr); return; }
    const b=document.createElement('button'); b.type='button';
    b.textContent = it.label;
    if(it.danger) b.className = 'danger';
    b.onclick = ()=>{ hideNavMenu(); it.fn(); };
    m.appendChild(b);
  });
  m.style.display = 'block';
  const x = Math.min(e.clientX, window.innerWidth - 220);
  const y = Math.min(e.clientY, window.innerHeight - 8 - m.offsetHeight);
  m.style.left = x+'px'; m.style.top = y+'px';
}
document.addEventListener('click', hideNavMenu);

function navChatMenu(e, c){
  const items = [
    {label:'Renommer', fn:()=>renameHistory(c.id, c.title).then(()=>loadNav())},
    {label:'Déplacer vers un projet', fn:()=>moveChatToProject(c)},
  ];
  if(c.project_id){
    items.push({label:'Retirer du projet', fn:()=>assignChatProject(c.id, '')});
  }
  items.push(
    {label: c.fav ? 'Retirer des favoris' : 'Mettre en favori', fn:()=>favHistory(c.id, !c.fav).then(()=>loadNav())},
    null,
    {label:'Supprimer', danger:true, fn:()=>deleteHistory(c.id, c.title).then(()=>loadNav())}
  );
  showNavMenu(e, items);
}
function navProjectMenu(e, p){
  showNavMenu(e, [
    {label:'Nouveau chat', fn:()=>newChat(p.id)},
    {label:'Renommer', fn:()=>renameProjectUI(p)},
    null,
    {label:'Supprimer le projet', danger:true, fn:()=>deleteProjectUI(p)}
  ]);
}

async function moveChatToProject(c){
  const names = ['(aucun projet)'].concat(NAV.projects.map(p=>p.name));
  const ids = [''].concat(NAV.projects.map(p=>p.id));
  const cur = names[Math.max(0, ids.indexOf(c.project_id||''))] || names[0];
  const name = await askPrompt('Projet (nom exact) :\n'+NAV.projects.map(p=>'• '+p.name).join('\n'), {
    title:'Déplacer', okText:'Déplacer', default: cur==='(aucun projet)'?'':cur, placeholder:'nom du projet'
  });
  if(name===null) return;
  const t = name.trim();
  let pid = '';
  if(t){
    const hit = NAV.projects.find(p=>p.name.toLowerCase()===t.toLowerCase());
    if(!hit){ toast('projet introuvable'); return; }
    pid = hit.id;
  }
  assignChatProject(c.id, pid);
}

async function assignChatProject(chatId, projectId){
  const c = NAV.conversations.find(x=>x.id===chatId);
  const next = projectId || '';
  if(c && (c.project_id||'') === next) return;
  let r; try{ r = await jpost('/api/chat/history/move', {id:chatId, project_id:next}); }catch(_){ toast('erreur réseau'); return; }
  if(!r.ok){ toast(r.error||'impossible'); return; }
  if(next){
    const s = navOpenSet(); s.add(next); navSaveOpen(s);
  }
  loadNav();
}

function navMatch(c, q){
  return !q || (c.title||'').toLowerCase().includes(q) || (c.id===NAV.active && !c.title);
}
function renderNavSearch(){
  const box = document.getElementById('nav-search-list');
  const title = document.getElementById('nav-search-title');
  if(!box) return;
  const q = (NAV.q||'').trim().toLowerCase();
  if(title) title.textContent = q ? 'Résultats' : 'Chats récents';
  box.innerHTML = '';
  const chats = NAV.conversations.filter(c=>navMatch(c, q)).slice(0, 40);
  if(!chats.length){
    const empty = document.createElement('div');
    empty.className = 'nsm-empty';
    empty.textContent = q ? 'Aucun chat' : 'Aucun chat récent';
    box.appendChild(empty);
    return;
  }
  chats.forEach(c=>{
    const b = document.createElement('button');
    b.type = 'button';
    b.className = 'nsm-item' + (c.id===NAV.active ? ' active' : '');
    b.innerHTML = '<span class="nsm-dot"></span><span class="nsm-label"></span>';
    b.querySelector('.nsm-label').textContent = c.title || 'Nouveau chat';
    b.onclick = ()=>{
      closeNavSearch();
      closeSettings();
      if(c.workspace) openWorkspace('discussions',c.id); else restoreHistory(c.id);
    };
    box.appendChild(b);
  });
}
function openNavSearch(){
  const modal = document.getElementById('nav-search-modal');
  const inp = document.getElementById('nav-search');
  if(!modal) return;
  modal.hidden = false;
  NAV.q = inp ? inp.value : '';
  renderNavSearch();
  if(inp){ inp.focus(); inp.select(); }
}
function closeNavSearch(){
  const modal = document.getElementById('nav-search-modal');
  if(modal) modal.hidden = true;
}

function renderNav(){
  const boxP = document.getElementById('nav-projects');
  const boxC = document.getElementById('nav-chats');
  if(!boxP || !boxC) return;

  boxP.innerHTML = '';
  NAV.projects.forEach(p=>{
    const wrap = document.createElement('div'); wrap.className = 'nav-proj' + (navIsOpen(p.id)?' open':'');
    const head = document.createElement('button'); head.type='button'; head.className='nav-item nav-proj-h';
    head.innerHTML = navIcon('<path d="M4 20h16a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.7-.9L9.6 3.9A2 2 0 0 0 7.9 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2z"/>')
      + '<span class="nav-label"></span>';
    head.querySelector('.nav-label').textContent = p.name;
    head.onclick = ()=>navToggleOpen(p.id);
    const plus = document.createElement('button'); plus.type='button'; plus.className='nav-more'; plus.title='Nouveau chat';
    plus.textContent = '+';
    plus.onclick = (e)=>{ e.stopPropagation(); newChat(p.id); };
    const more = document.createElement('button'); more.type='button'; more.className='nav-more'; more.title='Plus';
    more.textContent = '···';
    more.onclick = (e)=>{ e.stopPropagation(); navProjectMenu(e, p); };
    head.appendChild(plus); head.appendChild(more);
    wrap.dataset.projectId = p.id;
    wrap.appendChild(head);
    const kids = document.createElement('div'); kids.className = 'nav-proj-chats';
    NAV.conversations.filter(c=>c.project_id===p.id).forEach(c=>kids.appendChild(navChatBtn(c)));
    wrap.appendChild(kids);
    boxP.appendChild(wrap);
  });

  boxC.innerHTML = '';
  const loose = NAV.conversations.filter(c=>!c.project_id);
  const fav = loose.filter(c=>c.fav);
  const rest = loose.filter(c=>!c.fav);
  if(fav.length){
    const h = document.createElement('div'); h.className='nav-head'; h.textContent='Favoris';
    boxC.appendChild(h);
    fav.forEach(c=>boxC.appendChild(navChatBtn(c)));
  }
  rest.forEach(c=>boxC.appendChild(navChatBtn(c)));
  navDndBound();
}

async function loadNav(){
  try{
    const r = await jget('/api/chat/history');
    const unified=await jget('/api/runtime/sessions');
    const sessions=unified.ok?unified.sessions:[];
    const imported=new Set(sessions.flatMap(s=>[s.source_archive,s.native_archive]).filter(Boolean));
    NAV.conversations = [...sessions.map(s=>({...s,workspace:true})),...((r && r.conversations) || []).filter(c=>!imported.has(c.id))];
    NAV.projects = (r && r.projects) || [];
    const bound=sessions.find(s=>s.native_archive===r?.active&&s.runtime_id==='llama.cpp');
    NAV.active = (typeof THREAD!=='undefined'&&THREAD.current?.id) || bound?.id || (r && r.active) || '';
    NAV.project_id = (r && r.project_id) || '';
  }catch(_){}
  renderNav();
  if(typeof workspaceNavChanged==='function') workspaceNavChanged();
  const sm=document.getElementById('nav-search-modal');
  if(sm && !sm.hidden) renderNavSearch();
}

async function newChat(projectId){
  THREAD.resumeID='';
  closeSettings();
  closeNavSearch();
  try{
    const r=await jfetch('/api/chat/reset',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({project_id:projectId||''})});
    if(!r.ok){toast('Impossible de créer la discussion.');return;}
  }catch(_){}
  toast(projectId ? 'nouveau chat du projet' : 'nouvelle conversation');
  loadNav();
  const side=document.getElementById('side');
  if(side && side.classList.contains('open') && window.matchMedia('(max-width:720px)').matches) toggleSide();
}

async function createProjectUI(){
  if(typeof editWorkspace==='function'){ editWorkspace('project'); return; }
  const name = await askPrompt('Nom du projet', {title:'Nouveau projet', okText:'Créer', placeholder:'ex. Docs'});
  if(name===null) return;
  let r; try{ r = await jpost('/api/projects', {name}); }catch(_){ toast('erreur réseau'); return; }
  if(!r.ok){ toast(r.error||'impossible'); return; }
  if(r.project && r.project.id){
    const s = navOpenSet(); s.add(r.project.id); navSaveOpen(s);
  }
  loadNav();
}
async function renameProjectUI(p){
  const name = await askPrompt('Nom du projet', {title:'Renommer', okText:'Enregistrer', default:p.name||''});
  if(name===null) return;
  let r; try{ r = await jpost('/api/projects/rename', {id:p.id, name}); }catch(_){ toast('erreur réseau'); return; }
  if(!r.ok){ toast(r.error||'impossible'); return; }
  loadNav();
}
async function deleteProjectUI(p){
  if(!await askConfirm('Supprimer « '+(p.name||'ce projet')+' » ? Les chats ne sont pas effacés, ils reviennent dans la liste.', {title:'Supprimer le projet', okText:'Supprimer', danger:true})) return;
  let r; try{ r = await jpost('/api/projects/delete', {id:p.id}); }catch(_){ toast('erreur réseau'); return; }
  if(!r.ok){ toast(r.error||'impossible'); return; }
  loadNav();
}

function leaveHubPreview(){
  if(typeof SESSION==='undefined' || !SESSION.preview) return;
  SESSION.preview = false;
  SESSION.dirty = false;
  if(typeof refreshSession==='function') refreshSession(true);
}
function showMainView(name){
  if(typeof workspaceViewChanged==='function') workspaceViewChanged(name);
  const wasHub = document.documentElement.hasAttribute('data-hub');
  const chat = document.getElementById('chat-view');
  const set = document.getElementById('settings-view');
  const hub = document.getElementById('hub-view');
  const srv = document.getElementById('srv-view');
  const bench = document.getElementById('bench-view');
  if(chat) chat.hidden = name !== 'chat';
  if(set) set.hidden = name !== 'settings';
  if(hub) hub.hidden = name !== 'hub';
  if(srv) srv.hidden = name !== 'server';
  if(bench) bench.hidden = name !== 'bench';
  if(name === 'settings') document.documentElement.setAttribute('data-settings','1');
  else {
    document.documentElement.removeAttribute('data-settings');
    document.documentElement.removeAttribute('data-set-page');
  }
  const libHub = name === 'hub' && typeof HUB !== 'undefined' && HUB.place !== 'remote';
  if(name === 'hub'){
    document.documentElement.setAttribute('data-hub','1');
    if(libHub){
      document.documentElement.setAttribute('data-hub-lib','1');
      document.documentElement.removeAttribute('data-hub-remote');
    } else {
      document.documentElement.removeAttribute('data-hub-lib');
      document.documentElement.setAttribute('data-hub-remote','1');
    }
  } else {
    document.documentElement.removeAttribute('data-hub');
    document.documentElement.removeAttribute('data-hub-model');
    document.documentElement.removeAttribute('data-hub-lib');
    document.documentElement.removeAttribute('data-hub-remote');
    if(wasHub) leaveHubPreview();
  }
  if(name === 'server') document.documentElement.setAttribute('data-server','1');
  else document.documentElement.removeAttribute('data-server');
  if(name === 'bench') document.documentElement.setAttribute('data-bench','1');
  else document.documentElement.removeAttribute('data-bench');
  const ns = document.getElementById('nav-settings');
  const nh = document.getElementById('nav-hub');
  const nv = document.getElementById('nav-srv');
  const nb = document.getElementById('nav-bench');
  if(ns) ns.classList.toggle('active', name === 'settings');
  if(nh) nh.classList.toggle('active', name === 'hub' || name === 'models' || name === 'bench');
  if(nv) nv.classList.toggle('active', name === 'server');
  if(nb) nb.classList.toggle('active', name === 'bench');
  const keepParams = name === 'chat' || libHub;
  if(!keepParams && typeof setParamsOpen === 'function') setParamsOpen(false);
  const side = document.getElementById('side');
  if(name !== 'chat' && side && side.classList.contains('open') && window.matchMedia('(max-width:720px)').matches) toggleSide();
  if(typeof srvSyncPoll === 'function') srvSyncPoll();
  if(typeof benchSyncPoll === 'function') benchSyncPoll();
  if(typeof activityAttachToHeader === 'function') activityAttachToHeader(name);
}
function isSettingsStack(){
  return window.matchMedia('(max-width:900px)').matches;
}
function paintSettingsHead(){
  const h=document.getElementById('set-title');
  if(!h) return;
  const page=document.documentElement.getAttribute('data-set-page')==='1';
  const tab=document.querySelector('.set-tab.active');
  h.textContent = (page && tab) ? (tab.textContent||'Réglages') : 'Réglages';
}
function closeSettingsPage(){
  document.documentElement.removeAttribute('data-set-page');
  paintSettingsHead();
}
function settingsBack(){
  if(isSettingsStack() && document.documentElement.getAttribute('data-set-page')==='1'){
    closeSettingsPage();
    return;
  }
  closeSettings();
}
function openSettings(tab){
  const set = document.getElementById('settings-view');
  if(!set) return;
  showMainView('settings');
  if(isSettingsStack() && !tab){
    closeSettingsPage();
    return;
  }
  showSettingsTab(tab || localStorage.getItem('loom-set-tab') || 'apparence');
}
function closeSettings(){
  showMainView('chat');
}
function applySideWidth(collapsed){
  document.documentElement.setAttribute('data-side-collapsed', collapsed?'1':'0');
  try{ localStorage.setItem('loom-side-collapsed', collapsed?'1':'0'); }catch(e){}
  const btn=document.getElementById('side-toggle');
  if(btn){
    btn.title = collapsed ? 'Agrandir la barre' : 'Réduire la barre';
    btn.setAttribute('aria-label', btn.title);
  }
}
function toggleSideWidth(){
  const on = document.documentElement.getAttribute('data-side-collapsed')!=='1';
  applySideWidth(on);
}
function showSettingsTab(id){
  localStorage.setItem('loom-set-tab', id);
  document.querySelectorAll('.set-tab').forEach(b=>b.classList.toggle('active', b.dataset.set===id));
  document.querySelectorAll('#settings-panels [data-set]').forEach(el=>{
    el.hidden = el.dataset.set !== id;
    if(el.dataset.set===id && el.tagName==='DETAILS') el.open = true;
  });
  if(isSettingsStack()) document.documentElement.setAttribute('data-set-page','1');
  paintSettingsHead();
  if(typeof srvOnTab==='function') srvOnTab(id);
  if(id==='activite' && typeof activityPaintSettings==='function') activityPaintSettings();
}

document.addEventListener('DOMContentLoaded', ()=>{
  loadNav();
  applySideWidth(localStorage.getItem('loom-side-collapsed')==='1');
  addEventListener('resize', ()=>{
    const sv=document.getElementById('settings-view');
    if(!sv || sv.hidden) return;
    if(isSettingsStack()) return;
    document.documentElement.removeAttribute('data-set-page');
    showSettingsTab(localStorage.getItem('loom-set-tab')||'apparence');
  });
  document.addEventListener('keydown', e=>{
    const searchOpen = document.getElementById('nav-search-modal') && !document.getElementById('nav-search-modal').hidden;
    if((e.key==='k' || e.key==='K') && (e.metaKey || e.ctrlKey)){
      e.preventDefault();
      if(searchOpen) closeNavSearch(); else openNavSearch();
      return;
    }
    if(e.key==='Escape'){
      if(searchOpen){ closeNavSearch(); return; }
      if(document.documentElement.getAttribute('data-params-open')==='1') return;
      if((document.getElementById('settings-view') && !document.getElementById('settings-view').hidden) ||
         (document.getElementById('hub-view') && !document.getElementById('hub-view').hidden) ||
         (document.getElementById('srv-view') && !document.getElementById('srv-view').hidden)){
        if(document.getElementById('ask-modal') && document.getElementById('ask-modal').classList.contains('show')) return;
        if(typeof hubCloseQuant === 'function' && hubCloseQuant()) return;
        if(document.documentElement.getAttribute('data-hub-model')==='1' && typeof hubCloseModel==='function'){
          hubCloseModel(); return;
        }
        if(document.documentElement.getAttribute('data-set-page')==='1'){
          closeSettingsPage(); return;
        }
        closeSettings();
      }
    }
  });
});
