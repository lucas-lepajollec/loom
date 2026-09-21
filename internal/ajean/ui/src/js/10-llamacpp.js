// Backend llama.cpp — Réglages → Moteur.
//   Lier un llama-server déjà là, ou installer :
//     - llama-server seulement (binaire officiel)
//     - llama.cpp complet (clone + compile)
//   Un serveur seul peut plus tard « Ajouter llama.cpp ».
let lcState = null, lcPoll = null, lcLogNext = 0;
let lcSetupForced = false;

async function loadLlamacpp(){
  let s;
  try{ s = await jget('/api/llamacpp'); }catch(_){ return; }
  lcState = s;
  lcRenderEngine(s);

  if(s.job && s.job.exists && s.job.running && !lcPoll){
    const det = document.getElementById('lc-details');
    if(det) det.open = true;
    lcStartPolling();
  } else if(s.job && s.job.exists && !s.job.running && !lcPoll && s.job.error && !lcEndShown){
    const det = document.getElementById('lc-details');
    if(det) det.open = true;
    document.getElementById('lc-job').style.display = '';
    lcLogNext = 0;
    document.getElementById('lc-log').textContent = '';
    lcEndShown = true;
    lcPollJob(true);
  }
  lcChipSync(s.job);
  if(typeof prefetchGpuDevices === 'function') prefetchGpuDevices(s.config_bin);
}

function lcEsc(t){
  return String(t==null?'':t).replace(/[<>&]/g, c => ({'<':'&lt;','>':'&gt;','&':'&amp;'}[c]));
}

function lcRenderEngine(s){
  const linked = document.getElementById('lc-linked');
  const setup = document.getElementById('lc-setup');
  if(!linked || !setup) return;

  const has = !!(s && s.linked && s.config_bin);
  const showSetup = !has || lcSetupForced;
  linked.hidden = !has;
  setup.hidden = !showSetup;

  if(has){
    const kindEl = document.getElementById('lc-kind');
    const descEl = document.getElementById('lc-kind-d');
    const pathEl = document.getElementById('lc-bin-path');
    const metaEl = document.getElementById('lc-meta');
    const upg = document.getElementById('lc-btn-upgrade');
    const full = s.kind === 'full';
    if(kindEl) kindEl.textContent = full ? 'llama.cpp complet' : 'llama-server';
    if(descEl) descEl.textContent = full
      ? 'Projet complet (sources + binaire). Loom le pilote ; llama-server se lance aussi sans Loom.'
      : 'Binaire seulement. Suffit pour chatter. llama.cpp complet peut être ajouté plus tard.';
    if(pathEl){ pathEl.textContent = s.config_bin; pathEl.title = s.config_bin; }
    const bits = [];
    const gpu = {cuda:'NVIDIA CUDA', hip:'AMD ROCm', metal:'Apple Metal', vulkan:'Vulkan', cpu:'CPU'};
    if(s.plan && s.plan.backend) bits.push(gpu[s.plan.backend] || String(s.plan.backend).toUpperCase());
    if(full && s.commit) bits.push(s.commit);
    if(s.prebuilt && s.prebuilt.in_use && s.prebuilt.tag) bits.push(s.prebuilt.tag);
    if(full && typeof s.behind === 'number' && s.behind > 0) bits.push(s.behind + ' commit(s) en retard');
    if(metaEl) metaEl.textContent = bits.join(' · ');
    if(upg) upg.hidden = full;
  }

  const probe = document.getElementById('lc-probe');
  if(probe){
    const cur = (document.getElementById('lc-exist-path')||{}).value || '';
    const list = (s.probes || []).slice();
    let html = '<option value="">llama-server détectés…</option>';
    list.forEach(p => {
      html += '<option value="'+lcEsc(p).replace(/"/g,'&quot;')+'"'+(p===cur?' selected':'')+' title="'+lcEsc(p).replace(/"/g,'&quot;')+'">'+(typeof baseName==='function'?lcEsc(baseName(p)):lcEsc(p))+'</option>';
    });
    probe.innerHTML = html;
  }

  const note = document.getElementById('lc-server-note');
  if(note){
    if(s.server_gpu_note){
      note.hidden = false;
      note.textContent = s.server_gpu_note;
    } else {
      note.hidden = true;
      note.textContent = '';
    }
  }

  const fullDir = document.getElementById('lc-full-dir');
  if(fullDir && !fullDir.value && s.default_full_dir){
    fullDir.placeholder = s.default_full_dir;
  }

  const hints = lcPathHints(s);
  const exist = document.getElementById('lc-exist-path');
  if(exist && !exist.value) exist.placeholder = hints.bin;
  ['set-dir-path','set-dl-path','m-dir-path'].forEach(id=>{
    const el = document.getElementById(id);
    if(el && !el.value) el.placeholder = hints.dir;
  });
  const head = document.getElementById('lc-setup-head');
  if(head) head.hidden = !(has && showSetup);
  const chg = document.getElementById('lc-btn-change');
  if(chg) chg.textContent = (has && showSetup) ? 'fermer' : 'changer';
}

function lcPathHints(s){
  const os = (s && s.os) || '';
  if(os === 'windows') return {bin:'C:\\chemin\\llama-server.exe', dir:'C:\\chemin\\models', full:'C:\\chemin\\llama.cpp'};
  if(os === 'darwin') return {bin:'/chemin/llama-server', dir:'/chemin/models', full:'/chemin/llama.cpp'};
  return {bin:'/chemin/vers/llama-server', dir:'/chemin/vers/models', full:'/chemin/vers/llama.cpp'};
}

function lcShowSetup(on){
  lcSetupForced = !!on;
  lcRenderEngine(lcState || {});
  if(on){
    const el = document.getElementById('lc-setup');
    if(el) el.scrollIntoView({block:'nearest'});
  }
}

function lcPickProbe(){
  const sel = document.getElementById('lc-probe');
  const inp = document.getElementById('lc-exist-path');
  if(sel && inp && sel.value) inp.value = sel.value;
}

let lcSeenEnd = false, lcEndShown = false;
function lcChipLabel(action){
  return {install:'Compilation de llama.cpp', update:'Mise à jour du moteur',
          prebuilt:'Téléchargement de llama-server', custom:'Installation du backend'}[action] || 'Installation du moteur';
}
function lcChipSync(j){
  const chip = document.getElementById('lc-chip');
  if(!chip) return;
  if(!j || !j.exists || (!j.running && (!j.error || lcSeenEnd))){ chip.hidden = true; return; }
  chip.hidden = false;
  chip.classList.toggle('failed', !j.running && !!j.error);
  chip.textContent = j.running
    ? '⏳ ' + lcChipLabel(j.action) + ' — ' + (j.phase || '…')
    : '✗ ' + lcChipLabel(j.action) + ' interrompue';
}
function lcChipOpen(){
  if(typeof openSettings === 'function') openSettings('moteur');
  const det = document.getElementById('lc-details');
  if(det){ det.open = true; det.scrollIntoView({block:'center'}); }
  const chip = document.getElementById('lc-chip');
  if(chip && chip.classList.contains('failed')){
    lcSeenEnd = true;
    lcChipSync(null);
  }
}

async function lcCheckCurrent(){
  const s = lcState || {};
  toast('vérification…');
  try{
    if(s.kind === 'full'){
      const r = await jpost('/api/llamacpp/check', {});
      if(!r.ok){ toast('erreur : '+(r.error||'')); return; }
      if(r.behind > 0) toast(r.behind+' nouveau(x) commit(s) — utilise « mettre à jour »');
      else toast('llama.cpp à jour ✓');
    } else {
      const r = await jpost('/api/llamacpp/prebuilt/check', {});
      if(!r.ok){ toast('erreur : '+(r.error||'')); return; }
      if(r.update) toast('nouvelle version : '+r.latest+(r.current ? ' (actuelle : '+r.current+')' : ''));
      else toast('llama-server à jour ✓'+(r.latest ? ' ('+r.latest+')' : ''));
    }
  }catch(_){ toast('erreur réseau'); }
}

async function lcUpdateCurrent(){
  const s = lcState || {};
  if(s.kind === 'full'){
    if(!await askConfirm('Mettre à jour ce llama.cpp (git pull + recompile) ? Les autres apps qui utilisent ce dossier verront le nouveau binaire.', {title:'Mettre à jour', okText:'Mettre à jour'})) return;
    const r = await jpost('/api/llamacpp/update', {clean:false});
    if(!r.ok){ toast('erreur : '+(r.error||'')); return; }
  } else {
    if(!await askConfirm('Télécharger le dernier llama-server officiel et l\'utiliser ?', {title:'Mettre à jour', okText:'Télécharger'})) return;
    const r = await jpost('/api/llamacpp/prebuilt', {});
    if(!r.ok){ toast('erreur : '+(r.error||'')); return; }
  }
  lcStartPolling();
}

async function lcUseExisting(){
  const bin = (document.getElementById('lc-exist-path').value||'').trim();
  if(!bin){ toast('indique le chemin de llama-server'); return; }
  const r = await jpost('/api/llamacpp/use', {mode:'exist', bin});
  if(!r.ok){ toast('erreur : '+(r.error||'')); return; }
  lcSetupForced = false;
  toast(r.models_dir ? ('lié · models : '+r.models_dir) : ('lié · '+r.bin));
  loadLlamacpp();
  if(typeof loadCfg === 'function') loadCfg();
  if(typeof populateModelDirs === 'function') populateModelDirs();
  if(typeof refreshPicker === 'function') refreshPicker();
}

async function lcInstallServer(){
  const extra = (lcState && lcState.server_gpu_note) ? '\n\n'+lcState.server_gpu_note : '';
  if(!await askConfirm('Télécharger le binaire officiel de llama-server (sans les sources). llama.cpp complet pourra être ajouté ensuite.'+extra, {title:'Installer llama-server', okText:'Télécharger'})) return;
  const r = await jpost('/api/llamacpp/prebuilt', {});
  if(!r.ok){ toast('erreur : '+(r.error||'')); return; }
  lcSetupForced = false;
  lcStartPolling();
}

async function lcInstallFull(){
  const dir = (document.getElementById('lc-full-dir').value||'').trim();
  const where = dir || ((lcState && lcState.default_full_dir) || 'les données Loom');
  if(!await askConfirm('Cloner et compiler llama.cpp dans :\n'+where+'\n\nPlusieurs minutes possibles. Le résultat est un llama.cpp autonome.', {title:'Installer llama.cpp', okText:'Compiler'})) return;
  const r = await jpost('/api/llamacpp/install', {dir});
  if(!r.ok){ toast('erreur : '+(r.error||'')); return; }
  lcSetupForced = false;
  lcStartPolling();
}

async function lcUpgradeToFull(){
  const def = (lcState && lcState.default_full_dir) || '';
  const dir = await askPrompt('Dossier où cloner llama.cpp. Vide = données Loom. Le llama-server actuel reste en place tant que la compilation n’est pas finie.', {
    title:'Ajouter llama.cpp', okText:'Compiler', placeholder: def, default: def
  });
  if(dir === null) return;
  const r = await jpost('/api/llamacpp/install', {dir: String(dir||'').trim()});
  if(!r.ok){ toast('erreur : '+(r.error||'')); return; }
  lcSetupForced = false;
  lcStartPolling();
}

function lcBusy(on){
  const setup = document.getElementById('lc-setup');
  const linked = document.getElementById('lc-linked');
  if(setup) setup.classList.toggle('busy', on);
  if(linked) linked.classList.toggle('busy', on);
}

function lcStartPolling(){
  document.getElementById('lc-job').style.display = '';
  document.getElementById('lc-log').textContent = '';
  const dis = document.getElementById('lc-job-dismiss'); if(dis) dis.hidden = true;
  lcLogNext = 0;
  lcSeenEnd = false; lcEndShown = false;
  lcBusy(true);
  if(lcPoll) clearInterval(lcPoll);
  lcPoll = setInterval(lcPollJob, 1000);
  lcPollJob();
}

async function lcPollJob(quiet){
  let j;
  try{ j = await jget('/api/llamacpp/job?from='+lcLogNext); }catch(_){ return; }
  if(!j.exists) return;
  const phaseEl = document.getElementById('lc-job-phase');
  if(j.lines && j.lines.length){
    const pre = document.getElementById('lc-log');
    const stick = pre.scrollTop + pre.clientHeight >= pre.scrollHeight - 20;
    pre.textContent += j.lines.join('\n') + '\n';
    if(stick) pre.scrollTop = pre.scrollHeight;
  }
  if(typeof j.next === 'number') lcLogNext = j.next;
  lcChipSync(j);
  if(j.running){
    phaseEl.innerHTML = '<span class="lc-spin">⏳</span> <span>'+lcEsc(j.phase||'…')+'</span>';
    return;
  }
  if(lcPoll){ clearInterval(lcPoll); lcPoll = null; }
  lcBusy(false);
  const dis = document.getElementById('lc-job-dismiss');
  if(dis) dis.hidden = false;
  if(j.error){
    phaseEl.innerHTML = '<span style="color:var(--err)">✗ '+lcEsc(j.error)+'</span>';
    const pre = document.getElementById('lc-log');
    if(pre.hasAttribute('hidden')) lcToggleLog();
    pre.scrollTop = pre.scrollHeight;
    if(!quiet) toast('échec — voir les détails');
  } else {
    phaseEl.innerHTML = '<span style="color:var(--ok)">✓ '+lcEsc(j.phase||'terminé')+'</span>';
    if(!quiet) toast('c\'est prêt ✓');
  }
  if(!quiet) loadAll();
}

async function lcDismissJob(){
  try{
    const r = await jpost('/api/llamacpp/job/dismiss', {});
    if(!r.ok){ toast(r.error||'impossible de masquer'); return; }
  }catch(_){ toast('erreur réseau'); return; }
  if(lcPoll){ clearInterval(lcPoll); lcPoll = null; }
  document.getElementById('lc-job').style.display = 'none';
  const dis = document.getElementById('lc-job-dismiss'); if(dis) dis.hidden = true;
  lcSeenEnd = true; lcEndShown = false;
  lcChipSync(null);
}

function lcToggleLog(){
  const pre = document.getElementById('lc-log');
  const bar = document.querySelector('.lc-logbar');
  if(pre.hasAttribute('hidden')){ pre.removeAttribute('hidden'); bar.classList.add('open'); pre.scrollTop = pre.scrollHeight; }
  else { pre.setAttribute('hidden',''); bar.classList.remove('open'); }
}
