// Estimation VRAM live (panneau modèle + éditeur de preset), d'après
// l'estimateur Unsloth Studio côté serveur (/api/estimate).

function fmtGiB(mb){
  const n = Number(mb)||0;
  if(n < 1) return (n/1024).toFixed(2).replace('.',',')+' Gio';
  const g = n/1024;
  return (g>=10 ? g.toFixed(1) : g.toFixed(2)).replace('.',',')+' Gio';
}
function fmtTok(n){
  n = Number(n)||0;
  if(typeof fmtCtxTokens==='function') return fmtCtxTokens(n);
  if(n>=1000000) return (n/1000000).toFixed(1).replace(/\.0$/,'')+'M';
  if(n>=1000) return Math.round(n/1000)+'K';
  return String(n);
}

function paintVramEst(id, est){
  const el = document.getElementById(id);
  if(!el) return;
  if(!est || !est.ok){ el.hidden = true; el.innerHTML=''; return; }
  const gpu = est.gpu_mb||0, total = est.total_mb||gpu, vram = est.vram_total_mb||0;
  const off = est.ram_offload_mb||0;
  const pct = vram ? Math.min(100, Math.round(gpu*100/vram)) : (off?100:0);
  let cls = '';
  if(off>64 || est.verdict==='no') cls = 'err';
  else if(off>0 || est.verdict==='tight' || est.verdict==='offload') cls = 'warn';
  let note = '';
  if(off>64){
    const fit = est.max_ctx_fit>0 ? ' Jusqu\'à ~'+fmtTok(est.max_ctx_fit)+' jetons tiendraient en VRAM.' : '';
    note = 'Dépasse la VRAM : ~'+fmtGiB(off)+' iraient en RAM.'+fit;
  } else if(vram){
    note = (est.ctx?fmtTok(est.ctx)+' jetons · ':'')+'KV '+fmtGiB(est.kv_mb||0)+(est.mtp_mb? ' · MTP '+fmtGiB(est.mtp_mb):'');
  }
  el.hidden = false;
  el.innerHTML =
    '<div class="vram-est-h">Estimation mémoire</div>'+
    '<div class="vram-est-nums"><span>GPU <b>'+fmtGiB(Math.min(gpu, vram||gpu))+'</b></span>'+
    '<span>Total <b>'+fmtGiB(total)+'</b></span>'+
    (vram? '<span class="muted">'+fmtGiB(vram)+' VRAM</span>':'')+'</div>'+
    '<div class="vram-est-bar '+cls+'"><i style="width:'+pct+'%"></i></div>'+
    (note? '<div class="vram-est-note '+cls+'">'+note+'</div>':'');
}

function estimateAlertMsg(name, est){
  const off = est.ram_offload_mb||0;
  const fit = est.max_ctx_fit>0 ? fmtTok(est.max_ctx_fit) : '—';
  return '« '+(name||'ce modèle')+' » à '+fmtTok(est.ctx||0)+' jetons :\n'+
    '~'+fmtGiB(est.gpu_mb)+' GPU / ~'+fmtGiB(est.total_mb)+' au total.\n'+
    (est.vram_total_mb? fmtGiB(est.vram_total_mb)+' VRAM disponible.\n':'')+
    (off>0 ? '~'+fmtGiB(off)+' iraient en RAM système.\n':'')+
    'Jusqu\'à ~'+fit+' jetons tiendraient entièrement sur le GPU.\n\n'+
    'Ajuste le contexte avant de charger, ou force le chargement.';
}

let _estPanelT=0, _estPresetT=0, _estPanelSeq=0;
function clearParamsEstimate(){
  _estPanelSeq++;
  clearTimeout(_estPanelT);
  paintVramEst('params-vram', null);
}
function scheduleParamsEstimate(){
  _estPanelSeq++;
  clearTimeout(_estPanelT);
  _estPanelT = setTimeout(refreshParamsEstimate, 160);
}
function schedulePresetEstimate(){
  clearTimeout(_estPresetT);
  _estPresetT = setTimeout(refreshPresetEstimate, 160);
}

async function refreshParamsEstimate(){
  const seq = ++_estPanelSeq;
  if(typeof SESSION==='undefined' || (SESSION.mode==='empty' && !SESSION.preview)){
    paintVramEst('params-vram', null); return;
  }
  const model = (SESSION.model||'').trim();
  if(!model){ paintVramEst('params-vram', null); return; }
  const content = (typeof pTA==='function' && pTA()) ? pTA().value : '';
  try{
    const r = await jpost('/api/estimate', {model, content});
    if(seq !== _estPanelSeq) return;
    paintVramEst('params-vram', r);
  }catch(_){
    if(seq !== _estPanelSeq) return;
    paintVramEst('params-vram', null);
  }
}

async function refreshPresetEstimate(){
  const ta = document.getElementById('m-content');
  const model = (typeof cfgReadKey==='function' ? cfgReadKey('MODEL') : '') ||
    (document.getElementById('m-model')||{}).value || '';
  if(!model || !ta){ paintVramEst('preset-vram', null); return; }
  try{
    const r = await jpost('/api/estimate', {model, content: ta.value||''});
    paintVramEst('preset-vram', r);
  }catch(_){ paintVramEst('preset-vram', null); }
}

async function resetNakedDefaults(){
  if(typeof SESSION==='undefined' || (SESSION.mode!=='model' && !SESSION.preview)) return;
  const model = SESSION.model||'';
  if(!model) return;
  if(!await askConfirm('Remettre les paramètres natifs de « '+(typeof baseName==='function'?baseName(model):model)+' » dans le panneau ? Le moteur ne change pas tant que tu n\'appliques pas.', {title:'Défauts du modèle', okText:'Remettre'})) return;
  const r = await jget('/api/naked/defaults?model='+encodeURIComponent(model));
  if(!r || !r.ok){ toast('erreur : '+(r&&r.error||'')); return; }
  const ta = typeof pTA==='function' ? pTA() : null;
  if(ta) ta.value = r.content||'';
  SESSION.dirty = true;
  if(typeof populatePanel==='function') populatePanel();
  if(typeof NAKED_REMEMBERED!=='undefined') NAKED_REMEMBERED = false;
  if(typeof renderSessionChrome==='function') renderSessionChrome();
  scheduleParamsEstimate();
  toast('défauts du modèle');
}

// Un GGUF trop lourd pour la VRAM s’édite dans la bibliothèque locale,
// sans charger le moteur. « Charger » mémorise puis démarre.

async function openLoadFit(model, name, est){
  if(typeof openLibrary === 'function'){
    await openLibrary({model, est});
    return;
  }
  if(typeof libFillNaked === 'function'){
    await libFillNaked(model, est);
    if(typeof setParamsOpen==='function') setParamsOpen(true);
    if(typeof scheduleParamsEstimate==='function') scheduleParamsEstimate();
  }
}

async function savePreviewAndLoad(){
  if(!SESSION.preview || !SESSION.model) return;
  const ok = await rememberNaked({silent:true});
  if(!ok) return;
  const model = SESSION.model;
  if(typeof startNakedLoad==='function') await startNakedLoad(model);
}

