// Workspace shell. Existing model, chat, MCP and engine controllers retain their
// DOM and APIs. New project context and skills use the server's local store.
const WS = {page:'chat', project:'', session:'', data:null, status:null, request:0, editor:null};
const WS_PAGES = {
  discussions: ['Discussions', '', 'Discussions', 'Un même fil, quel que soit le modèle utilisé.'],
  models: ['Modèles', '', 'Modèles', 'Gérez les modèles locaux et cloud proposés dans vos discussions.'],
  agents: ['Harnesses', '', 'Harnesses', 'Configurez vos harnesses et les modèles à leur associer.'],
  projects: ['Projets', '', 'Projets', 'Regroupez les discussions et leur contexte commun.'],
  capabilities: ['Skills', '', 'Skills', 'Des instructions réutilisables, sélectionnées par projet.'],
  connections: ['Connexions', '', 'Connexions', 'Gérez les serveurs MCP et les outils externes. Les providers IA sont dans Modèles.'],
  services: ['Services', '', 'Services', 'Moteur local, ressources et terminaux de projet.']
};
const WS_ICONS = {
  agents:'<rect x="4" y="7" width="16" height="13" rx="4"/><path d="M12 3v4M9 13h.01M15 13h.01M9 17h6M1 12v4M23 12v4"/>',
  projects:'<path d="M3 7V5a2 2 0 0 1 2-2h5l3 3h6a2 2 0 0 1 2 2v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2Z"/>',
  capabilities:'<path d="m12 3 3 6 6 3-6 3-3 6-3-6-6-3 6-3Z"/>',
  connections:'<path d="m8 12-2 2a4 4 0 0 0 6 6l3-3M16 12l2-2a4 4 0 0 0-6-6L9 7M8 16l8-8"/>',
  services:'<rect x="3" y="4" width="18" height="16" rx="3"/><path d="m7 9 3 3-3 3M13 15h4"/>',
  local:'<rect x="5" y="5" width="14" height="14" rx="3"/><path d="M9 1v4M15 1v4M9 19v4M15 19v4M1 9h4M1 15h4M19 9h4M19 15h4M9 9h6v6H9Z"/>',
  cloud:'<path d="M6 18a5 5 0 0 1-1-10 7 7 0 0 1 13-1 5.5 5.5 0 0 1 0 11Z"/>',
  arrow:'<path d="M5 12h14m-5-5 5 5-5 5"/>'
};
function wsIcon(name){ return navIcon(WS_ICONS[name] || WS_ICONS.agents); }
function wsEl(id){ return document.getElementById(id); }
function wsEsc(value){ return escHtml(String(value || '')); }
function wsButton(label, action, primary){ return '<button type="button" class="ws-button'+(primary?' primary':'')+'" data-ws-action="'+action+'">'+label+'</button>'; }
function wsTag(label, ready){ return '<span class="ws-tag'+(ready?' ready':'')+'">'+(ready?'<span class="ws-dot"></span>':'')+label+'</span>'; }
function wsSection(title, content, extra){ return '<section class="ws-section"><div class="ws-section-head"><h2>'+title+'</h2>'+(extra||'')+'</div>'+content+'</section>'; }
function wsEmpty(icon, title, description, action){
  return '<div class="ws-empty"><span class="ws-empty-icon">'+wsIcon(icon)+'</span><h2>'+title+'</h2><p>'+description+'</p>'+(action||'')+'</div>';
}

function workspaceViewChanged(name){
  if(typeof threadLeaveView==='function') threadLeaveView();
  if(wsEl('ws-hardware-home') && wsEl('side-stats')) wsEl('ws-hardware-home').appendChild(wsEl('side-stats'));
  WS.page = name;
  ++WS.request; // Invalidate a previous page's asynchronous render.
  const workspace = !!WS_PAGES[name];
  wsEl('workspace-view').hidden = !workspace;
  document.documentElement.toggleAttribute('data-workspace', workspace);
  document.documentElement.dataset.page = name;
  document.querySelectorAll('[data-ws-page]').forEach(b=>{
    const active = b.dataset.wsPage===name || (b.dataset.wsPage==='discussions' && name==='chat') || (b.dataset.wsPage==='services' && name==='server') || (b.dataset.wsPage==='models' && (name==='bench'||name==='hub'));
    b.classList.toggle('active', active);
    if(active) b.setAttribute('aria-current','page'); else b.removeAttribute('aria-current');
  });
  const detail=name==='projects'?WS.project:name==='discussions'?WS.session:'';
  const route = '#'+name+(detail?'/'+encodeURIComponent(detail):'');
  if(location.hash!==route) history.pushState(null, '', route);
  // Restore shared MCP controls when leaving Connections. Moving the real DOM
  // avoids duplicate element IDs or two competing configuration controllers.
  const mcp = wsEl('mcp-block');
  if(name!=='connections' && mcp && wsEl('ws-mcp-home')) wsEl('ws-mcp-home').appendChild(mcp);
}

async function openWorkspace(page, projectID){
  if(!WS_PAGES[page]) return;
  if(page==='discussions'){const id=projectID||THREAD.resumeID;if(id)await threadOpen(id);else showMainView('chat');if(window.matchMedia('(max-width:720px)').matches)closeSide();return;}
  WS.project = page==='projects' ? (projectID||'') : '';
  WS.session = page==='discussions' ? (projectID||'') : '';
  showMainView(page);
  const meta = WS_PAGES[page];
  wsEl('ws-kicker').textContent = meta[1];
  wsEl('ws-title').textContent = meta[2];
  wsEl('ws-description').textContent = meta[3];
  wsEl('ws-actions').innerHTML = page==='projects'?wsButton('+ Nouveau projet','new-project',true):page==='capabilities'?wsButton('+ Créer un skill','new-skill',true):page==='discussions'?wsButton('+ Nouvelle discussion','new-thread',true):'';
  // Protect moved controls from being removed when the same page refreshes.
  if(wsEl('ws-mcp-home') && wsEl('mcp-block')) wsEl('ws-mcp-home').appendChild(wsEl('mcp-block'));
  wsEl('ws-body').innerHTML = '<div class="ws-loading" role="status">Chargement de votre espace…</div>';
  wsEl('ws-scroll').scrollTop = 0;
  const request = ++WS.request;
  try{
    const result = await jget('/api/workspace');
    if(!result.ok) throw new Error(result.error || 'Espace indisponible');
    if(request!==WS.request) return;
    WS.data = result;
    renderWorkspace();
    wsEl('ws-title').focus({preventScroll:true});
  }catch(error){
    if(request!==WS.request) return;
    wsEl('ws-body').innerHTML = wsEmpty('connections','Impossible de charger cet espace',wsEsc(error.message),wsButton('Réessayer','retry'));
  }
}

function renderWorkspace(){
  if(!WS.data || !WS_PAGES[WS.page]) return;
  const painters = {discussions:renderDiscussions, models:wsRenderModels, agents:wsRenderHarnesses, projects:wsRenderProjects, capabilities:wsRenderCapabilities, connections:wsRenderConnections, services:wsRenderServices};
  painters[WS.page]();
  if(WS.page==='projects' && WS.project) renderProjectThreads(WS.project);
}
function wsLocalState(){
  if(!WS.status) return 'État en cours de lecture';
  if(WS.status.load_error) return 'Erreur de chargement';
  if(WS.status.health) return 'Modèle prêt';
  if(WS.status.active && WS.status.model) return 'Chargement du modèle';
  return 'Aucun modèle chargé';
}
function wsRenderProjects(){
  const projects = WS.data.projects;
  if(WS.project){
    const p = projects.find(p=>p.id===WS.project);
    if(p){ wsRenderProject(p); return; }
    WS.project='';
    history.replaceState(null,'','#projects');
  }
  const conversations = NAV.conversations || [];
  const cards = projects.map(p=>{
    const count = conversations.filter(c=>c.project_id===p.id).length;
    return '<button type="button" class="ws-project-card" data-ws-project="'+wsEsc(p.id)+'"><div class="ws-card-top">'+wsIcon('projects')+'<span>'+count+' discussion'+(count===1?'':'s')+'</span></div><h2>'+wsEsc(p.name)+'</h2><p>'+wsEsc(p.instructions || 'Ajoutez des instructions pour partager le contexte entre vos discussions.')+'</p><div class="ws-card-bottom"><span>'+(p.capability_ids||[]).filter(id=>WS.data.capabilities.some(c=>c.id===id)).length+' skill(s) · '+(p.directory?'Dossier lié':'Sans dossier')+'</span>'+wsIcon('arrow')+'</div></button>';
  }).join('');
  wsEl('ws-body').innerHTML = projects.length?'<div class="ws-project-grid">'+cards+'</div>':wsEmpty('projects','Aucun projet','Un projet garde vos discussions ensemble et partage les instructions que vous choisissez.',wsButton('Créer mon premier projet','new-project',true));
  wsEl('ws-body').innerHTML += '<div class="ws-footnote">'+wsIcon('projects')+'<p>Le contexte est ajouté aux prochains messages du projet. Vos discussions existantes restent dans leur historique.</p></div>';
}
function wsRenderProject(p){
  wsEl('ws-title').textContent = p.name;
  wsEl('ws-description').textContent = 'Le contexte commun à vos discussions et à vos futurs agents.';
  wsEl('ws-actions').innerHTML = wsButton('← Tous les projets','projects')+wsButton('Nouvelle discussion','project-chat',true);
  const selected = WS.data.capabilities.filter(c=>(p.capability_ids||[]).includes(c.id));
  const chats = NAV.conversations.filter(c=>c.project_id===p.id&&!c.workspace);
  wsEl('ws-body').innerHTML = '<div class="ws-project-detail"><section class="ws-panel"><div class="ws-section-head"><h2>Contexte du projet</h2>'+wsButton('Modifier','edit-project')+'</div><p class="ws-note">Instructions partagées</p><div class="ws-context-text">'+wsEsc(p.instructions || 'Aucune instruction pour le moment.')+'</div><div class="ws-detail-line"><span>Dossier de travail</span><code>'+wsEsc(p.directory || 'Non renseigné')+'</code></div><p class="ws-note">Le dossier prépare les futures sessions de terminal et de harness. Son contenu n’est pas lu automatiquement.</p></section><section class="ws-panel"><div class="ws-section-head"><h2>Skills sélectionnés</h2><span class="ws-count">'+selected.length+'</span></div>'+(selected.length?selected.map(c=>'<div class="ws-skill-mini">'+wsIcon('capabilities')+'<div><strong>'+wsEsc(c.name)+'</strong><p>'+wsEsc(c.description)+'</p></div></div>').join(''):'<p class="ws-muted">Sélectionnez des skills dans « Modifier » pour les utiliser dans ce projet.</p>')+'</section></div>'+wsSection('Archives existantes',chats.length?'<div class="ws-row-list">'+chats.map(c=>'<button type="button" class="ws-discussion-row" data-ws-chat="'+wsEsc(c.id)+'"><span>'+wsEsc(c.title||'Nouvelle discussion')+'</span>'+wsIcon('arrow')+'</button>').join('')+'</div>':'<p class="ws-note">Aucune archive antérieure pour ce projet.</p>');
}

function wsRenderCapabilities(){
  const skills = WS.data.capabilities;
  const list = skills.map(c=>'<article class="ws-skill-card"><div class="ws-card-top">'+wsIcon('capabilities')+wsTag('Instructions')+'</div><h2>'+wsEsc(c.name)+'</h2><p>'+wsEsc(c.description || 'Instructions réutilisables pour vos projets.')+'</p><div class="ws-card-bottom"><span>'+WS.data.projects.filter(p=>(p.capability_ids||[]).includes(c.id)).length+' projet(s)</span><div><button type="button" class="ws-button" data-ws-edit-skill="'+wsEsc(c.id)+'">Modifier</button><button type="button" class="ws-icon-button" data-ws-delete-skill="'+wsEsc(c.id)+'" aria-label="Supprimer '+wsEsc(c.name)+'">×</button></div></div></article>').join('');
  wsEl('ws-body').innerHTML = wsSection('Vos skills',skills.length?'<div class="ws-project-grid">'+list+'</div>':wsEmpty('capabilities','Aucun skill','Une méthode de revue, un style d’écriture ou une convention de code. Créez un skill, puis sélectionnez-le dans vos projets.',wsButton('Créer un skill','new-skill',true)))+wsSection('Outils intégrés','<div class="ws-tools-grid"><button type="button" data-ws-action="memory"><strong>Mémoire</strong><span>Rappeler des informations à la demande</span>→</button><button type="button" data-ws-action="internet"><strong>Recherche web</strong><span>Activer Internet dans une discussion</span>→</button><button type="button" data-ws-action="connections"><strong>Outils MCP</strong><span>Connecter vos sources et services</span>→</button></div>')+'<p class="ws-note">Un skill fournit des instructions. Les accès aux outils restent réglés séparément, par discussion.</p>';
}

function wsRenderConnections(){
  wsEl('ws-body').innerHTML = wsSection('Serveurs MCP','<div class="ws-panel" id="ws-mcp-slot"></div>')+'<p class="ws-note">La configuration MCP existante est conservée. Ses outils sont disponibles dans les discussions avec un modèle local. Le connecteur cloud reste textuel ; chaque futur harness devra confirmer ses accès MCP.</p>'+wsButton('Providers IA →','providers');
  wsEl('ws-mcp-slot').appendChild(wsEl('mcp-block'));
  loadMCP().catch(()=>{ wsEl('mcp-list').textContent='Impossible de lire les serveurs MCP. Réessayez en rouvrant Connexions.'; });
}

function wsRenderServices(){
  const linked = WS.data.projects.filter(p=>p.directory);
  wsEl('ws-body').innerHTML = '<div class="ws-services-layout"><section class="ws-panel"><div class="ws-section-head"><h2>Moteur local</h2>'+wsTag('llama.cpp')+'</div><h3 class="ws-service-status" id="ws-local-state">'+wsLocalState()+'</h3><p id="ws-service-model" class="ws-muted">'+wsEsc(WS.status&&WS.status.model_name || 'Choisissez un modèle dans votre bibliothèque.')+'</p><div class="ws-button-row">'+wsButton('Ouvrir le serveur','server',true)+wsButton('Configurer le moteur','engine')+'</div><p class="ws-note">État du serveur, requêtes, slots, clé API et exposition réseau.</p></section><section class="ws-panel"><div class="ws-section-head"><h2>Ressources machine</h2></div><div id="ws-hardware"></div></section></div>'+wsSection('Terminaux de projet','<div class="ws-panel"><div class="ws-section-head"><h3>Vos dossiers, prêts pour la suite.</h3>'+wsTag('Terminal à venir')+'</div><p class="ws-muted">Les terminaux interactifs seront attachés à un projet pour lancer vos applications et suivre vos services.</p>'+(linked.length?'<div class="ws-folder-list">'+linked.map(p=>'<div>'+wsIcon('projects')+'<strong>'+wsEsc(p.name)+'</strong><code>'+wsEsc(p.directory)+'</code></div>').join('')+'</div>':'<p class="ws-note">Renseignez un dossier de travail dans un projet pour préparer cet espace.</p>')+wsButton('Gérer les projets','projects')+'</div>');
  wsEl('ws-hardware').appendChild(wsEl('side-stats'));
}

function workspaceStatusChanged(status){
  WS.status=status;
  if(typeof threadSyncLocalControls==='function')threadSyncLocalControls();
  if(wsEl('ws-local-state')) wsEl('ws-local-state').textContent=wsLocalState();
  if(wsEl('ws-service-model')) wsEl('ws-service-model').textContent=status.model_name || 'Choisissez un modèle dans votre bibliothèque.';
}
function workspaceNavChanged(){
  // Project saves refresh this read model, without replacing an open form.
  if(WS.data) WS.data.projects=NAV.projects;
}

function wsField(label, name, value, options){
  options=options||{};
  const wrap=document.createElement('label'); wrap.className='ws-field';
  const title=document.createElement('span'); title.textContent=label; wrap.appendChild(title);
  const input=document.createElement(options.multiline?'textarea':'input'); input.name=name; input.value=value||'';
  input.required=!!options.required; input.maxLength=options.maxLength||12000;
  if(options.multiline) input.rows=options.rows||5;
  if(options.placeholder) input.placeholder=options.placeholder;
  wrap.appendChild(input);
  if(options.note){ const note=document.createElement('small'); note.textContent=options.note; wrap.appendChild(note); }
  return wrap;
}
async function editWorkspace(kind, id){
  if(!WS.data){
    try{ WS.data=await jget('/api/workspace'); if(!WS.data.ok) throw new Error(''); }
    catch(_){ toast('Impossible de charger le workspace'); return; }
  }
  const item=kind==='project'?WS.data.projects.find(p=>p.id===id):WS.data.capabilities.find(c=>c.id===id);
  WS.editor={kind, id:id||'', item:item||{}};
  wsEl('ws-editor-title').textContent=(id?'Modifier le ':'Nouveau ')+(kind==='project'?'projet':'skill');
  wsEl('ws-editor-kicker').textContent=kind==='project'?'Contexte partagé':'Capacité réutilisable';
  wsEl('ws-editor-error').hidden=true;
  const fields=wsEl('ws-editor-fields'); fields.replaceChildren();
  fields.appendChild(wsField('Nom','name',item&&item.name,{required:true,maxLength:kind==='project'?80:160,placeholder:kind==='project'?'Ex. Mon prochain produit':'Ex. Revue de code'}));
  if(kind==='project'){
    fields.appendChild(wsField('Dossier de travail','directory',item&&item.directory,{placeholder:'Chemin absolu vers un dossier existant',note:'Facultatif. Aucun fichier n’est lu ou envoyé automatiquement.'}));
    fields.appendChild(wsField('Instructions du projet','instructions',item&&item.instructions,{multiline:true,note:'Ajoutées aux prochains messages des discussions de ce projet.'}));
    const group=document.createElement('fieldset'); group.className='ws-skill-picker';
    const legend=document.createElement('legend'); legend.textContent='Skills du projet'; group.appendChild(legend);
    WS.data.capabilities.forEach(c=>{
      const label=document.createElement('label'), check=document.createElement('input'); check.type='checkbox'; check.name='capability_ids'; check.value=c.id; check.checked=!!(item&&item.capability_ids||[]).includes(c.id);
      label.append(check,document.createTextNode(c.name)); group.appendChild(label);
    });
    if(!WS.data.capabilities.length){ const note=document.createElement('p'); note.className='ws-note'; note.textContent='Créez vos premiers skills dans Skills & capacités pour les sélectionner ici.'; group.appendChild(note); }
    fields.appendChild(group);
  }else{
    fields.appendChild(wsField('Description','description',item&&item.description,{maxLength:500,placeholder:'Quand utiliser ce skill ?'}));
    fields.appendChild(wsField('Instructions','instructions',item&&item.instructions,{multiline:true,required:true,maxLength:8000,rows:8,note:'Ces instructions sont injectées seulement dans les projets qui sélectionnent ce skill.'}));
  }
  wsEl('ws-editor').showModal();
  fields.querySelector('input').focus();
}
function closeWorkspaceEditor(){ wsEl('ws-editor').close(); }
async function saveWorkspaceEditor(event){
  event.preventDefault();
  if(WS.editor?.kind==='discussion'){await saveDiscussionEditor(event);return;}
  if(WS.editor && (WS.editor.kind==='provider' || WS.editor.kind==='harness')){ await saveRuntimeEditor(event); return; }
  const editor=WS.editor, form=new FormData(event.target), button=wsEl('ws-editor-save');
  button.disabled=true; wsEl('ws-editor-error').hidden=true;
  try{
    let result;
    if(editor.kind==='project'){
      result=await jpost('/api/projects/context',{id:editor.id,name:form.get('name'),directory:form.get('directory'),instructions:form.get('instructions'),capability_ids:form.getAll('capability_ids')});
    }else{
      result=await jpost('/api/capabilities/save',{id:editor.id,name:form.get('name'),description:form.get('description'),instructions:form.get('instructions')});
    }
    if(!result.ok) throw new Error(result.error || 'Enregistrement impossible');
    if(editor.kind==='project') editor.id=result.project.id;
    closeWorkspaceEditor(); toast(editor.kind==='project'?'Projet enregistré':'Skill enregistré');
    await loadNav();
    openWorkspace(editor.kind==='project'?'projects':'capabilities',editor.kind==='project'?editor.id:'');
  }catch(error){ wsEl('ws-editor-error').textContent=error.message||'Erreur réseau'; wsEl('ws-editor-error').hidden=false; }
  finally{ button.disabled=false; }
}

async function wsAction(action){
  const actions={
    'new-project':()=>editWorkspace('project'), 'new-skill':()=>editWorkspace('skill'),
    'new-provider':()=>editProvider(), 'new-thread':()=>newChat(''),
    discussions:()=>openWorkspace('discussions'), 'local-chat':()=>showMainView('chat'),
    'edit-project':()=>editWorkspace('project',WS.project), 'project-chat':()=>newChat(WS.project),
    harnesses:()=>openWorkspace('agents'),
    projects:()=>openWorkspace('projects'), models:()=>openWorkspace('models'), server:()=>openServer(),
    library:()=>openLibrary(), download:()=>openHub(), providers:()=>{THREAD.modelsTab='providers';openWorkspace('models');},
    engine:()=>openSettings('moteur'), memory:()=>openSettings('params'), internet:()=>openSettings('internet'),
    connections:()=>openWorkspace('connections'), retry:()=>openWorkspace(WS.page,WS.page==='discussions'?WS.session:WS.project)
  };
  if(actions[action]) await actions[action]();
}
function wsRouteFromURL(){
  const [page,id]=location.hash.slice(1).split('/');
  let projectID='';
  try{ projectID=id?decodeURIComponent(id):''; }catch(_){}
  if(WS_PAGES[page]){ openWorkspace(page,projectID); return; }
  const routes={chat:()=>showMainView('chat'),local:()=>showMainView('chat'),hub:openLibrary,settings:openSettings,server:openServer,bench:openBench};
  (routes[page] || routes.chat)();
}

document.addEventListener('DOMContentLoaded',()=>{
  const nav=wsEl('sidenav'), models=wsEl('nav-hub');
  const discussion=wsEl('nav-new'); discussion.onclick=()=>openWorkspace('discussions'); discussion.dataset.wsPage='discussions'; discussion.title='Discussions';
  models.dataset.wsPage='models'; models.title='Modèles locaux et cloud';
  wsEl('nav-settings').dataset.wsPage='settings'; wsEl('nav-settings').title='Réglages';
  ['agents','projects','capabilities','connections','services'].forEach(page=>{
    const button=document.createElement('button'); button.type='button'; button.className='nav-item'; button.dataset.wsPage=page; button.title=WS_PAGES[page][0];
    button.innerHTML=wsIcon(page)+'<span class="nav-label">'+WS_PAGES[page][0]+'</span>'; button.onclick=()=>openWorkspace(page);
    nav.insertBefore(button,wsEl('nav-srv'));
  });
  const mcpHome=document.createElement('div'); mcpHome.id='ws-mcp-home'; wsEl('mcp-block').parentElement.appendChild(mcpHome); mcpHome.appendChild(wsEl('mcp-block'));
  // Preserve the live hardware DOM in a parking node when Services rerenders.
  const parking=document.createElement('div'); parking.id='ws-hardware-home'; parking.hidden=true; document.body.appendChild(parking); parking.appendChild(wsEl('side-stats'));
  wsEl('ws-editor-form').addEventListener('submit',saveWorkspaceEditor);
  document.addEventListener('click',async event=>{
    const target=event.target.closest('[data-ws-action],[data-ws-project],[data-ws-chat],[data-ws-edit-skill],[data-ws-delete-skill]');
    if(!target) return;
    if(target.dataset.wsAction){ await wsAction(target.dataset.wsAction); return; }
    if(target.dataset.wsProject){ openWorkspace('projects',target.dataset.wsProject); return; }
    if(target.dataset.wsChat){ restoreHistory(target.dataset.wsChat); return; }
    if(target.dataset.wsEditSkill){ editWorkspace('skill',target.dataset.wsEditSkill); return; }
    if(target.dataset.wsDeleteSkill){
      const c=WS.data.capabilities.find(c=>c.id===target.dataset.wsDeleteSkill);
      if(!c || !await askConfirm('Supprimer « '+c.name+' » ? Ses instructions ne seront plus ajoutées aux prochains messages des projets.',{title:'Supprimer le skill',okText:'Supprimer',danger:true})) return;
      try{ const r=await jpost('/api/capabilities/delete',{id:c.id}); if(!r.ok) throw new Error(r.error); openWorkspace('capabilities'); }
      catch(error){ toast(error.message || 'Suppression impossible'); }
    }
  });
  window.addEventListener('popstate',wsRouteFromURL);
  const syncDrawer=()=>{
    const drawer=window.matchMedia('(max-width:720px)').matches;
    wsEl('side').inert=drawer && !wsEl('side').classList.contains('open');
    wsEl('menubtn').setAttribute('aria-expanded',String(wsEl('side').classList.contains('open')));
  };
  new MutationObserver(syncDrawer).observe(wsEl('side'),{attributes:true,attributeFilter:['class']});
  window.addEventListener('resize',syncDrawer); syncDrawer();
  document.addEventListener('keydown',event=>{
    if(event.key==='Escape' && wsEl('side').classList.contains('open') && window.matchMedia('(max-width:720px)').matches){
      closeSide(); wsEl('menubtn').focus();
    }
  });
  wsRouteFromURL();
});
