// Bibliothèque locale : GGUF déjà sur le disque + presets, sans charger le moteur.
// Le panneau de paramètres s’ouvre ici pour éditer hors chargement.

let LIB = {models:[], presets:[], sel:''};

function libQ(){ return ((document.getElementById('hub-q')||{}).value||'').trim().toLowerCase(); }
function libPathOf(m){ return (m && (m.path || m.value || m.name)) || ''; }
function libKeyModel(m){ return 'm:'+libPathOf(m); }
function libKeyPreset(p){ return 'p:'+(p && p.id); }
function libShortDir(d){
  if(typeof hubDirLabel==='function') return hubDirLabel(d);
  const s = String(d||'').replace(/\\/g,'/');
  const i = s.lastIndexOf('/');
  return i>=0 ? s.slice(i+1) : s;
}

async function libLoad(){
  let models=[], presets=[];
  try{ models = await jget('/api/models') || []; }catch(_){}
  try{ presets = await jget('/api/presets') || []; }catch(_){}
  LIB.models = (models||[]).filter(m=> typeof isWeightModel==='function' ? isWeightModel(m) : !/mmproj/i.test(m.name||''));
  LIB.presets = presets||[];
  const dirs = [];
  LIB.models.forEach(m=>{
    const d = m.dir || '';
    if(d && dirs.indexOf(d)<0) dirs.push(d);
  });
  dirs.sort();
  HUB.dirs = dirs;
}

function libMatch(name, extra){
  const q = libQ();
  if(!q) return true;
  const hay = (name+' '+(extra||'')).toLowerCase();
  return hay.indexOf(q)>=0;
}

function libTrashBtn(title, onClick){
  const x = document.createElement('button');
  x.type = 'button';
  x.className = 'hub-item-x';
  x.title = title;
  x.setAttribute('aria-label', title);
  x.innerHTML = '<svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><path d="M4 7h16M9 7V5h6v2M8 7l1 12h6l1-12"/></svg>';
  x.onclick = (e)=>{ e.stopPropagation(); onClick(); };
  return x;
}

function libRow(opts){
  const row = document.createElement('div');
  row.className = 'hub-item'+(LIB.sel===opts.key?' on':'');
  row.setAttribute('data-k', opts.key);
  const main = document.createElement('button');
  main.type = 'button';
  main.className = 'hub-item-t';
  const n = document.createElement('span');
  n.className = 'hub-item-n';
  n.textContent = opts.name;
  const m = document.createElement('span');
  m.className = 'hub-item-m';
  m.textContent = opts.meta || '';
  if(opts.metaTitle) m.title = opts.metaTitle;
  main.appendChild(n);
  main.appendChild(m);
  main.onclick = opts.onOpen;
  row.appendChild(main);
  row.appendChild(libTrashBtn(opts.trashTitle, opts.onTrash));
  return row;
}

function libSec(title, extra){
  const sec = document.createElement('section');
  sec.className = 'hub-sec';
  const h = document.createElement('div');
  h.className = 'hub-sec-h';
  h.appendChild(document.createTextNode(title));
  if(extra) h.appendChild(extra);
  sec.appendChild(h);
  return sec;
}

async function libRender(){
  const box = document.getElementById('hub-lib');
  if(!box) return;
  await libLoad();
  if(typeof hubPaintFilters==='function') hubPaintFilters();
  const kind = (HUB && HUB.kind) || '';
  const dirf = (HUB && HUB.dir) || '';
  let presets = LIB.presets.filter(p=> libMatch(p.name||'', p.model||''));
  let models = LIB.models.filter(m=>{
    if(dirf && (m.dir||'') !== dirf) return false;
    return libMatch(m.name||'', (m.path||'')+' '+(m.dir||''));
  });
  if(kind==='preset') models = [];
  if(kind==='gguf') presets = [];
  box.innerHTML = '';

  if(typeof libRenderDownloads === 'function'){
    const dlSec = libRenderDownloads();
    if(dlSec) box.appendChild(dlSec);
  }

  if(kind!=='gguf'){
    const add = document.createElement('button');
    add.type = 'button';
    add.className = 'preset-add';
    add.title = 'Nouveau preset';
    add.textContent = '+';
    add.onclick = (e)=>{ e.preventDefault(); libNewPreset(); };
    const sec = libSec('Presets', add);
    const cards = document.createElement('div');
    cards.className = 'hub-cards';
    if(!presets.length){
      const empty = document.createElement('div');
      empty.className = 'hub-empty';
      empty.style.padding = '18px 16px';
      empty.textContent = 'Aucun preset.';
      cards.appendChild(empty);
    } else {
      presets.forEach(p=>{
        const bits = [];
        if(p.model) bits.push((p.model+'').split(/[\\/]/).pop());
        if(p.quant) bits.push(p.quant);
        if(p.active) bits.push('chargé');
        cards.appendChild(libRow({
          key: libKeyPreset(p),
          name: p.name||p.id,
          meta: bits.join(' · ') || 'preset',
          trashTitle: 'Supprimer',
          onOpen: ()=>libOpenPreset(p.id),
          onTrash: ()=>libDeletePreset(p.id, p.name||p.id)
        }));
      });
    }
    sec.appendChild(cards);
    box.appendChild(sec);
  }
  if(kind!=='preset'){
    const sec = libSec('Fichiers');
    const cards = document.createElement('div');
    cards.className = 'hub-cards';
    if(!models.length){
      const empty = document.createElement('div');
      empty.className = 'hub-empty';
      empty.style.padding = '18px 16px';
      empty.textContent = 'Aucun GGUF. Ouvre le Hub pour en télécharger.';
      cards.appendChild(empty);
    } else {
      models.forEach(m=>{
        const path = libPathOf(m);
        const bits = [];
        if(m.dir) bits.push(libShortDir(m.dir));
        if(m.size && typeof fmtSize==='function') bits.push(fmtSize(m.size));
        cards.appendChild(libRow({
          key: libKeyModel(m),
          name: m.name,
          meta: m.path || path,
          metaTitle: m.path || path,
          trashTitle: 'Désinstaller',
          onOpen: ()=>libOpenModel(path),
          onTrash: ()=>libDeleteModel(path, m.name)
        }));
      });
    }
    sec.appendChild(cards);
    box.appendChild(sec);
  }
}

function libMark(key){
  LIB.sel = key||'';
  document.querySelectorAll('#hub-lib .hub-item').forEach(el=> el.classList.toggle('on', el.getAttribute('data-k')===LIB.sel));
}

function libLiveSameModel(model){
  if(typeof SESSION==='undefined' || SESSION.preview) return false;
  if(SESSION.mode!=='model' || !SESSION.model) return false;
  return typeof samePath==='function' ? samePath(SESSION.model, model) : SESSION.model===model;
}
function libLiveSamePreset(id){
  if(typeof SESSION==='undefined' || SESSION.preview) return false;
  return SESSION.mode==='preset' && SESSION.presetId===id;
}

async function libOpenModel(model, est){
  model = String(model||'').trim();
  if(!model) return;
  libMark(libKeyModel({path:model, value:model}));
  if(libLiveSameModel(model)){
    if(typeof setParamsOpen==='function') setParamsOpen(true);
    if(typeof populatePanel==='function') populatePanel();
    if(typeof renderSessionChrome==='function') renderSessionChrome();
    return;
  }
  await libFillNaked(model, est);
  if(typeof setParamsOpen==='function') setParamsOpen(true);
}

async function libFillNaked(model, est){
  SESSION.preview = true;
  SESSION.mode = 'model';
  SESSION.presetId = '';
  SESSION.presetName = '';
  SESSION.model = model;
  SESSION.dirty = true;
  NAKED_REMEMBERED = false;
  const d = await jget('/api/naked/defaults?model='+encodeURIComponent(model));
  if(!d || !d.ok){
    SESSION.preview = false;
    toast('erreur : '+(d&&d.error||'défauts indisponibles'));
    return;
  }
  NAKED_NATIVE = (d.native_ctx|0) || 0;
  let rem = null;
  try{ rem = await jget('/api/naked/remember?model='+encodeURIComponent(model)); }catch(_){}
  NAKED_REMEMBERED = !!(rem && rem.remembered);
  const ta = typeof pTA==='function' ? pTA() : null;
  if(ta) ta.value = (NAKED_REMEMBERED && rem && rem.content) ? rem.content : (d.content||'');
  const fit = est && (est.max_ctx_fit|0);
  if(fit > 0 && (!NAKED_NATIVE || fit < NAKED_NATIVE) && typeof pWrite==='function' && !NAKED_REMEMBERED){
    pWrite('CTX', String(Math.max(1024, Math.floor(fit/1024)*1024)));
  }
  if(typeof populatePanel==='function') await populatePanel();
  SESSION.dirty = true;
  if(typeof renderSessionChrome==='function') renderSessionChrome();
  if(typeof scheduleParamsEstimate==='function') scheduleParamsEstimate();
}

async function libOpenPreset(id){
  id = String(id||'').trim();
  if(!id) return;
  libMark(libKeyPreset({id}));
  if(libLiveSamePreset(id)){
    if(typeof setParamsOpen==='function') setParamsOpen(true);
    if(typeof populatePanel==='function') populatePanel();
    if(typeof renderSessionChrome==='function') renderSessionChrome();
    return;
  }
  let d;
  try{ d = await jfetch('/api/preset?id='+encodeURIComponent(id)).then(r=>r.json()); }
  catch(_){ d = null; }
  if(!d || (!d.content && !d.id)){ toast('preset introuvable'); return; }
  SESSION.preview = true;
  SESSION.mode = 'preset';
  SESSION.presetId = d.id || id;
  SESSION.presetName = d.name || id;
  SESSION.model = (typeof readEnvKey==='function' ? readEnvKey(d.content||'', 'MODEL') : '') || '';
  SESSION.dirty = false;
  const ta = typeof pTA==='function' ? pTA() : null;
  if(ta) ta.value = d.content||'';
  if(typeof populatePanel==='function') await populatePanel();
  if(typeof renderSessionChrome==='function') renderSessionChrome();
  if(typeof setParamsOpen==='function') setParamsOpen(true);
  if(typeof scheduleParamsEstimate==='function') scheduleParamsEstimate();
}

async function libNewPreset(){
  if(typeof openPreset==='function') openPreset('');
}

async function libDeleteModel(path, name){
  const label = name || (typeof baseName==='function' ? baseName(path) : path);
  if(!await askConfirm('Désinstaller « '+label+' » du disque ? Le fichier .gguf est supprimé.', {title:'Désinstaller', okText:'Désinstaller', danger:true})) return;
  const r = await jpost('/api/models/delete', {name: path});
  if(!r || !r.ok){ toast('erreur : '+((r&&r.error)||'')); return; }
  toast('désinstallé');
  if(SESSION.preview && SESSION.model && (SESSION.model===path || (typeof samePath==='function' && samePath(SESSION.model, path)))){
    SESSION.preview = false; SESSION.mode = 'empty'; SESSION.model = '';
    if(typeof populatePanel==='function') populatePanel();
    if(typeof renderSessionChrome==='function') renderSessionChrome();
  }
  libRender();
  if(typeof populateModelPicker==='function') populateModelPicker();
}

async function libDeletePreset(id, name){
  if(!await askConfirm('Supprimer le preset « '+(name||id)+' » ? Le fichier .gguf n’est pas touché.', {title:'Supprimer le preset', okText:'Supprimer', danger:true})) return;
  const r = await jpost('/api/preset/delete', {id});
  if(!r || !r.ok){ toast('erreur : '+((r&&r.error)||'')); return; }
  toast('preset supprimé');
  if(SESSION.preview && SESSION.presetId===id){
    SESSION.preview = false; SESSION.mode = 'empty'; SESSION.presetId = ''; SESSION.presetName = '';
    if(typeof populatePanel==='function') populatePanel();
    if(typeof renderSessionChrome==='function') renderSessionChrome();
  }
  libRender();
}
