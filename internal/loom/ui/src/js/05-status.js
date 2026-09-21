let LAST_BOOT=null; // empreinte de démarrage du serveur vue au dernier poll
async function loadStatus(){
  const s=await jget('/api/status');
  const nctx = Number(s && s.ctx);
  if(nctx>0) CTX_MAX=nctx;
  if(typeof NAKED_NATIVE!=='undefined'){
    NAKED_NATIVE = (typeof s.ctx_native==='number' && s.ctx_native>0) ? s.ctx_native : 0;
  }
  if(typeof updateCtxMeter==='function') updateCtxMeter();
  // Redémarrage du serveur détecté (l'empreinte 'boot' a changé) : le coffre
  // mémoire s'est reverrouillé en RAM. On relance le déverrouillage automatique
  // avec la clé stockée, sinon la mémoire reste muette jusqu'à un refresh manuel.
  if(typeof s.boot!=='undefined'){
    if(LAST_BOOT!==null && s.boot!==LAST_BOOT){
      if(typeof memUnlockDone!=='undefined') memUnlockDone=false;
      if(typeof loadMemEnc==='function') loadMemEnc();
    }
    LAST_BOOT=s.boot;
  }
  const el=document.getElementById('status-svc');
  // Trois états : service coupé (err) · service actif mais modèle pas encore
  // chargé (loading, llama-server renvoie 503) · modèle prêt (ok).
  // L'état vit dans la barre llama.cpp en bas : la pastille d'en-tête n'est
  // plus affichée (remplacée par l'icône de recherche).
  let cls='err', txt='arrêté';
  if(s.active && s.health){ cls='ok'; txt='prêt'; }
  else if(s.load_error){ cls='err'; txt='erreur'; }
  else if(s.active && s.model){ cls='loading'; txt='chargement…'; }
  else if(s.active){ cls='ok'; txt='connecté'; }
  if(el){
    el.className='statuspill '+cls;
    el.innerHTML='<span class="dot"></span>'+txt;
  }
  syncEngineBar(s);
  MODEL_READY = !!(s.active && s.health);
  if(typeof syncPickerLabel==='function') syncPickerLabel(s);
  if(typeof pendingModel!=='undefined' && pendingModel && s.health) pendingModel='';
  // Le bouton d'envoi suit l'état du moteur : inutile de pouvoir envoyer un
  // message à un modèle qui n'est pas encore chargé (voir syncSendBtn).
  STATUS_SEEN = true;
  if(typeof syncSendBtn==='function') syncSendBtn();
  if(s.version){
    const v=document.getElementById('ver'); if(v) v.textContent='v'+s.version;
  }
  // Avertissement de lancement (App Translocation macOS) : rare, mais il explique
  // des symptomes tres deroutants, donc on l'affiche en permanence tant qu'il dure.
  const wb=document.getElementById('app-warn');
  if(wb){
    if(s.warn){ wb.textContent='⚠ '+s.warn; wb.style.display=''; }
    else { wb.style.display='none'; }
  }
  // Modèle qui ne charge pas (souvent un moteur incompatible) : message explicite
  // plutôt qu'un « chargement… » perpétuel ou un crash-loop muet.
  const me=document.getElementById('model-err');
  if(me){
    if(s.load_error){ me.textContent='⚠ '+s.load_error; me.style.display=''; }
    else { me.style.display='none'; }
  }
}
// checkServerFreshness : en accès distant ou local,
// le front interroge /api/update (le serveur compare SA version à la dernière
// release GitHub) et, si une mise à jour existe, affiche un bandeau passif
// invitant à mettre à jour. Silencieux en local si déjà à jour.
async function checkServerFreshness(){
  const box=document.getElementById('server-stale');
  if(!box) return;
  let r;
  try{ r=await jget('/api/update'); }catch(e){ return; }
  if(!r || r.error || !r.available || !r.latest){ box.style.display='none'; return; }
  if(localStorage.getItem('loom.staleDismissed')===r.latest){ box.style.display='none'; return; }
  const cur=r.current ? ' (actuellement v'+escHtml(r.current)+')' : '';
  box.innerHTML='⚠ Le serveur Loom de cette machine n\'est pas à jour'+cur+'. '+
    'La version <b>v'+escHtml(r.latest)+'</b> est disponible. '+
    '<span style="white-space:nowrap"><span id="stale-go" style="cursor:pointer;text-decoration:underline">Mettre à jour</span> '+
    '· <span id="stale-x" style="cursor:pointer;text-decoration:underline">ignorer</span></span>';
  box.style.display='';
  const go=document.getElementById('stale-go');
  if(go) go.onclick=function(){
    box.innerHTML='⏳ Mise à jour en cours… le serveur va redémarrer et l\'interface se reconnectera.';
    if(typeof toast==='function') toast('mise à jour du serveur lancée…');
    if(typeof applyUpdate==='function') applyUpdate();
  };
  const x=document.getElementById('stale-x');
  if(x) x.onclick=function(){ localStorage.setItem('loom.staleDismissed', r.latest); box.style.display='none'; };
}
// Journal du moteur — replié par défaut, on l'ouvre en cliquant la pastille.
function toggleSvcLog(){
  const eng=document.getElementById('side-engine');
  if(!eng) return;
  const show = !eng.classList.contains('open');
  eng.classList.toggle('open', show);
  if(show){ loadSvcLog(); showPaths(); }
}
async function loadSvcLog(){
  const el=document.getElementById('svc-log');
  if(!el) return;
  el.textContent='chargement du journal…';
  try{
    const r=await jget('/api/service/log?n=120');
    el.textContent = (r && r.log && r.log.trim()) ? r.log : 'journal vide — le moteur n\'a encore rien écrit.';
    el.scrollTop = el.scrollHeight;
  }catch(e){ el.textContent='journal indisponible : '+e; }
}
async function checkUpdate(){
  const b=document.getElementById('upd-check'), msg=document.getElementById('upd-msg');
  b.disabled=true; msg.textContent='Vérification…';
  try{
    const r=await jget('/api/update');
    if(r.error){ msg.textContent='Erreur : '+r.error; }
    else if(r.available){
      msg.innerHTML='Nouvelle version <b>v'+r.latest+'</b> disponible. ';
      const btn=document.createElement('button'); btn.textContent='Mettre à jour'; btn.onclick=applyUpdate;
      msg.appendChild(btn);
    } else { msg.textContent='Loom est à jour ✓'; }
  }catch(e){ msg.textContent='Erreur réseau'; }
  b.disabled=false;
}
// Emplacements — affichés avec le journal du moteur : c'est le panneau qu'on
// ouvre quand on cherche à comprendre l'état de son installation.
async function showPaths(){
  const el=document.getElementById('paths-msg');
  if(!el) return;
  el.textContent='…';
  try{
    const p=await jget('/api/paths');
    const rows=[['Données',p.home],['Base (config, préférences, conversation)',p.database],['Modèles',p.models],['Presets',p.presets],['Mémoire',p.memory],['Fichiers créés par l\'IA',p.workspace],['Moteur llama.cpp',p.backends],['Programme',p.exe]];
    el.innerHTML=rows.map(r=>'<div style="margin-bottom:4px">'+r[0]+'<br><code style="word-break:break-all">'+escHtml(r[1]||'')+'</code></div>').join('');
  }catch(e){ el.textContent='Erreur'; }
}
async function applyUpdate(){
  const msg=document.getElementById('upd-msg');
  msg.textContent='Téléchargement et installation…';
  try{
    // Signal dédié : le timeout par défaut (30 s) coupe le téléchargement du
    // binaire sur une connexion lente et fait croire à un échec alors que la
    // mise à jour aboutit côté serveur.
    const ac=new AbortController(); const t=setTimeout(()=>ac.abort(), 10*60*1000);
    let r;
    try{ r=await (await jfetch('/api/update/apply',{method:'POST',headers:{'Content-Type':'application/json'},body:'{}',signal:ac.signal})).json(); }
    finally{ clearTimeout(t); }
    if(r.ok){
      msg.innerHTML='✓ Installé en <b>v'+r.version+'</b>.<br>'+(r.restart||'');
      // Redémarrage auto du service côté serveur : le flux va se couper puis
      // reconnecter tout seul (connectStream boucle). On rafraîchit l'état après.
      if(r.restarting){ toast('mise à jour appliquée — reconnexion…'); setTimeout(loadAll, 6000); }
    }
    else { msg.textContent='Échec : '+(r.error||'inconnu'); }
  }catch(e){ msg.textContent='Erreur pendant la mise à jour (réessaie).'; }
}
// Compteur de contexte : CTX_USED estimé via les stats serveur (prefill+decode
// du dernier tour ≈ taille du prochain prompt). À 90% on propose de compacter.
let CTX_MAX=0, CTX_USED=0, MODEL_READY=false;
// STATUS_SEEN : /api/status a répondu au moins une fois. Avant ça (ou s'il ne
// répond pas), on ne verrouille RIEN — mieux vaut un envoi qui échoue qu'un chat
// bloqué par un état inconnu.
let STATUS_SEEN=false;
function setCtxUsed(n){ CTX_USED=n||0; updateCtxMeter(); }
// Compteur de compactages de la session (issue #47) : combien de fois le contexte
// a déjà été résumé. Masqué à zéro. Sert de repère pour décider de repartir sur une
// nouvelle session avant que les tout premiers détails ne se diluent dans les
// résumés successifs.
let COMPACTIONS=0;
function setCompactCount(n){
  COMPACTIONS=n||0;
  const el=document.getElementById('ctx-compactions');
  if(!el) return;
  if(COMPACTIONS>0){
    el.textContent='· '+COMPACTIONS+'× compacté';
    el.title='Le contexte de cette session a été résumé '+COMPACTIONS+' fois. Après plusieurs compactages, les tout premiers détails se diluent : pense à repartir sur une nouvelle session si le fil dérive.';
    el.hidden = false;
  } else {
    el.hidden = true;
  }
}
function updateCtxMeter(){
  const max = CTX_MAX||0;
  const used = CTX_USED||0;
  const pct = max ? Math.min(100, Math.round(used*100/max)) : 0;
  const fill=document.getElementById('ctx-fill');
  if(fill){
    fill.style.width=pct+'%';
    fill.style.background = pct>=90 ? 'var(--err)' : pct>=70 ? 'var(--warn)' : 'var(--ok)';
  }
  const ct=document.getElementById('ctx-text');
  if(ct){
    if(!max){
      ct.textContent='— / —';
      ct.title='fenêtre de contexte inconnue';
    } else {
      ct.textContent=fmtCtxTokens(used)+' / '+fmtCtxTokens(max);
      ct.title='contexte utilisé : '+used.toLocaleString('fr')+' / '+max.toLocaleString('fr')+' jetons ('+pct+' %)';
    }
  }
  const showCompact = max>0 && pct>=50 && used>0;
  const c=document.getElementById('ctx-compact');
  if(c) c.style.display = showCompact ? 'inline-block' : 'none';
  const pc=document.getElementById('p-compact');
  if(pc) pc.hidden = !showCompact;
  if(typeof syncParamsTools==='function') syncParamsTools();
}
// Abrège un nombre de jetons pour le pied de carte : < 1000 tel quel, sinon en
// milliers avec une décimale utile (10 240 → « 10.2K », 40 960 → « 41K », le .0
// tombant). Gagne de la place là où c'est étroit.
function fmtCtxTokens(n){
  n = n||0;
  if(n < 1000) return String(n);
  const k = n/1000;
  return (k>=100 ? Math.round(k) : (Math.round(k*10)/10).toFixed(1).replace(/\.0$/,'')) + 'K';
}
// ─── Raccourci « niveau de réflexion » (composeur) ───────────────────────────
// Reflète l'effort défini sur le PRESET ACTIF (REASONING_EFFORT). Le bouton
// n'apparaît que si un effort est défini — c'est le sens de « quand on a défini un
// niveau sur un preset » : on ne propose de le changer que là où il compte.
let REASON_EFFORT = null; // null = pas encore chargé
function updateReasonBtn(eff){
  REASON_EFFORT = eff==null ? REASON_EFFORT : eff;
  const e=(REASON_EFFORT||'').trim();
  const row=document.getElementById('p-reason-row');
  const sel=document.getElementById('p-reason-live');
  if(!e){
    if(row) row.hidden = true;
    if(typeof syncParamsTools==='function') syncParamsTools();
    return;
  }
  if(row) row.hidden = false;
  if(sel && sel.value!==e) sel.value = e;
  if(typeof syncParamsTools==='function') syncParamsTools();
}
function syncParamsTools(){
  const box=document.getElementById('params-tools');
  if(!box) return;
  box.hidden = ![...box.querySelectorAll('.pe-row')].some(r=>!r.hidden);
}
function toggleReasonMenu(ev){
  ev.stopPropagation();
  const menu=document.getElementById('reason-menu');
  if(menu.style.display==='block'){ menu.style.display='none'; return; }
  // Surligne le niveau courant.
  const cur=(REASON_EFFORT||'').trim();
  menu.querySelectorAll('button').forEach(b=>b.classList.toggle('on', b.dataset.eff===cur));
  // Position : au-dessus du bouton, aligné à droite dessus (fixed, hors flux).
  const r=document.getElementById('reason-btn').getBoundingClientRect();
  menu.style.display='block';
  menu.style.visibility='hidden';       // mesurer avant de placer, sans clignoter
  const mw=menu.offsetWidth, mh=menu.offsetHeight;
  let left=Math.max(8, r.right-mw);
  let top=r.top-mh-6;
  if(top<8) top=r.bottom+6;             // pas de place au-dessus : sous le bouton
  menu.style.left=left+'px';
  menu.style.top=top+'px';
  menu.style.visibility='';
}
function closeReasonMenu(){ const m=document.getElementById('reason-menu'); if(m) m.style.display='none'; }
document.addEventListener('click', (e)=>{
  const m=document.getElementById('reason-menu');
  if(m && m.style.display==='block' && !m.contains(e.target) && e.target.closest('#reason-btn')===null){ m.style.display='none'; }
});
async function pickReason(eff){
  closeReasonMenu();
  const prev=REASON_EFFORT;
  updateReasonBtn(eff);                 // retour visuel immédiat
  try{
    const r=await jpost('/api/reasoning', {effort:eff});
    if(!r || !r.ok){ throw new Error((r&&r.error)||'échec'); }
    toast('réflexion : '+(eff||'défaut du modèle'));
  }catch(err){
    updateReasonBtn(prev);              // on remet l'ancien niveau si l'écriture échoue
    toast('réglage du niveau impossible');
  }
}
async function loadVram(){
  const gpus=await jget('/api/vram');
  // Bloc de statistique : intitulé + valeur sur une ligne, jauge, détail dessous.
  // Même gabarit que la RAM (voir .stat dans le CSS) — le HTML libre d'avant
  // collait aux bords de la carte.
  document.getElementById('vram').innerHTML = (gpus||[]).map(g=>{
    const pct=Math.round(g.used*100/g.total);
    return '<div class="stat"><div class="stat-h"><span class="stat-n">'+g.name+'</span>'+
      '<span class="stat-v">'+(g.used/1024).toFixed(1)+' / '+(g.total/1024).toFixed(1)+' GiB</span></div>'+
      '<div class="bar"><div style="width:'+pct+'%"></div></div>'+
      '<div class="stat-s">GPU '+g.util+' % · '+g.temp+' °C</div></div>';
  }).join('') || '<div class="stat"><span class="stat-s">(pas de GPU)</span></div>';
}
async function loadRam(){
  const m=await jget('/api/ram');
  const box=document.getElementById('ram-details');
  if(!m || !m.total){ if(box) box.style.display='none'; return; }
  if(box) box.style.display='';
  const pct=Math.round(m.used*100/m.total);
  document.getElementById('ram').innerHTML =
    '<div class="stat"><div class="stat-h"><span class="stat-n">Mémoire vive</span>'+
    '<span class="stat-v">'+(m.used/1024).toFixed(1)+' / '+(m.total/1024).toFixed(1)+' GiB</span></div>'+
    '<div class="bar"><div style="width:'+pct+'%"></div></div>'+
    '<div class="stat-s">'+pct+' % utilisée</div></div>';
}
async function loadCfg(){
  // /api/llamacpp en parallèle : il indique si le BIN de la config correspond au
  // précompilé (prebuilt.in_use) ou compilé ici (in_use) — sinon c'est un fork perso.
  const [c, lc] = await Promise.all([jget('/api/config'), jget('/api/llamacpp').catch(()=>null)]);
  const row=(k,v,title)=>'<div class="kv"><span>'+k+'</span><span title="'+String(title!=null?title:v).replace(/"/g,'&quot;')+'">'+String(v)+'</span></div>';
  const rows=[];
  if(c.BIN){
    // Moteur : précompilé / compilé / personnalisé (avec le chemin). Le title garde
    // toujours le chemin complet, quel que soit le libellé.
    let v;
    if(lc && lc.kind === 'full') v = 'llama.cpp complet';
    else if(lc && lc.kind === 'server') v = 'llama-server';
    else v = c.BIN;
    rows.push(row('Moteur', v, c.BIN));
  }
  const labs={MODEL:'Modèle',CTX:'Contexte',BATCH:'Batch',UBATCH:'Ubatch',NGL:'Couches GPU'};
  ['MODEL','CTX','BATCH','UBATCH','NGL'].filter(k=>c[k]).forEach(k=>{
    let v=c[k]; if(k==='MODEL') v=v.split('/').pop();
    rows.push(row(labs[k]||k, v));
  });
  // n-cpu-moe : affiché seulement s'il est réellement présent dans EXTRA_ARGS.
  const m=(c.EXTRA_ARGS||'').match(/--n-cpu-moe\s+(\d+)/);
  if(m) rows.push(row('N-CPU-MOE', m[1]));
  document.getElementById('cfg').innerHTML = rows.join('');
  // Raccourci « niveau de réflexion » du composeur : présent seulement si le preset
  // actif définit un effort. Rafraîchi à chaque loadCfg (donc après une bascule de
  // preset ou une édition).
  updateReasonBtn(c.REASONING_EFFORT || '');
}
function syncEngineBar(s){
  const box=document.getElementById('side-engine');
  if(!box) return;
  const hasModel = !!(s.model && s.model.trim());
  const ok=!!(s.active && (s.health || !hasModel) && !s.load_error);
  const loading=!!(s.active && hasModel && !s.health && !s.load_error);
  const open = box.classList.contains('open');
  box.className='side-engine '+(ok?'ok':(loading?'loading':'err'))+(open?' open':'');
  const lab=document.getElementById('eng-label');
  if(lab){
    if(ok) lab.textContent = s.health ? 'llama.cpp connecté' : 'llama.cpp connecté';
    else if(s.load_error) lab.textContent='llama.cpp erreur';
    else if(loading) lab.textContent='llama.cpp charge…';
    else lab.textContent='llama.cpp coupé';
  }
  const start=document.getElementById('eng-start');
  const stop=document.getElementById('eng-stop');
  const restart=document.getElementById('eng-restart');
  if(start) start.disabled=!!s.active;
  if(stop) stop.disabled=!s.active;
  if(restart) restart.disabled=!s.active;
}
