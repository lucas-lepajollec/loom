import { t, getLang, setLang } from '../../core/i18n.js';
// Guide de premier lancement : langue, machine détectée, moteur (installer,
// lier ou s'en passer), premier modèle adapté à la mémoire, puis harnesses et
// cloud. Il ne s'affiche qu'une fois sur une installation neuve ; Réglages ›
// À propos permet de le relancer.
import { html, useState, useEffect, useStore, cls, fmtBytes } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Logo } from '../../ui/logo.js';
import { toast } from '../../ui/dialog.js';
import { get, post } from '../../core/api.js';
import { app, go, refreshLibrary, refreshStatus, refreshWorkspace, refreshEngineNode } from '../../core/state.js';
import { DirectEngineForm } from '../settings/page.js';
import { newDiscussion } from '../chat/engine.js';

// Modèles proposés, du plus léger au plus lourd (GGUF Q4_K_M).
const MODELS = [
  { id: 'qwen3-1.7b', name: 'Qwen3 1.7B', vendor: 'qwen', size: 1107409472, url: 'https://huggingface.co/unsloth/Qwen3-1.7B-GGUF/resolve/main/Qwen3-1.7B-Q4_K_M.gguf' },
  { id: 'qwen3-4b', name: 'Qwen3 4B', vendor: 'qwen', size: 2497281312, url: 'https://huggingface.co/unsloth/Qwen3-4B-GGUF/resolve/main/Qwen3-4B-Q4_K_M.gguf' },
  { id: 'gemma3-4b', name: 'Gemma 3 4B', vendor: 'google', size: 2489894016, url: 'https://huggingface.co/unsloth/gemma-3-4b-it-GGUF/resolve/main/gemma-3-4b-it-Q4_K_M.gguf' },
  { id: 'qwen3-8b', name: 'Qwen3 8B', vendor: 'qwen', size: 5027784512, url: 'https://huggingface.co/unsloth/Qwen3-8B-GGUF/resolve/main/Qwen3-8B-Q4_K_M.gguf' },
  { id: 'gemma3-12b', name: 'Gemma 3 12B', vendor: 'google', size: 7300778336, url: 'https://huggingface.co/unsloth/gemma-3-12b-it-GGUF/resolve/main/gemma-3-12b-it-Q4_K_M.gguf' },
  { id: 'qwen3-14b', name: 'Qwen3 14B', vendor: 'qwen', size: 9001753984, url: 'https://huggingface.co/unsloth/Qwen3-14B-GGUF/resolve/main/Qwen3-14B-Q4_K_M.gguf' },
  { id: 'gpt-oss-20b', name: 'gpt-oss 20B', vendor: 'openai', size: 11624759488, url: 'https://huggingface.co/unsloth/gpt-oss-20b-GGUF/resolve/main/gpt-oss-20b-Q4_K_M.gguf' },
];
// llama.cpp est prêt s'il est compilé (dépôt git) ou installé en binaire officiel.
const llamaReady = lc => !!(lc && (lc.installed || (lc.prebuilt && lc.prebuilt.bin)));
const STEPS = ['hello', 'machine', 'engine', 'model', 'tools', 'done'];

// Affiché seulement sur une installation neuve : aucun modèle, aucun moteur,
// aucune discussion. Une installation existante est marquée sans rien montrer.
export async function initWelcome() {
  try {
    const p = await get('/api/prefs');
    if (p.prefs && p.prefs.onboarded === '1') return;
    const [lc, models, hist] = await Promise.all([get('/api/llamacpp').catch(() => ({})), get('/api/models').catch(() => []), get('/api/chat/history').catch(() => [])]);
    const fresh = !llamaReady(lc) && !(models || []).length && !((hist && hist.conversations) || []).length;
    if (fresh) app.set({ welcome: true });
    else post('/api/prefs', { onboarded: '1' }).catch(() => {});
  } catch (_) {}
}
export const openWelcome = () => app.set({ welcome: true });

function finish(next) {
  post('/api/prefs', { onboarded: '1' }).catch(() => {});
  app.set({ welcome: false });
  refreshWorkspace(); refreshLibrary(); refreshStatus();
  if (next) next();
}

function Hello({ onNext }) {
  const lang = getLang();
  return html`<div class="wl-body">
    <span class="wl-mark"><svg viewBox="0 0 16 16" aria-hidden="true"><rect width="16" height="16" rx="4.5" fill="currentColor"></rect><g fill="none" stroke="var(--bg)" stroke-width="1.3" stroke-linecap="round"><path d="M4 6.2h8M4 9.8h8M6.2 4v8M9.8 4v8"></path></g></svg></span>
    <h1>${t('welcome.hello.title')}</h1>
    <p class="wl-lead">${t('welcome.hello.lead')}</p>
    <div class="wl-langs" role="radiogroup" aria-label="Langue / Language">
      ${[['fr', 'Français'], ['en', 'English']].map(([v, l]) => html`<button type="button" role="radio" aria-checked=${String(lang === v)} class=${cls('wl-lang', lang === v && 'on')} onClick=${() => setLang(v)}>${l}</button>`)}
    </div>
    <div class="wl-foot"><button class="btn ghost" onClick=${() => finish()}>${t('welcome.skip')}</button><span class="grow"></span><button class="btn primary" onClick=${onNext}>${t('welcome.hello.start')}<${Icon} n="right" /></button></div>
  </div>`;
}

function useMachine() {
  const [m, setM] = useState(null);
  useEffect(() => {
    Promise.all([get('/api/llamacpp').catch(() => ({})), get('/api/vram').catch(() => []), get('/api/ram').catch(() => null), get('/api/status').catch(() => ({}))])
      .then(([lc, gpus, ram, st]) => setM({ lc, gpus: gpus || [], ram, host: st.hostname || '' }));
  }, []);
  return m;
}
const vramMiB = m => Math.max(0, ...(m.gpus || []).map(g => g.total || 0));
const osName = os => ({ linux: 'Linux', darwin: 'macOS', windows: 'Windows' })[os] || os || '—';

function Machine({ m, onNext, onBack }) {
  const vram = vramMiB(m);
  return html`<div class="wl-body">
    <h2>${t('welcome.machine.title')}</h2>
    <p class="wl-lead">${t('welcome.machine.lead')}</p>
    <div class="card rows">
      <div class="row"><span class="mx-ico"><${Icon} n="server" /></span><div class="grow"><div class="t">${m.host || t('app.shell.cette_machine')}</div><div class="s">${osName(m.lc.os)} · ${m.lc.arch || ''}</div></div></div>
      <div class="row"><span class="mx-ico"><${Icon} n="chip" /></span><div class="grow"><div class="t">${t('welcome.machine.memory')}</div><div class="s">${m.ram ? fmtBytes(m.ram.total * 1048576) : '—'}</div></div></div>
      ${m.gpus.length ? m.gpus.map(g => html`<div class="row"><span class="mx-ico"><${Icon} n="gauge" /></span><div class="grow"><div class="t">${g.name}</div><div class="s">${t('welcome.machine.vram', { size: fmtBytes(g.total * 1048576) })}</div></div><span class="tag green">${t('welcome.machine.gpu_ok')}</span></div>`)
        : html`<div class="row"><span class="mx-ico"><${Icon} n="gauge" /></span><div class="grow"><div class="t">${t('welcome.machine.no_gpu')}</div><div class="s">${t('welcome.machine.no_gpu_text')}</div></div></div>`}
    </div>
    <p class="note">${vram >= 12000 ? t('welcome.machine.big') : vram >= 6000 ? t('welcome.machine.mid') : vram ? t('welcome.machine.small') : t('welcome.machine.cpu')}</p>
    <div class="wl-foot"><button class="btn ghost" onClick=${onBack}>${t('welcome.back')}</button><span class="grow"></span><button class="btn primary" onClick=${onNext}>${t('welcome.next')}<${Icon} n="right" /></button></div>
  </div>`;
}

// Installation de llama.cpp : binaire officiel ou compilation selon la
// recommandation du serveur (CUDA natif sous Linux = compilation).
function LlamaInstall({ lc, onReady }) {
  const [job, setJob] = useState(null);
  const [started, setStarted] = useState(false);
  useEffect(() => {
    if (!started) return;
    const id = setInterval(async () => {
      const j = await get('/api/llamacpp/job').catch(() => null);
      setJob(j);
      if (j && j.exists && !j.running) { clearInterval(id); if (!j.error) { refreshStatus(); onReady(); } }
    }, 1200);
    return () => clearInterval(id);
  }, [started]);
  const run = async compile => {
    const r = await post(compile ? '/api/llamacpp/install' : '/api/llamacpp/prebuilt', compile ? { dir: '' } : {});
    if (r.ok === false) return toast(r.error || t('settings.page.echec_2'), 'err');
    setStarted(true);
  };
  if (started) {
    const lines = ((job && job.lines) || []).slice(-6).join('\n');
    return html`<div class="wl-job">${job && job.error ? html`<p class="note err">${job.error}</p>` : html`<div class="state"><span class="spinner"></span>${(job && job.phase) || t('settings.page.installation_en_cours')}</div>`}
      ${lines && html`<pre class="mono">${lines}</pre>`}</div>`;
  }
  const compile = lc.reco && lc.reco.mode === 'opt';
  return html`<div class="wl-job">
    ${lc.reco && lc.reco.why && html`<p class="note">${({ 'linux-cuda': t('welcome.reco.linux-cuda'), 'linux-hip': t('welcome.reco.linux-hip') })[lc.reco.code] || lc.reco.why}</p>`}
    <div class="wl-acts"><button class="btn primary" onClick=${() => run(compile)}>${compile ? t('welcome.engine.compile') : t('welcome.engine.download')}</button>
      <button class="btn ghost" onClick=${() => run(!compile)}>${compile ? t('welcome.engine.download_instead') : t('welcome.engine.compile_instead')}</button></div>
  </div>`;
}

function VllmInstall({ onReady }) {
  const [x, setX] = useState(null);
  const load = () => get('/api/engines/vllm').then(setX).catch(() => setX(null));
  useEffect(() => { load(); }, []);
  useEffect(() => { if (!x || !x.job) return; const id = setInterval(load, 2500); return () => clearInterval(id); }, [x && x.job]);
  useEffect(() => { if (x && x.installed && !x.job) onReady(); }, [x && x.installed, x && x.job]);
  if (!x) return null;
  if (x.missing) return html`<p class="note err">${x.missing}</p>`;
  if (x.job) return html`<div class="wl-job"><div class="state"><span class="spinner"></span>${t('settings.page.installation_plusieurs_minutes')}</div>${x.log && html`<pre class="mono">${x.log.split('\n').slice(-6).join('\n')}</pre>`}</div>`;
  if (x.installed) return html`<p class="note">${t('welcome.engine.vllm_ready')}</p>`;
  return html`<div class="wl-job"><p class="note">${t('welcome.engine.vllm_note')}</p>
    <div class="wl-acts"><button class="btn primary" onClick=${async () => { const r = await post('/api/engines/vllm', { action: 'install' }); if (!r.ok) return toast(r.error, 'err'); load(); }}>${t('settings.page.installer_vllm')}</button></div></div>`;
}

function Engine({ m, choice, setChoice, onNext, onBack }) {
  const [ready, setReady] = useState(false);
  const linux = m.lc.os === 'linux', nvidia = (m.gpus || []).some(g => /nvidia/i.test(g.name));
  const opts = [
    { id: 'llama', name: 'llama.cpp', text: t('welcome.engine.llama'), tag: t('welcome.engine.recommended'), done: llamaReady(m.lc) },
    { id: 'vllm', name: 'vLLM', text: t('welcome.engine.vllm'), off: !(linux && nvidia) && t('welcome.engine.vllm_off') },
    { id: 'link', name: t('welcome.engine.link'), text: t('welcome.engine.link_text') },
    { id: 'none', name: t('welcome.engine.none'), text: t('welcome.engine.none_text') },
  ];
  const canGo = choice === 'none' || (choice === 'llama' && (llamaReady(m.lc) || ready)) || (choice === 'vllm' && ready) || (choice === 'link' && ready);
  return html`<div class="wl-body">
    <h2>${t('welcome.engine.title')}</h2>
    <p class="wl-lead">${t('welcome.engine.lead')}</p>
    <div class="wl-opts" role="radiogroup">${opts.map(o => html`<button type="button" role="radio" aria-checked=${String(choice === o.id)} disabled=${!!o.off} class=${cls('wl-opt', choice === o.id && 'on')} onClick=${() => { setChoice(o.id); setReady(false); }}>
      <span class="wl-radio"></span><span class="grow"><b>${o.name}${o.tag && html` <span class="tag">${o.tag}</span>`}${o.done && html` <span class="tag green">${t('welcome.engine.installed')}</span>`}</b><small>${o.off || o.text}</small></span></button>`)}</div>
    ${choice === 'llama' && !llamaReady(m.lc) && html`<${LlamaInstall} lc=${m.lc} onReady=${() => setReady(true)} />`}
    ${choice === 'llama' && (llamaReady(m.lc) || ready) && html`<p class="note">${t('welcome.engine.llama_ready')}</p>`}
    ${choice === 'vllm' && html`<${VllmInstall} onReady=${() => setReady(true)} />`}
    ${choice === 'link' && (ready ? html`<p class="note">${t('welcome.engine.linked')}</p>` : html`<${DirectEngineForm} onDone=${() => { refreshEngineNode(); setReady(true); }} />`)}
    <div class="wl-foot"><button class="btn ghost" onClick=${onBack}>${t('welcome.back')}</button><span class="grow"></span>
      ${!canGo && choice !== 'none' && html`<button class="btn ghost" onClick=${() => onNext(true)}>${t('welcome.later')}</button>`}
      <button class="btn primary" disabled=${!canGo} onClick=${() => onNext()}>${t('welcome.next')}<${Icon} n="right" /></button></div>
  </div>`;
}

// Premier modèle : les modèles qui tiennent dans la mémoire GPU (ou la moitié
// de la RAM sans GPU), le plus gros d'entre eux sous 10 Go étant recommandé.
function Model({ m, onNext, onBack }) {
  const budget = (vramMiB(m) || (m.ram ? m.ram.total / 2 : 4096)) * 1048576 * 0.85;
  const fit = MODELS.filter(x => x.size * 1.15 <= budget);
  const reco = fit.filter(x => x.size < 10e9).slice(-1)[0];
  const [sel, setSel] = useState(reco ? reco.id : (MODELS[0] || {}).id);
  const [dl, setDl] = useState(null);
  const pick = MODELS.find(x => x.id === sel);
  useEffect(() => {
    if (!dl || dl.finished || dl.error) return;
    const id = setTimeout(async () => {
      const list = await get('/api/models/download/status').catch(() => []);
      const d = (list || []).find(x => x.filename === dl.filename);
      if (d) { setDl(d); if (d.finished) refreshLibrary(); }
      else setDl({ ...dl });
    }, 900);
    return () => clearTimeout(id);
  }, [dl]);
  const start = async () => {
    const p = await post('/api/models/download/probe', { url: pick.url, dir: '' });
    if (!p.ok || !p.enough) return toast(p.error || t('local.hub.espace_disque_insuffisant'), 'err');
    const r = await post('/api/models/download', { url: pick.url, dir: '' });
    if (!r.ok) return toast(r.error || t('local.hub.telechargement_impossible'), 'err');
    setDl({ filename: r.filename || pick.url.split('/').pop(), done: 0, total: pick.size });
  };
  const pct = dl && dl.total ? Math.round(dl.done / dl.total * 100) : 0;
  return html`<div class="wl-body">
    <h2>${t('welcome.model.title')}</h2>
    <p class="wl-lead">${t('welcome.model.lead')}</p>
    <div class="wl-opts" role="radiogroup">${MODELS.map(x => { const ok = fit.includes(x); return html`<button type="button" role="radio" aria-checked=${String(sel === x.id)} disabled=${!!dl} class=${cls('wl-opt', 'sm', sel === x.id && 'on', !ok && 'dim')} onClick=${() => setSel(x.id)}>
      <span class="wl-radio"></span><${Logo} name=${x.vendor} size="sm" /><span class="grow"><b>${x.name}${reco && reco.id === x.id && html` <span class="tag">${t('welcome.engine.recommended')}</span>`}</b><small>${fmtBytes(x.size)} · ${ok ? t('welcome.model.fits') : t('welcome.model.tight')}</small></span></button>`; })}</div>
    ${dl && html`<div class="wl-job">${dl.error ? html`<p class="note err">${dl.error}</p>` : dl.finished ? html`<p class="note">${t('welcome.model.done', { name: pick.name })}</p>`
      : html`<div class="state">${t('welcome.model.downloading', { done: fmtBytes(dl.done), total: fmtBytes(dl.total) })}</div><div class="meter"><i style=${`width:${pct}%;background:var(--text)`}></i></div>`}</div>`}
    <p class="note">${t('welcome.model.more')}</p>
    <div class="wl-foot"><button class="btn ghost" onClick=${onBack}>${t('welcome.back')}</button><span class="grow"></span>
      ${!dl ? html`<button class="btn ghost" onClick=${onNext}>${t('welcome.later')}</button><button class="btn primary" onClick=${start}><${Icon} n="download" />${t('welcome.model.download')}</button>`
        : html`<button class="btn primary" onClick=${onNext}>${dl.finished ? t('welcome.next') : t('welcome.model.continue')}<${Icon} n="right" /></button>`}</div>
  </div>`;
}

function Tools({ onNext, onBack }) {
  const ws = useStore(app, x => x.workspace);
  useEffect(() => { refreshWorkspace(); }, []);
  const harnesses = ((ws && ws.runtimes) || []).filter(r => r.kind === 'harness' && !r.custom && r.id !== 'loom-fake-acp');
  const found = harnesses.filter(r => r.available), missing = harnesses.filter(r => !r.available);
  return html`<div class="wl-body">
    <h2>${t('welcome.tools.title')}</h2>
    <p class="wl-lead">${t('welcome.tools.lead')}</p>
    <div class="card rows">
      ${found.map(r => html`<div class="row"><${Logo} name=${r.logo || r.id} size="sm" /><div class="grow"><div class="t">${r.name}</div><div class="s">${t('welcome.tools.found')}</div></div><span class="tag green">${t('welcome.tools.ready')}</span></div>`)}
      ${missing.length ? html`<div class="row"><span class="mx-ico"><${Icon} n="plug" /></span><div class="grow"><div class="t">${t('welcome.tools.others')}</div><div class="s">${missing.map(r => r.name).join(', ')}</div></div><button class="btn sm ghost" onClick=${() => finish(() => go('harnesses'))}>${t('welcome.tools.install')}</button></div>` : ''}
      <div class="row"><span class="mx-ico"><${Icon} n="cloud" /></span><div class="grow"><div class="t">${t('welcome.tools.cloud')}</div><div class="s">${t('welcome.tools.cloud_text')}</div></div><button class="btn sm ghost" onClick=${() => finish(() => go('cloud'))}>${t('welcome.tools.connect')}</button></div>
      <div class="row"><span class="mx-ico"><${Icon} n="server" /></span><div class="grow"><div class="t">${t('welcome.tools.machines')}</div><div class="s">${t('welcome.tools.machines_text')}</div></div><button class="btn sm ghost" onClick=${() => finish(() => go('settings', 'machines'))}>${t('welcome.tools.add')}</button></div>
    </div>
    <div class="wl-foot"><button class="btn ghost" onClick=${onBack}>${t('welcome.back')}</button><span class="grow"></span><button class="btn primary" onClick=${onNext}>${t('welcome.next')}<${Icon} n="right" /></button></div>
  </div>`;
}

function Done() {
  return html`<div class="wl-body">
    <span class="wl-mark ok"><${Icon} n="check" /></span>
    <h1>${t('welcome.done.title')}</h1>
    <p class="wl-lead">${t('welcome.done.lead')}</p>
    <div class="wl-foot"><button class="btn primary" onClick=${() => finish(() => { go('chat'); newDiscussion(); })}>${t('welcome.done.chat')}</button></div>
  </div>`;
}

export function Welcome() {
  const on = useStore(app, x => x.welcome);
  const [step, setStep] = useState(0);
  const [choice, setChoice] = useState('llama');
  const m = useMachine();
  if (!on) return null;
  const name = STEPS[step];
  const next = skipModel => setStep(s => {
    let n = s + 1;
    // Sans llama.cpp prêt, pas de modèle GGUF à proposer.
    if (STEPS[n] === 'model' && (skipModel === true || choice !== 'llama')) n++;
    return Math.min(n, STEPS.length - 1);
  });
  const back = () => setStep(s => { let n = s - 1; if (STEPS[n] === 'model' && choice !== 'llama') n--; return Math.max(0, n); });
  return html`<div class="onboard" role="dialog" aria-modal="true" aria-label=${t('welcome.hello.title')}>
    <div class="wl-card anim-rise" key=${name}>
      ${step > 0 && step < STEPS.length - 1 && html`<div class="wl-steps" aria-hidden="true">${STEPS.slice(1, -1).map((s, i) => html`<i class=${cls(i + 1 < step && 'past', i + 1 === step && 'now')}></i>`)}</div>`}
      ${!m && step > 0 ? html`<div class="skeleton" style="height:240px"></div>`
        : name === 'hello' ? html`<${Hello} onNext=${() => next()} />`
        : name === 'machine' ? html`<${Machine} m=${m} onNext=${() => next()} onBack=${back} />`
        : name === 'engine' ? html`<${Engine} m=${m} choice=${choice} setChoice=${setChoice} onNext=${later => next(later === true)} onBack=${back} />`
        : name === 'model' ? html`<${Model} m=${m} onNext=${() => next()} onBack=${back} />`
        : name === 'tools' ? html`<${Tools} onNext=${() => next()} onBack=${back} />`
        : html`<${Done} />`}
    </div>
  </div>`;
}
