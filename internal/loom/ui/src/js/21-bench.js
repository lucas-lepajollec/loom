// Page Bench : test brut + prompts enregistrés, lancés en file de GGUF.

let BENCH = {tests:[], test:'perf', models:[], presets:[], pick:{}, job:null, poll:null, form:false};

function benchItemKey(it){
  return it.kind==='preset' ? 'p:'+it.id : 'm:'+(it.path||it.model||it.name);
}

function openBench(){
  if(typeof showMainView === 'function') showMainView('bench');
  benchLoad();
}
function benchViewOpen(){
  const v = document.getElementById('bench-view');
  return !!(v && !v.hidden);
}
function benchSyncPoll(){
  if(BENCH.poll){ clearInterval(BENCH.poll); BENCH.poll = null; }
  if(!benchViewOpen()) return;
  benchRefreshJob();
  BENCH.poll = setInterval(benchRefreshJob, 1200);
}
function benchFmt(n, d){
  n = Number(n);
  if(!isFinite(n) || n <= 0) return '—';
  const x = n >= 100 ? n.toFixed(0) : n.toFixed(d==null?1:d);
  return String(x).replace('.', ',');
}
function benchTestOf(id){
  return (BENCH.tests||[]).find(t=>t.id===id) || BENCH.tests[0] || {id:'perf', name:'Perfs brutes', kind:'perf', builtin:true};
}

async function benchLoad(){
  let tests=[], models=[], presets=[];
  try{ const r = await jget('/api/bench/tests'); tests = (r && r.tests) || []; }catch(_){}
  try{ models = await jget('/api/models') || []; }catch(_){}
  try{ presets = await jget('/api/presets') || []; }catch(_){}
  BENCH.tests = tests;
  BENCH.models = (models||[]).filter(m=> typeof isWeightModel==='function' ? isWeightModel(m) : !/mmproj/i.test(m.name||''));
  BENCH.presets = Array.isArray(presets) ? presets : [];
  if(!(BENCH.tests||[]).some(t=>t.id===BENCH.test)) BENCH.test = (BENCH.tests[0]&&BENCH.tests[0].id) || 'perf';
  benchPaintTests();
  benchPaintModels();
  benchRefreshJob();
}

function benchPaintTests(){
  const box = document.getElementById('bench-tests');
  if(!box) return;
  box.innerHTML = '';
  (BENCH.tests||[]).forEach(t=>{
    const b = document.createElement('button');
    b.type = 'button';
    b.className = 'bench-chip'+(BENCH.test===t.id?' on':'');
    b.textContent = t.name;
    b.onclick = ()=>{ BENCH.test = t.id; BENCH.form = false; benchPaintTests(); };
    const wrap = document.createElement('span');
    wrap.style.display = 'inline-flex';
    wrap.style.alignItems = 'center';
    wrap.style.gap = '2px';
    wrap.appendChild(b);
    if(!t.builtin){
      const x = document.createElement('button');
      x.type = 'button';
      x.className = 'bench-chip-x';
      x.title = 'Supprimer';
      x.setAttribute('aria-label', 'Supprimer '+t.name);
      x.textContent = '×';
      x.onclick = (e)=>{ e.stopPropagation(); benchDeleteTest(t.id, t.name); };
      wrap.appendChild(x);
    }
    box.appendChild(wrap);
  });
  const add = document.createElement('button');
  add.type = 'button';
  add.className = 'bench-chip';
  add.textContent = '+ Nouveau';
  add.onclick = ()=>benchToggleNew(true);
  box.appendChild(add);
  const form = document.getElementById('bench-new');
  if(form) form.hidden = !BENCH.form;
  const note = document.getElementById('bench-test-note');
  if(note){
    const t = benchTestOf(BENCH.test);
    if(t.kind==='perf' || t.id==='perf') note.textContent = 'Corpus fixe (~2000 tok) puis 300 tokens de decode. Prefill et decode en tok/s.';
    else note.textContent = t.prompt || '';
  }
}
function benchToggleNew(on){
  BENCH.form = !!on;
  const form = document.getElementById('bench-new');
  if(form) form.hidden = !BENCH.form;
  if(on){
    const n = document.getElementById('bench-new-name');
    if(n) n.focus();
  }
}
function benchItems(){
  const presets = (BENCH.presets||[]).map(p=>({
    kind:'preset', id:p.id, name:p.name||p.id,
    model:p.model||'', sub: p.model ? String(p.model).split(/[\\/]/).pop() : 'preset'
  }));
  const models = (BENCH.models||[]).map(m=>({
    kind:'model', path:m.path||m.value||m.name, name:m.name,
    sub: (typeof fmtSize==='function' && m.size) ? fmtSize(m.size) : ''
  }));
  return {presets, models};
}
function benchAddRow(box, it){
  const key = benchItemKey(it);
  const on = !!BENCH.pick[key];
  const b = document.createElement('button');
  b.type = 'button';
  b.className = 'bench-model'+(on?' on':'');
  b.innerHTML = '<span class="bench-model-k" aria-hidden="true"></span><span class="bench-model-n"></span><span class="bench-model-s"></span>';
  b.querySelector('.bench-model-n').textContent = it.name;
  b.querySelector('.bench-model-s').textContent = it.sub || '';
  b.onclick = ()=>{ BENCH.pick[key] = !BENCH.pick[key]; benchPaintModels(); };
  box.appendChild(b);
}
function benchPaintModels(){
  const box = document.getElementById('bench-models');
  if(!box) return;
  box.innerHTML = '';
  const {presets, models} = benchItems();
  if(!presets.length && !models.length){
    const empty = document.createElement('div');
    empty.className = 'srv-empty';
    empty.textContent = 'Aucun preset ni GGUF à tester.';
    box.appendChild(empty);
    return;
  }
  if(presets.length){
    const g = document.createElement('div');
    g.className = 'bench-g';
    g.textContent = 'Presets';
    box.appendChild(g);
    presets.forEach(it=>benchAddRow(box, it));
  }
  if(models.length){
    const g = document.createElement('div');
    g.className = 'bench-g';
    g.textContent = 'Modèles';
    box.appendChild(g);
    models.forEach(it=>benchAddRow(box, it));
  }
}
function benchPickAll(on){
  const {presets, models} = benchItems();
  presets.concat(models).forEach(it=>{ BENCH.pick[benchItemKey(it)] = !!on; });
  benchPaintModels();
}
function benchSelected(){
  const {presets, models} = benchItems();
  const out = [];
  presets.forEach(it=>{
    if(BENCH.pick[benchItemKey(it)]) out.push({preset: it.id, name: it.name, model: it.model||''});
  });
  models.forEach(it=>{
    if(BENCH.pick[benchItemKey(it)]) out.push({model: it.path, name: it.name});
  });
  return out;
}

async function benchSaveTest(){
  const name = ((document.getElementById('bench-new-name')||{}).value||'').trim();
  const prompt = ((document.getElementById('bench-new-prompt')||{}).value||'').trim();
  const n = parseInt((document.getElementById('bench-new-n')||{}).value,10)||256;
  if(!name || !prompt){ toast('nom et prompt requis'); return; }
  const r = await jpost('/api/bench/tests', {name, prompt, max_tokens:n});
  if(!r.ok){ toast(r.error||'impossible'); return; }
  BENCH.tests = r.tests || BENCH.tests;
  BENCH.test = (r.test && r.test.id) || BENCH.test;
  BENCH.form = false;
  const nameEl = document.getElementById('bench-new-name');
  const pEl = document.getElementById('bench-new-prompt');
  if(nameEl) nameEl.value = '';
  if(pEl) pEl.value = '';
  benchPaintTests();
  toast('test enregistré');
}
async function benchDeleteTest(id, name){
  if(!await askConfirm('Supprimer « '+(name||'ce test')+' » ?', {title:'Supprimer le test', okText:'Supprimer', danger:true})) return;
  const r = await jpost('/api/bench/tests/delete', {id});
  if(!r.ok){ toast(r.error||'impossible'); return; }
  BENCH.tests = r.tests || [];
  if(BENCH.test===id) BENCH.test = 'perf';
  benchPaintTests();
}

async function benchStart(){
  const models = benchSelected();
  if(!models.length){ toast('coche au moins un modèle ou un preset'); return; }
  const r = await jpost('/api/bench/queue', {test_id: BENCH.test, models});
  if(!r.ok){ toast(r.error||'impossible'); return; }
  BENCH.job = r.job;
  benchPaintJob();
  benchSyncPoll();
}
async function benchCancel(){
  const r = await jpost('/api/bench/queue/cancel', {});
  if(r && r.job) BENCH.job = r.job;
  benchPaintJob();
}
async function benchRefreshJob(){
  if(!benchViewOpen()) return;
  try{
    const r = await jget('/api/bench/queue');
    if(r && r.job) BENCH.job = r.job;
  }catch(_){}
  benchPaintJob();
}
function benchPaintJob(){
  const j = BENCH.job;
  const run = document.getElementById('bench-run');
  const stop = document.getElementById('bench-cancel');
  const sub = document.getElementById('bench-head-sub');
  const busy = !!(j && j.status==='running');
  if(run) run.disabled = busy;
  if(stop) stop.hidden = !busy;
  if(sub){
    if(busy){
      const row = (j.rows||[])[j.index] || {};
      sub.textContent = (j.test_name||'Test')+' · '+(row.name||'…')+' ('+((j.index|0)+1)+'/'+(j.rows||[]).length+')';
    } else if(j && j.status==='done') sub.textContent = (j.test_name||'Test')+' · file terminée';
    else if(j && j.status==='cancel') sub.textContent = (j.test_name||'Test')+' · file arrêtée';
    else sub.textContent = 'Compare les GGUF et les presets en file, un chargement à la fois.';
  }
  const box = document.getElementById('bench-results');
  if(!box) return;
  if(!j || !(j.rows||[]).length){
    box.innerHTML = '<div class="srv-empty">Coche des modèles ou presets, choisis un test, lance la file. Chaque entrée est chargée puis mesurée.</div>';
    return;
  }
  const h = '<div class="bench-row bench-row-h"><span>Modèle</span><span>Prefill</span><span>Decode</span><span>Durée</span></div>';
  const rows = (j.rows||[]).map(row=>{
    const st = row.status||'pending';
    const lab = {pending:'en attente', loading:'chargement…', running:'mesure…', ok:'', err: row.error||'erreur', skip:'passé'}[st] || st;
    const r = row.result || {};
    const pre = st==='ok' ? benchFmt(r.prompt_per_second)+' t/s' : '—';
    const dec = st==='ok' ? benchFmt(r.predicted_per_second)+' t/s' : '—';
    const el = st==='ok' ? benchFmt(r.elapsed_sec)+' s' : lab;
    let html = '<div class="bench-row">'
      + '<div><div class="bench-row-n">'+escHtml(row.name||row.model)+'</div>'
      + (lab && st!=='ok' ? '<div class="bench-row-m">'+escHtml(lab)+'</div>' : '')
      + '</div>'
      + '<div class="bench-row-v">'+escHtml(pre)+'</div>'
      + '<div class="bench-row-v">'+escHtml(dec)+'</div>'
      + '<div class="bench-row-v">'+escHtml(el)+'</div>';
    if(row.preview) html += '<div class="bench-prev">'+escHtml(row.preview)+'</div>';
    return html+'</div>';
  }).join('');
  box.innerHTML = h+rows;
}
