// Sélecteur modèle/preset (haut du chat) + panneau de paramètres à gauche.
// Deux modes : modèle nu (créer un preset) ou preset chargé (mettre à jour).

let SESSION = {mode:'empty', presetId:'', presetName:'', model:'', dirty:false, preview:false};
let pendingModel = '';
let LIVE_MODEL = '';
let NAKED_REMEMBERED = false;
let NAKED_NATIVE = 0;

function fmtNakedNative(n){
  if(typeof fmtCtxTokens==='function') return fmtCtxTokens(n);
  return String(n);
}

function pTA(){ return document.getElementById('p-content'); }
function pRead(key){ const ta=pTA(); return ta ? readEnvKey(ta.value, key) : ''; }
function pWrite(key, val){
  const ta=pTA(); if(!ta) return;
  val = String(val==null?'':val);
  const encoded = typeof envEncodeVal==='function' ? envEncodeVal(key, val) : String(val).trim();
  const reLine = new RegExp('^[ \\t]*'+key+'[ \\t]*=.*$','m');
  if(encoded === ''){
    ta.value = ta.value.replace(new RegExp('^[ \\t]*'+key+'[ \\t]*=.*\\n?','m'),'').replace(/\n{3,}/g,'\n\n');
  } else {
    const line = key+'='+encoded;
    if(reLine.test(ta.value)) ta.value = ta.value.replace(reLine, line);
    else ta.value = ta.value.replace(/\s*$/,'') + '\n'+line+'\n';
  }
  SESSION.dirty = true;
  if(typeof scheduleParamsEstimate==='function') scheduleParamsEstimate();
}
function pEaTokens(){ return pRead('EXTRA_ARGS').split(/\s+/).filter(Boolean); }
function pEaSet(t){ pWrite('EXTRA_ARGS', t.join(' ')); }
function pEaFlag(flag, on){
  const t = pEaTokens().filter(x=>x!==flag);
  if(on) t.push(flag);
  pEaSet(t);
}
function pEaVal(flag, val){
  const t = pEaTokens(), i = t.indexOf(flag);
  if(i>=0){ const had = i+1<t.length && !t[i+1].startsWith('-'); t.splice(i, had?2:1); }
  val = String(val||'').trim();
  if(val !== '') t.push(flag, val);
  pEaSet(t);
}
function pKvSet(val){
  const [k, v] = String(val||'').split('|');
  pWrite('KV_TYPE', '');
  pWrite('KV_TYPE_K', k || '');
  pWrite('KV_TYPE_V', v || '');
}
function pKvRead(){
  const base = pRead('KV_TYPE');
  const k = pRead('KV_TYPE_K') || base;
  const v = pRead('KV_TYPE_V') || base;
  return (k || v) ? (k + '|' + v) : '';
}
function pOnEffort(val){
  pWrite('REASONING_EFFORT', val);
  if(val) pEaFlag('--jinja', true);
}

function populatePanel(){
  document.querySelectorAll('body > .tip.placed').forEach(t=>t.remove());
  if(SESSION.mode==='empty' && !SESSION.preview){
    if(typeof clearParamsEstimate==='function') clearParamsEstimate();
    else if(typeof paintVramEst==='function') paintVramEst('params-vram', null);
    const host=document.getElementById('params-form');
    if(host) host.innerHTML='';
    SESSION.dirty = false;
    return Promise.resolve();
  }
  if(typeof renderPanelParams==='function') return renderPanelParams().then(()=>{ SESSION.dirty = false; });
  if(typeof scheduleParamsEstimate==='function') scheduleParamsEstimate();
  SESSION.dirty = false;
  return Promise.resolve();
}

function setParamsOpen(open){
  if(open && typeof isDrawerSide==='function' && isDrawerSide() && typeof closeSide==='function') closeSide();
  if(open && typeof toggleActivityPopover==='function') toggleActivityPopover(false);
  document.documentElement.setAttribute('data-params-open', open?'1':'0');
  const box=document.getElementById('params');
  if(box) box.setAttribute('aria-hidden', open?'false':'true');
  const btn=document.getElementById('params-open');
  if(btn){
    btn.setAttribute('aria-expanded', open?'true':'false');
    btn.title = open ? 'Fermer les paramètres' : 'Paramètres du modèle';
    btn.setAttribute('aria-label', btn.title);
  }
  syncParamsOverlay();
  if(open){
    const form=document.getElementById('params-form');
    if((SESSION.mode!=='empty' || SESSION.preview) && form && !form.childElementCount) populatePanel();
    if(typeof scheduleParamsEstimate==='function') scheduleParamsEstimate();
  }
}
function syncParamsOverlay(){
  const bd=document.getElementById('backdrop');
  if(!bd) return;
  const open=document.documentElement.getAttribute('data-params-open')==='1';
  const overlay=typeof isOverlayParams==='function' ? isOverlayParams() : window.matchMedia('(max-width:1100px)').matches;
  if(overlay && open){
    bd.classList.add('open','params-veil');
    return;
  }
  if(bd.classList.contains('params-veil')){
    bd.classList.remove('params-veil');
    const side=document.getElementById('side');
    if(!side || !side.classList.contains('open')) bd.classList.remove('open');
  }
}
function toggleParams(){
  setParamsOpen(document.documentElement.getAttribute('data-params-open')!=='1');
}

function closeModelMenu(){
  const m=document.getElementById('model-menu');
  const b=document.getElementById('model-pick-btn');
  if(m) m.hidden = true;
  if(b) b.setAttribute('aria-expanded','false');
}
async function toggleModelMenu(){
  const m=document.getElementById('model-menu');
  const b=document.getElementById('model-pick-btn');
  if(!m) return;
  const show = m.hidden;
  m.hidden = !show;
  if(b) b.setAttribute('aria-expanded', show?'true':'false');
  if(show){
    if(SESSION.mode==='empty') await refreshSession(true);
    refreshPicker();
  }
}

function syncPickerLabel(s){
  const running = !!(s && s.model && s.health && !s.load_error);
  if(running){
    LIVE_MODEL = s.model;
    if(!SESSION.model){
      SESSION.model = s.model;
      if(SESSION.mode==='empty') SESSION.mode = s.preset_id ? 'preset' : 'model';
      if(s.preset_id) SESSION.presetId = s.preset_id;
      if(s.preset_name) SESSION.presetName = s.preset_name;
    }
    if(typeof renderSessionChrome==='function') renderSessionChrome();
    const form=document.getElementById('params-form');
    if(SESSION.mode!=='empty' && form && !form.childElementCount && typeof populatePanel==='function') populatePanel();
  } else if(s && (s.active!==undefined || s.health!==undefined || s.model!==undefined || s.load_error)){
    LIVE_MODEL = '';
    const pending = pendingModel || (typeof pendingPreset==='number' && pendingPreset>0);
    if(!pending && (!s.model || !s.health || s.load_error)){
      if(SESSION.mode!=='empty' && !SESSION.preview){
        SESSION.mode = 'empty';
        SESSION.model = '';
        SESSION.presetId = '';
        SESSION.presetName = '';
        if(typeof renderSessionChrome==='function') renderSessionChrome();
      }
    }
  }
  const el=document.getElementById('model-pick-label');
  if(!el) return;
  if(typeof threadSyncPickerLabel==='function'&&threadSyncPickerLabel())return;
  const pending = pendingModel || (typeof pendingPreset==='number' && pendingPreset>0);
  if(pending){ el.textContent = 'Chargement…'; return; }
  if(running && s.preset_name){ el.textContent = s.preset_name; return; }
  if(running && s.model_name){ el.textContent = s.model_name; return; }
  if(SESSION.mode!=='empty' && SESSION.presetName){ el.textContent = SESSION.presetName; return; }
  if(SESSION.mode!=='empty' && SESSION.model){ el.textContent = baseName(SESSION.model); return; }
  el.textContent = 'Choisir un modèle';
}

function pickerRow(opts){
  const row=document.createElement('div');
  row.className='mpick-item'+(opts.active?' active':'')+(opts.inuse?' inuse':'')+(opts.pending?' pending':'');
  const main=document.createElement('button');
  main.type='button';
  main.className='mpick-main';
  const name=document.createElement('span');
  name.className='mpick-name';
  if(opts.inuse){
    const pill=document.createElement('span');
    pill.className='mpick-pill';
    pill.title='modèle en cours';
    pill.setAttribute('aria-hidden','true');
    name.appendChild(pill);
  }
  name.appendChild(document.createTextNode(opts.label));
  name.title = opts.label;
  main.appendChild(name);
  if(opts.sub){
    const s=document.createElement('span');
    s.className='mpick-sub';
    s.textContent=opts.sub;
    s.title=opts.sub;
    main.appendChild(s);
  }
  main.onclick=()=>{ closeModelMenu(); opts.onClick(); };
  row.appendChild(main);
  if(opts.onUnload){
    const ej=document.createElement('button');
    ej.type='button';
    ej.className='mpick-eject';
    ej.title='Décharger';
    ej.setAttribute('aria-label','Décharger');
    ej.innerHTML='<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" aria-hidden="true"><circle cx="12" cy="12" r="8.5"/><path d="M8 12h8"/></svg>';
    ej.onclick=(e)=>{ e.stopPropagation(); closeModelMenu(); opts.onUnload(); };
    row.appendChild(ej);
  }
  return row;
}

function liveModelMatch(m){
  if(!MODEL_READY) return false;
  const live = SESSION.mode==='empty' ? '' : (SESSION.model || LIVE_MODEL);
  if(!live || !m) return false;
  return samePath(live, m.value) || samePath(live, m.path) || samePath(baseName(live), m.name);
}

async function refreshPicker(){
  const modelsBox=document.getElementById('model-menu-models');
  const presetsBox=document.getElementById('model-menu-presets');
  if(!modelsBox || !presetsBox) return;
  let models=[], presets=[];
  try{ models = await jget('/api/models') || []; }catch(_){}
  try{ presets = await jget('/api/presets') || []; }catch(_){}
  modelsBox.innerHTML='';
  const usable = models.filter(m => typeof isWeightModel==='function' ? isWeightModel(m) : !isMmprojName(m.name));
  if(!usable.length){
    const empty=document.createElement('div'); empty.className='mpick-empty';
    empty.textContent='Aucun .gguf dans les dossiers de modèles';
    modelsBox.appendChild(empty);
  } else {
    usable.forEach(m=>{
      const inuse = liveModelMatch(m);
      const pend = pendingModel && (samePath(pendingModel, m.value) || samePath(pendingModel, m.path));
      let tag = fmtSize(m.size);
      if(m.shards>1) tag += ' · '+m.shards+' fichiers';
      if(m.missing && m.missing.length) tag += ' · manquant';
      if(inuse && SESSION.mode==='preset') tag = (tag ? tag+' · ' : '')+'via preset';
      modelsBox.appendChild(pickerRow({
        label:m.name, sub:tag, active:SESSION.mode==='model' && inuse, inuse, pending:pend,
        onClick:()=>typeof threadChooseLocal==='function'?threadChooseLocal(m.value,m.name):loadNaked(m.value,m.name),
        onUnload:inuse ? ()=>unloadCurrent(m.name) : null
      }));
    });
  }
  presetsBox.innerHTML='';
  if(!presets.length){
    const empty=document.createElement('div'); empty.className='mpick-empty';
    empty.textContent='Aucun preset — utilise + pour en créer un';
    presetsBox.appendChild(empty);
  } else {
    presets.forEach((x,i)=>{
      const livePreset = !!(SESSION.mode==='preset' && (x.active || (SESSION.presetId && SESSION.presetId===x.id)));
      const pend = !livePreset && pendingPreset===i+1;
      let tag = x.quant || x.ctx || '';
      if(x.model) tag = (tag ? tag+' · ' : '')+baseName(x.model);
      presetsBox.appendChild(pickerRow({
        label:x.name, sub:tag, active:livePreset, pending:pend,
        onClick:()=>typeof threadChooseLocal==='function'?threadChooseLocal(x.model,x.name,i+1):loadPresetAt(i+1,x.name),
        onUnload:livePreset ? ()=>unloadCurrent(x.name) : null
      }));
    });
  }
  if(typeof threadRefreshPicker==='function')await threadRefreshPicker();
}

async function loadNaked(model, name){
  if(!model) return;
  const label = name||baseName(model);
  let est=null;
  try{ est = await jpost('/api/estimate', {model}); }catch(_){}
  let msg = 'Charger « '+label+' » et redémarrer le moteur ?';
  let opts = {title:'Charger le modèle', okText:'Charger'};
  if(est && est.ok && (est.ram_offload_mb|0) > 64){
    msg = typeof estimateAlertMsg==='function' ? estimateAlertMsg(label, est) : msg;
    opts = {title:'Modèle trop lourd pour la VRAM', okText:'Charger quand même', extraText:'Ajuster les paramètres', danger:true};
  }
  const go = await askConfirm(msg, opts);
  if(go === 'extra'){
    if(typeof openLoadFit==='function') await openLoadFit(model, label, est);
    return;
  }
  if(!go) return;
  await startNakedLoad(model);
}

async function startNakedLoad(model){
  if(!model) return;
  const wasPreview = !!SESSION.preview;
  SESSION.preview = false;
  pendingModel = model; pendingPreset = 0;
  syncPickerLabel({}); toast('chargement…');
  const r=await jpost('/api/load-model',{model});
  if(!r.ok){
    SESSION.preview = wasPreview;
    pendingModel='';
    toast('erreur : '+(r.error||''));
    if(wasPreview && typeof renderSessionChrome==='function') renderSessionChrome();
    refreshPicker();
    return;
  }
  SESSION.dirty = false;
  await refreshSession(true);
  for(let i=0; i<40 && pendingModel; i++){
    await new Promise(r=>setTimeout(r,1500));
    try{ await loadStatus(); }catch(_){}
    if(MODEL_READY) pendingModel='';
  }
  pendingModel='';
  loadAll();
}

async function loadPresetAt(n, name){
  if(typeof switchTo==='function') await switchTo(n, name);
  SESSION.dirty = false;
  refreshSession(true);
}

async function unloadCurrent(name){
  const label = name || SESSION.presetName || baseName(SESSION.model) || 'le modèle';
  if(!await askConfirm('Décharger « '+label+' » et libérer la mémoire VRAM ?', {title:'Décharger', okText:'Décharger'})) return;
  pendingModel=''; pendingPreset=0;
  LIVE_MODEL='';
  if(typeof clearParamsEstimate==='function') clearParamsEstimate();
  SESSION.mode='empty'; SESSION.model=''; SESSION.presetId=''; SESSION.presetName=''; SESSION.dirty=false; SESSION.preview=false;
  syncPickerLabel({}); toast('libération VRAM…');
  const r=await jpost('/api/unload');
  if(!r.ok){ toast('erreur : '+(r.error||'')); refreshPicker(); return; }
  toast('modèle déchargé (0 VRAM)');
  await loadAll();
  refreshSession(true);
}

function renderSessionChrome(){
  if(typeof threadPanelMode==='function'&&threadPanelMode()!=='local')return;
  const empty=document.getElementById('params-empty');
  const body=document.getElementById('params-body');
  const kind=document.getElementById('params-kind');
  const sub=document.getElementById('params-sub');
  const save=document.getElementById('params-preset-btn');
  const remember=document.getElementById('params-remember-btn');
  const reset=document.getElementById('params-reset-btn');
  const apply=document.getElementById('params-apply');
  const pill=document.getElementById('params-preview-pill');
  const headSave=document.getElementById('params-head-save');
  const preview=!!SESSION.preview;
  if(preview) document.documentElement.setAttribute('data-params-preview','1');
  else document.documentElement.removeAttribute('data-params-preview');
  if(pill) pill.hidden = true;
  if(headSave) headSave.hidden = true;
  const loaded = SESSION.mode!=='empty' || preview;
  if(empty) empty.hidden = loaded;
  if(body) body.hidden = !loaded;
  if(!loaded){
    if(kind) kind.textContent = 'Paramètres';
    if(sub) sub.textContent = '';
    if(remember) remember.hidden = true;
    if(reset) reset.hidden = true;
    if(apply) apply.textContent = 'Appliquer';
    if(save) save.hidden = false;
    if(typeof clearParamsEstimate==='function') clearParamsEstimate();
    else if(typeof paintVramEst==='function') paintVramEst('params-vram', null);
    return;
  }
  if(preview){
    if(apply) apply.textContent = 'Charger';
    if(save) save.hidden = false;
    if(SESSION.mode==='preset'){
      if(kind) kind.textContent = SESSION.presetName || 'Preset';
      if(sub) sub.textContent = (baseName(SESSION.model) ? baseName(SESSION.model)+' · ' : '')+'pas chargé';
      if(save) save.textContent = 'Mettre à jour le preset';
      if(remember) remember.hidden = true;
      if(reset) reset.hidden = true;
    } else {
      if(kind) kind.textContent = baseName(SESSION.model) || 'Modèle';
      if(sub) sub.textContent = NAKED_REMEMBERED ? 'pas chargé · souvenir enregistré' : 'pas chargé';
      if(save) save.textContent = 'Créer un preset';
      if(remember) remember.hidden = false;
      if(reset) reset.hidden = false;
    }
    return;
  }
  if(apply) apply.textContent = 'Appliquer';
  if(save) save.hidden = false;
  if(SESSION.mode==='preset'){
    if(kind) kind.textContent = SESSION.presetName || 'Preset';
    if(sub) sub.textContent = baseName(SESSION.model) || 'preset';
    if(save) save.textContent = 'Mettre à jour le preset';
    if(remember) remember.hidden = true;
    if(reset) reset.hidden = true;
  } else {
    if(kind) kind.textContent = baseName(SESSION.model) || 'Modèle';
    if(sub) sub.textContent = NAKED_REMEMBERED ? 'modèle nu · souvenir au prochain chargement' : (NAKED_NATIVE ? 'modèle nu · natif '+fmtNakedNative(NAKED_NATIVE) : 'modèle nu');
    if(save) save.textContent = 'Créer un preset';
    if(remember) remember.hidden = false;
    if(reset) reset.hidden = false;
  }
}

async function refreshSession(force){
  if(SESSION.preview){ refreshPicker(); renderSessionChrome(); return; }
  if(SESSION.dirty && !force){ refreshPicker(); return; }
  let cfg={}, presets=[];
  try{ cfg = await jget('/api/config') || {}; }catch(_){}
  try{ presets = await jget('/api/presets') || []; }catch(_){}
  const act = presets.find(x=>x.active);
  const model = (cfg.MODEL||'').trim();
  if(act){
    SESSION.mode = 'preset';
    SESSION.presetId = act.id;
    SESSION.presetName = act.name;
    SESSION.model = model;
    NAKED_REMEMBERED = false;
    let content = '';
    try{
      const d = await jfetch('/api/preset?id='+encodeURIComponent(act.id)).then(r=>r.json());
      content = d.content || '';
    }catch(_){}
    if(!content) content = typeof formatPanelEnv==='function' ? '' : '';
    const ta=pTA(); if(ta) ta.value = content || envFromCfg(cfg);
  } else if(model){
    SESSION.mode = 'model';
    SESSION.presetId = '';
    SESSION.presetName = '';
    SESSION.model = model;
    const ta=pTA(); if(ta) ta.value = envFromCfg(cfg);
    NAKED_REMEMBERED = false;
    try{
      const r = await jget('/api/naked/remember?model='+encodeURIComponent(model));
      NAKED_REMEMBERED = !!(r && r.remembered);
    }catch(_){}
  } else {
    SESSION.mode = 'empty';
    SESSION.presetId = '';
    SESSION.presetName = '';
    SESSION.model = '';
    NAKED_REMEMBERED = false;
    const ta=pTA(); if(ta) ta.value = '';
  }
  await populatePanel();
  renderSessionChrome();
  syncPickerLabel({preset_name:SESSION.presetName, model_name:baseName(SESSION.model)});
  refreshPicker();
  const nctx=parseInt(cfg.CTX||pRead('CTX')||'0',10);
  if(nctx>0 && typeof CTX_MAX!=='undefined'){
    CTX_MAX=nctx;
    if(typeof updateCtxMeter==='function') updateCtxMeter();
  }
}

function envFromCfg(cfg){
  const skip = {MEM_MODE:1,CRAWL4AI_URL:1,WEB_ENGINE:1,CUDA_VISIBLE_DEVICES:1,HOST:1,MEM_ENCRYPTED:1,BACKUP_AUTO:1,COMPACT:1,MACHINES:1};
  const keys = Object.keys(cfg||{}).filter(k=>cfg[k] && !skip[k]).sort();
  return keys.map(k=>{
    const enc = typeof envEncodeVal==='function' ? envEncodeVal(k, cfg[k]) : ((/\s/.test(cfg[k])?'"'+cfg[k]+'"':cfg[k]));
    return k+'='+enc;
  }).join('\n')+'\n';
}

function flushReasoning(){
  if(typeof pfFlush==='function') pfFlush('panel');
}

async function rememberNaked(opts){
  opts = opts||{};
  if(SESSION.mode!=='model' && !SESSION.preview) return false;
  flushReasoning();
  const content = (pTA()&&pTA().value)||'';
  if(!content.trim()){ toast('rien à mémoriser'); return false; }
  const silent = !!opts.silent;
  if(!silent){
    if(!await askConfirm('Mémoriser ces paramètres pour le prochain chargement de « '+(baseName(SESSION.model)||'ce modèle')+' » ? Le moteur actuel ne change pas.', {title:'Se souvenir', okText:'Mémoriser'})) return false;
  }
  const r=await jpost('/api/naked/remember',{content, model:SESSION.model||''});
  if(!r.ok){ toast('erreur : '+(r.error||'')); return false; }
  NAKED_REMEMBERED = true;
  renderSessionChrome();
  toast('mémorisé pour le prochain chargement');
  return true;
}

async function applyPanel(){
  if(SESSION.preview){
    flushReasoning();
    const content = (pTA()&&pTA().value)||'';
    if(SESSION.mode==='preset'){
      if(!await askConfirm('Charger « '+(SESSION.presetName||'ce preset')+' » et redémarrer le moteur ?', {title:'Charger', okText:'Charger'})) return;
      toast('chargement…');
      const r=await jpost('/api/apply',{content, preset_id: SESSION.presetId||''});
      if(!r.ok){ toast('erreur : '+(r.error||'')); return; }
      SESSION.preview = false;
      SESSION.dirty = false;
      setTimeout(loadAll, 1500);
      return;
    }
    if(typeof savePreviewAndLoad==='function') await savePreviewAndLoad();
    return;
  }
  if(SESSION.mode==='empty') return;
  flushReasoning();
  const content = (pTA()&&pTA().value)||'';
  if(!await askConfirm('Appliquer ces paramètres et redémarrer le moteur ?', {title:'Appliquer', okText:'Appliquer'})) return;
  toast('application…');
  const r=await jpost('/api/apply',{content, preset_id: SESSION.presetId||''});
  if(!r.ok){ toast('erreur : '+(r.error||'')); return; }
  SESSION.dirty=false;
  toast('appliqué');
  setTimeout(loadAll, 1500);
}

async function savePanelPreset(){
  if(SESSION.mode==='empty' && !SESSION.preview) return;
  flushReasoning();
  const content = (pTA()&&pTA().value)||'';
  const offline = !!SESSION.preview;
  if(SESSION.mode==='preset'){
    const msg = offline
      ? 'Enregistrer « '+(SESSION.presetName||'ce preset')+' » ? Le moteur ne change pas.'
      : 'Enregistrer « '+(SESSION.presetName||'ce preset')+' » et relancer le moteur ?';
    if(!await askConfirm(msg, {title:'Mettre à jour', okText:'Enregistrer'})) return;
    const r=await jpost('/api/preset/save',{id:SESSION.presetId, name:SESSION.presetName, content});
    if(!r.ok){ toast('erreur : '+(r.error||'')); return; }
    SESSION.dirty=false;
    if(offline){
      toast('preset enregistré');
      if(typeof libRender==='function') libRender();
      return;
    }
    const a=await jpost('/api/apply',{content, preset_id:r.id||SESSION.presetId});
    if(!a.ok){ toast('enregistré, application : '+(a.error||'')); }
    else toast('preset mis à jour');
    setTimeout(loadAll, 1500);
    return;
  }
  const name=await askPrompt('Nom du preset', {title:'Nouveau preset', okText:'Créer', placeholder:'ex. Qwen 7B rapide'});
  if(name===null) return;
  const trimmed=String(name).trim();
  if(!trimmed){ toast('nom requis'); return; }
  const r=await jpost('/api/preset/save',{id:'', name:trimmed, content});
  if(!r.ok){ toast('erreur : '+(r.error||'')); return; }
  SESSION.dirty=false;
  if(offline){
    SESSION.mode='preset';
    SESSION.presetId=r.id||'';
    SESSION.presetName=trimmed;
    SESSION.preview=true;
    toast('preset créé');
    if(typeof renderSessionChrome==='function') renderSessionChrome();
    if(typeof libRender==='function') libRender();
    return;
  }
  const a=await jpost('/api/apply',{content, preset_id:r.id||''});
  if(!a.ok){ toast('créé, application : '+(a.error||'')); }
  else toast('preset créé');
  setTimeout(loadAll, 1500);
}

document.addEventListener('DOMContentLoaded', ()=>{
  setParamsOpen(false);
  addEventListener('resize', syncParamsOverlay);
  document.addEventListener('click', e=>{
    const pick=document.getElementById('model-pick');
    if(pick && !pick.contains(e.target)) closeModelMenu();
  });
  document.addEventListener('keydown', e=>{
    if(e.key==='Escape'){
      closeModelMenu();
      if(document.documentElement.getAttribute('data-params-open')==='1') setParamsOpen(false);
    }
  });
});
