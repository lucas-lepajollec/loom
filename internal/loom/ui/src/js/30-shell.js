// ===== Shell Loom ============================================================
// Structure de l'interface : Local · Cloud · Harnesses · Ressources · Usage,
// un sélecteur d'exécution à onglets et une carte moteur. Ce module réorganise
// l'existant sans le réécrire : les contrôleurs (bibliothèque, Hub, serveur,
// bench, panneau de paramètres, discussions) gardent leur DOM et leurs API.

const SHELL={tab:'local',resTab:'skills',cloudOnly:false,gpus:[],status:null,chatObs:null};
const SHELL_ICONS={
  local:'<rect x="5" y="5" width="14" height="14" rx="3"/><path d="M9 9h6v6H9zM9 2v3M15 2v3M9 19v3M15 19v3M2 9h3M2 15h3M19 9h3M19 15h3"/>',
  cloud:'<path d="M7 18a5 5 0 0 1-.9-9.9A6.5 6.5 0 0 1 18.6 9 4.5 4.5 0 0 1 17.5 18Z"/>',
  harnesses:'<rect x="2" y="3" width="20" height="18" rx="3"/><path d="m6 9 3 3-3 3M12 15h6"/>',
  resources:'<path d="m12 3 8 4.5v9L12 21l-8-4.5v-9Z"/><path d="m4 7.5 8 4.5 8-4.5M12 12v9"/>',
  usage:'<path d="M4 20V10M10 20V4M16 20v-7M22 20H2"/>',
  search:'<circle cx="11" cy="11" r="7"/><path d="m20 20-3.5-3.5"/>'
};
function shellIcon(name){return '<svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">'+SHELL_ICONS[name]+'</svg>';}
const SHELL_NAV=[
  ['local','Local',()=>shellOpenLocal('library')],
  ['cloud','Cloud',()=>shellOpenCloud()],
  ['harnesses','Harnesses',()=>{SHELL.cloudOnly=false;openWorkspace('agents');}],
  ['resources','Ressources',()=>{SHELL.cloudOnly=false;openWorkspace('resources');}],
  ['usage','Usage',()=>{SHELL.cloudOnly=false;openWorkspace('usage');}]
];

// ---------- navigation ----------
function shellSection(view){
  if(['hub','server','bench'].includes(view))return 'local';
  if(view==='models')return SHELL.cloudOnly?'cloud':'local';
  if(view==='agents')return 'harnesses';
  if(['resources','capabilities','connections'].includes(view))return 'resources';
  if(view==='usage')return 'usage';
  return '';
}
function shellSyncNav(view){
  const section=shellSection(view);
  document.querySelectorAll('.shell-nav [data-shell]').forEach(b=>{
    const on=b.dataset.shell===section;
    b.classList.toggle('active',on);
    if(on)b.setAttribute('aria-current','page');else b.removeAttribute('aria-current');
  });
  if(section==='local')shellPaintLocalTabs(view);
}
function shellBuildNav(){
  const nav=wsEl('sidenav');if(!nav)return;
  // Les anciennes entrées restent dans le DOM (d'autres modules les lisent)
  // mais ne s'affichent plus : la nouvelle navigation les remplace.
  nav.querySelectorAll(':scope > .nav-item').forEach(b=>{b.hidden=true;b.setAttribute('aria-hidden','true');});
  const box=document.createElement('div');box.className='shell-nav';box.setAttribute('role','list');
  SHELL_NAV.forEach(([id,label,open])=>{
    const b=document.createElement('button');b.type='button';b.className='nav-item';b.dataset.shell=id;b.setAttribute('role','listitem');
    b.innerHTML=shellIcon(id)+'<span class="nav-label">'+label+'</span>';b.title=label;
    b.onclick=()=>{open();if(window.matchMedia('(max-width:720px)').matches&&typeof closeSide==='function')closeSide();};
    box.appendChild(b);
  });
  nav.insertBefore(box,nav.querySelector('.nav-section'));
}

// ---------- Local : une page, quatre onglets ----------
const LOCAL_TABS=[['library','Bibliothèque'],['hub','Hub'],['engine','Moteur & API'],['bench','Bench']];
function shellLocalTab(view){
  if(view==='server')return 'engine';
  if(view==='bench')return 'bench';
  if(view==='hub')return (typeof HUB!=='undefined'&&HUB.place==='remote')?'hub':'library';
  return 'library';
}
function shellOpenLocal(tab){
  SHELL.cloudOnly=false;
  ({library:()=>openLibrary(),hub:()=>openHub(),engine:()=>openServer(),bench:()=>openBench()})[tab]();
}
function shellLocalHead(viewId){
  const view=wsEl(viewId);if(!view||view.querySelector('.local-head'))return;
  const head=document.createElement('div');head.className='local-head';
  head.innerHTML='<h1>Local</h1><p>Tes modèles sur cette machine, servis par llama.cpp.</p><div class="local-tabs" role="tablist" aria-label="Local">'+
    LOCAL_TABS.map(([id,label])=>'<button type="button" role="tab" data-local-tab="'+id+'" aria-selected="false">'+label+'</button>').join('')+'</div>';
  view.insertBefore(head,view.firstElementChild);
}
function shellPaintLocalTabs(view){
  const current=shellLocalTab(view);
  document.querySelectorAll('[data-local-tab]').forEach(b=>b.setAttribute('aria-selected',String(b.dataset.localTab===current)));
}

// ---------- Cloud ----------
function shellOpenCloud(){
  SHELL.cloudOnly=true;
  if(THREAD.modelsTab==='local')THREAD.modelsTab='cloud';
  openWorkspace('models');
}

// ---------- Ressources et Usage : pages du workspace ----------
function shellResourcesTabs(){
  return '<div class="workspace-tabs" role="tablist" aria-label="Ressources">'+[['skills','Skills'],['mcp','Serveurs MCP']].map(([id,label])=>
    '<button type="button" role="tab" data-res-tab="'+id+'" class="'+(SHELL.resTab===id?'selected':'')+'" aria-pressed="'+(SHELL.resTab===id)+'">'+label+'</button>').join('')+'</div>';
}
function shellRenderResources(){
  wsEl('ws-actions').innerHTML=SHELL.resTab==='skills'?wsButton('Créer un skill','new-skill',true):'';
  if(SHELL.resTab==='mcp')wsRenderConnections();else wsRenderCapabilities();
  wsEl('ws-body').insertAdjacentHTML('afterbegin',shellResourcesTabs());
}
async function shellRenderUsage(){
  wsEl('ws-actions').innerHTML='';
  wsEl('ws-body').innerHTML='<div class="usage-page"><div class="usage-body" aria-live="polite"><div class="ws-loading" role="status">Lecture de l’usage enregistré…</div></div></div>';
  const box=wsEl('ws-body').querySelector('.usage-body');
  try{const r=await jget('/api/usage');if(WS.page!=='usage')return;if(!r.ok)throw new Error(r.error);USAGE.data=r;renderUsage(r,box);}
  catch(e){box.textContent='Lecture indisponible : '+e.message;}
}

// ---------- Sélecteur d'exécution ----------
function shellPickerSetup(){
  const menu=wsEl('model-menu');if(!menu||menu.querySelector('.pick-top'))return;
  const secs=menu.querySelectorAll(':scope > .mpick-sec');
  if(secs[0])secs[0].dataset.exec='local';
  if(secs[1])secs[1].dataset.exec='local';
  wsEl('model-menu-cloud-section').dataset.exec='cloud';
  wsEl('model-menu-harness-section').dataset.exec='harness';
  const top=document.createElement('div');top.className='pick-top';
  top.innerHTML='<label class="pick-search">'+shellIcon('search')+'<input type="search" id="pick-q" placeholder="Rechercher un modèle, un preset, un harness…" autocomplete="off" aria-label="Rechercher un modèle"></label>'+
    '<div class="seg" role="tablist" aria-label="Type d’exécution"><span class="seg-ind" aria-hidden="true"></span>'+
    [['local','Local'],['cloud','Cloud'],['harness','Harness']].map(([id,label])=>'<button type="button" role="tab" data-pick-tab="'+id+'" aria-selected="false">'+label+' <span class="n" data-pick-count="'+id+'"></span></button>').join('')+'</div>';
  menu.insertBefore(top,menu.firstChild);
  const empty=document.createElement('div');empty.className='pick-empty';empty.hidden=true;menu.appendChild(empty);
  const foot=document.createElement('div');foot.className='pick-foot';
  foot.innerHTML='<span>Changer de modèle garde la même discussion.</span><button type="button" data-pick-manage>Gérer</button>';
  menu.appendChild(foot);
  wsEl('pick-q').addEventListener('input',shellPickerFilter);
  wsEl('pick-q').addEventListener('keydown',e=>{if(e.key==='Escape'){closeModelMenu();wsEl('model-pick-btn').focus();}});
  top.addEventListener('click',e=>{const b=e.target.closest('[data-pick-tab]');if(b)shellPickerTab(b.dataset.pickTab);});
  foot.querySelector('[data-pick-manage]').onclick=()=>{closeModelMenu();({local:()=>{SHELL.cloudOnly=false;THREAD.modelsTab='local';openWorkspace('models');},cloud:()=>shellOpenCloud(),harness:()=>openWorkspace('agents')})[SHELL.tab]();};
}
function shellItemsFor(tab){
  return [...wsEl('model-menu').querySelectorAll('.mpick-sec[data-exec="'+tab+'"] .mpick-item')].filter(r=>!r.hidden);
}
function shellPickerCounts(){
  ['local','cloud','harness'].forEach(t=>{const el=document.querySelector('[data-pick-count="'+t+'"]');if(el)el.textContent=shellItemsFor(t).length||'';});
}
function shellPickerTab(tab){
  SHELL.tab=tab;
  const menu=wsEl('model-menu');menu.dataset.tab=tab;
  menu.querySelectorAll('[data-pick-tab]').forEach(b=>b.setAttribute('aria-selected',String(b.dataset.pickTab===tab)));
  // Les sections cloud/harness sont masquées par leur propre module quand vides.
  ['cloud','harness'].forEach(k=>{const s=wsEl('model-menu-'+k+'-section');if(s&&k===tab)s.hidden=false;});
  shellSegIndicator(menu.querySelector('.seg'));
  shellPickerFilter();
}
function shellPickerFilter(){
  const q=(wsEl('pick-q')?.value||'').trim().toLowerCase();
  const menu=wsEl('model-menu');let shown=0;
  menu.querySelectorAll('.mpick-sec[data-exec="'+SHELL.tab+'"] .mpick-item').forEach(r=>{
    if(r.dataset.catalogHidden==='1'){r.hidden=true;return;}
    const match=!q||r.textContent.toLowerCase().includes(q);r.hidden=!match;if(match)shown++;
  });
  const empty=menu.querySelector('.pick-empty');
  empty.hidden=shown>0;
  empty.textContent=q?'Aucun résultat pour « '+q+' ».':({local:'Aucun modèle local. Télécharge un GGUF depuis le Hub.',cloud:'Aucun modèle cloud. Ajoute un fournisseur dans Cloud.',harness:'Aucun harness connecté. Connecte-en un dans Harnesses.'})[SHELL.tab];
  shellPickerCounts();
}
function shellSegIndicator(seg){
  if(!seg)return;const on=seg.querySelector('[aria-selected="true"],.on'),ind=seg.querySelector('.seg-ind');if(!on||!ind)return;
  ind.style.width=on.offsetWidth+'px';ind.style.transform='translateX('+on.offsetLeft+'px)';
}
function shellExecKind(){
  try{return typeof threadPanelMode==='function'?threadPanelMode():'local';}catch(_){return 'local';}
}
function shellSyncExecTag(){
  const btn=wsEl('model-pick-btn'),label=wsEl('model-pick-label');if(!btn||!label)return;
  let tag=btn.querySelector('.exec-tag');
  if(!tag){tag=document.createElement('span');tag.className='exec-tag';btn.insertBefore(tag,label);}
  const kind=shellExecKind(),empty=/^Choisir un modèle$/.test(label.textContent.trim());
  tag.hidden=empty;tag.dataset.kind=kind;tag.textContent=({local:'LOCAL',cloud:'CLOUD',harness:'HARNESS'})[kind]||'LOCAL';
}

// ---------- Carte moteur ----------
function shellBuildEngineCard(){
  const foot=document.querySelector('.nav-foot');if(!foot||wsEl('eng-card'))return;
  const card=document.createElement('button');card.type='button';card.id='eng-card';card.className='eng-card';card.dataset.state='off';
  card.innerHTML='<span class="eng-card-top"><span class="eng-dot" aria-hidden="true"></span><span class="eng-name" id="eng-card-name">llama.cpp</span><span class="eng-build">local</span></span>'+
    '<span class="eng-track" aria-hidden="true"><span class="eng-fill" id="eng-card-fill"></span></span><span class="eng-meta" id="eng-card-meta">Lecture de l’état…</span>';
  card.onclick=()=>{shellOpenLocal('engine');if(window.matchMedia('(max-width:720px)').matches&&typeof closeSide==='function')closeSide();};
  foot.insertBefore(card,foot.firstChild);
  // Démarrer/arrêter et le journal vivent désormais dans Local › Moteur & API.
  const eng=wsEl('side-engine'),srv=wsEl('srv-view');
  if(eng&&srv){const slot=document.createElement('div');slot.className='local-engine-slot';slot.appendChild(eng);srv.querySelector('.local-head')?.appendChild(slot);}
}
function shellPaintEngine(){
  const s=SHELL.status,card=wsEl('eng-card');if(!card)return;
  let state='off',name='Moteur arrêté';
  if(s){
    if(s.load_error){state='error';name='Erreur de chargement';}
    else if(s.health&&s.model){state='ready';name=s.preset_name||s.model_name||'Modèle prêt';}
    else if(s.active&&s.model){state='loading';name='Chargement · '+(s.preset_name||s.model_name||'');}
    else if(s.active){state='idle';name='Moteur prêt · aucun modèle';}
  }
  card.dataset.state=state;wsEl('eng-card-name').textContent=name;
  const g=(SHELL.gpus||[])[0],fill=wsEl('eng-card-fill');
  if(g&&g.total){const pct=Math.round(g.used*100/g.total);fill.style.width=pct+'%';fill.classList.toggle('hot',pct>=92);
    wsEl('eng-card-meta').textContent='VRAM '+(g.used/1024).toFixed(1)+' / '+(g.total/1024).toFixed(1)+' GB · GPU '+g.util+' %';}
  else{fill.style.width='0';wsEl('eng-card-meta').textContent=s&&s.active?'Pas de GPU détecté':'Clique pour démarrer le moteur';}
  card.setAttribute('aria-label','Moteur local : '+name);
}

// La carte moteur lit la VRAM elle-même, au plus toutes les 4 s et seulement
// quand l'onglet est visible : l'ancien bloc matériel n'est plus affiché partout.
let shellVramAt=0;
async function shellPollVram(){
  if(document.hidden||Date.now()-shellVramAt<4000)return;
  shellVramAt=Date.now();
  try{SHELL.gpus=(await jget('/api/vram'))||[];}catch(_){SHELL.gpus=[];}
  shellPaintEngine();
}

// ---------- Panneau de droite : Paramètres | Contexte ----------
function shellInspectorSetup(){
  const head=document.querySelector('#params .params-head'),ctx=wsEl('params-context');if(!head||!ctx||wsEl('insp-seg'))return;
  const seg=document.createElement('div');seg.className='seg insp-seg';seg.id='insp-seg';seg.setAttribute('role','tablist');seg.hidden=true;
  seg.innerHTML='<span class="seg-ind" aria-hidden="true"></span><button type="button" role="tab" data-insp="params" aria-selected="true">Paramètres</button><button type="button" role="tab" data-insp="context" aria-selected="false">Contexte</button>';
  head.after(seg);
  seg.addEventListener('click',e=>{const b=e.target.closest('[data-insp]');if(b)shellInspector(b.dataset.insp);});
  const sync=()=>{const has=!ctx.hidden&&!wsEl('params-body').hidden;seg.hidden=!has;if(!has)shellInspector('params');else requestAnimationFrame(()=>shellSegIndicator(seg));};
  new MutationObserver(sync).observe(ctx,{attributes:true,attributeFilter:['hidden']});
  new MutationObserver(sync).observe(wsEl('params-body'),{attributes:true,attributeFilter:['hidden']});
  sync();
}
function shellInspector(tab){
  const p=wsEl('params'),seg=wsEl('insp-seg');if(!p||!seg)return;
  p.dataset.insp=tab;
  seg.querySelectorAll('[data-insp]').forEach(b=>b.setAttribute('aria-selected',String(b.dataset.insp===tab)));
  if(tab==='context')wsEl('params-context').open=true;
  shellSegIndicator(seg);
}

// ---------- Messages : entrée animée des nouveaux messages seulement ----------
function shellWatchChat(){
  const chat=wsEl('chat');if(!chat||SHELL.chatObs)return;
  SHELL.chatObs=new MutationObserver(list=>{
    if(typeof REPLAYING!=='undefined'&&REPLAYING)return;
    list.forEach(m=>m.addedNodes.forEach(n=>{if(n.nodeType===1&&n.classList.contains('msg')){n.classList.add('enter');n.addEventListener('animationend',()=>n.classList.remove('enter'),{once:true});}}));
  });
  SHELL.chatObs.observe(chat,{childList:true});
}

// ---------- branchements sur l'existant ----------
function shellWrap(){
  const origShow=showMainView;
  showMainView=function(name){origShow(name);shellSyncNav(name);};
  const origStatus=workspaceStatusChanged;
  workspaceStatusChanged=function(status){origStatus(status);SHELL.status=status;shellPaintEngine();shellSyncExecTag();shellPollVram();};
  loadVram=async function(){
    const gpus=await jget('/api/vram');SHELL.gpus=gpus||[];
    wsEl('vram').innerHTML=(gpus||[]).map(g=>{const pct=Math.round(g.used*100/g.total);
      return '<div class="stat"><div class="stat-h"><span class="stat-n">'+escHtml(g.name)+'</span><span class="stat-v">'+(g.used/1024).toFixed(1)+' / '+(g.total/1024).toFixed(1)+' GiB</span></div><div class="bar"><div style="width:'+pct+'%"></div></div><div class="stat-s">GPU '+g.util+' % · '+g.temp+' °C</div></div>';
    }).join('')||'<div class="stat"><span class="stat-s">(pas de GPU)</span></div>';
    shellPaintEngine();
  };
  const origRender=renderWorkspace;
  renderWorkspace=function(){
    if(WS.page==='resources'){shellRenderResources();return;}
    if(WS.page==='usage'){shellRenderUsage();return;}
    origRender();
  };
  const origTabs=modelTabs;
  modelTabs=function(){
    if(!SHELL.cloudOnly)return origTabs();
    return '<div class="workspace-tabs" aria-label="Cloud">'+[['cloud','Modèles'],['providers','Fournisseurs']].map(([id,label])=>'<button type="button" class="'+(THREAD.modelsTab===id?'selected':'')+'" data-model-tab="'+id+'" aria-pressed="'+(THREAD.modelsTab===id)+'">'+label+'</button>').join('')+'</div>';
  };
  const origOpen=openWorkspace;
  openWorkspace=async function(page,id){
    if(page!=='models')SHELL.cloudOnly=false;
    await origOpen(page,id);
    if(page==='models'&&WS_PAGES.models){
      wsEl('ws-title').textContent=SHELL.cloudOnly?'Cloud':'Modèles du sélecteur';
      wsEl('ws-description').textContent=SHELL.cloudOnly?'Fournisseurs d’API et modèles distants, proposés dans tes discussions.':'Choisis les modèles proposés dans le sélecteur des discussions.';
    }
    shellSyncNav(page);
  };
  const origToggle=toggleModelMenu;
  toggleModelMenu=async function(){
    await origToggle();
    const menu=wsEl('model-menu');if(menu.hidden)return;
    // Les lignes masquées par la visibilité du catalogue restent masquées.
    menu.querySelectorAll('.mpick-item').forEach(r=>{r.dataset.catalogHidden=r.hidden?'1':'0';});
    if(wsEl('pick-q'))wsEl('pick-q').value='';
    shellPickerTab(shellExecKind()==='harness'?'harness':shellExecKind());
    requestAnimationFrame(()=>{shellSegIndicator(menu.querySelector('.seg'));wsEl('pick-q')?.focus({preventScroll:true});});
    // threadRefreshPicker remplit cloud/harness de façon asynchrone.
    setTimeout(()=>{menu.querySelectorAll('.mpick-item').forEach(r=>{if(r.dataset.catalogHidden===undefined)r.dataset.catalogHidden=r.hidden?'1':'0';});shellPickerFilter();},250);
  };
}

WS_PAGES.resources=['Ressources','','Ressources','Définies une fois dans Loom, activées par projet ou par harness.'];
WS_PAGES.usage=['Usage','','Usage','Quotas des abonnements et coût estimé des API. Une donnée absente reste inconnue.'];

document.addEventListener('DOMContentLoaded',()=>{
  shellBuildNav();
  ['hub-view','srv-view','bench-view'].forEach(shellLocalHead);
  shellBuildEngineCard();
  shellPickerSetup();
  shellInspectorSetup();
  shellWrap();
  shellWatchChat();
  shellSyncExecTag();
  const label=wsEl('model-pick-label');if(label)new MutationObserver(shellSyncExecTag).observe(label,{childList:true,characterData:true,subtree:true});
  document.addEventListener('click',async e=>{
    const lt=e.target.closest('[data-local-tab]');if(lt){shellOpenLocal(lt.dataset.localTab);return;}
    const rt=e.target.closest('[data-res-tab]');if(rt){SHELL.resTab=rt.dataset.resTab;shellRenderResources();return;}
    const page=e.target.closest('.usage-page');if(!page)return;
    const refresh=e.target.closest('[data-quota-refresh]');if(!refresh)return;
    refresh.disabled=true;refresh.textContent='Lecture…';
    try{const r=await jpost('/api/usage/refresh',{runtime_id:refresh.dataset.quotaRefresh});if(!r.ok)toast(r.error);await shellRenderUsage();}
    catch(error){toast(error.message);refresh.textContent='Réessayer';refresh.disabled=false;}
  });
  document.addEventListener('submit',async e=>{
    const form=e.target.closest('.usage-page [data-usage-price]');if(!form)return;e.preventDefault();
    const f=new FormData(form),button=form.querySelector('button');button.disabled=true;
    try{const r=await jpost('/api/usage/price',{choice_id:form.dataset.usagePrice,input_per_million:Number(f.get('input')),output_per_million:Number(f.get('output')),currency:f.get('currency')});if(!r.ok)throw new Error(r.error);await shellRenderUsage();}
    catch(error){toast(error.message);button.disabled=false;}
  });
  window.addEventListener('resize',()=>shellSegIndicator(document.querySelector('#model-menu .seg')));
  // La route initiale a été peinte avant ce module : on aligne l'état actif.
  const view=document.documentElement.dataset.page||'';shellSyncNav(view);
});
