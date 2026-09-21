// Page Serveur : endpoint /v1, stats, requêtes en cours (tokens + tok/s live).

let srvPoll = null, srvLast = null, srvPickAt = 0, srvPickSig = '';

function openServer(){
  if(typeof showMainView === 'function') showMainView('server');
  if(typeof loadNetwork === 'function') loadNetwork();
  srvSyncPoll();
}
function openAPIKeySettings(){
  if(typeof openSettings === 'function') openSettings('api');
  const el = document.getElementById('oai-key-require');
  if(el) setTimeout(()=>{ try{ el.focus(); }catch(_){ } }, 80);
}
function srvViewOpen(){
  const v = document.getElementById('srv-view');
  return !!(v && !v.hidden);
}
function srvSettingsOpen(){
  const s = document.getElementById('settings-view');
  const tab = document.querySelector('.set-tab.active');
  return !!(s && !s.hidden && tab && tab.dataset.set === 'api');
}
function srvOnTab(id){
  srvSyncPoll();
}
function srvSyncPoll(){
  if(srvPoll){ clearInterval(srvPoll); srvPoll = null; }
  if(srvViewOpen()){
    srvLoad();
    srvPoll = setInterval(srvLoad, 500);
  } else if(srvSettingsOpen()){
    srvLoad();
    srvPoll = setInterval(srvLoad, 1500);
  }
}
async function srvLoad(){
  let s;
  try{ s = await jget('/api/server'); }catch(_){ return; }
  srvLast = s;
  srvPaint(s);
}
function srvFmtN(n){
  n = Number(n)||0;
  try{ return n.toLocaleString('fr-FR'); }catch(_){ return String(n); }
}
function srvFmtSpeed(v){
  v = Number(v)||0;
  if(v <= 0) return '—';
  const n = Math.round(v*10)/10;
  return String(n).replace('.', ',')+' tok/s';
}
function srvFmtMs(ms){
  ms = Number(ms)||0;
  if(ms <= 0) return '—';
  if(ms < 1000) return Math.round(ms)+' ms';
  const s = ms/1000;
  return (s >= 10 ? s.toFixed(0) : s.toFixed(1).replace(/\.0$/,''))+' s';
}
function srvPaintKeyBtn(d){
  const btn = document.getElementById('srv-key-btn');
  if(!btn) return;
  const set = !!(d && (d.set || d.key_set));
  btn.textContent = set ? 'Gérer la clé API' : 'Créer une clé API';
}
function srvPaintNow(s){
  const box = document.getElementById('srv-now');
  if(!box) return;
  const name = s.preset_name || s.model_name || '';
  const path = s.model || '';
  const hasModel = !!(name || path);
  const st = !s.running ? 'off' : (!s.health ? (hasModel ? 'warn' : 'ok') : 'ok');
  const stLab = !s.running ? 'Moteur arrêté' : (!s.health ? (hasModel ? 'Chargement…' : 'Moteur prêt (en veille)') : 'Prêt');
  const bits = [];
  if(s.preset_name && s.model_name) bits.push(s.model_name);
  if(path) bits.push(path);
  bits.push(stLab);
  const title = name || 'Aucun modèle chargé';
  const meta = hasModel ? bits.join(' · ') : 'Moteur actif (0 VRAM). Charge un GGUF ou un preset pour inférer.';
  const pathCls = path ? ' path' : '';
  let html = '<span class="srv-now-dot '+st+'" aria-hidden="true"></span>'
    + '<div class="srv-now-t"><div class="srv-now-n">'+escHtml(title)+'</div>'
    + '<div class="srv-now-m'+pathCls+'" title="'+escHtml(path)+'">'+escHtml(meta)+'</div></div>';
  if(s.running && hasModel){
    html += '<button type="button" class="lc-update-link" onclick="srvUnload()">Décharger</button>';
  }
  box.innerHTML = html;
}
function srvPaint(s){
  if(!s) return;
  const who = s.preset_name ? (s.preset_name + (s.model_name ? ' · ' + s.model_name : '')) : (s.model_name || '');
  const sub = document.getElementById('srv-head-sub');
  if(sub){
    if(!s.running) sub.textContent = 'Moteur arrêté';
    else if(!s.health) sub.textContent = who ? 'Chargement du modèle…' : 'Moteur en veille (aucun modèle chargé)';
    else sub.textContent = who || 'Prêt';
  }
  srvPaintNow(s);
  srvPaintKeyBtn({set: s.key_set, key_set: s.key_set});

  const url = s.url || s.url_local || '';
  const dashUrl = document.getElementById('srv-url');
  if(dashUrl && url) dashUrl.value = url;
  const oai = document.getElementById('oai-url');
  if(oai && url) oai.value = url;
  const lab = document.getElementById('srv-url-label');
  if(lab) lab.textContent = s.lan ? 'Adresse réseau — llama-server /v1' : 'Adresse locale — llama-server /v1';
  const meta = document.getElementById('srv-url-meta');
  if(meta){
    const bits = [];
    bits.push(s.lan ? 'Réseau local' : 'Cette machine seulement');
    bits.push(s.key_required ? 'clé obligatoire' : (s.key_set ? 'clé définie' : 'sans clé'));
    meta.textContent = bits.join(' · ');
  }
  const lan = document.getElementById('srv-lan-toggle');
  if(lan) lan.checked = !!s.lan;
  srvPaintCurl();
  const npVal = String(s.np_live || s.np || 4);
  const np = document.getElementById('srv-np');
  if(np && document.activeElement !== np) np.value = npVal;
  const dashNp = document.getElementById('srv-dash-np');
  if(dashNp && document.activeElement !== dashNp) dashNp.value = npVal;
  const nph = document.getElementById('srv-np-h');
  if(nph) nph.textContent = (s.np_live && s.np_live !== s.np) ? ('en cours : ' + s.np_live) : '';
  const nphr = document.getElementById('srv-np-h-row');
  if(nphr) nphr.hidden = !(nph && nph.textContent);
  srvPaintMetrics(s);
  srvPaintLive(s);
  srvPaintHist(s.recent);
  srvFillPick(s);
}
function srvPaintCurl(){
  const pre = document.getElementById('srv-curl');
  const btn = document.getElementById('srv-curl-copy');
  if(!pre) return;
  const url = (document.getElementById('srv-url')||{}).value || (document.getElementById('oai-url')||{}).value || (srvLast && (srvLast.url || srvLast.url_local)) || '';
  if(!url){ pre.hidden = true; if(btn) btn.hidden = true; return; }
  const model = (srvLast && srvLast.model_name) || 'local';
  const key = (typeof OAI_KEY === 'string' && OAI_KEY) ? OAI_KEY : '';
  let cmd = 'curl '+url+'/chat/completions \\\n  -H \'Content-Type: application/json\'';
  if(key) cmd += ' \\\n  -H \'Authorization: Bearer '+key+'\'';
  cmd += ' \\\n  -d \'{"model":'+JSON.stringify(model)+',"messages":[{"role":"user","content":"bonjour"}]}\'';
  pre.textContent = cmd;
  pre.hidden = false;
  if(btn) btn.hidden = false;
}
function srvPaintMetrics(s){
  const box = document.getElementById('srv-metrics');
  if(!box) return;
  const slots = s.slots || {};
  const st = s.stats || {};
  const n = slots.n || s.np || 4;
  const busy = slots.busy || st.inflight || 0;
  const live = Number(st.live_toks)||0;
  const avg = Number(st.avg_toks)||0;
  const debit = busy ? live : avg;
  const debitL = busy ? 'Débit' : 'Moyenne';
  const card = (k,v)=>'<div class="srv-metric"><div class="srv-metric-k">'+k+'</div><div class="srv-metric-v">'+v+'</div></div>';
  box.innerHTML = card('En cours', String(busy))
    + card('Slots', busy+'/'+n)
    + card('Terminées', srvFmtN(st.completed||0))
    + card(debitL, srvFmtSpeed(debit));
}
function srvPaintLive(s){
  const box = document.getElementById('srv-live');
  if(!box) return;
  const items = ((s.slots||{}).items||[]).filter(it=>it.busy);
  if(!s.running){
    box.innerHTML = '<div class="srv-empty">Le moteur est arrêté. Démarre le moteur pour servir /v1.</div>';
    return;
  }
  if(!s.model && !s.preset_name){
    box.innerHTML = '<div class="srv-empty">Moteur en veille. Aucun modèle n’est chargé en VRAM. Sélectionne un modèle pour inférer.</div>';
    return;
  }
  if(!s.health){
    box.innerHTML = '<div class="srv-empty">Chargement du modèle…</div>';
    return;
  }
  if(s.slots && s.slots.ok === false){
    box.innerHTML = '<div class="srv-empty">Suivi live indisponible : ce llama-server n’expose pas /slots. Un redémarrage après mise à jour du moteur l’active souvent.</div>';
    return;
  }
  if(!items.length){
    box.innerHTML = '<div class="srv-empty">Aucune requête en cours. Les appels /v1 s’affichent ici en direct (jetons et vitesse).</div>';
    return;
  }
  box.innerHTML = items.map(it=>{
    const prompt = it.prompt ? srvFmtN(it.prompt)+' entrée' : '';
    const gen = srvFmtN(it.tokens||0)+' générés';
    const spd = srvFmtSpeed(it.toks);
    const el = srvFmtMs(it.elapsed_ms);
    return '<div class="srv-req">'
      + '<div class="srv-req-h"><span class="srv-req-dot"></span>Slot '+it.id+'</div>'
      + '<div class="srv-req-n" title="Jetons en entrée (contexte/prompt) → jetons générés en sortie">'+(prompt?prompt+' → ':'')+gen+'</div>'
      + '<div class="srv-req-s">'+spd+' · '+el+'</div>'
      + '</div>';
  }).join('');
}
let srvHistExpanded = false;
let srvHistLimit = 5;

try {
  const savedLim = localStorage.getItem('loom_srv_hist_limit');
  if(savedLim) srvHistLimit = Number(savedLim) || 5;
} catch(_) {}

function srvToggleHistLimit(){
  srvHistExpanded = !srvHistExpanded;
  if(srvLast) srvPaintHist(srvLast.recent);
}

function srvPaintHist(list){
  const box = document.getElementById('srv-hist');
  const badge = document.getElementById('srv-hist-badge');
  if(!box) return;
  const rows = list || [];
  if(!rows.length){
    if(badge) badge.textContent = '';
    box.innerHTML = '<div class="srv-empty">Aucune pour l’instant. Les complétions /v1 restent ici tant que Loom tourne.</div>';
    return;
  }
  const total = rows.length;
  const all = rows.slice().reverse();
  const limit = srvHistExpanded ? total : Math.min(srvHistLimit, total);
  const visible = all.slice(0, limit);

  if(badge){
    badge.textContent = (total > srvHistLimit && !srvHistExpanded)
      ? '(' + visible.length + ' sur ' + total + ')'
      : '(' + total + ')';
  }

  const rowsHtml = visible.map(r=>{
    const when = r.ended ? new Date(r.ended).toLocaleTimeString() : '';
    const bits = [];
    if(r.prompt) bits.push(srvFmtN(r.prompt)+' entrée');
    bits.push(srvFmtN(r.tokens||0)+' générés');
    if(r.toks) bits.push(srvFmtSpeed(r.toks));
    if(r.ms) bits.push(srvFmtMs(r.ms));
    return '<div class="srv-hist-row" title="Jetons en entrée (contexte/prompt) → jetons générés en sortie"><span class="srv-hist-t">'+escHtml(when)+'</span><span class="srv-hist-b">slot '+r.slot+' · '+bits.join(' · ')+'</span></div>';
  }).join('');

  let footHtml = '';
  if(total > srvHistLimit){
    footHtml = '<div class="srv-hist-foot">'
      + '<button type="button" class="lc-update-link" onclick="srvToggleHistLimit()">'
      + (srvHistExpanded ? 'Réduire aux ' + srvHistLimit + ' dernières' : 'Afficher tout (' + total + ' au total)')
      + '</button>'
      + '</div>';
  }

  box.innerHTML = rowsHtml + footHtml;
}
function srvSameModel(s, m){
  if(!s || !s.model) return false;
  const val = m.value || m.path || m.name;
  return val===s.model || (m.path && m.path===s.model) || (m.name && s.model_name===m.name);
}
function srvOptGroup(sel, label){
  const g = document.createElement('optgroup');
  g.label = label;
  sel.appendChild(g);
  return g;
}
function srvOpt(parent, value, label, selected){
  const o = document.createElement('option');
  o.value = value;
  o.textContent = label;
  if(selected) o.selected = true;
  parent.appendChild(o);
}
async function srvFillPick(s){
  const sel = document.getElementById('srv-pick');
  if(!sel) return;
  const sig = ((s&&s.preset_id)||'')+'|'+(s&&s.model||'')+'|'+(s&&s.running?'1':'0');
  if(sel.options.length && sig === srvPickSig && Date.now() - srvPickAt < 8000) return;
  srvPickSig = sig;
  srvPickAt = Date.now();
  let presets=[], models=[];
  try{ presets = await jget('/api/presets') || []; }catch(_){}
  try{ models = await jget('/api/models') || []; }catch(_){}
  const weights = (models||[]).filter(m=> typeof isWeightModel==='function' ? isWeightModel(m) : !/mmproj/i.test(m.name||''));
  sel.textContent = '';
  if(!presets.length && !weights.length){
    srvOpt(sel, '', 'Aucun preset ni GGUF', true);
    return;
  }
  if(presets.length){
    const g = srvOptGroup(sel, 'Presets');
    presets.forEach((p,i)=>{
      const on = !!(s && ((s.preset_id && p.id===s.preset_id) || p.active));
      srvOpt(g, 'p:'+(i+1), p.name || p.id, on);
    });
  }
  if(weights.length){
    const g = srvOptGroup(sel, 'Modèles');
    weights.forEach(m=>{
      const path = m.path || m.value || m.name;
      srvOpt(g, 'm:'+path, m.name || path, srvSameModel(s, m));
    });
  }
}
async function srvLoadPick(){
  const sel = document.getElementById('srv-pick');
  const v = (sel && sel.value) || '';
  if(!v){ toast('choisis un modèle ou un preset'); return; }
  const label = (sel.selectedOptions[0] && sel.selectedOptions[0].textContent) || v;
  if(v.startsWith('p:')){
    const n = parseInt(v.slice(2), 10);
    if(typeof switchTo==='function') await switchTo(n, label);
  } else {
    const path = v.startsWith('m:') ? v.slice(2) : v;
    if(typeof loadNaked==='function') await loadNaked(path, label);
  }
  srvPickSig = '';
  srvLoad();
}
async function srvUnload(){
  if(typeof unloadCurrent==='function') await unloadCurrent((srvLast&&(srvLast.preset_name||srvLast.model_name))||'le modèle');
  else {
    const r = await jpost('/api/unload');
    if(!r.ok) toast('erreur : '+(r.error||''));
  }
  srvLoad();
}
async function srvApplyDashNP(){
  const n = parseInt((document.getElementById('srv-dash-np')||{}).value, 10);
  await srvDoApplyNP(n);
}
async function srvApplyNP(){
  const n = parseInt((document.getElementById('srv-np')||{}).value, 10);
  await srvDoApplyNP(n);
}
async function srvDoApplyNP(n){
  if(!(n>=1)){ toast('nombre de slots invalide'); return; }
  if(!await askConfirm('Passer à '+n+' slot'+(n>1?'s':'')+' et redémarrer llama-server ? Les requêtes concurrentes (ex: TraDoc) seront traitées en simultané.', {title:'Requêtes simultanées', okText:'Appliquer'})) return;
  const r = await jpost('/api/server', {np:n});
  if(!r || !r.ok){ toast('erreur : '+((r&&r.error)||'')); return; }
  toast('slots : '+n+' — redémarrage…');
  srvLoad();
}
