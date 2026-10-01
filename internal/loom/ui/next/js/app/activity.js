// Centre d'activité : téléchargements, travaux du moteur et file de bench en
// cours. Un bouton discret dans la barre latérale, une liste avec progression.
import { html, useState, useEffect, useRef, cls } from '../core/lib.js';
import { Icon } from '../ui/icons.js';
import { Popover } from '../ui/controls.js';
import { get, post } from '../core/api.js';
import { go } from '../core/state.js';

const pct = (a, b) => (b > 0 ? Math.min(100, Math.round(a * 100 / b)) : null);

async function readActivity() {
  const [dl, job, bench] = await Promise.all([
    get('/api/models/download/status').catch(() => null),
    get('/api/llamacpp/job').catch(() => null),
    get('/api/bench/queue').catch(() => null),
  ]);
  const items = [];
  for (const d of Array.isArray(dl) ? dl : []) {
    if (d.finished || d.canceled) continue;
    const go1 = n => (n / 1073741824).toFixed(1);
    items.push({ id: 'dl:' + d.filename, icon: 'download', title: d.filename, err: d.error,
      sub: d.error || (d.total ? `${go1(d.done)} / ${go1(d.total)} Go` + (d.speed ? ` · ${(d.speed / 1048576).toFixed(0)} Mo/s` : '') : 'démarrage…'),
      p: pct(d.done, d.total), cancel: d.error ? null : () => post('/api/models/download/cancel', { filename: d.filename }), to: () => go('local', 'hub') });
  }
  if (job && job.exists) {
    const label = { install: 'Installation de llama.cpp', update: 'Mise à jour de llama.cpp', prebuilt: 'Téléchargement de llama.cpp' }[job.action] || 'llama.cpp';
    items.push({ id: 'job', icon: 'server', title: label, err: !job.running && job.error ? job.error : '',
      sub: job.running ? (job.phase || 'en cours') : job.error ? job.error : 'terminé',
      p: null, cancel: job.running ? null : () => post('/api/llamacpp/job/dismiss', {}), cancelLabel: 'Masquer', to: () => go('settings', 'engine') });
  }
  const b = bench && bench.job;
  if (b && b.status === 'running') {
    const done = (b.rows || []).filter(r => ['ok', 'err', 'skip'].includes(r.status)).length;
    items.push({ id: 'bench', icon: 'gauge', title: 'Bench · ' + (b.test_name || 'file'), sub: `${done} / ${(b.rows || []).length} modèles`,
      p: pct(done, (b.rows || []).length), cancel: () => post('/api/bench/queue/cancel', {}), cancelLabel: 'Arrêter', to: () => go('bench') });
  }
  return items;
}

export function Activity() {
  const [items, setItems] = useState([]);
  const [anchor, setAnchor] = useState(null);
  const btn = useRef();
  useEffect(() => {
    let alive = true;
    const tick = async () => { if (document.hidden) return; const r = await readActivity(); if (alive) setItems(r); };
    tick(); const t = setInterval(tick, 3000);
    return () => { alive = false; clearInterval(t); };
  }, []);
  const running = items.filter(i => !i.err).length;
  return html`<button class=${cls('icon-btn act-btn', running && 'busy')} ref=${btn} aria-label=${'Activité' + (items.length ? ' · ' + items.length : '')} title="Activité"
      onClick=${() => setAnchor(anchor ? null : btn.current)}><${Icon} n="activity" />${items.length > 0 && html`<i class="act-dot"></i>`}</button>
    ${anchor && html`<${Popover} anchor=${anchor} onClose=${() => setAnchor(null)} width=${320} class="act-pop">
      <div class="act-h">Activité</div>
      ${items.length ? items.map(i => html`<div class="act-row" key=${i.id}>
        <${Icon} n=${i.icon} />
        <button type="button" class="act-main" onClick=${() => { setAnchor(null); i.to(); }}><b>${i.title}</b><small class=${i.err ? 'err' : ''}>${i.sub}</small>
          ${i.p != null && !i.err && html`<span class="gauge"><i style=${`width:${i.p}%`}></i></span>`}</button>
        ${i.cancel && html`<button class="btn sm ghost" onClick=${async () => { await i.cancel(); setItems(await readActivity()); }}>${i.cancelLabel || 'Annuler'}</button>`}
      </div>`) : html`<div class="act-empty">Rien en cours.</div>`}
    </${Popover}>`}`;
}
