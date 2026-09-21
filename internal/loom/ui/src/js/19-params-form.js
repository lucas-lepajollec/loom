// Formulaire de paramètres partagé : panneau latéral ET popup preset.
// Trois niveaux (base / avancé / expert). Les widgets expert viennent du
// catalogue /api/llama-flags (llama-server --help).

let PF_FLAGS = null;
let PF_CAPS = {native_ctx:0, n_layers:0, thinks:false, has_effort:false, effort:[], effort_default:'', mmproj:[], nextn:0};
let PF_MODELS = [];
let pfMountSeq = 0;

const PF_META = {
  'ctx-size': {label:'Contexte', tip:'Fenêtre de tokens. Le curseur va jusqu’au natif du GGUF. Clique la bulle pour taper une valeur plus grande.'},
  'gpu-layers': {label:'Couches GPU', tip:'Combien de couches vont sur le GPU. « tout » envoie 999 (tout offload).'},
  'n-gpu-layers': {label:'Couches GPU', tip:'Combien de couches vont sur le GPU. « tout » envoie 999 (tout offload).'},
  'reasoning': {label:'Raisonnement', tip:'Le GGUF pense si son gabarit chat contient enable_thinking, <think>, etc. Allumé par défaut dans ce cas ; l’éteindre envoie --reasoning off.'},
  'reasoning-effort': {label:'Effort', tip:'Niveaux lus dans le gabarit du GGUF (ex. Qwen : low / medium / xhigh). Absent si le modèle n’a que on/off. Vide = défaut du gabarit.'},
  'temp': {label:'Température', tip:'Aléatoire de l’échantillonnage. Vide = défaut du modèle.'},
  'temperature': {label:'Température', tip:'Aléatoire de l’échantillonnage. Vide = défaut du modèle.'},
  'sysprompt': {label:'Prompt système', tip:'Instructions propres à ce modèle ou preset. Vide = celui des Réglages. Si les deux sont vides, aucun prompt n’est envoyé.'},
  'mmproj': {label:'Vision', tip:'Uniquement si un mmproj*.gguf est à côté du modèle (ou déjà choisi). Pas de liste de tous les projecteurs du disque.'},
  'cache-type-k': {label:'Cache KV', tip:'Compression du cache de contexte. f16 = max qualité ; q8/q4 réduisent la VRAM.'},
  'parallel': {label:'Slots parallèles', tip:'Séquences simultanées (-np). Continuous batching natif : 4 slots par défaut permettent à TraDoc ou aux clients externes d’inférer sans attente.'},
  'spec-type': {label:'Décodage spéculatif', tip:'Anticipe des jetons (MTP, EAGLE, n-grammes…) pour accélérer.'},
  'spec-draft-n-max': {label:'Jetons anticipés', tip:'Combien de jetons le brouillon propose par étape (défaut 3).'},
  'model-draft': {label:'Modèle de draft', tip:'GGUF brouillon pour EAGLE / un MTP fourni à part. Inutile si MTP est intégré.'},
  'batch-size': {label:'BATCH', tip:'Taille logique de lot au prompt. Plus grand = plus rapide, plus de VRAM.'},
  'ubatch-size': {label:'UBATCH', tip:'Taille physique de micro-lot. 512 est un bon départ.'},
  'flash-attn': {label:'Flash attention', tip:'Plus rapide, moins de VRAM. Auto laisse le moteur décider.'},
  'load-mode': {label:'Chargement', tip:'auto / mmap / mlock / dio. mlock garde le modèle en RAM.'},
  'mlock': {label:'Garder en RAM', tip:'Ancien --mlock. Préfère load-mode = mlock sur les moteurs récents.'},
  'mmap': {label:'Charger tout en mémoire', tip:'Désactive le mmap (--no-mmap) : charge le GGUF d’un coup.'},
  'threads': {label:'Threads CPU', tip:'0 ou -1 = auto.'},
  'threads-batch': {label:'Threads (batch)', tip:'Threads pour le prompt. 0 = même valeur que Threads.'},
  'n-cpu-moe': {label:'Experts MoE sur CPU', tip:'Garde les N premières couches d’experts sur le CPU. Vide = non.'},
  'kv-unified': {label:'Cache KV unifié', tip:'Un seul cache partagé entre les slots (--kv-unified).'},
  'jinja': {label:'Jinja', tip:'Moteur de gabarit chat. Souvent requis pour l’effort de réflexion.'},
  'fit': {label:'Ajuster à la VRAM', tip:'Laisse llama-server réduire le contexte / les couches pour tenir en mémoire.'},
  'top-p': {label:'top_p', tip:'Noyau de probabilité. 1 = désactivé.'},
  'top-k': {label:'top_k', tip:'0 = désactivé.'},
  'min-p': {label:'min_p', tip:'Seuil de probabilité. 0 = désactivé.'},
  'presence-penalty': {label:'Pénalité de présence', tip:'Pousse vers de nouveaux mots. 0 = off.'},
  'repeat-penalty': {label:'Pénalité de répétition', tip:'Anti-répétition. 1 = neutre.'}
};

const PF_KV_OPTS = [['','f16 — max qualité'],['q8_0|q8_0','q8_0'],['q4_0|q4_0','q4_0'],['q5_1|q5_1','q5_1'],['q8_0|q5_1','q8_0 / q5_1'],['q8_0|q5_0','q8_0 / q5_0'],['q5_0|q4_1','q5_0 / q4_1'],['q5_0|q4_0','q5_0 / q4_0']];
const PF_SPEC_OPTS = [['','non'],['draft-mtp','MTP'],['draft-eagle3','EAGLE-3'],['draft-simple','modèle brouillon'],['draft-dflash','dFlash'],['draft-dspark','dSpark'],['ngram-simple','n-grammes'],['ngram-mod','n-grammes (mod)'],['ngram-cache','n-grammes (cache)'],['ngram-map-k','n-grammes (map-k)'],['ngram-map-k4v','n-grammes (map-k4v)']];
const PF_ADV_SKIP = { 'cache-type-k':1,'cache-type-v':1,'mmproj':1,'mmproj-auto':1,'mmproj-offload':1,'batch-size':1,'ubatch-size':1,'threads':1,'threads-batch':1,'n-cpu-moe':1,'flash-attn':1,'load-mode':1,'mlock':1,'mmap':1,'kv-unified':1,'jinja':1,'fit':1,'parallel':1,'spec-type':1,'spec-draft-n-max':1,'model-draft':1,'gpu-layers':1,'n-gpu-layers':1,'ctx-size':1,'reasoning':1,'reasoning-effort':1,'temp':1,'temperature':1 };

let _sysLiveT = null;
function scheduleLiveSys(text){
  if(typeof SESSION!=='undefined' && SESSION.preview) return;
  if(typeof SESSION!=='undefined' && SESSION.mode==='empty') return;
  clearTimeout(_sysLiveT);
  _sysLiveT = setTimeout(()=>{
    jpost('/api/sysprompt/model', {text: text||''}).catch(()=>{});
  }, 400);
}

function pfEsc(s){ return String(s==null?'':s).replace(/[&<>"']/g, c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c])); }
function pfFlag(id){ return (PF_FLAGS||[]).find(f=>f.id===id) || null; }
function pfHas(id){ return !!pfFlag(id); }
async function pfEnsureFlags(){
  try{ const r = await jget('/api/llama-flags'); PF_FLAGS = (r && r.flags) || []; }
  catch(_){ if(!PF_FLAGS) PF_FLAGS = []; }
  return PF_FLAGS;
}
function pfEmptyCaps(){
  return {native_ctx:0, n_layers:0, thinks:false, has_effort:false, effort:[], effort_default:'', mmproj:[], nextn:0, arch:''};
}
async function pfLoadCaps(model){
  PF_CAPS = pfEmptyCaps();
  if(!model) return PF_CAPS;
  try{
    const r = await jget('/api/model-caps?model='+encodeURIComponent(model));
    if(r && r.ok) PF_CAPS = {
      native_ctx:r.native_ctx|0, n_layers:r.n_layers|0, thinks:!!r.thinks,
      has_effort:!!r.has_effort, effort:r.effort||[], effort_default:r.effort_default||'',
      mmproj:r.mmproj||[], nextn:r.nextn|0, arch:r.arch||'', vision:!!r.vision
    };
  }catch(_){}
  if(typeof NAKED_NATIVE!=='undefined' && PF_CAPS.native_ctx) NAKED_NATIVE = PF_CAPS.native_ctx;
  return PF_CAPS;
}
async function pfLoadModels(){
  try{ PF_MODELS = await jget('/api/models') || []; }catch(_){ PF_MODELS = []; }
  return PF_MODELS;
}

function pfAdapterPanel(){
  return {
    read: (k)=> typeof pRead==='function' ? pRead(k) : '',
    write: (k,v)=>{ if(typeof pWrite==='function') pWrite(k,v); },
    eaGet: (f)=>{ if(typeof pEaTokens!=='function') return ''; const t=pEaTokens(), i=t.indexOf(f); return (i>=0 && i+1<t.length && !t[i+1].startsWith('-')) ? t[i+1] : ''; },
    eaHas: (f)=> typeof pEaTokens==='function' && pEaTokens().includes(f),
    eaSet: (f,v)=>{ if(typeof pEaVal==='function') pEaVal(f,v); },
    eaFlag: (f,on)=>{ if(typeof pEaFlag==='function') pEaFlag(f,on); },
    model: ()=> typeof SESSION!=='undefined' ? SESSION.model : '',
    changed: ()=>{ if(typeof scheduleParamsEstimate==='function') scheduleParamsEstimate(); }
  };
}
function pfAdapterPreset(){
  return {
    read: (k)=> typeof cfgReadKey==='function' ? cfgReadKey(k) : '',
    write: (k,v)=>{ if(typeof cfgWriteKey==='function') cfgWriteKey(k,v); },
    eaGet: (f)=> typeof eaGetValued==='function' ? eaGetValued(f) : '',
    eaHas: (f)=> typeof eaHasFlag==='function' ? eaHasFlag(f) : false,
    eaSet: (f,v)=>{ if(typeof eaSetValued==='function') eaSetValued(f,v); },
    eaFlag: (f,on)=>{ if(typeof eaToggleFlag==='function') eaToggleFlag(f,on); },
    model: ()=> typeof cfgReadKey==='function' ? cfgReadKey('MODEL') : '',
    changed: ()=>{ if(typeof schedulePresetEstimate==='function') schedulePresetEstimate(); }
  };
}

function pfGet(ad, flag){
  if(!flag) return '';
  if(flag.key){ const v = ad.read(flag.key); if(v) return v; }
  if(flag.kind==='bool'){
    if(ad.eaHas(flag.flag)) return 'on';
    if(ad.eaHas('--no-'+flag.id)) return 'off';
    return '';
  }
  return ad.eaGet(flag.flag);
}
function pfSet(ad, flag, val){
  val = String(val==null?'':val).trim();
  if(flag.key){ ad.write(flag.key, val); ad.eaSet(flag.flag, ''); ad.changed(); return; }
  if(flag.kind==='bool'){
    ad.eaFlag('--no-'+flag.id, false);
    ad.eaFlag(flag.flag, val==='on' || val==='1' || val==='true');
    ad.changed(); return;
  }
  ad.eaSet(flag.flag, val); ad.changed();
}

function pfTip(id, fallback){
  const t = (PF_META[id] && PF_META[id].tip) || fallback || '';
  if(!t) return '';
  return '<span class="help" tabindex="0">?<span class="tip">'+pfEsc(t)+'</span></span>';
}
function pfLabel(id, fallback){ return (PF_META[id] && PF_META[id].label) || fallback || id; }
function pfRowOpen(id, extra){ return '<div class="pf-row'+(extra?(' '+extra):'')+'" data-pf="'+pfEsc(id)+'">'; }
function pfLab(id, fallback, flag){ return '<div class="pf-lab"><span>'+pfEsc(pfLabel(id, fallback))+'</span>'+pfTip(id, flag && flag.help)+'</div>'; }
function pfSelectHTML(opts, val){
  let h='<span class="pe-selc"><select>';
  (opts||[]).forEach(o=>{ h+='<option value="'+pfEsc(o[0])+'"'+(String(o[0])===String(val)?' selected':'')+'>'+pfEsc(o[1]||o[0])+'</option>'; });
  return h+'</select></span>';
}
function pfToggleHTML(on){ return '<label class="pe-switch"><input type="checkbox"'+(on?' checked':'')+'><span class="pe-sl"></span></label>'; }
function pfSlideVal(val, placeholder){
  const shown = String(val==null?'':val);
  return '<input class="pf-val" type="text" inputmode="numeric" size="7" value="'+pfEsc(shown)+'" placeholder="'+pfEsc(placeholder||'')+'">';
}
function pfSlideHTML(val, min, max, placeholder, step){
  const raw = String(val==null?'':val).trim();
  const tout = /^(tout|all|999)$/i.test(raw);
  const n = parseFloat(raw);
  const hi = max>0 ? max : 1, lo = min;
  const clamped = tout ? hi : (isFinite(n) ? Math.min(hi, Math.max(lo, n)) : lo);
  const st = step || 1;
  return '<div class="pf-slide" data-min="'+lo+'" data-max="'+hi+'"><input class="pf-range" type="range" min="'+lo+'" max="'+hi+'" step="'+st+'" value="'+clamped+'"></div>';
}

function pfKvRead(ad){ const base=ad.read('KV_TYPE'); const k=ad.read('KV_TYPE_K')||base; const v=ad.read('KV_TYPE_V')||base; return (k||v)?(k+'|'+v):''; }
function pfKvWrite(ad, val){ const p=String(val||'').split('|'); ad.write('KV_TYPE',''); ad.write('KV_TYPE_K', p[0]||''); ad.write('KV_TYPE_V', p[1]||''); ad.changed(); }

function pfReasonOn(ad){
  const rz = ad.read('REASONING');
  if(/^(off|none|false|0)$/i.test(rz)) return false;
  if(/^(on|1|true|auto|deepseek)$/i.test(rz)) return true;
  return !!PF_CAPS.thinks;
}
function pfReasonWrite(ad, on){
  if(!on){ ad.write('REASONING','off'); ad.changed(); return; }
  const cur = ad.read('REASONING');
  if(/^(on|auto|deepseek)$/i.test(cur)){ ad.changed(); return; }
  if(PF_CAPS.thinks) ad.write('REASONING','');
  else ad.write('REASONING','on');
  ad.changed();
}

function pfVisionCapable(){ return !!(PF_CAPS.mmproj && PF_CAPS.mmproj.length); }
function pfVisionForced(ad){ return !!(ad.read('MMPROJ') || ad.eaGet('--mmproj') || ad.eaHas('--mmproj-auto')); }
function pfShowVision(ad){ return pfVisionCapable() || pfVisionForced(ad); }
function pfVisionOn(ad){
  if(ad.eaHas('--no-mmproj')) return false;
  if(pfVisionForced(ad)) return true;
  return pfVisionCapable();
}
function pfVisionWrite(ad, on, file){
  if(!on){
    ad.write('MMPROJ',''); ad.eaSet('--mmproj',''); ad.eaFlag('--mmproj-auto', false);
    if(pfHas('mmproj-auto')) ad.eaFlag('--no-mmproj', true);
    ad.changed(); return;
  }
  ad.eaFlag('--no-mmproj', false);
  if(!file && PF_CAPS.mmproj && PF_CAPS.mmproj.length===1) file = PF_CAPS.mmproj[0];
  if(file){ ad.write('MMPROJ', file); ad.eaSet('--mmproj',''); ad.eaFlag('--mmproj-auto', false); }
  else if(pfHas('mmproj-auto')){ ad.write('MMPROJ',''); ad.eaFlag('--mmproj-auto', true); }
  ad.changed();
}
function pfFmtTok(n){
  n = Number(n)||0;
  if(typeof fmtTok==='function') return fmtTok(n);
  if(n>=1000000) return (n/1000000).toFixed(1).replace(/\.0$/,'')+'M';
  if(n>=1000) return Math.round(n/1000)+'K';
  return String(n);
}
function pfHasEffort(){
  return !!(PF_CAPS.has_effort || (PF_CAPS.effort && PF_CAPS.effort.length));
}
function pfEffortOpts(ad){
  const opts=[], seen={};
  const add=(v,l)=>{ v=String(v==null?'':v); if(seen[v]) return; seen[v]=1; opts.push([v, l||v||'défaut']); };
  const def = PF_CAPS.effort_default || '';
  add('', def ? ('défaut ('+def+')') : 'défaut');
  (PF_CAPS.effort || []).forEach(c=> add(c,c));
  if(!(PF_CAPS.effort && PF_CAPS.effort.length) && PF_CAPS.has_effort){
    const f=pfFlag('reasoning-effort');
    (f && f.choices || []).forEach(c=>{ if(!/^(default)?$/i.test(c)) add(c,c); });
  }
  const cur = ad && ad.read ? ad.read('REASONING_EFFORT') : '';
  if(cur) add(cur, cur);
  return opts;
}
function pfSpecOpts(){
  const opts=[], seen={};
  const add=(v,l)=>{ v=String(v==null?'':v); if(v==='none') v=''; if(seen[v]) return; seen[v]=1; opts.push([v, l||v||'non']); };
  PF_SPEC_OPTS.forEach(o=> add(o[0], o[1]));
  const f=pfFlag('spec-type');
  (f && f.choices || []).forEach(c=> add(c,c));
  return opts;
}
function pfEnumOpts(f){
  const opts=[['','(défaut)']];
  ((f && f.choices) || []).forEach(c=> opts.push([c,c]));
  if(opts.length===1) ['on','off','auto'].forEach(c=> opts.push([c,c]));
  return opts;
}
function pfMmprojOpts(ad){
  const seen={}, files=[];
  const add=p=>{ if(p && !seen[p]){ seen[p]=1; files.push(p); } };
  (PF_CAPS.mmproj||[]).forEach(add);
  const cur = ad && (ad.read('MMPROJ') || ad.eaGet('--mmproj'));
  if(cur) add(cur);
  if(files.length<=1) return files.map(p=>[p, String(p).split(/[\\/]/).pop()]);
  return [['','auto']].concat(files.map(p=>[p, String(p).split(/[\\/]/).pop()]));
}

function pfShowReasoning(ad){
  return !!PF_CAPS.thinks || !!(ad.read('REASONING'));
}
function pfBaseHTML(ad){
  const native = PF_CAPS.native_ctx || 8192;
  const ctx = ad.read('CTX') || String(native);
  const nglRaw = ad.read('NGL');
  const ngl = (!nglRaw || nglRaw==='999') ? 'tout' : nglRaw;
  const nMax = Math.max(1, PF_CAPS.n_layers||80);
  const visOn = pfVisionOn(ad);
  const visFile = ad.read('MMPROJ') || ad.eaGet('--mmproj') || '';
  const rzOn = pfReasonOn(ad);
  const temp = ad.read('TEMP');
  let h = '<div class="pe-group pf-tier"><div class="pe-gh">Essentiels</div><div class="pe-list pf-list">';
  h += pfRowOpen('ctx-size','pf-stack')+'<div class="pf-head">'+pfLab('ctx-size')+pfSlideVal(ctx,'natif')+'</div>'+pfSlideHTML(ctx, 1024, native, 'natif', 1024)+'<div class="pf-slide-note">natif '+pfEsc(pfFmtTok(native))+' · tape la valeur pour dépasser</div></div>';
  h += pfRowOpen('gpu-layers','pf-stack')+'<div class="pf-head">'+pfLab('gpu-layers')+pfSlideVal(ngl,'tout')+'</div>'+pfSlideHTML(ngl, 0, nMax, 'tout', 1)+'</div>';
  if(pfShowVision(ad)){
    const mmOpts = pfMmprojOpts(ad);
    const showPick = visOn && mmOpts.length>1;
    h += pfRowOpen('vision')+pfLab('mmproj')+'<div class="pf-ctl pf-vision">'+pfToggleHTML(visOn)+(showPick?pfSelectHTML(mmOpts, visFile):'')+'</div></div>';
  }
  if(pfShowReasoning(ad)){
    h += pfRowOpen('reasoning')+pfLab('reasoning')+'<div class="pf-ctl">'+pfToggleHTML(rzOn)+'</div></div>';
    if(rzOn && pfHasEffort()) h += pfRowOpen('reasoning-effort')+pfLab('reasoning-effort')+'<div class="pf-ctl">'+pfSelectHTML(pfEffortOpts(ad), ad.read('REASONING_EFFORT'))+'</div></div>';
  }
  h += pfRowOpen('temp')+pfLab('temp')+'<div class="pf-ctl"><input class="pe-val" data-pf-num="temp" type="number" min="0" step="0.05" value="'+pfEsc(temp)+'" placeholder="—"></div></div>';
  const gsys = ((document.getElementById('sysprompt')||{}).value||'').trim();
  const ph = gsys ? 'Vide = prompt des Réglages' : 'Vide = aucun prompt';
  h += pfRowOpen('sysprompt','pf-stack')+'<div class="pf-head">'+pfLab('sysprompt')+'</div><textarea class="pf-sys" placeholder="'+pfEsc(ph)+'">'+pfEsc(ad.read('SYSPROMPT')||'')+'</textarea><div class="pf-slide-note">'+(gsys?'Prioritaire sur le prompt global.':'Aucun prompt global. Saisir ici pour ce modèle seulement.')+'</div></div>';
  return h+'</div></div>';
}

function pfGenericRow(ad, f){
  const val = pfGet(ad, f);
  let ctl='';
  if(f.kind==='bool'){
    const no = ad.eaHas('--no-'+f.id);
    const has = ad.eaHas(f.flag);
    const defOn = /^(enabled|on|true|yes)$/i.test(f.default||'');
    ctl = pfToggleHTML(no ? false : (has || (!has && defOn)));
  } else if(f.kind==='enum' && f.choices && f.choices.length){
    ctl = pfSelectHTML([['','(défaut)']].concat(f.choices.map(c=>[c,c])), val);
  } else if(f.kind==='int' || f.kind==='float'){
    ctl = '<input class="pe-val" data-pf-num="'+pfEsc(f.id)+'" type="number" step="'+(f.kind==='float'?'any':'1')+'" value="'+pfEsc(val)+'" placeholder="'+pfEsc(f.default||'—')+'">';
  } else {
    ctl = '<input class="pe-val pf-str" data-pf-str="'+pfEsc(f.id)+'" type="text" value="'+pfEsc(val)+'" placeholder="'+pfEsc(f.default||'')+'">';
  }
  return pfRowOpen(f.id)+pfLab(f.id, f.flag, f)+'<div class="pf-ctl">'+ctl+'</div></div>';
}

function pfAdvHTML(ad){
  let h = '<details class="pf-more"><summary>Paramètres avancés</summary><div class="pf-list">';
  h += pfRowOpen('kv')+pfLab('cache-type-k')+'<div class="pf-ctl">'+pfSelectHTML(PF_KV_OPTS, pfKvRead(ad))+'</div></div>';
  {
    const np = ad.read('NP') || ad.eaGet('-np') || ad.eaGet('--parallel') || '1';
    h += pfRowOpen('parallel')+pfLab('parallel')+'<div class="pf-ctl"><input class="pe-val" data-pf-num="parallel" type="number" min="1" step="1" value="'+pfEsc(np)+'" placeholder="1"></div></div>';
  }
  {
    const spec = ad.eaGet('--spec-type');
    h += pfRowOpen('spec-type')+pfLab('spec-type')+'<div class="pf-ctl">'+pfSelectHTML(pfSpecOpts(), spec)+'</div></div>';
    if(spec && spec.indexOf('draft-')===0){
      const drafts = [['','(intégré / aucun)']];
      (PF_MODELS||[]).filter(m=> typeof isWeightModel==='function' ? isWeightModel(m) : !/mmproj/i.test(m.name||'')).forEach(m=> drafts.push([m.value||m.path, m.name]));
      h += pfRowOpen('model-draft')+pfLab('model-draft')+'<div class="pf-ctl">'+pfSelectHTML(drafts, ad.read('MODEL_DRAFT')||ad.eaGet('--model-draft')||ad.eaGet('--spec-draft-model'))+'</div></div>';
      h += pfRowOpen('spec-n')+pfLab('spec-draft-n-max')+'<div class="pf-ctl"><input class="pe-val" data-pf-num="spec-n" type="number" min="1" step="1" value="'+pfEsc(ad.eaGet('--spec-draft-n-max'))+'" placeholder="3"></div></div>';
    }
  }
  ['batch-size','ubatch-size','threads','threads-batch','n-cpu-moe'].forEach(id=>{
    const f=pfFlag(id); if(!f) return;
    h += pfRowOpen(id)+pfLab(id)+'<div class="pf-ctl"><input class="pe-val" data-pf-num="'+pfEsc(id)+'" type="number" step="1" value="'+pfEsc(pfGet(ad,f))+'" placeholder="—"></div></div>';
  });
  if(pfHas('flash-attn')){
    const f=pfFlag('flash-attn');
    h += pfRowOpen('flash-attn')+pfLab('flash-attn')+'<div class="pf-ctl">'+pfSelectHTML(pfEnumOpts(f), pfGet(ad,f))+'</div></div>';
  }
  if(pfHas('fit')){
    const raw=pfGet(ad, pfFlag('fit'));
    h += pfRowOpen('fit')+pfLab('fit')+'<div class="pf-ctl">'+pfToggleHTML(!/^off$/i.test(raw||''))+'</div></div>';
  }
  if(pfHas('load-mode')){
    const f=pfFlag('load-mode'); const opts=(f.choices&&f.choices.length?f.choices:['auto','mmap','mlock','dio']).map(c=>[c,c]); opts.unshift(['','auto']);
    h += pfRowOpen('load-mode')+pfLab('load-mode')+'<div class="pf-ctl">'+pfSelectHTML(opts, pfGet(ad,f))+'</div></div>';
  } else {
    if(pfHas('mlock')) h += pfRowOpen('mlock')+pfLab('mlock')+'<div class="pf-ctl">'+pfToggleHTML(ad.eaHas('--mlock'))+'</div></div>';
    if(pfHas('mmap')) h += pfRowOpen('mmap')+pfLab('mmap')+'<div class="pf-ctl">'+pfToggleHTML(ad.eaHas('--no-mmap'))+'</div></div>';
  }
  if(pfHas('kv-unified')) h += pfRowOpen('kv-unified')+pfLab('kv-unified')+'<div class="pf-ctl">'+pfToggleHTML(ad.eaHas('--kv-unified'))+'</div></div>';
  if(pfHas('jinja')) h += pfRowOpen('jinja')+pfLab('jinja')+'<div class="pf-ctl">'+pfToggleHTML(ad.eaHas('--jinja'))+'</div></div>';
  ['top-p','top-k','min-p','presence-penalty','repeat-penalty'].forEach(id=>{
    const f=pfFlag(id); if(!f) return;
    h += pfRowOpen(id)+pfLab(id)+'<div class="pf-ctl"><input class="pe-val" data-pf-num="'+pfEsc(id)+'" type="number" step="any" value="'+pfEsc(pfGet(ad,f))+'" placeholder="—"></div></div>';
  });
  (PF_FLAGS||[]).filter(f=>f.tier==='advanced' && !PF_ADV_SKIP[f.id]).forEach(f=>{ h += pfGenericRow(ad, f); });
  return h+'</div></details>';
}

function pfExpertHTML(ad){
  const groups={};
  (PF_FLAGS||[]).filter(f=>f.tier==='expert' && !f.deprecated).forEach(f=>{ (groups[f.group||'autres']=groups[f.group||'autres']||[]).push(f); });
  const dep=(PF_FLAGS||[]).filter(f=>f.tier==='expert' && f.deprecated);
  let h='<details class="pf-more pf-expert"><summary>Expert — tous les drapeaux llama.cpp</summary><input class="pf-filter" type="search" placeholder="filtrer un drapeau…"><div class="pf-list">';
  Object.keys(groups).forEach(g=>{
    h += '<div class="pf-gh">'+pfEsc(g)+'</div>';
    groups[g].forEach(f=> h += pfGenericRow(ad, f));
  });
  if(dep.length){
    h += '<details class="pf-dep"><summary>Obsolètes</summary>';
    dep.forEach(f=> h += pfGenericRow(ad, f));
    h += '</details>';
  }
  return h+'</div></details>';
}

function pfSyncSlide(slide){
  const range=slide.querySelector('.pf-range');
  const val=slide.closest('.pf-row') && slide.closest('.pf-row').querySelector('.pf-val');
  if(!range) return;
  const min=Number(range.min), max=Number(range.max);
  const raw=val ? String(val.value).trim() : range.value;
  let n=parseFloat(raw);
  if(!isFinite(n)) n = /^(tout|all|999)$/i.test(raw) ? max : min;
  const clamped = Math.min(max, Math.max(min, n));
  range.value = String(clamped);
  const pct = max===min ? 0 : (clamped-min)/(max-min);
  slide.style.setProperty('--pct', (pct*100)+'%');
  if(val) val.classList.toggle('over', isFinite(parseFloat(raw)) && parseFloat(raw)>max);
}

function pfBind(host, ad){
  host.querySelectorAll('.pf-slide').forEach(slide=>{
    const range=slide.querySelector('.pf-range');
    const row=slide.closest('.pf-row');
    const val=row && row.querySelector('.pf-val');
    const id=row && row.getAttribute('data-pf');
    const apply=(fromRange)=>{
      if(!val) return;
      if(fromRange){
        val.value = (id==='gpu-layers' && Number(range.value)>=Number(range.max)) ? 'tout' : range.value;
      } else if(id==='gpu-layers' && /^tout|all|999$/i.test(String(val.value).trim())){
        range.value = range.max;
      } else {
        const n=parseFloat(val.value); if(isFinite(n)) range.value=Math.min(Number(range.max), Math.max(Number(range.min), n));
      }
      pfSyncSlide(slide);
      if(id==='ctx-size'){ const n=parseInt(val.value,10); ad.write('CTX', isFinite(n)&&n>0?String(n):''); }
      else if(id==='gpu-layers'){ ad.write('NGL', /^tout|all$/i.test(String(val.value).trim()) ? '999' : String(val.value).trim()); }
      ad.changed();
    };
    range.addEventListener('input', ()=>apply(true));
    if(val){
      val.addEventListener('input', ()=>apply(false));
      val.addEventListener('focus', ()=>val.select());
    }
    pfSyncSlide(slide);
  });
  host.querySelectorAll('.pf-row').forEach(row=>{
    const id=row.getAttribute('data-pf');
    const toggle=row.querySelector('.pe-switch input');
    const sel=row.querySelector('select');
    const num=row.querySelector('[data-pf-num]');
    const str=row.querySelector('[data-pf-str]');
    if(id==='reasoning' && toggle) toggle.addEventListener('change', ()=>{ pfReasonWrite(ad, toggle.checked); pfMount(host, ad); });
    else if(id==='vision'){
      if(toggle) toggle.addEventListener('change', ()=>{ pfVisionWrite(ad, toggle.checked, row.querySelector('select')?row.querySelector('select').value:''); pfMount(host, ad); });
      if(sel) sel.addEventListener('change', ()=> pfVisionWrite(ad, true, sel.value));
    }
    else if(id==='reasoning-effort' && sel) sel.addEventListener('change', ()=>{ ad.write('REASONING_EFFORT', sel.value); if(sel.value) ad.eaFlag('--jinja', true); ad.changed(); });
    else if(id==='sysprompt'){
      const ta=row.querySelector('textarea.pf-sys');
      if(ta) ta.addEventListener('input', ()=>{ ad.write('SYSPROMPT', ta.value); ad.changed(); if(host.id==='params-form') scheduleLiveSys(ta.value); });
    }
    else if(id==='kv' && sel) sel.addEventListener('change', ()=> pfKvWrite(ad, sel.value));
    else if(id==='spec-type' && sel) sel.addEventListener('change', ()=>{ ad.eaSet('--spec-type', sel.value); if(!sel.value || sel.value.indexOf('draft-')!==0){ ad.write('MODEL_DRAFT',''); ad.eaSet('--model-draft',''); ad.eaSet('--spec-draft-n-max',''); } ad.changed(); pfMount(host, ad); });
    else if(id==='model-draft' && sel) sel.addEventListener('change', ()=>{ ad.write('MODEL_DRAFT', sel.value); ad.eaSet('--model-draft',''); ad.changed(); });
    else if(id==='flash-attn' && sel) sel.addEventListener('change', ()=>{ const f=pfFlag('flash-attn'); if(f) pfSet(ad,f, sel.value); });
    else if(id==='fit' && toggle) toggle.addEventListener('change', ()=>{ const f=pfFlag('fit'); if(f) pfSet(ad,f, toggle.checked?'':'off'); else ad.eaSet('--fit', toggle.checked?'':'off'); ad.changed(); });
    else if(id==='mlock' && toggle) toggle.addEventListener('change', ()=>{ ad.eaFlag('--mlock', toggle.checked); ad.changed(); });
    else if(id==='mmap' && toggle) toggle.addEventListener('change', ()=>{ ad.eaFlag('--no-mmap', toggle.checked); ad.changed(); });
    else if(id==='kv-unified' && toggle) toggle.addEventListener('change', ()=>{ ad.eaFlag('--kv-unified', toggle.checked); ad.changed(); });
    else if(id==='jinja' && toggle) toggle.addEventListener('change', ()=>{ ad.eaFlag('--no-jinja', false); ad.eaFlag('--jinja', toggle.checked); ad.changed(); });
    else if(id==='load-mode' && sel) sel.addEventListener('change', ()=>{ const f=pfFlag('load-mode'); if(f) pfSet(ad,f,sel.value); });
    else if(toggle) toggle.addEventListener('change', ()=>{ const f=pfFlag(id); if(!f) return; if(toggle.checked) pfSet(ad,f,'on'); else { ad.eaFlag(f.flag,false); if(f.kind==='bool') ad.eaFlag('--no-'+f.id, true); ad.changed(); } });
    else if(sel) sel.addEventListener('change', ()=>{ const f=pfFlag(id); if(f) pfSet(ad,f,sel.value); });
    if(num) num.addEventListener('input', ()=>{
      if(id==='parallel'){ ad.write('NP', num.value); ad.eaSet('-np',''); ad.eaSet('--parallel',''); ad.changed(); return; }
      if(id==='spec-n'){ ad.eaSet('--spec-draft-n-max', num.value); ad.changed(); return; }
      if(id==='temp' || id==='temperature'){ ad.write('TEMP', num.value); ad.changed(); return; }
      const f=pfFlag(id); if(f) pfSet(ad,f,num.value);
    });
    if(str) str.addEventListener('input', ()=>{ const f=pfFlag(id); if(f) pfSet(ad,f,str.value); });
  });
  const filter=host.querySelector('.pf-filter');
  if(filter) filter.addEventListener('input', ()=>{
    const q=filter.value.trim().toLowerCase();
    host.querySelectorAll('.pf-expert .pf-row').forEach(row=>{ row.hidden = !!(q && (row.getAttribute('data-pf')+' '+row.textContent).toLowerCase().indexOf(q)<0); });
  });
}

async function pfMount(host, ad){
  if(typeof host==='string') host=document.getElementById(host);
  if(!host) return;
  const seq = ++pfMountSeq;
  const paint = ()=>{
    if(seq !== pfMountSeq || !host.isConnected) return;
    const keepOpen={}; host.querySelectorAll('details.pf-more').forEach((d,i)=>{ keepOpen[i]=d.open; });
    const filt=host.querySelector('.pf-filter'); const filtVal=filt?filt.value:'';
    host.innerHTML = pfBaseHTML(ad)+pfAdvHTML(ad)+pfExpertHTML(ad);
    host.querySelectorAll('details.pf-more').forEach((d,i)=>{ if(keepOpen[i]) d.open=true; });
    const filt2=host.querySelector('.pf-filter'); if(filt2 && filtVal){ filt2.value=filtVal; filt2.dispatchEvent(new Event('input')); }
    pfBind(host, ad);
    if(host.id==='params-form' && typeof scheduleParamsEstimate==='function') scheduleParamsEstimate();
    if(host.id==='preset-form' && typeof schedulePresetEstimate==='function') schedulePresetEstimate();
  };
  try{
    const model = ad.model && ad.model();
    if(model) await pfLoadCaps(model);
    if(seq !== pfMountSeq) return;
    paint();
    const hadFlags = !!(PF_FLAGS && PF_FLAGS.length);
    Promise.all([pfEnsureFlags(), pfLoadModels()]).then(()=>{
      if(seq !== pfMountSeq) return;
      if(!hadFlags) paint();
    }).catch(e=> console.error(e));
  }catch(e){
    console.error(e);
    paint();
  }
}
async function renderPanelParams(){ const host=document.getElementById('params-form'); if(host) await pfMount(host, pfAdapterPanel()); }
async function renderPresetParams(){ const host=document.getElementById('preset-form'); if(host) await pfMount(host, pfAdapterPreset()); }
function pfFlush(which){
  const ad = which==='preset' ? pfAdapterPreset() : pfAdapterPanel();
  const host = document.getElementById(which==='preset'?'preset-form':'params-form');
  if(!host) return;
  const rz = host.querySelector('.pf-row[data-pf="reasoning"] .pe-switch input');
  if(rz) pfReasonWrite(ad, rz.checked);
}
