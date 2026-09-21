// Hub Hugging Face : catalogue en grille 3 cartes + modal centré 2 colonnes
// README propre façon Unsloth + liste des quants avec téléchargement direct.

let HUB = {
  q: '',
  offset: 0,
  busy: false,
  more: false,
  model: null,
  local: new Set(),
  poll: null,
  id: '',
  dlDir: '',
  place: 'lib',
  sort: 'downloads',
  pipeline: '',
  size: '',
  fit: '',
  kind: '',
  dir: '',
  vram: 0,
  ram: 0,
  activeDls: {}
};

function hubNeededMB(size, paramsB){
  const weights = (Number(size)||0) / (1024*1024);
  const kv = (Number(paramsB)||0) * 4 * 16;
  return weights*1.12 + kv + 384;
}
function hubSearchNeededMB(m){
  const b = Number(m && m.params_b)||0;
  const weights = b * 0.55 * 1024;
  return hubNeededMB(weights*1024*1024, b);
}
function hubVerdict(needed, vramTotal, vramUsed, ramTotal){
  const total = Number(vramTotal)||0;
  needed = Number(needed)||0;
  if(total > 0 && needed <= total*0.90) return 'fits';
  if(total > 0 && needed <= total) return 'tight';
  if(needed > 0 && ramTotal > 0 && needed <= ramTotal) return 'offload';
  if(!total && !ramTotal) return 'unknown';
  return 'no';
}
function hubFitLabel(v){
  return {fits:'Tient en VRAM', tight:'Juste en VRAM', offload:'Déport RAM', no:'Trop lourd', unknown:'VRAM inconnue'}[v] || v;
}
function hubFmtN(n){
  n = Number(n)||0;
  if(n >= 1e6) return (n/1e6).toFixed(1).replace(/\.0$/,'')+' M';
  if(n >= 1e3) return (n/1e3).toFixed(1).replace(/\.0$/,'')+' k';
  return String(n);
}
function hubFmtGB(mb){
  if(!mb) return '—';
  return (mb/1024).toFixed(mb >= 10240 ? 0 : 1).replace(/\.0$/,'')+' Go';
}
function hubShortName(id){
  const s = String(id||'');
  const i = s.lastIndexOf('/');
  return i >= 0 ? s.slice(i+1) : s;
}
function hubAuthorOf(id){
  const s = String(id||'');
  const i = s.indexOf('/');
  return i > 0 ? s.slice(0,i) : s;
}
function hubAvatarEl(author, cls){
  const img = document.createElement('img');
  img.className = 'hub-av'+(cls?' '+cls:'');
  img.alt = '';
  img.referrerPolicy = 'no-referrer';
  const a = String(author||'').trim();
  if(!a){ img.classList.add('hub-av-fb'); return img; }
  img.src = '/api/hub/avatar?a='+encodeURIComponent(a);
  img.onerror = ()=>{
    const s = document.createElement('span');
    s.className = img.className+' hub-av-fb';
    s.textContent = (a[0]||'?').toUpperCase();
    img.replaceWith(s);
  };
  return img;
}
function hubFiles(){
  const r = HUB.model;
  if(!r) return [];
  return (r.files||[]).slice().sort((a,b)=>(a.size||0)-(b.size||0));
}
function hubFileVerdict(f){
  const r = HUB.model;
  return hubVerdict(hubNeededMB(f.size, r.params_b), r.vram_total_mb, r.vram_used_mb, r.ram_total_mb);
}

// Nettoyage Markdown façon Unsloth (hf-readme.ts & model-readme.tsx)
const FRONTMATTER_RE = /^---\s*\n([\s\S]*?)\n---\s*\n?/;
const RE_HEADING = /^(#{1,6})\s+(.+?)\s*$/;
const ABSOLUTE_URL_RE = /^(?:[a-z][a-z0-9+.-]*:|\/\/|#|data:|mailto:|tel:)/i;

function stripFrontmatter(markdown) {
  const match = FRONTMATTER_RE.exec(markdown);
  if (match) return markdown.slice(match[0].length);
  return markdown;
}

function stripChromeHeadings(markdown) {
  const isChromeTitle = (text) => {
    const t = text.toLowerCase().replace(/[*_`]/g, '').replace(/\s+/g, ' ').trim();
    return (
      t === 'details' ||
      t === 'model card' ||
      t === 'dataset card' ||
      t.startsWith('model card for') ||
      t.startsWith('dataset card for') ||
      t.startsWith('card for')
    );
  };
  const lines = markdown.split(/\r?\n/);
  const out = [];
  for (const line of lines) {
    const m = RE_HEADING.exec(line);
    if (m && isChromeTitle(m[2])) continue;
    out.push(line);
  }
  return out.join('\n');
}

function resolveAgainstBase(src, baseUrl) {
  if (!src) return src;
  if (ABSOLUTE_URL_RE.test(src)) return src;
  try {
    return new URL(src, baseUrl).toString();
  } catch (_) {
    return src;
  }
}

function hubPrepareMarkdown(markdown) {
  if (!markdown) return '';
  let body = stripFrontmatter(markdown);
  body = stripChromeHeadings(body).trim();
  const limit = 120000;
  if (body.length > limit) {
    body = body.slice(0, limit).trimEnd() + '\n\n---\n\n*Carte tronquée pour la performance. Le README complet est disponible sur Hugging Face.*';
  }
  return body;
}

function hubPaintReadme(markdown, repoId) {
  const box = document.getElementById('hub-readme');
  if (!box) return;
  if (!markdown || !markdown.trim()) {
    box.innerHTML = '<div class="hub-empty" style="padding:24px 0">Ce modèle ne comporte pas de README.</div>';
    return;
  }
  const cleanMd = hubPrepareMarkdown(markdown);
  const baseUrl = 'https://huggingface.co/' + repoId + '/resolve/main/';

  let html = (typeof md === 'function') ? md(cleanMd) : escHtml(cleanMd);
  box.innerHTML = html;

  box.querySelectorAll('img').forEach(img => {
    const src = img.getAttribute('src');
    if (src) {
      img.src = resolveAgainstBase(src, baseUrl);
    }
    img.loading = 'lazy';
    img.referrerPolicy = 'no-referrer';
    img.onerror = () => { img.style.display = 'none'; };
  });

  box.querySelectorAll('a').forEach(a => {
    const href = a.getAttribute('href');
    if (href) {
      if (!href.startsWith('#')) {
        a.href = resolveAgainstBase(href, baseUrl);
        a.target = '_blank';
        a.rel = 'noopener noreferrer';
      }
    }
  });
}

function openHub(id){
  HUB.place = 'remote';
  if(typeof showMainView === 'function') showMainView('hub');
  hubApplyPlace();
  const box = document.getElementById('hub-results');
  if(id) hubOpenModel(id);
  else if(box && !box.dataset.ready) hubSearch(true);
  const q = document.getElementById('hub-q');
  if(q && !id) setTimeout(()=>q.focus(), 40);
}
async function openLibrary(opts){
  opts = opts||{};
  HUB.place = 'lib';
  if(typeof showMainView === 'function') showMainView('hub');
  hubApplyPlace();
  if(typeof libRender === 'function') await libRender();
  if(opts.model && typeof libOpenModel === 'function') await libOpenModel(opts.model, opts.est);
  else if(opts.presetId && typeof libOpenPreset === 'function') await libOpenPreset(opts.presetId);
}
function hubTogglePlace(){
  if(HUB.place==='lib') openHub();
  else openLibrary();
}
function hubApplyPlace(){
  const lib = HUB.place==='lib';
  if(lib){
    document.documentElement.setAttribute('data-hub-lib','1');
    document.documentElement.removeAttribute('data-hub-remote');
  } else {
    document.documentElement.removeAttribute('data-hub-lib');
    document.documentElement.setAttribute('data-hub-remote','1');
  }
  const libEl = document.getElementById('hub-lib');
  const rem = document.getElementById('hub-remote');
  if(libEl) libEl.hidden = !lib;
  if(rem) rem.hidden = lib;
  const sw = document.getElementById('hub-switch');
  if(sw){
    sw.textContent = lib ? 'Télécharger en ligne' : 'Bibliothèque';
    sw.title = lib ? 'Chercher et télécharger un GGUF sur Hugging Face' : 'Modèles et presets sur le disque';
  }
  const title = document.getElementById('hub-title');
  if(title) title.textContent = 'Modèles';
  const q = document.getElementById('hub-q');
  if(q) q.placeholder = 'Recherche';
  hubPaintFilters();
  if(typeof setParamsOpen==='function'){
    if(!lib) setParamsOpen(false);
    else {
      const show = typeof SESSION!=='undefined' && (SESSION.preview || (SESSION.mode && SESSION.mode!=='empty'));
      setParamsOpen(!!show);
    }
  }
}
function hubPaintFilters(){
  const bar = document.getElementById('hub-filters');
  if(!bar) return;
  if(HUB.place==='remote'){
    const chipP = (id, lab)=>'<button type="button" class="hub-chip'+(HUB.pipeline===id?' on':'')+'" onclick="hubSetPipeline(\''+id+'\')">'+lab+'</button>';
    const chipS = (id, lab)=>'<button type="button" class="hub-chip'+(HUB.sort===id?' on':'')+'" onclick="hubSetSort(\''+id+'\')">'+lab+'</button>';
    bar.innerHTML = 
      '<div class="hub-filter-group">' +
        chipP('', 'Tous') +
        chipP('text-generation', 'LLM') +
        chipP('image-text-to-text', 'Vision') +
        chipP('feature-extraction', 'Embeddings') +
      '</div>' +
      '<div class="hub-filter-divider"></div>' +
      '<div class="hub-filter-group">' +
        chipS('downloads', 'Téléchargements') +
        chipS('lastModified', 'Récents') +
        chipS('likes', 'J’aime') +
      '</div>' +
      '<div class="hub-filter-divider"></div>' +
      '<select class="hub-mini" id="hub-size" onchange="hubSetSize(this.value)">'+
        '<option value=""'+(HUB.size?'':' selected')+'>Toutes les tailles</option>'+
        '<option value="small"'+(HUB.size==='small'?' selected':'')+'>Jusqu’à 4B</option>'+
        '<option value="mid"'+(HUB.size==='mid'?' selected':'')+'>7 à 14B</option>'+
        '<option value="big"'+(HUB.size==='big'?' selected':'')+'>27B et plus</option>'+
      '</select>'+
      '<select class="hub-mini" id="hub-fit" onchange="hubSetFit(this.value)">'+
        '<option value=""'+(HUB.fit?'':' selected')+'>Toute VRAM</option>'+
        '<option value="vram"'+(HUB.fit==='vram'?' selected':'')+'>Tient en VRAM</option>'+
      '</select>';
    return;
  }
  const chip = (id, lab)=>'<button type="button" class="hub-chip'+(HUB.kind===id?' on':'')+'" onclick="hubSetKind(\''+id+'\')">'+lab+'</button>';
  let dirs = '<option value="">Tous les dossiers</option>';
  (HUB.dirs||[]).forEach(d=>{
    dirs += '<option value="'+escHtml(d)+'"'+(HUB.dir===d?' selected':'')+'>'+escHtml(hubDirLabel(d))+'</option>';
  });
  bar.innerHTML = chip('','Tous')+chip('preset','Presets')+chip('gguf','GGUF')+
    '<select class="hub-mini" id="hub-dir" onchange="hubSetDir(this.value)">'+dirs+'</select>';
}
function hubSetPipeline(p){ HUB.pipeline = p||''; hubPaintFilters(); hubSearch(true); }
function hubDirLabel(d){
  const s = String(d||'').replace(/\\/g,'/').replace(/\/+$/,'');
  const parts = s.split('/').filter(Boolean);
  const tidy = n => String(n||'').replace(/^models--+/, '').replace(/--/g, '/') || n;
  if(parts.length>=3 && parts[parts.length-2]==='snapshots'){
    return tidy(parts[parts.length-3]) || parts[parts.length-1];
  }
  return tidy(parts[parts.length-1]) || s || 'dossier';
}
function hubSetSort(s){ HUB.sort = s||'downloads'; hubPaintFilters(); hubSearch(true); }
function hubSetSize(s){ HUB.size = s||''; hubSearch(true); }
function hubSetFit(s){ HUB.fit = s||''; hubSearch(true); }
function hubSetKind(s){ HUB.kind = s||''; hubPaintFilters(); if(typeof libRender==='function') libRender(); }
function hubSetDir(s){ HUB.dir = s||''; if(typeof libRender==='function') libRender(); }
function hubPassSize(m){
  const b = Number(m && m.params_b)||0;
  if(!HUB.size) return true;
  if(!b) return HUB.size==='';
  if(HUB.size==='small') return b <= 4;
  if(HUB.size==='mid') return b >= 7 && b <= 16;
  if(HUB.size==='big') return b >= 27;
  return true;
}
function hubPassFit(m){
  if(HUB.fit!=='vram') return true;
  const total = Number(HUB.vram)||0;
  if(!total) return true;
  return hubSearchNeededMB(m) <= total*0.90;
}
function hubOnSearch(){
  if(HUB.place==='lib'){ if(typeof libRender==='function') libRender(); return; }
  hubSearch(true);
}
function hubOnQuery(){
  clearTimeout(HUB.t);
  HUB.t = setTimeout(()=>hubOnSearch(), 280);
}
async function hubSearch(reset){
  const box = document.getElementById('hub-results');
  if(!box || (HUB.busy && !reset)) return;
  if(reset){ HUB.offset = 0; box.innerHTML = '<div class="hub-empty">Recherche de modèles GGUF…</div>'; }
  if(!HUB.vram) await hubRefreshLocal();
  HUB.q = (document.getElementById('hub-q')||{}).value || '';
  HUB.gen = (HUB.gen||0) + 1;
  const gen = HUB.gen;
  HUB.busy = true;
  const p = new URLSearchParams({q:HUB.q, sort:HUB.sort||'downloads', offset:String(HUB.offset), limit:'24'});
  if(HUB.pipeline) p.set('pipeline', HUB.pipeline);
  let r;
  try{ r = await jget('/api/hub/search?'+p.toString()); }
  catch(_){ r = {ok:false, error:'réseau'}; }
  HUB.busy = false;
  if(gen !== HUB.gen) return;
  if(!r || !r.ok){
    box.innerHTML = '<div class="hub-err">'+(r && r.error ? escHtml(r.error) : 'Hugging Face injoignable')+'</div>';
    return;
  }
  const list = (r.models || []).filter(m=>hubPassSize(m) && hubPassFit(m));
  HUB.more = list.length >= 24;
  if(reset) box.innerHTML = '';
  if(!list.length && HUB.offset === 0){
    box.innerHTML = '<div class="hub-empty">Aucun modèle GGUF trouvé.</div>';
    box.dataset.ready = '1';
    return;
  }
  list.forEach(m=>{
    box.appendChild(hubCreateCard(m));
  });
  const oldMore = document.getElementById('hub-more-btn');
  if(oldMore) oldMore.remove();
  if(HUB.more){
    const more = document.createElement('button');
    more.type = 'button';
    more.id = 'hub-more-btn';
    more.className = 'hub-more';
    more.textContent = 'Charger plus de modèles';
    more.onclick = ()=>{ HUB.offset += 24; hubSearch(false); };
    box.appendChild(more);
  }
  box.dataset.ready = '1';
}

function hubCreateCard(m){
  const card = document.createElement('div');
  card.className = 'hub-card';
  card.dataset.id = m.id;
  card.tabIndex = 0;
  card.role = 'button';
  card.setAttribute('aria-label', m.id);

  const author = m.author || hubAuthorOf(m.id);
  const shortName = hubShortName(m.id);
  const paramsB = Number(m.params_b) || 0;
  const neededMB = hubSearchNeededMB(m);
  const verdict = hubVerdict(neededMB, HUB.vram, 0, HUB.ram);

  let pipeLabel = '';
  if(m.pipeline === 'image-text-to-text' || m.pipeline === 'image-to-text') pipeLabel = 'Vision';
  else if(m.pipeline === 'feature-extraction' || m.pipeline === 'sentence-similarity') pipeLabel = 'Embeddings';
  else if(m.pipeline === 'text-generation') pipeLabel = 'LLM';
  else if(m.pipeline) pipeLabel = m.pipeline;

  let badges = '';
  if(pipeLabel) badges += '<span class="hub-badge pipe">'+escHtml(pipeLabel)+'</span>';
  if(paramsB > 0) badges += '<span class="hub-badge params">'+paramsB+'B</span>';
  if(verdict === 'fits') badges += '<span class="hub-badge fit fits">VRAM OK</span>';
  else if(verdict === 'tight') badges += '<span class="hub-badge fit tight">Juste</span>';
  else if(verdict === 'offload') badges += '<span class="hub-badge fit offload">Déport RAM</span>';

  card.innerHTML = 
    '<div class="hub-card-head">'+
      '<div class="hub-card-av-slot"></div>'+
      '<div class="hub-card-meta">'+
        '<h3 class="hub-card-title" title="'+escHtml(shortName)+'">'+escHtml(shortName)+'</h3>'+
        '<div class="hub-card-author" title="'+escHtml(author)+'">'+escHtml(author)+'</div>'+
      '</div>'+
      (m.gated ? '<span class="hub-badge gated" title="Dépôt restreint">Gated</span>' : '')+
    '</div>'+
    '<div class="hub-card-badges">'+badges+'</div>'+
    '<div class="hub-card-foot">'+
      '<div class="hub-card-stat" title="Téléchargements">'+
        '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4M7 10l5 5 5-5M12 15V3"/></svg>'+
        '<span>'+hubFmtN(m.downloads)+'</span>'+
      '</div>'+
      '<div class="hub-card-stat" title="J’aime">'+
        '<svg viewBox="0 0 24 24" width="14" height="14" fill="currentColor"><path d="M12 21.35l-1.45-1.32C5.4 15.36 2 12.28 2 8.5 2 5.42 4.42 3 7.5 3c1.74 0 3.41.81 4.5 2.09C13.09 3.81 14.76 3 16.5 3 19.58 3 22 5.42 22 8.5c0 3.78-3.4 6.86-8.55 11.54L12 21.35z"/></svg>'+
        '<span>'+hubFmtN(m.likes)+'</span>'+
      '</div>'+
      '<div class="hub-card-arrow">'+
        '<svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M5 12h14M12 5l7 7-7 7"/></svg>'+
      '</div>'+
    '</div>';

  const avSlot = card.querySelector('.hub-card-av-slot');
  if(avSlot) avSlot.appendChild(hubAvatarEl(author, 'hub-card-av'));

  card.onclick = () => hubOpenModel(m.id);
  card.onkeydown = (e) => { if(e.key === 'Enter' || e.key === ' ') { e.preventDefault(); hubOpenModel(m.id); } };
  return card;
}

async function hubRefreshLocal(){
  HUB.local = new Set();
  try{
    const list = await jget('/api/models') || [];
    list.forEach(m=>{ const n = (m.name||'').toLowerCase(); if(n) HUB.local.add(n); });
  }catch(_){}
  try{
    const d = await jget('/api/models/dirs');
    HUB.dlDir = (d && d.download_dir) || '';
  }catch(_){}
  HUB.vram = 0;
  try{
    const gpus = await jget('/api/vram') || [];
    (gpus||[]).forEach(g=>{ HUB.vram += Number(g.total)||0; });
  }catch(_){}
  try{
    const mem = await jget('/api/memory');
    if(mem && mem.total) HUB.ram = Number(mem.total)||0;
  }catch(_){}
}

async function hubOpenModel(id){
  HUB.id = id;
  const overlay = document.getElementById('hub-modal-overlay');
  const headLeft = document.getElementById('hub-modal-head-left');
  const hfLink = document.getElementById('hub-modal-hf-link');
  const readmeBox = document.getElementById('hub-readme');
  const quantsList = document.getElementById('hub-quants-list');
  const summaryBox = document.getElementById('hub-quants-summary');
  const qlSub = document.getElementById('hub-ql-sub');

  if(!overlay) return;

  overlay.hidden = false;
  overlay.offsetHeight; // force reflow
  overlay.classList.add('open');
  document.body.style.overflow = 'hidden';

  const author = hubAuthorOf(id);
  const shortName = hubShortName(id);

  if(headLeft){
    headLeft.innerHTML = 
      '<div class="hub-modal-av-slot"></div>'+
      '<div class="hub-modal-title-wrap">'+
        '<h2 class="hub-modal-title" id="hub-modal-title">'+escHtml(shortName)+'</h2>'+
        '<div class="hub-modal-author">'+escHtml(author)+' · <span id="hub-modal-top-meta">chargement…</span></div>'+
      '</div>';
    const slot = headLeft.querySelector('.hub-modal-av-slot');
    if(slot) slot.appendChild(hubAvatarEl(author, 'hub-modal-av'));
  }

  if(hfLink){
    hfLink.href = 'https://huggingface.co/' + id;
  }

  if(readmeBox){
    readmeBox.innerHTML = 
      '<div class="hub-readme-skeleton">'+
        '<div class="hub-readme-sk-line title"></div>'+
        '<div class="hub-readme-sk-line text"></div>'+
        '<div class="hub-readme-sk-line text w80"></div>'+
        '<div class="hub-readme-sk-line text w60"></div>'+
        '<div class="hub-readme-sk-line card"></div>'+
      '</div>';
  }
  if(quantsList){
    quantsList.innerHTML = '<div class="hub-empty" style="padding:24px 0">Chargement des fichiers .gguf…</div>';
  }
  if(summaryBox) summaryBox.innerHTML = '';
  if(qlSub) qlSub.textContent = '';

  await hubRefreshLocal();

  let r;
  try{ r = await jget('/api/hub/model?id='+encodeURIComponent(id)); }
  catch(_){ r = {ok:false, error:'réseau'}; }

  if(HUB.id !== id) return;

  if(!r || !r.ok){
    if(readmeBox) readmeBox.innerHTML = '<div class="hub-err">'+(r && r.error ? escHtml(r.error) : 'impossible de lire le dépôt')+'</div>';
    if(quantsList) quantsList.innerHTML = '<div class="hub-err">Échec du chargement des quantifications.</div>';
    return;
  }

  HUB.model = r;
  hubRenderModalDetails(r);
}

function hubCloseModel(){
  const overlay = document.getElementById('hub-modal-overlay');
  if(overlay){
    overlay.classList.remove('open');
    setTimeout(() => {
      if(!overlay.classList.contains('open')) overlay.hidden = true;
    }, 200);
  }
  document.body.style.overflow = '';
  HUB.model = null;
  HUB.id = '';
}

function hubOnOverlayClick(e){
  if(e.target && e.target.id === 'hub-modal-overlay'){
    hubCloseModel();
  }
}

function hubRenderModalDetails(r){
  const topMeta = document.getElementById('hub-modal-top-meta');
  if(topMeta){
    const parts = [];
    if(r.downloads) parts.push(hubFmtN(r.downloads)+' téléchargements');
    if(r.likes) parts.push(hubFmtN(r.likes)+' j’aime');
    if(r.params_b) parts.push(r.params_b+'B');
    if(r.license) parts.push(r.license);
    topMeta.textContent = parts.join(' · ');
  }

  const summaryBox = document.getElementById('hub-quants-summary');
  if(summaryBox){
    const vramStr = r.vram_total_mb ? hubFmtGB(r.vram_total_mb)+' VRAM détectée' : 'Pas de GPU';
    const ramStr = r.ram_total_mb ? hubFmtGB(r.ram_total_mb)+' RAM' : '';
    summaryBox.innerHTML = 
      '<div class="hub-quants-summary-row">'+
        '<span class="hub-quants-summary-k">Matériel hôte</span>'+
        '<span class="hub-quants-summary-v">'+escHtml(vramStr)+(ramStr ? ' · ' + escHtml(ramStr) : '')+'</span>'+
      '</div>'+
      '<div class="hub-quants-summary-row">'+
        '<span class="hub-quants-summary-k">Taille du modèle</span>'+
        '<span class="hub-quants-summary-v">'+(r.params_b ? escHtml(r.params_b)+'B paramètres' : 'Non précisé')+'</span>'+
      '</div>'+
      (r.license ? 
      '<div class="hub-quants-summary-row">'+
        '<span class="hub-quants-summary-k">Licence</span>'+
        '<span class="hub-quants-summary-v">'+escHtml(r.license)+'</span>'+
      '</div>' : '')+
      (HUB.dlDir ? 
      '<div class="hub-quants-summary-row">'+
        '<span class="hub-quants-summary-k">Dossier de dépôt</span>'+
        '<span class="hub-quants-summary-v" title="'+escHtml(HUB.dlDir)+'">'+escHtml(hubDirLabel(HUB.dlDir))+'</span>'+
      '</div>' : '')+
      (r.vision && r.vision.length ? 
      '<div class="hub-quants-summary-row">'+
        '<span class="hub-quants-summary-k">Multimodal</span>'+
        '<span class="hub-quants-summary-v" style="color:#60a5fa">Projecteur vision mmproj inclus</span>'+
      '</div>' : '');
  }

  const quantsList = document.getElementById('hub-quants-list');
  const qlSub = document.getElementById('hub-ql-sub');
  const files = hubFiles();
  if(qlSub) qlSub.textContent = files.length ? files.length + ' format' + (files.length > 1 ? 's' : '') : '';

  if(!files.length){
    if(quantsList) quantsList.innerHTML = '<div class="hub-empty">Aucun fichier .gguf trouvé dans ce dépôt.</div>';
  } else if(quantsList){
    quantsList.innerHTML = '';
    files.forEach((f, idx)=>{
      quantsList.appendChild(hubCreateQuantCard(f, idx));
    });
  }

  hubPaintReadme(r.readme, r.id);
}

function hubCreateQuantCard(f, idx){
  const card = document.createElement('div');
  card.className = 'hub-quant-card';
  card.dataset.quantFile = f.name;

  const v = hubFileVerdict(f);
  const need = hubNeededMB(f.size, HUB.model.params_b);
  const local = HUB.local.has(String(f.name||'').toLowerCase());
  const activeDl = HUB.activeDls[f.name];

  const quantName = f.quant || f.name;
  const sizeStr = f.size ? fmtSize(f.size) : '—';
  const needStr = '~' + hubFmtGB(need) + ' en VRAM';

  card.innerHTML = 
    '<div class="hub-quant-top">'+
      '<span class="hub-quant-name">'+escHtml(quantName)+'</span>'+
      '<span class="hub-quant-size">'+escHtml(sizeStr)+'</span>'+
    '</div>'+
    '<div class="hub-quant-mid">'+
      '<span class="hub-quant-dot '+v+'"></span>'+
      '<span class="hub-quant-verdict">'+escHtml(hubFitLabel(v))+' ('+escHtml(needStr)+')</span>'+
    '</div>'+
    '<div class="hub-quant-act" id="hub-qact-'+idx+'"></div>';

  const actSlot = card.querySelector('#hub-qact-'+idx);
  hubPaintQuantAction(actSlot, f, idx, local, activeDl);
  return card;
}

function hubPaintQuantAction(slot, f, idx, local, activeDl){
  if(!slot) return;
  if(local){
    slot.innerHTML = '<button type="button" class="hub-btn-dl" disabled>✓ Sur le disque</button>';
    return;
  }
  if(activeDl && !activeDl.finished && !activeDl.error){
    const pct = activeDl.total > 0 ? (activeDl.done * 100 / activeDl.total) : 0;
    const progText = activeDl.total > 0 
      ? fmtSize(activeDl.done) + ' / ' + fmtSize(activeDl.total) + ' (' + Math.round(pct) + '%)'
      : fmtSize(activeDl.done);
    const speedText = activeDl.speed > 0 ? ' · ' + fmtSize(activeDl.speed) + '/s' : '';
    slot.innerHTML = 
      '<div class="hub-prog-row">'+
        '<div class="pe-bar" style="width:100%;margin:0"><i style="width:'+Math.min(100, pct).toFixed(1)+'%"></i></div>'+
        '<div class="hub-prog-meta">'+
          '<span>'+escHtml(progText + speedText)+'</span>'+
          '<button type="button" class="hub-btn-cancel" onclick="hubCancelDownload(\''+escHtml(f.name)+'\')">Annuler</button>'+
        '</div>'+
      '</div>';
    return;
  }
  slot.innerHTML = '<button type="button" class="hub-btn-dl" id="hub-dl-btn-'+idx+'" onclick="hubDownloadQuant('+idx+')">Télécharger</button>';
}

async function hubDownloadQuant(idx){
  const files = hubFiles();
  const f = files[idx];
  if(!f) return;
  const btn = document.getElementById('hub-dl-btn-'+idx);
  if(btn){ btn.disabled = true; btn.textContent = 'Vérification…'; }

  const p = await jpost('/api/models/download/probe', {url: f.url, dir: ''});
  if(!p.ok || !p.enough){
    if(typeof toast === 'function') toast(p.error || 'Espace disque insuffisant');
    if(btn){ btn.disabled = false; btn.textContent = 'Télécharger'; }
    return;
  }

  const r = await jpost('/api/models/download', {url: f.url, dir: ''});
  if(!r.ok){
    if(typeof toast === 'function') toast(r.error || 'Échec du lancement du téléchargement');
    if(btn){ btn.disabled = false; btn.textContent = 'Télécharger'; }
    return;
  }

  const fname = r.filename || f.name;
  HUB.activeDls[fname] = {
    quant: f.quant,
    idx: idx,
    done: 0,
    total: f.size || 0,
    speed: 0,
    finished: false
  };

  hubRefreshQuantCard(fname);
  hubEnsureWatcher();
}

async function hubCancelDownload(fname){
  try {
    await jpost('/api/models/download/cancel', {filename: fname});
  } catch(_) {}
  delete HUB.activeDls[fname];
  hubRefreshQuantCard(fname);
}

function hubRefreshQuantCard(fname){
  const card = document.querySelector('.hub-quant-card[data-quant-file="'+fname+'"]');
  if(!card) return;
  const files = hubFiles();
  const idx = files.findIndex(f => f.name === fname);
  if(idx < 0) return;
  const f = files[idx];
  const local = HUB.local.has(String(fname).toLowerCase());
  const activeDl = HUB.activeDls[fname];
  const actSlot = card.querySelector('.hub-quant-act');
  hubPaintQuantAction(actSlot, f, idx, local, activeDl);
}

function hubEnsureWatcher(){
  if(HUB.poll) return;
  HUB.poll = setInterval(hubWatcherTick, 800);
  hubWatcherTick();
}

async function hubWatcherTick(){
  if(!Object.keys(HUB.activeDls).length){
    if(HUB.poll){ clearInterval(HUB.poll); HUB.poll = null; }
    return;
  }
  let list;
  try{ list = await jget('/api/models/download/status'); }catch(_){ return; }
  const statusList = list || [];

  for(const fname of Object.keys(HUB.activeDls)){
    const st = statusList.find(d => d.filename === fname);
    if(!st) continue;
    if(st.canceled || st.error){
      if(st.error && typeof toast === 'function') toast(st.error);
      delete HUB.activeDls[fname];
      hubRefreshQuantCard(fname);
      continue;
    }
    if(st.finished){
      HUB.local.add(String(fname).toLowerCase());
      delete HUB.activeDls[fname];
      hubRefreshQuantCard(fname);
      if(typeof toast === 'function') toast('Téléchargement terminé : ' + fname);
      if(typeof populateModelPicker === 'function') populateModelPicker();
      if(typeof libRender === 'function' && HUB.place === 'lib') libRender();
      continue;
    }
    HUB.activeDls[fname].done = st.done;
    HUB.activeDls[fname].total = st.total;
    HUB.activeDls[fname].speed = st.speed;
    hubRefreshQuantCard(fname);
  }
}

document.addEventListener('keydown', e => {
  if(e.key === 'Escape' && HUB.id){
    hubCloseModel();
  }
});
